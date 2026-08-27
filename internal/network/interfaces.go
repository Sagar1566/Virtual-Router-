package network

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/iodesystems/ubuntu-router/internal/system"
)

// InterfaceType represents the type of network interface
type InterfaceType string

const (
	TypeEthernet  InterfaceType = "ethernet"
	TypeWiFi      InterfaceType = "wifi"
	TypeBridge    InterfaceType = "bridge"
	TypeVLAN      InterfaceType = "vlan"
	TypeBond      InterfaceType = "bond"
	TypeLoopback  InterfaceType = "loopback"
	TypeVirtual   InterfaceType = "virtual"
	TypeWireGuard InterfaceType = "wireguard"
	TypeUnknown   InterfaceType = "unknown"
)

// LinkState represents the physical link state
type LinkState string

const (
	LinkUp      LinkState = "up"
	LinkDown    LinkState = "down"
	LinkUnknown LinkState = "unknown"
)

// Interface represents a network interface with its properties
type Interface struct {
	Name      string        `json:"name"`
	Type      InterfaceType `json:"type"`
	MAC       string        `json:"mac"`
	MTU       int           `json:"mtu"`
	State     string        `json:"state"`      // admin state (up/down)
	LinkState LinkState     `json:"link_state"` // carrier state
	Addresses []Address     `json:"addresses"`
	Master    string        `json:"master,omitempty"` // bridge/bond master
	Slaves    []string      `json:"slaves,omitempty"` // for bridges/bonds
	Driver    string        `json:"driver,omitempty"`
	PCI       string        `json:"pci,omitempty"`   // PCI address
	Speed     int           `json:"speed,omitempty"` // Mbps
	Duplex    string        `json:"duplex,omitempty"`
	WifiInfo  *WiFiInfo     `json:"wifi_info,omitempty"`
}

// Address represents an IP address on an interface
type Address struct {
	IP     string `json:"ip"`
	Prefix int    `json:"prefix"`
	Family string `json:"family"` // inet or inet6
	Scope  string `json:"scope"`
}

// WiFiInfo contains WiFi-specific information
type WiFiInfo struct {
	PHY          string   `json:"phy"`
	Capabilities []string `json:"capabilities"`
	Channels     []int    `json:"channels"`
	Bands        []string `json:"bands"`
}

// Route represents a routing table entry
type Route struct {
	Destination string `json:"destination"`
	Gateway     string `json:"gateway,omitempty"`
	Interface   string `json:"interface"`
	Metric      int    `json:"metric,omitempty"`
	Protocol    string `json:"protocol,omitempty"`
	Scope       string `json:"scope,omitempty"`
}

// Manager handles network interface operations
type Manager struct {
	mu     sync.RWMutex
	fs     system.FileSystem
	runner system.CommandRunner
}

// NewManager creates a new network manager
func NewManager(fs system.FileSystem, runner system.CommandRunner) *Manager {
	return &Manager{
		fs:     fs,
		runner: runner,
	}
}

// ListInterfaces returns all network interfaces with their properties
func (m *Manager) ListInterfaces(ctx context.Context) ([]Interface, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries, err := m.fs.ReadDir("/sys/class/net")
	if err != nil {
		return nil, fmt.Errorf("failed to list interfaces: %w", err)
	}

	var interfaces []Interface
	for _, entry := range entries {
		iface, err := m.getInterface(ctx, entry.Name())
		if err != nil {
			continue // Skip interfaces we can't read
		}
		interfaces = append(interfaces, *iface)
	}

	return interfaces, nil
}

// GetInterface returns details about a specific interface
func (m *Manager) GetInterface(ctx context.Context, name string) (*Interface, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.getInterface(ctx, name)
}

func (m *Manager) getInterface(ctx context.Context, name string) (*Interface, error) {
	basePath := filepath.Join("/sys/class/net", name)

	iface := &Interface{
		Name: name,
	}

	// Get MAC address
	if data, err := m.fs.ReadFile(filepath.Join(basePath, "address")); err == nil {
		iface.MAC = strings.TrimSpace(string(data))
	}

	// Get MTU
	if data, err := m.fs.ReadFile(filepath.Join(basePath, "mtu")); err == nil {
		iface.MTU, _ = strconv.Atoi(strings.TrimSpace(string(data)))
	}

	// Get admin state (operstate)
	if data, err := m.fs.ReadFile(filepath.Join(basePath, "operstate")); err == nil {
		iface.State = strings.TrimSpace(string(data))
	}

	// Get carrier (link state)
	if data, err := m.fs.ReadFile(filepath.Join(basePath, "carrier")); err == nil {
		if strings.TrimSpace(string(data)) == "1" {
			iface.LinkState = LinkUp
		} else {
			iface.LinkState = LinkDown
		}
	} else {
		iface.LinkState = LinkUnknown
	}

	// Get speed (if available)
	if data, err := m.fs.ReadFile(filepath.Join(basePath, "speed")); err == nil {
		iface.Speed, _ = strconv.Atoi(strings.TrimSpace(string(data)))
	}

	// Get duplex
	if data, err := m.fs.ReadFile(filepath.Join(basePath, "duplex")); err == nil {
		iface.Duplex = strings.TrimSpace(string(data))
	}

	// Determine interface type
	iface.Type = m.detectInterfaceType(basePath, name)

	// Get driver
	if target, err := m.fs.ReadLink(filepath.Join(basePath, "device/driver")); err == nil {
		iface.Driver = filepath.Base(target)
	}

	// Get PCI address
	if target, err := m.fs.ReadLink(filepath.Join(basePath, "device")); err == nil {
		if strings.Contains(target, "pci") {
			iface.PCI = filepath.Base(target)
		}
	}

	// Get bridge master
	if target, err := m.fs.ReadLink(filepath.Join(basePath, "master")); err == nil {
		iface.Master = filepath.Base(target)
	}

	// Get bridge slaves
	if iface.Type == TypeBridge {
		if entries, err := m.fs.ReadDir(filepath.Join(basePath, "brif")); err == nil {
			for _, e := range entries {
				iface.Slaves = append(iface.Slaves, e.Name())
			}
		}
	}

	// Get IP addresses using ip command
	if addrs, err := m.getAddresses(ctx, name); err == nil {
		iface.Addresses = addrs
	}

	// Get WiFi info if applicable
	if iface.Type == TypeWiFi {
		iface.WifiInfo = m.getWiFiInfo(ctx, name)
	}

	return iface, nil
}

func (m *Manager) detectInterfaceType(basePath, name string) InterfaceType {
	if name == "lo" {
		return TypeLoopback
	}

	// Check for bridge
	if _, err := m.fs.Stat(filepath.Join(basePath, "bridge")); err == nil {
		return TypeBridge
	}

	// Check for bond
	if _, err := m.fs.Stat(filepath.Join(basePath, "bonding")); err == nil {
		return TypeBond
	}

	// Check for wireless
	if _, err := m.fs.Stat(filepath.Join(basePath, "wireless")); err == nil {
		return TypeWiFi
	}
	if _, err := m.fs.Stat(filepath.Join(basePath, "phy80211")); err == nil {
		return TypeWiFi
	}

	// Check for VLAN
	if strings.Contains(name, ".") || strings.HasPrefix(name, "vlan") {
		return TypeVLAN
	}

	// Check for WireGuard
	if strings.HasPrefix(name, "wg") {
		return TypeWireGuard
	}

	// Check for virtual interfaces
	if _, err := m.fs.ReadLink(filepath.Join(basePath, "device")); err != nil {
		// No device link usually means virtual
		if strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "docker") ||
			strings.HasPrefix(name, "br-") || strings.HasPrefix(name, "virbr") {
			return TypeVirtual
		}
	}

	// Check type file
	if data, err := m.fs.ReadFile(filepath.Join(basePath, "type")); err == nil {
		typeNum, _ := strconv.Atoi(strings.TrimSpace(string(data)))
		switch typeNum {
		case 1:
			return TypeEthernet
		case 801:
			return TypeWiFi
		}
	}

	// Default to ethernet for physical interfaces with a device link
	if _, err := m.fs.ReadLink(filepath.Join(basePath, "device")); err == nil {
		return TypeEthernet
	}

	return TypeUnknown
}

func (m *Manager) getAddresses(ctx context.Context, name string) ([]Address, error) {
	out, err := m.runner.Run(ctx, "ip", "-o", "addr", "show", name)
	if err != nil {
		return nil, err
	}

	var addrs []Address
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	addrRegex := regexp.MustCompile(`(inet6?)\s+(\S+)\s+.*scope\s+(\S+)`)

	for scanner.Scan() {
		line := scanner.Text()
		matches := addrRegex.FindStringSubmatch(line)
		if len(matches) >= 4 {
			ip, prefix := parseIPPrefix(matches[2])
			addrs = append(addrs, Address{
				IP:     ip,
				Prefix: prefix,
				Family: matches[1],
				Scope:  matches[3],
			})
		}
	}

	return addrs, nil
}

func (m *Manager) getWiFiInfo(ctx context.Context, name string) *WiFiInfo {
	info := &WiFiInfo{}

	// Get PHY name
	out, err := m.runner.Run(ctx, "iw", "dev", name, "info")
	if err != nil {
		return info
	}

	phyRegex := regexp.MustCompile(`wiphy\s+(\d+)`)
	if matches := phyRegex.FindStringSubmatch(string(out)); len(matches) >= 2 {
		info.PHY = "phy" + matches[1]
	}

	// Get capabilities
	if info.PHY != "" {
		out, err := m.runner.Run(ctx, "iw", info.PHY, "info")
		if err == nil {
			// Parse supported bands and channels
			lines := strings.Split(string(out), "\n")
			var currentBand string
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "Band ") {
					currentBand = line
					info.Bands = append(info.Bands, currentBand)
				}
				if strings.Contains(line, "Capabilities:") {
					info.Capabilities = append(info.Capabilities, strings.TrimPrefix(line, "Capabilities: "))
				}
				// Parse channel numbers
				if strings.HasPrefix(line, "* ") && strings.Contains(line, "MHz") {
					chanRegex := regexp.MustCompile(`\[(\d+)\]`)
					if matches := chanRegex.FindStringSubmatch(line); len(matches) >= 2 {
						if ch, err := strconv.Atoi(matches[1]); err == nil {
							info.Channels = append(info.Channels, ch)
						}
					}
				}
			}
		}
	}

	return info
}

// SetInterfaceUp brings an interface up
func (m *Manager) SetInterfaceUp(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.runner.Run(ctx, "ip", "link", "set", name, "up")
	return err
}

// SetInterfaceDown brings an interface down
func (m *Manager) SetInterfaceDown(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.runner.Run(ctx, "ip", "link", "set", name, "down")
	return err
}

// AddAddress adds an IP address to an interface
func (m *Manager) AddAddress(ctx context.Context, name, address string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.runner.Run(ctx, "ip", "addr", "add", address, "dev", name)
	return err
}

// RemoveAddress removes an IP address from an interface
func (m *Manager) RemoveAddress(ctx context.Context, name, address string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.runner.Run(ctx, "ip", "addr", "del", address, "dev", name)
	return err
}

// FlushAddresses removes all addresses from an interface
func (m *Manager) FlushAddresses(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.runner.Run(ctx, "ip", "addr", "flush", "dev", name)
	return err
}

// CreateBridge creates a new bridge interface
func (m *Manager) CreateBridge(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.runner.Run(ctx, "ip", "link", "add", name, "type", "bridge")
	return err
}

// DeleteBridge deletes a bridge interface
func (m *Manager) DeleteBridge(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.runner.Run(ctx, "ip", "link", "delete", name, "type", "bridge")
	return err
}

// AddToBridge adds an interface to a bridge
func (m *Manager) AddToBridge(ctx context.Context, bridge, iface string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.runner.Run(ctx, "ip", "link", "set", iface, "master", bridge)
	return err
}

// RemoveFromBridge removes an interface from a bridge
func (m *Manager) RemoveFromBridge(ctx context.Context, iface string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.runner.Run(ctx, "ip", "link", "set", iface, "nomaster")
	return err
}

// GetRoutes returns the routing table
func (m *Manager) GetRoutes(ctx context.Context) ([]Route, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out, err := m.runner.Run(ctx, "ip", "-o", "route", "show")
	if err != nil {
		return nil, err
	}

	var routes []Route
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		route := parseRoute(line)
		if route != nil {
			routes = append(routes, *route)
		}
	}

	return routes, nil
}

// RouteLookupResult contains the result of a route lookup
type RouteLookupResult struct {
	Destination string `json:"destination"`
	Interface   string `json:"interface"`
	Gateway     string `json:"gateway,omitempty"`
	Source      string `json:"source,omitempty"`
	UID         string `json:"uid,omitempty"`
	RawOutput   string `json:"raw_output"`
}

// LookupRoute uses "ip route get" to determine which interface handles traffic to a destination
func (m *Manager) LookupRoute(ctx context.Context, destination string) (*RouteLookupResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out, err := m.runner.Run(ctx, "ip", "route", "get", destination)
	if err != nil {
		return nil, fmt.Errorf("route lookup failed: %w", err)
	}

	output := strings.TrimSpace(string(out))
	result := &RouteLookupResult{
		Destination: destination,
		RawOutput:   output,
	}

	// Parse output like: "192.168.1.50 via 10.100.0.1 dev wg1 src 10.100.0.4 uid 1000"
	// or: "192.168.1.1 dev br0 src 192.168.1.170 uid 1000"
	fields := strings.Fields(output)
	for i, field := range fields {
		switch field {
		case "dev":
			if i+1 < len(fields) {
				result.Interface = fields[i+1]
			}
		case "via":
			if i+1 < len(fields) {
				result.Gateway = fields[i+1]
			}
		case "src":
			if i+1 < len(fields) {
				result.Source = fields[i+1]
			}
		case "uid":
			if i+1 < len(fields) {
				result.UID = fields[i+1]
			}
		}
	}

	return result, nil
}

// AddRoute adds a route
func (m *Manager) AddRoute(ctx context.Context, dest, gateway, iface string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	args := []string{"route", "add", dest}
	if gateway != "" {
		args = append(args, "via", gateway)
	}
	if iface != "" {
		args = append(args, "dev", iface)
	}

	_, err := m.runner.Run(ctx, "ip", args...)
	return err
}

// DeleteRoute deletes a route
func (m *Manager) DeleteRoute(ctx context.Context, dest string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.runner.Run(ctx, "ip", "route", "del", dest)
	return err
}

// SetDefaultGateway sets the default gateway
func (m *Manager) SetDefaultGateway(ctx context.Context, gateway, iface string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Remove existing default route
	m.runner.Run(ctx, "ip", "route", "del", "default")

	args := []string{"route", "add", "default", "via", gateway}
	if iface != "" {
		args = append(args, "dev", iface)
	}

	_, err := m.runner.Run(ctx, "ip", args...)
	return err
}

// EnableIPForward enables IP forwarding
func (m *Manager) EnableIPForward(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.fs.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1"), 0644); err != nil {
		return err
	}

	// Also set sysctl for persistence
	_, err := m.runner.Run(ctx, "sysctl", "-w", "net.ipv4.ip_forward=1")
	return err
}

// DisableIPForward disables IP forwarding
func (m *Manager) DisableIPForward(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.fs.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("0"), 0644)
}

// IsIPForwardEnabled checks if IP forwarding is enabled
func (m *Manager) IsIPForwardEnabled() bool {
	data, err := m.fs.ReadFile("/proc/sys/net/ipv4/ip_forward")
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == "1"
}

// GetDefaultInterface returns the interface used for the default route
func (m *Manager) GetDefaultInterface(ctx context.Context) (string, error) {
	routes, err := m.GetRoutes(ctx)
	if err != nil {
		return "", err
	}

	for _, r := range routes {
		if r.Destination == "default" {
			return r.Interface, nil
		}
	}

	return "", fmt.Errorf("no default route found")
}

// Helper functions

func parseIPPrefix(addr string) (string, int) {
	parts := strings.Split(addr, "/")
	if len(parts) == 2 {
		prefix, _ := strconv.Atoi(parts[1])
		return parts[0], prefix
	}
	return addr, 0
}

func parseRoute(line string) *Route {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return nil
	}

	route := &Route{
		Destination: fields[0],
	}

	for i := 1; i < len(fields)-1; i++ {
		switch fields[i] {
		case "via":
			route.Gateway = fields[i+1]
		case "dev":
			route.Interface = fields[i+1]
		case "metric":
			route.Metric, _ = strconv.Atoi(fields[i+1])
		case "proto":
			route.Protocol = fields[i+1]
		case "scope":
			route.Scope = fields[i+1]
		}
	}

	return route
}

// GetPhysicalInterfaces returns only physical network interfaces (ethernet and wifi)
func (m *Manager) GetPhysicalInterfaces(ctx context.Context) ([]Interface, error) {
	all, err := m.ListInterfaces(ctx)
	if err != nil {
		return nil, err
	}

	var physical []Interface
	for _, iface := range all {
		if iface.Type == TypeEthernet || iface.Type == TypeWiFi {
			physical = append(physical, iface)
		}
	}

	return physical, nil
}

// GetInterfaceByMAC finds an interface by MAC address
func (m *Manager) GetInterfaceByMAC(ctx context.Context, mac string) (*Interface, error) {
	all, err := m.ListInterfaces(ctx)
	if err != nil {
		return nil, err
	}

	mac = strings.ToLower(mac)
	for _, iface := range all {
		if strings.ToLower(iface.MAC) == mac {
			return &iface, nil
		}
	}

	return nil, fmt.Errorf("interface with MAC %s not found", mac)
}

// DetectWANInterface attempts to find the WAN interface
func (m *Manager) DetectWANInterface(ctx context.Context) (*Interface, error) {
	// First try to get the interface with the default route
	defaultIface, err := m.GetDefaultInterface(ctx)
	if err == nil {
		return m.GetInterface(ctx, defaultIface)
	}

	// Fall back to physical interfaces
	physical, err := m.GetPhysicalInterfaces(ctx)
	if err != nil {
		return nil, err
	}

	// Look for ethernet interface with an IP address (likely DHCP-assigned)
	for _, iface := range physical {
		if iface.Type == TypeEthernet && iface.LinkState == LinkUp && len(iface.Addresses) > 0 {
			// Has an IP address - likely got one from DHCP
			return &iface, nil
		}
	}

	// Look for ethernet interface with carrier but no IP (might need DHCP)
	for _, iface := range physical {
		if iface.Type == TypeEthernet && iface.LinkState == LinkUp {
			return &iface, nil
		}
	}

	// Last resort: any ethernet interface
	for _, iface := range physical {
		if iface.Type == TypeEthernet {
			return &iface, nil
		}
	}

	return nil, fmt.Errorf("could not detect WAN interface: no ethernet interfaces found")
}

// RenewDHCP triggers DHCP renewal on an interface
func (m *Manager) RenewDHCP(ctx context.Context, iface string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Try dhclient first
	if _, err := os.Stat("/sbin/dhclient"); err == nil {
		m.runner.Run(ctx, "dhclient", "-r", iface)
		_, err := m.runner.Run(ctx, "dhclient", iface)
		return err
	}

	// Try dhcpcd
	if _, err := os.Stat("/sbin/dhcpcd"); err == nil {
		_, err := m.runner.Run(ctx, "dhcpcd", "-n", iface)
		return err
	}

	return fmt.Errorf("no DHCP client found")
}

// ApplyNetplan writes and applies netplan configuration
func (m *Manager) ApplyNetplan(ctx context.Context, config string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	configPath := "/etc/netplan/99-ubuntu-router.yaml"
	if err := m.fs.WriteFile(configPath, []byte(config), 0600); err != nil {
		return fmt.Errorf("failed to write netplan config: %w", err)
	}

	_, err := m.runner.Run(ctx, "netplan", "apply")
	return err
}

// GenerateNetplanConfig generates a netplan configuration
func (m *Manager) GenerateNetplanConfig(wanIface, wanMode, staticIP, gateway, dns string, bridge string, bridgePorts []string, bridgeAddresses []string) string {
	var sb strings.Builder
	sb.WriteString("network:\n")
	sb.WriteString("  version: 2\n")
	sb.WriteString("  renderer: networkd\n")
	sb.WriteString("  ethernets:\n")

	// WAN interface
	sb.WriteString(fmt.Sprintf("    %s:\n", wanIface))
	if wanMode == "dhcp" {
		sb.WriteString("      dhcp4: true\n")
	} else if wanMode == "static" {
		sb.WriteString("      dhcp4: false\n")
		sb.WriteString(fmt.Sprintf("      addresses: [%s]\n", staticIP))
		if gateway != "" {
			sb.WriteString("      routes:\n")
			sb.WriteString(fmt.Sprintf("        - to: default\n          via: %s\n", gateway))
		}
		if dns != "" {
			sb.WriteString("      nameservers:\n")
			sb.WriteString(fmt.Sprintf("        addresses: [%s]\n", dns))
		}
	}

	// Bridge ports (if any)
	for _, port := range bridgePorts {
		if port != wanIface {
			sb.WriteString(fmt.Sprintf("    %s:\n", port))
			sb.WriteString("      dhcp4: false\n")
		}
	}

	// Bridge configuration
	if bridge != "" && len(bridgePorts) > 0 {
		sb.WriteString("  bridges:\n")
		sb.WriteString(fmt.Sprintf("    %s:\n", bridge))
		sb.WriteString("      dhcp4: false\n")
		if len(bridgeAddresses) > 0 {
			sb.WriteString("      addresses:\n")
			for _, addr := range bridgeAddresses {
				sb.WriteString(fmt.Sprintf("        - %s\n", addr))
			}
		}
		sb.WriteString("      interfaces:\n")
		for _, port := range bridgePorts {
			if port != wanIface {
				sb.WriteString(fmt.Sprintf("        - %s\n", port))
			}
		}
	}

	return sb.String()
}

// CalculateLANSubnet calculates a LAN subnet that doesn't conflict with the given WAN IP.
// It increments the third octet to create a different /24 subnet.
// Returns gateway, dhcpStart, dhcpEnd.
func CalculateLANSubnet(wanIP string) (gateway, dhcpStart, dhcpEnd string) {
	// Default fallback
	defaultGateway := "192.168.1.1"
	defaultStart := "192.168.1.100"
	defaultEnd := "192.168.1.200"

	if wanIP == "" {
		return defaultGateway, defaultStart, defaultEnd
	}

	parts := strings.Split(wanIP, ".")
	if len(parts) != 4 {
		return defaultGateway, defaultStart, defaultEnd
	}

	// Parse third octet and increment
	thirdOctet, err := strconv.Atoi(parts[2])
	if err != nil {
		return defaultGateway, defaultStart, defaultEnd
	}

	newThird := (thirdOctet + 1) % 256
	base := fmt.Sprintf("%s.%s.%d", parts[0], parts[1], newThird)

	return base + ".1", base + ".100", base + ".200"
}

// SubnetsConflict checks if two IPs are in the same /24 subnet
func SubnetsConflict(ip1, ip2 string) bool {
	if ip1 == "" || ip2 == "" {
		return false
	}

	// Strip CIDR notation if present
	ip1 = strings.Split(ip1, "/")[0]
	ip2 = strings.Split(ip2, "/")[0]

	parts1 := strings.Split(ip1, ".")
	parts2 := strings.Split(ip2, ".")

	if len(parts1) < 3 || len(parts2) < 3 {
		return false
	}

	return parts1[0] == parts2[0] && parts1[1] == parts2[1] && parts1[2] == parts2[2]
}
