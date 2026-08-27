package stats

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Resolution represents the granularity of data points
type Resolution string

const (
	ResolutionMinute Resolution = "minute"
	ResolutionHour   Resolution = "hour"
	ResolutionDay    Resolution = "day"
	ResolutionMonth  Resolution = "month"
	ResolutionYear   Resolution = "year"
)

// Retention periods
const (
	MinuteRetention = 24 * time.Hour        // 1 day
	HourRetention   = 7 * 24 * time.Hour    // 1 week
	DayRetention    = 30 * 24 * time.Hour   // 1 month
	MonthRetention  = 365 * 24 * time.Hour  // 1 year
	// Year data kept forever
)

// AggregatedStats represents a single aggregated data point
type AggregatedStats struct {
	InterfaceName string     `json:"interface_name"`
	Timestamp     time.Time  `json:"timestamp"`
	Resolution    Resolution `json:"resolution"`

	// Traffic totals (sum over period)
	// Using int64 since SQLite stores signed integers and counters can wrap/reset
	RxBytes   int64 `json:"rx_bytes"`
	TxBytes   int64 `json:"tx_bytes"`
	RxPackets int64 `json:"rx_packets"`
	TxPackets int64 `json:"tx_packets"`

	// Rate averages (average over period)
	AvgRxBytesPerSec float64 `json:"avg_rx_bytes_per_sec"`
	AvgTxBytesPerSec float64 `json:"avg_tx_bytes_per_sec"`

	// Latency stats
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	MinLatencyMs float64 `json:"min_latency_ms"`
	MaxLatencyMs float64 `json:"max_latency_ms"`

	// Number of samples that contributed to this aggregate
	SampleCount int `json:"sample_count"`
}

// HistoryQuery represents a query for historical data
type HistoryQuery struct {
	InterfaceName string     `json:"interface_name"`
	Resolution    Resolution `json:"resolution"`
	Start         time.Time  `json:"start"`
	End           time.Time  `json:"end"`
}

// HistoryResponse represents historical data response
type HistoryResponse struct {
	InterfaceName string            `json:"interface_name"`
	Resolution    Resolution        `json:"resolution"`
	DataPoints    []AggregatedStats `json:"data_points"`
}

// WiFiClientAggregatedStats represents aggregated WiFi client data
type WiFiClientAggregatedStats struct {
	MAC           string     `json:"mac"`
	InterfaceName string     `json:"interface_name"`
	Timestamp     time.Time  `json:"timestamp"`
	Resolution    Resolution `json:"resolution"`

	// Traffic totals (int64 for SQLite compatibility)
	RxBytes int64 `json:"rx_bytes"`
	TxBytes int64 `json:"tx_bytes"`

	// Rate averages
	AvgRxBytesPerSec float64 `json:"avg_rx_bytes_per_sec"`
	AvgTxBytesPerSec float64 `json:"avg_tx_bytes_per_sec"`

	// Signal stats
	AvgSignal int `json:"avg_signal"`
	MinSignal int `json:"min_signal"`
	MaxSignal int `json:"max_signal"`

	// Bitrate averages
	AvgRxBitrate float64 `json:"avg_rx_bitrate"`
	AvgTxBitrate float64 `json:"avg_tx_bitrate"`

	SampleCount int `json:"sample_count"`
}

// WiFiClientHistoryQuery represents a query for WiFi client history
type WiFiClientHistoryQuery struct {
	MAC        string     `json:"mac"`         // Empty for all clients
	Resolution Resolution `json:"resolution"`
	Start      time.Time  `json:"start"`
	End        time.Time  `json:"end"`
}

// WiFiClientHistoryResponse represents WiFi client historical data
type WiFiClientHistoryResponse struct {
	Resolution Resolution                  `json:"resolution"`
	Clients    []WiFiClientAggregatedStats `json:"clients"`
}

// Device represents a network device with hostname mapping
type Device struct {
	MAC         string `json:"mac"`
	Hostname    string `json:"hostname"`
	DisplayName string `json:"display_name"`
	Vendor      string `json:"vendor"`
	FirstSeen   int64  `json:"first_seen"`
	LastSeen    int64  `json:"last_seen"`
	LastIP      string `json:"last_ip"`
	Notes       string `json:"notes"`
}

// NotificationEvent represents a logged notification
type NotificationEvent struct {
	ID        int64  `json:"id"`
	Timestamp int64  `json:"timestamp"`
	EventType string `json:"event_type"`
	Title     string `json:"title"`
	Message   string `json:"message"`
	Priority  int    `json:"priority"`
	Sent      bool   `json:"sent"`
	Error     string `json:"error,omitempty"`
}

// DHCPReservation represents a static DHCP lease
type DHCPReservation struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	MAC       string `json:"mac"`
	IP        string `json:"ip"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// DNSEntry represents a local DNS override
type DNSEntry struct {
	ID        int64  `json:"id"`
	Hostname  string `json:"hostname"`
	IP        string `json:"ip"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// PortForward represents a firewall port forward rule
type PortForward struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Protocol     string `json:"protocol"`
	ExternalPort int    `json:"external_port"`
	InternalIP   string `json:"internal_ip"`
	InternalPort int    `json:"internal_port"`
	Enabled      bool   `json:"enabled"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
}

// Store defines the interface for stats persistence
type Store interface {
	// Open opens/creates the database
	Open(ctx context.Context) error

	// Close closes the database connection
	Close() error

	// RecordSample stores a raw sample for aggregation
	RecordSample(ctx context.Context, rates InterfaceRates, latency *LatencyStats) error

	// RecordWiFiClientSample stores a WiFi client sample for aggregation
	RecordWiFiClientSample(ctx context.Context, rates WiFiClientRates) error

	// Query retrieves historical data
	Query(ctx context.Context, query HistoryQuery) (*HistoryResponse, error)

	// QueryWiFiClients retrieves WiFi client historical data
	QueryWiFiClients(ctx context.Context, query WiFiClientHistoryQuery) (*WiFiClientHistoryResponse, error)

	// Rollup triggers aggregation from finer to coarser resolutions
	Rollup(ctx context.Context) error

	// Prune removes old data beyond retention periods
	Prune(ctx context.Context) error

	// Device management
	GetDevice(ctx context.Context, mac string) (*Device, error)
	GetAllDevices(ctx context.Context) ([]Device, error)
	UpsertDevice(ctx context.Context, device Device) error
	DeleteDevice(ctx context.Context, mac string) error
	UpdateDeviceSeen(ctx context.Context, mac, ip, hostname string) error

	// Notification events
	LogNotificationEvent(ctx context.Context, event NotificationEvent) error
	GetNotificationEvents(ctx context.Context, limit int) ([]NotificationEvent, error)

	// DHCP Reservations
	GetDHCPReservations(ctx context.Context) ([]DHCPReservation, error)
	GetDHCPReservationByMAC(ctx context.Context, mac string) (*DHCPReservation, error)
	AddDHCPReservation(ctx context.Context, res DHCPReservation) error
	DeleteDHCPReservation(ctx context.Context, mac string) error

	// DNS Entries
	GetDNSEntries(ctx context.Context) ([]DNSEntry, error)
	AddDNSEntry(ctx context.Context, entry DNSEntry) error
	DeleteDNSEntry(ctx context.Context, hostname string) error

	// Port Forwards
	GetPortForwards(ctx context.Context) ([]PortForward, error)
	AddPortForward(ctx context.Context, pf PortForward) error
	UpdatePortForward(ctx context.Context, pf PortForward) error
	DeletePortForward(ctx context.Context, name string) error

	// Migration helpers
	MigrateDHCPReservations(ctx context.Context, reservations []DHCPReservation) error
	MigrateDNSEntries(ctx context.Context, entries []DNSEntry) error
	MigratePortForwards(ctx context.Context, forwards []PortForward) error
}

// SQLiteStore implements Store using SQLite
type SQLiteStore struct {
	mu     sync.RWMutex
	db     *sql.DB
	dbPath string
	dryRun bool

	// Last rollup timestamps to avoid duplicate work
	lastMinuteRollup time.Time
	lastHourRollup   time.Time
	lastDayRollup    time.Time
	lastMonthRollup  time.Time
}

// NewSQLiteStore creates a new SQLite-based stats store
func NewSQLiteStore(configDir string, dryRun bool) *SQLiteStore {
	dbPath := filepath.Join(configDir, "stats.db")
	return &SQLiteStore{
		dbPath: dbPath,
		dryRun: dryRun,
	}
}

// Open opens or creates the database and applies the schema
func (s *SQLiteStore) Open(ctx context.Context) error {
	var err error

	if s.dryRun {
		// In dry-run mode, use in-memory database
		s.db, err = sql.Open("sqlite", ":memory:")
		if err != nil {
			return fmt.Errorf("failed to open in-memory database: %w", err)
		}
	} else {
		// Ensure directory exists
		dir := filepath.Dir(s.dbPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create stats directory: %w", err)
		}

		s.db, err = sql.Open("sqlite", s.dbPath)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
	}

	// Apply schema
	if _, err := s.db.ExecContext(ctx, SchemaSQL); err != nil {
		return fmt.Errorf("failed to apply schema: %w", err)
	}

	// Enable WAL mode for better concurrent performance
	if _, err := s.db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		return fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	return nil
}

// Close closes the database connection
func (s *SQLiteStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// DBPath returns the path to the database file
func (s *SQLiteStore) DBPath() string {
	return s.dbPath
}

// Helper functions

func truncateToMinute(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, t.Location())
}

func truncateToHour(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
}

func truncateToDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func truncateToMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
}

func truncateToYear(t time.Time) time.Time {
	return time.Date(t.Year(), 1, 1, 0, 0, 0, 0, t.Location())
}

func getLatencyMs(l *LatencyStats) float64 {
	if l == nil {
		return 0
	}
	return l.LatencyMs
}

func getLatencyMin(l *LatencyStats) float64 {
	if l == nil {
		return 0
	}
	return l.MinLatencyMs
}

func getLatencyMax(l *LatencyStats) float64 {
	if l == nil {
		return 0
	}
	return l.MaxLatencyMs
}

func nullToZero(n sql.NullFloat64) float64 {
	if n.Valid {
		return n.Float64
	}
	return 0
}

func resolutionToTable(r Resolution) string {
	switch r {
	case ResolutionMinute:
		return "stats_minute"
	case ResolutionHour:
		return "stats_hour"
	case ResolutionDay:
		return "stats_day"
	case ResolutionMonth:
		return "stats_month"
	case ResolutionYear:
		return "stats_year"
	default:
		return "stats_minute"
	}
}

func wifiClientResolutionToTable(r Resolution) string {
	switch r {
	case ResolutionMinute:
		return "wifi_client_minute"
	case ResolutionHour:
		return "wifi_client_hour"
	case ResolutionDay:
		return "wifi_client_day"
	case ResolutionMonth:
		return "wifi_client_month"
	case ResolutionYear:
		return "wifi_client_year"
	default:
		return "wifi_client_minute"
	}
}

func nullIntToZero(n sql.NullInt64) int {
	if n.Valid {
		return int(n.Int64)
	}
	return 0
}
