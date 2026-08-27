// Package notifications provides push notification functionality via ntfy.sh
package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/iodesystems/ubuntu-router/internal/stats"
)

// Priority levels for ntfy
const (
	PriorityMin     = 1
	PriorityLow     = 2
	PriorityDefault = 3
	PriorityHigh    = 4
	PriorityUrgent  = 5
)

// Event types
const (
	EventWiFiClientConnected    = "wifi_client_connected"
	EventWiFiClientDisconnected = "wifi_client_disconnected"
	EventHealthCheckFailed      = "health_check_failed"
	EventHealthCheckRecovered   = "health_check_recovered"
	EventWANConnected           = "wan_connected"
	EventWANDisconnected        = "wan_disconnected"
	EventWANFailover            = "wan_failover"
	EventVPNPeerConnected       = "vpn_peer_connected"
	EventVPNPeerDisconnected    = "vpn_peer_disconnected"
	EventSystemStartup          = "system_startup"
	EventSystemShutdown         = "system_shutdown"
)

// Config holds ntfy notification settings
type Config struct {
	Enabled  bool   `json:"enabled"`
	Server   string `json:"server"`   // e.g., "https://ntfy.sh" or self-hosted
	Topic    string `json:"topic"`    // e.g., "my-router-alerts"
	Token    string `json:"token"`    // Optional access token
	Username string `json:"username"` // Optional basic auth
	Password string `json:"password"` // Optional basic auth

	// Event filters - which events to send
	NotifyWiFiClients   bool `json:"notify_wifi_clients"`
	NotifyHealthChecks  bool `json:"notify_health_checks"`
	NotifyWANChanges    bool `json:"notify_wan_changes"`
	NotifyVPNPeers      bool `json:"notify_vpn_peers"`
	NotifySystemEvents  bool `json:"notify_system_events"`
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		Enabled:             false,
		Server:              "https://ntfy.sh",
		Topic:               "",
		NotifyWiFiClients:   true,
		NotifyHealthChecks:  true,
		NotifyWANChanges:    true,
		NotifyVPNPeers:      true,
		NotifySystemEvents:  true,
	}
}

// Message represents a notification message
type Message struct {
	Topic    string   `json:"topic"`
	Title    string   `json:"title,omitempty"`
	Message  string   `json:"message"`
	Priority int      `json:"priority,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Click    string   `json:"click,omitempty"`
}

// Notifier handles sending notifications
type Notifier struct {
	mu     sync.RWMutex
	config Config
	store  stats.Store
	client *http.Client

	// Track connected clients to detect changes
	connectedClients map[string]bool
}

// New creates a new Notifier
func New(store stats.Store) *Notifier {
	return &Notifier{
		config:           DefaultConfig(),
		store:            store,
		client:           &http.Client{Timeout: 10 * time.Second},
		connectedClients: make(map[string]bool),
	}
}

// SetConfig updates the notification configuration
func (n *Notifier) SetConfig(config Config) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.config = config
}

// GetConfig returns the current configuration
func (n *Notifier) GetConfig() Config {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.config
}

// Send sends a notification message
func (n *Notifier) Send(ctx context.Context, eventType, title, message string, priority int, tags ...string) error {
	n.mu.RLock()
	config := n.config
	n.mu.RUnlock()

	// Log the event regardless of whether notifications are enabled
	if n.store != nil {
		event := stats.NotificationEvent{
			Timestamp: time.Now().Unix(),
			EventType: eventType,
			Title:     title,
			Message:   message,
			Priority:  priority,
			Sent:      false,
		}

		if !config.Enabled || config.Topic == "" {
			event.Error = "notifications disabled"
			_ = n.store.LogNotificationEvent(ctx, event)
			return nil
		}

		// Check if this event type should be sent
		if !n.shouldSendEventType(eventType) {
			event.Error = "event type filtered"
			_ = n.store.LogNotificationEvent(ctx, event)
			return nil
		}

		err := n.sendToNtfy(ctx, config, title, message, priority, tags)
		event.Sent = err == nil
		if err != nil {
			event.Error = err.Error()
		}
		_ = n.store.LogNotificationEvent(ctx, event)
		return err
	}

	if !config.Enabled || config.Topic == "" {
		return nil
	}

	if !n.shouldSendEventType(eventType) {
		return nil
	}

	return n.sendToNtfy(ctx, config, title, message, priority, tags)
}

func (n *Notifier) shouldSendEventType(eventType string) bool {
	n.mu.RLock()
	config := n.config
	n.mu.RUnlock()

	switch eventType {
	case EventWiFiClientConnected, EventWiFiClientDisconnected:
		return config.NotifyWiFiClients
	case EventHealthCheckFailed, EventHealthCheckRecovered:
		return config.NotifyHealthChecks
	case EventWANConnected, EventWANDisconnected, EventWANFailover:
		return config.NotifyWANChanges
	case EventVPNPeerConnected, EventVPNPeerDisconnected:
		return config.NotifyVPNPeers
	case EventSystemStartup, EventSystemShutdown:
		return config.NotifySystemEvents
	default:
		return true
	}
}

func (n *Notifier) sendToNtfy(ctx context.Context, config Config, title, message string, priority int, tags []string) error {
	msg := Message{
		Topic:    config.Topic,
		Title:    title,
		Message:  message,
		Priority: priority,
		Tags:     tags,
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	url := strings.TrimSuffix(config.Server, "/")
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Add authentication if configured
	if config.Token != "" {
		req.Header.Set("Authorization", "Bearer "+config.Token)
	} else if config.Username != "" && config.Password != "" {
		req.SetBasicAuth(config.Username, config.Password)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ntfy returned status %d", resp.StatusCode)
	}

	return nil
}

// NotifyWiFiClientConnected sends a notification for a new WiFi client
func (n *Notifier) NotifyWiFiClientConnected(ctx context.Context, mac, hostname, ip string) error {
	name := hostname
	if name == "" {
		name = mac
	}

	title := "WiFi Client Connected"
	message := fmt.Sprintf("%s connected", name)
	if ip != "" {
		message += fmt.Sprintf(" (IP: %s)", ip)
	}

	return n.Send(ctx, EventWiFiClientConnected, title, message, PriorityDefault, "wifi", "arrow_up")
}

// NotifyWiFiClientDisconnected sends a notification for a disconnected WiFi client
func (n *Notifier) NotifyWiFiClientDisconnected(ctx context.Context, mac, hostname string) error {
	name := hostname
	if name == "" {
		name = mac
	}

	title := "WiFi Client Disconnected"
	message := fmt.Sprintf("%s disconnected", name)

	return n.Send(ctx, EventWiFiClientDisconnected, title, message, PriorityLow, "wifi", "arrow_down")
}

// NotifyHealthCheckFailed sends a notification for a failed health check
func (n *Notifier) NotifyHealthCheckFailed(ctx context.Context, checkName, errorMsg string) error {
	title := "Health Check Failed"
	message := fmt.Sprintf("%s: %s", checkName, errorMsg)

	return n.Send(ctx, EventHealthCheckFailed, title, message, PriorityHigh, "warning", "health")
}

// NotifyHealthCheckRecovered sends a notification for a recovered health check
func (n *Notifier) NotifyHealthCheckRecovered(ctx context.Context, checkName string) error {
	title := "Health Check Recovered"
	message := fmt.Sprintf("%s is now healthy", checkName)

	return n.Send(ctx, EventHealthCheckRecovered, title, message, PriorityDefault, "white_check_mark", "health")
}

// NotifyWANConnected sends a notification for WAN connection
func (n *Notifier) NotifyWANConnected(ctx context.Context, interfaceName, ip string) error {
	title := "WAN Connected"
	message := fmt.Sprintf("%s connected with IP %s", interfaceName, ip)

	return n.Send(ctx, EventWANConnected, title, message, PriorityDefault, "globe_with_meridians", "connected")
}

// NotifyWANDisconnected sends a notification for WAN disconnection
func (n *Notifier) NotifyWANDisconnected(ctx context.Context, interfaceName string) error {
	title := "WAN Disconnected"
	message := fmt.Sprintf("%s lost connection", interfaceName)

	return n.Send(ctx, EventWANDisconnected, title, message, PriorityHigh, "warning", "disconnected")
}

// NotifyWANFailover sends a notification for WAN failover
func (n *Notifier) NotifyWANFailover(ctx context.Context, fromInterface, toInterface string) error {
	title := "WAN Failover"
	message := fmt.Sprintf("Switched from %s to %s", fromInterface, toInterface)

	return n.Send(ctx, EventWANFailover, title, message, PriorityHigh, "rotating_light", "failover")
}

// NotifySystemStartup sends a notification for system startup
func (n *Notifier) NotifySystemStartup(ctx context.Context) error {
	title := "Router Started"
	message := "Ubuntu Router service has started"

	return n.Send(ctx, EventSystemStartup, title, message, PriorityDefault, "rocket")
}

// UpdateConnectedClients checks for WiFi client changes and sends notifications
func (n *Notifier) UpdateConnectedClients(ctx context.Context, clients []stats.WiFiClientRates) {
	n.mu.Lock()
	defer n.mu.Unlock()

	currentClients := make(map[string]bool)
	for _, c := range clients {
		currentClients[c.MAC] = true

		// Check for new connections
		if !n.connectedClients[c.MAC] {
			// Get hostname from store if available
			hostname := ""
			if n.store != nil {
				if device, err := n.store.GetDevice(ctx, c.MAC); err == nil && device != nil {
					if device.DisplayName != "" {
						hostname = device.DisplayName
					} else {
						hostname = device.Hostname
					}
				}
			}
			go n.NotifyWiFiClientConnected(ctx, c.MAC, hostname, "")
		}
	}

	// Check for disconnections
	for mac := range n.connectedClients {
		if !currentClients[mac] {
			hostname := ""
			if n.store != nil {
				if device, err := n.store.GetDevice(ctx, mac); err == nil && device != nil {
					if device.DisplayName != "" {
						hostname = device.DisplayName
					} else {
						hostname = device.Hostname
					}
				}
			}
			go n.NotifyWiFiClientDisconnected(ctx, mac, hostname)
		}
	}

	n.connectedClients = currentClients
}
