package wifi

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ScannedNetwork represents a WiFi network found during scanning
type ScannedNetwork struct {
	SSID       string `json:"ssid"`
	BSSID      string `json:"bssid"`
	Signal     int    `json:"signal"`      // dBm
	Quality    int    `json:"quality"`     // 0-100%
	Frequency  int    `json:"frequency"`   // MHz
	Channel    int    `json:"channel"`
	Security   string `json:"security"`    // open, wep, wpa, wpa2, wpa3
	Band       string `json:"band"`        // 2.4ghz, 5ghz
	Connected  bool   `json:"connected"`   // Currently connected to this network
}

// ClientStatus represents the WiFi client connection status
type ClientStatus struct {
	Connected  bool   `json:"connected"`
	SSID       string `json:"ssid,omitempty"`
	BSSID      string `json:"bssid,omitempty"`
	Signal     int    `json:"signal,omitempty"`     // dBm
	Frequency  int    `json:"frequency,omitempty"`  // MHz
	IPAddress  string `json:"ip_address,omitempty"`
	Gateway    string `json:"gateway,omitempty"`
}

// ScanNetworks scans for available WiFi networks on an interface
func (m *Manager) ScanNetworks(ctx context.Context, iface string) ([]ScannedNetwork, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Ensure interface is up
	if _, err := m.runner.Run(ctx, "ip", "link", "set", iface, "up"); err != nil {
		return nil, fmt.Errorf("failed to bring up interface: %w", err)
	}

	// Small delay to let interface come up
	time.Sleep(100 * time.Millisecond)

	// Trigger scan
	if _, err := m.runner.Run(ctx, "iw", "dev", iface, "scan", "trigger"); err != nil {
		// Scan trigger may fail if already scanning, try dump anyway
	}

	// Wait for scan to complete
	time.Sleep(2 * time.Second)

	// Get scan results
	out, err := m.runner.Run(ctx, "iw", "dev", iface, "scan", "dump")
	if err != nil {
		// Fallback: try direct scan (may work better on some systems)
		out, err = m.runner.Run(ctx, "iw", "dev", iface, "scan")
		if err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}
	}

	networks := parseIwScan(string(out))

	// Check current connection status
	connectedSSID := m.getConnectedSSID(ctx, iface)
	for i := range networks {
		if networks[i].SSID == connectedSSID && connectedSSID != "" {
			networks[i].Connected = true
		}
	}

	return networks, nil
}

// parseIwScan parses output from "iw dev <iface> scan"
func parseIwScan(output string) []ScannedNetwork {
	var networks []ScannedNetwork
	var current *ScannedNetwork

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// New BSS entry
		if strings.HasPrefix(line, "BSS ") {
			if current != nil && current.SSID != "" {
				networks = append(networks, *current)
			}
			current = &ScannedNetwork{
				Security: "open",
			}
			// Parse BSSID
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				// BSS aa:bb:cc:dd:ee:ff(on wlan0)
				bssid := strings.TrimSuffix(parts[1], "(on")
				bssid = strings.Split(bssid, "(")[0]
				current.BSSID = bssid
			}
			continue
		}

		if current == nil {
			continue
		}

		// Parse properties
		if strings.HasPrefix(line, "SSID: ") {
			current.SSID = strings.TrimPrefix(line, "SSID: ")
		} else if strings.HasPrefix(line, "signal: ") {
			// signal: -65.00 dBm
			re := regexp.MustCompile(`(-?\d+\.?\d*)`)
			if matches := re.FindStringSubmatch(line); len(matches) >= 2 {
				f, _ := strconv.ParseFloat(matches[1], 64)
				current.Signal = int(f)
				// Convert dBm to quality percentage (rough estimate)
				// -50 dBm = 100%, -100 dBm = 0%
				quality := int((float64(current.Signal) + 100) * 2)
				if quality > 100 {
					quality = 100
				}
				if quality < 0 {
					quality = 0
				}
				current.Quality = quality
			}
		} else if strings.HasPrefix(line, "freq: ") {
			// freq: 2437
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				current.Frequency, _ = strconv.Atoi(parts[1])
				// Determine band
				if current.Frequency < 3000 {
					current.Band = "2.4ghz"
				} else {
					current.Band = "5ghz"
				}
				// Calculate channel
				current.Channel = frequencyToChannel(current.Frequency)
			}
		} else if strings.Contains(line, "WPA:") || strings.Contains(line, "RSN:") {
			// Security detection
			if strings.Contains(line, "WPA:") {
				if current.Security == "open" {
					current.Security = "wpa"
				}
			}
			if strings.Contains(line, "RSN:") {
				current.Security = "wpa2"
			}
		} else if strings.Contains(line, "SAE") {
			current.Security = "wpa3"
		} else if strings.Contains(line, "WEP") {
			current.Security = "wep"
		} else if strings.Contains(line, "Group cipher:") || strings.Contains(line, "Pairwise ciphers:") {
			// Check for WPA3
			if strings.Contains(line, "SAE") {
				current.Security = "wpa3"
			}
		}
	}

	if current != nil && current.SSID != "" {
		networks = append(networks, *current)
	}

	return networks
}

// frequencyToChannel converts WiFi frequency to channel number
func frequencyToChannel(freq int) int {
	// 2.4 GHz band
	if freq >= 2412 && freq <= 2484 {
		if freq == 2484 {
			return 14
		}
		return (freq - 2407) / 5
	}
	// 5 GHz band
	if freq >= 5170 && freq <= 5825 {
		return (freq - 5000) / 5
	}
	return 0
}

// getConnectedSSID returns the SSID of the currently connected network
func (m *Manager) getConnectedSSID(ctx context.Context, iface string) string {
	out, err := m.runner.Run(ctx, "iw", "dev", iface, "link")
	if err != nil {
		return ""
	}

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "SSID: ") {
			return strings.TrimPrefix(line, "SSID: ")
		}
	}

	return ""
}

// GetClientStatus returns the current WiFi client connection status
func (m *Manager) GetClientStatus(ctx context.Context, iface string) (*ClientStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := &ClientStatus{}

	// Check connection status using iw
	out, err := m.runner.Run(ctx, "iw", "dev", iface, "link")
	if err != nil {
		return status, nil
	}

	linkInfo := string(out)
	if strings.Contains(linkInfo, "Not connected") {
		return status, nil
	}

	status.Connected = true

	// Parse link info
	for _, line := range strings.Split(linkInfo, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "SSID: ") {
			status.SSID = strings.TrimPrefix(line, "SSID: ")
		} else if strings.HasPrefix(line, "signal: ") {
			re := regexp.MustCompile(`(-?\d+)`)
			if matches := re.FindStringSubmatch(line); len(matches) >= 2 {
				status.Signal, _ = strconv.Atoi(matches[1])
			}
		} else if strings.HasPrefix(line, "freq: ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				status.Frequency, _ = strconv.Atoi(parts[1])
			}
		} else if strings.HasPrefix(line, "Connected to ") {
			// Connected to aa:bb:cc:dd:ee:ff
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				status.BSSID = parts[2]
			}
		}
	}

	// Get IP address
	ipOut, err := m.runner.Run(ctx, "ip", "-4", "addr", "show", iface)
	if err == nil {
		re := regexp.MustCompile(`inet (\d+\.\d+\.\d+\.\d+)`)
		if matches := re.FindStringSubmatch(string(ipOut)); len(matches) >= 2 {
			status.IPAddress = matches[1]
		}
	}

	// Get default gateway through this interface
	routeOut, err := m.runner.Run(ctx, "ip", "route", "show", "default", "dev", iface)
	if err == nil {
		re := regexp.MustCompile(`via (\d+\.\d+\.\d+\.\d+)`)
		if matches := re.FindStringSubmatch(string(routeOut)); len(matches) >= 2 {
			status.Gateway = matches[1]
		}
	}

	return status, nil
}

// ConnectToNetwork connects to a WiFi network using wpa_supplicant
func (m *Manager) ConnectToNetwork(ctx context.Context, iface, ssid, password, security string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Generate wpa_supplicant config
	configPath := fmt.Sprintf("/etc/wpa_supplicant/wpa_supplicant-%s.conf", iface)

	if err := m.fs.MkdirAll("/etc/wpa_supplicant", 0755); err != nil {
		return fmt.Errorf("failed to create wpa_supplicant directory: %w", err)
	}

	config := m.generateWpaSupplicantConfig(ssid, password, security)
	if err := m.fs.WriteFile(configPath, []byte(config), 0600); err != nil {
		return fmt.Errorf("failed to write wpa_supplicant config: %w", err)
	}

	// Stop any existing wpa_supplicant for this interface
	m.runner.Run(ctx, "killall", "-q", "wpa_supplicant")
	time.Sleep(500 * time.Millisecond)

	// Bring interface up
	if _, err := m.runner.Run(ctx, "ip", "link", "set", iface, "up"); err != nil {
		return fmt.Errorf("failed to bring up interface: %w", err)
	}

	// Start wpa_supplicant
	out, err := m.runner.Run(ctx, "wpa_supplicant", "-B", "-i", iface, "-c", configPath)
	if err != nil {
		return fmt.Errorf("wpa_supplicant failed: %s", strings.TrimSpace(string(out)))
	}

	// Wait for connection
	time.Sleep(3 * time.Second)

	// Check if connected
	ssidConnected := m.getConnectedSSID(ctx, iface)
	if ssidConnected != ssid {
		return fmt.Errorf("failed to connect to %s", ssid)
	}

	// Request DHCP
	out, err = m.runner.Run(ctx, "dhclient", "-v", iface)
	if err != nil {
		// Try dhcpcd as fallback
		out, err = m.runner.Run(ctx, "dhcpcd", iface)
		if err != nil {
			return fmt.Errorf("DHCP failed: %s", strings.TrimSpace(string(out)))
		}
	}

	return nil
}

// generateWpaSupplicantConfig creates wpa_supplicant configuration
func (m *Manager) generateWpaSupplicantConfig(ssid, password, security string) string {
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

// DisconnectNetwork disconnects from the current WiFi network
func (m *Manager) DisconnectNetwork(ctx context.Context, iface string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Kill wpa_supplicant for this interface
	m.runner.Run(ctx, "wpa_cli", "-i", iface, "terminate")

	// Release DHCP lease
	m.runner.Run(ctx, "dhclient", "-r", iface)

	// Bring interface down
	m.runner.Run(ctx, "ip", "link", "set", iface, "down")

	return nil
}

// WriteWpaSupplicantService creates a systemd service for wpa_supplicant
func (m *Manager) WriteWpaSupplicantService(iface string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	configPath := fmt.Sprintf("/etc/wpa_supplicant/wpa_supplicant-%s.conf", iface)
	servicePath := fmt.Sprintf("/etc/systemd/system/wpa_supplicant-%s.service", iface)

	if err := m.fs.MkdirAll("/etc/systemd/system", 0755); err != nil {
		return fmt.Errorf("failed to create systemd directory: %w", err)
	}

	service := fmt.Sprintf(`[Unit]
Description=WPA supplicant for %s
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

	return m.fs.WriteFile(servicePath, []byte(service), 0644)
}

// EnableWpaSupplicant enables wpa_supplicant at boot for an interface
func (m *Manager) EnableWpaSupplicant(ctx context.Context, iface string) error {
	svcName := "wpa_supplicant-" + iface
	return m.services.Enable(ctx, svcName)
}

// DisableWpaSupplicant disables wpa_supplicant at boot for an interface
func (m *Manager) DisableWpaSupplicant(ctx context.Context, iface string) error {
	svcName := "wpa_supplicant-" + iface
	return m.services.Disable(ctx, svcName)
}

// StartWpaSupplicant starts wpa_supplicant for an interface
func (m *Manager) StartWpaSupplicant(ctx context.Context, iface string) error {
	svcName := "wpa_supplicant-" + iface
	return m.services.Start(ctx, svcName)
}

// StopWpaSupplicant stops wpa_supplicant for an interface
func (m *Manager) StopWpaSupplicant(ctx context.Context, iface string) error {
	svcName := "wpa_supplicant-" + iface
	return m.services.Stop(ctx, svcName)
}

// IsWpaSupplicantRunning checks if wpa_supplicant is running for an interface
func (m *Manager) IsWpaSupplicantRunning(ctx context.Context, iface string) bool {
	svcName := "wpa_supplicant-" + iface
	return m.services.IsActive(ctx, svcName)
}
