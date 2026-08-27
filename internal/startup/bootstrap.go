package startup

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/iodesystems/ubuntu-router/internal/config"
	"github.com/iodesystems/ubuntu-router/internal/dnsmasq"
	"github.com/iodesystems/ubuntu-router/internal/haproxy"
	"github.com/iodesystems/ubuntu-router/internal/health"
	"github.com/iodesystems/ubuntu-router/internal/system"
	"github.com/iodesystems/ubuntu-router/internal/wifi"
	"github.com/iodesystems/ubuntu-router/internal/wireguard"
)

// Bootstrap applies essential router configuration on startup.
// This ensures the router works after reboot without manual intervention.
// Uses the health check system with auto-fix for self-healing.
type Bootstrap struct {
	runner       system.CommandRunner
	fs           system.FileSystem
	config       *config.Config
	services     *system.ServiceManager
	wireguardP2P *wireguard.P2PManager
	dns          *dnsmasq.Manager
	wifi         *wifi.Manager
	haproxy      *haproxy.Manager
	healthRunner *health.Runner
}

// New creates a new Bootstrap instance
func New(runner system.CommandRunner, fs system.FileSystem, cfg *config.Config, wireguardP2P *wireguard.P2PManager) *Bootstrap {
	// Create health runner
	healthRunner := health.NewRunner(runner, fs)

	// Build WireGuard interface configs for health checks
	var wgInterfaces []health.WireGuardInterfaceConfig
	if cfg.WireGuard != nil && cfg.WireGuard.Enabled {
		iface := cfg.WireGuard.Interface
		if iface == "" {
			iface = "wg0"
		}
		configPath := cfg.WireGuard.ConfigPath
		if configPath == "" {
			configPath = fmt.Sprintf("/etc/wireguard/%s.conf", iface)
		}
		wgInterfaces = append(wgInterfaces, health.WireGuardInterfaceConfig{
			Name:       iface,
			ConfigPath: configPath,
			Enabled:    true,
		})
	}
	if cfg.WireGuardP2P != nil {
		for _, t := range cfg.WireGuardP2P.Tunnels {
			if t.Enabled && t.Interface != "" {
				wgInterfaces = append(wgInterfaces, health.WireGuardInterfaceConfig{
					Name:       t.Interface,
					ConfigPath: fmt.Sprintf("/etc/wireguard/%s.conf", t.Interface),
					Enabled:    true,
				})
			}
		}
	}

	// Build WiFi AP interface list
	var wifiAPInterfaces []string
	for _, wifiCfg := range cfg.WiFiInterfaces {
		if wifiCfg.Enabled {
			wifiAPInterfaces = append(wifiAPInterfaces, wifiCfg.Interface)
		}
	}

	// Register health checks with router config
	health.RegisterAllChecksWithConfig(healthRunner, runner, fs, &health.RouterConfig{
		LANBridge:           cfg.LANBridge,
		LANAddresses:        cfg.LANAddresses,
		WANInterface:        cfg.WANInterface,
		WANMode:             cfg.WANMode,
		DHCPStart:           cfg.DHCPStart,
		DHCPEnd:             cfg.DHCPEnd,
		WireGuardInterfaces: wgInterfaces,
		WiFiAPInterfaces:    wifiAPInterfaces,
	})

	return &Bootstrap{
		runner:       runner,
		fs:           fs,
		config:       cfg,
		services:     system.NewServiceManager(runner),
		wireguardP2P: wireguardP2P,
		dns:          dnsmasq.New(fs, runner, cfg.DNSConfigPath, cfg.DHCPConfigPath, cfg.DNSHostsPath),
		wifi:         wifi.New(fs, runner),
		haproxy:      haproxy.New(fs, runner, "/etc/ubuntu-router/certs"),
		healthRunner: healthRunner,
	}
}

// GetHealthRunner returns the health check runner for external access (API, UI)
func (b *Bootstrap) GetHealthRunner() *health.Runner {
	return b.healthRunner
}

// Run applies all essential startup configuration using the health check system
// Health checks run asynchronously with auto-fix enabled for self-healing
func (b *Bootstrap) Run(ctx context.Context) error {
	log.Println("Running startup bootstrap with health checks...")

	// Start health checks with auto-fix asynchronously
	// This allows the HTTP server to start immediately while fixes are applied
	err := b.healthRunner.StartAsync(ctx, true, func(status *health.Status, fixResult *health.AutoFixResult) {
		if status != nil {
			switch status.State {
			case health.StateOK:
				log.Printf("  ✓ %s: %s", status.Name, status.Message)
			case health.StateWarning:
				log.Printf("  ⚠ %s: %s", status.Name, status.Message)
			case health.StateError:
				log.Printf("  ✗ %s: %s", status.Name, status.Message)
			case health.StateSkipped:
				log.Printf("  ⊘ %s: %s", status.Name, status.Message)
			}
		}
		if fixResult != nil {
			if fixResult.Success {
				log.Printf("    → Fixed: %s", fixResult.Description)
			} else {
				log.Printf("    → Fix failed: %s (%s)", fixResult.Description, fixResult.Error)
			}
		}
	})
	if err != nil {
		log.Printf("Warning: failed to start health checks: %v", err)
	}

	// Start async background tasks that aren't covered by health checks
	// These run independently and don't block startup
	// All goroutines have panic recovery to ensure service stability

	// LAN bridge configuration (needed before WiFi AP can work properly)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("LAN bridge panic recovered: %v", r)
			}
		}()
		if err := b.configureLANBridge(ctx); err != nil {
			log.Printf("LAN bridge warning: %v", err)
		}
	}()

	// WiFi WAN connection (can take 30+ seconds for WPA3)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("WiFi WAN panic recovered: %v", r)
			}
		}()
		if err := b.configureWiFiWAN(ctx); err != nil {
			log.Printf("WiFi WAN warning: %v", err)
		}
	}()

	// WireGuard interfaces (may block on DNS resolution)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("WireGuard interfaces panic recovered: %v", r)
			}
		}()
		if err := b.startWireGuardInterfaces(ctx); err != nil {
			log.Printf("WireGuard interfaces warning: %v", err)
		}
	}()

	// WiFi access points
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("WiFi AP panic recovered: %v", r)
			}
		}()
		if err := b.configureWiFi(ctx); err != nil {
			log.Printf("WiFi AP warning: %v", err)
		}
	}()

	// HAProxy for services
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("HAProxy panic recovered: %v", r)
			}
		}()
		if err := b.configureHAProxy(ctx); err != nil {
			log.Printf("HAProxy warning: %v", err)
		}
	}()

	// Note: WAN health monitoring is handled by server.Run() using the shared multiwan.Manager
	// This avoids duplicate multi-WAN instances racing with each other

	log.Println("Startup bootstrap initiated (health checks running in background)")
	return nil
}

// enableIPForwarding enables IPv4 and IPv6 forwarding both immediately and persistently
func (b *Bootstrap) enableIPForwarding(ctx context.Context) error {
	log.Println("  Enabling IP forwarding...")

	// Write persistent sysctl config
	sysctlConfig := `# Ubuntu Router - IP forwarding
# Auto-generated - do not edit manually
net.ipv4.ip_forward=1
net.ipv6.conf.all.forwarding=1
`
	if err := b.fs.WriteFile("/etc/sysctl.d/99-ubuntu-router.conf", []byte(sysctlConfig), 0644); err != nil {
		log.Printf("  Warning: failed to write persistent sysctl config: %v", err)
		// Continue - we can still apply immediate settings
	}

	// Apply immediately via sysctl
	if _, err := b.runner.Run(ctx, "sysctl", "-w", "net.ipv4.ip_forward=1"); err != nil {
		return fmt.Errorf("failed to enable IPv4 forwarding: %w", err)
	}
	if _, err := b.runner.Run(ctx, "sysctl", "-w", "net.ipv6.conf.all.forwarding=1"); err != nil {
		log.Printf("  Warning: failed to enable IPv6 forwarding: %v", err)
		// Not critical - continue
	}

	log.Println("  IP forwarding enabled")
	return nil
}

// loadWireGuardModule loads the WireGuard kernel module and makes it persistent
func (b *Bootstrap) loadWireGuardModule(ctx context.Context) error {
	log.Println("  Loading WireGuard kernel module...")

	// Check if module is already loaded
	out, _ := b.runner.Run(ctx, "lsmod")
	if strings.Contains(string(out), "wireguard") {
		log.Println("  WireGuard module already loaded")
		return nil
	}

	// Check if module is available
	if _, err := b.runner.Run(ctx, "modinfo", "wireguard"); err != nil {
		log.Println("  WireGuard module not available (built into kernel on 5.6+)")
		return nil
	}

	// Write persistent module loading config
	if err := b.fs.WriteFile("/etc/modules-load.d/wireguard.conf", []byte("wireguard\n"), 0644); err != nil {
		log.Printf("  Warning: failed to write persistent module config: %v", err)
	}

	// Load module immediately
	if _, err := b.runner.Run(ctx, "modprobe", "wireguard"); err != nil {
		return fmt.Errorf("failed to load wireguard module: %w", err)
	}

	log.Println("  WireGuard module loaded")
	return nil
}

// configureWiFiWAN connects to a WiFi network for WAN uplink when wan_mode is "wifi"
func (b *Bootstrap) configureWiFiWAN(ctx context.Context) error {
	if b.config.WANMode != "wifi" {
		log.Println("  Skipping WiFi WAN (wan_mode is not wifi)")
		return nil
	}

	if b.config.WANInterface == "" || b.config.WANWiFiSSID == "" {
		log.Println("  Skipping WiFi WAN (no interface or SSID configured)")
		return nil
	}

	iface := b.config.WANInterface
	ssid := b.config.WANWiFiSSID
	password := b.config.WANWiFiPassword
	security := b.config.WANWiFiSecurity
	if security == "" {
		security = "wpa2"
	}

	log.Printf("  Configuring WiFi WAN on %s (SSID: %s)...", iface, ssid)

	// Generate wpa_supplicant config
	configPath := fmt.Sprintf("/etc/wpa_supplicant/wpa_supplicant-%s.conf", iface)

	if err := b.fs.MkdirAll("/etc/wpa_supplicant", 0755); err != nil {
		return fmt.Errorf("failed to create wpa_supplicant directory: %w", err)
	}

	config := b.generateWpaSupplicantConfig(ssid, password, security)
	if err := b.fs.WriteFile(configPath, []byte(config), 0600); err != nil {
		return fmt.Errorf("failed to write wpa_supplicant config: %w", err)
	}

	// Write systemd service for wpa_supplicant
	servicePath := fmt.Sprintf("/etc/systemd/system/wpa_supplicant-%s.service", iface)
	service := fmt.Sprintf(`[Unit]
Description=WPA supplicant for %s (WiFi WAN)
Before=network.target
Wants=network.target
After=dbus.service

[Service]
Type=simple
ExecStart=/sbin/wpa_supplicant -c %s -i %s
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
`, iface, configPath, iface)

	if err := b.fs.WriteFile(servicePath, []byte(service), 0644); err != nil {
		return fmt.Errorf("failed to write wpa_supplicant service: %w", err)
	}

	// Reload systemd
	b.runner.Run(ctx, "systemctl", "daemon-reload")

	// Enable and start wpa_supplicant
	svcName := "wpa_supplicant-" + iface
	if err := b.services.Enable(ctx, svcName); err != nil {
		log.Printf("  Warning: failed to enable wpa_supplicant: %v", err)
	}
	if err := b.services.Restart(ctx, svcName); err != nil {
		return fmt.Errorf("failed to start wpa_supplicant: %w", err)
	}

	// Give wpa_supplicant time to create the control socket
	time.Sleep(2 * time.Second)

	// Wait for WiFi connection (up to 45 seconds for WPA3/SAE which can be slow)
	log.Printf("  Waiting for WiFi connection to %s...", ssid)
	connected := false
	for i := 0; i < 45; i++ {
		time.Sleep(1 * time.Second)
		// Use timeout command to prevent wpa_cli from hanging if socket isn't ready
		out, err := b.runner.Run(ctx, "timeout", "3", "wpa_cli", "-i", iface, "status")
		if err == nil && strings.Contains(string(out), "wpa_state=COMPLETED") {
			connected = true
			log.Printf("  WiFi connected after %d seconds", i+1)
			break
		}
	}

	if !connected {
		return fmt.Errorf("failed to connect to WiFi network %s", ssid)
	}
	log.Printf("  WiFi connected to %s", ssid)

	// Request DHCP on the WiFi WAN interface
	log.Printf("  Requesting DHCP on %s...", iface)

	// Check which DHCP client is available and use it
	var dhcpErr error
	if _, err := b.runner.Run(ctx, "which", "dhclient"); err == nil {
		_, dhcpErr = b.runner.Run(ctx, "dhclient", "-v", iface)
	} else if _, err := b.runner.Run(ctx, "which", "dhcpcd"); err == nil {
		_, dhcpErr = b.runner.Run(ctx, "dhcpcd", "-4", iface)
	} else {
		return fmt.Errorf("no DHCP client found (dhclient or dhcpcd)")
	}
	if dhcpErr != nil {
		return fmt.Errorf("DHCP request failed on WiFi WAN: %w", dhcpErr)
	}

	// Verify we got an IP
	out, _ := b.runner.Run(ctx, "ip", "-4", "addr", "show", iface)
	if !strings.Contains(string(out), "inet ") {
		return fmt.Errorf("WiFi WAN did not get an IP address")
	}

	log.Printf("  WiFi WAN configured on %s", iface)
	return nil
}

// generateWpaSupplicantConfig creates wpa_supplicant configuration for WiFi WAN
func (b *Bootstrap) generateWpaSupplicantConfig(ssid, password, security string) string {
	var sb strings.Builder

	sb.WriteString("# Ubuntu Router wpa_supplicant configuration\n")
	sb.WriteString("# Auto-generated - do not edit\n\n")
	sb.WriteString("ctrl_interface=/var/run/wpa_supplicant\n")
	sb.WriteString("ctrl_interface_group=0\n")
	sb.WriteString("update_config=1\n\n")

	sb.WriteString("network={\n")
	sb.WriteString(fmt.Sprintf("\tssid=\"%s\"\n", ssid))

	switch security {
	case "open":
		sb.WriteString("\tkey_mgmt=NONE\n")
	case "wep":
		sb.WriteString("\tkey_mgmt=NONE\n")
		sb.WriteString(fmt.Sprintf("\twep_key0=\"%s\"\n", password))
		sb.WriteString("\twep_tx_keyidx=0\n")
	case "wpa3":
		sb.WriteString(fmt.Sprintf("\tpsk=\"%s\"\n", password))
		sb.WriteString("\tkey_mgmt=SAE\n")
		sb.WriteString("\tieee80211w=2\n")
	default: // wpa, wpa2
		sb.WriteString(fmt.Sprintf("\tpsk=\"%s\"\n", password))
		sb.WriteString("\tkey_mgmt=WPA-PSK\n")
	}

	sb.WriteString("\tscan_ssid=1\n")
	sb.WriteString("}\n")

	return sb.String()
}

// configureLANBridge creates (if needed), brings up the LAN bridge and adds configured IP addresses
func (b *Bootstrap) configureLANBridge(ctx context.Context) error {
	if b.config.LANBridge == "" {
		log.Println("  Skipping LAN bridge (no bridge configured)")
		return nil
	}

	bridgeName := b.config.LANBridge
	log.Printf("  Configuring LAN bridge %s...", bridgeName)

	// Check if bridge exists, create if not
	if _, err := b.runner.Run(ctx, "ip", "link", "show", bridgeName); err != nil {
		log.Printf("  Bridge %s does not exist, creating...", bridgeName)
		if _, err := b.runner.Run(ctx, "ip", "link", "add", bridgeName, "type", "bridge"); err != nil {
			return fmt.Errorf("failed to create bridge %s: %w", bridgeName, err)
		}
		log.Printf("  Bridge %s created", bridgeName)
	}

	// Add LAN ports to the bridge if configured
	for _, port := range b.config.LANPorts {
		// Check if port is already in bridge
		out, _ := b.runner.Run(ctx, "ip", "link", "show", port)
		if strings.Contains(string(out), "master "+bridgeName) {
			continue
		}
		// Add port to bridge
		if _, err := b.runner.Run(ctx, "ip", "link", "set", port, "master", bridgeName); err != nil {
			log.Printf("  Warning: failed to add %s to bridge: %v", port, err)
		} else {
			log.Printf("  Added %s to bridge %s", port, bridgeName)
		}
	}

	// Bring up the bridge
	if _, err := b.runner.Run(ctx, "ip", "link", "set", bridgeName, "up"); err != nil {
		log.Printf("  Warning: failed to bring up bridge: %v", err)
	}

	// Add configured IP addresses
	for _, addr := range b.config.LANAddresses {
		// Check if address already exists
		out, _ := b.runner.Run(ctx, "ip", "-4", "addr", "show", bridgeName)
		if strings.Contains(string(out), strings.Split(addr, "/")[0]) {
			log.Printf("  Address %s already configured on %s", addr, bridgeName)
			continue
		}

		// Add address
		if _, err := b.runner.Run(ctx, "ip", "addr", "add", addr, "dev", bridgeName); err != nil {
			log.Printf("  Warning: failed to add address %s: %v", addr, err)
		} else {
			log.Printf("  Added address %s to %s", addr, bridgeName)
		}
	}

	log.Printf("  LAN bridge %s configured", bridgeName)
	return nil
}

// configureDNSMasq writes dnsmasq config files and restarts the service.
// This ensures DHCP and DNS are available to LAN clients immediately on boot,
// without requiring WAN connectivity or manual intervention.
func (b *Bootstrap) configureDNSMasq(ctx context.Context) error {
	if !b.config.DNSEnabled && !b.config.DHCPEnabled {
		log.Println("  Skipping dnsmasq (DNS and DHCP both disabled)")
		return nil
	}

	log.Println("  Configuring dnsmasq (DNS/DHCP)...")

	// Write DNS and DHCP config files
	if err := b.dns.WriteConfig(b.config); err != nil {
		return fmt.Errorf("failed to write dnsmasq config: %w", err)
	}
	log.Println("  dnsmasq config files written")

	// Write custom hosts file for DNS entries
	if err := b.dns.WriteHosts(b.config.DNSEntries); err != nil {
		log.Printf("  Warning: failed to write DNS hosts file: %v", err)
		// Not fatal - continue
	}

	// Enable dnsmasq to start on boot
	if err := b.dns.Enable(ctx); err != nil {
		log.Printf("  Warning: failed to enable dnsmasq on boot: %v", err)
	}

	// Restart dnsmasq to apply the config
	if err := b.dns.Restart(ctx); err != nil {
		return fmt.Errorf("failed to restart dnsmasq: %w", err)
	}

	log.Println("  dnsmasq configured and restarted")
	return nil
}

// configureWiFi writes hostapd config files and starts the WiFi access points.
// This ensures WiFi APs are available to clients immediately on boot.
func (b *Bootstrap) configureWiFi(ctx context.Context) error {
	if len(b.config.WiFiInterfaces) == 0 {
		log.Println("  Skipping WiFi (no interfaces configured)")
		return nil
	}

	log.Println("  Configuring WiFi access points...")

	for i := range b.config.WiFiInterfaces {
		wifiCfg := &b.config.WiFiInterfaces[i]
		if !wifiCfg.Enabled {
			log.Printf("  WiFi %s: disabled, skipping", wifiCfg.Interface)
			continue
		}

		log.Printf("  WiFi %s: configuring hostapd (SSID: %s)...", wifiCfg.Interface, wifiCfg.SSID)

		// ConfigureInterface writes config, creates systemd service, enables and starts
		if err := b.wifi.ConfigureInterface(ctx, wifiCfg); err != nil {
			log.Printf("  Warning: failed to configure WiFi %s: %v", wifiCfg.Interface, err)
			// Continue with other interfaces
			continue
		}

		// hostapd is supposed to add the interface to the bridge when configured with bridge=X,
		// but this doesn't always work reliably. Explicitly add the interface to the bridge
		// after hostapd starts to ensure WiFi clients can communicate with the LAN.
		if wifiCfg.Mode == "bridge" && wifiCfg.Bridge != "" {
			log.Printf("  WiFi %s: ensuring interface is in bridge %s...", wifiCfg.Interface, wifiCfg.Bridge)
			// Give hostapd a moment to initialize
			time.Sleep(500 * time.Millisecond)
			// Add interface to bridge explicitly
			if _, err := b.runner.Run(ctx, "ip", "link", "set", wifiCfg.Interface, "master", wifiCfg.Bridge); err != nil {
				log.Printf("  Warning: failed to add %s to bridge %s: %v", wifiCfg.Interface, wifiCfg.Bridge, err)
			} else {
				log.Printf("  WiFi %s: added to bridge %s", wifiCfg.Interface, wifiCfg.Bridge)
			}
		}

		log.Printf("  WiFi %s: hostapd configured and started", wifiCfg.Interface)
	}

	log.Println("  WiFi access points configured")
	return nil
}

// applyNATRules applies NAT masquerade rules for the WAN interface
// It detects an available WAN if the configured one isn't working
func (b *Bootstrap) applyNATRules(ctx context.Context) error {
	// Detect the best available WAN interface
	wanInterface := b.detectActiveWAN(ctx)
	if wanInterface == "" {
		log.Println("  Skipping NAT rules (no WAN interface available)")
		return nil
	}

	log.Printf("  Applying NAT rules for WAN interface %s...", wanInterface)

	// Check if rule already exists
	out, _ := b.runner.Run(ctx, "iptables", "-t", "nat", "-L", "POSTROUTING", "-n", "-v")
	if strings.Contains(string(out), wanInterface) && strings.Contains(string(out), "MASQUERADE") {
		log.Println("  NAT masquerade rule already exists")
		return nil
	}

	// Add masquerade rule
	if _, err := b.runner.Run(ctx, "iptables", "-t", "nat", "-A", "POSTROUTING", "-o", wanInterface, "-j", "MASQUERADE"); err != nil {
		return fmt.Errorf("failed to add NAT masquerade rule: %w", err)
	}

	log.Printf("  NAT masquerade configured for %s", wanInterface)
	return nil
}

// detectActiveWAN finds the best available WAN interface
// Priority: configured WAN if up, then any interface with internet connectivity
func (b *Bootstrap) detectActiveWAN(ctx context.Context) string {
	// First, check if the configured WAN is up and has an IP
	if b.config.WANInterface != "" {
		if b.isInterfaceUp(ctx, b.config.WANInterface) {
			log.Printf("  Configured WAN %s is up", b.config.WANInterface)
			return b.config.WANInterface
		}
		log.Printf("  Configured WAN %s is not available, looking for alternatives...", b.config.WANInterface)
	}

	// Check configured WAN interfaces
	for _, wan := range b.config.WANs {
		if wan.Enabled && b.isInterfaceUp(ctx, wan.Interface) {
			log.Printf("  WAN interface %s is up", wan.Interface)
			return wan.Interface
		}
	}

	// Fallback: scan for any interface with an IP and default route
	// Skip bridge, loopback, wireguard, and known LAN interfaces
	lanBridge := b.config.LANBridge
	out, err := b.runner.Run(ctx, "ip", "-4", "route", "show", "default")
	if err == nil && len(out) > 0 {
		// Parse default route to find WAN interface
		// Format: default via X.X.X.X dev <interface>
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "default via") {
				parts := strings.Fields(line)
				for i, part := range parts {
					if part == "dev" && i+1 < len(parts) {
						iface := parts[i+1]
						// Skip LAN bridge and loopback
						if iface != lanBridge && iface != "lo" && !strings.HasPrefix(iface, "wg") {
							log.Printf("  Fallback WAN detected: %s", iface)
							return iface
						}
					}
				}
			}
		}
	}

	// Last resort: look for any ethernet interface with an IP
	out, _ = b.runner.Run(ctx, "ip", "-4", "addr", "show")
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, "inet ") && !strings.Contains(line, "127.0.0.1") {
			// Find interface from previous line or context
			// This is a simplified fallback
		}
	}

	return ""
}

// isInterfaceUp checks if an interface is up and has an IP address
func (b *Bootstrap) isInterfaceUp(ctx context.Context, iface string) bool {
	// Check link state
	out, err := b.runner.Run(ctx, "ip", "link", "show", iface)
	if err != nil {
		return false
	}
	if !strings.Contains(string(out), "state UP") {
		return false
	}

	// Check for IP address
	out, err = b.runner.Run(ctx, "ip", "-4", "addr", "show", iface)
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "inet ")
}

// startWireGuardInterfaces enables auto-start for configured WireGuard interfaces via systemd.
// We DON'T start the interfaces directly here to avoid racing with systemd's wg-quick@ services.
// If the systemd service is enabled, let it handle starting the interface on boot.
// If it's not running after boot, the health check will offer a fixer.
func (b *Bootstrap) startWireGuardInterfaces(ctx context.Context) error {
	// Main WireGuard interface (wg0)
	if b.config.WireGuard != nil && b.config.WireGuard.Enabled {
		iface := b.config.WireGuard.Interface
		if iface == "" {
			iface = "wg0"
		}

		configPath := b.config.WireGuard.ConfigPath
		if configPath == "" {
			configPath = fmt.Sprintf("/etc/wireguard/%s.conf", iface)
		}

		// Only proceed if config exists
		if _, err := b.fs.Stat(configPath); err == nil {
			// Enable auto-start via systemd - this is the primary mechanism
			// The wg-quick@.service will start the interface on boot
			if b.enableWireGuardAutoStart(ctx, iface) {
				log.Printf("  WireGuard %s: systemd auto-start enabled", iface)
			}

			// Check if the interface actually exists (not just if service is "active")
			// wg-quick@.service is Type=oneshot, so "active (exited)" doesn't mean interface exists
			serviceName := fmt.Sprintf("wg-quick@%s", iface)
			if _, err := b.runner.Run(ctx, "ip", "link", "show", iface); err != nil {
				// Interface doesn't exist - restart the service
				log.Printf("  WireGuard %s interface not found, restarting service...", iface)
				if err := b.services.Restart(ctx, serviceName); err != nil {
					log.Printf("  Warning: failed to restart %s: %v", serviceName, err)
				} else {
					log.Printf("  WireGuard %s started", iface)
				}
			} else {
				log.Printf("  WireGuard %s already running", iface)
			}
		}
	}

	// P2P/Site-to-Site tunnels
	if b.config.WireGuardP2P != nil && b.wireguardP2P != nil {
		for i := range b.config.WireGuardP2P.Tunnels {
			tunnel := &b.config.WireGuardP2P.Tunnels[i]
			if tunnel.Enabled && tunnel.Interface != "" {
				configPath := fmt.Sprintf("/etc/wireguard/%s.conf", tunnel.Interface)

				// Write config file if it doesn't exist
				if _, err := b.fs.Stat(configPath); err != nil {
					log.Printf("  WireGuard P2P %s: config file missing, generating...", tunnel.Interface)
					if err := b.wireguardP2P.WriteConfig(tunnel); err != nil {
						log.Printf("  Warning: failed to write config for %s: %v", tunnel.Interface, err)
						continue
					}
					log.Printf("  WireGuard P2P %s: config file created", tunnel.Interface)
				}

				if b.enableWireGuardAutoStart(ctx, tunnel.Interface) {
					log.Printf("  WireGuard P2P %s: systemd auto-start enabled", tunnel.Interface)
				}

				// Check if the interface actually exists
				serviceName := fmt.Sprintf("wg-quick@%s", tunnel.Interface)
				if _, err := b.runner.Run(ctx, "ip", "link", "show", tunnel.Interface); err != nil {
					log.Printf("  WireGuard P2P %s interface not found, restarting service...", tunnel.Interface)
					if err := b.services.Restart(ctx, serviceName); err != nil {
						log.Printf("  Warning: failed to restart %s: %v", serviceName, err)
					} else {
						log.Printf("  WireGuard P2P %s started", tunnel.Interface)
					}
				} else {
					log.Printf("  WireGuard P2P %s already running", tunnel.Interface)
				}
			}
		}
	}

	return nil
}

// enableWireGuardAutoStart enables systemd auto-start for a WireGuard interface
// Returns true if it was newly enabled, false if already enabled
func (b *Bootstrap) enableWireGuardAutoStart(ctx context.Context, iface string) bool {
	serviceName := fmt.Sprintf("wg-quick@%s", iface)

	// Check if already enabled
	if b.services.IsEnabled(ctx, serviceName) {
		return false
	}

	// Enable the service
	if err := b.services.Enable(ctx, serviceName); err != nil {
		log.Printf("  Warning: failed to enable auto-start for %s: %v", iface, err)
		return false
	}
	return true
}

// configureHAProxy writes HAProxy config and starts the service.
// This ensures the admin UI is accessible on port 80 via the gateway IP.
func (b *Bootstrap) configureHAProxy(ctx context.Context) error {
	// Check if HAProxy is installed
	if !haproxy.Available(b.runner) {
		log.Println("  Skipping HAProxy (not installed)")
		return nil
	}

	log.Println("  Configuring HAProxy...")

	// Get services and zones from config
	var services []config.Service
	var zones []config.ExternalDNSZone
	if b.config.Services != nil {
		services = b.config.Services.Services
	}
	if b.config.ExternalDNS != nil {
		zones = b.config.ExternalDNS.Zones
	}

	// Write HAProxy config (includes admin_ui backend for gateway IP)
	if err := b.haproxy.WriteConfig(services, zones, b.config.LANAddresses); err != nil {
		return fmt.Errorf("failed to write HAProxy config: %w", err)
	}

	// Enable HAProxy on boot
	if err := b.haproxy.Enable(ctx); err != nil {
		log.Printf("  Warning: failed to enable HAProxy on boot: %v", err)
	}

	// Restart HAProxy to apply config
	if err := b.haproxy.Restart(ctx); err != nil {
		return fmt.Errorf("failed to restart HAProxy: %w", err)
	}

	log.Println("  HAProxy configured and restarted")
	return nil
}
