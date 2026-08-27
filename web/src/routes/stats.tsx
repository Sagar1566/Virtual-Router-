import { createFileRoute } from '@tanstack/react-router';
import { useState, useEffect, useCallback } from 'react';
import {
  Box,
  Card,
  CardContent,
  Grid,
  Typography,
  CircularProgress,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  Chip,
  Table,
  TableBody,
  TableCell,
  TableRow,
  TextField,
  Button,
  Stack,
} from '@mui/material';
import {
  Speed as SpeedIcon,
  NetworkCheck as LatencyIcon,
  SwapVert as TrafficIcon,
  Wifi as WifiIcon,
  SignalCellular4Bar as SignalIcon,
} from '@mui/icons-material';
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  AreaChart,
  Area,
  Legend,
  PieChart,
  Pie,
  Cell,
} from 'recharts';
import { client } from '../api/client';
import type { InterfaceRates, LatencyStats, WiFiClientRates, AggregatedStats, WiFiClientAggregatedStats } from '../api/client';
import { useQuery } from '../api/hooks';

// Generate a consistent color for a MAC address
function getMacColor(_mac: string, index: number): string {
  const colors = [
    '#4caf50', '#2196f3', '#ff9800', '#e91e63', '#9c27b0',
    '#00bcd4', '#8bc34a', '#ff5722', '#673ab7', '#3f51b5',
    '#009688', '#ffc107', '#795548', '#607d8b', '#f44336',
  ];
  return colors[index % colors.length];
}

// Format bytes to human readable
function formatBytes(bytes: number, decimals = 2): string {
  if (!bytes || isNaN(bytes) || bytes <= 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(decimals)) + ' ' + sizes[i];
}

// Format bytes per second
function formatBps(bps: number): string {
  if (!bps || isNaN(bps) || bps <= 0) return '0 B/s';
  const k = 1024;
  const sizes = ['B/s', 'KB/s', 'MB/s', 'GB/s'];
  const i = Math.floor(Math.log(bps) / Math.log(k));
  return parseFloat((bps / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

interface BandwidthDataPoint {
  time: string;
  rx: number;
  tx: number;
}

interface LatencyDataPoint {
  time: string;
  latency: number;
  min: number;
  max: number;
}

// Format seconds to human readable duration
function formatDuration(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
  const hours = Math.floor(seconds / 3600);
  const mins = Math.floor((seconds % 3600) / 60);
  return `${hours}h ${mins}m`;
}

// Get signal quality label and color
function getSignalQuality(signal: number): { label: string; color: 'success' | 'warning' | 'error' } {
  if (signal >= -50) return { label: 'Excellent', color: 'success' };
  if (signal >= -60) return { label: 'Good', color: 'success' };
  if (signal >= -70) return { label: 'Fair', color: 'warning' };
  return { label: 'Weak', color: 'error' };
}

// Available time ranges
const TIME_RANGES = [
  { value: 'realtime', label: 'Real-time' },
  { value: '1h', label: '1 Hour' },
  { value: '6h', label: '6 Hours' },
  { value: '24h', label: '24 Hours' },
  { value: '7d', label: '7 Days' },
  { value: '30d', label: '30 Days' },
] as const;

function StatsPage() {
  const [interfaces, setInterfaces] = useState<Record<string, InterfaceRates>>({});
  const [selectedInterface, setSelectedInterface] = useState<string>('');
  const [timeRange, setTimeRange] = useState<string>('realtime');
  const [bandwidthHistory, setBandwidthHistory] = useState<BandwidthDataPoint[]>([]);
  const [latencyHistory, setLatencyHistory] = useState<LatencyDataPoint[]>([]);
  const [historicalData, setHistoricalData] = useState<AggregatedStats[]>([]);
  const [wifiClientHistoricalData, setWifiClientHistoricalData] = useState<WiFiClientAggregatedStats[]>([]);
  const [wifiClientRealtimeHistory, setWifiClientRealtimeHistory] = useState<Array<{ time: string; [key: string]: string | number }>>([]);
  const [currentLatency, setCurrentLatency] = useState<LatencyStats | null>(null);
  const [latencyTarget, setLatencyTarget] = useState('8.8.8.8');
  const [wifiClients, setWifiClients] = useState<WiFiClientRates[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Fetch devices for name lookup
  const { data: devicesData } = useQuery('listDevices');

  // Get display name for a MAC address from devices database
  const getDeviceName = (mac: string): string | null => {
    const device = devicesData?.devices?.find(
      d => d.mac.toLowerCase() === mac.toLowerCase()
    );
    if (!device) return null;
    return device.display_name || device.hostname || null;
  };

  const fetchStats = useCallback(async () => {
    try {
      const [statsData, latencyData, wifiData] = await Promise.all([
        client.getInterfaceStats() as Promise<Record<string, InterfaceRates>>,
        client.getLatency(),
        client.getWiFiClientStats().catch(() => ({ clients: [] })),
      ]);

      setInterfaces(statsData);
      const clients = wifiData.clients || [];
      setWifiClients(clients);

      // Auto-select first interface if none selected
      if (!selectedInterface && Object.keys(statsData).length > 0) {
        setSelectedInterface(Object.keys(statsData)[0]);
      }

      // Update bandwidth history for selected interface
      if (selectedInterface && statsData[selectedInterface]) {
        const iface = statsData[selectedInterface];
        const now = new Date().toLocaleTimeString();
        setBandwidthHistory(prev => {
          const next = [...prev, {
            time: now,
            rx: iface.rx_bytes_per_sec,
            tx: iface.tx_bytes_per_sec,
          }];
          // Keep last 60 data points
          return next.slice(-60);
        });
      }

      // Update WiFi client real-time history
      if (clients.length > 0) {
        const now = new Date().toLocaleTimeString();
        const dataPoint: { time: string; [key: string]: string | number } = { time: now };
        clients.forEach(c => {
          dataPoint[`${c.mac}_rx`] = c.rx_bytes_per_sec;
          dataPoint[`${c.mac}_tx`] = c.tx_bytes_per_sec;
        });
        setWifiClientRealtimeHistory(prev => {
          const next = [...prev, dataPoint];
          return next.slice(-60);
        });
      }

      // Update latency
      if (latencyData.current) {
        setCurrentLatency(latencyData.current);
        const now = new Date().toLocaleTimeString();
        setLatencyHistory(prev => {
          const next = [...prev, {
            time: now,
            latency: latencyData.current.avg_latency_ms,
            min: latencyData.current.min_latency_ms,
            max: latencyData.current.max_latency_ms,
          }];
          return next.slice(-60);
        });
      }

      setError(null);
    } catch (e) {
      setError(String(e));
    } finally {
      setIsLoading(false);
    }
  }, [selectedInterface]);

  // Fetch historical data when in history mode
  const fetchHistoricalData = useCallback(async () => {
    if (!selectedInterface || timeRange === 'realtime') return;

    try {
      setIsLoading(true);
      const [interfaceResponse, wifiResponse] = await Promise.all([
        client.getStatsHistory(selectedInterface, timeRange),
        client.getWiFiClientStatsHistory(timeRange).catch(() => ({ clients: [] })),
      ]);
      setHistoricalData(interfaceResponse.data_points || []);
      setWifiClientHistoricalData(wifiResponse.clients || []);
      setError(null);
    } catch (e) {
      setError(String(e));
      setHistoricalData([]);
      setWifiClientHistoricalData([]);
    } finally {
      setIsLoading(false);
    }
  }, [selectedInterface, timeRange]);

  // Initial fetch and polling (only in realtime mode)
  useEffect(() => {
    if (timeRange === 'realtime') {
      fetchStats();
      const interval = setInterval(fetchStats, 1000);
      return () => clearInterval(interval);
    } else {
      // Fetch historical data when not in realtime mode
      fetchHistoricalData();
    }
  }, [fetchStats, fetchHistoricalData, timeRange]);

  // Reset history when interface or time range changes
  useEffect(() => {
    setBandwidthHistory([]);
    setHistoricalData([]);
    setWifiClientHistoricalData([]);
    setWifiClientRealtimeHistory([]);
  }, [selectedInterface, timeRange]);

  const handleMeasureLatency = async () => {
    try {
      const result = await client.measureLatency(latencyTarget);
      setCurrentLatency(result);
    } catch (e) {
      console.error('Failed to measure latency:', e);
    }
  };

  const handleSetLatencyTarget = async () => {
    try {
      await client.setLatencyTarget(latencyTarget);
    } catch (e) {
      console.error('Failed to set latency target:', e);
    }
  };

  if (isLoading) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', mt: 4 }}>
        <CircularProgress />
      </Box>
    );
  }

  if (error) {
    return (
      <Box sx={{ p: 2 }}>
        <Typography color="error">Failed to load stats: {error}</Typography>
      </Box>
    );
  }

  const currentInterface = selectedInterface ? interfaces[selectedInterface] : null;

  return (
    <Box>
      <Typography variant="h4" gutterBottom>
        Network Statistics
      </Typography>

      <Grid container spacing={3}>
        {/* Interface and Time Range Selectors */}
        <Grid size={{ xs: 12, md: 6 }}>
          <FormControl fullWidth size="small">
            <InputLabel>Interface</InputLabel>
            <Select
              value={selectedInterface}
              label="Interface"
              onChange={(e) => setSelectedInterface(e.target.value)}
            >
              {Object.keys(interfaces).map((name) => (
                <MenuItem key={name} value={name}>{name}</MenuItem>
              ))}
            </Select>
          </FormControl>
        </Grid>
        <Grid size={{ xs: 12, md: 6 }}>
          <FormControl fullWidth size="small">
            <InputLabel>Time Range</InputLabel>
            <Select
              value={timeRange}
              label="Time Range"
              onChange={(e) => setTimeRange(e.target.value)}
            >
              {TIME_RANGES.map(({ value, label }) => (
                <MenuItem key={value} value={value}>{label}</MenuItem>
              ))}
            </Select>
          </FormControl>
        </Grid>

        {/* Bandwidth Chart */}
        <Grid size={{ xs: 12, lg: 8 }}>
          <Card>
            <CardContent>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 2 }}>
                <TrafficIcon color="primary" />
                <Typography variant="h6" color="primary">
                  Bandwidth - {selectedInterface || 'Select Interface'}
                  {timeRange !== 'realtime' && (
                    <Chip
                      label={TIME_RANGES.find(r => r.value === timeRange)?.label || timeRange}
                      size="small"
                      color="info"
                      sx={{ ml: 1 }}
                    />
                  )}
                </Typography>
              </Box>
              <Box sx={{ height: 300 }}>
                <ResponsiveContainer width="100%" height="100%">
                  <AreaChart
                    data={
                      timeRange === 'realtime'
                        ? bandwidthHistory
                        : historicalData.map((d) => ({
                            time: new Date(d.timestamp).toLocaleString(),
                            rx: d.avg_rx_bytes_per_sec,
                            tx: d.avg_tx_bytes_per_sec,
                          }))
                    }
                  >
                    <CartesianGrid strokeDasharray="3 3" stroke="#333" />
                    <XAxis
                      dataKey="time"
                      stroke="#888"
                      tick={{ fill: '#888', fontSize: 10 }}
                      interval="preserveStartEnd"
                    />
                    <YAxis
                      stroke="#888"
                      tick={{ fill: '#888', fontSize: 10 }}
                      tickFormatter={(value) => formatBps(value)}
                    />
                    <Tooltip
                      contentStyle={{ backgroundColor: '#1e1e1e', border: '1px solid #333' }}
                      labelStyle={{ color: '#fff' }}
                      formatter={(value) => formatBps(Number(value) || 0)}
                    />
                    <Area
                      type="monotone"
                      dataKey="rx"
                      name="Download"
                      stroke="#4caf50"
                      fill="#4caf50"
                      fillOpacity={0.3}
                    />
                    <Area
                      type="monotone"
                      dataKey="tx"
                      name="Upload"
                      stroke="#2196f3"
                      fill="#2196f3"
                      fillOpacity={0.3}
                    />
                  </AreaChart>
                </ResponsiveContainer>
              </Box>
            </CardContent>
          </Card>
        </Grid>

        {/* Current Stats */}
        <Grid size={{ xs: 12, lg: 4 }}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 2 }}>
                <SpeedIcon color="primary" />
                <Typography variant="h6" color="primary">
                  Current Rates
                </Typography>
              </Box>
              {currentInterface ? (
                <Table size="small">
                  <TableBody>
                    <TableRow>
                      <TableCell sx={{ border: 0, py: 1 }}>Download</TableCell>
                      <TableCell sx={{ border: 0, py: 1 }}>
                        <Chip
                          label={formatBps(currentInterface.rx_bytes_per_sec)}
                          color="success"
                          size="small"
                        />
                      </TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell sx={{ border: 0, py: 1 }}>Upload</TableCell>
                      <TableCell sx={{ border: 0, py: 1 }}>
                        <Chip
                          label={formatBps(currentInterface.tx_bytes_per_sec)}
                          color="primary"
                          size="small"
                        />
                      </TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell sx={{ border: 0, py: 1 }}>RX Packets/s</TableCell>
                      <TableCell sx={{ border: 0, py: 1 }}>
                        {currentInterface.rx_packets_per_sec.toFixed(1)}
                      </TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell sx={{ border: 0, py: 1 }}>TX Packets/s</TableCell>
                      <TableCell sx={{ border: 0, py: 1 }}>
                        {currentInterface.tx_packets_per_sec.toFixed(1)}
                      </TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell sx={{ border: 0, py: 1 }}>Avg RX Packet</TableCell>
                      <TableCell sx={{ border: 0, py: 1 }}>
                        {currentInterface.rx_avg_packet_size.toFixed(0)} bytes
                      </TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell sx={{ border: 0, py: 1 }}>Avg TX Packet</TableCell>
                      <TableCell sx={{ border: 0, py: 1 }}>
                        {currentInterface.tx_avg_packet_size.toFixed(0)} bytes
                      </TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell sx={{ border: 0, py: 1 }}>Total RX</TableCell>
                      <TableCell sx={{ border: 0, py: 1 }}>
                        {formatBytes(currentInterface.total_rx_bytes)}
                      </TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell sx={{ border: 0, py: 1 }}>Total TX</TableCell>
                      <TableCell sx={{ border: 0, py: 1 }}>
                        {formatBytes(currentInterface.total_tx_bytes)}
                      </TableCell>
                    </TableRow>
                  </TableBody>
                </Table>
              ) : (
                <Typography color="text.secondary">
                  Select an interface to view stats
                </Typography>
              )}
            </CardContent>
          </Card>
        </Grid>

        {/* Latency Chart */}
        <Grid size={{ xs: 12, lg: 8 }}>
          <Card>
            <CardContent>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 2 }}>
                <LatencyIcon color="primary" />
                <Typography variant="h6" color="primary">
                  Latency - {currentLatency?.target || latencyTarget}
                </Typography>
              </Box>
              <Box sx={{ height: 250 }}>
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={latencyHistory}>
                    <CartesianGrid strokeDasharray="3 3" stroke="#333" />
                    <XAxis
                      dataKey="time"
                      stroke="#888"
                      tick={{ fill: '#888', fontSize: 10 }}
                      interval="preserveStartEnd"
                    />
                    <YAxis
                      stroke="#888"
                      tick={{ fill: '#888', fontSize: 10 }}
                      unit=" ms"
                    />
                    <Tooltip
                      contentStyle={{ backgroundColor: '#1e1e1e', border: '1px solid #333' }}
                      labelStyle={{ color: '#fff' }}
                      formatter={(value) => `${(Number(value) || 0).toFixed(2)} ms`}
                    />
                    <Line
                      type="monotone"
                      dataKey="latency"
                      name="Avg Latency"
                      stroke="#ff9800"
                      strokeWidth={2}
                      dot={false}
                    />
                    <Line
                      type="monotone"
                      dataKey="min"
                      name="Min"
                      stroke="#4caf50"
                      strokeWidth={1}
                      strokeDasharray="5 5"
                      dot={false}
                    />
                    <Line
                      type="monotone"
                      dataKey="max"
                      name="Max"
                      stroke="#f44336"
                      strokeWidth={1}
                      strokeDasharray="5 5"
                      dot={false}
                    />
                  </LineChart>
                </ResponsiveContainer>
              </Box>
            </CardContent>
          </Card>
        </Grid>

        {/* Latency Stats & Config */}
        <Grid size={{ xs: 12, lg: 4 }}>
          <Card sx={{ height: '100%' }}>
            <CardContent>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 2 }}>
                <LatencyIcon color="primary" />
                <Typography variant="h6" color="primary">
                  Latency Stats
                </Typography>
              </Box>

              {currentLatency && (
                <Table size="small" sx={{ mb: 2 }}>
                  <TableBody>
                    <TableRow>
                      <TableCell sx={{ border: 0, py: 1 }}>Status</TableCell>
                      <TableCell sx={{ border: 0, py: 1 }}>
                        <Chip
                          label={currentLatency.reachable ? 'Reachable' : 'Unreachable'}
                          color={currentLatency.reachable ? 'success' : 'error'}
                          size="small"
                        />
                      </TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell sx={{ border: 0, py: 1 }}>Average</TableCell>
                      <TableCell sx={{ border: 0, py: 1 }}>
                        <strong>{currentLatency.avg_latency_ms.toFixed(2)} ms</strong>
                      </TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell sx={{ border: 0, py: 1 }}>Min / Max</TableCell>
                      <TableCell sx={{ border: 0, py: 1 }}>
                        {currentLatency.min_latency_ms.toFixed(2)} / {currentLatency.max_latency_ms.toFixed(2)} ms
                      </TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell sx={{ border: 0, py: 1 }}>Packet Loss</TableCell>
                      <TableCell sx={{ border: 0, py: 1 }}>
                        <Chip
                          label={`${currentLatency.packet_loss.toFixed(1)}%`}
                          color={currentLatency.packet_loss === 0 ? 'success' : 'warning'}
                          size="small"
                        />
                      </TableCell>
                    </TableRow>
                  </TableBody>
                </Table>
              )}

              <Stack spacing={1}>
                <TextField
                  size="small"
                  label="Ping Target"
                  value={latencyTarget}
                  onChange={(e) => setLatencyTarget(e.target.value)}
                  fullWidth
                />
                <Box sx={{ display: 'flex', gap: 1 }}>
                  <Button
                    variant="outlined"
                    size="small"
                    onClick={handleSetLatencyTarget}
                    fullWidth
                  >
                    Set Target
                  </Button>
                  <Button
                    variant="contained"
                    size="small"
                    onClick={handleMeasureLatency}
                    fullWidth
                  >
                    Measure Now
                  </Button>
                </Box>
              </Stack>
            </CardContent>
          </Card>
        </Grid>

        {/* Traffic Distribution Pie Charts */}
        <Grid size={{ xs: 12, md: 6 }}>
          <Card>
            <CardContent>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 2 }}>
                <TrafficIcon color="primary" />
                <Typography variant="h6" color="primary">
                  Interface Traffic Distribution
                </Typography>
              </Box>
              <Box sx={{ height: 300 }}>
                <ResponsiveContainer width="100%" height="100%">
                  {(() => {
                    // Build pie data for interfaces
                    const pieData: Array<{ name: string; value: number; color: string }> = [];
                    Object.entries(interfaces).forEach(([name, stats], index) => {
                      if (stats.total_rx_bytes > 0) {
                        pieData.push({
                          name: `${name} ↓`,
                          value: stats.total_rx_bytes,
                          color: getMacColor(name, index * 2),
                        });
                      }
                      if (stats.total_tx_bytes > 0) {
                        pieData.push({
                          name: `${name} ↑`,
                          value: stats.total_tx_bytes,
                          color: getMacColor(name, index * 2 + 1),
                        });
                      }
                    });

                    if (pieData.length === 0) {
                      return (
                        <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
                          <Typography color="text.secondary">No interface traffic data</Typography>
                        </Box>
                      );
                    }

                    return (
                      <PieChart>
                        <Pie
                          data={pieData}
                          dataKey="value"
                          nameKey="name"
                          cx="50%"
                          cy="50%"
                          outerRadius={100}
                          label={({ name, percent }) => `${name} ${((percent ?? 0) * 100).toFixed(0)}%`}
                          labelLine={{ stroke: '#888' }}
                        >
                          {pieData.map((entry, index) => (
                            <Cell key={`cell-${index}`} fill={entry.color} />
                          ))}
                        </Pie>
                        <Tooltip
                          contentStyle={{ backgroundColor: '#1e1e1e', border: '1px solid #333' }}
                          formatter={(value) => formatBytes(Number(value))}
                        />
                        <Legend
                          formatter={(value) => value}
                          wrapperStyle={{ fontSize: '12px' }}
                        />
                      </PieChart>
                    );
                  })()}
                </ResponsiveContainer>
              </Box>
            </CardContent>
          </Card>
        </Grid>

        <Grid size={{ xs: 12, md: 6 }}>
          <Card>
            <CardContent>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 2 }}>
                <WifiIcon color="primary" />
                <Typography variant="h6" color="primary">
                  WiFi Client Traffic Distribution
                </Typography>
              </Box>
              <Box sx={{ height: 300 }}>
                <ResponsiveContainer width="100%" height="100%">
                  {(() => {
                    // Build pie data for WiFi clients
                    const pieData: Array<{ name: string; value: number; color: string }> = [];
                    wifiClients.forEach((wifiClient, index) => {
                      const deviceName = getDeviceName(wifiClient.mac);
                      const displayName = deviceName || wifiClient.mac.toUpperCase().slice(-8);
                      if (wifiClient.total_rx_bytes > 0) {
                        pieData.push({
                          name: `${displayName} ↓`,
                          value: wifiClient.total_rx_bytes,
                          color: getMacColor(wifiClient.mac, index * 2),
                        });
                      }
                      if (wifiClient.total_tx_bytes > 0) {
                        pieData.push({
                          name: `${displayName} ↑`,
                          value: wifiClient.total_tx_bytes,
                          color: getMacColor(wifiClient.mac, index * 2 + 1),
                        });
                      }
                    });

                    if (pieData.length === 0) {
                      return (
                        <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>
                          <Typography color="text.secondary">No WiFi client traffic data</Typography>
                        </Box>
                      );
                    }

                    return (
                      <PieChart>
                        <Pie
                          data={pieData}
                          dataKey="value"
                          nameKey="name"
                          cx="50%"
                          cy="50%"
                          outerRadius={100}
                          label={({ name, percent }) => `${name} ${((percent ?? 0) * 100).toFixed(0)}%`}
                          labelLine={{ stroke: '#888' }}
                        >
                          {pieData.map((entry, index) => (
                            <Cell key={`cell-${index}`} fill={entry.color} />
                          ))}
                        </Pie>
                        <Tooltip
                          contentStyle={{ backgroundColor: '#1e1e1e', border: '1px solid #333' }}
                          formatter={(value) => formatBytes(Number(value))}
                        />
                        <Legend
                          formatter={(value) => value}
                          wrapperStyle={{ fontSize: '12px' }}
                        />
                      </PieChart>
                    );
                  })()}
                </ResponsiveContainer>
              </Box>
            </CardContent>
          </Card>
        </Grid>

        {/* All Interfaces Overview */}
        <Grid size={12}>
          <Card>
            <CardContent>
              <Typography variant="h6" color="primary" gutterBottom>
                All Interfaces
              </Typography>
              <Box sx={{ overflowX: 'auto' }}>
                <Table size="small">
                  <TableBody>
                    <TableRow sx={{ '& th': { fontWeight: 'bold', color: 'text.secondary' } }}>
                      <TableCell component="th">Interface</TableCell>
                      <TableCell component="th" align="right">Download</TableCell>
                      <TableCell component="th" align="right">Upload</TableCell>
                      <TableCell component="th" align="right">RX Pkts/s</TableCell>
                      <TableCell component="th" align="right">TX Pkts/s</TableCell>
                      <TableCell component="th" align="right">Total RX</TableCell>
                      <TableCell component="th" align="right">Total TX</TableCell>
                    </TableRow>
                    {Object.entries(interfaces).map(([name, stats]) => (
                      <TableRow
                        key={name}
                        sx={{
                          cursor: 'pointer',
                          bgcolor: name === selectedInterface ? 'action.selected' : 'transparent',
                          '&:hover': { bgcolor: 'action.hover' },
                        }}
                        onClick={() => setSelectedInterface(name)}
                      >
                        <TableCell>
                          <code>{name}</code>
                        </TableCell>
                        <TableCell align="right">
                          <Chip
                            label={formatBps(stats.rx_bytes_per_sec)}
                            color="success"
                            size="small"
                            variant="outlined"
                          />
                        </TableCell>
                        <TableCell align="right">
                          <Chip
                            label={formatBps(stats.tx_bytes_per_sec)}
                            color="primary"
                            size="small"
                            variant="outlined"
                          />
                        </TableCell>
                        <TableCell align="right">{stats.rx_packets_per_sec.toFixed(1)}</TableCell>
                        <TableCell align="right">{stats.tx_packets_per_sec.toFixed(1)}</TableCell>
                        <TableCell align="right">{formatBytes(stats.total_rx_bytes)}</TableCell>
                        <TableCell align="right">{formatBytes(stats.total_tx_bytes)}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </Box>
            </CardContent>
          </Card>
        </Grid>

        {/* WiFi Clients */}
        <Grid size={12}>
          <Card>
            <CardContent>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 2 }}>
                <WifiIcon color="primary" />
                <Typography variant="h6" color="primary">
                  WiFi Clients
                </Typography>
                <Chip
                  label={`${wifiClients.length} connected`}
                  size="small"
                  color={wifiClients.length > 0 ? 'success' : 'default'}
                  sx={{ ml: 'auto' }}
                />
              </Box>
              {wifiClients.length === 0 ? (
                <Typography color="text.secondary">
                  No WiFi clients connected
                </Typography>
              ) : (
                <Box sx={{ overflowX: 'auto' }}>
                  <Table size="small">
                    <TableBody>
                      <TableRow sx={{ '& th': { fontWeight: 'bold', color: 'text.secondary' } }}>
                        <TableCell component="th">MAC Address</TableCell>
                        <TableCell component="th">Interface</TableCell>
                        <TableCell component="th" align="center">Signal</TableCell>
                        <TableCell component="th" align="right">Download</TableCell>
                        <TableCell component="th" align="right">Upload</TableCell>
                        <TableCell component="th" align="right">RX Rate</TableCell>
                        <TableCell component="th" align="right">TX Rate</TableCell>
                        <TableCell component="th" align="right">Total RX</TableCell>
                        <TableCell component="th" align="right">Total TX</TableCell>
                        <TableCell component="th" align="right">Connected</TableCell>
                      </TableRow>
                      {wifiClients.map((wifiClient) => {
                        const signalQuality = getSignalQuality(wifiClient.signal);
                        const deviceName = getDeviceName(wifiClient.mac);
                        return (
                          <TableRow key={wifiClient.mac}>
                            <TableCell>
                              {deviceName ? (
                                <>
                                  <Typography variant="body2">{deviceName}</Typography>
                                  <Typography variant="caption" color="text.secondary">
                                    <code>{wifiClient.mac.toUpperCase()}</code>
                                  </Typography>
                                </>
                              ) : (
                                <code>{wifiClient.mac.toUpperCase()}</code>
                              )}
                            </TableCell>
                            <TableCell>
                              <code>{wifiClient.interface}</code>
                            </TableCell>
                            <TableCell align="center">
                              <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 0.5 }}>
                                <SignalIcon fontSize="small" color={signalQuality.color} />
                                <Typography variant="body2">
                                  {wifiClient.signal} dBm
                                </Typography>
                              </Box>
                            </TableCell>
                            <TableCell align="right">
                              <Chip
                                label={formatBps(wifiClient.rx_bytes_per_sec)}
                                color="success"
                                size="small"
                                variant="outlined"
                              />
                            </TableCell>
                            <TableCell align="right">
                              <Chip
                                label={formatBps(wifiClient.tx_bytes_per_sec)}
                                color="primary"
                                size="small"
                                variant="outlined"
                              />
                            </TableCell>
                            <TableCell align="right">
                              {wifiClient.rx_bitrate.toFixed(1)} Mbps
                            </TableCell>
                            <TableCell align="right">
                              {wifiClient.tx_bitrate.toFixed(1)} Mbps
                            </TableCell>
                            <TableCell align="right">
                              {formatBytes(wifiClient.total_rx_bytes)}
                            </TableCell>
                            <TableCell align="right">
                              {formatBytes(wifiClient.total_tx_bytes)}
                            </TableCell>
                            <TableCell align="right">
                              {formatDuration(wifiClient.connected_time)}
                            </TableCell>
                          </TableRow>
                        );
                      })}
                    </TableBody>
                  </Table>
                </Box>
              )}
            </CardContent>
          </Card>
        </Grid>

        {/* WiFi Client Bandwidth (Stacked Chart) */}
        {((timeRange === 'realtime' && wifiClientRealtimeHistory.length > 0) ||
          (timeRange !== 'realtime' && wifiClientHistoricalData.length > 0)) && (
          <Grid size={12}>
            <Card>
              <CardContent>
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 2 }}>
                  <WifiIcon color="primary" />
                  <Typography variant="h6" color="primary">
                    WiFi Client Bandwidth
                  </Typography>
                  {timeRange !== 'realtime' && (
                    <Chip
                      label={TIME_RANGES.find(r => r.value === timeRange)?.label || timeRange}
                      size="small"
                      color="info"
                      sx={{ ml: 1 }}
                    />
                  )}
                </Box>
                <Box sx={{ height: 300 }}>
                  <ResponsiveContainer width="100%" height="100%">
                    {(() => {
                      let chartData: Array<Record<string, string | number>>;
                      let uniqueMacs: string[];

                      if (timeRange === 'realtime') {
                        // Use real-time data
                        chartData = wifiClientRealtimeHistory;
                        // Extract unique MACs from the data keys
                        const macSet = new Set<string>();
                        wifiClientRealtimeHistory.forEach(d => {
                          Object.keys(d).forEach(key => {
                            if (key.endsWith('_rx')) {
                              macSet.add(key.replace('_rx', ''));
                            }
                          });
                        });
                        uniqueMacs = Array.from(macSet);
                      } else {
                        // Use historical data - group by timestamp
                        uniqueMacs = [...new Set(wifiClientHistoricalData.map(d => d.mac))];
                        const dataByTimestamp = new Map<string, Record<string, string | number>>();

                        wifiClientHistoricalData.forEach(d => {
                          const ts = new Date(d.timestamp).toLocaleString();
                          if (!dataByTimestamp.has(ts)) {
                            dataByTimestamp.set(ts, { time: ts });
                          }
                          const entry = dataByTimestamp.get(ts)!;
                          entry[`${d.mac}_rx`] = d.avg_rx_bytes_per_sec;
                          entry[`${d.mac}_tx`] = d.avg_tx_bytes_per_sec;
                        });

                        chartData = Array.from(dataByTimestamp.values());
                      }

                      return (
                        <AreaChart data={chartData}>
                          <CartesianGrid strokeDasharray="3 3" stroke="#333" />
                          <XAxis
                            dataKey="time"
                            stroke="#888"
                            tick={{ fill: '#888', fontSize: 10 }}
                            interval="preserveStartEnd"
                          />
                          <YAxis
                            stroke="#888"
                            tick={{ fill: '#888', fontSize: 10 }}
                            tickFormatter={(value) => formatBps(value)}
                          />
                          <Tooltip
                            contentStyle={{ backgroundColor: '#1e1e1e', border: '1px solid #333' }}
                            labelStyle={{ color: '#fff' }}
                            formatter={(value, name) => {
                              const numValue = Number(value) || 0;
                              const strName = String(name);
                              const mac = strName.replace(/_[rt]x$/, '');
                              const direction = strName.endsWith('_rx') ? '↓' : '↑';
                              const deviceName = getDeviceName(mac);
                              const displayName = deviceName || mac.toUpperCase().slice(-8);
                              return [formatBps(numValue), `${displayName} ${direction}`];
                            }}
                          />
                          <Legend
                            formatter={(value: string) => {
                              const mac = value.replace(/_[rt]x$/, '');
                              const direction = value.endsWith('_rx') ? '↓' : '↑';
                              const deviceName = getDeviceName(mac);
                              const displayName = deviceName || mac.toUpperCase().slice(-8);
                              return `${displayName} ${direction}`;
                            }}
                          />
                          {uniqueMacs.flatMap((mac, index) => [
                            <Area
                              key={`${mac}_rx`}
                              type="monotone"
                              dataKey={`${mac}_rx`}
                              name={`${mac}_rx`}
                              stackId="1"
                              stroke={getMacColor(mac, index * 2)}
                              fill={getMacColor(mac, index * 2)}
                              fillOpacity={0.6}
                            />,
                            <Area
                              key={`${mac}_tx`}
                              type="monotone"
                              dataKey={`${mac}_tx`}
                              name={`${mac}_tx`}
                              stackId="1"
                              stroke={getMacColor(mac, index * 2 + 1)}
                              fill={getMacColor(mac, index * 2 + 1)}
                              fillOpacity={0.4}
                            />,
                          ])}
                        </AreaChart>
                      );
                    })()}
                  </ResponsiveContainer>
                </Box>
              </CardContent>
            </Card>
          </Grid>
        )}
      </Grid>
    </Box>
  );
}

export const Route = createFileRoute('/stats')({
  component: StatsPage,
});
