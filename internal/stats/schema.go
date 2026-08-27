package stats

// SchemaSQL contains the SQLite schema for time-series stats persistence
const SchemaSQL = `
-- Minute-level aggregates (1 day retention = 1440 rows per interface)
CREATE TABLE IF NOT EXISTS stats_minute (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    interface_name TEXT NOT NULL,
    timestamp INTEGER NOT NULL,

    rx_bytes INTEGER NOT NULL DEFAULT 0,
    tx_bytes INTEGER NOT NULL DEFAULT 0,
    rx_packets INTEGER NOT NULL DEFAULT 0,
    tx_packets INTEGER NOT NULL DEFAULT 0,

    avg_rx_bytes_per_sec REAL NOT NULL DEFAULT 0,
    avg_tx_bytes_per_sec REAL NOT NULL DEFAULT 0,

    avg_latency_ms REAL NOT NULL DEFAULT 0,
    min_latency_ms REAL NOT NULL DEFAULT 0,
    max_latency_ms REAL NOT NULL DEFAULT 0,

    sample_count INTEGER NOT NULL DEFAULT 0,

    UNIQUE(interface_name, timestamp)
);
CREATE INDEX IF NOT EXISTS idx_stats_minute_iface_ts ON stats_minute(interface_name, timestamp);

-- Hourly aggregates (1 week retention = 168 rows per interface)
CREATE TABLE IF NOT EXISTS stats_hour (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    interface_name TEXT NOT NULL,
    timestamp INTEGER NOT NULL,

    rx_bytes INTEGER NOT NULL DEFAULT 0,
    tx_bytes INTEGER NOT NULL DEFAULT 0,
    rx_packets INTEGER NOT NULL DEFAULT 0,
    tx_packets INTEGER NOT NULL DEFAULT 0,

    avg_rx_bytes_per_sec REAL NOT NULL DEFAULT 0,
    avg_tx_bytes_per_sec REAL NOT NULL DEFAULT 0,

    avg_latency_ms REAL NOT NULL DEFAULT 0,
    min_latency_ms REAL NOT NULL DEFAULT 0,
    max_latency_ms REAL NOT NULL DEFAULT 0,

    sample_count INTEGER NOT NULL DEFAULT 0,

    UNIQUE(interface_name, timestamp)
);
CREATE INDEX IF NOT EXISTS idx_stats_hour_iface_ts ON stats_hour(interface_name, timestamp);

-- Daily aggregates (1 month retention = 30 rows per interface)
CREATE TABLE IF NOT EXISTS stats_day (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    interface_name TEXT NOT NULL,
    timestamp INTEGER NOT NULL,

    rx_bytes INTEGER NOT NULL DEFAULT 0,
    tx_bytes INTEGER NOT NULL DEFAULT 0,
    rx_packets INTEGER NOT NULL DEFAULT 0,
    tx_packets INTEGER NOT NULL DEFAULT 0,

    avg_rx_bytes_per_sec REAL NOT NULL DEFAULT 0,
    avg_tx_bytes_per_sec REAL NOT NULL DEFAULT 0,

    avg_latency_ms REAL NOT NULL DEFAULT 0,
    min_latency_ms REAL NOT NULL DEFAULT 0,
    max_latency_ms REAL NOT NULL DEFAULT 0,

    sample_count INTEGER NOT NULL DEFAULT 0,

    UNIQUE(interface_name, timestamp)
);
CREATE INDEX IF NOT EXISTS idx_stats_day_iface_ts ON stats_day(interface_name, timestamp);

-- Monthly aggregates (1 year retention = 12 rows per interface)
CREATE TABLE IF NOT EXISTS stats_month (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    interface_name TEXT NOT NULL,
    timestamp INTEGER NOT NULL,

    rx_bytes INTEGER NOT NULL DEFAULT 0,
    tx_bytes INTEGER NOT NULL DEFAULT 0,
    rx_packets INTEGER NOT NULL DEFAULT 0,
    tx_packets INTEGER NOT NULL DEFAULT 0,

    avg_rx_bytes_per_sec REAL NOT NULL DEFAULT 0,
    avg_tx_bytes_per_sec REAL NOT NULL DEFAULT 0,

    avg_latency_ms REAL NOT NULL DEFAULT 0,
    min_latency_ms REAL NOT NULL DEFAULT 0,
    max_latency_ms REAL NOT NULL DEFAULT 0,

    sample_count INTEGER NOT NULL DEFAULT 0,

    UNIQUE(interface_name, timestamp)
);
CREATE INDEX IF NOT EXISTS idx_stats_month_iface_ts ON stats_month(interface_name, timestamp);

-- Yearly aggregates (forever)
CREATE TABLE IF NOT EXISTS stats_year (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    interface_name TEXT NOT NULL,
    timestamp INTEGER NOT NULL,

    rx_bytes INTEGER NOT NULL DEFAULT 0,
    tx_bytes INTEGER NOT NULL DEFAULT 0,
    rx_packets INTEGER NOT NULL DEFAULT 0,
    tx_packets INTEGER NOT NULL DEFAULT 0,

    avg_rx_bytes_per_sec REAL NOT NULL DEFAULT 0,
    avg_tx_bytes_per_sec REAL NOT NULL DEFAULT 0,

    avg_latency_ms REAL NOT NULL DEFAULT 0,
    min_latency_ms REAL NOT NULL DEFAULT 0,
    max_latency_ms REAL NOT NULL DEFAULT 0,

    sample_count INTEGER NOT NULL DEFAULT 0,

    UNIQUE(interface_name, timestamp)
);
CREATE INDEX IF NOT EXISTS idx_stats_year_iface_ts ON stats_year(interface_name, timestamp);

-- Accumulator for current minute (before flush to stats_minute)
CREATE TABLE IF NOT EXISTS stats_accumulator (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    interface_name TEXT NOT NULL UNIQUE,
    minute_ts INTEGER NOT NULL,

    rx_bytes_delta INTEGER NOT NULL DEFAULT 0,
    tx_bytes_delta INTEGER NOT NULL DEFAULT 0,
    rx_packets_delta INTEGER NOT NULL DEFAULT 0,
    tx_packets_delta INTEGER NOT NULL DEFAULT 0,

    rx_rate_sum REAL NOT NULL DEFAULT 0,
    tx_rate_sum REAL NOT NULL DEFAULT 0,
    latency_sum REAL NOT NULL DEFAULT 0,
    latency_min REAL,
    latency_max REAL,

    sample_count INTEGER NOT NULL DEFAULT 0,

    last_rx_bytes INTEGER NOT NULL DEFAULT 0,
    last_tx_bytes INTEGER NOT NULL DEFAULT 0,
    last_rx_packets INTEGER NOT NULL DEFAULT 0,
    last_tx_packets INTEGER NOT NULL DEFAULT 0
);

-- WiFi Client Stats Tables --

-- WiFi client minute-level aggregates (1 day retention)
CREATE TABLE IF NOT EXISTS wifi_client_minute (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    mac TEXT NOT NULL,
    interface_name TEXT NOT NULL,
    timestamp INTEGER NOT NULL,

    rx_bytes INTEGER NOT NULL DEFAULT 0,
    tx_bytes INTEGER NOT NULL DEFAULT 0,

    avg_rx_bytes_per_sec REAL NOT NULL DEFAULT 0,
    avg_tx_bytes_per_sec REAL NOT NULL DEFAULT 0,

    avg_signal INTEGER NOT NULL DEFAULT 0,
    min_signal INTEGER,
    max_signal INTEGER,

    avg_rx_bitrate REAL NOT NULL DEFAULT 0,
    avg_tx_bitrate REAL NOT NULL DEFAULT 0,

    sample_count INTEGER NOT NULL DEFAULT 0,

    UNIQUE(mac, timestamp)
);
CREATE INDEX IF NOT EXISTS idx_wifi_client_minute_mac_ts ON wifi_client_minute(mac, timestamp);
CREATE INDEX IF NOT EXISTS idx_wifi_client_minute_ts ON wifi_client_minute(timestamp);

-- WiFi client hourly aggregates (1 week retention)
CREATE TABLE IF NOT EXISTS wifi_client_hour (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    mac TEXT NOT NULL,
    interface_name TEXT NOT NULL,
    timestamp INTEGER NOT NULL,

    rx_bytes INTEGER NOT NULL DEFAULT 0,
    tx_bytes INTEGER NOT NULL DEFAULT 0,

    avg_rx_bytes_per_sec REAL NOT NULL DEFAULT 0,
    avg_tx_bytes_per_sec REAL NOT NULL DEFAULT 0,

    avg_signal INTEGER NOT NULL DEFAULT 0,
    min_signal INTEGER,
    max_signal INTEGER,

    avg_rx_bitrate REAL NOT NULL DEFAULT 0,
    avg_tx_bitrate REAL NOT NULL DEFAULT 0,

    sample_count INTEGER NOT NULL DEFAULT 0,

    UNIQUE(mac, timestamp)
);
CREATE INDEX IF NOT EXISTS idx_wifi_client_hour_mac_ts ON wifi_client_hour(mac, timestamp);
CREATE INDEX IF NOT EXISTS idx_wifi_client_hour_ts ON wifi_client_hour(timestamp);

-- WiFi client daily aggregates (1 month retention)
CREATE TABLE IF NOT EXISTS wifi_client_day (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    mac TEXT NOT NULL,
    interface_name TEXT NOT NULL,
    timestamp INTEGER NOT NULL,

    rx_bytes INTEGER NOT NULL DEFAULT 0,
    tx_bytes INTEGER NOT NULL DEFAULT 0,

    avg_rx_bytes_per_sec REAL NOT NULL DEFAULT 0,
    avg_tx_bytes_per_sec REAL NOT NULL DEFAULT 0,

    avg_signal INTEGER NOT NULL DEFAULT 0,
    min_signal INTEGER,
    max_signal INTEGER,

    avg_rx_bitrate REAL NOT NULL DEFAULT 0,
    avg_tx_bitrate REAL NOT NULL DEFAULT 0,

    sample_count INTEGER NOT NULL DEFAULT 0,

    UNIQUE(mac, timestamp)
);
CREATE INDEX IF NOT EXISTS idx_wifi_client_day_mac_ts ON wifi_client_day(mac, timestamp);
CREATE INDEX IF NOT EXISTS idx_wifi_client_day_ts ON wifi_client_day(timestamp);

-- WiFi client monthly aggregates (1 year retention)
CREATE TABLE IF NOT EXISTS wifi_client_month (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    mac TEXT NOT NULL,
    interface_name TEXT NOT NULL,
    timestamp INTEGER NOT NULL,

    rx_bytes INTEGER NOT NULL DEFAULT 0,
    tx_bytes INTEGER NOT NULL DEFAULT 0,

    avg_rx_bytes_per_sec REAL NOT NULL DEFAULT 0,
    avg_tx_bytes_per_sec REAL NOT NULL DEFAULT 0,

    avg_signal INTEGER NOT NULL DEFAULT 0,
    min_signal INTEGER,
    max_signal INTEGER,

    avg_rx_bitrate REAL NOT NULL DEFAULT 0,
    avg_tx_bitrate REAL NOT NULL DEFAULT 0,

    sample_count INTEGER NOT NULL DEFAULT 0,

    UNIQUE(mac, timestamp)
);
CREATE INDEX IF NOT EXISTS idx_wifi_client_month_mac_ts ON wifi_client_month(mac, timestamp);
CREATE INDEX IF NOT EXISTS idx_wifi_client_month_ts ON wifi_client_month(timestamp);

-- WiFi client yearly aggregates (forever)
CREATE TABLE IF NOT EXISTS wifi_client_year (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    mac TEXT NOT NULL,
    interface_name TEXT NOT NULL,
    timestamp INTEGER NOT NULL,

    rx_bytes INTEGER NOT NULL DEFAULT 0,
    tx_bytes INTEGER NOT NULL DEFAULT 0,

    avg_rx_bytes_per_sec REAL NOT NULL DEFAULT 0,
    avg_tx_bytes_per_sec REAL NOT NULL DEFAULT 0,

    avg_signal INTEGER NOT NULL DEFAULT 0,
    min_signal INTEGER,
    max_signal INTEGER,

    avg_rx_bitrate REAL NOT NULL DEFAULT 0,
    avg_tx_bitrate REAL NOT NULL DEFAULT 0,

    sample_count INTEGER NOT NULL DEFAULT 0,

    UNIQUE(mac, timestamp)
);
CREATE INDEX IF NOT EXISTS idx_wifi_client_year_mac_ts ON wifi_client_year(mac, timestamp);
CREATE INDEX IF NOT EXISTS idx_wifi_client_year_ts ON wifi_client_year(timestamp);

-- WiFi client accumulator for current minute
CREATE TABLE IF NOT EXISTS wifi_client_accumulator (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    mac TEXT NOT NULL UNIQUE,
    interface_name TEXT NOT NULL,
    minute_ts INTEGER NOT NULL,

    rx_bytes_delta INTEGER NOT NULL DEFAULT 0,
    tx_bytes_delta INTEGER NOT NULL DEFAULT 0,

    rx_rate_sum REAL NOT NULL DEFAULT 0,
    tx_rate_sum REAL NOT NULL DEFAULT 0,

    signal_sum INTEGER NOT NULL DEFAULT 0,
    signal_min INTEGER,
    signal_max INTEGER,

    rx_bitrate_sum REAL NOT NULL DEFAULT 0,
    tx_bitrate_sum REAL NOT NULL DEFAULT 0,

    sample_count INTEGER NOT NULL DEFAULT 0,

    last_rx_bytes INTEGER NOT NULL DEFAULT 0,
    last_tx_bytes INTEGER NOT NULL DEFAULT 0
);

-- Devices table for MAC to hostname mapping
CREATE TABLE IF NOT EXISTS devices (
    mac TEXT PRIMARY KEY NOT NULL,
    hostname TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    vendor TEXT NOT NULL DEFAULT '',
    first_seen INTEGER NOT NULL DEFAULT 0,
    last_seen INTEGER NOT NULL DEFAULT 0,
    last_ip TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_devices_hostname ON devices(hostname);
CREATE INDEX IF NOT EXISTS idx_devices_last_seen ON devices(last_seen);

-- Notification events log
CREATE TABLE IF NOT EXISTS notification_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp INTEGER NOT NULL,
    event_type TEXT NOT NULL,
    title TEXT NOT NULL,
    message TEXT NOT NULL,
    priority INTEGER NOT NULL DEFAULT 3,
    sent INTEGER NOT NULL DEFAULT 0,
    error TEXT
);
CREATE INDEX IF NOT EXISTS idx_notification_events_ts ON notification_events(timestamp);
CREATE INDEX IF NOT EXISTS idx_notification_events_type ON notification_events(event_type);

-- DHCP Reservations (static leases)
CREATE TABLE IF NOT EXISTS dhcp_reservations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    mac TEXT NOT NULL UNIQUE,
    ip TEXT NOT NULL,
    created_at INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_dhcp_reservations_mac ON dhcp_reservations(mac);
CREATE INDEX IF NOT EXISTS idx_dhcp_reservations_ip ON dhcp_reservations(ip);

-- DNS Entries (local hostname overrides)
CREATE TABLE IF NOT EXISTS dns_entries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    hostname TEXT NOT NULL,
    ip TEXT NOT NULL,
    created_at INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL DEFAULT 0,
    UNIQUE(hostname, ip)
);
CREATE INDEX IF NOT EXISTS idx_dns_entries_hostname ON dns_entries(hostname);

-- Port Forwards (firewall rules)
CREATE TABLE IF NOT EXISTS port_forwards (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    protocol TEXT NOT NULL DEFAULT 'tcp',
    external_port INTEGER NOT NULL,
    internal_ip TEXT NOT NULL,
    internal_port INTEGER NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_port_forwards_enabled ON port_forwards(enabled);

-- Schema version for future migrations
CREATE TABLE IF NOT EXISTS stats_schema_version (
    version INTEGER PRIMARY KEY
);
INSERT OR IGNORE INTO stats_schema_version (version) VALUES (4);
`
