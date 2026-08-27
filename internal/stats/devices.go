package stats

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// GetDevice retrieves a device by MAC address
func (s *SQLiteStore) GetDevice(ctx context.Context, mac string) (*Device, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	mac = strings.ToLower(mac)

	var d Device
	err := s.db.QueryRowContext(ctx, `
		SELECT mac, hostname, display_name, vendor, first_seen, last_seen, last_ip, notes
		FROM devices WHERE mac = ?
	`, mac).Scan(&d.MAC, &d.Hostname, &d.DisplayName, &d.Vendor, &d.FirstSeen, &d.LastSeen, &d.LastIP, &d.Notes)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &d, nil
}

// GetAllDevices retrieves all known devices
func (s *SQLiteStore) GetAllDevices(ctx context.Context) ([]Device, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT mac, hostname, display_name, vendor, first_seen, last_seen, last_ip, notes
		FROM devices ORDER BY last_seen DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []Device
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.MAC, &d.Hostname, &d.DisplayName, &d.Vendor, &d.FirstSeen, &d.LastSeen, &d.LastIP, &d.Notes); err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}

	return devices, rows.Err()
}

// UpsertDevice creates or updates a device
func (s *SQLiteStore) UpsertDevice(ctx context.Context, device Device) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	device.MAC = strings.ToLower(device.MAC)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO devices (mac, hostname, display_name, vendor, first_seen, last_seen, last_ip, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(mac) DO UPDATE SET
			hostname = CASE WHEN excluded.hostname != '' THEN excluded.hostname ELSE hostname END,
			display_name = CASE WHEN excluded.display_name != '' THEN excluded.display_name ELSE display_name END,
			vendor = CASE WHEN excluded.vendor != '' THEN excluded.vendor ELSE vendor END,
			last_seen = excluded.last_seen,
			last_ip = CASE WHEN excluded.last_ip != '' THEN excluded.last_ip ELSE last_ip END,
			notes = CASE WHEN excluded.notes != '' THEN excluded.notes ELSE notes END
	`, device.MAC, device.Hostname, device.DisplayName, device.Vendor, device.FirstSeen, device.LastSeen, device.LastIP, device.Notes)

	return err
}

// DeleteDevice removes a device by MAC
func (s *SQLiteStore) DeleteDevice(ctx context.Context, mac string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	mac = strings.ToLower(mac)
	_, err := s.db.ExecContext(ctx, `DELETE FROM devices WHERE mac = ?`, mac)
	return err
}

// UpdateDeviceSeen updates the last seen time and optionally hostname/IP for a device
func (s *SQLiteStore) UpdateDeviceSeen(ctx context.Context, mac, ip, hostname string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	mac = strings.ToLower(mac)
	now := time.Now().Unix()

	// Try to update existing device
	result, err := s.db.ExecContext(ctx, `
		UPDATE devices SET
			last_seen = ?,
			last_ip = CASE WHEN ? != '' THEN ? ELSE last_ip END,
			hostname = CASE WHEN ? != '' THEN ? ELSE hostname END
		WHERE mac = ?
	`, now, ip, ip, hostname, hostname, mac)

	if err != nil {
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		// Device doesn't exist, create it
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO devices (mac, hostname, display_name, vendor, first_seen, last_seen, last_ip, notes)
			VALUES (?, ?, '', '', ?, ?, ?, '')
		`, mac, hostname, now, now, ip)
	}

	return err
}

// LogNotificationEvent records a notification event
func (s *SQLiteStore) LogNotificationEvent(ctx context.Context, event NotificationEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sentInt := 0
	if event.Sent {
		sentInt = 1
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO notification_events (timestamp, event_type, title, message, priority, sent, error)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, event.Timestamp, event.EventType, event.Title, event.Message, event.Priority, sentInt, event.Error)

	return err
}

// GetNotificationEvents retrieves recent notification events
func (s *SQLiteStore) GetNotificationEvents(ctx context.Context, limit int) ([]NotificationEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, timestamp, event_type, title, message, priority, sent, COALESCE(error, '')
		FROM notification_events
		ORDER BY timestamp DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []NotificationEvent
	for rows.Next() {
		var e NotificationEvent
		var sentInt int
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.EventType, &e.Title, &e.Message, &e.Priority, &sentInt, &e.Error); err != nil {
			return nil, err
		}
		e.Sent = sentInt != 0
		events = append(events, e)
	}

	return events, rows.Err()
}

// GetDeviceDisplayName returns the display name or hostname for a MAC, or the MAC itself if unknown
func (s *SQLiteStore) GetDeviceDisplayName(ctx context.Context, mac string) string {
	device, err := s.GetDevice(ctx, mac)
	if err != nil || device == nil {
		return mac
	}

	if device.DisplayName != "" {
		return device.DisplayName
	}
	if device.Hostname != "" {
		return device.Hostname
	}
	return mac
}
