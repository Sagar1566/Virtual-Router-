import { createFileRoute } from '@tanstack/react-router';
import { useState } from 'react';
import {
  Box,
  Card,
  CardContent,
  Typography,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Button,
  TextField,
  Chip,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  CircularProgress,
  Alert,
  RadioGroup,
  Radio,
  FormControlLabel,
  Tooltip,
  List,
  ListItem,
  ListItemButton,
  ListItemText,
  ListItemIcon,
  InputAdornment,
  IconButton,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Switch,
  Checkbox,
  Paper,
} from '@mui/material';
import Grid from '@mui/material/Grid';
import {
  Wifi as WifiIcon,
  WifiLock as WifiLockIcon,
  Refresh as RefreshIcon,
  Visibility,
  VisibilityOff,
  SignalWifi4Bar,
  SignalWifi3Bar,
  SignalWifi2Bar,
  SignalWifi1Bar,
  SignalWifi0Bar,
  Add as AddIcon,
  Edit as EditIcon,
  Delete as DeleteIcon,
  ArrowUpward as ArrowUpIcon,
  ArrowDownward as ArrowDownIcon,
  SwapVert as SwapIcon,
  CheckCircle as HealthyIcon,
  Error as UnhealthyIcon,
  Warning as WarningIcon,
} from '@mui/icons-material';
import { useQuery, useMutation } from '../api/hooks';
import type { ScannedWiFiNetwork, WANConfigItem, WANStatusItem, InterfaceStats } from '../api/client';
import { PendingConfirmBanner } from '../components/PendingConfirmBanner';

function getSignalIcon(quality: number) {
  if (quality >= 80) return <SignalWifi4Bar color="success" />;
  if (quality >= 60) return <SignalWifi3Bar color="success" />;
  if (quality >= 40) return <SignalWifi2Bar color="warning" />;
  if (quality >= 20) return <SignalWifi1Bar color="warning" />;
  return <SignalWifi0Bar color="error" />;
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}

function WANPage() {
  const { data: wansData, isLoading: wansLoading, refetch } = useQuery('getWANs');
  const { data: interfacesData } = useQuery('listInterfaces');
  const { data: wifiCardsData } = useQuery('getWiFiCards');
  const { mutate: addWAN, isLoading: isAdding } = useMutation('addWAN');
  const { mutate: updateWAN, isLoading: isUpdating } = useMutation('updateWAN');
  const { mutate: deleteWAN, isLoading: isDeleting } = useMutation('deleteWAN');
  const { mutate: reorderWANs, isLoading: isReordering } = useMutation('reorderWANs');
  const { mutate: switchWAN, isLoading: isSwitching } = useMutation('switchMultiWAN');
  const { mutate: scanWiFi, isLoading: isScanning } = useMutation('scanWiFiNetworks');
  const { mutate: autoDetect, isLoading: isAutoDetecting } = useMutation('autoDetectWANs');
  const { mutate: refreshDHCP, isLoading: isRefreshingDHCP } = useMutation('refreshWANDHCP');

  const [editingWAN, setEditingWAN] = useState<WANConfigItem | null>(null);
  const [isDialogOpen, setIsDialogOpen] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  const [wifiNetworks, setWifiNetworks] = useState<ScannedWiFiNetwork[]>([]);
  const [showPassword, setShowPassword] = useState(false);

  // Dialog form state
  const [formData, setFormData] = useState<Partial<WANConfigItem>>({
    name: '',
    interface: '',
    enabled: true,
    mode: 'dhcp',
    priority: 1,
    health_check_interval: 10,
    health_check_timeout: 5,
    health_check_retries: 3,
    health_check_targets: ['8.8.8.8', '1.1.1.1'],
  });

  // Get all available interfaces
  const ethernetInterfaces = interfacesData?.interfaces?.filter(
    (iface) => iface.type === 'ethernet' && iface.name !== 'lo'
  ) || [];
  const wifiInterfaces = wifiCardsData?.cards?.map(card => card.interface) || [];
  const allInterfaces = [...ethernetInterfaces.map(i => i.name), ...wifiInterfaces];

  // Find status for a WAN by interface
  const getWANStatus = (iface: string): WANStatusItem | undefined => {
    return wansData?.status?.wans?.find(w => w.interface === iface);
  };

  const openAddDialog = () => {
    setEditingWAN(null);
    setFormData({
      name: '',
      interface: allInterfaces[0] || '',
      enabled: true,
      mode: 'dhcp',
      priority: (wansData?.wans?.length || 0) + 1,
      health_check_interval: 10,
      health_check_timeout: 5,
      health_check_retries: 3,
      health_check_targets: ['8.8.8.8', '1.1.1.1'],
    });
    setWifiNetworks([]);
    setIsDialogOpen(true);
  };

  const openEditDialog = (wan: WANConfigItem) => {
    setEditingWAN(wan);
    setFormData({
      ...wan,
      health_check_targets: wan.health_check_targets || ['8.8.8.8', '1.1.1.1'],
    });
    setWifiNetworks([]);
    setIsDialogOpen(true);
  };

  const handleCloseDialog = () => {
    setIsDialogOpen(false);
    setEditingWAN(null);
    setShowPassword(false);
  };

  const handleSaveWAN = async () => {
    if (!formData.interface) {
      setError('Interface is required');
      return;
    }

    try {
      if (editingWAN?.id) {
        await updateWAN({ id: editingWAN.id, wan: formData });
        setSuccess(`WAN "${formData.name || formData.interface}" updated`);
      } else {
        await addWAN(formData as Omit<WANConfigItem, 'id'>);
        setSuccess(`WAN "${formData.name || formData.interface}" added`);
      }
      setError(null);
      handleCloseDialog();
      refetch();
    } catch (e) {
      setError(String(e));
    }
  };

  const handleDeleteWAN = async (wan: WANConfigItem) => {
    if (!wan.id) return;
    if (!confirm(`Delete WAN "${wan.name || wan.interface}"?`)) return;

    try {
      await deleteWAN(wan.id);
      setSuccess(`WAN "${wan.name || wan.interface}" deleted`);
      setError(null);
      refetch();
    } catch (e) {
      setError(String(e));
    }
  };

  const handleMoveWAN = async (index: number, direction: 'up' | 'down') => {
    if (!wansData?.wans) return;
    const wans = [...wansData.wans];
    const newIndex = direction === 'up' ? index - 1 : index + 1;
    if (newIndex < 0 || newIndex >= wans.length) return;

    // Swap
    [wans[index], wans[newIndex]] = [wans[newIndex], wans[index]];

    try {
      const order = wans.map(w => w.id!).filter(Boolean);
      await reorderWANs({ order });
      setSuccess('WAN order updated');
      setError(null);
      refetch();
    } catch (e) {
      setError(String(e));
    }
  };

  const handleSwitchToWAN = async (wan: WANConfigItem) => {
    try {
      await switchWAN({ interface: wan.interface });
      setSuccess(`Switched to "${wan.name || wan.interface}"`);
      setError(null);
      refetch();
    } catch (e) {
      setError(String(e));
    }
  };

  const handleScanWiFi = async () => {
    try {
      const iface = formData.interface || wifiInterfaces[0];
      if (!iface) {
        setError('No WiFi interface available');
        return;
      }
      const result = await scanWiFi({ interface: iface });
      setWifiNetworks(result.networks || []);
      setFormData(prev => ({ ...prev, interface: result.interface }));
      setError(null);
    } catch (e) {
      setError(String(e));
    }
  };

  const handleSelectNetwork = (network: ScannedWiFiNetwork) => {
    setFormData(prev => ({
      ...prev,
      wifi_ssid: network.ssid,
      wifi_security: network.security,
      wifi_password: network.security === 'open' ? '' : prev.wifi_password,
    }));
  };

  const handleAutoDetect = async () => {
    try {
      const result = await autoDetect(undefined);
      if (result.added?.length > 0) {
        setSuccess(`Auto-detected ${result.added.length} WAN interface(s)`);
      } else {
        setSuccess('No new WAN interfaces detected');
      }
      setError(null);
      refetch();
    } catch (e) {
      setError(String(e));
    }
  };

  const handleRefreshDHCP = async (wan: WANConfigItem) => {
    if (!wan.id) return;
    try {
      const result = await refreshDHCP(wan.id);
      setSuccess(result.message);
      setError(null);
      refetch();
    } catch (e) {
      setError(String(e));
    }
  };

  // Get interface stats
  const getInterfaceStats = (iface: string): InterfaceStats | undefined => {
    return wansData?.interface_stats?.[iface];
  };

  // Get interface routes
  const getInterfaceRoutes = (iface: string): string[] => {
    return wansData?.interface_routes?.[iface] || [];
  };

  // Get interface DNS
  const getInterfaceDNS = (iface: string): string[] => {
    return wansData?.interface_dns?.[iface] || [];
  };

  const isWiFiMode = formData.mode === 'wifi';
  const hasWiFiInterfaces = wifiInterfaces.length > 0;

  return (
    <Box>
      <Typography variant="h4" gutterBottom>
        WAN Configuration
      </Typography>

      {error && <Alert severity="error" sx={{ mb: 2 }} onClose={() => setError(null)}>{error}</Alert>}
      {success && <Alert severity="success" sx={{ mb: 2 }} onClose={() => setSuccess(null)}>{success}</Alert>}

      {/* Pending Configuration Banner */}
      <PendingConfirmBanner
        onConfirmed={() => {
          setSuccess('Configuration confirmed and saved.');
          refetch();
        }}
        onCancelled={() => {
          setSuccess('Configuration rolled back to previous state.');
          refetch();
        }}
      />

      {/* WAN List */}
      <Card sx={{ mb: 3 }}>
        <CardContent>
          <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2 }}>
            <Typography variant="h6" color="primary">
              WAN Interfaces
              {wansData?.status?.mode && (
                <Chip
                  label={wansData.status.mode === 'loadbalance (WIP)' ? 'Load Balance (WIP)' : 'Failover'}
                  size="small"
                  color={wansData.status.mode.includes('loadbalance') ? 'warning' : 'primary'}
                  sx={{ ml: 2 }}
                />
              )}
            </Typography>
            <Box>
              <Button
                variant="outlined"
                startIcon={isAutoDetecting ? <CircularProgress size={20} /> : <RefreshIcon />}
                onClick={handleAutoDetect}
                disabled={isAutoDetecting}
                sx={{ mr: 1 }}
              >
                {isAutoDetecting ? 'Detecting...' : 'Auto-Detect'}
              </Button>
              <Button
                variant="outlined"
                startIcon={<RefreshIcon />}
                onClick={() => refetch()}
                sx={{ mr: 1 }}
              >
                Refresh
              </Button>
              <Button
                variant="contained"
                startIcon={<AddIcon />}
                onClick={openAddDialog}
              >
                Add WAN
              </Button>
            </Box>
          </Box>

          {wansLoading || isAutoDetecting ? (
            <CircularProgress />
          ) : wansData?.wans?.length === 0 ? (
            <Box sx={{ textAlign: 'center', py: 4 }}>
              <Typography color="text.secondary" gutterBottom>
                No WAN interfaces configured.
              </Typography>
              <Button
                variant="contained"
                onClick={handleAutoDetect}
                disabled={isAutoDetecting}
                sx={{ mt: 2 }}
              >
                Auto-Detect WAN Interfaces
              </Button>
            </Box>
          ) : (
            <TableContainer sx={{ overflowX: 'auto' }}>
              <Table size="small" sx={{ minWidth: 800 }}>
                <TableHead>
                  <TableRow>
                    <TableCell>Priority</TableCell>
                    <TableCell>Name</TableCell>
                    <TableCell>Interface</TableCell>
                    <TableCell>Mode</TableCell>
                    <TableCell>Status</TableCell>
                    <TableCell>IP / Gateway</TableCell>
                    <TableCell>RX / TX</TableCell>
                    <TableCell>Health</TableCell>
                    <TableCell align="right">Actions</TableCell>
                  </TableRow>
                </TableHead>
              <TableBody>
                {wansData?.wans?.map((wan, index) => {
                  const status = getWANStatus(wan.interface);
                  const isActive = wansData.status?.active_wan === wan.interface;
                  return (
                    <TableRow key={wan.id || index} sx={{ bgcolor: isActive ? 'action.selected' : 'inherit' }}>
                      <TableCell>
                        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
                          <IconButton
                            size="small"
                            onClick={() => handleMoveWAN(index, 'up')}
                            disabled={index === 0 || isReordering}
                          >
                            <ArrowUpIcon fontSize="small" />
                          </IconButton>
                          <Typography>{wan.priority}</Typography>
                          <IconButton
                            size="small"
                            onClick={() => handleMoveWAN(index, 'down')}
                            disabled={index === (wansData?.wans?.length || 0) - 1 || isReordering}
                          >
                            <ArrowDownIcon fontSize="small" />
                          </IconButton>
                        </Box>
                      </TableCell>
                      <TableCell>
                        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                          {wan.name || wan.interface}
                          {isActive && <Chip label="Active" color="success" size="small" />}
                          {wan.fuse_with_next && (
                            <Tooltip title="Load balancing with next WAN (WIP)">
                              <Chip label="Fused" color="warning" size="small" variant="outlined" />
                            </Tooltip>
                          )}
                        </Box>
                      </TableCell>
                      <TableCell>
                        <code>{wan.interface}</code>
                      </TableCell>
                      <TableCell>
                        <Chip
                          label={(wan.mode || 'dhcp').toUpperCase()}
                          size="small"
                          variant="outlined"
                          icon={wan.mode === 'wifi' ? <WifiIcon /> : undefined}
                        />
                      </TableCell>
                      <TableCell>
                        {!wan.enabled ? (
                          <Chip label="Disabled" color="default" size="small" />
                        ) : status?.up ? (
                          <Chip label="Up" color="success" size="small" />
                        ) : (
                          <Chip label="Down" color="error" size="small" />
                        )}
                      </TableCell>
                      <TableCell>
                        {(() => {
                          const routes = getInterfaceRoutes(wan.interface);
                          const dns = getInterfaceDNS(wan.interface);
                          const routeTooltip = routes.length > 0
                            ? routes.join('\n')
                            : 'No routes';

                          return status?.ip_address ? (
                            <Tooltip title={<pre style={{ margin: 0, fontSize: '11px' }}>{routeTooltip}</pre>}>
                              <Box>
                                <Typography variant="body2">{status.ip_address}</Typography>
                                <Typography variant="caption" color="text.secondary">
                                  via {status.gateway || 'N/A'}
                                </Typography>
                                {dns.length > 0 && (
                                  <Typography variant="caption" color="text.secondary" display="block">
                                    DNS: {dns.join(', ')}
                                  </Typography>
                                )}
                              </Box>
                            </Tooltip>
                          ) : (
                            <Typography color="text.secondary">-</Typography>
                          );
                        })()}
                      </TableCell>
                      <TableCell>
                        {(() => {
                          const stats = getInterfaceStats(wan.interface);
                          return stats ? (
                            <Box>
                              <Typography variant="body2">↓ {formatBytes(stats.rx_bytes)}</Typography>
                              <Typography variant="body2">↑ {formatBytes(stats.tx_bytes)}</Typography>
                            </Box>
                          ) : (
                            <Typography color="text.secondary">-</Typography>
                          );
                        })()}
                      </TableCell>
                      <TableCell>
                        {status ? (
                          <Tooltip title={`${status.checks_passed}/${status.checks_run} checks passed`}>
                            <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
                              {status.healthy ? (
                                <HealthyIcon color="success" fontSize="small" />
                              ) : status.failure_count > 0 ? (
                                <UnhealthyIcon color="error" fontSize="small" />
                              ) : (
                                <WarningIcon color="warning" fontSize="small" />
                              )}
                              <Typography variant="body2">
                                {status.healthy ? 'Healthy' : `${status.failure_count} failures`}
                              </Typography>
                            </Box>
                          </Tooltip>
                        ) : (
                          <Typography color="text.secondary">-</Typography>
                        )}
                      </TableCell>
                      <TableCell align="right">
                        <Box sx={{ display: 'flex', gap: 0.5, justifyContent: 'flex-end' }}>
                          {(wan.mode === 'dhcp' || !wan.mode) && (
                            <Tooltip title="Refresh DHCP lease">
                              <IconButton
                                size="small"
                                onClick={() => handleRefreshDHCP(wan)}
                                disabled={isRefreshingDHCP}
                              >
                                <RefreshIcon fontSize="small" />
                              </IconButton>
                            </Tooltip>
                          )}
                          {!isActive && wan.enabled && status?.healthy && (
                            <Tooltip title="Force switch to this WAN">
                              <IconButton
                                size="small"
                                onClick={() => handleSwitchToWAN(wan)}
                                disabled={isSwitching}
                                color="primary"
                              >
                                <SwapIcon fontSize="small" />
                              </IconButton>
                            </Tooltip>
                          )}
                          <IconButton
                            size="small"
                            onClick={() => openEditDialog(wan)}
                          >
                            <EditIcon fontSize="small" />
                          </IconButton>
                          <IconButton
                            size="small"
                            onClick={() => handleDeleteWAN(wan)}
                            disabled={isDeleting}
                            color="error"
                          >
                            <DeleteIcon fontSize="small" />
                          </IconButton>
                        </Box>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
              </Table>
            </TableContainer>
          )}

          {/* Failback delay info */}
          {wansData?.failback_delay != null && wansData.failback_delay > 0 && (
            <Typography variant="caption" color="text.secondary" sx={{ mt: 2, display: 'block' }}>
              Failback delay: {wansData.failback_delay} seconds
            </Typography>
          )}
        </CardContent>
      </Card>

      {/* Add/Edit WAN Dialog */}
      <Dialog open={isDialogOpen} onClose={handleCloseDialog} maxWidth="md" fullWidth>
        <DialogTitle>
          {editingWAN ? `Edit WAN: ${editingWAN.name || editingWAN.interface}` : 'Add New WAN'}
        </DialogTitle>
        <DialogContent>
          <Box sx={{ pt: 2 }}>
            <Grid container spacing={2}>
              {/* Name */}
              <Grid size={{ xs: 12, md: 6 }}>
                <TextField
                  fullWidth
                  label="Name"
                  size="small"
                  value={formData.name || ''}
                  onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                  placeholder="Primary WAN"
                />
              </Grid>

              {/* Interface Selection */}
              <Grid size={{ xs: 12, md: 6 }}>
                <FormControl fullWidth size="small">
                  <InputLabel>Interface</InputLabel>
                  <Select
                    value={formData.interface || ''}
                    label="Interface"
                    onChange={(e) => {
                      const iface = e.target.value;
                      const isWifi = wifiInterfaces.includes(iface);
                      setFormData({
                        ...formData,
                        interface: iface,
                        mode: isWifi ? 'wifi' : formData.mode === 'wifi' ? 'dhcp' : formData.mode,
                      });
                    }}
                  >
                    {allInterfaces.map((iface) => (
                      <MenuItem key={iface} value={iface}>
                        {iface}
                        {wifiInterfaces.includes(iface) && ' (WiFi)'}
                      </MenuItem>
                    ))}
                  </Select>
                </FormControl>
              </Grid>

              {/* Enabled */}
              <Grid size={12}>
                <FormControlLabel
                  control={
                    <Switch
                      checked={formData.enabled ?? true}
                      onChange={(e) => setFormData({ ...formData, enabled: e.target.checked })}
                    />
                  }
                  label="Enabled"
                />
              </Grid>

              {/* Mode Selection */}
              <Grid size={12}>
                <Typography variant="subtitle2" gutterBottom>
                  Connection Mode
                </Typography>
                <FormControl>
                  <RadioGroup
                    row
                    value={formData.mode || 'dhcp'}
                    onChange={(e) => setFormData({ ...formData, mode: e.target.value as 'dhcp' | 'static' | 'wifi' })}
                  >
                    <FormControlLabel value="dhcp" control={<Radio />} label="DHCP" />
                    <FormControlLabel value="static" control={<Radio />} label="Static IP" />
                    <Tooltip title={hasWiFiInterfaces ? "Connect via WiFi" : "No WiFi interfaces detected"}>
                      <FormControlLabel
                        value="wifi"
                        control={<Radio />}
                        label={<Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}><WifiIcon fontSize="small" /> WiFi</Box>}
                        disabled={!hasWiFiInterfaces || !wifiInterfaces.includes(formData.interface || '')}
                      />
                    </Tooltip>
                  </RadioGroup>
                </FormControl>
              </Grid>

              {/* Static IP Fields */}
              {formData.mode === 'static' && (
                <>
                  <Grid size={{ xs: 12, md: 4 }}>
                    <TextField
                      fullWidth
                      label="IP Address"
                      size="small"
                      value={formData.static_ip || ''}
                      onChange={(e) => setFormData({ ...formData, static_ip: e.target.value })}
                      placeholder="192.168.1.2/24"
                      helperText="Include CIDR notation"
                    />
                  </Grid>
                  <Grid size={{ xs: 12, md: 4 }}>
                    <TextField
                      fullWidth
                      label="Gateway"
                      size="small"
                      value={formData.static_gateway || ''}
                      onChange={(e) => setFormData({ ...formData, static_gateway: e.target.value })}
                      placeholder="192.168.1.1"
                    />
                  </Grid>
                  <Grid size={{ xs: 12, md: 4 }}>
                    <TextField
                      fullWidth
                      label="DNS Servers"
                      size="small"
                      value={formData.static_dns || ''}
                      onChange={(e) => setFormData({ ...formData, static_dns: e.target.value })}
                      placeholder="8.8.8.8,8.8.4.4"
                    />
                  </Grid>
                </>
              )}

              {/* WiFi Fields */}
              {isWiFiMode && (
                <>
                  <Grid size={{ xs: 12, md: 6 }}>
                    <Button
                      variant="outlined"
                      startIcon={isScanning ? <CircularProgress size={20} /> : <RefreshIcon />}
                      onClick={handleScanWiFi}
                      disabled={isScanning}
                      fullWidth
                    >
                      {isScanning ? 'Scanning...' : 'Scan Networks'}
                    </Button>
                  </Grid>
                  <Grid size={6} />

                  {wifiNetworks.length > 0 && (
                    <Grid size={12}>
                      <Typography variant="subtitle2" gutterBottom>
                        Available Networks
                      </Typography>
                      <List dense sx={{ maxHeight: 200, overflow: 'auto', border: 1, borderColor: 'divider', borderRadius: 1 }}>
                        {wifiNetworks.map((network, idx) => (
                          <ListItem key={`${network.bssid}-${idx}`} disablePadding>
                            <ListItemButton
                              selected={formData.wifi_ssid === network.ssid}
                              onClick={() => handleSelectNetwork(network)}
                            >
                              <ListItemIcon>
                                {network.security === 'open' ? <WifiIcon /> : <WifiLockIcon />}
                              </ListItemIcon>
                              <ListItemText
                                primary={network.ssid || '(Hidden)'}
                                secondary={`${network.security.toUpperCase()} - ${network.band}`}
                              />
                              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                                {getSignalIcon(network.quality)}
                                <Typography variant="body2">{network.quality}%</Typography>
                              </Box>
                            </ListItemButton>
                          </ListItem>
                        ))}
                      </List>
                    </Grid>
                  )}

                  <Grid size={{ xs: 12, md: 6 }}>
                    <TextField
                      fullWidth
                      label="Network Name (SSID)"
                      size="small"
                      value={formData.wifi_ssid || ''}
                      onChange={(e) => setFormData({ ...formData, wifi_ssid: e.target.value })}
                    />
                  </Grid>
                  <Grid size={{ xs: 12, md: 6 }}>
                    <FormControl fullWidth size="small">
                      <InputLabel>Security</InputLabel>
                      <Select
                        value={formData.wifi_security || 'wpa2'}
                        label="Security"
                        onChange={(e) => setFormData({ ...formData, wifi_security: e.target.value })}
                      >
                        <MenuItem value="open">Open</MenuItem>
                        <MenuItem value="wep">WEP</MenuItem>
                        <MenuItem value="wpa">WPA</MenuItem>
                        <MenuItem value="wpa2">WPA2</MenuItem>
                        <MenuItem value="wpa3">WPA3</MenuItem>
                      </Select>
                    </FormControl>
                  </Grid>
                  {formData.wifi_security !== 'open' && (
                    <Grid size={12}>
                      <TextField
                        fullWidth
                        label="Password"
                        size="small"
                        type={showPassword ? 'text' : 'password'}
                        value={formData.wifi_password || ''}
                        onChange={(e) => setFormData({ ...formData, wifi_password: e.target.value })}
                        InputProps={{
                          endAdornment: (
                            <InputAdornment position="end">
                              <IconButton onClick={() => setShowPassword(!showPassword)} edge="end">
                                {showPassword ? <VisibilityOff /> : <Visibility />}
                              </IconButton>
                            </InputAdornment>
                          ),
                        }}
                      />
                    </Grid>
                  )}
                </>
              )}

              {/* Health Check Settings */}
              <Grid size={12}>
                <Typography variant="subtitle2" gutterBottom sx={{ mt: 2 }}>
                  Health Check Settings
                </Typography>
              </Grid>
              <Grid size={{ xs: 12, md: 4 }}>
                <TextField
                  fullWidth
                  label="Check Interval (seconds)"
                  size="small"
                  type="number"
                  value={formData.health_check_interval || 10}
                  onChange={(e) => setFormData({ ...formData, health_check_interval: parseInt(e.target.value) || 10 })}
                />
              </Grid>
              <Grid size={{ xs: 12, md: 4 }}>
                <TextField
                  fullWidth
                  label="Timeout (seconds)"
                  size="small"
                  type="number"
                  value={formData.health_check_timeout || 5}
                  onChange={(e) => setFormData({ ...formData, health_check_timeout: parseInt(e.target.value) || 5 })}
                />
              </Grid>
              <Grid size={{ xs: 12, md: 4 }}>
                <TextField
                  fullWidth
                  label="Retries before failover"
                  size="small"
                  type="number"
                  value={formData.health_check_retries || 3}
                  onChange={(e) => setFormData({ ...formData, health_check_retries: parseInt(e.target.value) || 3 })}
                />
              </Grid>
              <Grid size={12}>
                <TextField
                  fullWidth
                  label="Health Check Targets"
                  size="small"
                  value={(formData.health_check_targets || []).join(', ')}
                  onChange={(e) => setFormData({
                    ...formData,
                    health_check_targets: e.target.value.split(',').map(s => s.trim()).filter(Boolean)
                  })}
                  helperText="Comma-separated IP addresses to ping"
                />
              </Grid>

              {/* Load Balancing (WIP) */}
              <Grid size={12}>
                <Paper variant="outlined" sx={{ p: 2, bgcolor: 'action.hover' }}>
                  <FormControlLabel
                    control={
                      <Checkbox
                        checked={formData.fuse_with_next ?? false}
                        onChange={(e) => setFormData({ ...formData, fuse_with_next: e.target.checked })}
                      />
                    }
                    label={
                      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                        Fuse with next WAN (Load Balancing)
                        <Chip label="WIP" size="small" color="warning" />
                      </Box>
                    }
                  />
                  <Typography variant="caption" color="text.secondary" display="block">
                    When enabled, traffic will be load-balanced between this WAN and the next one in the list.
                    This feature is a work in progress.
                  </Typography>
                </Paper>
              </Grid>
            </Grid>
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={handleCloseDialog}>Cancel</Button>
          <Button
            variant="contained"
            onClick={handleSaveWAN}
            disabled={isAdding || isUpdating || !formData.interface}
          >
            {isAdding || isUpdating ? 'Saving...' : editingWAN ? 'Update' : 'Add'}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}

export const Route = createFileRoute('/wan')({
  component: WANPage,
});
