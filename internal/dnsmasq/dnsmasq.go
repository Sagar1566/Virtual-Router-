package dnsmasq

import (
	"bufio"
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/iodesystems/ubuntu-router/internal/config"
	"github.com/iodesystems/ubuntu-router/internal/system"
)

// Lease represents a DHCP lease
type Lease struct {
	Expires  time.Time `json:"expires"`
	MAC      string    `json:"mac"`
	IP       string    `json:"ip"`
	Hostname string    `json:"hostname"`
	ClientID string    `json:"client_id,omitempty"`
}

// Manager handles dnsmasq configuration and operations
type Manager struct {
	mu             sync.RWMutex
	fs             system.FileSystem
	runner         system.CommandRunner
	services       *system.ServiceManager
	configPath     string // DNS config path
	dhcpConfigPath string // DHCP config path (separate file)
	hostsPath      string
	leasesPath     string
}

// New creates a new dnsmasq manager
func New(fs system.FileSystem, runner system.CommandRunner, configPath, dhcpConfigPath, hostsPath string) *Manager {
	return &Manager{
		fs:             fs,
		runner:         runner,
		services:       system.NewServiceManager(runner),
		configPath:     configPath,
		dhcpConfigPath: dhcpConfigPath,
		hostsPath:      hostsPath,
		leasesPath:     "/var/lib/misc/dnsmasq.leases",
	}
}

// WriteConfig generates and writes both DNS and DHCP dnsmasq configurations
func (m *Manager) WriteConfig(cfg *config.Config) error {
	if err := m.WriteDNSConfig(cfg); err != nil {
		return err
	}
	return m.WriteDHCPConfig(cfg)
}

// WriteDNSConfig generates and writes the DNS configuration
func (m *Manager) WriteDNSConfig(cfg *config.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var sb strings.Builder

	// Header
	sb.WriteString("# Ubuntu Router DNS configuration\n")
	sb.WriteString("# Auto-generated - do not edit\n\n")

	// General settings
	sb.WriteString("# General settings\n")
	sb.WriteString("domain-needed\n")
	sb.WriteString("bogus-priv\n")
	sb.WriteString("no-resolv\n")
	sb.WriteString("no-poll\n")
	sb.WriteString("expand-hosts\n")
	sb.WriteString("localise-queries\n")
	sb.WriteString("\n")

	// Listen interface - use bind-dynamic for systemd-resolved compatibility
	// This allows dnsmasq to coexist with systemd-resolved on port 53
	if cfg.LANBridge != "" {
		sb.WriteString(fmt.Sprintf("interface=%s\n", cfg.LANBridge))
		sb.WriteString("bind-dynamic\n")
		sb.WriteString("except-interface=lo\n")
	}
	sb.WriteString("\n")

	// Local domain
	if cfg.DNSLocalZone != "" {
		sb.WriteString(fmt.Sprintf("local=/%s/\n", cfg.DNSLocalZone))
		sb.WriteString(fmt.Sprintf("domain=%s\n", cfg.DNSLocalZone))
	}
	sb.WriteString("\n")

	// Upstream DNS servers
	sb.WriteString("# Upstream DNS servers\n")
	for _, server := range cfg.DNSUpstream {
		sb.WriteString(fmt.Sprintf("server=%s\n", server))
	}
	sb.WriteString("\n")

	// Custom hosts file
	if m.hostsPath != "" {
		sb.WriteString(fmt.Sprintf("addn-hosts=%s\n", m.hostsPath))
	}
	sb.WriteString("\n")

	// Logging
	sb.WriteString("# Logging\n")
	sb.WriteString("log-facility=/var/log/dnsmasq.log\n")
	sb.WriteString("log-async\n")

	// Write config file
	if err := m.fs.WriteFile(m.configPath, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("failed to write dnsmasq DNS config: %w", err)
	}

	return nil
}

// WriteDHCPConfig generates and writes the DHCP configuration
func (m *Manager) WriteDHCPConfig(cfg *config.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var sb strings.Builder

	// Header
	sb.WriteString("# Ubuntu Router DHCP configuration\n")
	sb.WriteString("# Auto-generated - do not edit\n\n")

	// DHCP settings
	if cfg.DHCPEnabled {
		sb.WriteString("# DHCP settings\n")
		sb.WriteString(fmt.Sprintf("dhcp-range=%s,%s,%s\n", cfg.DHCPStart, cfg.DHCPEnd, cfg.DHCPLease))

		// Force broadcast for DHCP responses - required for some clients (macOS)
		// that can't receive unicast before having an IP address
		sb.WriteString("dhcp-broadcast\n")

		// Set router and DNS for DHCP clients
		if len(cfg.LANAddresses) > 0 {
			// Extract IP from CIDR
			lanIP := strings.Split(cfg.LANAddresses[0], "/")[0]
			sb.WriteString(fmt.Sprintf("dhcp-option=option:router,%s\n", lanIP))
			sb.WriteString(fmt.Sprintf("dhcp-option=option:dns-server,%s\n", lanIP))
		}

		// Domain for DHCP clients
		if cfg.DNSLocalZone != "" {
			sb.WriteString(fmt.Sprintf("dhcp-option=option:domain-name,%s\n", cfg.DNSLocalZone))
		}

		sb.WriteString("\n")

		// Static DHCP reservations
		if len(cfg.DHCPReservations) > 0 {
			sb.WriteString("# Static DHCP reservations\n")
			for _, res := range cfg.DHCPReservations {
				if res.Name != "" {
					sb.WriteString(fmt.Sprintf("dhcp-host=%s,%s,%s\n", res.MAC, res.IP, res.Name))
				} else {
					sb.WriteString(fmt.Sprintf("dhcp-host=%s,%s\n", res.MAC, res.IP))
				}
			}
			sb.WriteString("\n")
		}
	} else {
		sb.WriteString("# DHCP disabled\n")
	}

	// Write config file
	if err := m.fs.WriteFile(m.dhcpConfigPath, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("failed to write dnsmasq DHCP config: %w", err)
	}

	return nil
}

// WriteHosts writes the custom hosts file for DNS entries
func (m *Manager) WriteHosts(entries []config.DNSEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var sb strings.Builder
	sb.WriteString("# Ubuntu Router DNS entries\n")
	sb.WriteString("# Auto-generated - do not edit\n\n")

	for _, entry := range entries {
		sb.WriteString(fmt.Sprintf("%s\t%s\n", entry.IP, entry.Hostname))
	}

	return m.fs.WriteFile(m.hostsPath, []byte(sb.String()), 0644)
}

// Reload restarts dnsmasq to apply configuration changes
func (m *Manager) Reload(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// First try to reload (SIGHUP)
	if err := m.services.Reload(ctx, "dnsmasq"); err != nil {
		// If reload fails, try restart
		return m.services.Restart(ctx, "dnsmasq")
	}
	return nil
}

// Restart restarts the dnsmasq service
func (m *Manager) Restart(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.services.Restart(ctx, "dnsmasq")
}

// Start starts the dnsmasq service
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.services.Start(ctx, "dnsmasq")
}

// Stop stops the dnsmasq service
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.services.Stop(ctx, "dnsmasq")
}

// Enable enables the dnsmasq service at boot
func (m *Manager) Enable(ctx context.Context) error {
	return m.services.Enable(ctx, "dnsmasq")
}

// Disable disables the dnsmasq service at boot
func (m *Manager) Disable(ctx context.Context) error {
	return m.services.Disable(ctx, "dnsmasq")
}

// IsRunning checks if dnsmasq is running
func (m *Manager) IsRunning(ctx context.Context) bool {
	return m.services.IsActive(ctx, "dnsmasq")
}

// GetLeases returns current DHCP leases
func (m *Manager) GetLeases() ([]Lease, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := m.fs.ReadFile(m.leasesPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read leases file: %w", err)
	}

	var leases []Lease
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		// Format: timestamp mac ip hostname [client-id]
		expires, _ := parseLeaseTimestamp(fields[0])
		lease := Lease{
			Expires:  expires,
			MAC:      fields[1],
			IP:       fields[2],
			Hostname: fields[3],
		}
		if len(fields) >= 5 {
			lease.ClientID = fields[4]
		}
		leases = append(leases, lease)
	}

	// Sort by IP
	sort.Slice(leases, func(i, j int) bool {
		return leases[i].IP < leases[j].IP
	})

	return leases, nil
}

// TestConfig tests the dnsmasq configuration
func (m *Manager) TestConfig(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out, err := m.runner.Run(ctx, "dnsmasq", "--test", "-C", m.configPath)
	if err != nil {
		return fmt.Errorf("config test failed: %s", string(out))
	}
	return nil
}

// GetDNSMappings reads current DNS mappings from the hosts file
func (m *Manager) GetDNSMappings() ([]config.DNSEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := m.fs.ReadFile(m.hostsPath)
	if err != nil {
		return nil, err
	}

	var entries []config.DNSEntry
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	hostRegex := regexp.MustCompile(`^(\S+)\s+(\S+)`)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		matches := hostRegex.FindStringSubmatch(line)
		if len(matches) >= 3 {
			entries = append(entries, config.DNSEntry{
				IP:       matches[1],
				Hostname: matches[2],
			})
		}
	}

	return entries, nil
}

// AddDNSEntry adds a DNS entry to the hosts file
func (m *Manager) AddDNSEntry(ctx context.Context, hostname, ip string) error {
	entries, err := m.GetDNSMappings()
	if err != nil {
		entries = []config.DNSEntry{}
	}

	// Check for duplicate
	for _, e := range entries {
		if e.Hostname == hostname {
			return fmt.Errorf("hostname %s already exists", hostname)
		}
	}

	entries = append(entries, config.DNSEntry{
		Hostname: hostname,
		IP:       ip,
	})

	if err := m.WriteHosts(entries); err != nil {
		return err
	}

	return m.Reload(ctx)
}

// RemoveDNSEntry removes a DNS entry from the hosts file
func (m *Manager) RemoveDNSEntry(ctx context.Context, hostname string) error {
	entries, err := m.GetDNSMappings()
	if err != nil {
		return err
	}

	var newEntries []config.DNSEntry
	found := false
	for _, e := range entries {
		if e.Hostname == hostname {
			found = true
			continue
		}
		newEntries = append(newEntries, e)
	}

	if !found {
		return fmt.Errorf("hostname %s not found", hostname)
	}

	if err := m.WriteHosts(newEntries); err != nil {
		return err
	}

	return m.Reload(ctx)
}

// AddDHCPReservation adds a static DHCP reservation
func (m *Manager) AddDHCPReservation(ctx context.Context, cfg *config.Config, name, mac, ip string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for duplicate
	mac = strings.ToLower(mac)
	for _, res := range cfg.DHCPReservations {
		if strings.ToLower(res.MAC) == mac {
			return fmt.Errorf("reservation for MAC %s already exists", mac)
		}
	}

	cfg.DHCPReservations = append(cfg.DHCPReservations, config.DHCPReservation{
		Name: name,
		MAC:  mac,
		IP:   ip,
	})

	return nil
}

// RemoveDHCPReservation removes a static DHCP reservation
func (m *Manager) RemoveDHCPReservation(ctx context.Context, cfg *config.Config, mac string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	mac = strings.ToLower(mac)
	var newReservations []config.DHCPReservation
	found := false
	for _, res := range cfg.DHCPReservations {
		if strings.ToLower(res.MAC) == mac {
			found = true
			continue
		}
		newReservations = append(newReservations, res)
	}

	if !found {
		return fmt.Errorf("reservation for MAC %s not found", mac)
	}

	cfg.DHCPReservations = newReservations
	return nil
}

// LookupReverse performs a reverse DNS lookup
func (m *Manager) LookupReverse(ctx context.Context, ip string) (string, error) {
	// Check local entries first
	entries, _ := m.GetDNSMappings()
	for _, e := range entries {
		if e.IP == ip {
			return e.Hostname, nil
		}
	}

	// Check leases
	leases, _ := m.GetLeases()
	for _, l := range leases {
		if l.IP == ip && l.Hostname != "*" {
			return l.Hostname, nil
		}
	}

	return "", fmt.Errorf("no reverse DNS entry for %s", ip)
}

// DisableSystemResolved disables systemd-resolved which conflicts with dnsmasq
func (m *Manager) DisableSystemResolved(ctx context.Context) error {
	// Stop and disable systemd-resolved
	m.services.Stop(ctx, "systemd-resolved")
	m.services.Disable(ctx, "systemd-resolved")

	// Point /etc/resolv.conf to dnsmasq
	m.fs.Remove("/etc/resolv.conf")

	var sb strings.Builder
	sb.WriteString("# Generated by ubuntu-router\n")
	sb.WriteString("nameserver 127.0.0.1\n")

	return m.fs.WriteFile("/etc/resolv.conf", []byte(sb.String()), 0644)
}

// ConfigureUpstreamDNS updates upstream DNS servers
func (m *Manager) ConfigureUpstreamDNS(servers []string, cfg *config.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg.DNSUpstream = servers
	return nil
}

func parseLeaseTimestamp(ts string) (time.Time, error) {
	// dnsmasq uses Unix timestamp
	var timestamp int64
	if _, err := fmt.Sscanf(ts, "%d", &timestamp); err != nil {
		return time.Time{}, err
	}
	return time.Unix(timestamp, 0), nil
}

// Status returns the current status of dnsmasq
type Status struct {
	Running     bool     `json:"running"`
	ConfigValid bool     `json:"config_valid"`
	LeaseCount  int      `json:"lease_count"`
	DNSEntries  int      `json:"dns_entries"`
	UpstreamDNS []string `json:"upstream_dns"`
}

// GetStatus returns the current status
func (m *Manager) GetStatus(ctx context.Context, cfg *config.Config) (*Status, error) {
	status := &Status{
		Running:     m.IsRunning(ctx),
		UpstreamDNS: cfg.DNSUpstream,
	}

	// Test config
	if err := m.TestConfig(ctx); err == nil {
		status.ConfigValid = true
	}

	// Count leases
	if leases, err := m.GetLeases(); err == nil {
		status.LeaseCount = len(leases)
	}

	// Count DNS entries
	if entries, err := m.GetDNSMappings(); err == nil {
		status.DNSEntries = len(entries)
	}

	return status, nil
}
