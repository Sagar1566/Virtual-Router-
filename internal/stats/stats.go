// Package stats provides network interface statistics collection and monitoring.
package stats

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iodesystems/ubuntu-router/internal/system"
)

const (
	sysClassNet = "/sys/class/net"
	// Default sample interval for rate calculations
	DefaultSampleInterval = time.Second
	// Default history size (keeps last 60 samples = 1 minute at 1s interval)
	DefaultHistorySize = 60
)

// InterfaceStats represents raw statistics for a network interface.
type InterfaceStats struct {
	Name      string    `json:"name"`
	Timestamp time.Time `json:"timestamp"`

	// Received
	RxBytes      uint64 `json:"rx_bytes"`
	RxPackets    uint64 `json:"rx_packets"`
	RxErrors     uint64 `json:"rx_errors"`
	RxDropped    uint64 `json:"rx_dropped"`
	RxFifo       uint64 `json:"rx_fifo"`
	RxFrame      uint64 `json:"rx_frame"`
	RxCompressed uint64 `json:"rx_compressed"`
	RxMulticast  uint64 `json:"rx_multicast"`

	// Transmitted
	TxBytes      uint64 `json:"tx_bytes"`
	TxPackets    uint64 `json:"tx_packets"`
	TxErrors     uint64 `json:"tx_errors"`
	TxDropped    uint64 `json:"tx_dropped"`
	TxFifo       uint64 `json:"tx_fifo"`
	TxCollisions uint64 `json:"tx_collisions"`
	TxCarrier    uint64 `json:"tx_carrier"`
	TxCompressed uint64 `json:"tx_compressed"`
}

// InterfaceRates represents calculated rates for a network interface.
type InterfaceRates struct {
	Name      string    `json:"name"`
	Timestamp time.Time `json:"timestamp"`

	// Bytes per second
	RxBytesPerSec float64 `json:"rx_bytes_per_sec"`
	TxBytesPerSec float64 `json:"tx_bytes_per_sec"`

	// Packets per second
	RxPacketsPerSec float64 `json:"rx_packets_per_sec"`
	TxPacketsPerSec float64 `json:"tx_packets_per_sec"`

	// Average packet size (bytes)
	RxAvgPacketSize float64 `json:"rx_avg_packet_size"`
	TxAvgPacketSize float64 `json:"tx_avg_packet_size"`

	// Error rates
	RxErrorsPerSec float64 `json:"rx_errors_per_sec"`
	TxErrorsPerSec float64 `json:"tx_errors_per_sec"`

	// Totals for reference
	TotalRxBytes   uint64 `json:"total_rx_bytes"`
	TotalTxBytes   uint64 `json:"total_tx_bytes"`
	TotalRxPackets uint64 `json:"total_rx_packets"`
	TotalTxPackets uint64 `json:"total_tx_packets"`
}

// LatencyStats represents latency measurements.
type LatencyStats struct {
	Target    string    `json:"target"`
	Timestamp time.Time `json:"timestamp"`

	// Latency in milliseconds
	LatencyMs    float64 `json:"latency_ms"`
	MinLatencyMs float64 `json:"min_latency_ms"`
	MaxLatencyMs float64 `json:"max_latency_ms"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`

	// Packet loss percentage
	PacketLoss float64 `json:"packet_loss"`

	// Number of samples in average
	SampleCount int `json:"sample_count"`

	// Whether the target is reachable
	Reachable bool `json:"reachable"`
}

// StatsSnapshot represents a complete snapshot of interface statistics.
type StatsSnapshot struct {
	Timestamp  time.Time                 `json:"timestamp"`
	Interfaces map[string]InterfaceRates `json:"interfaces"`
	Latency    *LatencyStats             `json:"latency,omitempty"`
}

// sample stores a single stats sample for rate calculation.
type sample struct {
	stats     InterfaceStats
	timestamp time.Time
}

// Manager handles statistics collection and calculation.
type Manager struct {
	mu     sync.RWMutex
	fs     system.FileSystem
	runner system.CommandRunner

	// History of samples per interface
	history     map[string][]sample
	historySize int

	// Latency tracking
	latencyHistory []LatencyStats
	latencyTarget  string

	// WiFi client tracking
	wifiClients *WiFiClientManager

	// Sampling configuration
	sampleInterval time.Duration

	// Background sampling
	stopChan chan struct{}
	running  bool

	// Persistence store for historical data
	store Store

	// Callback for WiFi client updates (for notifications)
	onWiFiClientsSampled func(ctx context.Context, clients []WiFiClientRates)
}

// New creates a new stats Manager.
func New(fs system.FileSystem, runner system.CommandRunner) *Manager {
	return &Manager{
		fs:             fs,
		runner:         runner,
		history:        make(map[string][]sample),
		historySize:    DefaultHistorySize,
		latencyHistory: make([]LatencyStats, 0, DefaultHistorySize),
		latencyTarget:  "8.8.8.8", // Default ping target
		wifiClients:    NewWiFiClientManager(runner),
		sampleInterval: DefaultSampleInterval,
	}
}

// SetLatencyTarget sets the target for latency measurements.
func (m *Manager) SetLatencyTarget(target string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.latencyTarget = target
}

// SetHistorySize sets the number of samples to keep.
func (m *Manager) SetHistorySize(size int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.historySize = size
}

// SetStore configures the persistence store for historical data.
func (m *Manager) SetStore(store Store) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store = store
}

// GetStore returns the configured persistence store.
func (m *Manager) GetStore() Store {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.store
}

// SetWiFiClientCallback sets a callback to be called when WiFi clients are sampled.
// This is used for notifications about client connect/disconnect events.
func (m *Manager) SetWiFiClientCallback(fn func(ctx context.Context, clients []WiFiClientRates)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onWiFiClientsSampled = fn
}

// GetInterfaceStats reads current statistics for a specific interface.
func (m *Manager) GetInterfaceStats(ctx context.Context, name string) (*InterfaceStats, error) {
	stats := &InterfaceStats{
		Name:      name,
		Timestamp: time.Now(),
	}

	basePath := filepath.Join(sysClassNet, name, "statistics")

	// Read all statistics files
	stats.RxBytes = m.readStatFile(basePath, "rx_bytes")
	stats.RxPackets = m.readStatFile(basePath, "rx_packets")
	stats.RxErrors = m.readStatFile(basePath, "rx_errors")
	stats.RxDropped = m.readStatFile(basePath, "rx_dropped")
	stats.RxFifo = m.readStatFile(basePath, "rx_fifo_errors")
	stats.RxFrame = m.readStatFile(basePath, "rx_frame_errors")
	stats.RxCompressed = m.readStatFile(basePath, "rx_compressed")
	stats.RxMulticast = m.readStatFile(basePath, "multicast")

	stats.TxBytes = m.readStatFile(basePath, "tx_bytes")
	stats.TxPackets = m.readStatFile(basePath, "tx_packets")
	stats.TxErrors = m.readStatFile(basePath, "tx_errors")
	stats.TxDropped = m.readStatFile(basePath, "tx_dropped")
	stats.TxFifo = m.readStatFile(basePath, "tx_fifo_errors")
	stats.TxCollisions = m.readStatFile(basePath, "collisions")
	stats.TxCarrier = m.readStatFile(basePath, "tx_carrier_errors")
	stats.TxCompressed = m.readStatFile(basePath, "tx_compressed")

	return stats, nil
}

// GetAllInterfaceStats reads statistics for all interfaces.
func (m *Manager) GetAllInterfaceStats(ctx context.Context) (map[string]*InterfaceStats, error) {
	entries, err := m.fs.ReadDir(sysClassNet)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*InterfaceStats)
	for _, entry := range entries {
		name := entry.Name()
		// Skip loopback
		if name == "lo" {
			continue
		}
		// Entries in /sys/class/net/ are symlinks to interface directories
		stats, err := m.GetInterfaceStats(ctx, name)
		if err != nil {
			continue
		}
		result[name] = stats
	}

	return result, nil
}

// Sample takes a snapshot of all interface statistics and stores it.
func (m *Manager) Sample(ctx context.Context) error {
	stats, err := m.GetAllInterfaceStats(ctx)
	if err != nil {
		return err
	}

	m.mu.Lock()
	store := m.store
	now := time.Now()
	for name, s := range stats {
		samples := m.history[name]
		samples = append(samples, sample{
			stats:     *s,
			timestamp: now,
		})
		// Trim to history size
		if len(samples) > m.historySize {
			samples = samples[len(samples)-m.historySize:]
		}
		m.history[name] = samples
	}
	m.mu.Unlock()

	// Persist to store if configured
	if store != nil {
		// Get latest latency for persistence
		m.mu.RLock()
		var latency *LatencyStats
		if len(m.latencyHistory) > 0 {
			latest := m.latencyHistory[len(m.latencyHistory)-1]
			latency = &latest
		}
		m.mu.RUnlock()

		// Record each interface's rates to the store
		for name := range stats {
			rates, err := m.GetRates(ctx, name)
			if err != nil || rates == nil {
				continue
			}
			_ = store.RecordSample(ctx, *rates, latency)
		}
	}

	return nil
}

// GetRates calculates rates for a specific interface based on recent samples.
func (m *Manager) GetRates(ctx context.Context, name string) (*InterfaceRates, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	samples := m.history[name]
	if len(samples) < 2 {
		// Not enough samples, return current stats without rates
		stats, err := m.GetInterfaceStats(ctx, name)
		if err != nil {
			return nil, err
		}
		return &InterfaceRates{
			Name:           name,
			Timestamp:      stats.Timestamp,
			TotalRxBytes:   stats.RxBytes,
			TotalTxBytes:   stats.TxBytes,
			TotalRxPackets: stats.RxPackets,
			TotalTxPackets: stats.TxPackets,
		}, nil
	}

	// Calculate rates from most recent samples
	newest := samples[len(samples)-1]
	oldest := samples[0]

	duration := newest.timestamp.Sub(oldest.timestamp).Seconds()
	if duration <= 0 {
		duration = 1 // Prevent division by zero
	}

	// Calculate byte deltas
	rxBytesDelta := float64(newest.stats.RxBytes - oldest.stats.RxBytes)
	txBytesDelta := float64(newest.stats.TxBytes - oldest.stats.TxBytes)
	rxPacketsDelta := float64(newest.stats.RxPackets - oldest.stats.RxPackets)
	txPacketsDelta := float64(newest.stats.TxPackets - oldest.stats.TxPackets)
	rxErrorsDelta := float64(newest.stats.RxErrors - oldest.stats.RxErrors)
	txErrorsDelta := float64(newest.stats.TxErrors - oldest.stats.TxErrors)

	rates := &InterfaceRates{
		Name:            name,
		Timestamp:       newest.timestamp,
		RxBytesPerSec:   rxBytesDelta / duration,
		TxBytesPerSec:   txBytesDelta / duration,
		RxPacketsPerSec: rxPacketsDelta / duration,
		TxPacketsPerSec: txPacketsDelta / duration,
		RxErrorsPerSec:  rxErrorsDelta / duration,
		TxErrorsPerSec:  txErrorsDelta / duration,
		TotalRxBytes:    newest.stats.RxBytes,
		TotalTxBytes:    newest.stats.TxBytes,
		TotalRxPackets:  newest.stats.RxPackets,
		TotalTxPackets:  newest.stats.TxPackets,
	}

	// Calculate average packet sizes
	if rxPacketsDelta > 0 {
		rates.RxAvgPacketSize = rxBytesDelta / rxPacketsDelta
	}
	if txPacketsDelta > 0 {
		rates.TxAvgPacketSize = txBytesDelta / txPacketsDelta
	}

	return rates, nil
}

// GetAllRates calculates rates for all interfaces.
func (m *Manager) GetAllRates(ctx context.Context) (map[string]*InterfaceRates, error) {
	entries, err := m.fs.ReadDir(sysClassNet)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*InterfaceRates)
	for _, entry := range entries {
		name := entry.Name()
		if name == "lo" {
			continue
		}
		// Entries in /sys/class/net/ are symlinks to interface directories
		rates, err := m.GetRates(ctx, name)
		if err != nil {
			continue
		}
		result[name] = rates
	}

	return result, nil
}

// MeasureLatency performs a ping measurement to the configured target.
func (m *Manager) MeasureLatency(ctx context.Context) (*LatencyStats, error) {
	m.mu.RLock()
	target := m.latencyTarget
	m.mu.RUnlock()

	stats := &LatencyStats{
		Target:    target,
		Timestamp: time.Now(),
	}

	// Run ping with 3 packets, 1 second timeout per packet
	output, err := m.runner.Run(ctx, "ping", "-c", "3", "-W", "1", target)
	if err != nil {
		// Ping failed - target unreachable
		stats.Reachable = false
		stats.PacketLoss = 100
		return stats, nil
	}

	stats.Reachable = true
	stats.SampleCount = 3

	// Parse ping output
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		// Look for summary line: "rtt min/avg/max/mdev = 1.234/2.345/3.456/0.567 ms"
		if strings.Contains(line, "rtt min/avg/max") || strings.Contains(line, "round-trip min/avg/max") {
			parts := strings.Split(line, "=")
			if len(parts) >= 2 {
				values := strings.TrimSpace(parts[1])
				// Remove "ms" suffix
				values = strings.TrimSuffix(values, " ms")
				fields := strings.Split(values, "/")
				if len(fields) >= 3 {
					stats.MinLatencyMs, _ = strconv.ParseFloat(fields[0], 64)
					stats.AvgLatencyMs, _ = strconv.ParseFloat(fields[1], 64)
					stats.MaxLatencyMs, _ = strconv.ParseFloat(fields[2], 64)
					stats.LatencyMs = stats.AvgLatencyMs
				}
			}
		}
		// Look for packet loss: "3 packets transmitted, 3 received, 0% packet loss"
		if strings.Contains(line, "packet loss") {
			// Find percentage
			parts := strings.Split(line, ",")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if strings.Contains(part, "packet loss") {
					pct := strings.TrimSuffix(part, "% packet loss")
					pct = strings.TrimSuffix(pct, " packet loss")
					// Handle both "0% packet loss" and "0 packet loss"
					pct = strings.TrimSuffix(pct, "%")
					stats.PacketLoss, _ = strconv.ParseFloat(strings.TrimSpace(pct), 64)
				}
			}
		}
	}

	// Store in history
	m.mu.Lock()
	m.latencyHistory = append(m.latencyHistory, *stats)
	if len(m.latencyHistory) > m.historySize {
		m.latencyHistory = m.latencyHistory[len(m.latencyHistory)-m.historySize:]
	}
	m.mu.Unlock()

	return stats, nil
}

// GetLatencyHistory returns recent latency measurements.
func (m *Manager) GetLatencyHistory() []LatencyStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]LatencyStats, len(m.latencyHistory))
	copy(result, m.latencyHistory)
	return result
}

// GetSnapshot returns a complete stats snapshot.
func (m *Manager) GetSnapshot(ctx context.Context) (*StatsSnapshot, error) {
	rates, err := m.GetAllRates(ctx)
	if err != nil {
		return nil, err
	}

	snapshot := &StatsSnapshot{
		Timestamp:  time.Now(),
		Interfaces: make(map[string]InterfaceRates),
	}

	for name, r := range rates {
		snapshot.Interfaces[name] = *r
	}

	// Get latest latency if available
	m.mu.RLock()
	if len(m.latencyHistory) > 0 {
		latest := m.latencyHistory[len(m.latencyHistory)-1]
		snapshot.Latency = &latest
	}
	m.mu.RUnlock()

	return snapshot, nil
}

// StartSampling begins background sampling of statistics.
func (m *Manager) StartSampling(ctx context.Context, interval time.Duration) {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.stopChan = make(chan struct{})
	store := m.store
	m.mu.Unlock()

	// Stats sampling goroutine
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Take initial samples
		_ = m.Sample(ctx)
		_ = m.wifiClients.Sample(ctx)
		m.recordWiFiClientSamples(ctx)

		for {
			select {
			case <-ticker.C:
				_ = m.Sample(ctx)
				_ = m.wifiClients.Sample(ctx)
				m.recordWiFiClientSamples(ctx)
			case <-m.stopChan:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	// Persistence rollup/prune goroutine (if store is configured)
	if store != nil {
		go func() {
			rollupTicker := time.NewTicker(time.Minute)
			pruneTicker := time.NewTicker(time.Hour)
			defer rollupTicker.Stop()
			defer pruneTicker.Stop()

			for {
				select {
				case <-rollupTicker.C:
					_ = store.Rollup(ctx)
				case <-pruneTicker.C:
					_ = store.Prune(ctx)
				case <-m.stopChan:
					return
				case <-ctx.Done():
					return
				}
			}
		}()
	}
}

// StopSampling stops background sampling.
func (m *Manager) StopSampling() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running && m.stopChan != nil {
		close(m.stopChan)
		m.running = false
	}
}

// readStatFile reads a single statistics file and returns its value.
func (m *Manager) readStatFile(basePath, name string) uint64 {
	data, err := m.fs.ReadFile(filepath.Join(basePath, name))
	if err != nil {
		return 0
	}
	val, _ := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	return val
}

// GetWiFiClientRates returns rates for all connected WiFi clients.
func (m *Manager) GetWiFiClientRates(ctx context.Context) ([]WiFiClientRates, error) {
	return m.wifiClients.GetAllClientRates(ctx)
}

// GetWiFiClientHistory returns the sample history for a specific client.
func (m *Manager) GetWiFiClientHistory(mac string) []WiFiClientStats {
	return m.wifiClients.GetClientHistory(mac)
}

// QueryHistory retrieves historical data from the persistence store.
func (m *Manager) QueryHistory(ctx context.Context, query HistoryQuery) (*HistoryResponse, error) {
	m.mu.RLock()
	store := m.store
	m.mu.RUnlock()

	if store == nil {
		return nil, nil
	}

	return store.Query(ctx, query)
}

// QueryWiFiClientHistory retrieves historical WiFi client data from the persistence store.
func (m *Manager) QueryWiFiClientHistory(ctx context.Context, query WiFiClientHistoryQuery) (*WiFiClientHistoryResponse, error) {
	m.mu.RLock()
	store := m.store
	m.mu.RUnlock()

	if store == nil {
		return nil, nil
	}

	return store.QueryWiFiClients(ctx, query)
}

// recordWiFiClientSamples persists WiFi client samples to the store and triggers callbacks.
func (m *Manager) recordWiFiClientSamples(ctx context.Context) {
	m.mu.RLock()
	store := m.store
	callback := m.onWiFiClientsSampled
	m.mu.RUnlock()

	rates, err := m.wifiClients.GetAllClientRates(ctx)
	if err != nil {
		return
	}

	// Persist to store
	if store != nil {
		for _, r := range rates {
			_ = store.RecordWiFiClientSample(ctx, r)
		}
	}

	// Call notification callback
	if callback != nil {
		callback(ctx, rates)
	}
}
