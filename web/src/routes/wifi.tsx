import { createFileRoute } from '@tanstack/react-router';
import { useState, useEffect } from 'react';
import {
  Box,
  Card,
  CardContent,
  Typography,
  Button,
  TextField,
  Grid,
  Chip,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  FormControlLabel,
  Checkbox,
  CircularProgress,
  Alert,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  TableContainer,
  Collapse,
} from '@mui/material';
import {
  PlayArrow as StartIcon,
  Stop as StopIcon,
  Refresh as RestartIcon,
  Delete as DeleteIcon,
  Edit as EditIcon,
  Visibility as VisibilityIcon,
  VisibilityOff as VisibilityOffIcon,
  ExpandMore as ExpandMoreIcon,
  ExpandLess as ExpandLessIcon,
  Devices as DevicesIcon,
} from '@mui/icons-material';
import { IconButton, InputAdornment } from '@mui/material';
import { useQuery, useMutation } from '../api/hooks';
import { client } from '../api/client';
import type { WiFiStation } from '../api/client';

function WiFiPage() {
  const { data: statusData, isLoading: statusLoading, refetch } = useQuery('getWiFiStatus');
  const { data: interfacesData } = useQuery('listWiFiInterfaces');
  const { data: devicesData } = useQuery('listDevices');
  const { mutate: configureAP, isLoading: isConfiguring } = useMutation('configureAP');
  const { mutate: control, isLoading: isControlling } = useMutation('wifiControl');
  const { mutate: deleteWifi, isLoading: isDeleting } = useMutation('wifiDelete');

  // Get display name for a MAC address from devices database
  const getDeviceName = (mac: string): string | null => {
    const device = devicesData?.devices?.find(
      d => d.mac.toLowerCase() === mac.toLowerCase()
    );
    if (!device) return null;
    return device.display_name || device.hostname || null;
  };

  const [config, setConfig] = useState({
    interface: '',
    ssid: '',
    password: '',
    channel: 6,
    auto_channel: false,
    band: '2.4ghz',
    mode: 'bridge',
    bridge: 'br0',
    hidden: false,
    enabled: true,
  });
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  const [showPassword, setShowPassword] = useState(false);
  const [editingInterface, setEditingInterface] = useState<string | null>(null);
  const [stations, setStations] = useState<Record<string, WiFiStation[]>>({});
  const [expandedInterfaces, setExpandedInterfaces] = useState<Set<string>>(new Set());

  // Fetch stations for each running interface
  useEffect(() => {
    const fetchStations = async () => {
      if (!statusData?.interfaces) return;
      const newStations: Record<string, WiFiStation[]> = {};
      for (const iface of statusData.interfaces) {
        if (iface.running) {
          try {
            const result = await client.listStations({ interface: iface.interface });
            newStations[iface.interface] = result.stations || [];
          } catch {
            newStations[iface.interface] = [];
          }
        }
      }
      setStations(newStations);
    };
    fetchStations();
  }, [statusData]);

  const toggleExpanded = (iface: string) => {
    setExpandedInterfaces(prev => {
      const next = new Set(prev);
      if (next.has(iface)) {
        next.delete(iface);
      } else {
        next.add(iface);
      }
      return next;
    });
  };

  const formatBytes = (bytes: number): string => {
    if (!bytes || isNaN(bytes) || bytes < 0) return '0 B';
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
    return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
  };

  const handleConfigure = async () => {
    if (!config.interface) {
      setError('Interface is required');
      return;
    }
    try {
      await configureAP(config);
      setSuccess('WiFi configured');
      setError(null);
      setEditingInterface(null);
      setConfig({
        interface: '',
        ssid: '',
        password: '',
        channel: 6,
        auto_channel: false,
        band: '2.4ghz',
        mode: 'bridge',
        bridge: 'br0',
        hidden: false,
        enabled: true,
      });
      refetch();
    } catch (e) {
      setError(String(e));
    }
  };

  const handleControl = async (iface: string, action: 'start' | 'stop' | 'restart') => {
    try {
      await control({ interface: iface, action });
      setSuccess(`WiFi ${action}ed`);
      refetch();
    } catch (e) {
      setError(String(e));
    }
  };

  const handleDelete = async (iface: string) => {
    if (!confirm(`Delete WiFi configuration for ${iface}?`)) {
      return;
    }
    try {
      await deleteWifi({ interface: iface });
      setSuccess('WiFi interface deleted');
      setError(null);
      refetch();
    } catch (e) {
      setError(String(e));
    }
  };

  const handleEdit = async (iface: string) => {
    try {
      const wifiConfig = await client.getWiFiConfig({ interface: iface });
      setConfig({
        interface: wifiConfig.interface,
        ssid: wifiConfig.ssid,
        password: wifiConfig.password,
        channel: wifiConfig.channel,
        auto_channel: wifiConfig.auto_channel,
        band: wifiConfig.band,
        mode: wifiConfig.mode,
        bridge: wifiConfig.bridge,
        hidden: wifiConfig.hidden,
        enabled: wifiConfig.enabled,
      });
      setEditingInterface(iface);
      setError(null);
      // Scroll to the form
      window.scrollTo({ top: 0, behavior: 'smooth' });
    } catch (e) {
      setError(String(e));
    }
  };

  return (
    <Box>
      <Typography variant="h4" gutterBottom>
        WiFi Management
      </Typography>

      {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}
      {success && <Alert severity="success" sx={{ mb: 2 }}>{success}</Alert>}

      <Card sx={{ mb: 3 }}>
        <CardContent>
          <Typography variant="h6" color="primary" gutterBottom>
            {editingInterface ? `Edit Access Point (${editingInterface})` : 'Configure Access Point'}
          </Typography>
          <Grid container spacing={2}>
            <Grid size={{ xs: 12, md: 6 }}>
              <FormControl fullWidth size="small">
                <InputLabel>Interface</InputLabel>
                <Select
                  value={config.interface}
                  label="Interface"
                  onChange={(e) => setConfig({ ...config, interface: e.target.value })}
                >
                  {interfacesData?.interfaces?.map((iface) => (
                    <MenuItem key={iface} value={iface}>{iface}</MenuItem>
                  ))}
                </Select>
              </FormControl>
            </Grid>
            <Grid size={{ xs: 12, md: 6 }}>
              <TextField
                fullWidth
                label="SSID"
                size="small"
                value={config.ssid}
                onChange={(e) => setConfig({ ...config, ssid: e.target.value })}
                placeholder="Auto-generated if empty"
              />
            </Grid>
            <Grid size={{ xs: 12, md: 6 }}>
              <TextField
                fullWidth
                label="Password"
                size="small"
                type={showPassword ? 'text' : 'password'}
                value={config.password}
                onChange={(e) => setConfig({ ...config, password: e.target.value })}
                helperText="Min 8 characters"
                slotProps={{
                  input: {
                    endAdornment: (
                      <InputAdornment position="end">
                        <IconButton
                          onClick={() => setShowPassword(!showPassword)}
                          edge="end"
                          size="small"
                        >
                          {showPassword ? <VisibilityOffIcon /> : <VisibilityIcon />}
                        </IconButton>
                      </InputAdornment>
                    ),
                  },
                }}
              />
            </Grid>
            <Grid size={{ xs: 12, md: 3 }}>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                <TextField
                  fullWidth
                  label="Channel"
                  size="small"
                  type="number"
                  value={config.channel}
                  disabled={config.auto_channel}
                  onChange={(e) => setConfig({ ...config, channel: parseInt(e.target.value) })}
                />
                <FormControlLabel
                  control={
                    <Checkbox
                      size="small"
                      checked={config.auto_channel}
                      onChange={(e) => setConfig({ ...config, auto_channel: e.target.checked })}
                    />
                  }
                  label="Auto"
                  sx={{ whiteSpace: 'nowrap' }}
                />
              </Box>
            </Grid>
            <Grid size={{ xs: 12, md: 3 }}>
              <FormControl fullWidth size="small">
                <InputLabel>Band</InputLabel>
                <Select
                  value={config.band}
                  label="Band"
                  onChange={(e) => setConfig({ ...config, band: e.target.value })}
                >
                  <MenuItem value="2.4ghz">2.4 GHz</MenuItem>
                  <MenuItem value="5ghz">5 GHz</MenuItem>
                </Select>
              </FormControl>
            </Grid>
            <Grid size={{ xs: 12, md: 3 }}>
              <FormControl fullWidth size="small">
                <InputLabel>Mode</InputLabel>
                <Select
                  value={config.mode}
                  label="Mode"
                  onChange={(e) => setConfig({ ...config, mode: e.target.value })}
                >
                  <MenuItem value="bridge">Bridge</MenuItem>
                  <MenuItem value="ap">Standalone AP</MenuItem>
                </Select>
              </FormControl>
            </Grid>
            <Grid size={{ xs: 12, md: 3 }}>
              <TextField
                fullWidth
                label="Bridge"
                size="small"
                value={config.bridge}
                onChange={(e) => setConfig({ ...config, bridge: e.target.value })}
              />
            </Grid>
            <Grid size={{ xs: 12, md: 6 }}>
              <FormControlLabel
                control={
                  <Checkbox
                    checked={config.hidden}
                    onChange={(e) => setConfig({ ...config, hidden: e.target.checked })}
                  />
                }
                label="Hidden Network"
              />
              <FormControlLabel
                control={
                  <Checkbox
                    checked={config.enabled}
                    onChange={(e) => setConfig({ ...config, enabled: e.target.checked })}
                  />
                }
                label="Enable on save"
              />
            </Grid>
            <Grid size={{ xs: 12 }}>
              <Box sx={{ display: 'flex', gap: 1 }}>
                <Button
                  variant="contained"
                  onClick={handleConfigure}
                  disabled={isConfiguring}
                >
                  Save & Apply
                </Button>
                {editingInterface && (
                  <Button
                    variant="outlined"
                    onClick={() => {
                      setEditingInterface(null);
                      setConfig({
                        interface: '',
                        ssid: '',
                        password: '',
                        channel: 6,
                        auto_channel: false,
                        band: '2.4ghz',
                        mode: 'bridge',
                        bridge: 'br0',
                        hidden: false,
                        enabled: true,
                      });
                    }}
                  >
                    Cancel
                  </Button>
                )}
              </Box>
            </Grid>
          </Grid>
        </CardContent>
      </Card>

      {statusLoading ? (
        <CircularProgress />
      ) : statusData?.interfaces?.length ? (
        <Card>
          <CardContent>
            <Typography variant="h6" color="primary" gutterBottom>
              Active WiFi Interfaces
            </Typography>
            {statusData.interfaces.map((iface) => (
              <Box key={iface.interface} sx={{ mb: 3 }}>
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1, flexWrap: 'wrap' }}>
                  <Typography variant="subtitle1">
                    <code>{iface.interface}</code>
                  </Typography>
                  {iface.running ? (
                    <Chip label="Running" color="success" size="small" />
                  ) : (
                    <Chip label="Stopped" color="error" size="small" />
                  )}
                  {iface.ssid && (
                    <Chip label={iface.ssid} variant="outlined" size="small" />
                  )}
                </Box>
                <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: 'auto 1fr' }, gap: 0.5, mb: 2, fontSize: '0.875rem' }}>
                  <Typography color="text.secondary">Channel:</Typography>
                  <Typography>{iface.channel} ({iface.frequency} MHz)</Typography>
                  <Typography color="text.secondary">TX Power:</Typography>
                  <Typography>{iface.txPower} dBm</Typography>
                  <Typography color="text.secondary">Clients:</Typography>
                  <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                    <Typography>{iface.stationCount}</Typography>
                    {iface.stationCount > 0 && (
                      <IconButton
                        size="small"
                        onClick={() => toggleExpanded(iface.interface)}
                        sx={{ p: 0 }}
                      >
                        {expandedInterfaces.has(iface.interface) ? <ExpandLessIcon fontSize="small" /> : <ExpandMoreIcon fontSize="small" />}
                      </IconButton>
                    )}
                  </Box>
                </Box>
                <Collapse in={expandedInterfaces.has(iface.interface)}>
                  {stations[iface.interface]?.length > 0 && (
                    <Box sx={{ mb: 2 }}>
                      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 1 }}>
                        <DevicesIcon fontSize="small" color="primary" />
                        <Typography variant="subtitle2" color="primary">Connected Clients</Typography>
                      </Box>
                      <TableContainer sx={{ overflowX: 'auto' }}>
                        <Table size="small" sx={{ minWidth: 500 }}>
                          <TableHead>
                            <TableRow>
                              <TableCell>MAC Address</TableCell>
                              <TableCell align="right">Signal</TableCell>
                              <TableCell align="right">RX Rate</TableCell>
                              <TableCell align="right">TX Rate</TableCell>
                              <TableCell align="right">RX</TableCell>
                              <TableCell align="right">TX</TableCell>
                            </TableRow>
                          </TableHead>
                          <TableBody>
                            {stations[iface.interface].map((station) => {
                              const deviceName = getDeviceName(station.mac);
                              return (
                                <TableRow key={station.mac}>
                                  <TableCell>
                                    {deviceName ? (
                                      <>
                                        <Typography variant="body2">{deviceName}</Typography>
                                        <Typography variant="caption" color="text.secondary">
                                          <code>{station.mac.toUpperCase()}</code>
                                        </Typography>
                                      </>
                                    ) : (
                                      <code>{station.mac.toUpperCase()}</code>
                                    )}
                                  </TableCell>
                                  <TableCell align="right">{station.signal} dBm</TableCell>
                                  <TableCell align="right">{station.rxRate} Mbps</TableCell>
                                  <TableCell align="right">{station.txRate} Mbps</TableCell>
                                  <TableCell align="right">{formatBytes(station.rxBytes)}</TableCell>
                                  <TableCell align="right">{formatBytes(station.txBytes)}</TableCell>
                                </TableRow>
                              );
                            })}
                          </TableBody>
                        </Table>
                      </TableContainer>
                    </Box>
                  )}
                </Collapse>
                <Box sx={{ display: 'flex', gap: 1, flexWrap: 'wrap' }}>
                  {iface.running ? (
                    <>
                      <Button
                        size="small"
                        variant="outlined"
                        color="error"
                        startIcon={<StopIcon />}
                        onClick={() => handleControl(iface.interface, 'stop')}
                        disabled={isControlling}
                      >
                        Stop
                      </Button>
                      <Button
                        size="small"
                        variant="outlined"
                        color="warning"
                        startIcon={<RestartIcon />}
                        onClick={() => handleControl(iface.interface, 'restart')}
                        disabled={isControlling}
                      >
                        Restart
                      </Button>
                    </>
                  ) : (
                    <Button
                      size="small"
                      variant="outlined"
                      color="success"
                      startIcon={<StartIcon />}
                      onClick={() => handleControl(iface.interface, 'start')}
                      disabled={isControlling}
                    >
                      Start
                    </Button>
                  )}
                  <Button
                    size="small"
                    variant="outlined"
                    color="primary"
                    startIcon={<EditIcon />}
                    onClick={() => handleEdit(iface.interface)}
                  >
                    Edit
                  </Button>
                  <Button
                    size="small"
                    variant="outlined"
                    color="error"
                    startIcon={<DeleteIcon />}
                    onClick={() => handleDelete(iface.interface)}
                    disabled={isDeleting || iface.running}
                  >
                    Delete
                  </Button>
                </Box>
              </Box>
            ))}
          </CardContent>
        </Card>
      ) : (
        <Typography color="text.secondary">No WiFi interfaces configured</Typography>
      )}
    </Box>
  );
}

export const Route = createFileRoute('/wifi')({
  component: WiFiPage,
});
