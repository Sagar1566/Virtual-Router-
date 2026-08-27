package stats

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iodesystems/ubuntu-router/internal/system"
)

// WiFiClientStats represents raw statistics for a WiFi client
type WiFiClientStats struct {
	MAC           string    `json:"mac"`
	Interface     string    `json:"interface"`
	Timestamp     time.Time `json:"timestamp"`
	Signal        int       `json:"signal"`         // dBm
	RxBitrate     float64   `json:"rx_bitrate"`     // Mbps
	TxBitrate     float64   `json:"tx_bitrate"`     // Mbps
	RxBytes       uint64    `json:"rx_bytes"`
	TxBytes       uint64    `json:"tx_bytes"`
	RxPackets     uint64    `json:"rx_packets"`
	TxPackets     uint64    `json:"tx_packets"`
	RxRetries     uint64    `json:"rx_retries"`
	TxRetries     uint64    `json:"tx_retries"`
	TxFailed      uint64    `json:"tx_failed"`
	ConnectedTime int       `json:"connected_time"` // seconds
	Inactive      int       `json:"inactive"`       // ms since last activity
	Authorized    bool      `json:"authorized"`
	Authenticated bool      `json:"authenticated"`
}

// WiFiClientRates represents calculated rates for a WiFi client
type WiFiClientRates struct {
	MAC           string    `json:"mac"`
	Interface     string    `json:"interface"`
	Timestamp     time.Time `json:"timestamp"`
	Signal        int       `json:"signal"`
	RxBitrate     float64   `json:"rx_bitrate"`
	TxBitrate     float64   `json:"tx_bitrate"`
	RxBytesPerSec float64   `json:"rx_bytes_per_sec"`
	TxBytesPerSec float64   `json:"tx_bytes_per_sec"`
	TotalRxBytes  uint64    `json:"total_rx_bytes"`
	TotalTxBytes  uint64    `json:"total_tx_bytes"`
	ConnectedTime int       `json:"connected_time"`
	Inactive      int       `json:"inactive"`
	Authorized    bool      `json:"authorized"`
}

// wifiClientSample stores a single stats sample for rate calculation
type wifiClientSample struct {
	stats     WiFiClientStats
	timestamp time.Time
}

// WiFiClientManager handles WiFi client statistics collection
type WiFiClientManager struct {
	mu          sync.RWMutex
	runner      system.CommandRunner
	history     map[string][]wifiClientSample // key: MAC address
	historySize int
}

// NewWiFiClientManager creates a new WiFi client stats manager
func NewWiFiClientManager(runner system.CommandRunner) *WiFiClientManager {
	return &WiFiClientManager{
		runner:      runner,
		history:     make(map[string][]wifiClientSample),
		historySize: DefaultHistorySize,
	}
}

// GetAllClientStats reads current statistics for all WiFi clients
func (m *WiFiClientManager) GetAllClientStats(ctx context.Context) ([]WiFiClientStats, error) {
	// Get list of wireless interfaces
	interfaces, err := m.getWirelessInterfaces(ctx)
	if err != nil {
		return nil, err
	}

	var allStats []WiFiClientStats
	for _, iface := range interfaces {
		stats, err := m.getClientStatsForInterface(ctx, iface)
		if err != nil {
			continue // Skip interfaces that fail
		}
		allStats = append(allStats, stats...)
	}

	return allStats, nil
}

// getWirelessInterfaces returns a list of wireless interface names
func (m *WiFiClientManager) getWirelessInterfaces(ctx context.Context) ([]string, error) {
	output, err := m.runner.Run(ctx, "iw", "dev")
	if err != nil {
		return nil, err
	}

	var interfaces []string
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Interface ") {
			iface := strings.TrimPrefix(line, "Interface ")
			interfaces = append(interfaces, iface)
		}
	}

	return interfaces, nil
}

// getClientStatsForInterface reads client stats for a specific interface
func (m *WiFiClientManager) getClientStatsForInterface(ctx context.Context, iface string) ([]WiFiClientStats, error) {
	output, err := m.runner.Run(ctx, "iw", "dev", iface, "station", "dump")
	if err != nil {
		return nil, err
	}

	return parseStationDump(iface, string(output)), nil
}

// parseStationDump parses iw station dump output
func parseStationDump(iface, output string) []WiFiClientStats {
	var stats []WiFiClientStats
	var current *WiFiClientStats
	now := time.Now()

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// New station entry
		if strings.HasPrefix(line, "Station ") {
			if current != nil {
				stats = append(stats, *current)
			}
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				current = &WiFiClientStats{
					MAC:       strings.ToUpper(parts[1]),
					Interface: iface,
					Timestamp: now,
				}
			}
			continue
		}

		if current == nil {
			continue
		}

		// Parse station properties
		switch {
		case strings.HasPrefix(line, "signal:"):
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				current.Signal, _ = strconv.Atoi(parts[1])
			}

		case strings.HasPrefix(line, "signal avg:"):
			// Could track average signal too if needed

		case strings.HasPrefix(line, "rx bitrate:"):
			re := regexp.MustCompile(`(\d+\.?\d*)\s*MBit`)
			if matches := re.FindStringSubmatch(line); len(matches) >= 2 {
				current.RxBitrate, _ = strconv.ParseFloat(matches[1], 64)
			}

		case strings.HasPrefix(line, "tx bitrate:"):
			re := regexp.MustCompile(`(\d+\.?\d*)\s*MBit`)
			if matches := re.FindStringSubmatch(line); len(matches) >= 2 {
				current.TxBitrate, _ = strconv.ParseFloat(matches[1], 64)
			}

		case strings.HasPrefix(line, "rx bytes:"):
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				current.RxBytes, _ = strconv.ParseUint(parts[2], 10, 64)
			}

		case strings.HasPrefix(line, "tx bytes:"):
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				current.TxBytes, _ = strconv.ParseUint(parts[2], 10, 64)
			}

		case strings.HasPrefix(line, "rx packets:"):
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				current.RxPackets, _ = strconv.ParseUint(parts[2], 10, 64)
			}

		case strings.HasPrefix(line, "tx packets:"):
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				current.TxPackets, _ = strconv.ParseUint(parts[2], 10, 64)
			}

		case strings.HasPrefix(line, "tx retries:"):
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				current.TxRetries, _ = strconv.ParseUint(parts[2], 10, 64)
			}

		case strings.HasPrefix(line, "tx failed:"):
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				current.TxFailed, _ = strconv.ParseUint(parts[2], 10, 64)
			}

		case strings.HasPrefix(line, "connected time:"):
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				current.ConnectedTime, _ = strconv.Atoi(parts[2])
			}

		case strings.HasPrefix(line, "inactive time:"):
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				current.Inactive, _ = strconv.Atoi(parts[2])
			}

		case strings.HasPrefix(line, "authorized:"):
			current.Authorized = strings.Contains(line, "yes")

		case strings.HasPrefix(line, "authenticated:"):
			current.Authenticated = strings.Contains(line, "yes")
		}
	}

	if current != nil {
		stats = append(stats, *current)
	}

	return stats
}

// Sample takes a snapshot of all WiFi client statistics and stores it
func (m *WiFiClientManager) Sample(ctx context.Context) error {
	stats, err := m.GetAllClientStats(ctx)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()

	// Track which MACs we saw this sample
	seenMACs := make(map[string]bool)

	for _, s := range stats {
		seenMACs[s.MAC] = true

		samples := m.history[s.MAC]
		samples = append(samples, wifiClientSample{
			stats:     s,
			timestamp: now,
		})
		// Trim to history size
		if len(samples) > m.historySize {
			samples = samples[len(samples)-m.historySize:]
		}
		m.history[s.MAC] = samples
	}

	// Clean up disconnected clients (not seen for a while)
	for mac, samples := range m.history {
		if !seenMACs[mac] && len(samples) > 0 {
			lastSeen := samples[len(samples)-1].timestamp
			if time.Since(lastSeen) > 5*time.Minute {
				delete(m.history, mac)
			}
		}
	}

	return nil
}

// GetClientRates calculates rates for a specific client
func (m *WiFiClientManager) GetClientRates(ctx context.Context, mac string) (*WiFiClientRates, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	mac = strings.ToUpper(mac)
	samples := m.history[mac]

	if len(samples) < 2 {
		// Not enough samples, try to get current stats
		allStats, err := m.GetAllClientStats(ctx)
		if err != nil {
			return nil, err
		}
		for _, s := range allStats {
			if s.MAC == mac {
				return &WiFiClientRates{
					MAC:           s.MAC,
					Interface:     s.Interface,
					Timestamp:     s.Timestamp,
					Signal:        s.Signal,
					RxBitrate:     s.RxBitrate,
					TxBitrate:     s.TxBitrate,
					TotalRxBytes:  s.RxBytes,
					TotalTxBytes:  s.TxBytes,
					ConnectedTime: s.ConnectedTime,
					Inactive:      s.Inactive,
					Authorized:    s.Authorized,
				}, nil
			}
		}
		return nil, nil
	}

	newest := samples[len(samples)-1]
	oldest := samples[0]

	duration := newest.timestamp.Sub(oldest.timestamp).Seconds()
	if duration <= 0 {
		duration = 1
	}

	rxBytesDelta := float64(newest.stats.RxBytes - oldest.stats.RxBytes)
	txBytesDelta := float64(newest.stats.TxBytes - oldest.stats.TxBytes)

	return &WiFiClientRates{
		MAC:           newest.stats.MAC,
		Interface:     newest.stats.Interface,
		Timestamp:     newest.timestamp,
		Signal:        newest.stats.Signal,
		RxBitrate:     newest.stats.RxBitrate,
		TxBitrate:     newest.stats.TxBitrate,
		RxBytesPerSec: rxBytesDelta / duration,
		TxBytesPerSec: txBytesDelta / duration,
		TotalRxBytes:  newest.stats.RxBytes,
		TotalTxBytes:  newest.stats.TxBytes,
		ConnectedTime: newest.stats.ConnectedTime,
		Inactive:      newest.stats.Inactive,
		Authorized:    newest.stats.Authorized,
	}, nil
}

// GetAllClientRates calculates rates for all clients
func (m *WiFiClientManager) GetAllClientRates(ctx context.Context) ([]WiFiClientRates, error) {
	// First get current stats to know which clients are connected
	allStats, err := m.GetAllClientStats(ctx)
	if err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var rates []WiFiClientRates
	for _, s := range allStats {
		samples := m.history[s.MAC]

		rate := WiFiClientRates{
			MAC:           s.MAC,
			Interface:     s.Interface,
			Timestamp:     s.Timestamp,
			Signal:        s.Signal,
			RxBitrate:     s.RxBitrate,
			TxBitrate:     s.TxBitrate,
			TotalRxBytes:  s.RxBytes,
			TotalTxBytes:  s.TxBytes,
			ConnectedTime: s.ConnectedTime,
			Inactive:      s.Inactive,
			Authorized:    s.Authorized,
		}

		if len(samples) >= 2 {
			newest := samples[len(samples)-1]
			oldest := samples[0]

			duration := newest.timestamp.Sub(oldest.timestamp).Seconds()
			if duration > 0 {
				rate.RxBytesPerSec = float64(newest.stats.RxBytes-oldest.stats.RxBytes) / duration
				rate.TxBytesPerSec = float64(newest.stats.TxBytes-oldest.stats.TxBytes) / duration
			}
		}

		rates = append(rates, rate)
	}

	return rates, nil
}

// GetClientHistory returns the sample history for a client
func (m *WiFiClientManager) GetClientHistory(mac string) []WiFiClientStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	mac = strings.ToUpper(mac)
	samples := m.history[mac]

	result := make([]WiFiClientStats, len(samples))
	for i, s := range samples {
		result[i] = s.stats
	}
	return result
}
