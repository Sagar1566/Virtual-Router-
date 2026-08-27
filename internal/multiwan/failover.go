package multiwan

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/iodesystems/ubuntu-router/internal/config"
	"github.com/iodesystems/ubuntu-router/internal/logger"
	"github.com/iodesystems/ubuntu-router/internal/system"
)

// WANStatus represents the current status of a WAN interface
type WANStatus struct {
	Name          string    `json:"name"`
	Interface     string    `json:"interface"`
	Priority      int       `json:"priority"`
	Enabled       bool      `json:"enabled"`
	Active        bool      `json:"active"`        // Currently the active WAN
	Up            bool      `json:"up"`            // Link is up
	Healthy       bool      `json:"healthy"`       // Passes health checks
	IPAddress     string    `json:"ip_address"`
	Gateway       string    `json:"gateway"`
	LastCheck     time.Time `json:"last_check"`
	LastHealthy   time.Time `json:"last_healthy"`
	FailureCount  int       `json:"failure_count"`
	ChecksRun     int       `json:"checks_run"`
	ChecksPassed  int       `json:"checks_passed"`
}

// FailoverStatus represents the overall multi-WAN status
type FailoverStatus struct {
	Enabled     bool        `json:"enabled"`
	Mode        string      `json:"mode"`
	ActiveWAN   string      `json:"active_wan"`   // Interface name of active WAN
	WANs        []WANStatus `json:"wans"`
	LastSwitch  time.Time   `json:"last_switch"`
	SwitchCount int         `json:"switch_count"`
}

// Manager handles multi-WAN failover
type Manager struct {
	mu              sync.RWMutex
	runner          system.CommandRunner
	fs              system.FileSystem
	wans            []config.WANConfig
	failbackDelay   int
	status          map[string]*WANStatus // keyed by interface name
	activeWAN       string
	lastSwitch      time.Time
	switchCount     int
	stopChan        chan struct{}
	running         bool
	hadHealthyWANs  bool // Track if we previously had healthy WANs (for logging)
}

// New creates a new multi-WAN manager
func New(runner system.CommandRunner, fs system.FileSystem) *Manager {
	return &Manager{
		runner: runner,
		fs:     fs,
		status: make(map[string]*WANStatus),
	}
}

// Configure sets the multi-WAN configuration from the main config
func (m *Manager) Configure(cfg *config.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.wans = cfg.WANs
	m.failbackDelay = cfg.FailbackDelay
	if m.failbackDelay <= 0 {
		m.failbackDelay = 60 // default 60 seconds
	}
	m.status = make(map[string]*WANStatus)

	if len(m.wans) == 0 {
		return
	}

	// Initialize status for each WAN
	for _, wan := range m.wans {
		if !wan.Enabled {
			continue
		}
		m.status[wan.Interface] = &WANStatus{
			Name:      wan.Name,
			Interface: wan.Interface,
			Priority:  wan.Priority,
			Enabled:   wan.Enabled,
			Healthy:   true, // Assume healthy until proven otherwise
		}
	}
}

// interfaceExists checks if a network interface exists on the system
func (m *Manager) interfaceExists(ctx context.Context, iface string) bool {
	_, err := m.runner.Run(ctx, "ip", "link", "show", iface)
	return err == nil
}

// Start begins the failover monitoring loop
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return nil
	}
	if len(m.wans) == 0 {
		m.mu.Unlock()
		return nil
	}
	m.running = true
	m.stopChan = make(chan struct{})
	wans := m.wans
	m.mu.Unlock()

	// Filter out WANs with missing interfaces to avoid errors
	var activeWANs []config.WANConfig
	for _, wan := range wans {
		if !wan.Enabled {
			continue
		}
		if !m.interfaceExists(ctx, wan.Interface) {
			logger.Warn("Multi-WAN: Skipping %s (%s) - interface does not exist", wan.Name, wan.Interface)
			// Mark as unhealthy in status
			m.mu.Lock()
			if status, ok := m.status[wan.Interface]; ok {
				status.Healthy = false
				status.Up = false
			}
			m.mu.Unlock()
			continue
		}
		activeWANs = append(activeWANs, wan)
	}

	if len(activeWANs) == 0 {
		logger.Warn("Multi-WAN: No valid WAN interfaces found, failover disabled")
		m.mu.Lock()
		m.running = false
		m.mu.Unlock()
		return nil
	}

	logger.Info("Multi-WAN failover starting with %d WANs", len(activeWANs))

	// Ensure all active WANs have DHCP running (for health monitoring)
	for i := range activeWANs {
		wan := &activeWANs[i]
		if wan.Mode == "dhcp" || wan.Mode == "wifi" {
			m.ensureDHCP(ctx, wan)
		}
	}

	// Initial check and activation
	m.checkAllWANs(ctx)
	m.selectActiveWAN(ctx)

	// Start monitoring loop
	go m.monitorLoop(ctx)

	return nil
}

// Stop stops the failover monitoring
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return
	}

	close(m.stopChan)
	m.running = false
	logger.Info("Multi-WAN failover stopped")
}

// GetStatus returns the current failover status
func (m *Manager) GetStatus() *FailoverStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.wans) == 0 {
		return &FailoverStatus{Enabled: false}
	}

	// Determine mode based on fuse_with_next flags
	mode := "failover"
	for _, wan := range m.wans {
		if wan.FuseWithNext {
			mode = "loadbalance (WIP)"
			break
		}
	}

	status := &FailoverStatus{
		Enabled:     len(m.wans) > 0,
		Mode:        mode,
		ActiveWAN:   m.activeWAN,
		LastSwitch:  m.lastSwitch,
		SwitchCount: m.switchCount,
	}

	// Get sorted WAN list
	var wans []WANStatus
	for _, ws := range m.status {
		wans = append(wans, *ws)
	}
	sort.Slice(wans, func(i, j int) bool {
		return wans[i].Priority < wans[j].Priority
	})
	status.WANs = wans

	return status
}

// monitorLoop continuously checks WAN health
func (m *Manager) monitorLoop(ctx context.Context) {
	// Get check interval from first enabled WAN, default 10s
	interval := 10 * time.Second
	m.mu.RLock()
	for _, wan := range m.wans {
		if wan.Enabled && wan.HealthCheckInterval > 0 {
			interval = time.Duration(wan.HealthCheckInterval) * time.Second
			break
		}
	}
	m.mu.RUnlock()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopChan:
			return
		case <-ticker.C:
			m.checkAllWANs(ctx)
			m.selectActiveWAN(ctx)
		}
	}
}

// checkAllWANs runs health checks on all configured WANs
func (m *Manager) checkAllWANs(ctx context.Context) {
	m.mu.RLock()
	wans := m.wans
	m.mu.RUnlock()

	for i := range wans {
		wan := &wans[i]
		if !wan.Enabled {
			continue
		}
		m.checkWAN(ctx, wan)
	}
}

// checkWAN checks the health of a single WAN
func (m *Manager) checkWAN(ctx context.Context, wan *config.WANConfig) {
	m.mu.Lock()
	status, ok := m.status[wan.Interface]
	if !ok {
		status = &WANStatus{
			Name:      wan.Name,
			Interface: wan.Interface,
			Priority:  wan.Priority,
			Enabled:   wan.Enabled,
		}
		m.status[wan.Interface] = status
	}
	m.mu.Unlock()

	// Skip if interface doesn't exist (may have been removed)
	if !m.interfaceExists(ctx, wan.Interface) {
		m.mu.Lock()
		if status.Up {
			logger.Warn("Multi-WAN: %s interface disappeared", wan.Interface)
		}
		status.Up = false
		status.Healthy = false
		status.IPAddress = ""
		status.Gateway = ""
		status.LastCheck = time.Now()
		m.mu.Unlock()
		return
	}

	// Check link state
	linkUp := m.checkLinkState(ctx, wan.Interface)

	// Get IP and gateway
	ip, gateway := m.getInterfaceInfo(ctx, wan)

	// Run health check (ping)
	healthy := false
	if linkUp && gateway != "" {
		healthy = m.runHealthCheck(ctx, wan, gateway)
	}

	// Update status
	m.mu.Lock()
	wasUp := status.Up
	wasHealthy := status.Healthy

	status.Up = linkUp
	status.IPAddress = ip
	status.Gateway = gateway
	status.LastCheck = time.Now()
	status.ChecksRun++

	retries := wan.HealthCheckRetries
	if retries <= 0 {
		retries = 3
	}

	if healthy {
		status.Healthy = true
		status.LastHealthy = time.Now()
		status.FailureCount = 0
		status.ChecksPassed++
	} else {
		status.FailureCount++
		if status.FailureCount >= retries {
			status.Healthy = false
		}
	}

	// Log state changes only
	if wasUp && !linkUp {
		logger.Warn("Multi-WAN: %s link DOWN", wan.Interface)
	} else if !wasUp && linkUp {
		logger.Info("Multi-WAN: %s link UP (ip=%s, gw=%s)", wan.Interface, ip, gateway)
	}

	if wasHealthy && !status.Healthy {
		logger.Warn("Multi-WAN: %s UNHEALTHY (failures=%d)", wan.Interface, status.FailureCount)
	} else if !wasHealthy && status.Healthy {
		logger.Info("Multi-WAN: %s HEALTHY", wan.Interface)
	}
	m.mu.Unlock()
}

// checkLinkState checks if the interface link is up
func (m *Manager) checkLinkState(ctx context.Context, iface string) bool {
	out, err := m.runner.Run(ctx, "ip", "link", "show", iface)
	if err != nil {
		return false
	}
	// Check for "state UP" - unplugged cables show "state DOWN" or "NO-CARRIER"
	return strings.Contains(string(out), "state UP")
}

// getInterfaceInfo gets IP and gateway for an interface
func (m *Manager) getInterfaceInfo(ctx context.Context, wan *config.WANConfig) (ip, gateway string) {
	iface := wan.Interface

	// Get IP
	out, err := m.runner.Run(ctx, "ip", "-4", "addr", "show", iface)
	if err == nil {
		re := regexp.MustCompile(`inet (\d+\.\d+\.\d+\.\d+)`)
		if matches := re.FindStringSubmatch(string(out)); len(matches) >= 2 {
			ip = matches[1]
		}
	}

	// 1. For static mode, use configured gateway
	if wan.Mode == "static" && wan.StaticGateway != "" {
		gateway = wan.StaticGateway
		return ip, gateway
	}

	// 2. Check routing table for this interface (works for active WAN)
	out, err = m.runner.Run(ctx, "ip", "route", "show", "dev", iface)
	if err == nil {
		// Look for default route or extract gateway from route
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "default via ") {
				parts := strings.Fields(line)
				if len(parts) >= 3 {
					gateway = parts[2]
					break
				}
			}
		}
		// If no default route, try to find any "via" gateway
		if gateway == "" {
			re := regexp.MustCompile(`via (\d+\.\d+\.\d+\.\d+)`)
			if matches := re.FindStringSubmatch(string(out)); len(matches) >= 2 {
				gateway = matches[1]
			}
		}
	}

	// 3. Check dhclient lease file for gateway (for DHCP/WiFi modes without default route)
	if gateway == "" {
		gateway = m.getGatewayFromDHCPLease(ctx, iface)
	}

	// 4. Fallback: derive from IP (x.x.x.1)
	if gateway == "" && ip != "" {
		parts := strings.Split(ip, ".")
		if len(parts) == 4 {
			gateway = fmt.Sprintf("%s.%s.%s.1", parts[0], parts[1], parts[2])
		}
	}

	return ip, gateway
}

// getGatewayFromDHCPLease parses dhclient lease file for gateway
func (m *Manager) getGatewayFromDHCPLease(ctx context.Context, iface string) string {
	// dhclient stores leases in /var/lib/dhcp/dhclient.<interface>.leases
	leasePaths := []string{
		fmt.Sprintf("/var/lib/dhcp/dhclient.%s.leases", iface),
		fmt.Sprintf("/var/lib/dhcp/dhclient-%s.leases", iface),
		"/var/lib/dhcp/dhclient.leases",
	}

	for _, leasePath := range leasePaths {
		data, err := m.fs.ReadFile(leasePath)
		if err != nil {
			continue
		}

		// Parse lease file looking for most recent "option routers" line
		// Format: option routers 192.168.1.1;
		lines := strings.Split(string(data), "\n")
		var lastGateway string
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "option routers ") {
				// Extract gateway IP
				line = strings.TrimPrefix(line, "option routers ")
				line = strings.TrimSuffix(line, ";")
				// May have multiple routers, take first one
				parts := strings.Split(line, ",")
				if len(parts) > 0 {
					gw := strings.TrimSpace(parts[0])
					if gw != "" {
						lastGateway = gw
					}
				}
			}
		}

		if lastGateway != "" {
			return lastGateway
		}
	}

	return ""
}

// runHealthCheck pings targets to verify connectivity
func (m *Manager) runHealthCheck(ctx context.Context, wan *config.WANConfig, gateway string) bool {
	timeout := wan.HealthCheckTimeout
	if timeout <= 0 {
		timeout = 5
	}

	// For non-active WANs, we can only reliably ping the gateway
	// (external targets like 8.8.8.8 require a default route)
	m.mu.RLock()
	isActiveWAN := m.activeWAN == wan.Interface
	m.mu.RUnlock()

	// Always try gateway first - this works for any WAN with a local route
	if gateway != "" {
		out, err := m.runner.Run(ctx, "ping", "-I", wan.Interface, "-c", "1", "-W", fmt.Sprintf("%d", timeout), gateway)
		if err == nil && strings.Contains(string(out), "1 received") {
			return true
		}
	}

	// For active WAN, also try external targets (they have the default route)
	if isActiveWAN {
		targets := wan.HealthCheckTargets
		if len(targets) == 0 {
			targets = []string{"8.8.8.8", "1.1.1.1"}
		}

		for _, target := range targets {
			if target == "" || target == gateway {
				continue
			}
			out, err := m.runner.Run(ctx, "ping", "-I", wan.Interface, "-c", "1", "-W", fmt.Sprintf("%d", timeout), target)
			if err == nil && strings.Contains(string(out), "1 received") {
				return true
			}
		}
	}

	return false
}

// selectActiveWAN selects the best available WAN and activates it
func (m *Manager) selectActiveWAN(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.wans) == 0 {
		return
	}

	// Sort WANs by priority
	var healthyWANs []*WANStatus
	for _, ws := range m.status {
		if ws.Enabled && ws.Healthy && ws.Up {
			healthyWANs = append(healthyWANs, ws)
		}
	}

	sort.Slice(healthyWANs, func(i, j int) bool {
		return healthyWANs[i].Priority < healthyWANs[j].Priority
	})

	if len(healthyWANs) == 0 {
		if m.hadHealthyWANs {
			logger.Warn("Multi-WAN: No healthy WANs available!")
			m.hadHealthyWANs = false
		}
		return
	}

	if !m.hadHealthyWANs {
		m.hadHealthyWANs = true
	}

	bestWAN := healthyWANs[0]

	// Check if we need to switch
	if m.activeWAN == bestWAN.Interface {
		return
	}

	// Check failback delay - don't switch back to primary too quickly
	if m.activeWAN != "" && bestWAN.Priority < m.status[m.activeWAN].Priority {
		if time.Since(m.lastSwitch) < time.Duration(m.failbackDelay)*time.Second {
			return // Waiting for failback delay
		}
	}

	// Perform the switch
	oldWAN := m.activeWAN
	m.activeWAN = bestWAN.Interface

	// Update active status
	for _, ws := range m.status {
		ws.Active = (ws.Interface == m.activeWAN)
	}

	m.lastSwitch = time.Now()
	m.switchCount++

	if oldWAN == "" {
		logger.Info("Multi-WAN: Setting initial WAN to %s (priority %d)", bestWAN.Interface, bestWAN.Priority)
	} else {
		logger.Info("Multi-WAN: Switching from %s to %s (priority %d)", oldWAN, bestWAN.Interface, bestWAN.Priority)
	}

	// Get WAN config for the new active WAN
	var activeWANConfig *config.WANConfig
	for i := range m.wans {
		if m.wans[i].Interface == bestWAN.Interface {
			activeWANConfig = &m.wans[i]
			break
		}
	}

	// Apply the route change (unlock during route change to avoid deadlock)
	m.mu.Unlock()

	// Ensure DHCP is fresh on the new active WAN
	if activeWANConfig != nil && (activeWANConfig.Mode == "dhcp" || activeWANConfig.Mode == "wifi") {
		m.ensureDHCP(ctx, activeWANConfig)
	}

	m.applyRouteChange(ctx, bestWAN.Interface, bestWAN.Gateway)
	m.mu.Lock()
}

// applyRouteChange updates the default route to use the new WAN
func (m *Manager) applyRouteChange(ctx context.Context, iface, gateway string) {
	if gateway == "" {
		logger.Error("Multi-WAN: Cannot apply route change - no gateway for %s", iface)
		return
	}

	// Remove existing default routes
	m.runner.Run(ctx, "ip", "route", "del", "default")

	// Add new default route
	out, err := m.runner.Run(ctx, "ip", "route", "add", "default", "via", gateway, "dev", iface)
	if err != nil {
		logger.Error("Multi-WAN: Failed to add default route: %s", string(out))
		return
	}

	logger.Info("Multi-WAN: Default route updated to %s via %s", iface, gateway)

	// Update NAT rules if needed
	m.updateNATRules(ctx, iface)
}

// updateNATRules updates iptables NAT rules for the new WAN
func (m *Manager) updateNATRules(ctx context.Context, iface string) {
	// Remove old MASQUERADE rules for all WAN interfaces
	m.mu.RLock()
	for wanIface := range m.status {
		m.runner.Run(ctx, "iptables", "-t", "nat", "-D", "POSTROUTING", "-o", wanIface, "-j", "MASQUERADE")
	}
	m.mu.RUnlock()

	// Add MASQUERADE for new active WAN
	out, err := m.runner.Run(ctx, "iptables", "-t", "nat", "-A", "POSTROUTING", "-o", iface, "-j", "MASQUERADE")
	if err != nil {
		logger.Error("Multi-WAN: Failed to add NAT rule: %s", string(out))
	}
}

// ForceSwitch forces a switch to a specific WAN (for manual override)
func (m *Manager) ForceSwitch(ctx context.Context, iface string) error {
	m.mu.Lock()
	status, ok := m.status[iface]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("unknown WAN interface: %s", iface)
	}
	if !status.Enabled {
		m.mu.Unlock()
		return fmt.Errorf("WAN interface %s is not enabled", iface)
	}

	oldWAN := m.activeWAN
	m.activeWAN = iface

	for _, ws := range m.status {
		ws.Active = (ws.Interface == m.activeWAN)
	}

	m.lastSwitch = time.Now()
	m.switchCount++
	gateway := status.Gateway
	m.mu.Unlock()

	if oldWAN == "" {
		logger.Info("Multi-WAN: Manual switch to %s", iface)
	} else {
		logger.Info("Multi-WAN: Manual switch from %s to %s", oldWAN, iface)
	}

	m.applyRouteChange(ctx, iface, gateway)
	return nil
}

// GetActiveWAN returns the currently active WAN interface
func (m *Manager) GetActiveWAN() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeWAN
}

// IsEnabled returns whether multi-WAN is enabled (any WANs configured)
func (m *Manager) IsEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.wans) > 0
}

// ensureDHCP ensures an interface has a DHCP lease for health monitoring
func (m *Manager) ensureDHCP(ctx context.Context, wan *config.WANConfig) {
	iface := wan.Interface

	// Check if interface exists first
	if !m.interfaceExists(ctx, iface) {
		logger.Debug("Multi-WAN: %s does not exist, skipping DHCP", iface)
		return
	}

	// Check if interface already has an IP
	out, err := m.runner.Run(ctx, "ip", "-4", "addr", "show", iface)
	if err == nil && strings.Contains(string(out), "inet ") {
		logger.Debug("Multi-WAN: %s already has IP, skipping DHCP", iface)
		return
	}

	logger.Info("Multi-WAN: Requesting DHCP for %s", iface)

	// Try dhcpcd first (more common on modern systems), then dhclient
	if _, err := m.runner.Run(ctx, "which", "dhcpcd"); err == nil {
		out, err := m.runner.Run(ctx, "dhcpcd", "-4", "-b", iface)
		if err != nil {
			logger.Warn("Multi-WAN: dhcpcd failed for %s: %s", iface, string(out))
		} else {
			logger.Info("Multi-WAN: DHCP started for %s via dhcpcd", iface)
			return
		}
	}

	if _, err := m.runner.Run(ctx, "which", "dhclient"); err == nil {
		out, err := m.runner.Run(ctx, "dhclient", "-4", iface)
		if err != nil {
			logger.Warn("Multi-WAN: dhclient failed for %s: %s", iface, string(out))
		} else {
			logger.Info("Multi-WAN: DHCP started for %s via dhclient", iface)
			return
		}
	}

	logger.Error("Multi-WAN: No DHCP client available for %s", iface)
}
