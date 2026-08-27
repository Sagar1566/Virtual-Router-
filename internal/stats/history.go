package stats

import (
	"context"
	"fmt"
	"time"
)

// Query retrieves historical data based on the query parameters
func (s *SQLiteStore) Query(ctx context.Context, query HistoryQuery) (*HistoryResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tableName := resolutionToTable(query.Resolution)

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT timestamp, rx_bytes, tx_bytes, rx_packets, tx_packets,
		       avg_rx_bytes_per_sec, avg_tx_bytes_per_sec,
		       avg_latency_ms, min_latency_ms, max_latency_ms, sample_count
		FROM %s
		WHERE interface_name = ? AND timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp ASC
	`, tableName), query.InterfaceName, query.Start.Unix(), query.End.Unix())
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	var dataPoints []AggregatedStats
	for rows.Next() {
		var ts int64
		var stat AggregatedStats
		stat.InterfaceName = query.InterfaceName
		stat.Resolution = query.Resolution

		if err := rows.Scan(
			&ts,
			&stat.RxBytes, &stat.TxBytes, &stat.RxPackets, &stat.TxPackets,
			&stat.AvgRxBytesPerSec, &stat.AvgTxBytesPerSec,
			&stat.AvgLatencyMs, &stat.MinLatencyMs, &stat.MaxLatencyMs,
			&stat.SampleCount,
		); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		stat.Timestamp = time.Unix(ts, 0)
		dataPoints = append(dataPoints, stat)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return &HistoryResponse{
		InterfaceName: query.InterfaceName,
		Resolution:    query.Resolution,
		DataPoints:    dataPoints,
	}, nil
}

// ParseRange converts a human-readable range like "7d" to start/end times and resolution
func ParseRange(rangeStr string) (start, end time.Time, resolution Resolution, err error) {
	end = time.Now()

	switch rangeStr {
	case "1h":
		start = end.Add(-time.Hour)
		resolution = ResolutionMinute
	case "6h":
		start = end.Add(-6 * time.Hour)
		resolution = ResolutionMinute
	case "12h":
		start = end.Add(-12 * time.Hour)
		resolution = ResolutionMinute
	case "24h", "1d":
		start = end.Add(-24 * time.Hour)
		resolution = ResolutionMinute
	case "7d", "1w":
		start = end.Add(-7 * 24 * time.Hour)
		resolution = ResolutionHour
	case "30d", "1m":
		start = end.Add(-30 * 24 * time.Hour)
		resolution = ResolutionDay
	case "90d", "3m":
		start = end.Add(-90 * 24 * time.Hour)
		resolution = ResolutionDay
	case "1y":
		start = end.Add(-365 * 24 * time.Hour)
		resolution = ResolutionMonth
	case "all":
		// Query from beginning of time
		start = time.Unix(0, 0)
		resolution = ResolutionYear
	default:
		err = fmt.Errorf("invalid range: %s (valid: 1h, 6h, 12h, 24h, 7d, 30d, 90d, 1y, all)", rangeStr)
	}

	return
}

// GetAvailableInterfaces returns a list of interfaces that have historical data
func (s *SQLiteStore) GetAvailableInterfaces(ctx context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT interface_name FROM stats_minute
		UNION
		SELECT DISTINCT interface_name FROM stats_hour
		UNION
		SELECT DISTINCT interface_name FROM stats_day
		UNION
		SELECT DISTINCT interface_name FROM stats_month
		UNION
		SELECT DISTINCT interface_name FROM stats_year
		ORDER BY interface_name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var interfaces []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		interfaces = append(interfaces, name)
	}

	return interfaces, rows.Err()
}

// GetDataRange returns the time range of available data for an interface
func (s *SQLiteStore) GetDataRange(ctx context.Context, interfaceName string) (oldest, newest time.Time, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var oldestTs, newestTs int64

	// Check minute table first (most granular)
	err = s.db.QueryRowContext(ctx, `
		SELECT MIN(timestamp), MAX(timestamp) FROM stats_minute WHERE interface_name = ?
	`, interfaceName).Scan(&oldestTs, &newestTs)

	if err != nil || oldestTs == 0 {
		// Try hour table
		err = s.db.QueryRowContext(ctx, `
			SELECT MIN(timestamp), MAX(timestamp) FROM stats_hour WHERE interface_name = ?
		`, interfaceName).Scan(&oldestTs, &newestTs)
	}

	if err != nil || oldestTs == 0 {
		// Try day table
		err = s.db.QueryRowContext(ctx, `
			SELECT MIN(timestamp), MAX(timestamp) FROM stats_day WHERE interface_name = ?
		`, interfaceName).Scan(&oldestTs, &newestTs)
	}

	if err != nil {
		return
	}

	oldest = time.Unix(oldestTs, 0)
	newest = time.Unix(newestTs, 0)
	return
}

// QueryWiFiClients retrieves historical WiFi client data
func (s *SQLiteStore) QueryWiFiClients(ctx context.Context, query WiFiClientHistoryQuery) (*WiFiClientHistoryResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tableName := wifiClientResolutionToTable(query.Resolution)

	var rows interface {
		Next() bool
		Scan(...interface{}) error
		Close() error
		Err() error
	}
	var err error

	if query.MAC == "" {
		// Query all clients
		rows, err = s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT mac, interface_name, timestamp, rx_bytes, tx_bytes,
			       avg_rx_bytes_per_sec, avg_tx_bytes_per_sec,
			       avg_signal, min_signal, max_signal,
			       avg_rx_bitrate, avg_tx_bitrate, sample_count
			FROM %s
			WHERE timestamp >= ? AND timestamp <= ?
			ORDER BY mac, timestamp ASC
		`, tableName), query.Start.Unix(), query.End.Unix())
	} else {
		// Query specific client
		rows, err = s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT mac, interface_name, timestamp, rx_bytes, tx_bytes,
			       avg_rx_bytes_per_sec, avg_tx_bytes_per_sec,
			       avg_signal, min_signal, max_signal,
			       avg_rx_bitrate, avg_tx_bitrate, sample_count
			FROM %s
			WHERE mac = ? AND timestamp >= ? AND timestamp <= ?
			ORDER BY timestamp ASC
		`, tableName), query.MAC, query.Start.Unix(), query.End.Unix())
	}

	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	var clients []WiFiClientAggregatedStats
	for rows.Next() {
		var ts int64
		var stat WiFiClientAggregatedStats
		stat.Resolution = query.Resolution

		if err := rows.Scan(
			&stat.MAC, &stat.InterfaceName, &ts,
			&stat.RxBytes, &stat.TxBytes,
			&stat.AvgRxBytesPerSec, &stat.AvgTxBytesPerSec,
			&stat.AvgSignal, &stat.MinSignal, &stat.MaxSignal,
			&stat.AvgRxBitrate, &stat.AvgTxBitrate,
			&stat.SampleCount,
		); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		stat.Timestamp = time.Unix(ts, 0)
		clients = append(clients, stat)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return &WiFiClientHistoryResponse{
		Resolution: query.Resolution,
		Clients:    clients,
	}, nil
}

// GetAvailableWiFiClients returns a list of WiFi client MACs that have historical data
func (s *SQLiteStore) GetAvailableWiFiClients(ctx context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT mac FROM wifi_client_minute
		UNION
		SELECT DISTINCT mac FROM wifi_client_hour
		UNION
		SELECT DISTINCT mac FROM wifi_client_day
		UNION
		SELECT DISTINCT mac FROM wifi_client_month
		UNION
		SELECT DISTINCT mac FROM wifi_client_year
		ORDER BY mac
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var macs []string
	for rows.Next() {
		var mac string
		if err := rows.Scan(&mac); err != nil {
			return nil, err
		}
		macs = append(macs, mac)
	}

	return macs, rows.Err()
}
