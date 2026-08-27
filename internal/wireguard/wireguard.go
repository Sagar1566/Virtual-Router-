package wireguard

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iodesystems/ubuntu-router/internal/config"
	"github.com/iodesystems/ubuntu-router/internal/system"
)

// PeerStatus represents the live status of a WireGuard peer
type PeerStatus struct {
	PublicKey           string    `json:"public_key"`
	Endpoint            string    `json:"endpoint,omitempty"`
	AllowedIPs          []string  `json:"allowed_ips"`
	LatestHandshake     time.Time `json:"latest_handshake,omitempty"`
	TransferRx          int64     `json:"transfer_rx"`
	TransferTx          int64     `json:"transfer_tx"`
	PersistentKeepalive int       `json:"persistent_keepalive,omitempty"`
}

// InterfaceStatus represents the live status of a WireGuard interface
type InterfaceStatus struct {
	Interface  string       `json:"interface"`
	PublicKey  string       `json:"public_key"`
	ListenPort int          `json:"listen_port"`
	Peers      []PeerStatus `json:"peers"`
}

// ClientConfig contains a generated client configuration
type ClientConfig struct {
	Name       string `json:"name"`
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
	Address    string `json:"address"`
	DNS        string `json:"dns"`
	Config     string `json:"config"`
	QRCode     string `json:"qr_code,omitempty"`
}

// Manager handles WireGuard configuration and operations
type Manager struct {
	mu         sync.RWMutex
	fs         system.FileSystem
	runner     system.CommandRunner
	services   *system.ServiceManager
	configPath string
	ifaceName  string
}

// New creates a new WireGuard manager
func New(fs system.FileSystem, runner system.CommandRunner, configPath, ifaceName string) *Manager {
	return &Manager{
		fs:         fs,
		runner:     runner,
		services:   system.NewServiceManager(runner),
		configPath: configPath,
		ifaceName:  ifaceName,
	}
}

// GenerateKeyPair generates a new WireGuard key pair
func GenerateKeyPair() (privateKey, publicKey string, err error) {
	// Generate 32 bytes of random data for private key
	privBytes := make([]byte, 32)
	if _, err := rand.Read(privBytes); err != nil {
		return "", "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	// Clamp the private key per WireGuard spec
	privBytes[0] &= 248
	privBytes[31] &= 127
	privBytes[31] |= 64

	privateKey = base64.StdEncoding.EncodeToString(privBytes)

	// Generate public key from private key using curve25519
	// For simplicity, we'll use wg command to generate the public key
	return privateKey, "", nil
}

// GenerateKeyPairWithWg generates a key pair using wg command
func (m *Manager) GenerateKeyPairWithWg(ctx context.Context) (privateKey, publicKey string, err error) {
	// Generate private key
	privOut, err := m.runner.Run(ctx, "wg", "genkey")
	if err != nil {
		return "", "", fmt.Errorf("failed to generate private key: %w", err)
	}
	privateKey = strings.TrimSpace(string(privOut))

	// Generate public key from private key
	pubOut, err := m.runner.RunWithStdin(ctx, strings.NewReader(privateKey), "wg", "pubkey")
	if err != nil {
		return "", "", fmt.Errorf("failed to generate public key: %w", err)
	}
	publicKey = strings.TrimSpace(string(pubOut))

	return privateKey, publicKey, nil
}

// GeneratePresharedKey generates a preshared key
func (m *Manager) GeneratePresharedKey(ctx context.Context) (string, error) {
	out, err := m.runner.Run(ctx, "wg", "genpsk")
	if err != nil {
		return "", fmt.Errorf("failed to generate preshared key: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// GetPublicKeyFromPrivate derives public key from a private key using wg pubkey
func (m *Manager) GetPublicKeyFromPrivate(ctx context.Context, privateKey string) (string, error) {
	if privateKey == "" {
		return "", fmt.Errorf("no private key provided")
	}
	pubOut, err := m.runner.RunWithStdin(ctx, strings.NewReader(privateKey), "wg", "pubkey")
	if err != nil {
		return "", fmt.Errorf("failed to derive public key: %w", err)
	}
	return strings.TrimSpace(string(pubOut)), nil
}

// WriteConfig writes the WireGuard configuration file
func (m *Manager) WriteConfig(cfg *config.WireGuardConfig, serverEndpoint string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var sb strings.Builder

	// [Interface] section
	sb.WriteString("[Interface]\n")
	sb.WriteString(fmt.Sprintf("PrivateKey = %s\n", cfg.PrivateKey))
	sb.WriteString(fmt.Sprintf("Address = %s\n", cfg.Address))
	sb.WriteString(fmt.Sprintf("ListenPort = %d\n", cfg.ListenPort))

	// PostUp/PostDown for NAT
	sb.WriteString("PostUp = iptables -A FORWARD -i %i -j ACCEPT; iptables -t nat -A POSTROUTING -o eth0 -j MASQUERADE\n")
	sb.WriteString("PostDown = iptables -D FORWARD -i %i -j ACCEPT; iptables -t nat -D POSTROUTING -o eth0 -j MASQUERADE\n")
	sb.WriteString("\n")

	// [Peer] sections
	for _, peer := range cfg.Peers {
		sb.WriteString("[Peer]\n")
		sb.WriteString(fmt.Sprintf("# %s\n", peer.Name))
		sb.WriteString(fmt.Sprintf("PublicKey = %s\n", peer.PublicKey))
		if peer.PresharedKey != "" {
			sb.WriteString(fmt.Sprintf("PresharedKey = %s\n", peer.PresharedKey))
		}
		sb.WriteString(fmt.Sprintf("AllowedIPs = %s\n", strings.Join(peer.AllowedIPs, ", ")))
		if peer.Endpoint != "" {
			sb.WriteString(fmt.Sprintf("Endpoint = %s\n", peer.Endpoint))
		}
		sb.WriteString("\n")
	}

	return m.fs.WriteFile(m.configPath, []byte(sb.String()), 0600)
}

// EnsureConfig ensures a WireGuard config exists, creating defaults if needed.
// Returns whether config was newly created.
func (m *Manager) EnsureConfig(cfg *config.WireGuardConfig, wanInterface string) (bool, error) {
	return m.EnsureConfigWithContext(context.Background(), cfg, wanInterface)
}

// EnsureConfigWithContext ensures a WireGuard config exists, creating defaults if needed.
// Returns whether config was newly created.
func (m *Manager) EnsureConfigWithContext(ctx context.Context, cfg *config.WireGuardConfig, wanInterface string) (bool, error) {
	// Check if config file already exists
	if _, err := m.fs.ReadFile(m.configPath); err == nil {
		return false, nil // Config exists
	}

	// Ensure config directory exists with correct permissions
	if err := m.EnsureConfigDir(); err != nil {
		return false, err
	}

	// Generate a private key if not set
	if cfg.PrivateKey == "" {
		privKey, _, err := m.GenerateKeyPairWithWg(ctx)
		if err != nil {
			return false, fmt.Errorf("failed to generate key pair: %w", err)
		}
		cfg.PrivateKey = privKey
	}

	// Set defaults
	if cfg.Address == "" {
		cfg.Address = "10.0.0.1/24"
	}
	if cfg.ListenPort == 0 {
		cfg.ListenPort = 51820
	}
	if cfg.Interface == "" {
		cfg.Interface = "wg0"
	}

	// Write the config with WAN interface for NAT rules
	if err := m.WriteConfigWithWAN(cfg, wanInterface); err != nil {
		return false, err
	}

	return true, nil
}

// WriteConfigWithWAN writes config using the specified WAN interface for NAT
func (m *Manager) WriteConfigWithWAN(cfg *config.WireGuardConfig, wanInterface string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if wanInterface == "" {
		wanInterface = "eth0"
	}

	var sb strings.Builder

	// [Interface] section
	sb.WriteString("[Interface]\n")
	sb.WriteString(fmt.Sprintf("PrivateKey = %s\n", cfg.PrivateKey))
	sb.WriteString(fmt.Sprintf("Address = %s\n", cfg.Address))
	sb.WriteString(fmt.Sprintf("ListenPort = %d\n", cfg.ListenPort))

	// PostUp/PostDown rules depend on RouteLAN and RouteAllTraffic settings
	// We need forwarding for VPN traffic
	postUp := []string{"iptables -A FORWARD -i %i -j ACCEPT", "iptables -A FORWARD -o %i -j ACCEPT"}
	postDown := []string{"iptables -D FORWARD -i %i -j ACCEPT", "iptables -D FORWARD -o %i -j ACCEPT"}

	// Add masquerade for internet access or LAN access
	if cfg.RouteAllTraffic || cfg.RouteLAN {
		postUp = append(postUp, fmt.Sprintf("iptables -t nat -A POSTROUTING -o %s -j MASQUERADE", wanInterface))
		postDown = append(postDown, fmt.Sprintf("iptables -t nat -D POSTROUTING -o %s -j MASQUERADE", wanInterface))
	}

	sb.WriteString(fmt.Sprintf("PostUp = %s\n", strings.Join(postUp, "; ")))
	sb.WriteString(fmt.Sprintf("PostDown = %s\n", strings.Join(postDown, "; ")))
	sb.WriteString("\n")

	// [Peer] sections
	for _, peer := range cfg.Peers {
		sb.WriteString("[Peer]\n")
		sb.WriteString(fmt.Sprintf("# %s\n", peer.Name))
		sb.WriteString(fmt.Sprintf("PublicKey = %s\n", peer.PublicKey))
		if peer.PresharedKey != "" {
			sb.WriteString(fmt.Sprintf("PresharedKey = %s\n", peer.PresharedKey))
		}
		sb.WriteString(fmt.Sprintf("AllowedIPs = %s\n", strings.Join(peer.AllowedIPs, ", ")))
		if peer.Endpoint != "" {
			sb.WriteString(fmt.Sprintf("Endpoint = %s\n", peer.Endpoint))
		}
		sb.WriteString("\n")
	}

	return m.fs.WriteFile(m.configPath, []byte(sb.String()), 0600)
}

// ConfigExists checks if the WireGuard config file exists
func (m *Manager) ConfigExists() bool {
	_, err := m.fs.ReadFile(m.configPath)
	return err == nil
}

// EnsureConfigDir ensures the config directory exists with correct permissions (0700)
func (m *Manager) EnsureConfigDir() error {
	dir := filepath.Dir(m.configPath)
	if err := m.fs.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create WireGuard config directory %s: %w", dir, err)
	}
	return nil
}

// IsWireGuardInterface checks if the interface is actually a WireGuard interface
// by checking if it exists in /sys/class/net/<iface>/device/uevent as a wireguard device
// or by attempting to query it with 'wg show'
func (m *Manager) IsWireGuardInterface(ctx context.Context) bool {
	// Try wg show - this will succeed only if it's a valid WireGuard interface
	_, err := m.runner.Run(ctx, "wg", "show", m.ifaceName)
	return err == nil
}

// ReadConfig reads and parses the current WireGuard configuration
func (m *Manager) ReadConfig() (*config.WireGuardConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := m.fs.ReadFile(m.configPath)
	if err != nil {
		return nil, err
	}

	cfg := &config.WireGuardConfig{
		Interface:  m.ifaceName,
		ConfigPath: m.configPath,
	}

	var currentPeer *config.WireGuardPeer
	scanner := bufio.NewScanner(strings.NewReader(string(data)))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			// Check for peer name comment
			if currentPeer != nil && strings.HasPrefix(line, "#") {
				currentPeer.Name = strings.TrimPrefix(line, "# ")
			}
			continue
		}

		if line == "[Interface]" {
			currentPeer = nil
			continue
		}

		if line == "[Peer]" {
			currentPeer = &config.WireGuardPeer{}
			cfg.Peers = append(cfg.Peers, *currentPeer)
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		if currentPeer == nil {
			// Interface section
			switch key {
			case "PrivateKey":
				cfg.PrivateKey = value
			case "Address":
				cfg.Address = value
			case "ListenPort":
				cfg.ListenPort, _ = strconv.Atoi(value)
			case "DNS":
				cfg.DNS = value
			}
		} else {
			// Peer section - update the last peer
			idx := len(cfg.Peers) - 1
			switch key {
			case "PublicKey":
				cfg.Peers[idx].PublicKey = value
			case "PresharedKey":
				cfg.Peers[idx].PresharedKey = value
			case "AllowedIPs":
				cfg.Peers[idx].AllowedIPs = strings.Split(strings.ReplaceAll(value, " ", ""), ",")
			case "Endpoint":
				cfg.Peers[idx].Endpoint = value
			}
		}
	}

	return cfg, nil
}

// GetStatus returns the live status of the WireGuard interface
func (m *Manager) GetStatus(ctx context.Context) (*InterfaceStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out, err := m.runner.Run(ctx, "wg", "show", m.ifaceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get wg status: %w", err)
	}

	return parseWgShow(m.ifaceName, string(out))
}

func parseWgShow(ifaceName, output string) (*InterfaceStatus, error) {
	status := &InterfaceStatus{
		Interface: ifaceName,
	}

	var currentPeer *PeerStatus
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "public key":
			if currentPeer == nil {
				status.PublicKey = value
			}
		case "listening port":
			status.ListenPort, _ = strconv.Atoi(value)
		case "peer":
			if currentPeer != nil {
				status.Peers = append(status.Peers, *currentPeer)
			}
			currentPeer = &PeerStatus{PublicKey: value}
		case "endpoint":
			if currentPeer != nil {
				currentPeer.Endpoint = value
			}
		case "allowed ips":
			if currentPeer != nil {
				currentPeer.AllowedIPs = strings.Split(strings.ReplaceAll(value, " ", ""), ",")
			}
		case "latest handshake":
			if currentPeer != nil {
				currentPeer.LatestHandshake = parseHandshakeTime(value)
			}
		case "transfer":
			if currentPeer != nil {
				rx, tx := parseTransfer(value)
				currentPeer.TransferRx = rx
				currentPeer.TransferTx = tx
			}
		case "persistent keepalive":
			if currentPeer != nil {
				currentPeer.PersistentKeepalive = parseKeepalive(value)
			}
		}
	}

	if currentPeer != nil {
		status.Peers = append(status.Peers, *currentPeer)
	}

	return status, nil
}

func parseHandshakeTime(value string) time.Time {
	// Format: "X minutes, Y seconds ago" or "X hours, Y minutes, Z seconds ago"
	if strings.Contains(value, "ago") {
		// Parse duration
		re := regexp.MustCompile(`(\d+)\s*(second|minute|hour|day)s?`)
		matches := re.FindAllStringSubmatch(value, -1)
		var duration time.Duration
		for _, match := range matches {
			num, _ := strconv.Atoi(match[1])
			switch match[2] {
			case "second":
				duration += time.Duration(num) * time.Second
			case "minute":
				duration += time.Duration(num) * time.Minute
			case "hour":
				duration += time.Duration(num) * time.Hour
			case "day":
				duration += time.Duration(num) * 24 * time.Hour
			}
		}
		return time.Now().Add(-duration)
	}
	return time.Time{}
}

func parseTransfer(value string) (rx, tx int64) {
	// Format: "X.XX MiB received, Y.YY MiB sent"
	re := regexp.MustCompile(`([\d.]+)\s*(\w+)\s+received.*?([\d.]+)\s*(\w+)\s+sent`)
	matches := re.FindStringSubmatch(value)
	if len(matches) >= 5 {
		rx = parseBytes(matches[1], matches[2])
		tx = parseBytes(matches[3], matches[4])
	}
	return
}

func parseBytes(value, unit string) int64 {
	f, _ := strconv.ParseFloat(value, 64)
	switch strings.ToLower(unit) {
	case "b":
		return int64(f)
	case "kib":
		return int64(f * 1024)
	case "mib":
		return int64(f * 1024 * 1024)
	case "gib":
		return int64(f * 1024 * 1024 * 1024)
	}
	return int64(f)
}

func parseKeepalive(value string) int {
	// Format: "every X seconds"
	re := regexp.MustCompile(`every\s+(\d+)\s+seconds?`)
	if matches := re.FindStringSubmatch(value); len(matches) >= 2 {
		n, _ := strconv.Atoi(matches[1])
		return n
	}
	return 0
}

// Start brings up the WireGuard interface
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	out, err := m.runner.Run(ctx, "wg-quick", "up", m.ifaceName)
	if err != nil {
		return fmt.Errorf("wg-quick up failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// Stop brings down the WireGuard interface
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	out, err := m.runner.Run(ctx, "wg-quick", "down", m.ifaceName)
	if err != nil {
		return fmt.Errorf("wg-quick down failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// Restart restarts the WireGuard interface
func (m *Manager) Restart(ctx context.Context) error {
	m.Stop(ctx)
	return m.Start(ctx)
}

// IsRunning checks if the WireGuard interface is up
func (m *Manager) IsRunning(ctx context.Context) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out, err := m.runner.Run(ctx, "ip", "link", "show", m.ifaceName)
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "UP")
}

// AddPeer adds a new peer to the configuration
func (m *Manager) AddPeer(ctx context.Context, cfg *config.WireGuardConfig, peer config.WireGuardPeer) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for duplicate
	for _, p := range cfg.Peers {
		if p.PublicKey == peer.PublicKey {
			return fmt.Errorf("peer with public key already exists")
		}
		if p.Name == peer.Name {
			return fmt.Errorf("peer with name %s already exists", peer.Name)
		}
	}

	cfg.Peers = append(cfg.Peers, peer)
	return nil
}

// RemovePeer removes a peer from the configuration
func (m *Manager) RemovePeer(ctx context.Context, cfg *config.WireGuardConfig, publicKey string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var newPeers []config.WireGuardPeer
	found := false
	for _, p := range cfg.Peers {
		if p.PublicKey == publicKey {
			found = true
			continue
		}
		newPeers = append(newPeers, p)
	}

	if !found {
		return fmt.Errorf("peer not found")
	}

	cfg.Peers = newPeers
	return nil
}

// GenerateClientConfig generates a client configuration for a new peer
func (m *Manager) GenerateClientConfig(ctx context.Context, cfg *config.WireGuardConfig, name, serverEndpoint, serverPublicKey string) (*ClientConfig, error) {
	// Generate key pair for client
	privKey, pubKey, err := m.GenerateKeyPairWithWg(ctx)
	if err != nil {
		return nil, err
	}

	// Generate preshared key
	psk, err := m.GeneratePresharedKey(ctx)
	if err != nil {
		return nil, err
	}

	// Find next available IP
	clientIP, err := m.findNextIP(cfg)
	if err != nil {
		return nil, err
	}

	// DNS server (usually the WireGuard server's IP)
	dns := cfg.DNS
	if dns == "" {
		// Extract server IP from address
		serverIP := strings.Split(cfg.Address, "/")[0]
		dns = serverIP
	}

	// Determine AllowedIPs for client based on server settings
	var clientAllowedIPs []string
	if cfg.RouteAllTraffic {
		// Route all traffic through VPN
		clientAllowedIPs = []string{"0.0.0.0/0", "::/0"}
	} else if cfg.RouteLAN {
		// Route only VPN network and LAN subnets
		clientAllowedIPs = []string{cfg.Address} // VPN network
		if len(cfg.LANSubnets) > 0 {
			clientAllowedIPs = append(clientAllowedIPs, cfg.LANSubnets...)
		}
	} else {
		// Only route VPN network
		clientAllowedIPs = []string{cfg.Address}
	}

	// Generate client config
	var sb strings.Builder
	sb.WriteString("[Interface]\n")
	sb.WriteString(fmt.Sprintf("PrivateKey = %s\n", privKey))
	sb.WriteString(fmt.Sprintf("Address = %s/32\n", clientIP))
	sb.WriteString(fmt.Sprintf("DNS = %s\n", dns))
	sb.WriteString("\n")
	sb.WriteString("[Peer]\n")
	sb.WriteString(fmt.Sprintf("PublicKey = %s\n", serverPublicKey))
	sb.WriteString(fmt.Sprintf("PresharedKey = %s\n", psk))
	sb.WriteString(fmt.Sprintf("AllowedIPs = %s\n", strings.Join(clientAllowedIPs, ", ")))
	sb.WriteString(fmt.Sprintf("Endpoint = %s:%d\n", serverEndpoint, cfg.ListenPort))
	sb.WriteString("PersistentKeepalive = 25\n")

	return &ClientConfig{
		Name:       name,
		PrivateKey: privKey,
		PublicKey:  pubKey,
		Address:    clientIP + "/32",
		DNS:        dns,
		Config:     sb.String(),
	}, nil
}

func (m *Manager) findNextIP(cfg *config.WireGuardConfig) (string, error) {
	// Parse server address to get network
	serverIP, ipNet, err := net.ParseCIDR(cfg.Address)
	if err != nil {
		return "", fmt.Errorf("invalid server address: %w", err)
	}

	// Get used IPs
	usedIPs := make(map[string]bool)
	usedIPs[serverIP.String()] = true
	for _, peer := range cfg.Peers {
		for _, allowed := range peer.AllowedIPs {
			ip := strings.Split(allowed, "/")[0]
			usedIPs[ip] = true
		}
	}

	// Find next available IP
	ip := serverIP.To4()
	if ip == nil {
		return "", fmt.Errorf("only IPv4 supported")
	}

	for ip := ip.Mask(ipNet.Mask); ipNet.Contains(ip); inc(ip) {
		if !usedIPs[ip.String()] && !ip.Equal(serverIP) {
			// Skip network and broadcast addresses
			if ip[3] == 0 || ip[3] == 255 {
				continue
			}
			return ip.String(), nil
		}
	}

	return "", fmt.Errorf("no available IP addresses in range")
}

func inc(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

// SyncConfig applies configuration changes without full restart
func (m *Manager) SyncConfig(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.runner.Run(ctx, "wg", "syncconf", m.ifaceName, m.configPath)
	return err
}

// Enable enables the WireGuard interface at boot via wg-quick@service
func (m *Manager) Enable(ctx context.Context) error {
	return m.services.Enable(ctx, "wg-quick@"+m.ifaceName)
}

// Disable disables the WireGuard interface at boot
func (m *Manager) Disable(ctx context.Context) error {
	return m.services.Disable(ctx, "wg-quick@"+m.ifaceName)
}

// Status represents the overall WireGuard status
type Status struct {
	Enabled         bool             `json:"enabled"`
	Running         bool             `json:"running"`
	Interface       string           `json:"interface"`
	PublicKey       string           `json:"public_key"`
	ListenPort      int              `json:"listen_port"`
	Address         string           `json:"address"`
	PeerCount       int              `json:"peer_count"`
	ActivePeers     int              `json:"active_peers"`
	TotalRx         int64            `json:"total_rx"`
	TotalTx         int64            `json:"total_tx"`
	InterfaceStatus *InterfaceStatus `json:"interface_status,omitempty"`
}

// GetFullStatus returns comprehensive status
func (m *Manager) GetFullStatus(ctx context.Context, cfg *config.WireGuardConfig) (*Status, error) {
	status := &Status{
		Interface:  m.ifaceName,
		Running:    m.IsRunning(ctx),
		Address:    cfg.Address,
		ListenPort: cfg.ListenPort,
		PeerCount:  len(cfg.Peers),
	}

	// Check if enabled
	enabledOut, _ := m.runner.Run(ctx, "systemctl", "is-enabled", "wg-quick@"+m.ifaceName)
	status.Enabled = strings.TrimSpace(string(enabledOut)) == "enabled"

	// Get live status if running
	if status.Running {
		if ifaceStatus, err := m.GetStatus(ctx); err == nil {
			status.InterfaceStatus = ifaceStatus
			status.PublicKey = ifaceStatus.PublicKey

			// Count active peers (handshake within last 3 minutes)
			cutoff := time.Now().Add(-3 * time.Minute)
			for _, peer := range ifaceStatus.Peers {
				if !peer.LatestHandshake.IsZero() && peer.LatestHandshake.After(cutoff) {
					status.ActivePeers++
				}
				status.TotalRx += peer.TransferRx
				status.TotalTx += peer.TransferTx
			}
		}
	}

	return status, nil
}
