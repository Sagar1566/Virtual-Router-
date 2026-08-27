package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config represents the router configuration
type Config struct {
	// Server settings
	ListenAddr         string   `json:"listen_addr"`                    // Deprecated: use WebListenAddresses
	WebListenAddresses []string `json:"web_listen_addresses,omitempty"` // List of addresses to listen on (e.g., ["192.168.1.1:8080", "10.0.0.1:8080"])

	// WAN interfaces (internet uplinks) - ordered by priority
	WANs          []WANConfig `json:"wans"`
	FailbackDelay int         `json:"failback_delay,omitempty"` // Seconds to wait before failing back (default: 60)

	// Legacy WAN fields (deprecated - migrated to WANs on load)
	WANInterface     string `json:"wan_interface,omitempty"`
	WANMode          string `json:"wan_mode,omitempty"`
	WANStaticIP      string `json:"wan_static_ip,omitempty"`
	WANStaticGateway string `json:"wan_static_gateway,omitempty"`
	WANStaticDNS     string `json:"wan_static_dns,omitempty"`
	WANWiFiSSID      string `json:"wan_wifi_ssid,omitempty"`
	WANWiFiPassword  string `json:"wan_wifi_password,omitempty"`
	WANWiFiSecurity  string `json:"wan_wifi_security,omitempty"`

	// LAN settings
	LANBridge    string   `json:"lan_bridge"`    // br0
	LANAddresses []string `json:"lan_addresses"` // ["192.168.1.1/24"]
	LANPorts     []string `json:"lan_ports"`     // Physical ports to bridge

	// DHCP settings
	DHCPEnabled bool   `json:"dhcp_enabled"`
	DHCPStart   string `json:"dhcp_start"` // 192.168.1.100
	DHCPEnd     string `json:"dhcp_end"`   // 192.168.1.200
	DHCPLease   string `json:"dhcp_lease"` // 24h

	// DNS settings
	DNSEnabled    bool     `json:"dns_enabled"`
	DNSUpstream   []string `json:"dns_upstream"`   // ["8.8.8.8", "1.1.1.1"]
	DNSLocalZone  string   `json:"dns_local_zone"` // lan
	DNSHostsPath  string   `json:"dns_hosts_path"`
	DNSConfigPath string   `json:"dns_config_path"`

	// DHCP config path (separate from DNS for modularity)
	DHCPConfigPath string `json:"dhcp_config_path"`

	// Static DNS entries
	DNSEntries []DNSEntry `json:"dns_entries"`

	// Static DHCP leases
	DHCPReservations []DHCPReservation `json:"dhcp_reservations"`

	// WiFi settings
	WiFiInterfaces []WiFiInterface `json:"wifi_interfaces"`

	// WireGuard VPN (client-server)
	WireGuard *WireGuardConfig `json:"wireguard,omitempty"`

	// WireGuard P2P Tunnels (site-to-site)
	WireGuardP2P *WireGuardP2PConfig `json:"wireguard_p2p,omitempty"`

	// Firewall
	FirewallEnabled bool           `json:"firewall_enabled"`
	PortForwards    []PortForward  `json:"port_forwards"`
	FirewallRules   []FirewallRule `json:"firewall_rules"`

	// External DNS (libdns providers) and Let's Encrypt
	ExternalDNS *ExternalDNSConfig `json:"external_dns,omitempty"`

	// Services (HAProxy, split-horizon DNS, SSL management)
	Services *ServicesConfig `json:"services,omitempty"`

	// QoS (Quality of Service)
	QoS *QoSConfig `json:"qos,omitempty"`

	// Notifications (ntfy.sh)
	Notifications *NotificationsConfig `json:"notifications,omitempty"`
}

// QoSConfig represents Quality of Service settings
type QoSConfig struct {
	Enabled         bool             `json:"enabled"`
	Mode            string           `json:"mode"`                        // cake, htb
	DownloadSpeed   string           `json:"download_speed"`              // e.g., "100mbit"
	UploadSpeed     string           `json:"upload_speed"`                // e.g., "20mbit"
	PerHostFairness bool             `json:"per_host_fairness,omitempty"` // CAKE: fair sharing per LAN host
	RTTCompensation string           `json:"rtt_compensation,omitempty"`  // CAKE: regional, internet, oceanic, satellite
	WANType         string           `json:"wan_type,omitempty"`          // CAKE: ethernet, docsis, pppoe-ptm, etc.
	ClientLimits    []QoSClientLimit `json:"client_limits,omitempty"`
}

// QoSClientLimit defines bandwidth limits for a specific client
type QoSClientLimit struct {
	Name          string `json:"name"`
	IP            string `json:"ip"`
	MAC           string `json:"mac,omitempty"`
	DownloadSpeed string `json:"download_speed"`
	UploadSpeed   string `json:"upload_speed"`
}

// NotificationsConfig represents push notification settings (ntfy.sh)
type NotificationsConfig struct {
	Enabled  bool   `json:"enabled"`
	Server   string `json:"server"`   // e.g., "https://ntfy.sh" or self-hosted
	Topic    string `json:"topic"`    // e.g., "my-router-alerts"
	Token    string `json:"token"`    // Optional access token
	Username string `json:"username"` // Optional basic auth
	Password string `json:"password"` // Optional basic auth

	// Event filters
	NotifyWiFiClients  bool `json:"notify_wifi_clients"`
	NotifyHealthChecks bool `json:"notify_health_checks"`
	NotifyWANChanges   bool `json:"notify_wan_changes"`
	NotifyVPNPeers     bool `json:"notify_vpn_peers"`
	NotifySystemEvents bool `json:"notify_system_events"`
}

// DNSEntry represents a static DNS entry
type DNSEntry struct {
	Hostname string `json:"hostname"`
	IP       string `json:"ip"`
}

// DHCPReservation represents a static DHCP lease
type DHCPReservation struct {
	Name string `json:"name"`
	MAC  string `json:"mac"`
	IP   string `json:"ip"`
}

// WiFiInterface represents a WiFi access point configuration
type WiFiInterface struct {
	Interface   string `json:"interface"` // wlan0
	Enabled     bool   `json:"enabled"`
	SSID        string `json:"ssid"`
	Password    string `json:"password"`
	Channel     int    `json:"channel"`
	AutoChannel bool   `json:"auto_channel"` // Use ACS (Automatic Channel Selection)
	Band        string `json:"band"`         // 2.4ghz, 5ghz
	Mode        string `json:"mode"`         // ap, bridge
	Hidden      bool   `json:"hidden"`
	Bridge      string `json:"bridge"` // Bridge to join (if mode is bridge)
}

// WireGuardConfig represents WireGuard VPN settings
type WireGuardConfig struct {
	Enabled        bool            `json:"enabled"`
	Interface      string          `json:"interface"`        // wg0
	ListenPort     int             `json:"listen_port"`      // 51820
	PrivateKey     string          `json:"private_key"`
	Address        string          `json:"address"`          // 10.0.0.1/24
	DNS            string          `json:"dns"`              // DNS for clients
	ConfigPath     string          `json:"config_path"`
	RouteAllTraffic bool           `json:"route_all_traffic"` // Clients route all traffic through VPN
	RouteLAN       bool            `json:"route_lan"`         // Clients can access LAN
	LANSubnets     []string        `json:"lan_subnets"`       // LAN subnets to expose (auto-detected if empty)
	Peers          []WireGuardPeer `json:"peers"`
}

// WireGuardPeer represents a WireGuard peer/client
type WireGuardPeer struct {
	Name         string   `json:"name"`
	PublicKey    string   `json:"public_key"`
	AllowedIPs   []string `json:"allowed_ips"`
	Endpoint     string   `json:"endpoint,omitempty"`
	PresharedKey string   `json:"preshared_key,omitempty"`
}

// WireGuardP2PTunnel represents a point-to-point site-to-site tunnel
type WireGuardP2PTunnel struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Enabled             bool     `json:"enabled"`
	Interface           string   `json:"interface"`             // wg1, wg2, etc.
	ListenPort          int      `json:"listen_port"`           // 51821, 51822, etc.
	PrivateKey          string   `json:"private_key"`
	Address             string   `json:"address"`               // e.g., "10.100.0.1/30"
	RemotePublicKey     string   `json:"remote_public_key"`
	RemoteEndpoint      string   `json:"remote_endpoint"`       // e.g., "vpn.remote.com:51821"
	PresharedKey        string   `json:"preshared_key,omitempty"`
	LocalSubnets        []string `json:"local_subnets"`         // LANs to advertise
	RemoteSubnets       []string `json:"remote_subnets"`        // LANs to route (auto-filtered for conflicts)
	ForcedSubnets       []string `json:"forced_subnets"`        // Subnets to ALWAYS route through tunnel (bypass conflict filter)
	PersistentKeepalive int      `json:"persistent_keepalive"`
}

// WireGuardP2PConfig holds all P2P tunnel configurations
type WireGuardP2PConfig struct {
	Tunnels []WireGuardP2PTunnel `json:"tunnels"`
}

// PortForward represents a port forwarding rule
type PortForward struct {
	Name         string `json:"name"`
	Enabled      bool   `json:"enabled"`
	Protocol     string `json:"protocol"` // tcp, udp, both
	ExternalPort int    `json:"external_port"`
	InternalIP   string `json:"internal_ip"`
	InternalPort int    `json:"internal_port"`
}

// FirewallRule represents a custom firewall rule
type FirewallRule struct {
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	Chain    string `json:"chain"`    // INPUT, OUTPUT, FORWARD
	Action   string `json:"action"`   // ACCEPT, DROP, REJECT
	Protocol string `json:"protocol"` // tcp, udp, icmp, all
	Source   string `json:"source"`
	Dest     string `json:"dest"`
	Port     string `json:"port"`
}

// WANConfig represents a WAN interface configuration
type WANConfig struct {
	ID        string `json:"id"`                 // Unique ID for this WAN
	Name      string `json:"name"`               // User-friendly name (e.g., "Primary", "Backup")
	Interface string `json:"interface"`          // eth0, wlan0, etc.
	Enabled   bool   `json:"enabled"`
	Priority  int    `json:"priority"`           // Lower = higher priority (0 = primary)
	Mode      string `json:"mode"`               // dhcp, static, pppoe, wifi

	// Load balancing (WIP - not yet implemented)
	FuseWithNext bool `json:"fuse_with_next"` // Combine with next priority WAN for load balancing

	// Static mode settings
	StaticIP      string `json:"static_ip,omitempty"`
	StaticGateway string `json:"static_gateway,omitempty"`
	StaticDNS     string `json:"static_dns,omitempty"`

	// WiFi mode settings
	WiFiSSID     string `json:"wifi_ssid,omitempty"`
	WiFiPassword string `json:"wifi_password,omitempty"`
	WiFiSecurity string `json:"wifi_security,omitempty"`

	// Health check settings
	HealthCheckEnabled  bool     `json:"health_check_enabled"`
	HealthCheckTargets  []string `json:"health_check_targets,omitempty"`  // IPs to ping (default: gateway, 8.8.8.8)
	HealthCheckInterval int      `json:"health_check_interval,omitempty"` // Seconds between checks (default: 10)
	HealthCheckTimeout  int      `json:"health_check_timeout,omitempty"`  // Seconds to wait for response (default: 5)
	HealthCheckRetries  int      `json:"health_check_retries,omitempty"`  // Failures before marking down (default: 3)
}

// DNSProviderType identifies which external DNS provider to use
type DNSProviderType string

const (
	DNSProviderRoute53      DNSProviderType = "route53"
	DNSProviderCloudflare   DNSProviderType = "cloudflare"
	DNSProviderDigitalOcean DNSProviderType = "digitalocean"
	DNSProviderHetzner      DNSProviderType = "hetzner"
	DNSProviderGandi        DNSProviderType = "gandi"
	DNSProviderGoogleCloud  DNSProviderType = "googlecloud"
	DNSProviderDuckDNS      DNSProviderType = "duckdns"
	DNSProviderNamecom      DNSProviderType = "namecom"
)

// DNSProviderConfig holds credentials for an external DNS provider
type DNSProviderConfig struct {
	Type DNSProviderType `json:"type"`

	// Generic API token (used by most providers)
	APIToken string `json:"api_token,omitempty"`

	// AWS Route53 specific
	AWSAccessKeyID     string `json:"aws_access_key_id,omitempty"`
	AWSSecretAccessKey string `json:"aws_secret_access_key,omitempty"`
	AWSRegion          string `json:"aws_region,omitempty"`
	AWSHostedZoneID    string `json:"aws_hosted_zone_id,omitempty"`
	AWSProfile         string `json:"aws_profile,omitempty"` // Use AWS credentials file profile

	// Cloudflare specific
	CloudflareAPIToken string `json:"cloudflare_api_token,omitempty"`
	CloudflareZoneID   string `json:"cloudflare_zone_id,omitempty"`

	// Name.com specific
	NamecomUsername string `json:"namecom_username,omitempty"`
	NamecomAPIToken string `json:"namecom_api_token,omitempty"`

	// Google Cloud specific
	GCPProject            string `json:"gcp_project,omitempty"`
	GCPServiceAccountJSON string `json:"gcp_service_account_json,omitempty"`
}

// ExternalDNSRecord represents a DNS record to manage externally
type ExternalDNSRecord struct {
	ID       string `json:"id"`
	Name     string `json:"name"`      // Full hostname (e.g., "vpn.example.com")
	Type     string `json:"type"`      // A, AAAA, CNAME, TXT
	Value    string `json:"value"`     // IP or target (auto-detect WAN IP if empty for A records)
	TTL      int    `json:"ttl"`       // Seconds (default: 300)
	ZoneID   string `json:"zone_id"`   // Which zone this belongs to
	AutoSync bool   `json:"auto_sync"` // Auto-update when WAN IP changes
}

// ExternalDNSZone represents a DNS zone managed by an external provider
type ExternalDNSZone struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`     // Zone name (e.g., "example.com")
	Provider    DNSProviderConfig `json:"provider"` // Provider configuration
	SSLEnabled  bool              `json:"ssl_enabled"`
	SSLEmail    string            `json:"ssl_email,omitempty"`    // Let's Encrypt email
	SSLDomains  []string          `json:"ssl_domains,omitempty"`  // Additional domains for cert
	CertPath    string            `json:"cert_path,omitempty"`    // Path to certificate
	KeyPath     string            `json:"key_path,omitempty"`     // Path to private key
	LastSynced  string            `json:"last_synced,omitempty"`  // ISO timestamp
	LastSSLRenew string           `json:"last_ssl_renew,omitempty"`
}

// ExternalDNSConfig holds all external DNS configuration
type ExternalDNSConfig struct {
	Enabled      bool                `json:"enabled"`
	PublicIP     string              `json:"public_ip,omitempty"`      // Override auto-detected WAN IP
	AutoSyncIP   bool                `json:"auto_sync_ip"`             // Auto-update records when IP changes
	SyncInterval int                 `json:"sync_interval,omitempty"`  // Minutes between syncs (default: 5)
	Zones        []ExternalDNSZone   `json:"zones"`
	Records      []ExternalDNSRecord `json:"records"`
	CertDir      string              `json:"cert_dir,omitempty"` // Directory for SSL certs (default: /etc/ubuntu-router/certs)
}

// ServiceDNS configures DNS for a service (internal or external)
type ServiceDNS struct {
	IP       string `json:"ip"`
	TTL      int    `json:"ttl,omitempty"`       // TTL in seconds (default: 300)
	AutoSync bool   `json:"auto_sync,omitempty"` // Sync external DNS with public IP
}

// ServiceProxy configures HAProxy backend for a service
type ServiceProxy struct {
	Backend     string `json:"backend"`                // Backend URL, e.g., "http://192.168.1.50:8123"
	HealthCheck string `json:"health_check,omitempty"` // Health check path, e.g., "/api/health"
	Mode        string `json:"mode,omitempty"`         // http or tcp (default: http)
}

// ServiceSSL configures SSL domains for a service
type ServiceSSL struct {
	Enabled    bool     `json:"enabled"`
	Subdomains []string `json:"subdomains,omitempty"` // e.g., ["*", "*.api"] for multi-level wildcards
}

// Service represents a managed service with DNS and proxy configuration
type Service struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`                     // Display name, e.g., "Home Assistant"
	DNSName     string        `json:"dns_name"`                 // Subdomain, e.g., "homeassistant" (without zone)
	ZoneID      string        `json:"zone_id"`                  // Reference to ExternalDNSZone
	Enabled     bool          `json:"enabled"`
	InternalDNS *ServiceDNS   `json:"internal_dns,omitempty"`   // Internal DNS record (dnsmasq)
	ExternalDNS *ServiceDNS   `json:"external_dns,omitempty"`   // External DNS record (libdns)
	Proxy       *ServiceProxy `json:"proxy,omitempty"`          // HAProxy backend
	SSL         *ServiceSSL   `json:"ssl,omitempty"`            // SSL configuration
	CreatedAt   string        `json:"created_at,omitempty"`
	UpdatedAt   string        `json:"updated_at,omitempty"`
}

// ServicesConfig holds all service configurations
type ServicesConfig struct {
	Enabled  bool      `json:"enabled"`
	Services []Service `json:"services"`
}

// DefaultConfig returns a config with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		ListenAddr:      ":8080",
		LANBridge:       "br0",
		LANAddresses:    []string{"192.168.1.1/24"},
		DHCPEnabled:     true,
		DHCPStart:       "192.168.1.100",
		DHCPEnd:         "192.168.1.200",
		DHCPLease:       "24h",
		DNSEnabled:      true,
		DNSUpstream:     []string{"8.8.8.8", "1.1.1.1"},
		DNSLocalZone:    "lan",
		DNSHostsPath:    "/etc/hosts.ubuntu-router",
		DNSConfigPath:   "/etc/dnsmasq.d/ubuntu-router-dns.conf",
		DHCPConfigPath:  "/etc/dnsmasq.d/ubuntu-router-dhcp.conf",
		FirewallEnabled: true,
		WireGuard: &WireGuardConfig{
			Interface:  "wg0",
			ListenPort: 51820,
			Address:    "10.0.0.1/24",
			ConfigPath: "/etc/wireguard/wg0.conf",
		},
		WireGuardP2P: &WireGuardP2PConfig{
			Tunnels: []WireGuardP2PTunnel{},
		},
		Services: &ServicesConfig{
			Enabled:  false,
			Services: []Service{},
		},
	}
}

// searchPaths returns config file search paths in order
func searchPaths() []string {
	return []string{
		"./config.json",
		"./ubuntu-router.json",
		"/etc/ubuntu-router/config.json",
		"/etc/ubuntu-router.json",
	}
}

// LoadOrCreate loads config from file or creates default
func LoadOrCreate(path string) (*Config, string, error) {
	// If path specified, use it
	if path != "" {
		cfg, err := Load(path)
		if err != nil {
			if os.IsNotExist(err) {
				// Create default config
				cfg = DefaultConfig()
				if err := Save(path, cfg); err != nil {
					return nil, "", fmt.Errorf("failed to create config: %w", err)
				}
				return cfg, path, nil
			}
			return nil, "", err
		}
		return cfg, path, nil
	}

	// Search for existing config
	for _, p := range searchPaths() {
		if _, err := os.Stat(p); err == nil {
			cfg, err := Load(p)
			if err != nil {
				return nil, "", fmt.Errorf("failed to load %s: %w", p, err)
			}
			return cfg, p, nil
		}
	}

	// No config found, create default at /etc/ubuntu-router/config.json
	defaultPath := "/etc/ubuntu-router/config.json"
	if err := os.MkdirAll(filepath.Dir(defaultPath), 0755); err != nil {
		// Fall back to current directory
		defaultPath = "./config.json"
	}

	cfg := DefaultConfig()
	if err := Save(defaultPath, cfg); err != nil {
		return nil, "", fmt.Errorf("failed to create config at %s: %w", defaultPath, err)
	}

	return cfg, defaultPath, nil
}

// Load reads config from a JSON file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Strip JSONC comments
	data = stripComments(data)

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Migrate legacy WAN config to new WANs list
	cfg.migrateWANConfig()

	// Apply defaults to WiFi interfaces
	for i := range cfg.WiFiInterfaces {
		if cfg.WiFiInterfaces[i].SSID == "" {
			cfg.WiFiInterfaces[i].SSID = GenerateSSID()
		}
	}

	return cfg, nil
}

// migrateWANConfig converts legacy single-WAN config to the new WANs list format
func (cfg *Config) migrateWANConfig() {
	// If WANs already populated, nothing to migrate
	if len(cfg.WANs) > 0 {
		// Ensure all WANs have IDs
		for i := range cfg.WANs {
			if cfg.WANs[i].ID == "" {
				cfg.WANs[i].ID = GenerateWANID()
			}
		}
		return
	}

	// Migrate legacy single WAN config
	if cfg.WANInterface != "" {
		wan := WANConfig{
			ID:        GenerateWANID(),
			Name:      "Primary",
			Interface: cfg.WANInterface,
			Enabled:   true,
			Priority:  0,
			Mode:      cfg.WANMode,
			// Static settings
			StaticIP:      cfg.WANStaticIP,
			StaticGateway: cfg.WANStaticGateway,
			StaticDNS:     cfg.WANStaticDNS,
			// WiFi settings
			WiFiSSID:     cfg.WANWiFiSSID,
			WiFiPassword: cfg.WANWiFiPassword,
			WiFiSecurity: cfg.WANWiFiSecurity,
			// Default health check
			HealthCheckEnabled:  true,
			HealthCheckTargets:  []string{"8.8.8.8", "1.1.1.1"},
			HealthCheckInterval: 10,
			HealthCheckTimeout:  5,
			HealthCheckRetries:  3,
		}
		if wan.Mode == "" {
			wan.Mode = "dhcp"
		}
		cfg.WANs = []WANConfig{wan}

		// Clear legacy fields
		cfg.WANInterface = ""
		cfg.WANMode = ""
		cfg.WANStaticIP = ""
		cfg.WANStaticGateway = ""
		cfg.WANStaticDNS = ""
		cfg.WANWiFiSSID = ""
		cfg.WANWiFiPassword = ""
		cfg.WANWiFiSecurity = ""
	}
}

// GenerateWANID creates a unique ID for a WAN
func GenerateWANID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// GetPrimaryWAN returns the highest priority enabled WAN, or nil if none
func (cfg *Config) GetPrimaryWAN() *WANConfig {
	var best *WANConfig
	for i := range cfg.WANs {
		wan := &cfg.WANs[i]
		if wan.Enabled {
			if best == nil || wan.Priority < best.Priority {
				best = wan
			}
		}
	}
	return best
}

// GetPrimaryWANInterface returns the interface name of the primary WAN for backward compatibility
func (cfg *Config) GetPrimaryWANInterface() string {
	wan := cfg.GetPrimaryWAN()
	if wan != nil {
		return wan.Interface
	}
	return ""
}

// GetPrimaryWANMode returns the mode of the primary WAN for backward compatibility
func (cfg *Config) GetPrimaryWANMode() string {
	wan := cfg.GetPrimaryWAN()
	if wan != nil {
		return wan.Mode
	}
	return ""
}

// GetEnabledWANs returns all enabled WANs sorted by priority
func (cfg *Config) GetEnabledWANs() []WANConfig {
	var enabled []WANConfig
	for _, wan := range cfg.WANs {
		if wan.Enabled {
			enabled = append(enabled, wan)
		}
	}
	// Sort by priority
	for i := 0; i < len(enabled); i++ {
		for j := i + 1; j < len(enabled); j++ {
			if enabled[j].Priority < enabled[i].Priority {
				enabled[i], enabled[j] = enabled[j], enabled[i]
			}
		}
	}
	return enabled
}

// GetWebListenAddresses returns the effective list of web listen addresses.
// If WebListenAddresses is set, returns that list.
// Otherwise, if ListenAddr is set, returns a single-element list with that.
// If neither is set, returns nil (server should compute default from LAN addresses).
func (cfg *Config) GetWebListenAddresses() []string {
	if len(cfg.WebListenAddresses) > 0 {
		return cfg.WebListenAddresses
	}
	if cfg.ListenAddr != "" {
		return []string{cfg.ListenAddr}
	}
	return nil
}

// GetWebListenPort extracts the port from the first configured listen address,
// or returns ":8080" as default.
func (cfg *Config) GetWebListenPort() string {
	addrs := cfg.GetWebListenAddresses()
	if len(addrs) > 0 {
		// Extract port from first address
		addr := addrs[0]
		if idx := strings.LastIndex(addr, ":"); idx != -1 {
			return addr[idx:]
		}
	}
	return ":8080"
}

// Save writes config to a JSON file
func Save(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

// stripComments removes // and /* */ comments from JSON
func stripComments(data []byte) []byte {
	var result strings.Builder
	s := string(data)
	i := 0
	inString := false

	for i < len(s) {
		// Track string state
		if s[i] == '"' && (i == 0 || s[i-1] != '\\') {
			inString = !inString
			result.WriteByte(s[i])
			i++
			continue
		}

		// Skip comments outside strings
		if !inString {
			// Line comment
			if i+1 < len(s) && s[i:i+2] == "//" {
				for i < len(s) && s[i] != '\n' {
					i++
				}
				continue
			}
			// Block comment
			if i+1 < len(s) && s[i:i+2] == "/*" {
				i += 2
				for i+1 < len(s) && s[i:i+2] != "*/" {
					i++
				}
				i += 2
				continue
			}
		}

		result.WriteByte(s[i])
		i++
	}

	return []byte(result.String())
}

// GeneratePassword creates a random admin password (16 alphanumeric chars)
func GeneratePassword() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 16)
	rand.Read(b)
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return string(b)
}

// GenerateSSID creates an SSID based on the hostname like "UbuntuRouter-hostname"
func GenerateSSID() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		// Fallback to random suffix if hostname unavailable
		b := make([]byte, 2)
		rand.Read(b)
		return "UbuntuRouter-" + strings.ToUpper(hex.EncodeToString(b))
	}
	// Truncate hostname if too long (SSID max is 32 chars, prefix is 13)
	if len(hostname) > 19 {
		hostname = hostname[:19]
	}
	return "UbuntuRouter-" + hostname
}

// LoadPassword loads or generates admin password
func LoadPassword(configPath string) (string, bool, error) {
	passwordPath := configPath + ".password"

	// Try to read existing password
	data, err := os.ReadFile(passwordPath)
	if err == nil {
		return strings.TrimSpace(string(data)), false, nil
	}

	// Generate new password
	password := GeneratePassword()
	if err := os.WriteFile(passwordPath, []byte(password), 0600); err != nil {
		return "", false, fmt.Errorf("failed to save password: %w", err)
	}

	return password, true, nil
}

// ExampleConfig is printed when --config-template is used
const ExampleConfig = `{
  // Ubuntu Router Configuration

  // Server settings
  "listen_addr": ":8080",

  // WAN (internet) interface
  "wan_interface": "eth0",
  "wan_mode": "dhcp",  // dhcp, static, or pppoe

  // LAN settings
  "lan_bridge": "br0",
  "lan_addresses": ["192.168.1.1/24"],
  "lan_ports": ["eth1", "eth2"],

  // DHCP server
  "dhcp_enabled": true,
  "dhcp_start": "192.168.1.100",
  "dhcp_end": "192.168.1.200",
  "dhcp_lease": "24h",

  // DNS server
  "dns_enabled": true,
  "dns_upstream": ["8.8.8.8", "1.1.1.1"],
  "dns_local_zone": "lan",

  // WiFi access points
  "wifi_interfaces": [
    {
      "interface": "wlan0",
      "enabled": true,
      // "ssid": "MyNetwork",  // Auto-generated from hostname if omitted
      "password": "changeme",
      "channel": 6,
      "band": "2.4ghz",
      "mode": "bridge",
      "bridge": "br0"
    }
  ],

  // WireGuard VPN
  "wireguard": {
    "enabled": true,
    "interface": "wg0",
    "listen_port": 51820,
    "address": "10.0.0.1/24",
    "peers": []
  },

  // Firewall
  "firewall_enabled": true,
  "port_forwards": [],
  "firewall_rules": []
}
`
