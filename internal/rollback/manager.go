package rollback

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/iodesystems/ubuntu-router/internal/config"
	"github.com/iodesystems/ubuntu-router/internal/system"
)

// DefaultTimeout is the default time to wait for confirmation
const DefaultTimeout = 30 * time.Second

// Manager handles configuration rollback with commit-confirm semantics
type Manager struct {
	mu         sync.Mutex
	fs         system.FileSystem
	runner     system.CommandRunner
	configPath string

	state   State
	pending *PendingChange
	timer   *time.Timer

	// Callback to reload config in server after rollback
	onRollback func(*config.Config)
}

// NewManager creates a new rollback manager
func NewManager(fs system.FileSystem, runner system.CommandRunner, configPath string) *Manager {
	return &Manager{
		fs:         fs,
		runner:     runner,
		configPath: configPath,
		state:      StateIdle,
	}
}

// SetOnRollback sets the callback function called after a rollback
func (m *Manager) SetOnRollback(fn func(*config.Config)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onRollback = fn
}

// BeginChange starts a pending change that requires confirmation
// Returns an error if there's already a pending change
func (m *Manager) BeginChange(reason string, snapshot *Snapshot, timeout time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state == StatePending {
		return fmt.Errorf("another change is pending confirmation (reason: %s)", m.pending.Reason)
	}

	if timeout == 0 {
		timeout = DefaultTimeout
	}

	m.pending = &PendingChange{
		Snapshot:  snapshot,
		ExpiresAt: time.Now().Add(timeout),
		Reason:    reason,
	}
	m.state = StatePending

	// Start rollback timer
	m.timer = time.AfterFunc(timeout, func() {
		m.rollback()
	})

	log.Printf("Rollback: pending change started (reason: %s, timeout: %s)", reason, timeout)
	return nil
}

// Confirm confirms the pending change, stopping the rollback timer
func (m *Manager) Confirm() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state != StatePending {
		return fmt.Errorf("no pending change to confirm")
	}

	// Stop the timer
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}

	log.Printf("Rollback: change confirmed (reason: %s)", m.pending.Reason)

	m.pending = nil
	m.state = StateIdle

	return nil
}

// Cancel cancels the pending change and performs immediate rollback
func (m *Manager) Cancel() error {
	m.mu.Lock()

	if m.state != StatePending {
		m.mu.Unlock()
		return fmt.Errorf("no pending change to cancel")
	}

	// Stop the timer (we'll do rollback manually)
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}

	log.Printf("Rollback: change cancelled, performing rollback (reason: %s)", m.pending.Reason)
	m.mu.Unlock()

	// Perform rollback (this acquires the lock internally)
	m.rollback()

	return nil
}

// GetPending returns the current pending change status
func (m *Manager) GetPending() *PendingStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	status := &PendingStatus{
		Pending: m.state == StatePending,
	}

	if m.pending != nil {
		status.ExpiresAt = m.pending.ExpiresAt
		status.SecondsLeft = int(time.Until(m.pending.ExpiresAt).Seconds())
		if status.SecondsLeft < 0 {
			status.SecondsLeft = 0
		}
		status.Reason = m.pending.Reason
		status.SnapshotID = m.pending.Snapshot.ID
		status.SnapshotTime = m.pending.Snapshot.Timestamp
	}

	return status
}

// IsPending returns true if there's a pending change
func (m *Manager) IsPending() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state == StatePending
}

// rollback performs the actual rollback (called by timer or Cancel)
func (m *Manager) rollback() {
	m.mu.Lock()

	if m.state != StatePending || m.pending == nil {
		m.mu.Unlock()
		return
	}

	snapshot := m.pending.Snapshot
	reason := m.pending.Reason

	// Clear state before releasing lock
	m.pending = nil
	m.state = StateIdle
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}

	onRollback := m.onRollback
	m.mu.Unlock()

	log.Printf("Rollback: executing rollback (reason: %s, snapshot: %s)", reason, snapshot.ID)

	// Restore the snapshot
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := RestoreSnapshot(ctx, m.fs, m.runner, m.configPath, snapshot); err != nil {
		log.Printf("Rollback: ERROR restoring snapshot: %v", err)
		return
	}

	// Reload config in server
	if onRollback != nil {
		cfg, err := LoadConfigFromSnapshot(snapshot)
		if err != nil {
			log.Printf("Rollback: ERROR loading config from snapshot: %v", err)
			return
		}
		onRollback(cfg)
	}

	log.Printf("Rollback: complete")
}

// MakeChangeResponse creates a ChangeResponse for API returns
func (m *Manager) MakeChangeResponse(success bool, message string) *ChangeResponse {
	status := m.GetPending()

	return &ChangeResponse{
		Success:             success,
		Message:             message,
		PendingConfirmation: status.Pending,
		ExpiresAt:           status.ExpiresAt,
		SecondsLeft:         status.SecondsLeft,
	}
}

// MakeErrorResponse creates a ChangeResponse for errors
func MakeErrorResponse(err error) *ChangeResponse {
	return &ChangeResponse{
		Success: false,
		Error:   err.Error(),
	}
}
