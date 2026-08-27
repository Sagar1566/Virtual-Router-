package stats

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// =============================================================================
// DHCP Reservations
// =============================================================================

// GetDHCPReservations retrieves all DHCP reservations
func (s *SQLiteStore) GetDHCPReservations(ctx context.Context) ([]DHCPReservation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, mac, ip, created_at, updated_at
		FROM dhcp_reservations
		ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reservations []DHCPReservation
	for rows.Next() {
		var r DHCPReservation
		if err := rows.Scan(&r.ID, &r.Name, &r.MAC, &r.IP, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		reservations = append(reservations, r)
	}

	return reservations, rows.Err()
}

// GetDHCPReservationByMAC retrieves a reservation by MAC address
func (s *SQLiteStore) GetDHCPReservationByMAC(ctx context.Context, mac string) (*DHCPReservation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	mac = strings.ToLower(mac)

	var r DHCPReservation
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, mac, ip, created_at, updated_at
		FROM dhcp_reservations WHERE mac = ?
	`, mac).Scan(&r.ID, &r.Name, &r.MAC, &r.IP, &r.CreatedAt, &r.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &r, nil
}

// AddDHCPReservation adds or updates a DHCP reservation
func (s *SQLiteStore) AddDHCPReservation(ctx context.Context, res DHCPReservation) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res.MAC = strings.ToLower(res.MAC)
	now := time.Now().Unix()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO dhcp_reservations (name, mac, ip, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(mac) DO UPDATE SET
			name = excluded.name,
			ip = excluded.ip,
			updated_at = excluded.updated_at
	`, res.Name, res.MAC, res.IP, now, now)

	return err
}

// DeleteDHCPReservation removes a DHCP reservation by MAC
func (s *SQLiteStore) DeleteDHCPReservation(ctx context.Context, mac string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	mac = strings.ToLower(mac)
	_, err := s.db.ExecContext(ctx, `DELETE FROM dhcp_reservations WHERE mac = ?`, mac)
	return err
}

// =============================================================================
// DNS Entries
// =============================================================================

// GetDNSEntries retrieves all DNS entries
func (s *SQLiteStore) GetDNSEntries(ctx context.Context) ([]DNSEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, hostname, ip, created_at, updated_at
		FROM dns_entries
		ORDER BY hostname
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []DNSEntry
	for rows.Next() {
		var e DNSEntry
		if err := rows.Scan(&e.ID, &e.Hostname, &e.IP, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}

	return entries, rows.Err()
}

// AddDNSEntry adds a DNS entry
func (s *SQLiteStore) AddDNSEntry(ctx context.Context, entry DNSEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO dns_entries (hostname, ip, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(hostname, ip) DO UPDATE SET
			updated_at = excluded.updated_at
	`, entry.Hostname, entry.IP, now, now)

	return err
}

// DeleteDNSEntry removes a DNS entry by hostname
func (s *SQLiteStore) DeleteDNSEntry(ctx context.Context, hostname string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(ctx, `DELETE FROM dns_entries WHERE hostname = ?`, hostname)
	return err
}

// =============================================================================
// Port Forwards
// =============================================================================

// GetPortForwards retrieves all port forwards
func (s *SQLiteStore) GetPortForwards(ctx context.Context) ([]PortForward, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, protocol, external_port, internal_ip, internal_port, enabled, created_at, updated_at
		FROM port_forwards
		ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var forwards []PortForward
	for rows.Next() {
		var pf PortForward
		var enabled int
		if err := rows.Scan(&pf.ID, &pf.Name, &pf.Protocol, &pf.ExternalPort, &pf.InternalIP, &pf.InternalPort, &enabled, &pf.CreatedAt, &pf.UpdatedAt); err != nil {
			return nil, err
		}
		pf.Enabled = enabled != 0
		forwards = append(forwards, pf)
	}

	return forwards, rows.Err()
}

// AddPortForward adds a port forward rule
func (s *SQLiteStore) AddPortForward(ctx context.Context, pf PortForward) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()
	enabled := 0
	if pf.Enabled {
		enabled = 1
	}

	if pf.Protocol == "" {
		pf.Protocol = "tcp"
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO port_forwards (name, protocol, external_port, internal_ip, internal_port, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, pf.Name, pf.Protocol, pf.ExternalPort, pf.InternalIP, pf.InternalPort, enabled, now, now)

	return err
}

// UpdatePortForward updates an existing port forward
func (s *SQLiteStore) UpdatePortForward(ctx context.Context, pf PortForward) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()
	enabled := 0
	if pf.Enabled {
		enabled = 1
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE port_forwards SET
			protocol = ?,
			external_port = ?,
			internal_ip = ?,
			internal_port = ?,
			enabled = ?,
			updated_at = ?
		WHERE name = ?
	`, pf.Protocol, pf.ExternalPort, pf.InternalIP, pf.InternalPort, enabled, now, pf.Name)

	return err
}

// DeletePortForward removes a port forward by name
func (s *SQLiteStore) DeletePortForward(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(ctx, `DELETE FROM port_forwards WHERE name = ?`, name)
	return err
}

// =============================================================================
// Migration helpers
// =============================================================================

// MigrateDHCPReservations imports reservations from config
func (s *SQLiteStore) MigrateDHCPReservations(ctx context.Context, reservations []DHCPReservation) error {
	for _, r := range reservations {
		if err := s.AddDHCPReservation(ctx, r); err != nil {
			return err
		}
	}
	return nil
}

// MigrateDNSEntries imports DNS entries from config
func (s *SQLiteStore) MigrateDNSEntries(ctx context.Context, entries []DNSEntry) error {
	for _, e := range entries {
		if err := s.AddDNSEntry(ctx, e); err != nil {
			return err
		}
	}
	return nil
}

// MigratePortForwards imports port forwards from config
func (s *SQLiteStore) MigratePortForwards(ctx context.Context, forwards []PortForward) error {
	for _, pf := range forwards {
		if err := s.AddPortForward(ctx, pf); err != nil {
			return err
		}
	}
	return nil
}
