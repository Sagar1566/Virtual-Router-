package rollback

import (
	"time"
)

// State represents the rollback manager's state
type State int

const (
	// StateIdle means no pending changes
	StateIdle State = iota
	// StatePending means changes were applied and awaiting confirmation
	StatePending
)

// Snapshot captures the state before a change
type Snapshot struct {
	ID        string            `json:"id"`
	Timestamp time.Time         `json:"timestamp"`
	Config    []byte            `json:"-"` // JSON config contents
	Files     map[string][]byte `json:"-"` // path -> file contents
}

// PendingChange represents a change awaiting confirmation
type PendingChange struct {
	Snapshot  *Snapshot `json:"-"`
	ExpiresAt time.Time `json:"expires_at"`
	Reason    string    `json:"reason"` // "wan_configure", "apply", etc.
}

// PendingStatus is returned by the API to show pending change info
type PendingStatus struct {
	Pending      bool      `json:"pending"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	SecondsLeft  int       `json:"seconds_left,omitempty"`
	Reason       string    `json:"reason,omitempty"`
	SnapshotID   string    `json:"snapshot_id,omitempty"`
	SnapshotTime time.Time `json:"snapshot_time,omitempty"`
}

// ChangeResponse is returned when a dangerous operation starts a pending change
type ChangeResponse struct {
	Success             bool      `json:"success"`
	Message             string    `json:"message,omitempty"`
	Error               string    `json:"error,omitempty"`
	PendingConfirmation bool      `json:"pending_confirmation"`
	ExpiresAt           time.Time `json:"expires_at,omitempty"`
	SecondsLeft         int       `json:"seconds_left,omitempty"`
}

// System files that need to be snapshotted for rollback
var SystemFiles = []string{
	"/etc/netplan/99-ubuntu-router.yaml",
	"/etc/dnsmasq.d/ubuntu-router-dns.conf",
	"/etc/dnsmasq.d/ubuntu-router-dhcp.conf",
	"/etc/hosts.ubuntu-router",
}
