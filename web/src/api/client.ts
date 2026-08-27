// API client for Ubuntu Router
// Uses JSON-RPC style HTTP calls

const API_BASE = '/api';

// Types based on protobuf definitions
export interface NetworkInterface {
  name: string;
  type: string;
  mac: string;
  mtu: number;
  state: string;
  linkState: string;
  addresses: Address[];
  master?: string;
  slaves?: string[];
  driver?: string;
  speed?: number;
  duplex?: string;
}

export interface Address {
  ip: string;
  prefix: number;
  family: string;
  scope: string;
}

export interface Route {
  destination: string;
  gateway?: string;
  interface: string;
  metric?: number;
  protocol?: string;
  scope?: string;
}

export interface RouteLookupResult {
  destination: string;
  interface: string;
  gateway?: string;
  source?: string;
  raw_output: string;
}

export interface DNSStatus {
  running: boolean;
  configValid: boolean;
  leaseCount: number;
  dnsEntries: number;
  upstreamDns: string[];
}

export interface DNSEntry {
  hostname: string;
  ip: string;
}

export interface DHCPLease {
  mac: string;
  ip: string;
  hostname: string;
  expires: number;
}

export interface DHCPReservation {
  name: string;
  mac: string;
  ip: string;
}

export interface DHCPSettings {
  enabled: boolean;
  start: string;
  end: string;
  lease: string;
  gateway: string;
  lan_bridge: string;
}

export interface DHCPSettingsUpdate {
  enabled: boolean;
  start: string;
  end: string;
  lease: string;
  gateway: string;
}

export interface WireGuardStatus {
  enabled: boolean;
  running: boolean;
  interface: string;
  publicKey: string;
  listenPort: number;
  address: string;
  peerCount: number;
  activePeers: number;
  totalRx: number;
  totalTx: number;
}

export interface WireGuardPeer {
  name: string;
  publicKey: string;
  allowedIps: string[];
  endpoint?: string;
  transferRx?: number;
  transferTx?: number;
  latestHandshake?: number;
}

export interface WireGuardSettings {
  enabled: boolean;
  interface: string;
  listen_port: number;
  address: string;
  dns: string;
  public_key: string;
  config_exists: boolean;
  config_path: string;
  route_all_traffic: boolean;
  route_lan: boolean;
  lan_subnets: string[];
  vpn_server_ip: string;
  upstream_dns: string[];
  available_subnets: SubnetInfo[];
}

export interface SubnetInfo {
  subnet: string;
  description: string;
  source: string;
}

export interface WiFiInterfaceStatus {
  interface: string;
  mode: string;
  ssid: string;
  channel: number;
  frequency: number;
  txPower: number;
  running: boolean;
  stationCount: number;
  stations?: WiFiStation[];
}

export interface WiFiStation {
  mac: string;
  signal: number;
  rxRate: number;
  txRate: number;
  rxBytes: number;
  txBytes: number;
  connected: string;
}

export interface WiFiInterfaceConfig {
  interface: string;
  enabled: boolean;
  ssid: string;
  password: string;
  channel: number;
  auto_channel: boolean;
  band: string;
  mode: string;
  hidden: boolean;
  bridge: string;
}

export interface PortForward {
  name: string;
  enabled: boolean;
  protocol: string;
  externalPort: number;
  internalIp: string;
  internalPort: number;
}

export interface RouterStatus {
  version: string;
  ipForwardEnabled: boolean;
  wanInterface: string;
  defaultInterface: string;
  dnsStatus?: DNSStatus;
  wireguardStatus?: WireGuardStatus;
}

export interface RouterConfig {
  listenAddr: string;
  webListenAddresses?: string[];
  wanInterface: string;
  wanMode: string;
  wanStaticIp?: string;
  wanStaticGateway?: string;
  wanStaticDns?: string;
  lanBridge: string;
  lanAddresses: string[];
  lanPorts: string[];
  dhcpEnabled: boolean;
  dhcpStart: string;
  dhcpEnd: string;
  dhcpLease: string;
  dnsEnabled: boolean;
  dnsUpstream: string[];
  dnsLocalZone: string;
  firewallEnabled: boolean;
}

// API Response types
interface ListInterfacesResponse {
  interfaces: NetworkInterface[];
}

interface ListRoutesResponse {
  routes: Route[];
}

interface ListDNSEntriesResponse {
  entries: DNSEntry[];
}

interface ListLeasesResponse {
  leases: DHCPLease[];
}

interface ListReservationsResponse {
  reservations: DHCPReservation[];
}

interface ListPeersResponse {
  peers: WireGuardPeer[];
}

interface GetWiFiStatusResponse {
  interfaces: WiFiInterfaceStatus[];
}

interface ListStationsResponse {
  stations: WiFiStation[];
}

interface ListPortForwardsResponse {
  portForwards: PortForward[];
}

interface WANStatusResponse {
  configured: boolean;
  interface: {
    name: string;
    state: string;
    addresses: string[];
    gateway: string;
  } | null;
  config: {
    interface: string;
    mode: string;
    staticIP: string;
    staticGateway: string;
    staticDNS: string;
    wifiSSID?: string;
    wifiSecurity?: string;
  };
  wifiStatus?: WiFiClientStatus;
}

interface WANDetectResponse {
  interface: string;
  mode: string;
  currentIP: string;
  currentGateway: string;
  hasAddress: boolean;
}

interface WANConfigureRequest {
  interface: string;
  mode: 'dhcp' | 'static' | 'pppoe' | 'wifi';
  static_ip?: string;
  static_gateway?: string;
  static_dns?: string;
  wifi_ssid?: string;
  wifi_password?: string;
  wifi_security?: string;
}

// WiFi WAN types
export interface ScannedWiFiNetwork {
  ssid: string;
  bssid: string;
  signal: number;
  quality: number;
  frequency: number;
  channel: number;
  security: 'open' | 'wep' | 'wpa' | 'wpa2' | 'wpa3';
  band: '2.4ghz' | '5ghz';
  connected: boolean;
}

export interface WiFiClientStatus {
  connected: boolean;
  ssid?: string;
  bssid?: string;
  signal?: number;
  frequency?: number;
  ip_address?: string;
  gateway?: string;
}

interface WiFiScanResponse {
  interface: string;
  networks: ScannedWiFiNetwork[];
}

interface WiFiWANStatusResponse {
  interface: string;
  status: WiFiClientStatus;
  config: {
    ssid: string;
    security: string;
  };
}

interface WiFiConnectRequest {
  interface?: string;
  ssid: string;
  password?: string;
  security?: 'open' | 'wep' | 'wpa' | 'wpa2' | 'wpa3';
}

// Multi-WAN types
export interface WANConfigItem {
  id?: string;
  name: string;
  interface: string;
  enabled: boolean;
  priority: number;
  mode?: 'dhcp' | 'static' | 'wifi';
  static_ip?: string;
  static_gateway?: string;
  static_dns?: string;
  wifi_ssid?: string;
  wifi_password?: string;
  wifi_security?: string;
  fuse_with_next?: boolean; // Load balancing (WIP)
  health_check_interval?: number;
  health_check_timeout?: number;
  health_check_retries?: number;
  health_check_targets?: string[];
}

export interface InterfaceStats {
  rx_bytes: number;
  tx_bytes: number;
  rx_packets: number;
  tx_packets: number;
}

export interface WANsResponse {
  wans: WANConfigItem[];
  failback_delay: number;
  status: MultiWANStatus;
  interface_stats: Record<string, InterfaceStats>;
  interface_routes: Record<string, string[]>;
  interface_dns: Record<string, string[]>;
}

export interface MultiWANConfig {
  enabled: boolean;
  mode: 'failover' | 'loadbalance';
  wans: WANConfigItem[];
  failback_delay: number;
}

export interface WANStatusItem {
  name: string;
  interface: string;
  priority: number;
  enabled: boolean;
  active: boolean;
  up: boolean;
  healthy: boolean;
  ip_address: string;
  gateway: string;
  last_check: string;
  last_healthy: string;
  failure_count: number;
  checks_run: number;
  checks_passed: number;
}

export interface MultiWANStatus {
  enabled: boolean;
  mode: string;
  active_wan: string;
  wans: WANStatusItem[];
  last_switch: string;
  switch_count: number;
}

export interface SystemDependency {
  name: string;
  description: string;
  required: boolean;
  installed: boolean;
  version?: string;
  install_cmd?: string;
}

interface SystemDependenciesResponse {
  dependencies: SystemDependency[];
  summary: {
    installed: number;
    missing: number;
    optional: number;
    ready: boolean;
  };
}

export interface LogEntry {
  time: string;
  level: string;
  message: string;
}

interface SystemLogsResponse {
  entries: LogEntry[];
}

// Health Check types
export interface HealthIssue {
  severity: 'info' | 'warning' | 'critical';
  message: string;
  details?: string;
  fixer_id?: string;
  fixer?: string;
}

export interface HealthCheckResult {
  check_id: string;
  name: string;
  description: string;
  state: 'ok' | 'warning' | 'error' | 'unknown' | 'skipped';
  message: string;
  issues?: HealthIssue[];
  sub_checks?: HealthCheckResult[];
}

interface HealthCheckResponse {
  results: HealthCheckResult[];
  summary: {
    total: number;
    ok: number;
    warnings: number;
    errors: number;
    skipped: number;
    ready: boolean;
  };
}

// External DNS types
export type DNSProviderType =
  | 'route53'
  | 'cloudflare'
  | 'digitalocean'
  | 'hetzner'
  | 'gandi'
  | 'googlecloud'
  | 'duckdns'
  | 'namecom';

export interface DNSProviderConfig {
  type: DNSProviderType;
  api_token?: string;
  aws_access_key_id?: string;
  aws_secret_access_key?: string;
  aws_region?: string;
  aws_hosted_zone_id?: string;
  aws_profile?: string;
  cloudflare_api_token?: string;
  cloudflare_zone_id?: string;
  namecom_username?: string;
  namecom_api_token?: string;
  gcp_project?: string;
  gcp_service_account_json?: string;
}

export interface ExternalDNSZone {
  id: string;
  name: string;
  provider: DNSProviderConfig;
  ssl_enabled: boolean;
  ssl_email?: string;
  ssl_domains?: string[];
  cert_path?: string;
  key_path?: string;
  last_synced?: string;
  last_ssl_renew?: string;
}

export interface ExternalDNSRecord {
  id: string;
  name: string;
  type: 'A' | 'AAAA' | 'CNAME' | 'TXT';
  value: string;
  ttl: number;
  zone_id: string;
  auto_sync: boolean;
}

export interface ExternalDNSStatus {
  enabled: boolean;
  public_ip: string;
  auto_sync_ip: boolean;
  sync_interval: number;
  zones: {
    id: string;
    name: string;
    provider: string;
    ssl_enabled: boolean;
    ssl_email: string;
    last_synced: string;
  }[];
  records: {
    id: string;
    name: string;
    type: string;
    value: string;
    ttl: number;
    zone_id: string;
    auto_sync: boolean;
  }[];
}

export interface ExternalDNSSettings {
  enabled: boolean;
  auto_sync_ip: boolean;
  sync_interval: number;
  public_ip: string;
  cert_dir: string;
}

interface DiscoveredZone {
  id: string;
  name: string;
  dns_managed: boolean;
}

interface SyncResult {
  record_id: string;
  name: string;
  value?: string;
  changed?: boolean;
  success?: boolean;
  error?: string;
}

interface SSLStatus {
  zone_id: string;
  zone_name: string;
  cert_exists: boolean;
  cert_path: string;
  key_path: string;
  expiry_info?: string;
  cert_info?: {
    domain: string;
    sans: string[];
    issuer: string;
    not_before: string;
    not_after: string;
    serial: string;
  };
}

export interface WiFiCard {
  interface: string;
  phy: string;
  driver: string;
  mac: string;
  supports_ap: boolean;
  supports_mesh: boolean;
  supports_ibss: boolean;
  supports_monitor: boolean;
  bands: string[];
  channels_2ghz: number[];
  channels_5ghz: number[];
  ht_capable: boolean;
  vht_capable: boolean;
  he_capable: boolean;
  current_mode?: string;
  in_use: boolean;
}

interface WiFiCardsResponse {
  cards: WiFiCard[];
}

interface AddPeerResponse {
  success: boolean;
  message: string;
  clientConfig: string;
  qrCode?: string;
}

// WireGuard P2P types
export interface WireGuardP2PTunnel {
  id: string;
  name: string;
  enabled: boolean;
  interface: string;
  listen_port: number;
  private_key?: string;
  address: string;
  remote_public_key: string;
  remote_endpoint: string;
  preshared_key?: string;
  local_subnets: string[];
  remote_subnets: string[];
  forced_subnets: string[];  // Subnets that bypass conflict filter
  persistent_keepalive: number;
}

export interface P2PStatus {
  tunnel_id: string;
  name: string;
  interface: string;
  running: boolean;
  enabled: boolean;
  public_key: string;
  listen_port: number;
  address: string;
  remote_endpoint: string;
  connected: boolean;
  latest_handshake?: string;
  transfer_rx: number;
  transfer_tx: number;
  local_subnets: string[];
  remote_subnets: string[];
}

export interface RemotePeerConfig {
  tunnel_name: string;
  local_public_key: string;
  local_endpoint: string;
  local_subnets: string[];
  preshared_key?: string;
  suggested_address: string;
  wireguard_config: string;
}

export interface P2PKeyPair {
  private_key: string;
  public_key: string;
}

export interface ParsedWireGuardConfig {
  private_key?: string;
  address?: string;
  listen_port?: number;
  remote_public_key?: string;
  remote_endpoint?: string;
  preshared_key?: string;
  allowed_ips?: string[];
  persistent_keepalive?: number;
}

// Subnet conflict detected during tunnel validation
export interface SubnetConflict {
  local_subnet: string;
  remote_subnet: string;
  description: string;
}

// Result of tunnel configuration validation
export interface P2PValidationResult {
  valid: boolean;
  conflicts: SubnetConflict[];
  warnings: string[];
  // Subnets that were filtered out due to conflicts
  filtered_subnets: string[];
  // Safe subnets that will be used in the WireGuard config
  safe_remote_subnets: string[];
}

interface P2PStatusResponse {
  tunnels: P2PStatus[];
}

interface P2PTunnelsResponse {
  tunnels: WireGuardP2PTunnel[];
}

interface CreateP2PTunnelResponse {
  success: boolean;
  message: string;
  tunnel: WireGuardP2PTunnel;
}

interface GetP2PTunnelResponse {
  tunnel: WireGuardP2PTunnel;
  status: P2PStatus;
}

interface ApiResponse {
  success: boolean;
  message: string;
}

// Rollback/Pending Change types
export interface PendingStatus {
  pending: boolean;
  expires_at?: string;
  seconds_left?: number;
  reason?: string;
  snapshot_id?: string;
  snapshot_time?: string;
}

export interface ChangeResponse {
  success: boolean;
  message?: string;
  error?: string;
  pending_confirmation: boolean;
  expires_at?: string;
  seconds_left?: number;
}

// Systemd Service types
export interface ServiceStatus {
  installed: boolean;
  enabled: boolean;
  running: boolean;
  needs_update: boolean;
  verify_ok: boolean;
  verify_output?: string;
  current_content?: string;
  expected_content: string;
  binary_path: string;
  config_path: string;
  working_directory: string;
}

// HAProxy/Services types
export interface ServiceDNS {
  ip: string;
  ttl?: number;
  auto_sync?: boolean;
}

export interface ServiceProxy {
  backend: string;
  health_check?: string;
  mode?: string;
}

export interface ServiceSSL {
  enabled: boolean;
  subdomains?: string[];
}

export interface Service {
  id: string;
  name: string;
  dns_name: string;
  zone_id: string;
  enabled: boolean;
  internal_dns?: ServiceDNS;
  external_dns?: ServiceDNS;
  proxy?: ServiceProxy;
  ssl?: ServiceSSL;
  created_at?: string;
  updated_at?: string;
}

export interface ServicesStatus {
  enabled: boolean;
  service_count: number;
  active_count: number;
  haproxy_ok: boolean;
  last_synced?: string;
}

export interface ServicesSettings {
  enabled: boolean;
}

export interface HAProxyStatus {
  running: boolean;
  enabled: boolean;
  installed: boolean;
  config_exists: boolean;
  config_valid: boolean;
  version?: string;
  error?: string;
  backend_count: number;
}

export interface HAProxyBackend {
  name: string;
  domain_match: string;
  server: string;
  http_check: boolean;
  check_path: string;
  mode: string;
  healthy: boolean;
  last_check: string;
  error?: string;
}

export interface ServiceSyncResult {
  service_id: string;
  service_name: string;
  success: boolean;
  message?: string;
  error?: string;
  internal_dns_changed?: boolean;
  external_dns_changed?: boolean;
  haproxy_changed?: boolean;
}

// Interface Statistics types
export interface InterfaceRates {
  name: string;
  timestamp: string;
  rx_bytes_per_sec: number;
  tx_bytes_per_sec: number;
  rx_packets_per_sec: number;
  tx_packets_per_sec: number;
  rx_avg_packet_size: number;
  tx_avg_packet_size: number;
  rx_errors_per_sec: number;
  tx_errors_per_sec: number;
  total_rx_bytes: number;
  total_tx_bytes: number;
  total_rx_packets: number;
  total_tx_packets: number;
}

export interface LatencyStats {
  target: string;
  timestamp: string;
  latency_ms: number;
  min_latency_ms: number;
  max_latency_ms: number;
  avg_latency_ms: number;
  packet_loss: number;
  sample_count: number;
  reachable: boolean;
}

export interface StatsSnapshot {
  timestamp: string;
  interfaces: Record<string, InterfaceRates>;
  latency?: LatencyStats;
}

export interface LatencyResponse {
  current: LatencyStats;
  history: LatencyStats[];
}

// WiFi Client Statistics types
export interface WiFiClientRates {
  mac: string;
  interface: string;
  timestamp: string;
  signal: number;
  rx_bitrate: number;
  tx_bitrate: number;
  rx_bytes_per_sec: number;
  tx_bytes_per_sec: number;
  total_rx_bytes: number;
  total_tx_bytes: number;
  connected_time: number;
  inactive: number;
  authorized: boolean;
}

export interface WiFiClientsResponse {
  clients: WiFiClientRates[];
}

// Stats History types
export type Resolution = 'minute' | 'hour' | 'day' | 'month' | 'year';

export interface AggregatedStats {
  interface_name: string;
  timestamp: string;
  resolution: Resolution;
  rx_bytes: number;
  tx_bytes: number;
  rx_packets: number;
  tx_packets: number;
  avg_rx_bytes_per_sec: number;
  avg_tx_bytes_per_sec: number;
  avg_latency_ms: number;
  min_latency_ms: number;
  max_latency_ms: number;
  sample_count: number;
}

export interface HistoryResponse {
  interface_name: string;
  resolution: Resolution;
  data_points: AggregatedStats[];
}

// WiFi Client History types
export interface WiFiClientAggregatedStats {
  mac: string;
  interface_name: string;
  timestamp: string;
  resolution: Resolution;
  rx_bytes: number;
  tx_bytes: number;
  avg_rx_bytes_per_sec: number;
  avg_tx_bytes_per_sec: number;
  avg_signal: number;
  min_signal: number;
  max_signal: number;
  avg_rx_bitrate: number;
  avg_tx_bitrate: number;
  sample_count: number;
}

export interface WiFiClientHistoryResponse {
  resolution: Resolution;
  clients: WiFiClientAggregatedStats[];
}

// QoS types
export interface QdiscStats {
  type: string;
  handle: string;
  parent: string;
  bandwidth?: string;
  bytes: number;
  packets: number;
  dropped: number;
  overlimits: number;
  requeues: number;
  backlog: number;
  target_delay?: string;
  peak_delay_us?: number;
  avg_delay_us?: number;
  base_delay_us?: number;
  memory_used?: number;
  memory_limit?: number;
}

export interface QoSStatus {
  enabled: boolean;
  mode: string;
  wan_interface: string;
  lan_interface: string;
  download_speed: string;
  upload_speed: string;
  wan_qdisc?: QdiscStats;
  lan_qdisc?: QdiscStats;
}

export interface QoSConfigRequest {
  enabled: boolean;
  mode: string;
  download_speed: string;
  upload_speed: string;
  per_host_fairness?: boolean;
  rtt_compensation?: string;
  wan_type?: string;
}

// Device/Hostname types
export interface Device {
  mac: string;
  hostname: string;
  display_name: string;
  vendor: string;
  first_seen: number;
  last_seen: number;
  last_ip: string;
  notes: string;
}

export interface DevicesResponse {
  devices: Device[];
}

export interface DeviceResponse {
  success: boolean;
  device: Device;
}

// Notification types
export interface NotificationSettings {
  enabled: boolean;
  server: string;
  topic: string;
  has_token: boolean;
  has_auth: boolean;
  notify_wifi_clients: boolean;
  notify_health_checks: boolean;
  notify_wan_changes: boolean;
  notify_vpn_peers: boolean;
  notify_system_events: boolean;
}

export interface NotificationSettingsUpdate {
  enabled: boolean;
  server: string;
  topic: string;
  token?: string;
  username?: string;
  password?: string;
  notify_wifi_clients: boolean;
  notify_health_checks: boolean;
  notify_wan_changes: boolean;
  notify_vpn_peers: boolean;
  notify_system_events: boolean;
}

export interface NotificationEvent {
  id: number;
  timestamp: number;
  event_type: string;
  title: string;
  message: string;
  priority: number;
  sent: boolean;
  error?: string;
}

export interface NotificationEventsResponse {
  events: NotificationEvent[];
}

// API client
async function apiCall<T>(endpoint: string, method: 'GET' | 'POST' | 'PUT' | 'DELETE' = 'GET', body?: unknown): Promise<T> {
  const options: RequestInit = {
    method,
    headers: {
      'Content-Type': 'application/json',
    },
    credentials: 'include',
  };

  if (body) {
    options.body = JSON.stringify(body);
  }

  const response = await fetch(`${API_BASE}${endpoint}`, options);

  if (!response.ok) {
    const errorText = await response.text();
    throw new Error(`API error: ${response.status} ${errorText}`);
  }

  return response.json();
}

// Auth types
export interface AuthStatus {
  authenticated: boolean;
}

export interface LoginResponse {
  success: boolean;
  error?: string;
}

// API methods - using object parameters for mutation methods
export const client = {
  // Auth
  getAuthStatus: () => apiCall<AuthStatus>('/auth/status'),
  login: (params: { password: string }) => apiCall<LoginResponse>('/auth/login', 'POST', params),
  logout: () => apiCall<LoginResponse>('/auth/logout', 'POST'),

  // Router
  getStatus: () => apiCall<RouterStatus>('/status'),
  getConfig: () => apiCall<RouterConfig>('/config'),
  updateConfig: (params: Partial<RouterConfig>) => apiCall<ApiResponse>('/config', 'POST', params),
  applyConfig: () => apiCall<ChangeResponse>('/apply', 'POST'),

  // Config Rollback
  getPendingStatus: () => apiCall<PendingStatus>('/config/pending'),
  confirmChanges: () => apiCall<ChangeResponse>('/config/confirm', 'POST'),
  cancelChanges: () => apiCall<ChangeResponse>('/config/cancel', 'POST'),

  // Network
  listInterfaces: () => apiCall<ListInterfacesResponse>('/interfaces'),
  setInterfaceState: (params: { name: string; up: boolean }) =>
    apiCall<ApiResponse>('/interfaces/state', 'POST', params),
  getRoutes: () => apiCall<ListRoutesResponse>('/routes'),
  lookupRoute: (params: { destination: string }) =>
    apiCall<RouteLookupResult>(`/routes?lookup=${encodeURIComponent(params.destination)}`),
  renewDHCP: (params: { interface: string }) => apiCall<ApiResponse>('/dhcp/renew', 'POST', params),

  // DNS
  getDNSStatus: () => apiCall<DNSStatus>('/dns/status'),
  listDNSEntries: () => apiCall<ListDNSEntriesResponse>('/dns/entries'),
  addDNSEntry: (params: { hostname: string; ip: string }) =>
    apiCall<ApiResponse>('/dns/entries', 'POST', params),
  removeDNSEntry: (params: { hostname: string }) =>
    apiCall<ApiResponse>('/dns/entries/delete', 'POST', params),
  getDNSSettings: () => apiCall<{ upstream_dns: string[]; dns_local_zone: string }>('/dns/settings'),
  updateDNSSettings: (params: { upstream_dns?: string[]; dns_local_zone?: string }) =>
    apiCall<ApiResponse>('/dns/settings', 'POST', params),

  // DHCP
  listLeases: () => apiCall<ListLeasesResponse>('/leases'),
  listReservations: () => apiCall<ListReservationsResponse>('/dhcp/reservations'),
  addReservation: (params: { name: string; mac: string; ip: string }) =>
    apiCall<ApiResponse>('/dhcp/reservations', 'POST', params),
  removeReservation: (params: { mac: string }) =>
    apiCall<ApiResponse>('/dhcp/reservations/delete', 'POST', params),
  getDHCPSettings: () => apiCall<DHCPSettings>('/dhcp/settings'),
  updateDHCPSettings: (params: DHCPSettingsUpdate) =>
    apiCall<ApiResponse>('/dhcp/settings', 'POST', params),

  // WireGuard
  getWireGuardStatus: () => apiCall<WireGuardStatus>('/wireguard/status'),
  getWireGuardSettings: () => apiCall<WireGuardSettings>('/wireguard/settings'),
  updateWireGuardSettings: (params: Partial<{
    enabled: boolean;
    address: string;
    listen_port: number;
    dns: string;
    route_all_traffic: boolean;
    route_lan: boolean;
    lan_subnets: string[];
  }>) => apiCall<ApiResponse>('/wireguard/settings', 'POST', params),
  listPeers: () => apiCall<ListPeersResponse>('/wireguard/peers'),
  addPeer: (params: { name: string; serverEndpoint: string }) =>
    apiCall<AddPeerResponse>('/wireguard/peers', 'POST', params),
  removePeer: (params: { publicKey: string }) =>
    apiCall<ApiResponse>('/wireguard/peers/delete', 'POST', params),
  wireGuardControl: (params: { action: 'start' | 'stop' | 'restart' | 'enable' | 'disable' }) =>
    apiCall<ApiResponse>('/wireguard/control', 'POST', params),

  // WireGuard P2P (Site-to-Site)
  getP2PStatus: () => apiCall<P2PStatusResponse>('/wireguard/p2p/status'),
  listP2PTunnels: () => apiCall<P2PTunnelsResponse>('/wireguard/p2p/tunnels'),
  createP2PTunnel: (params: Omit<WireGuardP2PTunnel, 'id' | 'interface' | 'listen_port' | 'private_key'>) =>
    apiCall<CreateP2PTunnelResponse>('/wireguard/p2p/tunnels', 'POST', params),
  getP2PTunnel: (params: { id: string }) =>
    apiCall<GetP2PTunnelResponse>(`/wireguard/p2p/tunnels/${params.id}`),
  updateP2PTunnel: (params: { id: string } & Partial<WireGuardP2PTunnel>) =>
    apiCall<ApiResponse>(`/wireguard/p2p/tunnels/${params.id}`, 'PUT', params),
  deleteP2PTunnel: (params: { id: string }) =>
    apiCall<ApiResponse>(`/wireguard/p2p/tunnels/${params.id}`, 'DELETE'),
  controlP2PTunnel: (params: { id: string; action: 'start' | 'stop' | 'restart' | 'enable' | 'disable' }) =>
    apiCall<ChangeResponse>(`/wireguard/p2p/tunnels/${params.id}/control`, 'POST', { action: params.action }),
  getP2PRemoteConfig: (params: { id: string; endpoint: string }) =>
    apiCall<RemotePeerConfig>(`/wireguard/p2p/tunnels/${params.id}/remote-config?endpoint=${encodeURIComponent(params.endpoint)}`),
  validateP2PTunnel: (params: { id: string }) =>
    apiCall<P2PValidationResult>(`/wireguard/p2p/tunnels/${params.id}/validate`),
  generateP2PKeys: () => apiCall<P2PKeyPair>('/wireguard/p2p/generate-keys', 'POST'),
  importP2PConfig: (params: { name: string; config: string }) =>
    apiCall<{ success: boolean; tunnel: WireGuardP2PTunnel; parsed: ParsedWireGuardConfig }>('/wireguard/p2p/import-config', 'POST', params),

  // WiFi
  getWiFiStatus: () => apiCall<GetWiFiStatusResponse>('/wifi/status'),
  listWiFiInterfaces: () => apiCall<{ interfaces: string[] }>('/wifi/interfaces'),
  configureAP: (params: WiFiInterfaceConfig) =>
    apiCall<ApiResponse>('/wifi/configure', 'POST', params),
  wifiControl: (params: { interface: string; action: 'start' | 'stop' | 'restart' }) =>
    apiCall<ApiResponse>('/wifi/control', 'POST', params),
  wifiDelete: (params: { interface: string }) =>
    apiCall<ApiResponse>('/wifi/delete', 'POST', params),
  listStations: (params: { interface: string }) =>
    apiCall<ListStationsResponse>(`/wifi/stations?interface=${params.interface}`),
  getWiFiConfig: (params: { interface: string }) =>
    apiCall<WiFiInterfaceConfig>(`/wifi/config?interface=${params.interface}`),

  // Firewall
  listPortForwards: () => apiCall<ListPortForwardsResponse>('/firewall/forwards'),
  addPortForward: (params: PortForward) =>
    apiCall<ApiResponse>('/firewall/forwards', 'POST', params),
  removePortForward: (params: { name: string }) =>
    apiCall<ApiResponse>('/firewall/forwards/delete', 'POST', params),

  // WAN
  getWANStatus: () => apiCall<WANStatusResponse>('/wan/status'),
  detectWAN: (_params?: undefined) => apiCall<WANDetectResponse>('/wan/detect'),
  configureWAN: (params: WANConfigureRequest) =>
    apiCall<ChangeResponse>('/wan/configure', 'POST', params),

  // WiFi WAN
  scanWiFiNetworks: (params?: { interface?: string }) =>
    apiCall<WiFiScanResponse>(`/wan/wifi/scan${params?.interface ? `?interface=${params.interface}` : ''}`),
  getWiFiWANStatus: (params?: { interface?: string }) =>
    apiCall<WiFiWANStatusResponse>(`/wan/wifi/status${params?.interface ? `?interface=${params.interface}` : ''}`),
  connectWiFiWAN: (params: WiFiConnectRequest) =>
    apiCall<ApiResponse>('/wan/wifi/connect', 'POST', params),
  disconnectWiFiWAN: (params?: { interface?: string }) =>
    apiCall<ApiResponse>('/wan/wifi/disconnect', 'POST', params || {}),

  // Multi-WAN
  getMultiWANStatus: () => apiCall<MultiWANStatus>('/multiwan/status'),
  configureMultiWAN: (params: MultiWANConfig) =>
    apiCall<ApiResponse>('/multiwan/configure', 'POST', params),
  switchMultiWAN: (params: { interface: string }) =>
    apiCall<ApiResponse>('/multiwan/switch', 'POST', params),

  // WAN List Management (unified WAN management)
  getWANs: () => apiCall<WANsResponse>('/wans'),
  addWAN: (params: Omit<WANConfigItem, 'id'>) =>
    apiCall<WANConfigItem>('/wans', 'POST', params),
  updateWAN: (params: { id: string; wan: Partial<WANConfigItem> }) =>
    apiCall<WANConfigItem>(`/wans/${params.id}`, 'PUT', params.wan),
  deleteWAN: (id: string) =>
    apiCall<ApiResponse>(`/wans/${id}`, 'DELETE'),
  reorderWANs: (params: { order: string[] }) =>
    apiCall<WANConfigItem[]>('/wans/reorder', 'POST', params),
  autoDetectWANs: () =>
    apiCall<{ message: string; added: WANConfigItem[] }>('/wans/autodetect', 'POST'),
  refreshWANDHCP: (id: string) =>
    apiCall<{ success: boolean; message: string; output: string }>(`/wans/${id}/refresh-dhcp`, 'POST'),

  // System
  getSystemDependencies: () => apiCall<SystemDependenciesResponse>('/system/dependencies'),
  getSystemLogs: (params?: { limit?: number }) =>
    apiCall<SystemLogsResponse>(`/system/logs${params?.limit ? `?limit=${params.limit}` : ''}`),
  systemShutdown: () => apiCall<ApiResponse>('/system/shutdown', 'POST'),
  systemReboot: () => apiCall<ApiResponse>('/system/reboot', 'POST'),

  // WiFi Cards
  getWiFiCards: (apOnly: boolean = false) =>
    apiCall<WiFiCardsResponse>(`/wifi/cards${apOnly ? '?ap_only=true' : ''}`),

  // Health Checks
  runHealthChecks: () => apiCall<HealthCheckResponse>('/health/check'),
  runHealthFix: (params: { fixer_id: string; dry_run?: boolean }) =>
    apiCall<ApiResponse & { dry_run?: boolean; description?: string }>('/health/fix', 'POST', params),

  // Service Management
  getServiceStatus: () => apiCall<ServiceStatus>('/service/status'),
  installService: () => apiCall<ApiResponse>('/service/install', 'POST'),
  enableService: () => apiCall<ApiResponse>('/service/enable', 'POST'),
  startService: () => apiCall<ApiResponse>('/service/start', 'POST'),
  restartService: () => apiCall<ApiResponse>('/service/restart', 'POST'),

  // External DNS
  getExternalDNSStatus: () => apiCall<ExternalDNSStatus>('/external-dns/status'),
  getExternalDNSSettings: () => apiCall<ExternalDNSSettings>('/external-dns/settings'),
  updateExternalDNSSettings: (params: Partial<ExternalDNSSettings>) =>
    apiCall<ApiResponse>('/external-dns/settings', 'POST', params),
  getPublicIP: () => apiCall<{ public_ip: string }>('/external-dns/public-ip'),
  refreshPublicIP: () => apiCall<{ public_ip: string }>('/external-dns/public-ip', 'POST'),
  listExternalDNSZones: () => apiCall<{ zones: ExternalDNSZone[] }>('/external-dns/zones'),
  addExternalDNSZone: (params: Omit<ExternalDNSZone, 'id'>) =>
    apiCall<{ success: boolean; zone: ExternalDNSZone }>('/external-dns/zones', 'POST', params),
  updateExternalDNSZone: (id: string, params: Partial<ExternalDNSZone>) =>
    apiCall<ApiResponse>(`/external-dns/zones/${id}`, 'PUT', params),
  deleteExternalDNSZone: (id: string) =>
    apiCall<ApiResponse>(`/external-dns/zones/${id}`, 'DELETE'),
  listExternalDNSRecords: () => apiCall<{ records: ExternalDNSRecord[] }>('/external-dns/records'),
  addExternalDNSRecord: (params: Omit<ExternalDNSRecord, 'id'>) =>
    apiCall<{ success: boolean; record: ExternalDNSRecord }>('/external-dns/records', 'POST', params),
  updateExternalDNSRecord: (id: string, params: Partial<ExternalDNSRecord>) =>
    apiCall<ApiResponse>(`/external-dns/records/${id}`, 'PUT', params),
  deleteExternalDNSRecord: (id: string) =>
    apiCall<ApiResponse>(`/external-dns/records/${id}`, 'DELETE'),
  syncExternalDNS: (params?: { record_id?: string; dry_run?: boolean }) =>
    apiCall<{ success: boolean; dry_run?: boolean; results: SyncResult[] }>('/external-dns/sync', 'POST', params || {}),
  discoverDNSZones: (provider: DNSProviderConfig) =>
    apiCall<{ zones: DiscoveredZone[] }>('/external-dns/discover-zones', 'POST', provider),
  requestSSLCertificate: (params: { zone_id: string; dry_run?: boolean }) =>
    apiCall<{ success: boolean; dry_run?: boolean; cert_path: string; key_path: string; message?: string }>('/external-dns/ssl/request', 'POST', params),
  getSSLStatus: (zoneId: string) =>
    apiCall<SSLStatus>(`/external-dns/ssl/status?zone_id=${zoneId}`),

  // Statistics
  getInterfaceStats: (interfaceName?: string) =>
    interfaceName
      ? apiCall<InterfaceRates>(`/stats/interfaces?interface=${interfaceName}`)
      : apiCall<Record<string, InterfaceRates>>('/stats/interfaces'),
  getLatency: () => apiCall<LatencyResponse>('/stats/latency'),
  measureLatency: (target?: string) =>
    apiCall<LatencyStats>('/stats/latency', 'POST', { measure: true, target }),
  setLatencyTarget: (target: string) =>
    apiCall<ApiResponse>('/stats/latency', 'POST', { target }),
  getStatsSnapshot: () => apiCall<StatsSnapshot>('/stats/snapshot'),
  getWiFiClientStats: () => apiCall<WiFiClientsResponse>('/stats/wifi/clients'),
  getStatsHistory: (
    interfaceName: string,
    range: string = '24h',
    resolution?: Resolution
  ) => {
    let url = `/stats/history?interface=${encodeURIComponent(interfaceName)}&range=${encodeURIComponent(range)}`;
    if (resolution) {
      url += `&resolution=${encodeURIComponent(resolution)}`;
    }
    return apiCall<HistoryResponse>(url);
  },
  getWiFiClientStatsHistory: (
    range: string = '24h',
    mac?: string,
    resolution?: Resolution
  ) => {
    let url = `/stats/wifi-clients/history?range=${encodeURIComponent(range)}`;
    if (mac) {
      url += `&mac=${encodeURIComponent(mac)}`;
    }
    if (resolution) {
      url += `&resolution=${encodeURIComponent(resolution)}`;
    }
    return apiCall<WiFiClientHistoryResponse>(url);
  },

  // QoS
  getQoSStatus: () => apiCall<QoSStatus>('/qos/status'),
  configureQoS: (params: QoSConfigRequest) =>
    apiCall<ApiResponse>('/qos/configure', 'POST', params),

  // Devices (hostname tracking)
  listDevices: () => apiCall<DevicesResponse>('/devices'),
  getDevice: (mac: string) => apiCall<Device>(`/devices/${encodeURIComponent(mac)}`),
  createDevice: (device: Partial<Device>) => apiCall<DeviceResponse>('/devices', 'POST', device),
  updateDevice: (mac: string, device: Partial<Device>) =>
    apiCall<DeviceResponse>(`/devices/${encodeURIComponent(mac)}`, 'PUT', device),
  deleteDevice: (mac: string) => apiCall<ApiResponse>(`/devices/${encodeURIComponent(mac)}`, 'DELETE'),

  // Notifications
  getNotificationSettings: () => apiCall<NotificationSettings>('/notifications/settings'),
  updateNotificationSettings: (settings: NotificationSettingsUpdate) =>
    apiCall<ApiResponse>('/notifications/settings', 'POST', settings),
  testNotification: () => apiCall<ApiResponse>('/notifications/test', 'POST'),
  getNotificationEvents: (limit?: number) =>
    apiCall<NotificationEventsResponse>(`/notifications/events${limit ? `?limit=${limit}` : ''}`),

  // Services
  getServicesStatus: () => apiCall<ServicesStatus>('/services/status'),
  getServicesSettings: () => apiCall<ServicesSettings>('/services/settings'),
  updateServicesSettings: (settings: ServicesSettings) =>
    apiCall<ApiResponse>('/services/settings', 'POST', settings),
  listServices: () => apiCall<{ services: Service[] }>('/services'),
  getService: (id: string) => apiCall<Service>(`/services/${encodeURIComponent(id)}`),
  createService: (service: Partial<Service>) =>
    apiCall<{ success: boolean; service: Service }>('/services', 'POST', service),
  updateService: (params: { id: string; service: Partial<Service> }) =>
    apiCall<{ success: boolean; service: Service }>(`/services/${encodeURIComponent(params.id)}`, 'PUT', params.service),
  deleteService: (id: string) =>
    apiCall<ApiResponse>(`/services/${encodeURIComponent(id)}`, 'DELETE'),
  syncService: (id: string) =>
    apiCall<ServiceSyncResult>(`/services/${encodeURIComponent(id)}/sync`, 'POST'),
  syncAllServices: () =>
    apiCall<{ success: boolean; results: ServiceSyncResult[] }>('/services/sync-all', 'POST'),

  // HAProxy
  getHAProxyStatus: () => apiCall<HAProxyStatus>('/haproxy/status'),
  reloadHAProxy: () => apiCall<ApiResponse>('/haproxy/reload', 'POST'),
  getHAProxyConfig: () => apiCall<{ config: string }>('/haproxy/config'),
  testHAProxyConfig: () => apiCall<{ valid: boolean; error?: string }>('/haproxy/test', 'POST'),
  getHAProxyBackends: () => apiCall<{ backends: HAProxyBackend[] }>('/haproxy/backends'),
};

export type ApiMethod = keyof typeof client;
