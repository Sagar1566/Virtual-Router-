package wifi

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iodesystems/ubuntu-router/internal/config"
	"github.com/iodesystems/ubuntu-router/internal/logger"
	"github.com/iodesystems/ubuntu-router/internal/system"
)

// Station represents a connected WiFi client
type Station struct {
	MAC           string `json:"mac"`
	Interface     string `json:"interface"`
	Signal        int    `json:"signal"`  // dBm
	RxRate        int    `json:"rx_rate"` // Mbps
	TxRate        int    `json:"tx_rate"` // Mbps
	RxBytes       int64  `json:"rx_bytes"`
	TxBytes       int64  `json:"tx_bytes"`
	Connected     string `json:"connected"` // Duration
	Authorized    bool   `json:"authorized"`
	Authenticated bool   `json:"authenticated"`
}

// InterfaceStatus represents the status of a WiFi interface
type InterfaceStatus struct {
	Interface    string    `json:"interface"`
	Mode         string    `json:"mode"` // ap, managed, etc.
	SSID         string    `json:"ssid"`
	Channel      int       `json:"channel"`
	Frequency    int       `json:"frequency"`    // MHz
	TxPower      int       `json:"txPower"`      // dBm
	Stations     []Station `json:"stations"`
	StationCount int       `json:"stationCount"`
	Running      bool      `json:"running"`
}

// Manager handles WiFi/hostapd configuration and operations
type Manager struct {
	mu       sync.RWMutex
	fs       system.FileSystem
	runner   system.CommandRunner
	services *system.ServiceManager
}

// New creates a new WiFi manager
func New(fs system.FileSystem, runner system.CommandRunner) *Manager {
	return &Manager{
		fs:       fs,
		runner:   runner,
		services: system.NewServiceManager(runner),
	}
}

// WriteHostapdConfig generates and writes hostapd configuration for an interface
func (m *Manager) WriteHostapdConfig(wifi *config.WiFiInterface) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	configPath := fmt.Sprintf("/etc/hostapd/hostapd-%s.conf", wifi.Interface)

	// Ensure directory exists
	if err := m.fs.MkdirAll("/etc/hostapd", 0755); err != nil {
		return fmt.Errorf("failed to create hostapd directory: %w", err)
	}

	var sb strings.Builder

	// Basic settings
	sb.WriteString("# Ubuntu Router hostapd configuration\n")
	sb.WriteString("# Auto-generated - do not edit\n\n")

	sb.WriteString(fmt.Sprintf("interface=%s\n", wifi.Interface))

	// Bridge mode
	if wifi.Mode == "bridge" && wifi.Bridge != "" {
		sb.WriteString(fmt.Sprintf("bridge=%s\n", wifi.Bridge))
	}

	// Driver (nl80211 for modern cards)
	sb.WriteString("driver=nl80211\n")

	// SSID
	sb.WriteString(fmt.Sprintf("ssid=%s\n", wifi.SSID))

	// Hidden SSID
	if wifi.Hidden {
		sb.WriteString("ignore_broadcast_ssid=1\n")
	} else {
		sb.WriteString("ignore_broadcast_ssid=0\n")
	}

	// Channel and band
	if wifi.AutoChannel {
		// ACS (Automatic Channel Selection) - hostapd picks the best channel
		sb.WriteString("channel=0\n")
		sb.WriteString("# ACS (Automatic Channel Selection) settings\n")
		sb.WriteString("acs_num_scans=5\n")
		sb.WriteString("acs_exclude_dfs=1\n")
	} else {
		sb.WriteString(fmt.Sprintf("channel=%d\n", wifi.Channel))
	}

	// Hardware mode based on band
	switch wifi.Band {
	case "5ghz":
		sb.WriteString("hw_mode=a\n")
		// 802.11ac/ax settings
		sb.WriteString("ieee80211n=1\n")
		sb.WriteString("ieee80211ac=1\n")
		sb.WriteString("wmm_enabled=1\n")
	default: // 2.4ghz
		sb.WriteString("hw_mode=g\n")
		sb.WriteString("ieee80211n=1\n")
		sb.WriteString("wmm_enabled=1\n")
	}

	// Country code (required for proper channel operation)
	sb.WriteString("country_code=US\n")
	sb.WriteString("ieee80211d=1\n")

	// Security settings
	if wifi.Password != "" && len(wifi.Password) >= 8 {
		sb.WriteString("\n# Security settings\n")
		sb.WriteString("wpa=2\n")
		sb.WriteString(fmt.Sprintf("wpa_passphrase=%s\n", wifi.Password))
		sb.WriteString("wpa_key_mgmt=WPA-PSK\n")
		sb.WriteString("rsn_pairwise=CCMP\n")
	} else {
		// Open network
		sb.WriteString("auth_algs=1\n")
	}

	// Performance tuning
	sb.WriteString("\n# Performance\n")
	sb.WriteString("max_num_sta=32\n")

	return m.fs.WriteFile(configPath, []byte(sb.String()), 0600)
}

// findHostapdPath returns the path to the hostapd binary
func (m *Manager) findHostapdPath(ctx context.Context) string {
	// Try which command first (most reliable)
	if out, err := m.runner.Run(ctx, "which", "hostapd"); err == nil {
		path := strings.TrimSpace(string(out))
		if path != "" {
			logger.Info("Found hostapd via which: %s", path)
			return path
		}
	}

	// Try common locations
	paths := []string{
		"/usr/sbin/hostapd",
		"/usr/bin/hostapd",
		"/sbin/hostapd",
		"/bin/hostapd",
	}

	for _, p := range paths {
		if _, err := m.fs.Stat(p); err == nil {
			logger.Info("Found hostapd at: %s", p)
			return p
		}
	}

	logger.Warn("hostapd not found in any known location, defaulting to /usr/sbin/hostapd")
	return "/usr/sbin/hostapd"
}

// WriteSystemdService creates a systemd service for hostapd
func (m *Manager) WriteSystemdService(iface string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	configPath := fmt.Sprintf("/etc/hostapd/hostapd-%s.conf", iface)
	servicePath := fmt.Sprintf("/etc/systemd/system/hostapd-%s.service", iface)

	// Ensure directory exists
	if err := m.fs.MkdirAll("/etc/systemd/system", 0755); err != nil {
		return fmt.Errorf("failed to create systemd directory: %w", err)
	}

	// Find hostapd binary path
	hostapdPath := m.findHostapdPath(context.Background())
	logger.Info("WriteSystemdService: using hostapd path: %s", hostapdPath)

	service := fmt.Sprintf(`[Unit]
Description=Hostapd Access Point for %s
After=network.target

[Service]
Type=simple
ExecStart=%s %s
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
`, iface, hostapdPath, configPath)

	logger.Info("WriteSystemdService: writing service file to %s", servicePath)
	err := m.fs.WriteFile(servicePath, []byte(service), 0644)
	if err != nil {
		logger.Error("WriteSystemdService: failed to write service file: %v", err)
	}
	return err
}

// serviceName returns the systemd service name for an interface
func serviceName(iface string) string {
	return "hostapd-" + iface
}

// Start starts hostapd for an interface
func (m *Manager) Start(ctx context.Context, iface string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	svcName := serviceName(iface)
	logger.Info("WiFi Start: interface=%s service=%s", iface, svcName)

	// Check if config file exists
	configPath := fmt.Sprintf("/etc/hostapd/hostapd-%s.conf", iface)
	if _, err := m.fs.Stat(configPath); err != nil {
		logger.Error("WiFi Start: config file missing: %s", configPath)
		return fmt.Errorf("hostapd config not found: %s - run configure first", configPath)
	}
	logger.Info("WiFi Start: config file exists: %s", configPath)

	// Check if service file exists
	servicePath := fmt.Sprintf("/etc/systemd/system/%s.service", svcName)
	if _, err := m.fs.Stat(servicePath); err != nil {
		logger.Error("WiFi Start: service file missing: %s", servicePath)
		return fmt.Errorf("systemd service not found: %s - run configure first", servicePath)
	}
	logger.Info("WiFi Start: service file exists: %s", servicePath)

	// Reload systemd to pick up any changes
	out, err := m.runner.Run(ctx, "systemctl", "daemon-reload")
	logger.Info("WiFi Start: daemon-reload output: %s", string(out))

	// Ensure the interface is up
	out, err = m.runner.Run(ctx, "ip", "link", "set", iface, "up")
	if err != nil {
		logger.Warn("WiFi Start: ip link set up failed: %v, output: %s", err, string(out))
	} else {
		logger.Info("WiFi Start: interface %s is up", iface)
	}

	// Check interface state
	out, _ = m.runner.Run(ctx, "ip", "link", "show", iface)
	logger.Info("WiFi Start: interface state: %s", string(out))

	// Start the service
	err = m.services.Start(ctx, svcName)
	if err != nil {
		logger.Error("WiFi Start: service start failed: %v", err)
		return err
	}

	// Wait for service to become active (up to 5 seconds)
	logger.Info("WiFi Start: waiting for service to become active...")
	var isActive bool
	var status string
	for i := 0; i < 10; i++ {
		status, _ = m.services.Status(ctx, svcName)
		logger.Info("WiFi Start: status check %d/10: %s", i+1, status)

		if status == "active" {
			isActive = true
			break
		}
		if status == "failed" || status == "inactive" {
			// Service definitively failed
			break
		}
		// Wait 500ms before next check
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}

	if !isActive {
		// Get journal logs for more info
		journalOut, _ := m.runner.Run(ctx, "journalctl", "-u", svcName, "-n", "30", "--no-pager")
		logger.Error("WiFi Start: service not active, status: %s\nJournal:\n%s", status, string(journalOut))
		return fmt.Errorf("service failed to start, status: %s - check logs for details", status)
	}

	logger.Info("WiFi Start: successfully started %s", iface)
	return nil
}

// Stop stops hostapd for an interface
func (m *Manager) Stop(ctx context.Context, iface string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	svcName := serviceName(iface)
	logger.Info("WiFi Stop: interface=%s service=%s", iface, svcName)

	err := m.services.Stop(ctx, svcName)
	if err != nil {
		logger.Error("WiFi Stop: failed: %v", err)
	} else {
		logger.Info("WiFi Stop: success")
	}
	return err
}

// Restart restarts hostapd for an interface
func (m *Manager) Restart(ctx context.Context, iface string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	svcName := serviceName(iface)
	logger.Info("WiFi Restart: interface=%s service=%s", iface, svcName)

	err := m.services.Restart(ctx, svcName)
	if err != nil {
		logger.Error("WiFi Restart: failed: %v", err)
	} else {
		logger.Info("WiFi Restart: success")
	}
	return err
}

// Enable enables hostapd at boot for an interface
func (m *Manager) Enable(ctx context.Context, iface string) error {
	return m.services.Enable(ctx, serviceName(iface))
}

// Disable disables hostapd at boot for an interface
func (m *Manager) Disable(ctx context.Context, iface string) error {
	return m.services.Disable(ctx, serviceName(iface))
}

// IsRunning checks if hostapd is running for an interface
func (m *Manager) IsRunning(ctx context.Context, iface string) bool {
	return m.services.IsActive(ctx, serviceName(iface))
}

// GetInterfaceStatus returns the status of a WiFi interface
func (m *Manager) GetInterfaceStatus(ctx context.Context, iface string) (*InterfaceStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := &InterfaceStatus{
		Interface: iface,
		Running:   m.services.IsActive(ctx, serviceName(iface)),
	}

	// Get interface info using iw
	out, err := m.runner.Run(ctx, "iw", "dev", iface, "info")
	if err == nil {
		parseIwInfo(string(out), status)
	}

	// Get connected stations
	stationsOut, err := m.runner.Run(ctx, "iw", "dev", iface, "station", "dump")
	if err == nil {
		status.Stations = parseStationDump(iface, string(stationsOut))
		status.StationCount = len(status.Stations)
	}

	return status, nil
}

func parseIwInfo(output string, status *InterfaceStatus) {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "type ") {
			status.Mode = strings.TrimPrefix(line, "type ")
		}
		if strings.HasPrefix(line, "ssid ") {
			status.SSID = strings.TrimPrefix(line, "ssid ")
		}
		if strings.HasPrefix(line, "channel ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				status.Channel, _ = strconv.Atoi(parts[1])
			}
			// Extract frequency
			freqRe := regexp.MustCompile(`\((\d+)\s*MHz\)`)
			if matches := freqRe.FindStringSubmatch(line); len(matches) >= 2 {
				status.Frequency, _ = strconv.Atoi(matches[1])
			}
		}
		if strings.HasPrefix(line, "txpower ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				f, _ := strconv.ParseFloat(parts[1], 64)
				status.TxPower = int(f)
			}
		}
	}
}

func parseStationDump(iface, output string) []Station {
	var stations []Station
	var current *Station

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "Station ") {
			if current != nil {
				stations = append(stations, *current)
			}
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				current = &Station{
					MAC:       parts[1],
					Interface: iface,
				}
			}
			continue
		}

		if current == nil {
			continue
		}

		// Parse station properties
		if strings.HasPrefix(line, "signal:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				current.Signal, _ = strconv.Atoi(parts[1])
			}
		}
		if strings.HasPrefix(line, "rx bitrate:") {
			re := regexp.MustCompile(`(\d+\.?\d*)\s*MBit`)
			if matches := re.FindStringSubmatch(line); len(matches) >= 2 {
				f, _ := strconv.ParseFloat(matches[1], 64)
				current.RxRate = int(f)
			}
		}
		if strings.HasPrefix(line, "tx bitrate:") {
			re := regexp.MustCompile(`(\d+\.?\d*)\s*MBit`)
			if matches := re.FindStringSubmatch(line); len(matches) >= 2 {
				f, _ := strconv.ParseFloat(matches[1], 64)
				current.TxRate = int(f)
			}
		}
		if strings.HasPrefix(line, "rx bytes:") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				current.RxBytes, _ = strconv.ParseInt(parts[2], 10, 64)
			}
		}
		if strings.HasPrefix(line, "tx bytes:") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				current.TxBytes, _ = strconv.ParseInt(parts[2], 10, 64)
			}
		}
		if strings.HasPrefix(line, "connected time:") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				current.Connected = parts[2] + " seconds"
			}
		}
		if strings.HasPrefix(line, "authorized:") {
			current.Authorized = strings.Contains(line, "yes")
		}
		if strings.HasPrefix(line, "authenticated:") {
			current.Authenticated = strings.Contains(line, "yes")
		}
	}

	if current != nil {
		stations = append(stations, *current)
	}

	return stations
}

// ListWiFiInterfaces returns all WiFi-capable interfaces
func (m *Manager) ListWiFiInterfaces(ctx context.Context) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Try iw dev first
	out, err := m.runner.Run(ctx, "iw", "dev")
	if err == nil {
		var interfaces []string
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Interface ") {
				iface := strings.TrimPrefix(line, "Interface ")
				interfaces = append(interfaces, iface)
			}
		}
		if len(interfaces) > 0 {
			return interfaces, nil
		}
	}

	// Fallback: scan /sys/class/net for wireless interfaces
	return m.listWiFiInterfacesFromSysfs()
}

// listWiFiInterfacesFromSysfs detects WiFi interfaces by checking /sys/class/net
func (m *Manager) listWiFiInterfacesFromSysfs() ([]string, error) {
	entries, err := m.fs.ReadDir("/sys/class/net")
	if err != nil {
		return nil, fmt.Errorf("failed to read /sys/class/net: %w", err)
	}

	var interfaces []string
	for _, entry := range entries {
		name := entry.Name()
		// Check for wireless indicator files
		wirelessPath := fmt.Sprintf("/sys/class/net/%s/wireless", name)
		phy80211Path := fmt.Sprintf("/sys/class/net/%s/phy80211", name)

		if _, err := m.fs.Stat(wirelessPath); err == nil {
			interfaces = append(interfaces, name)
		} else if _, err := m.fs.Stat(phy80211Path); err == nil {
			interfaces = append(interfaces, name)
		}
	}

	return interfaces, nil
}

// PHYInfo contains information about a WiFi PHY
type PHYInfo struct {
	Name       string   `json:"name"`
	Bands      []string `json:"bands"`
	Channels   []int    `json:"channels"`
	HTCapable  bool     `json:"ht_capable"`
	VHTCapable bool     `json:"vht_capable"`
	HECapable  bool     `json:"he_capable"`
}

// WiFiCard represents a detected WiFi card with its capabilities
type WiFiCard struct {
	Interface       string   `json:"interface"`
	PHY             string   `json:"phy"`
	Driver          string   `json:"driver"`
	MAC             string   `json:"mac"`
	SupportsAP      bool     `json:"supports_ap"`
	SupportsMesh    bool     `json:"supports_mesh"`
	SupportsIBSS    bool     `json:"supports_ibss"`
	SupportsMonitor bool     `json:"supports_monitor"`
	Bands           []string `json:"bands"`
	Channels2GHz    []int    `json:"channels_2ghz"`
	Channels5GHz    []int    `json:"channels_5ghz"`
	HTCapable       bool     `json:"ht_capable"`
	VHTCapable      bool     `json:"vht_capable"`
	HECapable       bool     `json:"he_capable"`
	CurrentMode     string   `json:"current_mode,omitempty"`
	InUse           bool     `json:"in_use"`
}

// GetPHYInfo returns info about the PHY for an interface
func (m *Manager) GetPHYInfo(ctx context.Context, iface string) (*PHYInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Get PHY name
	infoOut, err := m.runner.Run(ctx, "iw", "dev", iface, "info")
	if err != nil {
		return nil, err
	}

	phyName := ""
	for _, line := range strings.Split(string(infoOut), "\n") {
		if strings.Contains(line, "wiphy") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				phyName = "phy" + parts[1]
			}
		}
	}

	if phyName == "" {
		return nil, fmt.Errorf("could not determine PHY for interface %s", iface)
	}

	info := &PHYInfo{Name: phyName}

	// Get PHY capabilities
	phyOut, err := m.runner.Run(ctx, "iw", phyName, "info")
	if err != nil {
		return info, nil
	}

	phyStr := string(phyOut)

	// Check for capabilities
	if strings.Contains(phyStr, "HT") {
		info.HTCapable = true
	}
	if strings.Contains(phyStr, "VHT") {
		info.VHTCapable = true
	}
	if strings.Contains(phyStr, "HE ") {
		info.HECapable = true
	}

	// Parse bands and channels
	currentBand := ""
	for _, line := range strings.Split(phyStr, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Band ") {
			currentBand = line
			info.Bands = append(info.Bands, currentBand)
		}
		if strings.HasPrefix(line, "* ") && strings.Contains(line, "MHz") {
			chanRe := regexp.MustCompile(`\[(\d+)\]`)
			if matches := chanRe.FindStringSubmatch(line); len(matches) >= 2 {
				ch, _ := strconv.Atoi(matches[1])
				info.Channels = append(info.Channels, ch)
			}
		}
	}

	return info, nil
}

// SetChannel sets the channel for an interface (requires hostapd restart)
func (m *Manager) SetChannel(ctx context.Context, iface string, channel int, cfg *config.WiFiInterface) error {
	cfg.Channel = channel

	if err := m.WriteHostapdConfig(cfg); err != nil {
		return err
	}

	return m.Restart(ctx, iface)
}

// SetTxPower sets the transmit power for an interface
func (m *Manager) SetTxPower(ctx context.Context, iface string, power int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.runner.Run(ctx, "iw", "dev", iface, "set", "txpower", "fixed", strconv.Itoa(power*100))
	return err
}

// DisconnectStation disconnects a station from the AP
func (m *Manager) DisconnectStation(ctx context.Context, iface, mac string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.runner.Run(ctx, "hostapd_cli", "-i", iface, "disassociate", mac)
	return err
}

// Status represents overall WiFi status
type Status struct {
	Interfaces []InterfaceStatus `json:"interfaces"`
}

// GetStatus returns status of all configured WiFi interfaces
func (m *Manager) GetStatus(ctx context.Context, wifiConfigs []config.WiFiInterface) (*Status, error) {
	status := &Status{}

	for _, wifi := range wifiConfigs {
		if ifaceStatus, err := m.GetInterfaceStatus(ctx, wifi.Interface); err == nil {
			status.Interfaces = append(status.Interfaces, *ifaceStatus)
		}
	}

	return status, nil
}

// ConfigureInterface configures and starts a WiFi interface
func (m *Manager) ConfigureInterface(ctx context.Context, wifi *config.WiFiInterface) error {
	// Write hostapd config
	if err := m.WriteHostapdConfig(wifi); err != nil {
		return fmt.Errorf("failed to write hostapd config: %w", err)
	}

	// Write systemd service
	if err := m.WriteSystemdService(wifi.Interface); err != nil {
		return fmt.Errorf("failed to write systemd service: %w", err)
	}

	// Reload systemd
	m.runner.Run(ctx, "systemctl", "daemon-reload")

	// Enable and start if configured
	if wifi.Enabled {
		if err := m.Enable(ctx, wifi.Interface); err != nil {
			return fmt.Errorf("failed to enable hostapd: %w", err)
		}
		if err := m.Start(ctx, wifi.Interface); err != nil {
			return fmt.Errorf("failed to start hostapd: %w", err)
		}
	}

	return nil
}

// GetStations returns connected stations for an interface
func (m *Manager) GetStations(ctx context.Context, iface string) ([]Station, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out, err := m.runner.Run(ctx, "iw", "dev", iface, "station", "dump")
	if err != nil {
		return nil, err
	}

	return parseStationDump(iface, string(out)), nil
}

// DeconfigureInterface stops and removes a WiFi interface configuration
func (m *Manager) DeconfigureInterface(ctx context.Context, iface string) error {
	m.Stop(ctx, iface)
	m.Disable(ctx, iface)

	// Remove config files
	m.fs.Remove(fmt.Sprintf("/etc/hostapd/hostapd-%s.conf", iface))
	m.fs.Remove(fmt.Sprintf("/etc/systemd/system/hostapd-%s.service", iface))

	m.runner.Run(ctx, "systemctl", "daemon-reload")

	return nil
}

// DetectAPCapableCards detects WiFi cards that support access point mode
func (m *Manager) DetectAPCapableCards(ctx context.Context) ([]WiFiCard, error) {
	cards, err := m.getAllWiFiCardsInternal(ctx)
	if err != nil {
		return nil, err
	}

	// Filter to only AP-capable cards
	var apCards []WiFiCard
	for _, card := range cards {
		if card.SupportsAP {
			apCards = append(apCards, card)
		}
	}

	return apCards, nil
}

// parsePhyCapabilities parses iw phy info output to extract capabilities
func (card *WiFiCard) parsePhyCapabilities(phyStr string) {
	lines := strings.Split(phyStr, "\n")
	inModes := false
	inBand := ""

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Parse supported interface modes
		if strings.Contains(line, "Supported interface modes:") {
			inModes = true
			continue
		}
		if inModes {
			if strings.HasPrefix(line, "* ") {
				mode := strings.TrimPrefix(line, "* ")
				switch mode {
				case "AP":
					card.SupportsAP = true
				case "mesh point":
					card.SupportsMesh = true
				case "IBSS":
					card.SupportsIBSS = true
				case "monitor":
					card.SupportsMonitor = true
				}
			} else if !strings.HasPrefix(line, "*") && line != "" {
				inModes = false
			}
		}

		// Parse bands
		if strings.HasPrefix(line, "Band ") {
			inBand = line
			card.Bands = append(card.Bands, inBand)
		}

		// Parse channels
		if strings.HasPrefix(line, "* ") && strings.Contains(line, "MHz") {
			chanRe := regexp.MustCompile(`\[(\d+)\]`)
			freqRe := regexp.MustCompile(`(\d+)\s*MHz`)
			if matches := chanRe.FindStringSubmatch(line); len(matches) >= 2 {
				ch, _ := strconv.Atoi(matches[1])
				// Determine band from frequency
				if freqMatches := freqRe.FindStringSubmatch(line); len(freqMatches) >= 2 {
					freq, _ := strconv.Atoi(freqMatches[1])
					if freq < 3000 {
						card.Channels2GHz = append(card.Channels2GHz, ch)
					} else {
						card.Channels5GHz = append(card.Channels5GHz, ch)
					}
				}
			}
		}

		// Parse capabilities
		if strings.Contains(line, "HT") && !strings.Contains(line, "VHT") {
			card.HTCapable = true
		}
		if strings.Contains(line, "VHT") {
			card.VHTCapable = true
		}
		if strings.Contains(line, "HE ") || strings.Contains(line, "HE:") {
			card.HECapable = true
		}
	}
}

// GetAllWiFiCards returns all WiFi cards regardless of AP support
func (m *Manager) GetAllWiFiCards(ctx context.Context) ([]WiFiCard, error) {
	return m.getAllWiFiCardsInternal(ctx)
}

// getAllWiFiCardsInternal is the internal implementation that detects all WiFi cards
func (m *Manager) getAllWiFiCardsInternal(ctx context.Context) ([]WiFiCard, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var cards []WiFiCard

	// Try iw dev first to get interface info with PHY mapping
	type ifaceInfo struct {
		name   string
		phyNum string
		mode   string
		mac    string
	}
	var ifaces []ifaceInfo

	devOut, err := m.runner.Run(ctx, "iw", "dev")
	if err == nil {
		var current *ifaceInfo
		for _, line := range strings.Split(string(devOut), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "phy#") {
				if current != nil && current.name != "" {
					ifaces = append(ifaces, *current)
				}
				current = &ifaceInfo{
					phyNum: strings.TrimPrefix(line, "phy#"),
				}
			}
			if current != nil {
				if strings.HasPrefix(line, "Interface ") {
					current.name = strings.TrimPrefix(line, "Interface ")
				}
				if strings.HasPrefix(line, "type ") {
					current.mode = strings.TrimPrefix(line, "type ")
				}
				if strings.HasPrefix(line, "addr ") {
					current.mac = strings.TrimPrefix(line, "addr ")
				}
			}
		}
		if current != nil && current.name != "" {
			ifaces = append(ifaces, *current)
		}
	}

	// If iw dev didn't find anything, fallback to sysfs detection
	if len(ifaces) == 0 {
		sysfsIfaces, err := m.listWiFiInterfacesFromSysfs()
		if err != nil {
			return nil, err
		}
		for _, name := range sysfsIfaces {
			info := ifaceInfo{name: name}

			// Try to get MAC from sysfs
			if data, err := m.fs.ReadFile(fmt.Sprintf("/sys/class/net/%s/address", name)); err == nil {
				info.mac = strings.TrimSpace(string(data))
			}

			// Try to get PHY from sysfs symlink
			if target, err := m.fs.ReadLink(fmt.Sprintf("/sys/class/net/%s/phy80211", name)); err == nil {
				info.phyNum = strings.TrimPrefix(filepath.Base(target), "phy")
			}

			ifaces = append(ifaces, info)
		}
	}

	// Build card info for each interface
	for _, iface := range ifaces {
		card := WiFiCard{
			Interface:   iface.name,
			MAC:         iface.mac,
			CurrentMode: iface.mode,
			InUse:       m.services.IsActive(ctx, serviceName(iface.name)),
		}

		if iface.phyNum != "" {
			card.PHY = "phy" + iface.phyNum
		}

		// Get driver info from sysfs
		driverLink := fmt.Sprintf("/sys/class/net/%s/device/driver", iface.name)
		if target, err := m.fs.ReadLink(driverLink); err == nil {
			parts := strings.Split(target, "/")
			card.Driver = parts[len(parts)-1]
		}

		// Get PHY capabilities if we have a PHY name and iw is available
		if card.PHY != "" {
			phyOut, err := m.runner.Run(ctx, "iw", card.PHY, "info")
			if err == nil {
				card.parsePhyCapabilities(string(phyOut))
			}
		}

		// If we couldn't get capabilities from iw, try to detect from sysfs
		if !card.SupportsAP && card.PHY == "" {
			// Most modern WiFi cards support AP mode, mark as unknown capability
			// The card will still show up but SupportsAP will be false
		}

		cards = append(cards, card)
	}

	return cards, nil
}
