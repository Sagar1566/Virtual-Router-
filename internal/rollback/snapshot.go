package rollback

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/iodesystems/ubuntu-router/internal/config"
	"github.com/iodesystems/ubuntu-router/internal/system"
)

// CreateSnapshot captures the current config and system files
func CreateSnapshot(fs system.FileSystem, configPath string, cfg *config.Config) (*Snapshot, error) {
	return CreateSnapshotWithExtra(fs, configPath, cfg, nil)
}

// CreateSnapshotWithExtra captures config and system files plus additional files
func CreateSnapshotWithExtra(fs system.FileSystem, configPath string, cfg *config.Config, extraFiles []string) (*Snapshot, error) {
	snapshot := &Snapshot{
		ID:        uuid.New().String(),
		Timestamp: time.Now(),
		Files:     make(map[string][]byte),
	}

	// Snapshot the config
	configData, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}
	snapshot.Config = configData

	// Build list of files to snapshot
	filesToSnapshot := make([]string, 0, len(SystemFiles)+len(extraFiles))
	filesToSnapshot = append(filesToSnapshot, SystemFiles...)
	filesToSnapshot = append(filesToSnapshot, extraFiles...)

	// Also include WireGuard configs if they exist
	if cfg.WireGuard != nil && cfg.WireGuard.ConfigPath != "" {
		filesToSnapshot = append(filesToSnapshot, cfg.WireGuard.ConfigPath)
	}
	if cfg.WireGuardP2P != nil {
		for _, tunnel := range cfg.WireGuardP2P.Tunnels {
			wgPath := fmt.Sprintf("/etc/wireguard/%s.conf", tunnel.Interface)
			filesToSnapshot = append(filesToSnapshot, wgPath)
		}
	}

	// Snapshot system files (ignore errors for files that don't exist yet)
	for _, path := range filesToSnapshot {
		data, err := fs.ReadFile(path)
		if err != nil {
			// File doesn't exist yet, that's OK - store empty to indicate "delete on rollback"
			log.Printf("Snapshot: file %s doesn't exist (will be removed on rollback)", path)
			continue
		}
		snapshot.Files[path] = data
	}

	log.Printf("Created snapshot %s with %d system files", snapshot.ID, len(snapshot.Files))
	return snapshot, nil
}

// RestoreSnapshot restores config and system files from a snapshot
func RestoreSnapshot(ctx context.Context, fs system.FileSystem, runner system.CommandRunner, configPath string, snapshot *Snapshot) error {
	log.Printf("Restoring snapshot %s from %s", snapshot.ID, snapshot.Timestamp.Format(time.RFC3339))

	// Restore config file
	if len(snapshot.Config) > 0 {
		if err := fs.WriteFile(configPath, snapshot.Config, 0600); err != nil {
			return fmt.Errorf("failed to restore config: %w", err)
		}
		log.Printf("Restored config to %s", configPath)
	}

	// Restore system files
	for path, data := range snapshot.Files {
		if err := fs.WriteFile(path, data, 0644); err != nil {
			log.Printf("Warning: failed to restore %s: %v", path, err)
			continue
		}
		log.Printf("Restored %s (%d bytes)", path, len(data))
	}

	// Remove files that didn't exist in the snapshot
	for _, path := range SystemFiles {
		if _, exists := snapshot.Files[path]; !exists {
			// File wasn't in snapshot, remove it
			if err := fs.Remove(path); err != nil {
				// Ignore errors (file might not exist)
				log.Printf("Note: couldn't remove %s (may not exist): %v", path, err)
			} else {
				log.Printf("Removed %s (didn't exist in snapshot)", path)
			}
		}
	}

	// Re-apply network configuration
	log.Printf("Applying netplan...")
	if _, err := runner.Run(ctx, "netplan", "apply"); err != nil {
		log.Printf("Warning: netplan apply failed: %v", err)
		// Don't fail - the config is restored, network might come back
	}

	// Restart dnsmasq
	log.Printf("Restarting dnsmasq...")
	if _, err := runner.Run(ctx, "systemctl", "restart", "dnsmasq"); err != nil {
		log.Printf("Warning: dnsmasq restart failed: %v", err)
	}

	// Restart any WireGuard interfaces that were in the snapshot
	for path := range snapshot.Files {
		if strings.HasPrefix(path, "/etc/wireguard/") && strings.HasSuffix(path, ".conf") {
			// Extract interface name from path: /etc/wireguard/wg0.conf -> wg0
			ifaceName := strings.TrimSuffix(strings.TrimPrefix(path, "/etc/wireguard/"), ".conf")
			log.Printf("Restarting WireGuard interface %s...", ifaceName)
			// Try to restart the interface (it might not be running)
			if _, err := runner.Run(ctx, "wg-quick", "down", ifaceName); err != nil {
				log.Printf("Note: wg-quick down %s: %v (may not have been running)", ifaceName, err)
			}
			if _, err := runner.Run(ctx, "wg-quick", "up", ifaceName); err != nil {
				log.Printf("Warning: wg-quick up %s failed: %v", ifaceName, err)
			}
		}
	}

	log.Printf("Snapshot restore complete")
	return nil
}

// LoadConfig parses the config from a snapshot
func LoadConfigFromSnapshot(snapshot *Snapshot) (*config.Config, error) {
	if len(snapshot.Config) == 0 {
		return nil, fmt.Errorf("snapshot has no config data")
	}

	cfg := config.DefaultConfig()
	if err := json.Unmarshal(snapshot.Config, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse snapshot config: %w", err)
	}

	return cfg, nil
}
