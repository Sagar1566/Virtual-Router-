// Package qos provides Quality of Service management using Linux traffic control.
package qos

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/iodesystems/ubuntu-router/internal/system"
)

// Mode represents the QoS mode
type Mode string

const (
	ModeCake Mode = "cake"
	ModeHTB  Mode = "htb"
	ModeNone Mode = "none"
)

// Config represents QoS configuration
type Config struct {
	Enabled          bool          `json:"enabled"`
	Mode             Mode          `json:"mode"`
	WANInterface     string        `json:"wan_interface"`
	LANInterface     string        `json:"lan_interface"`
	DownloadSpeed    string        `json:"download_speed"`    // e.g., "100mbit"
	UploadSpeed      string        `json:"upload_speed"`      // e.g., "20mbit"
	PerHostFairness  bool          `json:"per_host_fairness"` // CAKE: fair sharing per LAN host
	RTTCompensation  string        `json:"rtt_compensation"`  // CAKE: regional, internet, oceanic, satellite
	WANType          string        `json:"wan_type"`          // CAKE: ethernet, docsis, pppoe, etc.
	ClientLimits     []ClientLimit `json:"client_limits"`
	PriorityRules    []PriorityRule `json:"priority_rules"`
}

// ClientLimit defines bandwidth limits for a specific client
type ClientLimit struct {
	Name          string `json:"name"`
	IP            string `json:"ip"`
	MAC           string `json:"mac,omitempty"`
	DownloadSpeed string `json:"download_speed"`
	UploadSpeed   string `json:"upload_speed"`
}

// PriorityRule defines traffic prioritization rules
type PriorityRule struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"` // tcp, udp, icmp
	DstPorts []int  `json:"dst_ports,omitempty"`
	SrcPorts []int  `json:"src_ports,omitempty"`
	DSCP     string `json:"dscp,omitempty"`
	Priority int    `json:"priority"` // 1=highest, lower=bulk
}

// Status represents current QoS status
type Status struct {
	Enabled       bool              `json:"enabled"`
	Mode          Mode              `json:"mode"`
	WANInterface  string            `json:"wan_interface"`
	LANInterface  string            `json:"lan_interface"`
	WANQdisc      *QdiscStats       `json:"wan_qdisc,omitempty"`
	LANQdisc      *QdiscStats       `json:"lan_qdisc,omitempty"`
	DownloadSpeed string            `json:"download_speed"`
	UploadSpeed   string            `json:"upload_speed"`
}

// QdiscStats represents statistics for a queueing discipline
type QdiscStats struct {
	Type       string `json:"type"`
	Handle     string `json:"handle"`
	Parent     string `json:"parent"`
	Bandwidth  string `json:"bandwidth,omitempty"`

	// Traffic stats
	Bytes      uint64 `json:"bytes"`
	Packets    uint64 `json:"packets"`
	Dropped    uint64 `json:"dropped"`
	Overlimits uint64 `json:"overlimits"`
	Requeues   uint64 `json:"requeues"`
	Backlog    uint64 `json:"backlog"`

	// CAKE specific
	TargetDelay   string `json:"target_delay,omitempty"`
	PeakDelayUs   uint64 `json:"peak_delay_us,omitempty"`
	AvgDelayUs    uint64 `json:"avg_delay_us,omitempty"`
	BaseDelayUs   uint64 `json:"base_delay_us,omitempty"`
	MemoryUsed    uint64 `json:"memory_used,omitempty"`
	MemoryLimit   uint64 `json:"memory_limit,omitempty"`
}

// Manager handles QoS configuration
type Manager struct {
	mu     sync.RWMutex
	runner system.CommandRunner
	config *Config
}

// New creates a new QoS Manager
func New(runner system.CommandRunner) *Manager {
	return &Manager{
		runner: runner,
		config: &Config{
			Enabled: false,
			Mode:    ModeCake,
		},
	}
}

// Configure applies QoS configuration
func (m *Manager) Configure(ctx context.Context, cfg *Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Remove existing qdiscs first
	if m.config != nil && m.config.Enabled {
		m.removeQdiscs(ctx, m.config.WANInterface, m.config.LANInterface)
	}

	m.config = cfg

	if !cfg.Enabled {
		return nil
	}

	switch cfg.Mode {
	case ModeCake:
		return m.applyCake(ctx, cfg)
	case ModeHTB:
		return fmt.Errorf("HTB mode not yet implemented")
	default:
		return nil
	}
}

// applyCake applies CAKE qdisc configuration
func (m *Manager) applyCake(ctx context.Context, cfg *Config) error {
	// Apply egress shaping on WAN (upload limiting)
	if cfg.WANInterface != "" && cfg.UploadSpeed != "" {
		args := m.buildCakeArgs(cfg.UploadSpeed, cfg, true)
		if err := m.applyQdisc(ctx, cfg.WANInterface, "root", args); err != nil {
			return fmt.Errorf("failed to apply CAKE on WAN egress: %w", err)
		}
	}

	// Apply ingress shaping on WAN via IFB (download limiting)
	// CAKE can't do ingress directly, so we use an IFB device
	if cfg.WANInterface != "" && cfg.DownloadSpeed != "" {
		if err := m.setupIngressShaping(ctx, cfg); err != nil {
			return fmt.Errorf("failed to setup ingress shaping: %w", err)
		}
	}

	return nil
}

// buildCakeArgs constructs the tc qdisc arguments for CAKE
func (m *Manager) buildCakeArgs(bandwidth string, cfg *Config, isEgress bool) []string {
	args := []string{"cake", "bandwidth", bandwidth}

	// Add RTT compensation
	switch cfg.RTTCompensation {
	case "regional":
		args = append(args, "regional")
	case "internet":
		args = append(args, "internet")
	case "oceanic":
		args = append(args, "oceanic")
	case "satellite":
		args = append(args, "satellite")
	default:
		args = append(args, "internet") // sensible default
	}

	// Add WAN type for overhead calculation
	switch cfg.WANType {
	case "ethernet":
		args = append(args, "ethernet")
	case "docsis":
		args = append(args, "docsis")
	case "pppoe-ptm":
		args = append(args, "pppoe-ptm")
	case "pppoe-llc":
		args = append(args, "pppoe-llcsnap")
	case "bridged-ptm":
		args = append(args, "bridged-ptm")
	case "bridged-llc":
		args = append(args, "bridged-llcsnap")
	default:
		args = append(args, "ethernet")
	}

	// Per-host fairness
	if cfg.PerHostFairness {
		if isEgress {
			// For egress (upload), fairness by source host
			args = append(args, "dual-srchost")
		} else {
			// For ingress (download), fairness by destination host
			args = append(args, "dual-dsthost")
		}
	} else {
		args = append(args, "flows")
	}

	// Enable ACK filtering for better performance on asymmetric links
	args = append(args, "ack-filter")

	// Use diffserv4 for basic priority handling
	args = append(args, "diffserv4")

	// NAT mode for proper host tracking behind router
	args = append(args, "nat")

	// Enable ECN for congestion signaling
	args = append(args, "ecn")

	return args
}

// setupIngressShaping sets up ingress shaping using IFB device
func (m *Manager) setupIngressShaping(ctx context.Context, cfg *Config) error {
	ifbDev := "ifb0"

	// Load IFB module
	_, _ = m.runner.Run(ctx, "modprobe", "ifb")

	// Create and bring up IFB device
	_, _ = m.runner.Run(ctx, "ip", "link", "add", ifbDev, "type", "ifb")
	_, _ = m.runner.Run(ctx, "ip", "link", "set", ifbDev, "up")

	// Add ingress qdisc to WAN interface
	_, _ = m.runner.Run(ctx, "tc", "qdisc", "del", "dev", cfg.WANInterface, "ingress")
	_, err := m.runner.Run(ctx, "tc", "qdisc", "add", "dev", cfg.WANInterface, "handle", "ffff:", "ingress")
	if err != nil {
		return fmt.Errorf("failed to add ingress qdisc: %w", err)
	}

	// Redirect ingress traffic to IFB
	_, err = m.runner.Run(ctx, "tc", "filter", "add", "dev", cfg.WANInterface,
		"parent", "ffff:", "protocol", "all", "u32", "match", "u32", "0", "0",
		"action", "mirred", "egress", "redirect", "dev", ifbDev)
	if err != nil {
		return fmt.Errorf("failed to add ingress redirect filter: %w", err)
	}

	// Apply CAKE to IFB device (this shapes the ingress/download)
	args := m.buildCakeArgs(cfg.DownloadSpeed, cfg, false)
	if err := m.applyQdisc(ctx, ifbDev, "root", args); err != nil {
		return fmt.Errorf("failed to apply CAKE on IFB: %w", err)
	}

	return nil
}

// applyQdisc applies a qdisc to an interface
func (m *Manager) applyQdisc(ctx context.Context, iface, parent string, args []string) error {
	// First try to replace, then add if it doesn't exist
	fullArgs := []string{"qdisc", "replace", "dev", iface, parent}
	fullArgs = append(fullArgs, args...)

	_, err := m.runner.Run(ctx, "tc", fullArgs...)
	if err != nil {
		// Try add instead
		fullArgs[1] = "add"
		_, err = m.runner.Run(ctx, "tc", fullArgs...)
	}
	return err
}

// removeQdiscs removes QoS qdiscs from interfaces
func (m *Manager) removeQdiscs(ctx context.Context, wanIface, lanIface string) {
	if wanIface != "" {
		// Remove root qdisc (egress)
		m.runner.Run(ctx, "tc", "qdisc", "del", "dev", wanIface, "root")
		// Remove ingress qdisc
		m.runner.Run(ctx, "tc", "qdisc", "del", "dev", wanIface, "ingress")
	}

	// Remove IFB device
	m.runner.Run(ctx, "ip", "link", "del", "ifb0")

	if lanIface != "" {
		m.runner.Run(ctx, "tc", "qdisc", "del", "dev", lanIface, "root")
	}
}

// GetStatus returns current QoS status
func (m *Manager) GetStatus(ctx context.Context) (*Status, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := &Status{
		Enabled:       m.config.Enabled,
		Mode:          m.config.Mode,
		WANInterface:  m.config.WANInterface,
		LANInterface:  m.config.LANInterface,
		DownloadSpeed: m.config.DownloadSpeed,
		UploadSpeed:   m.config.UploadSpeed,
	}

	if !m.config.Enabled {
		return status, nil
	}

	// Get WAN qdisc stats
	if m.config.WANInterface != "" {
		stats, err := m.getQdiscStats(ctx, m.config.WANInterface)
		if err == nil && stats != nil {
			status.WANQdisc = stats
		}
	}

	// Get IFB (ingress) qdisc stats
	stats, err := m.getQdiscStats(ctx, "ifb0")
	if err == nil && stats != nil {
		status.LANQdisc = stats
	}

	return status, nil
}

// getQdiscStats retrieves statistics for qdisc on an interface
func (m *Manager) getQdiscStats(ctx context.Context, iface string) (*QdiscStats, error) {
	output, err := m.runner.Run(ctx, "tc", "-s", "qdisc", "show", "dev", iface)
	if err != nil {
		return nil, err
	}

	return parseQdiscStats(string(output))
}

// parseQdiscStats parses tc qdisc output
func parseQdiscStats(output string) (*QdiscStats, error) {
	lines := strings.Split(output, "\n")
	if len(lines) == 0 {
		return nil, fmt.Errorf("no qdisc output")
	}

	stats := &QdiscStats{}

	// Parse first line for qdisc type and handles
	// Example: "qdisc cake 8001: root refcnt 2 bandwidth 100Mbit"
	firstLine := lines[0]
	parts := strings.Fields(firstLine)
	if len(parts) >= 2 && parts[0] == "qdisc" {
		stats.Type = parts[1]
		if len(parts) >= 3 {
			stats.Handle = strings.TrimSuffix(parts[2], ":")
		}
	}

	// Find bandwidth in first line
	for i, part := range parts {
		if part == "bandwidth" && i+1 < len(parts) {
			stats.Bandwidth = parts[i+1]
		}
	}

	// Parse stats lines
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Parse "Sent X bytes Y pkt (dropped Z, overlimits W requeues Q)"
		if strings.HasPrefix(line, "Sent") {
			re := regexp.MustCompile(`Sent (\d+) bytes (\d+) pkt \(dropped (\d+), overlimits (\d+) requeues (\d+)\)`)
			matches := re.FindStringSubmatch(line)
			if len(matches) >= 6 {
				stats.Bytes, _ = strconv.ParseUint(matches[1], 10, 64)
				stats.Packets, _ = strconv.ParseUint(matches[2], 10, 64)
				stats.Dropped, _ = strconv.ParseUint(matches[3], 10, 64)
				stats.Overlimits, _ = strconv.ParseUint(matches[4], 10, 64)
				stats.Requeues, _ = strconv.ParseUint(matches[5], 10, 64)
			}
		}

		// Parse "backlog Xb Yp"
		if strings.Contains(line, "backlog") {
			re := regexp.MustCompile(`backlog (\d+)b`)
			matches := re.FindStringSubmatch(line)
			if len(matches) >= 2 {
				stats.Backlog, _ = strconv.ParseUint(matches[1], 10, 64)
			}
		}

		// Parse CAKE-specific stats
		// "memory used: X limit: Y"
		if strings.Contains(line, "memory used:") {
			re := regexp.MustCompile(`memory used: (\d+).* limit: (\d+)`)
			matches := re.FindStringSubmatch(line)
			if len(matches) >= 3 {
				stats.MemoryUsed, _ = strconv.ParseUint(matches[1], 10, 64)
				stats.MemoryLimit, _ = strconv.ParseUint(matches[2], 10, 64)
			}
		}

		// "target X interval Y" - target delay
		if strings.Contains(line, "target") && strings.Contains(line, "interval") {
			re := regexp.MustCompile(`target (\S+)`)
			matches := re.FindStringSubmatch(line)
			if len(matches) >= 2 {
				stats.TargetDelay = matches[1]
			}
		}

		// "peak_delay_us X base_delay_us Y"
		if strings.Contains(line, "peak_delay_us") {
			re := regexp.MustCompile(`peak_delay_us (\d+)`)
			matches := re.FindStringSubmatch(line)
			if len(matches) >= 2 {
				stats.PeakDelayUs, _ = strconv.ParseUint(matches[1], 10, 64)
			}
		}
		if strings.Contains(line, "base_delay_us") {
			re := regexp.MustCompile(`base_delay_us (\d+)`)
			matches := re.FindStringSubmatch(line)
			if len(matches) >= 2 {
				stats.BaseDelayUs, _ = strconv.ParseUint(matches[1], 10, 64)
			}
		}
		if strings.Contains(line, "avg_delay_us") {
			re := regexp.MustCompile(`avg_delay_us (\d+)`)
			matches := re.FindStringSubmatch(line)
			if len(matches) >= 2 {
				stats.AvgDelayUs, _ = strconv.ParseUint(matches[1], 10, 64)
			}
		}
	}

	return stats, nil
}

// Disable disables QoS and removes all qdiscs
func (m *Manager) Disable(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.config != nil {
		m.removeQdiscs(ctx, m.config.WANInterface, m.config.LANInterface)
	}

	m.config.Enabled = false
	return nil
}

// GetConfig returns the current configuration
func (m *Manager) GetConfig() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.config == nil {
		return &Config{}
	}
	return m.config
}
