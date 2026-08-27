package stats

import (
	"context"
	"database/sql"
	"time"
)

// RecordSample stores incoming sample data into the accumulator
func (s *SQLiteStore) RecordSample(ctx context.Context, rates InterfaceRates, latency *LatencyStats) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	minuteTs := truncateToMinute(now).Unix()

	latencyMs := getLatencyMs(latency)
	latencyMin := getLatencyMin(latency)
	latencyMax := getLatencyMax(latency)

	// Check if we need to flush the previous minute first
	var existingMinuteTs sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT minute_ts FROM stats_accumulator WHERE interface_name = ?
	`, rates.Name).Scan(&existingMinuteTs)

	if err == nil && existingMinuteTs.Valid && existingMinuteTs.Int64 < minuteTs {
		// Minute boundary crossed - flush the old accumulator to stats_minute
		if err := s.flushAccumulator(ctx, rates.Name, existingMinuteTs.Int64); err != nil {
			return err
		}
	}

	// Update or insert accumulator
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO stats_accumulator (
			interface_name, minute_ts,
			rx_bytes_delta, tx_bytes_delta, rx_packets_delta, tx_packets_delta,
			rx_rate_sum, tx_rate_sum,
			latency_sum, latency_min, latency_max,
			sample_count,
			last_rx_bytes, last_tx_bytes, last_rx_packets, last_tx_packets
		) VALUES (?, ?, 0, 0, 0, 0, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?)
		ON CONFLICT(interface_name) DO UPDATE SET
			minute_ts = ?,
			rx_rate_sum = rx_rate_sum + ?,
			tx_rate_sum = tx_rate_sum + ?,
			latency_sum = latency_sum + ?,
			latency_min = CASE
				WHEN latency_min IS NULL OR ? < latency_min THEN ?
				ELSE latency_min
			END,
			latency_max = CASE
				WHEN latency_max IS NULL OR ? > latency_max THEN ?
				ELSE latency_max
			END,
			rx_bytes_delta = rx_bytes_delta + (? - last_rx_bytes),
			tx_bytes_delta = tx_bytes_delta + (? - last_tx_bytes),
			rx_packets_delta = rx_packets_delta + (? - last_rx_packets),
			tx_packets_delta = tx_packets_delta + (? - last_tx_packets),
			sample_count = sample_count + 1,
			last_rx_bytes = ?,
			last_tx_bytes = ?,
			last_rx_packets = ?,
			last_tx_packets = ?
	`,
		// INSERT values
		rates.Name, minuteTs,
		rates.RxBytesPerSec, rates.TxBytesPerSec,
		latencyMs, latencyMin, latencyMax,
		rates.TotalRxBytes, rates.TotalTxBytes, rates.TotalRxPackets, rates.TotalTxPackets,
		// UPDATE values
		minuteTs,
		rates.RxBytesPerSec,
		rates.TxBytesPerSec,
		latencyMs,
		latencyMin, latencyMin,
		latencyMax, latencyMax,
		rates.TotalRxBytes,
		rates.TotalTxBytes,
		rates.TotalRxPackets,
		rates.TotalTxPackets,
		rates.TotalRxBytes,
		rates.TotalTxBytes,
		rates.TotalRxPackets,
		rates.TotalTxPackets,
	)

	return err
}

// flushAccumulator moves accumulated data to the minute table
func (s *SQLiteStore) flushAccumulator(ctx context.Context, interfaceName string, minuteTs int64) error {
	// Read accumulator
	var rxBytes, txBytes, rxPackets, txPackets int64
	var rxRateSum, txRateSum, latSum float64
	var latMin, latMax sql.NullFloat64
	var sampleCount int

	err := s.db.QueryRowContext(ctx, `
		SELECT rx_bytes_delta, tx_bytes_delta, rx_packets_delta, tx_packets_delta,
		       rx_rate_sum, tx_rate_sum, latency_sum, latency_min, latency_max, sample_count
		FROM stats_accumulator
		WHERE interface_name = ? AND minute_ts = ?
	`, interfaceName, minuteTs).Scan(
		&rxBytes, &txBytes, &rxPackets, &txPackets,
		&rxRateSum, &txRateSum, &latSum, &latMin, &latMax, &sampleCount,
	)

	if err == sql.ErrNoRows || sampleCount == 0 {
		return nil
	}
	if err != nil {
		return err
	}

	// Insert into minute table
	_, err = s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO stats_minute (
			interface_name, timestamp,
			rx_bytes, tx_bytes, rx_packets, tx_packets,
			avg_rx_bytes_per_sec, avg_tx_bytes_per_sec,
			avg_latency_ms, min_latency_ms, max_latency_ms, sample_count
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		interfaceName, minuteTs,
		rxBytes, txBytes, rxPackets, txPackets,
		rxRateSum/float64(sampleCount), txRateSum/float64(sampleCount),
		latSum/float64(sampleCount), nullToZero(latMin), nullToZero(latMax), sampleCount,
	)

	if err != nil {
		return err
	}

	// Reset the accumulator
	_, err = s.db.ExecContext(ctx, `
		UPDATE stats_accumulator SET
			rx_bytes_delta = 0, tx_bytes_delta = 0,
			rx_packets_delta = 0, tx_packets_delta = 0,
			rx_rate_sum = 0, tx_rate_sum = 0,
			latency_sum = 0, latency_min = NULL, latency_max = NULL,
			sample_count = 0
		WHERE interface_name = ?
	`, interfaceName)

	return err
}

// Rollup performs aggregation from finer to coarser resolutions
func (s *SQLiteStore) Rollup(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	// Flush all accumulators for completed minutes
	if err := s.flushAllAccumulators(ctx, now); err != nil {
		return err
	}
	if err := s.flushAllWiFiClientAccumulators(ctx, now); err != nil {
		return err
	}

	// Rollup minutes to hours (every hour, process the previous hour)
	if now.Sub(s.lastHourRollup) >= time.Hour {
		if err := s.rollupMinutesToHours(ctx, now); err != nil {
			return err
		}
		if err := s.rollupWiFiClientMinutesToHours(ctx, now); err != nil {
			return err
		}
		s.lastHourRollup = now
	}

	// Rollup hours to days (every day)
	if now.Sub(s.lastDayRollup) >= 24*time.Hour {
		if err := s.rollupHoursToDays(ctx, now); err != nil {
			return err
		}
		if err := s.rollupWiFiClientHoursToDays(ctx, now); err != nil {
			return err
		}
		s.lastDayRollup = now
	}

	// Rollup days to months (approximately monthly)
	if now.Sub(s.lastMonthRollup) >= 30*24*time.Hour {
		if err := s.rollupDaysToMonths(ctx, now); err != nil {
			return err
		}
		if err := s.rollupMonthsToYears(ctx, now); err != nil {
			return err
		}
		if err := s.rollupWiFiClientDaysToMonths(ctx, now); err != nil {
			return err
		}
		if err := s.rollupWiFiClientMonthsToYears(ctx, now); err != nil {
			return err
		}
		s.lastMonthRollup = now
	}

	return nil
}

// flushAllAccumulators flushes all accumulators for completed minutes
func (s *SQLiteStore) flushAllAccumulators(ctx context.Context, now time.Time) error {
	currentMinute := truncateToMinute(now).Unix()

	rows, err := s.db.QueryContext(ctx, `
		SELECT interface_name, minute_ts FROM stats_accumulator WHERE minute_ts < ?
	`, currentMinute)
	if err != nil {
		return err
	}
	defer rows.Close()

	var toFlush []struct {
		iface    string
		minuteTs int64
	}

	for rows.Next() {
		var iface string
		var ts int64
		if err := rows.Scan(&iface, &ts); err != nil {
			return err
		}
		toFlush = append(toFlush, struct {
			iface    string
			minuteTs int64
		}{iface, ts})
	}

	for _, item := range toFlush {
		if err := s.flushAccumulator(ctx, item.iface, item.minuteTs); err != nil {
			return err
		}
	}

	return nil
}

// rollupMinutesToHours aggregates minute data into hourly buckets
func (s *SQLiteStore) rollupMinutesToHours(ctx context.Context, now time.Time) error {
	// Process the previous complete hour
	hourTs := truncateToHour(now.Add(-time.Hour)).Unix()
	nextHourTs := hourTs + 3600

	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO stats_hour (
			interface_name, timestamp,
			rx_bytes, tx_bytes, rx_packets, tx_packets,
			avg_rx_bytes_per_sec, avg_tx_bytes_per_sec,
			avg_latency_ms, min_latency_ms, max_latency_ms, sample_count
		)
		SELECT
			interface_name,
			? as timestamp,
			SUM(rx_bytes),
			SUM(tx_bytes),
			SUM(rx_packets),
			SUM(tx_packets),
			SUM(avg_rx_bytes_per_sec * sample_count) / SUM(sample_count),
			SUM(avg_tx_bytes_per_sec * sample_count) / SUM(sample_count),
			SUM(avg_latency_ms * sample_count) / SUM(sample_count),
			MIN(min_latency_ms),
			MAX(max_latency_ms),
			SUM(sample_count)
		FROM stats_minute
		WHERE timestamp >= ? AND timestamp < ?
		GROUP BY interface_name
	`, hourTs, hourTs, nextHourTs)

	return err
}

// rollupHoursToDays aggregates hourly data into daily buckets
func (s *SQLiteStore) rollupHoursToDays(ctx context.Context, now time.Time) error {
	// Process the previous complete day
	dayTs := truncateToDay(now.Add(-24 * time.Hour)).Unix()
	nextDayTs := dayTs + 86400

	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO stats_day (
			interface_name, timestamp,
			rx_bytes, tx_bytes, rx_packets, tx_packets,
			avg_rx_bytes_per_sec, avg_tx_bytes_per_sec,
			avg_latency_ms, min_latency_ms, max_latency_ms, sample_count
		)
		SELECT
			interface_name,
			? as timestamp,
			SUM(rx_bytes),
			SUM(tx_bytes),
			SUM(rx_packets),
			SUM(tx_packets),
			SUM(avg_rx_bytes_per_sec * sample_count) / SUM(sample_count),
			SUM(avg_tx_bytes_per_sec * sample_count) / SUM(sample_count),
			SUM(avg_latency_ms * sample_count) / SUM(sample_count),
			MIN(min_latency_ms),
			MAX(max_latency_ms),
			SUM(sample_count)
		FROM stats_hour
		WHERE timestamp >= ? AND timestamp < ?
		GROUP BY interface_name
	`, dayTs, dayTs, nextDayTs)

	return err
}

// rollupDaysToMonths aggregates daily data into monthly buckets
func (s *SQLiteStore) rollupDaysToMonths(ctx context.Context, now time.Time) error {
	// Process the previous complete month
	monthStart := truncateToMonth(now.AddDate(0, -1, 0))
	nextMonthStart := truncateToMonth(now)

	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO stats_month (
			interface_name, timestamp,
			rx_bytes, tx_bytes, rx_packets, tx_packets,
			avg_rx_bytes_per_sec, avg_tx_bytes_per_sec,
			avg_latency_ms, min_latency_ms, max_latency_ms, sample_count
		)
		SELECT
			interface_name,
			? as timestamp,
			SUM(rx_bytes),
			SUM(tx_bytes),
			SUM(rx_packets),
			SUM(tx_packets),
			SUM(avg_rx_bytes_per_sec * sample_count) / SUM(sample_count),
			SUM(avg_tx_bytes_per_sec * sample_count) / SUM(sample_count),
			SUM(avg_latency_ms * sample_count) / SUM(sample_count),
			MIN(min_latency_ms),
			MAX(max_latency_ms),
			SUM(sample_count)
		FROM stats_day
		WHERE timestamp >= ? AND timestamp < ?
		GROUP BY interface_name
	`, monthStart.Unix(), monthStart.Unix(), nextMonthStart.Unix())

	return err
}

// rollupMonthsToYears aggregates monthly data into yearly buckets
func (s *SQLiteStore) rollupMonthsToYears(ctx context.Context, now time.Time) error {
	// Process the previous complete year
	yearStart := truncateToYear(now.AddDate(-1, 0, 0))
	nextYearStart := truncateToYear(now)

	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO stats_year (
			interface_name, timestamp,
			rx_bytes, tx_bytes, rx_packets, tx_packets,
			avg_rx_bytes_per_sec, avg_tx_bytes_per_sec,
			avg_latency_ms, min_latency_ms, max_latency_ms, sample_count
		)
		SELECT
			interface_name,
			? as timestamp,
			SUM(rx_bytes),
			SUM(tx_bytes),
			SUM(rx_packets),
			SUM(tx_packets),
			SUM(avg_rx_bytes_per_sec * sample_count) / SUM(sample_count),
			SUM(avg_tx_bytes_per_sec * sample_count) / SUM(sample_count),
			SUM(avg_latency_ms * sample_count) / SUM(sample_count),
			MIN(min_latency_ms),
			MAX(max_latency_ms),
			SUM(sample_count)
		FROM stats_month
		WHERE timestamp >= ? AND timestamp < ?
		GROUP BY interface_name
	`, yearStart.Unix(), yearStart.Unix(), nextYearStart.Unix())

	return err
}

// Prune removes old data beyond retention periods
func (s *SQLiteStore) Prune(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	// Prune minute data older than 1 day
	_, _ = s.db.ExecContext(ctx, `DELETE FROM stats_minute WHERE timestamp < ?`,
		now.Add(-MinuteRetention).Unix())

	// Prune hour data older than 1 week
	_, _ = s.db.ExecContext(ctx, `DELETE FROM stats_hour WHERE timestamp < ?`,
		now.Add(-HourRetention).Unix())

	// Prune day data older than 1 month
	_, _ = s.db.ExecContext(ctx, `DELETE FROM stats_day WHERE timestamp < ?`,
		now.Add(-DayRetention).Unix())

	// Prune month data older than 1 year
	_, _ = s.db.ExecContext(ctx, `DELETE FROM stats_month WHERE timestamp < ?`,
		now.Add(-MonthRetention).Unix())

	// Year data kept forever

	// Prune WiFi client data with same retention periods
	_, _ = s.db.ExecContext(ctx, `DELETE FROM wifi_client_minute WHERE timestamp < ?`,
		now.Add(-MinuteRetention).Unix())
	_, _ = s.db.ExecContext(ctx, `DELETE FROM wifi_client_hour WHERE timestamp < ?`,
		now.Add(-HourRetention).Unix())
	_, _ = s.db.ExecContext(ctx, `DELETE FROM wifi_client_day WHERE timestamp < ?`,
		now.Add(-DayRetention).Unix())
	_, _ = s.db.ExecContext(ctx, `DELETE FROM wifi_client_month WHERE timestamp < ?`,
		now.Add(-MonthRetention).Unix())

	return nil
}

// RecordWiFiClientSample stores a WiFi client sample for aggregation
func (s *SQLiteStore) RecordWiFiClientSample(ctx context.Context, rates WiFiClientRates) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	minuteTs := truncateToMinute(now).Unix()

	// Check if we need to flush the previous minute first
	var existingMinuteTs sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT minute_ts FROM wifi_client_accumulator WHERE mac = ?
	`, rates.MAC).Scan(&existingMinuteTs)

	if err == nil && existingMinuteTs.Valid && existingMinuteTs.Int64 < minuteTs {
		// Minute boundary crossed - flush the old accumulator
		if err := s.flushWiFiClientAccumulator(ctx, rates.MAC, existingMinuteTs.Int64); err != nil {
			return err
		}
	}

	// Update or insert accumulator
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO wifi_client_accumulator (
			mac, interface_name, minute_ts,
			rx_bytes_delta, tx_bytes_delta,
			rx_rate_sum, tx_rate_sum,
			signal_sum, signal_min, signal_max,
			rx_bitrate_sum, tx_bitrate_sum,
			sample_count,
			last_rx_bytes, last_tx_bytes
		) VALUES (?, ?, ?, 0, 0, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
		ON CONFLICT(mac) DO UPDATE SET
			interface_name = ?,
			minute_ts = ?,
			rx_rate_sum = rx_rate_sum + ?,
			tx_rate_sum = tx_rate_sum + ?,
			signal_sum = signal_sum + ?,
			signal_min = CASE
				WHEN signal_min IS NULL OR ? < signal_min THEN ?
				ELSE signal_min
			END,
			signal_max = CASE
				WHEN signal_max IS NULL OR ? > signal_max THEN ?
				ELSE signal_max
			END,
			rx_bitrate_sum = rx_bitrate_sum + ?,
			tx_bitrate_sum = tx_bitrate_sum + ?,
			rx_bytes_delta = rx_bytes_delta + (? - last_rx_bytes),
			tx_bytes_delta = tx_bytes_delta + (? - last_tx_bytes),
			sample_count = sample_count + 1,
			last_rx_bytes = ?,
			last_tx_bytes = ?
	`,
		// INSERT values
		rates.MAC, rates.Interface, minuteTs,
		rates.RxBytesPerSec, rates.TxBytesPerSec,
		rates.Signal, rates.Signal, rates.Signal,
		rates.RxBitrate, rates.TxBitrate,
		rates.TotalRxBytes, rates.TotalTxBytes,
		// UPDATE values
		rates.Interface,
		minuteTs,
		rates.RxBytesPerSec,
		rates.TxBytesPerSec,
		rates.Signal,
		rates.Signal, rates.Signal,
		rates.Signal, rates.Signal,
		rates.RxBitrate,
		rates.TxBitrate,
		rates.TotalRxBytes,
		rates.TotalTxBytes,
		rates.TotalRxBytes,
		rates.TotalTxBytes,
	)

	return err
}

// flushWiFiClientAccumulator moves accumulated WiFi client data to the minute table
func (s *SQLiteStore) flushWiFiClientAccumulator(ctx context.Context, mac string, minuteTs int64) error {
	var interfaceName string
	var rxBytes, txBytes int64
	var rxRateSum, txRateSum float64
	var signalSum int64
	var signalMin, signalMax sql.NullInt64
	var rxBitrateSum, txBitrateSum float64
	var sampleCount int

	err := s.db.QueryRowContext(ctx, `
		SELECT interface_name, rx_bytes_delta, tx_bytes_delta,
		       rx_rate_sum, tx_rate_sum,
		       signal_sum, signal_min, signal_max,
		       rx_bitrate_sum, tx_bitrate_sum, sample_count
		FROM wifi_client_accumulator
		WHERE mac = ? AND minute_ts = ?
	`, mac, minuteTs).Scan(
		&interfaceName, &rxBytes, &txBytes,
		&rxRateSum, &txRateSum,
		&signalSum, &signalMin, &signalMax,
		&rxBitrateSum, &txBitrateSum, &sampleCount,
	)

	if err == sql.ErrNoRows || sampleCount == 0 {
		return nil
	}
	if err != nil {
		return err
	}

	// Insert into minute table
	_, err = s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO wifi_client_minute (
			mac, interface_name, timestamp,
			rx_bytes, tx_bytes,
			avg_rx_bytes_per_sec, avg_tx_bytes_per_sec,
			avg_signal, min_signal, max_signal,
			avg_rx_bitrate, avg_tx_bitrate,
			sample_count
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		mac, interfaceName, minuteTs,
		rxBytes, txBytes,
		rxRateSum/float64(sampleCount), txRateSum/float64(sampleCount),
		int(signalSum)/sampleCount, nullIntToZero(signalMin), nullIntToZero(signalMax),
		rxBitrateSum/float64(sampleCount), txBitrateSum/float64(sampleCount),
		sampleCount,
	)

	if err != nil {
		return err
	}

	// Reset the accumulator
	_, err = s.db.ExecContext(ctx, `
		UPDATE wifi_client_accumulator SET
			rx_bytes_delta = 0, tx_bytes_delta = 0,
			rx_rate_sum = 0, tx_rate_sum = 0,
			signal_sum = 0, signal_min = NULL, signal_max = NULL,
			rx_bitrate_sum = 0, tx_bitrate_sum = 0,
			sample_count = 0
		WHERE mac = ?
	`, mac)

	return err
}

// flushAllWiFiClientAccumulators flushes all WiFi client accumulators for completed minutes
func (s *SQLiteStore) flushAllWiFiClientAccumulators(ctx context.Context, now time.Time) error {
	currentMinute := truncateToMinute(now).Unix()

	rows, err := s.db.QueryContext(ctx, `
		SELECT mac, minute_ts FROM wifi_client_accumulator WHERE minute_ts < ?
	`, currentMinute)
	if err != nil {
		return err
	}
	defer rows.Close()

	var toFlush []struct {
		mac      string
		minuteTs int64
	}

	for rows.Next() {
		var mac string
		var ts int64
		if err := rows.Scan(&mac, &ts); err != nil {
			return err
		}
		toFlush = append(toFlush, struct {
			mac      string
			minuteTs int64
		}{mac, ts})
	}

	for _, item := range toFlush {
		if err := s.flushWiFiClientAccumulator(ctx, item.mac, item.minuteTs); err != nil {
			return err
		}
	}

	return nil
}

// rollupWiFiClientMinutesToHours aggregates WiFi client minute data into hourly buckets
func (s *SQLiteStore) rollupWiFiClientMinutesToHours(ctx context.Context, now time.Time) error {
	hourTs := truncateToHour(now.Add(-time.Hour)).Unix()
	nextHourTs := hourTs + 3600

	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO wifi_client_hour (
			mac, interface_name, timestamp,
			rx_bytes, tx_bytes,
			avg_rx_bytes_per_sec, avg_tx_bytes_per_sec,
			avg_signal, min_signal, max_signal,
			avg_rx_bitrate, avg_tx_bitrate,
			sample_count
		)
		SELECT
			mac,
			interface_name,
			? as timestamp,
			SUM(rx_bytes),
			SUM(tx_bytes),
			SUM(avg_rx_bytes_per_sec * sample_count) / SUM(sample_count),
			SUM(avg_tx_bytes_per_sec * sample_count) / SUM(sample_count),
			SUM(avg_signal * sample_count) / SUM(sample_count),
			MIN(min_signal),
			MAX(max_signal),
			SUM(avg_rx_bitrate * sample_count) / SUM(sample_count),
			SUM(avg_tx_bitrate * sample_count) / SUM(sample_count),
			SUM(sample_count)
		FROM wifi_client_minute
		WHERE timestamp >= ? AND timestamp < ?
		GROUP BY mac
	`, hourTs, hourTs, nextHourTs)

	return err
}

// rollupWiFiClientHoursToDays aggregates WiFi client hourly data into daily buckets
func (s *SQLiteStore) rollupWiFiClientHoursToDays(ctx context.Context, now time.Time) error {
	dayTs := truncateToDay(now.Add(-24 * time.Hour)).Unix()
	nextDayTs := dayTs + 86400

	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO wifi_client_day (
			mac, interface_name, timestamp,
			rx_bytes, tx_bytes,
			avg_rx_bytes_per_sec, avg_tx_bytes_per_sec,
			avg_signal, min_signal, max_signal,
			avg_rx_bitrate, avg_tx_bitrate,
			sample_count
		)
		SELECT
			mac,
			interface_name,
			? as timestamp,
			SUM(rx_bytes),
			SUM(tx_bytes),
			SUM(avg_rx_bytes_per_sec * sample_count) / SUM(sample_count),
			SUM(avg_tx_bytes_per_sec * sample_count) / SUM(sample_count),
			SUM(avg_signal * sample_count) / SUM(sample_count),
			MIN(min_signal),
			MAX(max_signal),
			SUM(avg_rx_bitrate * sample_count) / SUM(sample_count),
			SUM(avg_tx_bitrate * sample_count) / SUM(sample_count),
			SUM(sample_count)
		FROM wifi_client_hour
		WHERE timestamp >= ? AND timestamp < ?
		GROUP BY mac
	`, dayTs, dayTs, nextDayTs)

	return err
}

// rollupWiFiClientDaysToMonths aggregates WiFi client daily data into monthly buckets
func (s *SQLiteStore) rollupWiFiClientDaysToMonths(ctx context.Context, now time.Time) error {
	monthStart := truncateToMonth(now.AddDate(0, -1, 0))
	nextMonthStart := truncateToMonth(now)

	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO wifi_client_month (
			mac, interface_name, timestamp,
			rx_bytes, tx_bytes,
			avg_rx_bytes_per_sec, avg_tx_bytes_per_sec,
			avg_signal, min_signal, max_signal,
			avg_rx_bitrate, avg_tx_bitrate,
			sample_count
		)
		SELECT
			mac,
			interface_name,
			? as timestamp,
			SUM(rx_bytes),
			SUM(tx_bytes),
			SUM(avg_rx_bytes_per_sec * sample_count) / SUM(sample_count),
			SUM(avg_tx_bytes_per_sec * sample_count) / SUM(sample_count),
			SUM(avg_signal * sample_count) / SUM(sample_count),
			MIN(min_signal),
			MAX(max_signal),
			SUM(avg_rx_bitrate * sample_count) / SUM(sample_count),
			SUM(avg_tx_bitrate * sample_count) / SUM(sample_count),
			SUM(sample_count)
		FROM wifi_client_day
		WHERE timestamp >= ? AND timestamp < ?
		GROUP BY mac
	`, monthStart.Unix(), monthStart.Unix(), nextMonthStart.Unix())

	return err
}

// rollupWiFiClientMonthsToYears aggregates WiFi client monthly data into yearly buckets
func (s *SQLiteStore) rollupWiFiClientMonthsToYears(ctx context.Context, now time.Time) error {
	yearStart := truncateToYear(now.AddDate(-1, 0, 0))
	nextYearStart := truncateToYear(now)

	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO wifi_client_year (
			mac, interface_name, timestamp,
			rx_bytes, tx_bytes,
			avg_rx_bytes_per_sec, avg_tx_bytes_per_sec,
			avg_signal, min_signal, max_signal,
			avg_rx_bitrate, avg_tx_bitrate,
			sample_count
		)
		SELECT
			mac,
			interface_name,
			? as timestamp,
			SUM(rx_bytes),
			SUM(tx_bytes),
			SUM(avg_rx_bytes_per_sec * sample_count) / SUM(sample_count),
			SUM(avg_tx_bytes_per_sec * sample_count) / SUM(sample_count),
			SUM(avg_signal * sample_count) / SUM(sample_count),
			MIN(min_signal),
			MAX(max_signal),
			SUM(avg_rx_bitrate * sample_count) / SUM(sample_count),
			SUM(avg_tx_bitrate * sample_count) / SUM(sample_count),
			SUM(sample_count)
		FROM wifi_client_month
		WHERE timestamp >= ? AND timestamp < ?
		GROUP BY mac
	`, yearStart.Unix(), yearStart.Unix(), nextYearStart.Unix())

	return err
}
