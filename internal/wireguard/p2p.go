package wireguard

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/iodesystems/ubuntu-router/internal/config"
	"github.com/iodesystems/ubuntu-router/internal/system"
)

// P2PStatus represents the live status of a P2P tunnel
type P2PStatus struct {
	TunnelID        string    `json:"tunnel_id"`
	Name            string    `json:"name"`
	Interface       string    `json:"interface"`
	Running         bool      `json:"running"`
	Enabled         bool      `json:"enabled"`
	PublicKey       string    `json:"public_key"`
	ListenPort      int       `json:"listen_port"`
	Address         string    `json:"address"`
	RemoteEndpoint  string    `json:"remote_endpoint"`
	Connected       bool      `json:"connected"`
	LatestHandshake time.Time `json:"latest_handshake,omitempty"`
	TransferRx      int64     `json:"transfer_rx"`
	TransferTx      int64     `json:"transfer_tx"`
	LocalSubnets    []string  `json:"local_subnets"`
	RemoteSubnets   []string  `json:"remote_subnets"`
}

// RemotePeerConfig contains the configuration to share with the remote peer
type RemotePeerConfig struct {
	TunnelName       string   `json:"tunnel_name"`
	LocalPublicKey   string   `json:"local_public_key"`
	LocalEndpoint    string   `json:"local_endpoint"`
	LocalSubnets     []string `json:"local_subnets"`
	PresharedKey     string   `json:"preshared_key,omitempty"`
	SuggestedAddress string   `json:"suggested_address"`
	WireGuardConfig  string   `json:"wireguard_config"`
}

// KeyPair represents a WireGuard key pair
type KeyPair struct {
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
}

// SubnetConflict represents a conflict between local and remote subnets
type SubnetConflict struct {
	LocalSubnet  string `json:"local_subnet"`
	RemoteSubnet string `json:"remote_subnet"`
	Description  string `json:"description"`
}

// ValidationResult contains the results of tunnel configuration validation
type ValidationResult struct {
	Valid            bool              `json:"valid"`
	Conflicts        []SubnetConflict  `json:"conflicts,omitempty"`
	Warnings         []string          `json:"warnings,omitempty"`
	// FilteredSubnets contains remote subnets that were auto-filtered due to conflicts
	FilteredSubnets  []string          `json:"filtered_subnets,omitempty"`
	// SafeRemoteSubnets contains the remote subnets after filtering out conflicts
	SafeRemoteSubnets []string         `json:"safe_remote_subnets,omitempty"`
}

// P2PManager handles WireGuard point-to-point tunnel operations
type P2PManager struct {
	mu       sync.RWMutex
	fs       system.FileSystem
	runner   system.CommandRunner
	services *system.ServiceManager
}

// NewP2P creates a new P2P tunnel manager
func NewP2P(fs system.FileSystem, runner system.CommandRunner) *P2PManager {
	return &P2PManager{
		fs:       fs,
		runner:   runner,
		services: system.NewServiceManager(runner),
	}
}

// GenerateKeyPair generates a new WireGuard key pair
func (m *P2PManager) GenerateKeyPair(ctx context.Context) (*KeyPair, error) {
	// Generate private key
	privOut, err := m.runner.Run(ctx, "wg", "genkey")
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}
	privateKey := strings.TrimSpace(string(privOut))

	// Generate public key from private key
	pubOut, err := m.runner.RunWithStdin(ctx, strings.NewReader(privateKey), "wg", "pubkey")
	if err != nil {
		return nil, fmt.Errorf("failed to generate public key: %w", err)
	}
	publicKey := strings.TrimSpace(string(pubOut))

	return &KeyPair{
		PrivateKey: privateKey,
		PublicKey:  publicKey,
	}, nil
}

// GeneratePresharedKey generates a preshared key
func (m *P2PManager) GeneratePresharedKey(ctx context.Context) (string, error) {
	out, err := m.runner.Run(ctx, "wg", "genpsk")
	if err != nil {
		return "", fmt.Errorf("failed to generate preshared key: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// GetPublicKeyFromPrivate derives public key from a private key
func (m *P2PManager) GetPublicKeyFromPrivate(ctx context.Context, privateKey string) (string, error) {
	if privateKey == "" {
		return "", fmt.Errorf("no private key provided")
	}
	pubOut, err := m.runner.RunWithStdin(ctx, strings.NewReader(privateKey), "wg", "pubkey")
	if err != nil {
		return "", fmt.Errorf("failed to derive public key: %w", err)
	}
	return strings.TrimSpace(string(pubOut)), nil
}

// AllocateInterface finds the next available WireGuard interface name
func (m *P2PManager) AllocateInterface(existing []config.WireGuardP2PTunnel) string {
	used := make(map[string]bool)
	used["wg0"] = true // Reserved for client-server VPN

	for _, t := range existing {
		if t.Interface != "" {
			used[t.Interface] = true
		}
	}

	// Find first available starting from wg1
	for i := 1; i < 100; i++ {
		name := fmt.Sprintf("wg%d", i)
		if !used[name] {
			return name
		}
	}
	return "wg99"
}

// AllocatePort finds the next available listen port
func (m *P2PManager) AllocatePort(existing []config.WireGuardP2PTunnel) int {
	usedPorts := make(map[int]bool)
	usedPorts[51820] = true // Reserved for wg0

	for _, t := range existing {
		if t.ListenPort > 0 {
			usedPorts[t.ListenPort] = true
		}
	}

	// Start from 51821 for P2P tunnels
	for port := 51821; port < 52000; port++ {
		if !usedPorts[port] {
			return port
		}
	}
	return 51821
}

// ConfigPath returns the config file path for a tunnel
func (m *P2PManager) ConfigPath(ifaceName string) string {
	return filepath.Join("/etc/wireguard", ifaceName+".conf")
}

// PrepareNewTunnel prepares a new tunnel with auto-generated values
func (m *P2PManager) PrepareNewTunnel(ctx context.Context, tunnel *config.WireGuardP2PTunnel, existing []config.WireGuardP2PTunnel) error {
	// Generate ID if not set
	if tunnel.ID == "" {
		tunnel.ID = fmt.Sprintf("tunnel-%d", time.Now().UnixNano())
	}

	// Allocate interface if not set
	if tunnel.Interface == "" {
		tunnel.Interface = m.AllocateInterface(existing)
	}

	// Allocate port if not set
	if tunnel.ListenPort == 0 {
		tunnel.ListenPort = m.AllocatePort(existing)
	}

	// Generate private key if not set
	if tunnel.PrivateKey == "" {
		keyPair, err := m.GenerateKeyPair(ctx)
		if err != nil {
			return fmt.Errorf("failed to generate key pair: %w", err)
		}
		tunnel.PrivateKey = keyPair.PrivateKey
	}

	// Generate preshared key if not set and remote public key is provided
	if tunnel.PresharedKey == "" && tunnel.RemotePublicKey != "" {
		psk, err := m.GeneratePresharedKey(ctx)
		if err != nil {
			return fmt.Errorf("failed to generate preshared key: %w", err)
		}
		tunnel.PresharedKey = psk
	}

	// Set default persistent keepalive
	if tunnel.PersistentKeepalive == 0 {
		tunnel.PersistentKeepalive = 25
	}

	return nil
}

// WriteConfig writes the WireGuard configuration file for a tunnel
// Uses tunnel.RemoteSubnets directly - for safe writing use WriteConfigSafe
func (m *P2PManager) WriteConfig(tunnel *config.WireGuardP2PTunnel) error {
	return m.writeConfigWithSubnets(tunnel, tunnel.RemoteSubnets)
}

// WriteConfigSafe writes the WireGuard config with subnet filtering applied
// Conflicting subnets are removed to preserve local connectivity
// ForcedSubnets are always included regardless of conflicts
func (m *P2PManager) WriteConfigSafe(tunnel *config.WireGuardP2PTunnel, cfg *config.Config) (*ValidationResult, error) {
	validation := m.ValidateTunnelConfig(tunnel, cfg)

	// Start with safe subnets (filtered)
	subnetsToUse := make([]string, 0)
	subnetsToUse = append(subnetsToUse, validation.SafeRemoteSubnets...)

	// Add forced subnets (these bypass conflict detection)
	// User explicitly wants these routed through the tunnel
	if len(tunnel.ForcedSubnets) > 0 {
		subnetsToUse = append(subnetsToUse, tunnel.ForcedSubnets...)
		// Note in validation that forced subnets are being used
		for _, fs := range tunnel.ForcedSubnets {
			validation.Warnings = append(validation.Warnings,
				fmt.Sprintf("Forced subnet %s will route through tunnel (may override local connectivity)", fs))
		}
	}

	if err := m.writeConfigWithSubnets(tunnel, subnetsToUse); err != nil {
		return validation, err
	}

	return validation, nil
}

// writeConfigWithSubnets writes the WireGuard config with specified subnets
func (m *P2PManager) writeConfigWithSubnets(tunnel *config.WireGuardP2PTunnel, remoteSubnets []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	configPath := m.ConfigPath(tunnel.Interface)

	var sb strings.Builder

	// [Interface] section
	sb.WriteString("[Interface]\n")
	sb.WriteString(fmt.Sprintf("PrivateKey = %s\n", tunnel.PrivateKey))
	sb.WriteString(fmt.Sprintf("Address = %s\n", tunnel.Address))
	sb.WriteString(fmt.Sprintf("ListenPort = %d\n", tunnel.ListenPort))

	// PostUp/PostDown for forwarding (site-to-site, no NAT)
	sb.WriteString("PostUp = iptables -A FORWARD -i %i -j ACCEPT; iptables -A FORWARD -o %i -j ACCEPT\n")
	sb.WriteString("PostDown = iptables -D FORWARD -i %i -j ACCEPT; iptables -D FORWARD -o %i -j ACCEPT\n")
	sb.WriteString("\n")

	// [Peer] section
	sb.WriteString("[Peer]\n")
	sb.WriteString(fmt.Sprintf("# %s\n", tunnel.Name))
	sb.WriteString(fmt.Sprintf("PublicKey = %s\n", tunnel.RemotePublicKey))
	if tunnel.PresharedKey != "" {
		sb.WriteString(fmt.Sprintf("PresharedKey = %s\n", tunnel.PresharedKey))
	}

	// AllowedIPs = remote tunnel IP + remote subnets (filtered)
	allowedIPs := make([]string, 0)
	// Add remote tunnel IP (peer's end of the /30) - always include this
	remoteIP := m.CalculatePeerIP(tunnel.Address)
	if remoteIP != "" {
		allowedIPs = append(allowedIPs, remoteIP)
	}
	// Add remote subnets (may be filtered for safety)
	allowedIPs = append(allowedIPs, remoteSubnets...)
	sb.WriteString(fmt.Sprintf("AllowedIPs = %s\n", strings.Join(allowedIPs, ", ")))

	if tunnel.RemoteEndpoint != "" {
		sb.WriteString(fmt.Sprintf("Endpoint = %s\n", tunnel.RemoteEndpoint))
	}

	sb.WriteString(fmt.Sprintf("PersistentKeepalive = %d\n", tunnel.PersistentKeepalive))

	// Ensure directory exists
	dir := filepath.Dir(configPath)
	if err := m.fs.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create wireguard directory: %w", err)
	}

	return m.fs.WriteFile(configPath, []byte(sb.String()), 0600)
}

// DeleteConfig removes the config file for a tunnel
func (m *P2PManager) DeleteConfig(ifaceName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	configPath := m.ConfigPath(ifaceName)
	return m.fs.Remove(configPath)
}

// CalculatePeerIP calculates the peer's tunnel IP from our address
// For a /30 network, if we're .1, peer is .2 and vice versa
func (m *P2PManager) CalculatePeerIP(address string) string {
	ip, ipNet, err := net.ParseCIDR(address)
	if err != nil {
		return ""
	}

	ip4 := ip.To4()
	if ip4 == nil {
		return ""
	}

	// For /30 networks, flip between .1 and .2
	ones, _ := ipNet.Mask.Size()
	if ones == 30 {
		if ip4[3]%4 == 1 {
			ip4[3] = ip4[3] + 1
		} else if ip4[3]%4 == 2 {
			ip4[3] = ip4[3] - 1
		}
		return ip4.String() + "/32"
	}

	// For other networks, just increment
	ip4[3]++
	return ip4.String() + "/32"
}

// subnetsOverlap checks if two CIDR subnets overlap
func subnetsOverlap(cidr1, cidr2 string) bool {
	_, net1, err1 := net.ParseCIDR(cidr1)
	_, net2, err2 := net.ParseCIDR(cidr2)
	if err1 != nil || err2 != nil {
		return false
	}

	// Check if either network contains the start of the other
	return net1.Contains(net2.IP) || net2.Contains(net1.IP)
}

// normalizeCIDR ensures an IP or CIDR has a network mask
func normalizeCIDR(addr string) string {
	if strings.Contains(addr, "/") {
		return addr
	}
	// Assume /32 for bare IPs
	return addr + "/32"
}

// GetLocalSubnets returns all local network subnets from config AND live system interfaces
func GetLocalSubnets(cfg *config.Config) []string {
	seen := make(map[string]bool)
	subnets := make([]string, 0)

	addSubnet := func(cidr string) {
		if !seen[cidr] {
			seen[cidr] = true
			subnets = append(subnets, cidr)
		}
	}

	// LAN addresses from config (e.g., 192.168.1.1/24)
	for _, addr := range cfg.LANAddresses {
		// Convert interface address to network (192.168.1.1/24 -> 192.168.1.0/24)
		ip, ipNet, err := net.ParseCIDR(addr)
		if err != nil {
			continue
		}
		networkAddr := ip.Mask(ipNet.Mask)
		ones, _ := ipNet.Mask.Size()
		addSubnet(fmt.Sprintf("%s/%d", networkAddr.String(), ones))
	}

	// WireGuard VPN subnet (wg0)
	if cfg.WireGuard != nil && cfg.WireGuard.Enabled && cfg.WireGuard.Address != "" {
		ip, ipNet, err := net.ParseCIDR(cfg.WireGuard.Address)
		if err == nil {
			networkAddr := ip.Mask(ipNet.Mask)
			ones, _ := ipNet.Mask.Size()
			addSubnet(fmt.Sprintf("%s/%d", networkAddr.String(), ones))
		}
	}

	// Other P2P tunnel addresses (avoid conflicts between tunnels)
	if cfg.WireGuardP2P != nil {
		for _, t := range cfg.WireGuardP2P.Tunnels {
			if t.Address != "" {
				ip, ipNet, err := net.ParseCIDR(t.Address)
				if err == nil {
					networkAddr := ip.Mask(ipNet.Mask)
					ones, _ := ipNet.Mask.Size()
					addSubnet(fmt.Sprintf("%s/%d", networkAddr.String(), ones))
				}
			}
		}
	}

	// CRITICAL: Also get subnets from LIVE system interfaces
	// This catches interfaces not in config (like the one user is connecting through)
	ifaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifaces {
			// Skip loopback and down interfaces
			if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
				continue
			}
			// Skip WireGuard interfaces (they're the tunnels we're creating)
			if strings.HasPrefix(iface.Name, "wg") {
				continue
			}
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				ipNet, ok := addr.(*net.IPNet)
				if !ok {
					continue
				}
				// Only IPv4 for now
				if ipNet.IP.To4() == nil {
					continue
				}
				networkAddr := ipNet.IP.Mask(ipNet.Mask)
				ones, _ := ipNet.Mask.Size()
				addSubnet(fmt.Sprintf("%s/%d", networkAddr.String(), ones))
			}
		}
	}

	return subnets
}

// GetRouterAddresses returns all IP addresses assigned to the router itself
// Includes both config IPs and live system interface IPs
func GetRouterAddresses(cfg *config.Config) []string {
	seen := make(map[string]bool)
	addrs := make([]string, 0)

	addAddr := func(ip string) {
		if !seen[ip] {
			seen[ip] = true
			addrs = append(addrs, ip)
		}
	}

	// LAN addresses from config
	for _, addr := range cfg.LANAddresses {
		ip, _, err := net.ParseCIDR(addr)
		if err == nil {
			addAddr(ip.String())
		}
	}

	// WireGuard address
	if cfg.WireGuard != nil && cfg.WireGuard.Address != "" {
		ip, _, err := net.ParseCIDR(cfg.WireGuard.Address)
		if err == nil {
			addAddr(ip.String())
		}
	}

	// P2P tunnel addresses
	if cfg.WireGuardP2P != nil {
		for _, t := range cfg.WireGuardP2P.Tunnels {
			if t.Address != "" {
				ip, _, err := net.ParseCIDR(t.Address)
				if err == nil {
					addAddr(ip.String())
				}
			}
		}
	}

	// CRITICAL: Also get IPs from LIVE system interfaces
	ifaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifaces {
			// Skip loopback and down interfaces
			if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
				continue
			}
			// Skip WireGuard interfaces
			if strings.HasPrefix(iface.Name, "wg") {
				continue
			}
			ifAddrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, addr := range ifAddrs {
				ipNet, ok := addr.(*net.IPNet)
				if !ok {
					continue
				}
				// Only IPv4
				if ipNet.IP.To4() == nil {
					continue
				}
				addAddr(ipNet.IP.String())
			}
		}
	}

	return addrs
}

// ValidateTunnelConfig checks for routing conflicts before starting a tunnel
// Returns a ValidationResult with detected conflicts, safe subnets, and filtered subnets
// Conflicts are reported but the tunnel can still work with filtered subnets
func (m *P2PManager) ValidateTunnelConfig(tunnel *config.WireGuardP2PTunnel, cfg *config.Config) *ValidationResult {
	result := &ValidationResult{
		Valid:             true,
		Conflicts:         []SubnetConflict{},
		Warnings:          []string{},
		FilteredSubnets:   []string{},
		SafeRemoteSubnets: []string{},
	}

	localSubnets := GetLocalSubnets(cfg)
	routerAddresses := GetRouterAddresses(cfg)

	// Calculate the current tunnel's own network to exclude from conflict detection
	// The tunnel's network is BY DEFINITION reachable through the tunnel
	var ownTunnelNetwork string
	if tunnel.Address != "" {
		ip, ipNet, err := net.ParseCIDR(tunnel.Address)
		if err == nil {
			networkAddr := ip.Mask(ipNet.Mask)
			ones, _ := ipNet.Mask.Size()
			ownTunnelNetwork = fmt.Sprintf("%s/%d", networkAddr.String(), ones)
		}
	}

	// Check for common dangerous subnets (WAN access, default routes)
	dangerousSubnets := map[string]string{
		"0.0.0.0/0":      "default route - would route ALL traffic through tunnel",
		"0.0.0.0/1":      "half of internet - covers 0.0.0.0 to 127.255.255.255",
		"128.0.0.0/1":    "half of internet - covers 128.0.0.0 to 255.255.255.255",
		"10.0.0.0/8":     "entire RFC1918 Class A - very broad, may conflict",
		"172.16.0.0/12":  "entire RFC1918 Class B - very broad, may conflict",
		"192.168.0.0/16": "entire RFC1918 Class C - very broad, may conflict",
	}

	// Check each remote subnet for conflicts and build safe list
	for _, remoteSub := range tunnel.RemoteSubnets {
		remoteSub = normalizeCIDR(remoteSub)
		hasConflict := false

		// Check against local subnets
		for _, localSub := range localSubnets {
			// Skip the tunnel's own network - it's expected to overlap with remote subnets
			if ownTunnelNetwork != "" && localSub == ownTunnelNetwork {
				continue
			}
			if subnetsOverlap(remoteSub, localSub) {
				hasConflict = true
				result.Conflicts = append(result.Conflicts, SubnetConflict{
					LocalSubnet:  localSub,
					RemoteSubnet: remoteSub,
					Description:  fmt.Sprintf("Remote subnet %s overlaps with local subnet %s - will be filtered to preserve local connectivity", remoteSub, localSub),
				})
				break // One conflict is enough to filter this subnet
			}
		}

		// Check if remote subnet contains router's own addresses
		// Skip the tunnel's own IP - it's expected to be in the remote subnet
		if !hasConflict {
			var ownTunnelIP string
			if tunnel.Address != "" {
				if ip, _, err := net.ParseCIDR(tunnel.Address); err == nil {
					ownTunnelIP = ip.String()
				}
			}
			_, remoteNet, err := net.ParseCIDR(remoteSub)
			if err == nil {
				for _, addr := range routerAddresses {
					// Skip the tunnel's own IP
					if addr == ownTunnelIP {
						continue
					}
					ip := net.ParseIP(addr)
					if ip != nil && remoteNet.Contains(ip) {
						hasConflict = true
						result.Conflicts = append(result.Conflicts, SubnetConflict{
							LocalSubnet:  addr,
							RemoteSubnet: remoteSub,
							Description:  fmt.Sprintf("Remote subnet %s contains router's IP %s - will be filtered to preserve connectivity", remoteSub, addr),
						})
						break
					}
				}
			}
		}

		// Check for dangerous broad subnets
		if desc, isDangerous := dangerousSubnets[remoteSub]; isDangerous {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Remote subnet %s is %s", remoteSub, desc))
		}

		// Add to appropriate list
		if hasConflict {
			result.FilteredSubnets = append(result.FilteredSubnets, remoteSub)
		} else {
			result.SafeRemoteSubnets = append(result.SafeRemoteSubnets, remoteSub)
		}
	}

	// Check if tunnel address conflicts with anything
	if tunnel.Address != "" {
		tunnelNet := normalizeCIDR(tunnel.Address)
		for _, localSub := range localSubnets {
			// Skip comparing tunnel's own subnet from config (it's expected to be in there)
			if strings.Contains(localSub, strings.Split(tunnel.Address, "/")[0]) {
				continue
			}
			if subnetsOverlap(tunnelNet, localSub) {
				result.Warnings = append(result.Warnings, fmt.Sprintf("Tunnel address %s overlaps with existing local subnet %s", tunnel.Address, localSub))
			}
		}
	}

	// Validate remote endpoint if specified
	if tunnel.RemoteEndpoint != "" {
		// Check if remote endpoint is a local IP (configuration error)
		host := tunnel.RemoteEndpoint
		if idx := strings.LastIndex(host, ":"); idx != -1 {
			host = host[:idx]
		}
		remoteIP := net.ParseIP(host)
		if remoteIP != nil {
			for _, addr := range routerAddresses {
				if remoteIP.String() == addr {
					result.Valid = false
					result.Conflicts = append(result.Conflicts, SubnetConflict{
						LocalSubnet:  addr,
						RemoteSubnet: tunnel.RemoteEndpoint,
						Description:  "Remote endpoint points to this router's own IP address - tunnel cannot function",
					})
				}
			}
		}
	}

	return result
}

// ValidateTunnelStart validates the tunnel can be safely started
// Returns an error if there are critical conflicts
func (m *P2PManager) ValidateTunnelStart(tunnel *config.WireGuardP2PTunnel, cfg *config.Config) error {
	result := m.ValidateTunnelConfig(tunnel, cfg)
	if !result.Valid {
		var msgs []string
		for _, c := range result.Conflicts {
			msgs = append(msgs, c.Description)
		}
		return fmt.Errorf("tunnel has routing conflicts that would break connectivity:\n- %s", strings.Join(msgs, "\n- "))
	}
	return nil
}

// StartTunnel brings up a tunnel interface
func (m *P2PManager) StartTunnel(ctx context.Context, ifaceName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	out, err := m.runner.Run(ctx, "wg-quick", "up", ifaceName)
	if err != nil {
		return fmt.Errorf("wg-quick up failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// StopTunnel brings down a tunnel interface
func (m *P2PManager) StopTunnel(ctx context.Context, ifaceName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	out, err := m.runner.Run(ctx, "wg-quick", "down", ifaceName)
	if err != nil {
		return fmt.Errorf("wg-quick down failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// RestartTunnel restarts a tunnel interface
func (m *P2PManager) RestartTunnel(ctx context.Context, ifaceName string) error {
	m.StopTunnel(ctx, ifaceName)
	return m.StartTunnel(ctx, ifaceName)
}

// IsRunning checks if a tunnel interface is up
func (m *P2PManager) IsRunning(ctx context.Context, ifaceName string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out, err := m.runner.Run(ctx, "ip", "link", "show", ifaceName)
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "UP")
}

// EnableTunnel enables the tunnel at boot
func (m *P2PManager) EnableTunnel(ctx context.Context, ifaceName string) error {
	return m.services.Enable(ctx, "wg-quick@"+ifaceName)
}

// DisableTunnel disables the tunnel at boot
func (m *P2PManager) DisableTunnel(ctx context.Context, ifaceName string) error {
	return m.services.Disable(ctx, "wg-quick@"+ifaceName)
}

// IsEnabled checks if the tunnel is enabled at boot
func (m *P2PManager) IsEnabled(ctx context.Context, ifaceName string) bool {
	out, _ := m.runner.Run(ctx, "systemctl", "is-enabled", "wg-quick@"+ifaceName)
	return strings.TrimSpace(string(out)) == "enabled"
}

// GetTunnelStatus returns the status of a specific tunnel
func (m *P2PManager) GetTunnelStatus(ctx context.Context, tunnel *config.WireGuardP2PTunnel) (*P2PStatus, error) {
	status := &P2PStatus{
		TunnelID:       tunnel.ID,
		Name:           tunnel.Name,
		Interface:      tunnel.Interface,
		ListenPort:     tunnel.ListenPort,
		Address:        tunnel.Address,
		RemoteEndpoint: tunnel.RemoteEndpoint,
		LocalSubnets:   tunnel.LocalSubnets,
		RemoteSubnets:  tunnel.RemoteSubnets,
		Running:        m.IsRunning(ctx, tunnel.Interface),
		Enabled:        m.IsEnabled(ctx, tunnel.Interface),
	}

	// Get public key from private key
	if tunnel.PrivateKey != "" {
		pubKey, err := m.GetPublicKeyFromPrivate(ctx, tunnel.PrivateKey)
		if err == nil {
			status.PublicKey = pubKey
		}
	}

	// Get live status if running
	if status.Running {
		if ifaceStatus, err := m.getInterfaceStatus(ctx, tunnel.Interface); err == nil {
			if len(ifaceStatus.Peers) > 0 {
				peer := ifaceStatus.Peers[0]
				status.LatestHandshake = peer.LatestHandshake
				status.TransferRx = peer.TransferRx
				status.TransferTx = peer.TransferTx

				// Connected if handshake within last 3 minutes
				cutoff := time.Now().Add(-3 * time.Minute)
				status.Connected = !peer.LatestHandshake.IsZero() && peer.LatestHandshake.After(cutoff)
			}
		}
	}

	return status, nil
}

// GetAllStatus returns status for all tunnels
func (m *P2PManager) GetAllStatus(ctx context.Context, tunnels []config.WireGuardP2PTunnel) ([]*P2PStatus, error) {
	statuses := make([]*P2PStatus, 0, len(tunnels))
	for i := range tunnels {
		status, err := m.GetTunnelStatus(ctx, &tunnels[i])
		if err != nil {
			continue
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

// getInterfaceStatus gets the live status of a WireGuard interface
func (m *P2PManager) getInterfaceStatus(ctx context.Context, ifaceName string) (*InterfaceStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out, err := m.runner.Run(ctx, "wg", "show", ifaceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get wg status: %w", err)
	}

	return parseWgShow(ifaceName, string(out))
}

// ParsedConfig represents a parsed WireGuard configuration file
type ParsedConfig struct {
	// Interface section
	PrivateKey string `json:"private_key,omitempty"`
	Address    string `json:"address,omitempty"`
	ListenPort int    `json:"listen_port,omitempty"`
	// Peer section (first peer only for site-to-site)
	RemotePublicKey     string   `json:"remote_public_key,omitempty"`
	RemoteEndpoint      string   `json:"remote_endpoint,omitempty"`
	PresharedKey        string   `json:"preshared_key,omitempty"`
	AllowedIPs          []string `json:"allowed_ips,omitempty"`
	PersistentKeepalive int      `json:"persistent_keepalive,omitempty"`
}

// ParseConfig parses a WireGuard configuration string and extracts settings
func (m *P2PManager) ParseConfig(configStr string) (*ParsedConfig, error) {
	result := &ParsedConfig{}
	lines := strings.Split(configStr, "\n")

	currentSection := ""

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Check for section headers
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = strings.ToLower(strings.Trim(line, "[]"))
			continue
		}

		// Parse key = value pairs
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(strings.ToLower(parts[0]))
		value := strings.TrimSpace(parts[1])

		switch currentSection {
		case "interface":
			switch key {
			case "privatekey":
				result.PrivateKey = value
			case "address":
				result.Address = value
			case "listenport":
				fmt.Sscanf(value, "%d", &result.ListenPort)
			}
		case "peer":
			switch key {
			case "publickey":
				result.RemotePublicKey = value
			case "endpoint":
				result.RemoteEndpoint = value
			case "presharedkey":
				result.PresharedKey = value
			case "allowedips":
				// Parse comma-separated list
				ips := strings.Split(value, ",")
				for _, ip := range ips {
					ip = strings.TrimSpace(ip)
					if ip != "" {
						result.AllowedIPs = append(result.AllowedIPs, ip)
					}
				}
			case "persistentkeepalive":
				fmt.Sscanf(value, "%d", &result.PersistentKeepalive)
			}
		}
	}

	// Validate we got at least some useful data
	if result.PrivateKey == "" && result.RemotePublicKey == "" {
		return nil, fmt.Errorf("invalid config: missing both PrivateKey and peer PublicKey")
	}

	return result, nil
}

// CreateTunnelFromConfig creates a tunnel configuration from a parsed config
func (m *P2PManager) CreateTunnelFromConfig(ctx context.Context, name string, parsed *ParsedConfig, existing []config.WireGuardP2PTunnel) (*config.WireGuardP2PTunnel, error) {
	tunnel := &config.WireGuardP2PTunnel{
		ID:                  fmt.Sprintf("tunnel-%d", time.Now().UnixNano()),
		Name:                name,
		Enabled:             true,
		Interface:           m.AllocateInterface(existing),
		ListenPort:          parsed.ListenPort,
		PrivateKey:          parsed.PrivateKey,
		Address:             parsed.Address,
		RemotePublicKey:     parsed.RemotePublicKey,
		RemoteEndpoint:      parsed.RemoteEndpoint,
		PresharedKey:        parsed.PresharedKey,
		PersistentKeepalive: parsed.PersistentKeepalive,
	}

	// If no listen port, allocate one
	if tunnel.ListenPort == 0 {
		tunnel.ListenPort = m.AllocatePort(existing)
	}

	// If no private key, generate one
	if tunnel.PrivateKey == "" {
		keyPair, err := m.GenerateKeyPair(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to generate key pair: %w", err)
		}
		tunnel.PrivateKey = keyPair.PrivateKey
	}

	// Set default keepalive if not specified
	if tunnel.PersistentKeepalive == 0 {
		tunnel.PersistentKeepalive = 25
	}

	// Parse AllowedIPs to extract remote subnets
	// AllowedIPs typically contains the peer's tunnel IP and their subnets
	if len(parsed.AllowedIPs) > 0 {
		tunnel.RemoteSubnets = parsed.AllowedIPs
	}

	return tunnel, nil
}

// GenerateRemoteConfig generates configuration for the remote peer
func (m *P2PManager) GenerateRemoteConfig(ctx context.Context, tunnel *config.WireGuardP2PTunnel, localEndpoint string) (*RemotePeerConfig, error) {
	// Get our public key
	pubKey, err := m.GetPublicKeyFromPrivate(ctx, tunnel.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get public key: %w", err)
	}

	// Calculate suggested address for remote (peer IP)
	suggestedAddr := m.CalculatePeerIP(tunnel.Address)

	// Our tunnel IP (without mask)
	ourIP := strings.Split(tunnel.Address, "/")[0] + "/32"

	// Build AllowedIPs for remote: our tunnel IP + our local subnets
	allowedIPs := []string{ourIP}
	allowedIPs = append(allowedIPs, tunnel.LocalSubnets...)

	// Generate full WireGuard config for remote side
	var configSb strings.Builder
	configSb.WriteString("[Interface]\n")
	configSb.WriteString("# Remote site configuration for: " + tunnel.Name + "\n")
	configSb.WriteString("# Replace <REMOTE_PRIVATE_KEY> with your private key\n")
	configSb.WriteString("PrivateKey = <REMOTE_PRIVATE_KEY>\n")
	configSb.WriteString(fmt.Sprintf("Address = %s\n", suggestedAddr))
	configSb.WriteString("# ListenPort = <YOUR_PORT>\n")
	configSb.WriteString("\n")
	configSb.WriteString("# PostUp/PostDown for forwarding\n")
	configSb.WriteString("PostUp = iptables -A FORWARD -i %i -j ACCEPT; iptables -A FORWARD -o %i -j ACCEPT\n")
	configSb.WriteString("PostDown = iptables -D FORWARD -i %i -j ACCEPT; iptables -D FORWARD -o %i -j ACCEPT\n")
	configSb.WriteString("\n")
	configSb.WriteString("[Peer]\n")
	configSb.WriteString(fmt.Sprintf("# %s\n", tunnel.Name))
	configSb.WriteString(fmt.Sprintf("PublicKey = %s\n", pubKey))
	if tunnel.PresharedKey != "" {
		configSb.WriteString(fmt.Sprintf("PresharedKey = %s\n", tunnel.PresharedKey))
	}
	configSb.WriteString(fmt.Sprintf("AllowedIPs = %s\n", strings.Join(allowedIPs, ", ")))
	configSb.WriteString(fmt.Sprintf("Endpoint = %s:%d\n", localEndpoint, tunnel.ListenPort))
	configSb.WriteString("PersistentKeepalive = 25\n")

	return &RemotePeerConfig{
		TunnelName:       tunnel.Name,
		LocalPublicKey:   pubKey,
		LocalEndpoint:    fmt.Sprintf("%s:%d", localEndpoint, tunnel.ListenPort),
		LocalSubnets:     tunnel.LocalSubnets,
		PresharedKey:     tunnel.PresharedKey,
		SuggestedAddress: suggestedAddr,
		WireGuardConfig:  configSb.String(),
	}, nil
}
