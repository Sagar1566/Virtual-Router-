package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/iodesystems/ubuntu-router/internal/config"
	"github.com/iodesystems/ubuntu-router/internal/dns"
	"github.com/iodesystems/ubuntu-router/internal/dnsmasq"
	"github.com/iodesystems/ubuntu-router/internal/haproxy"
	"github.com/iodesystems/ubuntu-router/internal/system"
)

// SyncResult represents the result of syncing a service
type SyncResult struct {
	ServiceID   string `json:"service_id"`
	ServiceName string `json:"service_name"`
	Success     bool   `json:"success"`
	Message     string `json:"message,omitempty"`
	Error       string `json:"error,omitempty"`

	// Details
	InternalDNSChanged bool `json:"internal_dns_changed,omitempty"`
	ExternalDNSChanged bool `json:"external_dns_changed,omitempty"`
	HAProxyChanged     bool `json:"haproxy_changed,omitempty"`
}

// Status represents the overall services status
type Status struct {
	Enabled      bool   `json:"enabled"`
	ServiceCount int    `json:"service_count"`
	ActiveCount  int    `json:"active_count"`
	HAProxyOK    bool   `json:"haproxy_ok"`
	LastSynced   string `json:"last_synced,omitempty"`
}

// Manager orchestrates services -> DNS, HAProxy, SSL
type Manager struct {
	cfg     *config.Config
	cfgPath string
	fs      system.FileSystem
	runner  system.CommandRunner
	dnsmasq *dnsmasq.Manager
	haproxy *haproxy.Manager

	// Service hosts file path (separate from main DNS hosts)
	serviceHostsPath string
}

// New creates a new services manager
func New(
	cfg *config.Config,
	cfgPath string,
	fs system.FileSystem,
	runner system.CommandRunner,
	dnsmasq *dnsmasq.Manager,
	haproxy *haproxy.Manager,
) *Manager {
	return &Manager{
		cfg:              cfg,
		cfgPath:          cfgPath,
		fs:               fs,
		runner:           runner,
		dnsmasq:          dnsmasq,
		haproxy:          haproxy,
		serviceHostsPath: "/etc/dnsmasq.d/ubuntu-router-services.conf",
	}
}

// GetStatus returns the current status
func (m *Manager) GetStatus(ctx context.Context) (*Status, error) {
	status := &Status{
		Enabled: m.cfg.Services != nil && m.cfg.Services.Enabled,
	}

	if m.cfg.Services != nil {
		status.ServiceCount = len(m.cfg.Services.Services)
		for _, svc := range m.cfg.Services.Services {
			if svc.Enabled {
				status.ActiveCount++
			}
		}
	}

	// Check HAProxy status
	if haproxyStatus, err := m.haproxy.GetStatus(ctx, m.getServices()); err == nil {
		status.HAProxyOK = haproxyStatus.Running && haproxyStatus.ConfigValid
	}

	return status, nil
}

// GetService returns a service by ID
func (m *Manager) GetService(id string) *config.Service {
	if m.cfg.Services == nil {
		return nil
	}
	for i := range m.cfg.Services.Services {
		if m.cfg.Services.Services[i].ID == id {
			return &m.cfg.Services.Services[i]
		}
	}
	return nil
}

// GetServices returns all services
func (m *Manager) GetServices() []config.Service {
	return m.getServices()
}

func (m *Manager) getServices() []config.Service {
	if m.cfg.Services == nil {
		return nil
	}
	return m.cfg.Services.Services
}

// GetZone returns a zone by ID
func (m *Manager) GetZone(id string) *config.ExternalDNSZone {
	if m.cfg.ExternalDNS == nil {
		return nil
	}
	for i := range m.cfg.ExternalDNS.Zones {
		if m.cfg.ExternalDNS.Zones[i].ID == id {
			return &m.cfg.ExternalDNS.Zones[i]
		}
	}
	return nil
}

// GetZones returns all zones
func (m *Manager) GetZones() []config.ExternalDNSZone {
	if m.cfg.ExternalDNS == nil {
		return nil
	}
	return m.cfg.ExternalDNS.Zones
}

// CreateService creates a new service
func (m *Manager) CreateService(svc config.Service) (*config.Service, error) {
	if m.cfg.Services == nil {
		m.cfg.Services = &config.ServicesConfig{
			Enabled:  true,
			Services: []config.Service{},
		}
	}

	// Generate ID
	svc.ID = fmt.Sprintf("svc-%d", time.Now().UnixNano())
	svc.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	svc.UpdatedAt = svc.CreatedAt

	// Validate
	if err := m.validateService(&svc); err != nil {
		return nil, err
	}

	m.cfg.Services.Services = append(m.cfg.Services.Services, svc)

	// Save config
	if err := config.Save(m.cfgPath, m.cfg); err != nil {
		return nil, fmt.Errorf("failed to save config: %w", err)
	}

	return &svc, nil
}

// UpdateService updates an existing service
func (m *Manager) UpdateService(id string, updates config.Service) (*config.Service, error) {
	if m.cfg.Services == nil {
		return nil, fmt.Errorf("services not configured")
	}

	var svc *config.Service
	for i := range m.cfg.Services.Services {
		if m.cfg.Services.Services[i].ID == id {
			svc = &m.cfg.Services.Services[i]
			break
		}
	}

	if svc == nil {
		return nil, fmt.Errorf("service not found: %s", id)
	}

	// Update fields
	svc.Name = updates.Name
	svc.DNSName = updates.DNSName
	svc.ZoneID = updates.ZoneID
	svc.Enabled = updates.Enabled
	svc.InternalDNS = updates.InternalDNS
	svc.ExternalDNS = updates.ExternalDNS
	svc.Proxy = updates.Proxy
	svc.SSL = updates.SSL
	svc.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	// Validate
	if err := m.validateService(svc); err != nil {
		return nil, err
	}

	// Save config
	if err := config.Save(m.cfgPath, m.cfg); err != nil {
		return nil, fmt.Errorf("failed to save config: %w", err)
	}

	return svc, nil
}

// DeleteService removes a service
func (m *Manager) DeleteService(ctx context.Context, id string) error {
	if m.cfg.Services == nil {
		return fmt.Errorf("services not configured")
	}

	var found bool
	var newServices []config.Service
	for _, svc := range m.cfg.Services.Services {
		if svc.ID == id {
			found = true
			continue
		}
		newServices = append(newServices, svc)
	}

	if !found {
		return fmt.Errorf("service not found: %s", id)
	}

	m.cfg.Services.Services = newServices

	// Save config
	if err := config.Save(m.cfgPath, m.cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	// Sync to remove from DNS and HAProxy
	return m.syncInternal(ctx)
}

func (m *Manager) validateService(svc *config.Service) error {
	if svc.Name == "" {
		return fmt.Errorf("service name is required")
	}
	if svc.DNSName == "" {
		return fmt.Errorf("DNS name is required")
	}
	if svc.ZoneID == "" {
		return fmt.Errorf("zone is required")
	}

	// Check zone exists
	zone := m.GetZone(svc.ZoneID)
	if zone == nil {
		return fmt.Errorf("zone not found: %s", svc.ZoneID)
	}

	// Validate DNS name format
	if strings.Contains(svc.DNSName, ".") {
		return fmt.Errorf("DNS name should not contain dots - use subdomain only (e.g., 'homeassistant' not 'homeassistant.example.com')")
	}

	// Check for duplicate DNS names in same zone
	for _, existing := range m.cfg.Services.Services {
		if existing.ID != svc.ID && existing.DNSName == svc.DNSName && existing.ZoneID == svc.ZoneID {
			return fmt.Errorf("DNS name '%s' already exists in zone", svc.DNSName)
		}
	}

	return nil
}

// SyncAll syncs all service configurations
func (m *Manager) SyncAll(ctx context.Context) ([]SyncResult, error) {
	if m.cfg.Services == nil || !m.cfg.Services.Enabled {
		return nil, fmt.Errorf("services not enabled")
	}

	results := []SyncResult{}
	for _, svc := range m.cfg.Services.Services {
		result := m.syncService(ctx, &svc)
		results = append(results, result)
	}

	// Sync internal DNS and HAProxy
	if err := m.syncInternal(ctx); err != nil {
		return results, fmt.Errorf("internal sync failed: %w", err)
	}

	return results, nil
}

// SyncService syncs a single service
func (m *Manager) SyncService(ctx context.Context, serviceID string) (*SyncResult, error) {
	svc := m.GetService(serviceID)
	if svc == nil {
		return nil, fmt.Errorf("service not found: %s", serviceID)
	}

	result := m.syncService(ctx, svc)

	// Sync internal DNS and HAProxy
	if err := m.syncInternal(ctx); err != nil {
		result.Error = fmt.Sprintf("internal sync failed: %v", err)
		result.Success = false
	}

	return &result, nil
}

func (m *Manager) syncService(ctx context.Context, svc *config.Service) SyncResult {
	result := SyncResult{
		ServiceID:   svc.ID,
		ServiceName: svc.Name,
		Success:     true,
	}

	if !svc.Enabled {
		result.Message = "Service disabled, skipping"
		return result
	}

	zone := m.GetZone(svc.ZoneID)
	if zone == nil {
		result.Success = false
		result.Error = "Zone not found"
		return result
	}

	// Sync external DNS if configured
	if svc.ExternalDNS != nil {
		changed, err := m.syncExternalDNS(ctx, svc, zone)
		if err != nil {
			result.Success = false
			result.Error = fmt.Sprintf("external DNS sync failed: %v", err)
			return result
		}
		result.ExternalDNSChanged = changed
	}

	result.Message = "Synced successfully"
	return result
}

func (m *Manager) syncExternalDNS(ctx context.Context, svc *config.Service, zone *config.ExternalDNSZone) (bool, error) {
	if svc.ExternalDNS == nil {
		return false, nil
	}

	// Get provider
	provider, err := dns.NewProvider(&zone.Provider, zone.Name)
	if err != nil {
		return false, fmt.Errorf("failed to create DNS provider: %w", err)
	}

	// Determine IP
	ip := svc.ExternalDNS.IP
	if ip == "" || svc.ExternalDNS.AutoSync {
		// Get public IP
		publicIP, err := dns.GetPublicIP()
		if err != nil {
			return false, fmt.Errorf("failed to get public IP: %w", err)
		}
		ip = publicIP
	}

	// Build record
	fqdn := svc.DNSName + "." + zone.Name
	ttl := svc.ExternalDNS.TTL
	if ttl == 0 {
		ttl = 300
	}

	record := dns.Record{
		Name:   fqdn,
		Type:   "A",
		Value:  ip,
		TTL:    ttl,
		ZoneID: zone.Provider.AWSHostedZoneID, // For Route53
	}

	// Sync record
	changed, err := provider.SyncRecord(zone.Name, record)
	if err != nil {
		return false, err
	}

	return changed, nil
}

// syncInternal writes the internal DNS hosts file and HAProxy config
func (m *Manager) syncInternal(ctx context.Context) error {
	// Write internal DNS entries for services
	if err := m.writeServiceHosts(); err != nil {
		return fmt.Errorf("failed to write service hosts: %w", err)
	}

	// Reload dnsmasq
	if err := m.dnsmasq.Reload(ctx); err != nil {
		// Don't fail on dnsmasq reload errors
		fmt.Printf("Warning: dnsmasq reload failed: %v\n", err)
	}

	// Write HAProxy config
	zones := m.GetZones()
	services := m.getServices()
	if err := m.haproxy.WriteConfig(services, zones, m.cfg.LANAddresses); err != nil {
		return fmt.Errorf("failed to write HAProxy config: %w", err)
	}

	// Reload HAProxy if running
	if m.haproxy.IsRunning(ctx) {
		if err := m.haproxy.Reload(ctx); err != nil {
			return fmt.Errorf("failed to reload HAProxy: %w", err)
		}
	}

	return nil
}

func (m *Manager) writeServiceHosts() error {
	var sb strings.Builder
	sb.WriteString("# Ubuntu Router Services - Internal DNS\n")
	sb.WriteString("# Auto-generated - do not edit\n\n")

	for _, svc := range m.getServices() {
		if !svc.Enabled || svc.InternalDNS == nil {
			continue
		}

		zone := m.GetZone(svc.ZoneID)
		if zone == nil {
			continue
		}

		fqdn := svc.DNSName + "." + zone.Name
		ip := svc.InternalDNS.IP

		// Write as dnsmasq address directive
		sb.WriteString(fmt.Sprintf("address=/%s/%s\n", fqdn, ip))

		// If SSL wildcards are configured, add them too
		if svc.SSL != nil && svc.SSL.Enabled {
			for _, sub := range svc.SSL.Subdomains {
				if strings.HasPrefix(sub, "*") {
					// For wildcard like "*", add *.svc.zone.com
					suffix := strings.TrimPrefix(sub, "*")
					wildcardDomain := suffix + svc.DNSName + "." + zone.Name
					sb.WriteString(fmt.Sprintf("address=/%s/%s\n", wildcardDomain, ip))
				}
			}
		}
	}

	return m.fs.WriteFile(m.serviceHostsPath, []byte(sb.String()), 0644)
}

// GetDerivedSSLDomains returns all SSL domains from services for a zone
func (m *Manager) GetDerivedSSLDomains(zoneID string) []string {
	zone := m.GetZone(zoneID)
	if zone == nil {
		return nil
	}

	var domains []string
	seen := make(map[string]bool)

	for _, svc := range m.getServices() {
		if !svc.Enabled || svc.SSL == nil || !svc.SSL.Enabled || svc.ZoneID != zoneID {
			continue
		}

		baseDomain := svc.DNSName + "." + zone.Name

		// Always include base domain
		if !seen[baseDomain] {
			domains = append(domains, baseDomain)
			seen[baseDomain] = true
		}

		// Add subdomains
		for _, sub := range svc.SSL.Subdomains {
			var domain string
			if sub == "*" {
				domain = "*." + baseDomain
			} else if strings.HasPrefix(sub, "*.") {
				// Multi-level wildcard like "*.api"
				domain = sub + "." + baseDomain
			} else {
				domain = sub + "." + baseDomain
			}

			if !seen[domain] {
				domains = append(domains, domain)
				seen[domain] = true
			}
		}
	}

	return domains
}

// UpdateSettings updates services global settings
func (m *Manager) UpdateSettings(enabled bool) error {
	if m.cfg.Services == nil {
		m.cfg.Services = &config.ServicesConfig{
			Services: []config.Service{},
		}
	}
	m.cfg.Services.Enabled = enabled

	return config.Save(m.cfgPath, m.cfg)
}
