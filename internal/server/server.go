package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/iodesystems/ubuntu-router/internal/config"
	"github.com/iodesystems/ubuntu-router/internal/dnsmasq"
	"github.com/iodesystems/ubuntu-router/internal/haproxy"
	"github.com/iodesystems/ubuntu-router/internal/health"
	"github.com/iodesystems/ubuntu-router/internal/multiwan"
	"github.com/iodesystems/ubuntu-router/internal/network"
	"github.com/iodesystems/ubuntu-router/internal/notifications"
	"github.com/iodesystems/ubuntu-router/internal/qos"
	"github.com/iodesystems/ubuntu-router/internal/rollback"
	"github.com/iodesystems/ubuntu-router/internal/services"
	"github.com/iodesystems/ubuntu-router/internal/startup"
	"github.com/iodesystems/ubuntu-router/internal/stats"
	"github.com/iodesystems/ubuntu-router/internal/system"
	"github.com/iodesystems/ubuntu-router/internal/wifi"
	"github.com/iodesystems/ubuntu-router/internal/wireguard"
)

// Server is the main HTTP server
type Server struct {
	mu         sync.RWMutex
	config     *config.Config
	configPath string
	adminPassword string
	version    string
	dryRun     bool

	// Subsystems
	fs           system.FileSystem
	runner       system.CommandRunner
	services     *system.ServiceManager
	network      *network.Manager
	dns          *dnsmasq.Manager
	wireguard    *wireguard.Manager
	wireguardP2P *wireguard.P2PManager
	wifi         *wifi.Manager
	multiwan     *multiwan.Manager
	deps         *system.DependencyChecker
	health       *health.Runner
	bootstrap    *startup.Bootstrap
	rollback     *rollback.Manager
	stats        *stats.Manager
	statsStore   stats.Store
	qos          *qos.Manager
	notifier     *notifications.Notifier
	haproxy      *haproxy.Manager
	svcManager   *services.Manager

	// HTTP
	templates   *template.Template
	webDistPath string // Path to built Vite app (web/dist)

	// Dynamic DNS
	lastPublicIP string // Last known public IP for dynamic DNS sync
}

// New creates a new server instance
func New(cfg *config.Config, configPath string, dryRun bool, version string) (*Server, error) {
	// Set up system abstraction
	var fs system.FileSystem
	var runner system.CommandRunner

	if dryRun {
		fs = system.NewDryRunFileSystem()
		runner = system.NewDryRunCommandRunner()
		log.Println("Running in dry-run mode")
	} else {
		fs = system.RealFileSystem{}
		runner = system.RealCommandRunner{}
	}

	// Load or generate admin password
	password, isNew, err := config.LoadPassword(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load password: %w", err)
	}
	if isNew {
		fmt.Fprintf(os.Stderr, "Generated new admin password: %s\n", password)
	}

	// Find web/dist directory (check relative to executable, then config dir)
	webDistPath := ""
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidate := filepath.Join(exeDir, "web", "dist")
		if _, err := os.Stat(filepath.Join(candidate, "index.html")); err == nil {
			webDistPath = candidate
		}
	}
	if webDistPath == "" {
		// Try relative to config path
		configDir := filepath.Dir(configPath)
		candidate := filepath.Join(configDir, "web", "dist")
		if _, err := os.Stat(filepath.Join(candidate, "index.html")); err == nil {
			webDistPath = candidate
		}
	}
	if webDistPath == "" {
		// Try current working directory
		if cwd, err := os.Getwd(); err == nil {
			candidate := filepath.Join(cwd, "web", "dist")
			if _, err := os.Stat(filepath.Join(candidate, "index.html")); err == nil {
				webDistPath = candidate
			}
		}
	}
	if webDistPath != "" {
		log.Printf("Serving web UI from %s", webDistPath)
	}

	s := &Server{
		config:        cfg,
		configPath:    configPath,
		adminPassword: password,
		version:       version,
		dryRun:        dryRun,
		fs:            fs,
		runner:        runner,
		services:      system.NewServiceManager(runner),
		webDistPath:   webDistPath,
	}

	// Initialize subsystems
	s.network = network.NewManager(fs, runner)
	s.dns = dnsmasq.New(fs, runner, cfg.DNSConfigPath, cfg.DHCPConfigPath, cfg.DNSHostsPath)
	if cfg.WireGuard != nil {
		s.wireguard = wireguard.New(fs, runner, cfg.WireGuard.ConfigPath, cfg.WireGuard.Interface)
	}
	s.wireguardP2P = wireguard.NewP2P(fs, runner)
	s.wifi = wifi.New(fs, runner)
	s.multiwan = multiwan.New(runner, fs)
	s.deps = system.NewDependencyChecker(runner)
	s.rollback = rollback.NewManager(fs, runner, configPath)
	s.stats = stats.New(fs, runner)
	s.qos = qos.New(runner)

	// Initialize HAProxy manager
	certDir := "/etc/ubuntu-router/certs"
	if cfg.ExternalDNS != nil && cfg.ExternalDNS.CertDir != "" {
		certDir = cfg.ExternalDNS.CertDir
	}
	s.haproxy = haproxy.New(fs, runner, certDir)

	// Initialize stats persistence store
	configDir := filepath.Dir(configPath)
	statsStore := stats.NewSQLiteStore(configDir, dryRun)
	if err := statsStore.Open(context.Background()); err != nil {
		log.Printf("Warning: Failed to initialize stats persistence: %v", err)
		// Continue without persistence - not fatal
	} else {
		s.stats.SetStore(statsStore)
		s.statsStore = statsStore
		if !dryRun {
			log.Printf("Stats persistence enabled at %s", statsStore.DBPath())
		}

		// Migrate config data to SQLite if not already migrated
		s.migrateConfigToSQLite(context.Background(), cfg)
	}

	// Initialize notifications
	s.notifier = notifications.New(statsStore)
	if cfg.Notifications != nil {
		s.notifier.SetConfig(notifications.Config{
			Enabled:            cfg.Notifications.Enabled,
			Server:             cfg.Notifications.Server,
			Topic:              cfg.Notifications.Topic,
			Token:              cfg.Notifications.Token,
			Username:           cfg.Notifications.Username,
			Password:           cfg.Notifications.Password,
			NotifyWiFiClients:  cfg.Notifications.NotifyWiFiClients,
			NotifyHealthChecks: cfg.Notifications.NotifyHealthChecks,
			NotifyWANChanges:   cfg.Notifications.NotifyWANChanges,
			NotifyVPNPeers:     cfg.Notifications.NotifyVPNPeers,
			NotifySystemEvents: cfg.Notifications.NotifySystemEvents,
		})
	}

	// Set up rollback callback to reload config in server
	s.rollback.SetOnRollback(func(cfg *config.Config) {
		s.mu.Lock()
		s.config = cfg
		s.mu.Unlock()
		log.Printf("Config reloaded after rollback")
	})

	// Initialize services manager
	s.svcManager = services.New(cfg, configPath, fs, runner, s.dns, s.haproxy)

	// Initialize bootstrap (creates health runner with all checks registered)
	// Bootstrap handles both startup configuration and ongoing health checks
	s.bootstrap = startup.New(runner, fs, cfg, s.wireguardP2P)
	s.health = s.bootstrap.GetHealthRunner()

	// Configure multi-WAN manager with WAN list from config
	if len(cfg.WANs) > 0 {
		s.multiwan.Configure(cfg)
	}

	// Parse templates
	s.parseTemplates()

	return s, nil
}

// Run starts the HTTP server
func (s *Server) Run() error {
	// Compute effective listen addresses
	listenAddrs := s.computeListenAddresses()

	// Print admin password
	fmt.Println()
	fmt.Println("========================================")
	fmt.Printf("Admin Password: %s\n", s.adminPassword)
	for _, addr := range listenAddrs {
		fmt.Printf("Admin URL: http://%s/admin\n", addr)
	}
	fmt.Println("========================================")
	fmt.Println()

	ctx := context.Background()

	// Run startup bootstrap to apply essential config via health checks with auto-fix
	// This ensures the router works after reboot without manual intervention
	// Runs async with panic recovery to ensure web UI always starts
	if !s.dryRun {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("Warning: startup bootstrap panic recovered: %v", r)
				}
			}()
			if err := s.bootstrap.Run(ctx); err != nil {
				log.Printf("Warning: startup bootstrap failed: %v", err)
			}
		}()
	}

	// Start multi-WAN failover monitoring in background if there are configured WANs
	// This runs async to ensure the web UI is always available even if WAN config has issues
	if len(s.config.WANs) > 0 {
		go func() {
			// Recover from any panic in multi-WAN to prevent server crash
			defer func() {
				if r := recover(); r != nil {
					log.Printf("Warning: Multi-WAN panic recovered: %v", r)
				}
			}()
			if err := s.multiwan.Start(ctx); err != nil {
				log.Printf("Warning: Failed to start multi-WAN: %v", err)
			}
		}()
	}

	// Set up WiFi client callback for notifications
	s.stats.SetWiFiClientCallback(func(ctx context.Context, clients []stats.WiFiClientRates) {
		s.notifier.UpdateConnectedClients(ctx, clients)
	})

	// Start stats sampling in background
	s.stats.StartSampling(ctx, stats.DefaultSampleInterval)

	// Send system startup notification
	go s.notifier.NotifySystemStartup(ctx)

	// Start dynamic DNS sync if enabled
	s.startDynamicDNSSync(ctx)

	// Set up routes
	mux := s.setupRoutes()
	handler := s.loggingMiddleware(mux)

	// Start listeners on all configured addresses
	return s.startListeners(ctx, listenAddrs, handler)
}

// computeListenAddresses returns the list of addresses the server should listen on.
// Priority:
// 1. WebListenAddresses from config (if set)
// 2. ListenAddr from config (legacy, single address)
// 3. Default: all LAN addresses on port 8080
func (s *Server) computeListenAddresses() []string {
	configAddrs := s.config.GetWebListenAddresses()
	if len(configAddrs) > 0 {
		return configAddrs
	}

	// Default: bind to each LAN IP address on port 8080
	port := s.config.GetWebListenPort()
	if port == "" {
		port = ":8080"
	}

	var addrs []string
	for _, lanCIDR := range s.config.LANAddresses {
		// Extract IP from CIDR (e.g., "192.168.2.1/24" -> "192.168.2.1")
		lanIP := strings.Split(lanCIDR, "/")[0]
		addrs = append(addrs, lanIP+port)
	}

	// If no LAN addresses configured, fall back to all interfaces
	if len(addrs) == 0 {
		addrs = []string{port}
	}

	return addrs
}

// startListeners starts HTTP servers on all specified addresses
func (s *Server) startListeners(ctx context.Context, addrs []string, handler http.Handler) error {
	if len(addrs) == 0 {
		return fmt.Errorf("no listen addresses configured")
	}

	// Start each listener in a goroutine - they will wait for their IP if needed
	errCh := make(chan error, len(addrs))
	for _, addr := range addrs {
		go s.startListener(ctx, addr, handler, errCh)
	}

	// Wait for first error or context cancellation
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// startListener starts an HTTP server on a single address, waiting for the IP if needed
func (s *Server) startListener(ctx context.Context, addr string, handler http.Handler, errCh chan<- error) {
	// Check if the IP is available (for IP-bound addresses, not :port format)
	// Skip check for wildcard addresses (0.0.0.0, ::) and :port format
	if !strings.HasPrefix(addr, ":") {
		ip := strings.Split(addr, ":")[0]
		if ip != "0.0.0.0" && ip != "::" && ip != "" {
			if !s.isIPAvailable(ip) {
				log.Printf("IP %s not yet available, waiting...", ip)
				// Wait for the IP to become available
				s.waitForIP(ctx, ip)
				if ctx.Err() != nil {
					return
				}
				log.Printf("IP %s is now available, starting listener", ip)
			}
		}
	}

	server := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	log.Printf("Starting server on %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		errCh <- fmt.Errorf("listener %s: %w", addr, err)
	}
}

// waitForIP blocks until the given IP is available on the system
func (s *Server) waitForIP(ctx context.Context, ip string) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.isIPAvailable(ip) {
				log.Printf("IP %s is now available", ip)
				return
			}
		}
	}
}

// isIPAvailable checks if an IP address is configured on any interface
func (s *Server) isIPAvailable(ip string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out, err := s.runner.Run(ctx, "ip", "-4", "addr", "show")
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "inet "+ip)
}

func (s *Server) setupRoutes() *http.ServeMux {
	mux := http.NewServeMux()

	// Health check (always available)
	mux.HandleFunc("/health", s.handleHealth)

	// Auth endpoints (no auth required)
	mux.HandleFunc("/api/auth/status", s.handleAPIAuthStatus)
	mux.HandleFunc("/api/auth/login", s.handleAPIAuthLogin)
	mux.HandleFunc("/api/auth/logout", s.handleAPIAuthLogout)

	// WebSocket endpoints - require authentication
	mux.HandleFunc("/api/ws/logs", s.requireAPIAuth(s.handleWebSocketLogs))

	// API endpoints (JSON responses) - require authentication
	mux.HandleFunc("/api/status", s.requireAPIAuth(s.handleAPIStatus))
	mux.HandleFunc("/api/config", s.requireAPIAuth(s.handleAPIConfig))
	mux.HandleFunc("/api/config/pending", s.requireAPIAuth(s.handleAPIConfigPending))
	mux.HandleFunc("/api/config/confirm", s.requireAPIAuth(s.handleAPIConfigConfirm))
	mux.HandleFunc("/api/config/cancel", s.requireAPIAuth(s.handleAPIConfigCancel))
	mux.HandleFunc("/api/apply", s.requireAPIAuth(s.handleAPIApply))
	mux.HandleFunc("/api/interfaces", s.requireAPIAuth(s.handleAPIInterfaces))
	mux.HandleFunc("/api/interfaces/state", s.requireAPIAuth(s.handleAPIInterfaceState))
	mux.HandleFunc("/api/routes", s.requireAPIAuth(s.handleAPIRoutes))
	mux.HandleFunc("/api/leases", s.requireAPIAuth(s.handleAPILeases))
	mux.HandleFunc("/api/dns/status", s.requireAPIAuth(s.handleAPIDNSStatus))
	mux.HandleFunc("/api/dns/entries", s.requireAPIAuth(s.handleAPIDNSEntries))
	mux.HandleFunc("/api/dns/entries/delete", s.requireAPIAuth(s.handleAPIDNSEntriesDelete))
	mux.HandleFunc("/api/dns/settings", s.requireAPIAuth(s.handleAPIDNSSettings))
	mux.HandleFunc("/api/dhcp/reservations", s.requireAPIAuth(s.handleAPIDHCPReservations))
	mux.HandleFunc("/api/dhcp/reservations/delete", s.requireAPIAuth(s.handleAPIDHCPReservationsDelete))
	mux.HandleFunc("/api/dhcp/renew", s.requireAPIAuth(s.handleAPIDHCPRenew))
	mux.HandleFunc("/api/dhcp/settings", s.requireAPIAuth(s.handleAPIDHCPSettings))
	mux.HandleFunc("/api/wireguard/status", s.requireAPIAuth(s.handleAPIWireGuardStatus))
	mux.HandleFunc("/api/wireguard/settings", s.requireAPIAuth(s.handleAPIWireGuardSettings))
	mux.HandleFunc("/api/wireguard/peers", s.requireAPIAuth(s.handleAPIWireGuardPeers))
	mux.HandleFunc("/api/wireguard/peers/delete", s.requireAPIAuth(s.handleAPIWireGuardPeersDelete))
	mux.HandleFunc("/api/wireguard/control", s.requireAPIAuth(s.handleAPIWireGuardControl))
	mux.HandleFunc("/api/wireguard/p2p/status", s.requireAPIAuth(s.handleAPIWireGuardP2PStatus))
	mux.HandleFunc("/api/wireguard/p2p/tunnels", s.requireAPIAuth(s.handleAPIWireGuardP2PTunnels))
	mux.HandleFunc("/api/wireguard/p2p/tunnels/", s.requireAPIAuth(s.handleAPIWireGuardP2PTunnel))
	mux.HandleFunc("/api/wireguard/p2p/generate-keys", s.requireAPIAuth(s.handleAPIWireGuardP2PGenerateKeys))
	mux.HandleFunc("/api/wireguard/p2p/import-config", s.requireAPIAuth(s.handleAPIWireGuardP2PImportConfig))
	mux.HandleFunc("/api/wifi/status", s.requireAPIAuth(s.handleAPIWiFiStatus))
	mux.HandleFunc("/api/wifi/interfaces", s.requireAPIAuth(s.handleAPIWiFiInterfaces))
	mux.HandleFunc("/api/wifi/cards", s.requireAPIAuth(s.handleAPIWiFiCards))
	mux.HandleFunc("/api/wifi/config", s.requireAPIAuth(s.handleAPIWiFiConfig))
	mux.HandleFunc("/api/wifi/configure", s.requireAPIAuth(s.handleAPIWiFiConfigure))
	mux.HandleFunc("/api/wifi/control", s.requireAPIAuth(s.handleAPIWiFiControl))
	mux.HandleFunc("/api/wifi/delete", s.requireAPIAuth(s.handleAPIWiFiDelete))
	mux.HandleFunc("/api/wifi/stations", s.requireAPIAuth(s.handleAPIWiFiStations))
	mux.HandleFunc("/api/wifi/debug", s.requireAPIAuth(s.handleAPIWiFiDebug))
	mux.HandleFunc("/api/firewall/forwards", s.requireAPIAuth(s.handleAPIFirewallForwards))
	mux.HandleFunc("/api/firewall/forwards/delete", s.requireAPIAuth(s.handleAPIFirewallForwardsDelete))
	mux.HandleFunc("/api/wan/status", s.requireAPIAuth(s.handleAPIWANStatus))
	mux.HandleFunc("/api/wan/detect", s.requireAPIAuth(s.handleAPIWANDetect))
	mux.HandleFunc("/api/wan/configure", s.requireAPIAuth(s.handleAPIWANConfigure))
	mux.HandleFunc("/api/wan/wifi/scan", s.requireAPIAuth(s.handleAPIWANWiFiScan))
	mux.HandleFunc("/api/wan/wifi/status", s.requireAPIAuth(s.handleAPIWANWiFiStatus))
	mux.HandleFunc("/api/wan/wifi/connect", s.requireAPIAuth(s.handleAPIWANWiFiConnect))
	mux.HandleFunc("/api/wan/wifi/disconnect", s.requireAPIAuth(s.handleAPIWANWiFiDisconnect))
	mux.HandleFunc("/api/multiwan/status", s.requireAPIAuth(s.handleAPIMultiWANStatus))
	mux.HandleFunc("/api/multiwan/configure", s.requireAPIAuth(s.handleAPIMultiWANConfigure))
	mux.HandleFunc("/api/multiwan/switch", s.requireAPIAuth(s.handleAPIMultiWANSwitch))
	// WAN list endpoints (new unified WAN management)
	mux.HandleFunc("/api/wans", s.requireAPIAuth(s.handleAPIWANs))
	mux.HandleFunc("/api/wans/", s.requireAPIAuth(s.handleAPIWANByID))
	mux.HandleFunc("/api/wans/reorder", s.requireAPIAuth(s.handleAPIWANsReorder))
	mux.HandleFunc("/api/wans/autodetect", s.requireAPIAuth(s.handleAPIWANsAutoDetect))
	mux.HandleFunc("/api/system/dependencies", s.requireAPIAuth(s.handleAPISystemDependencies))
	mux.HandleFunc("/api/system/logs", s.requireAPIAuth(s.handleAPISystemLogs))
	mux.HandleFunc("/api/system/shutdown", s.requireAPIAuth(s.handleAPISystemShutdown))
	mux.HandleFunc("/api/system/reboot", s.requireAPIAuth(s.handleAPISystemReboot))
	mux.HandleFunc("/api/health/check", s.requireAPIAuth(s.handleAPIHealthCheck))
	mux.HandleFunc("/api/health/fix", s.requireAPIAuth(s.handleAPIHealthFix))

	// Statistics
	mux.HandleFunc("/api/stats/interfaces", s.requireAPIAuth(s.handleAPIStatsInterfaces))
	mux.HandleFunc("/api/stats/latency", s.requireAPIAuth(s.handleAPIStatsLatency))
	mux.HandleFunc("/api/stats/snapshot", s.requireAPIAuth(s.handleAPIStatsSnapshot))
	mux.HandleFunc("/api/stats/wifi/clients", s.requireAPIAuth(s.handleAPIStatsWiFiClients))
	mux.HandleFunc("/api/stats/history", s.requireAPIAuth(s.handleAPIStatsHistory))
	mux.HandleFunc("/api/stats/wifi-clients/history", s.requireAPIAuth(s.handleAPIStatsWiFiClientHistory))

	// Devices
	mux.HandleFunc("/api/devices", s.requireAPIAuth(s.handleAPIDevices))
	mux.HandleFunc("/api/devices/", s.requireAPIAuth(s.handleAPIDevice))

	// Notifications
	mux.HandleFunc("/api/notifications/settings", s.requireAPIAuth(s.handleAPINotificationsSettings))
	mux.HandleFunc("/api/notifications/test", s.requireAPIAuth(s.handleAPINotificationsTest))
	mux.HandleFunc("/api/notifications/events", s.requireAPIAuth(s.handleAPINotificationsEvents))

	// QoS
	mux.HandleFunc("/api/qos/status", s.requireAPIAuth(s.handleAPIQoSStatus))
	mux.HandleFunc("/api/qos/configure", s.requireAPIAuth(s.handleAPIQoSConfigure))

	// Service management
	mux.HandleFunc("/api/service/status", s.requireAPIAuth(s.handleAPIServiceStatus))
	mux.HandleFunc("/api/service/install", s.requireAPIAuth(s.handleAPIServiceInstall))
	mux.HandleFunc("/api/service/enable", s.requireAPIAuth(s.handleAPIServiceEnable))
	mux.HandleFunc("/api/service/start", s.requireAPIAuth(s.handleAPIServiceStart))
	mux.HandleFunc("/api/service/restart", s.requireAPIAuth(s.handleAPIServiceRestart))

	// External DNS
	mux.HandleFunc("/api/external-dns/status", s.requireAPIAuth(s.handleAPIExternalDNSStatus))
	mux.HandleFunc("/api/external-dns/settings", s.requireAPIAuth(s.handleAPIExternalDNSSettings))
	mux.HandleFunc("/api/external-dns/public-ip", s.requireAPIAuth(s.handleAPIExternalDNSPublicIP))
	mux.HandleFunc("/api/external-dns/zones", s.requireAPIAuth(s.handleAPIExternalDNSZones))
	mux.HandleFunc("/api/external-dns/zones/", s.requireAPIAuth(s.handleAPIExternalDNSZone))
	mux.HandleFunc("/api/external-dns/records", s.requireAPIAuth(s.handleAPIExternalDNSRecords))
	mux.HandleFunc("/api/external-dns/records/", s.requireAPIAuth(s.handleAPIExternalDNSRecord))
	mux.HandleFunc("/api/external-dns/sync", s.requireAPIAuth(s.handleAPIExternalDNSSync))
	mux.HandleFunc("/api/external-dns/discover-zones", s.requireAPIAuth(s.handleAPIExternalDNSDiscoverZones))
	mux.HandleFunc("/api/external-dns/ssl/request", s.requireAPIAuth(s.handleAPIExternalDNSSSLRequest))
	mux.HandleFunc("/api/external-dns/ssl/status", s.requireAPIAuth(s.handleAPIExternalDNSSSLStatus))

	// Services
	mux.HandleFunc("/api/services/status", s.requireAPIAuth(s.handleAPIServicesStatus))
	mux.HandleFunc("/api/services/settings", s.requireAPIAuth(s.handleAPIServicesSettings))
	mux.HandleFunc("/api/services/sync-all", s.requireAPIAuth(s.handleAPIServicesSyncAll))
	mux.HandleFunc("/api/services", s.requireAPIAuth(s.handleAPIServices))
	mux.HandleFunc("/api/services/", s.requireAPIAuth(s.handleAPIService))

	// HAProxy
	mux.HandleFunc("/api/haproxy/status", s.requireAPIAuth(s.handleAPIHAProxyStatus))
	mux.HandleFunc("/api/haproxy/reload", s.requireAPIAuth(s.handleAPIHAProxyReload))
	mux.HandleFunc("/api/haproxy/config", s.requireAPIAuth(s.handleAPIHAProxyConfig))
	mux.HandleFunc("/api/haproxy/test", s.requireAPIAuth(s.handleAPIHAProxyTest))
	mux.HandleFunc("/api/haproxy/backends", s.requireAPIAuth(s.handleAPIHAProxyBackends))

	// If web/dist exists, serve the SPA
	if s.webDistPath != "" {
		mux.HandleFunc("/", s.handleSPA)
	} else {
		// Legacy template-based routes
		mux.HandleFunc("/", s.handleLogin)
		mux.HandleFunc("/login", s.handleLoginPost)
		mux.HandleFunc("/logout", s.handleLogout)

		// Admin routes (require authentication)
		mux.HandleFunc("/admin", s.requireAuth(s.handleAdmin))
		mux.HandleFunc("/admin/setup", s.requireAuth(s.handleSetup))

		// Network
		mux.HandleFunc("/admin/interfaces", s.requireAuth(s.handleInterfaces))
		mux.HandleFunc("/admin/interfaces/configure", s.requireAuth(s.handleConfigureInterface))

		// DNS/DHCP
		mux.HandleFunc("/admin/dns", s.requireAuth(s.handleDNS))
		mux.HandleFunc("/admin/dns/add", s.requireAuth(s.handleDNSAdd))
		mux.HandleFunc("/admin/dns/remove", s.requireAuth(s.handleDNSRemove))
		mux.HandleFunc("/admin/dhcp", s.requireAuth(s.handleDHCP))
		mux.HandleFunc("/admin/dhcp/reservation/add", s.requireAuth(s.handleDHCPReservationAdd))
		mux.HandleFunc("/admin/dhcp/reservation/remove", s.requireAuth(s.handleDHCPReservationRemove))

		// WireGuard
		mux.HandleFunc("/admin/wireguard", s.requireAuth(s.handleWireGuard))
		mux.HandleFunc("/admin/wireguard/peer/add", s.requireAuth(s.handleWireGuardPeerAdd))
		mux.HandleFunc("/admin/wireguard/peer/remove", s.requireAuth(s.handleWireGuardPeerRemove))
		mux.HandleFunc("/admin/wireguard/toggle", s.requireAuth(s.handleWireGuardToggle))

		// WiFi
		mux.HandleFunc("/admin/wifi", s.requireAuth(s.handleWiFi))
		mux.HandleFunc("/admin/wifi/configure", s.requireAuth(s.handleWiFiConfigure))
		mux.HandleFunc("/admin/wifi/toggle", s.requireAuth(s.handleWiFiToggle))

		// Firewall
		mux.HandleFunc("/admin/firewall", s.requireAuth(s.handleFirewall))
		mux.HandleFunc("/admin/firewall/forward/add", s.requireAuth(s.handlePortForwardAdd))
		mux.HandleFunc("/admin/firewall/forward/remove", s.requireAuth(s.handlePortForwardRemove))

		// Settings
		mux.HandleFunc("/admin/settings", s.requireAuth(s.handleSettings))
		mux.HandleFunc("/admin/settings/save", s.requireAuth(s.handleSettingsSave))
	}

	return mux
}

// handleSPA serves the built Vite app with SPA fallback
func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	// Clean the path
	path := filepath.Clean(r.URL.Path)
	if path == "/" {
		path = "/index.html"
	}

	// Try to serve the static file
	filePath := filepath.Join(s.webDistPath, path)

	// Check if file exists
	info, err := os.Stat(filePath)
	if err != nil || info.IsDir() {
		// File doesn't exist or is a directory, serve index.html for SPA routing
		http.ServeFile(w, r, filepath.Join(s.webDistPath, "index.html"))
		return
	}

	// Serve the static file
	http.ServeFile(w, r, filePath)
}

// spaFileSystem wraps http.FileSystem to serve index.html for missing files
type spaFileSystem struct {
	fs   http.FileSystem
	root string
}

func (s spaFileSystem) Open(name string) (http.File, error) {
	f, err := s.fs.Open(name)
	if os.IsNotExist(err) {
		return s.fs.Open("/index.html")
	}
	return f, err
}

func (s spaFileSystem) Stat(name string) (fs.FileInfo, error) {
	return os.Stat(filepath.Join(s.root, name))
}

// Middleware

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.isAuthenticated(r) {
			http.Redirect(w, r, "/?next="+r.URL.Path, http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

// requireAPIAuth returns 401 for unauthenticated API requests
func (s *Server) requireAPIAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.isAuthenticated(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized","message":"Authentication required"}`))
			return
		}
		next(w, r)
	}
}

// Authentication - uses signed cookie, no expiration

func (s *Server) isAuthenticated(r *http.Request) bool {
	cookie, err := r.Cookie("session")
	if err != nil {
		return false
	}

	// Verify the signed cookie
	value, valid := s.verifyCookie(cookie.Value)
	return valid && value == "authenticated"
}

func (s *Server) signCookie(value string) string {
	h := hmac.New(sha256.New, []byte(s.adminPassword))
	h.Write([]byte(value))
	signature := hex.EncodeToString(h.Sum(nil))
	return value + "." + signature
}

func (s *Server) verifyCookie(signed string) (string, bool) {
	parts := strings.SplitN(signed, ".", 2)
	if len(parts) != 2 {
		return "", false
	}
	value, signature := parts[0], parts[1]

	h := hmac.New(sha256.New, []byte(s.adminPassword))
	h.Write([]byte(value))
	expectedSig := hex.EncodeToString(h.Sum(nil))

	if !hmac.Equal([]byte(signature), []byte(expectedSig)) {
		return "", false
	}

	return value, true
}

// Basic handlers

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.isAuthenticated(r) {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	data := map[string]interface{}{
		"Error": r.URL.Query().Get("error"),
		"Next":  r.URL.Query().Get("next"),
	}
	s.renderTemplate(w, "login", data)
}

func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	password := r.FormValue("password")
	next := r.FormValue("next")
	if next == "" {
		next = "/admin"
	}

	if password != s.adminPassword {
		http.Redirect(w, r, "/?error=invalid_password&next="+next, http.StatusSeeOther)
		return
	}

	// Set signed cookie - no expiration (persists until browser clears it or logout)
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    s.signCookie("authenticated"),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   315360000, // 10 years
	})

	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	// Clear the session cookie
	http.SetCookie(w, &http.Cookie{
		Name:   "session",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// API Auth handlers

func (s *Server) handleAPIAuthStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	authenticated := s.isAuthenticated(r)

	response := map[string]interface{}{
		"authenticated": authenticated,
	}

	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleAPIAuthLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Method not allowed",
		})
		return
	}

	var req struct {
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Invalid request body",
		})
		return
	}

	if req.Password != s.adminPassword {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Invalid password",
		})
		return
	}

	// Set signed cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    s.signCookie("authenticated"),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   315360000, // 10 years
	})

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

func (s *Server) handleAPIAuthLogout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Method not allowed",
		})
		return
	}

	// Clear the session cookie
	http.SetCookie(w, &http.Cookie{
		Name:   "session",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

// Template rendering

func (s *Server) renderTemplate(w http.ResponseWriter, name string, data interface{}) {
	// Wrap data with common fields
	wrapped := map[string]interface{}{
		"Version": s.version,
		"DryRun":  s.dryRun,
		"Data":    data,
	}

	err := s.templates.ExecuteTemplate(w, name, wrapped)
	if err != nil {
		log.Printf("Template error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (s *Server) saveConfig() error {
	return config.Save(s.configPath, s.config)
}

// Context helpers

func (s *Server) ctx(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 30*time.Second)
}

// migrateConfigToSQLite migrates DHCP reservations, DNS entries, and port forwards
// from the JSON config file to SQLite. This is a one-time migration that runs
// on startup if there is data in the config but not in SQLite.
func (s *Server) migrateConfigToSQLite(ctx context.Context, cfg *config.Config) {
	if s.statsStore == nil {
		return
	}

	// Migrate DHCP reservations
	if len(cfg.DHCPReservations) > 0 {
		existing, err := s.statsStore.GetDHCPReservations(ctx)
		if err == nil && len(existing) == 0 {
			log.Printf("Migrating %d DHCP reservations to SQLite", len(cfg.DHCPReservations))
			reservations := make([]stats.DHCPReservation, len(cfg.DHCPReservations))
			for i, r := range cfg.DHCPReservations {
				reservations[i] = stats.DHCPReservation{
					Name: r.Name,
					MAC:  r.MAC,
					IP:   r.IP,
				}
			}
			if err := s.statsStore.MigrateDHCPReservations(ctx, reservations); err != nil {
				log.Printf("Error migrating DHCP reservations: %v", err)
			}
		}
	}

	// Migrate DNS entries
	if len(cfg.DNSEntries) > 0 {
		existing, err := s.statsStore.GetDNSEntries(ctx)
		if err == nil && len(existing) == 0 {
			log.Printf("Migrating %d DNS entries to SQLite", len(cfg.DNSEntries))
			entries := make([]stats.DNSEntry, len(cfg.DNSEntries))
			for i, e := range cfg.DNSEntries {
				entries[i] = stats.DNSEntry{
					Hostname: e.Hostname,
					IP:       e.IP,
				}
			}
			if err := s.statsStore.MigrateDNSEntries(ctx, entries); err != nil {
				log.Printf("Error migrating DNS entries: %v", err)
			}
		}
	}

	// Migrate port forwards
	if len(cfg.PortForwards) > 0 {
		existing, err := s.statsStore.GetPortForwards(ctx)
		if err == nil && len(existing) == 0 {
			log.Printf("Migrating %d port forwards to SQLite", len(cfg.PortForwards))
			forwards := make([]stats.PortForward, len(cfg.PortForwards))
			for i, f := range cfg.PortForwards {
				forwards[i] = stats.PortForward{
					Name:         f.Name,
					Protocol:     f.Protocol,
					ExternalPort: f.ExternalPort,
					InternalIP:   f.InternalIP,
					InternalPort: f.InternalPort,
					Enabled:      f.Enabled,
				}
			}
			if err := s.statsStore.MigratePortForwards(ctx, forwards); err != nil {
				log.Printf("Error migrating port forwards: %v", err)
			}
		}
	}
}
