package haproxy

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/iodesystems/ubuntu-router/internal/config"
	"github.com/iodesystems/ubuntu-router/internal/system"
)

// Backend represents a HAProxy backend service
type Backend struct {
	Name        string `json:"name"`
	DomainMatch string `json:"domain_match"` // Full domain or suffix like ".example.com"
	Server      string `json:"server"`       // host:port
	HTTPCheck   bool   `json:"http_check"`
	CheckPath   string `json:"check_path"`
	Mode        string `json:"mode"` // http or tcp
}

// BackendStatus contains runtime status of a backend
type BackendStatus struct {
	Backend
	Healthy   bool      `json:"healthy"`
	LastCheck time.Time `json:"last_check"`
	Error     string    `json:"error,omitempty"`
}

// Status contains HAProxy status information
type Status struct {
	Running      bool   `json:"running"`
	Enabled      bool   `json:"enabled"`
	Installed    bool   `json:"installed"`
	ConfigExists bool   `json:"config_exists"`
	ConfigValid  bool   `json:"config_valid"`
	Version      string `json:"version,omitempty"`
	Error        string `json:"error,omitempty"`
	BackendCount int    `json:"backend_count"`
}

// Manager handles HAProxy configuration and operations
type Manager struct {
	mu         sync.RWMutex
	fs         system.FileSystem
	runner     system.CommandRunner
	services   *system.ServiceManager
	configPath string
	certDir    string
}

// New creates a new HAProxy manager
func New(fs system.FileSystem, runner system.CommandRunner, certDir string) *Manager {
	return &Manager{
		fs:         fs,
		runner:     runner,
		services:   system.NewServiceManager(runner),
		configPath: "/etc/haproxy/haproxy.cfg",
		certDir:    certDir,
	}
}

// WriteConfig generates and writes HAProxy configuration from services
// lanAddresses is used to add an admin UI backend for the gateway IP
func (m *Manager) WriteConfig(services []config.Service, zones []config.ExternalDNSZone, lanAddresses []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Build backends from services
	var backends []Backend
	for _, svc := range services {
		if !svc.Enabled || svc.Proxy == nil {
			continue
		}

		// Find zone name
		var zoneName string
		for _, z := range zones {
			if z.ID == svc.ZoneID {
				zoneName = z.Name
				break
			}
		}
		if zoneName == "" {
			continue // Skip services without valid zone
		}

		// Parse backend URL to get host:port
		server := svc.Proxy.Backend
		if strings.HasPrefix(server, "http://") || strings.HasPrefix(server, "https://") {
			if u, err := url.Parse(server); err == nil {
				host := u.Host
				if !strings.Contains(host, ":") {
					if u.Scheme == "https" {
						host += ":443"
					} else {
						host += ":80"
					}
				}
				server = host
			}
		}

		// Build domain match - the full domain
		domainMatch := svc.DNSName + "." + zoneName

		mode := svc.Proxy.Mode
		if mode == "" {
			mode = "http"
		}

		backends = append(backends, Backend{
			Name:        svc.Name,
			DomainMatch: domainMatch,
			Server:      server,
			HTTPCheck:   svc.Proxy.HealthCheck != "",
			CheckPath:   svc.Proxy.HealthCheck,
			Mode:        mode,
		})

		// Add wildcard match if SSL has wildcards
		if svc.SSL != nil && svc.SSL.Enabled {
			for _, sub := range svc.SSL.Subdomains {
				if strings.HasPrefix(sub, "*") {
					// For wildcards, add suffix match
					suffix := strings.TrimPrefix(sub, "*")
					wildcardMatch := suffix + "." + svc.DNSName + "." + zoneName
					backends = append(backends, Backend{
						Name:        svc.Name + "_" + strings.ReplaceAll(sub, "*", "wild"),
						DomainMatch: wildcardMatch,
						Server:      server,
						HTTPCheck:   svc.Proxy.HealthCheck != "",
						CheckPath:   svc.Proxy.HealthCheck,
						Mode:        mode,
					})
				}
			}
		}
	}

	// Add admin UI backend for the gateway IP
	// This allows access to the admin UI on port 80 from the LAN
	if len(lanAddresses) > 0 {
		lanIP := strings.Split(lanAddresses[0], "/")[0]
		backends = append(backends, Backend{
			Name:        "admin_ui",
			DomainMatch: lanIP, // Match requests to the gateway IP
			Server:      lanIP + ":8080",
			HTTPCheck:   false,
			Mode:        "http",
		})
	}

	cfg := m.generateConfig(backends, 80, 443)
	return m.fs.WriteFile(m.configPath, []byte(cfg), 0644)
}

// GenerateConfig returns the HAProxy configuration as a string (for preview)
func (m *Manager) GenerateConfig(services []config.Service, zones []config.ExternalDNSZone, lanAddresses []string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var backends []Backend
	for _, svc := range services {
		if !svc.Enabled || svc.Proxy == nil {
			continue
		}

		var zoneName string
		for _, z := range zones {
			if z.ID == svc.ZoneID {
				zoneName = z.Name
				break
			}
		}
		if zoneName == "" {
			continue
		}

		server := svc.Proxy.Backend
		if strings.HasPrefix(server, "http://") || strings.HasPrefix(server, "https://") {
			if u, err := url.Parse(server); err == nil {
				host := u.Host
				if !strings.Contains(host, ":") {
					if u.Scheme == "https" {
						host += ":443"
					} else {
						host += ":80"
					}
				}
				server = host
			}
		}

		mode := svc.Proxy.Mode
		if mode == "" {
			mode = "http"
		}

		backends = append(backends, Backend{
			Name:        svc.Name,
			DomainMatch: svc.DNSName + "." + zoneName,
			Server:      server,
			HTTPCheck:   svc.Proxy.HealthCheck != "",
			CheckPath:   svc.Proxy.HealthCheck,
			Mode:        mode,
		})
	}

	// Add admin UI backend for the gateway IP
	if len(lanAddresses) > 0 {
		lanIP := strings.Split(lanAddresses[0], "/")[0]
		backends = append(backends, Backend{
			Name:        "admin_ui",
			DomainMatch: lanIP,
			Server:      lanIP + ":8080",
			HTTPCheck:   false,
			Mode:        "http",
		})
	}

	return m.generateConfig(backends, 80, 443)
}

func (m *Manager) generateConfig(backends []Backend, httpPort, httpsPort int) string {
	var sb strings.Builder

	// Global section
	sb.WriteString(`# HAProxy configuration
# Auto-generated by ubuntu-router - do not edit

global
    log /dev/log local0
    log /dev/log local1 notice
    chroot /var/lib/haproxy
    stats socket /run/haproxy/admin.sock mode 660 level admin
    stats timeout 30s
    user haproxy
    group haproxy
    daemon
    ssl-default-bind-options ssl-min-ver TLSv1.2

defaults
    log     global
    mode    http
    option  httplog
    option  dontlognull
    timeout connect 5000
    timeout client  50000
    timeout server  50000

`)

	// Stats frontend
	sb.WriteString(`# Stats page
listen stats
    bind *:8404
    stats enable
    stats uri /stats
    stats refresh 10s
    stats admin if LOCALHOST

`)

	// Check if SSL certs exist
	sslEnabled := false
	if m.certDir != "" {
		entries, err := os.ReadDir(m.certDir)
		if err == nil {
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".pem") {
					sslEnabled = true
					break
				}
			}
		}
	}

	// HTTP frontend
	if sslEnabled {
		sb.WriteString(fmt.Sprintf(`frontend http_front
    bind *:%d
    mode http
    option forwardfor
    # Health check endpoint
    acl is_health_check path /haproxy-health
    http-request return status 200 content-type "text/plain" string "OK" if is_health_check
    # Redirect to HTTPS
    redirect scheme https code 301 if !is_health_check

`, httpPort))

		// HTTPS frontend
		certDir := m.certDir
		if !strings.HasSuffix(certDir, "/") {
			certDir += "/"
		}
		sb.WriteString(fmt.Sprintf(`frontend https_front
    bind *:%d ssl crt %s
    mode http
    option forwardfor
    http-request set-header X-Forwarded-Proto https
    http-request set-header X-Forwarded-Port %%[dst_port]
    http-request set-header X-Real-IP %%[src]
    # Health check endpoint
    http-request return status 200 content-type "text/plain" string "OK" if { path /haproxy-health }
`, httpsPort, certDir))
	} else {
		sb.WriteString(fmt.Sprintf(`frontend http_front
    bind *:%d
    mode http
    option forwardfor
    http-request set-header X-Real-IP %%[src]
    # Health check endpoint
    http-request return status 200 content-type "text/plain" string "OK" if { path /haproxy-health }
`, httpPort))
	}

	// Add ACLs for each backend
	for _, b := range backends {
		aclName := sanitizeName(b.Name)
		// Use exact match for the domain
		sb.WriteString(fmt.Sprintf("    acl host_%s hdr(host) -i %s\n", aclName, b.DomainMatch))
		// Also match with port stripped
		sb.WriteString(fmt.Sprintf("    acl host_%s hdr_beg(host) -i %s:\n", aclName, b.DomainMatch))
	}
	sb.WriteString("\n")

	// Add use_backend rules
	for _, b := range backends {
		aclName := sanitizeName(b.Name)
		sb.WriteString(fmt.Sprintf("    use_backend %s_backend if host_%s\n", aclName, aclName))
	}
	sb.WriteString("\n")

	// Backend definitions
	for _, b := range backends {
		aclName := sanitizeName(b.Name)
		sb.WriteString(fmt.Sprintf("backend %s_backend\n", aclName))
		sb.WriteString(fmt.Sprintf("    mode %s\n", b.Mode))
		sb.WriteString("    balance roundrobin\n")

		if b.HTTPCheck && b.Mode == "http" {
			checkPath := b.CheckPath
			if checkPath == "" {
				checkPath = "/"
			}
			sb.WriteString(fmt.Sprintf("    option httpchk GET %s\n", checkPath))
			sb.WriteString(fmt.Sprintf("    server %s %s check\n", aclName, b.Server))
		} else {
			sb.WriteString(fmt.Sprintf("    server %s %s\n", aclName, b.Server))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func sanitizeName(name string) string {
	result := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, name)
	return strings.ToLower(result)
}

// TestConfig validates the HAProxy configuration
func (m *Manager) TestConfig(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out, err := m.runner.Run(ctx, "haproxy", "-c", "-f", m.configPath)
	if err != nil {
		return fmt.Errorf("config validation failed: %s", string(out))
	}
	return nil
}

// Reload reloads HAProxy configuration
func (m *Manager) Reload(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Validate config first
	out, err := m.runner.Run(ctx, "haproxy", "-c", "-f", m.configPath)
	if err != nil {
		return fmt.Errorf("config validation failed: %s", string(out))
	}

	// Try reload first
	if err := m.services.Reload(ctx, "haproxy"); err != nil {
		// Fall back to restart
		return m.services.Restart(ctx, "haproxy")
	}
	return nil
}

// Restart restarts HAProxy
func (m *Manager) Restart(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.services.Restart(ctx, "haproxy")
}

// Start starts HAProxy
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.services.Start(ctx, "haproxy")
}

// Stop stops HAProxy
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.services.Stop(ctx, "haproxy")
}

// Enable enables HAProxy at boot
func (m *Manager) Enable(ctx context.Context) error {
	return m.services.Enable(ctx, "haproxy")
}

// Disable disables HAProxy at boot
func (m *Manager) Disable(ctx context.Context) error {
	return m.services.Disable(ctx, "haproxy")
}

// IsRunning checks if HAProxy is running
func (m *Manager) IsRunning(ctx context.Context) bool {
	return m.services.IsActive(ctx, "haproxy")
}

// IsEnabled checks if HAProxy is enabled at boot
func (m *Manager) IsEnabled(ctx context.Context) bool {
	return m.services.IsEnabled(ctx, "haproxy")
}

// GetStatus returns current HAProxy status
func (m *Manager) GetStatus(ctx context.Context, services []config.Service) (*Status, error) {
	status := &Status{
		Running: m.IsRunning(ctx),
		Enabled: m.IsEnabled(ctx),
	}

	// Check if installed
	_, err := m.runner.Run(ctx, "which", "haproxy")
	status.Installed = err == nil

	// Check if config exists
	if _, err := m.fs.Stat(m.configPath); err == nil {
		status.ConfigExists = true
	}

	// Validate config
	if status.ConfigExists {
		if err := m.TestConfig(ctx); err == nil {
			status.ConfigValid = true
		} else {
			status.Error = err.Error()
		}
	}

	// Get version
	out, err := m.runner.Run(ctx, "haproxy", "-v")
	if err == nil {
		lines := strings.Split(string(out), "\n")
		if len(lines) > 0 {
			status.Version = strings.TrimSpace(lines[0])
		}
	}

	// Count backends
	for _, svc := range services {
		if svc.Enabled && svc.Proxy != nil {
			status.BackendCount++
		}
	}

	return status, nil
}

// GetBackendStatuses checks health of all backends
func (m *Manager) GetBackendStatuses(ctx context.Context, services []config.Service, zones []config.ExternalDNSZone) []BackendStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var statuses []BackendStatus

	for _, svc := range services {
		if !svc.Enabled || svc.Proxy == nil {
			continue
		}

		var zoneName string
		for _, z := range zones {
			if z.ID == svc.ZoneID {
				zoneName = z.Name
				break
			}
		}

		// Parse server address
		server := svc.Proxy.Backend
		if strings.HasPrefix(server, "http://") || strings.HasPrefix(server, "https://") {
			if u, err := url.Parse(server); err == nil {
				host := u.Host
				if !strings.Contains(host, ":") {
					if u.Scheme == "https" {
						host += ":443"
					} else {
						host += ":80"
					}
				}
				server = host
			}
		}

		bs := BackendStatus{
			Backend: Backend{
				Name:        svc.Name,
				DomainMatch: svc.DNSName + "." + zoneName,
				Server:      server,
				HTTPCheck:   svc.Proxy.HealthCheck != "",
				CheckPath:   svc.Proxy.HealthCheck,
				Mode:        svc.Proxy.Mode,
			},
			LastCheck: time.Now(),
		}

		// Try to connect to the backend
		conn, err := net.DialTimeout("tcp", server, 3*time.Second)
		if err != nil {
			bs.Error = err.Error()
			bs.Healthy = false
		} else {
			conn.Close()
			bs.Healthy = true

			// If HTTP check is enabled, do a basic check
			if bs.HTTPCheck && bs.CheckPath != "" && bs.Mode == "http" {
				bs.Healthy, bs.Error = m.httpCheck(server, bs.CheckPath)
			}
		}

		statuses = append(statuses, bs)
	}

	return statuses
}

func (m *Manager) httpCheck(server, path string) (bool, string) {
	conn, err := net.DialTimeout("tcp", server, 3*time.Second)
	if err != nil {
		return false, err.Error()
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(5 * time.Second))
	request := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n", path)
	_, err = conn.Write([]byte(request))
	if err != nil {
		return false, err.Error()
	}

	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		return false, err.Error()
	}

	// Check for 2xx status
	if strings.Contains(statusLine, " 2") {
		return true, ""
	}

	return false, "HTTP " + strings.TrimSpace(statusLine)
}

// Available checks if haproxy is installed
func Available(runner system.CommandRunner) bool {
	ctx := context.Background()
	_, err := runner.Run(ctx, "which", "haproxy")
	return err == nil
}
