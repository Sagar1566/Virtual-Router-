import { createFileRoute, Link } from '@tanstack/react-router';
import {
  Box,
  Card,
  CardContent,
  Typography,
  TextField,
  Button,
  Grid,
  Alert,
  Chip,
  Switch,
  FormControlLabel,
  Divider,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  IconButton,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
} from '@mui/material';
import {
  Refresh as RefreshIcon,
  Notifications as NotificationsIcon,
  Send as SendIcon,
  Edit as EditIcon,
  Delete as DeleteIcon,
  PowerSettingsNew as PowerIcon,
  RestartAlt as RestartIcon,
} from '@mui/icons-material';
import { useState, useEffect } from 'react';
import { useQuery, useMutation } from '../api/hooks';
import { client, type LogEntry, type Device } from '../api/client';

function SettingsPage() {
  const { data: configData, isLoading } = useQuery('getConfig');
  const { mutate: updateConfig, isLoading: isUpdating } = useMutation('updateConfig');
  const { data: logsData, refetch: refetchLogs } = useQuery('getSystemLogs');
  const { data: notificationSettings, refetch: refetchNotifications } = useQuery('getNotificationSettings');
  const { data: devicesData, refetch: refetchDevices } = useQuery('listDevices');
  const { mutate: updateNotifications, isLoading: isUpdatingNotifications } = useMutation('updateNotificationSettings');
  const [isTesting, setIsTesting] = useState(false);

  const [webListenAddresses, setWebListenAddresses] = useState<string[]>(
    configData?.webListenAddresses || (configData?.listenAddr ? [configData.listenAddr] : [])
  );
  const [newListenAddr, setNewListenAddr] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  // Notification settings state
  const [ntfyEnabled, setNtfyEnabled] = useState(false);
  const [ntfyServer, setNtfyServer] = useState('https://ntfy.sh');
  const [ntfyTopic, setNtfyTopic] = useState('');
  const [ntfyToken, setNtfyToken] = useState('');
  const [notifyWifi, setNotifyWifi] = useState(true);
  const [notifyHealth, setNotifyHealth] = useState(true);
  const [notifyWan, setNotifyWan] = useState(true);
  const [notifyVpn, setNotifyVpn] = useState(true);
  const [notifySystem, setNotifySystem] = useState(true);

  // Device edit dialog state
  const [editDevice, setEditDevice] = useState<Device | null>(null);
  const [editDisplayName, setEditDisplayName] = useState('');
  const [editNotes, setEditNotes] = useState('');

  // Shutdown/Reboot state
  const [shutdownDialogOpen, setShutdownDialogOpen] = useState(false);
  const [rebootDialogOpen, setRebootDialogOpen] = useState(false);
  const [isShuttingDown, setIsShuttingDown] = useState(false);
  const [isRebooting, setIsRebooting] = useState(false);

  // Sync config settings from server
  useEffect(() => {
    if (configData) {
      setWebListenAddresses(
        configData.webListenAddresses || (configData.listenAddr ? [configData.listenAddr] : [])
      );
    }
  }, [configData]);

  // Sync notification settings from server
  useEffect(() => {
    if (notificationSettings) {
      setNtfyEnabled(notificationSettings.enabled);
      setNtfyServer(notificationSettings.server || 'https://ntfy.sh');
      setNtfyTopic(notificationSettings.topic || '');
      setNotifyWifi(notificationSettings.notify_wifi_clients);
      setNotifyHealth(notificationSettings.notify_health_checks);
      setNotifyWan(notificationSettings.notify_wan_changes);
      setNotifyVpn(notificationSettings.notify_vpn_peers);
      setNotifySystem(notificationSettings.notify_system_events);
    }
  }, [notificationSettings]);

  const getLevelColor = (level: string) => {
    switch (level) {
      case 'ERROR': return 'error';
      case 'WARN': return 'warning';
      case 'INFO': return 'info';
      case 'DEBUG': return 'default';
      default: return 'default';
    }
  };

  const handleSave = async () => {
    try {
      await updateConfig({ webListenAddresses });
      setSuccess('Settings saved. Restart required for changes to take effect.');
      setError(null);
    } catch (e) {
      setError(String(e));
    }
  };

  const handleAddListenAddr = () => {
    const addr = newListenAddr.trim();
    if (!addr) return;
    // Validate format: should be IP:port or :port
    if (!/^(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})?:\d+$/.test(addr)) {
      setError('Invalid address format. Use IP:port (e.g., 192.168.1.1:8080) or :port (e.g., :8080)');
      return;
    }
    if (webListenAddresses.includes(addr)) {
      setError('Address already in list');
      return;
    }
    setWebListenAddresses([...webListenAddresses, addr]);
    setNewListenAddr('');
    setError(null);
  };

  const handleRemoveListenAddr = (addr: string) => {
    setWebListenAddresses(webListenAddresses.filter(a => a !== addr));
  };

  const handleSaveNotifications = async () => {
    try {
      await updateNotifications({
        enabled: ntfyEnabled,
        server: ntfyServer,
        topic: ntfyTopic,
        token: ntfyToken || undefined,
        notify_wifi_clients: notifyWifi,
        notify_health_checks: notifyHealth,
        notify_wan_changes: notifyWan,
        notify_vpn_peers: notifyVpn,
        notify_system_events: notifySystem,
      });
      setSuccess('Notification settings saved');
      setError(null);
      refetchNotifications();
    } catch (e) {
      setError(String(e));
    }
  };

  const handleTestNotification = async () => {
    setIsTesting(true);
    try {
      await client.testNotification();
      setSuccess('Test notification sent');
      setError(null);
    } catch (e) {
      setError(String(e));
    } finally {
      setIsTesting(false);
    }
  };

  const handleEditDevice = (device: Device) => {
    setEditDevice(device);
    setEditDisplayName(device.display_name || '');
    setEditNotes(device.notes || '');
  };

  const handleSaveDevice = async () => {
    if (!editDevice) return;
    try {
      await client.updateDevice(editDevice.mac, { display_name: editDisplayName, notes: editNotes });
      setSuccess('Device updated');
      setEditDevice(null);
      refetchDevices();
    } catch (e) {
      setError(String(e));
    }
  };

  const handleDeleteDevice = async (mac: string) => {
    if (!confirm('Delete this device from history?')) return;
    try {
      await client.deleteDevice(mac);
      setSuccess('Device deleted');
      refetchDevices();
    } catch (e) {
      setError(String(e));
    }
  };

  const formatTimestamp = (ts: number) => {
    if (!ts) return 'Never';
    return new Date(ts * 1000).toLocaleString();
  };

  const handleShutdown = async () => {
    setIsShuttingDown(true);
    try {
      await client.systemShutdown();
      setSuccess('System is shutting down...');
      setShutdownDialogOpen(false);
    } catch (e) {
      setError(String(e));
      setIsShuttingDown(false);
    }
  };

  const handleReboot = async () => {
    setIsRebooting(true);
    try {
      await client.systemReboot();
      setSuccess('System is rebooting...');
      setRebootDialogOpen(false);
    } catch (e) {
      setError(String(e));
      setIsRebooting(false);
    }
  };

  return (
    <Box>
      <Typography variant="h4" gutterBottom>
        Settings
      </Typography>

      {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}
      {success && <Alert severity="success" sx={{ mb: 2 }}>{success}</Alert>}

      <Grid container spacing={3}>
        <Grid size={{ xs: 12, md: 6 }}>
          <Card>
            <CardContent>
              <Typography variant="h6" color="primary" gutterBottom>
                Web Interface Listen Addresses
              </Typography>
              <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                Configure which addresses the web UI listens on. If empty, defaults to LAN addresses on port 8080.
              </Typography>
              <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                {webListenAddresses.length > 0 ? (
                  <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 1 }}>
                    {webListenAddresses.map((addr) => (
                      <Chip
                        key={addr}
                        label={addr}
                        onDelete={() => handleRemoveListenAddr(addr)}
                        sx={{ fontFamily: 'monospace' }}
                      />
                    ))}
                  </Box>
                ) : (
                  <Typography variant="body2" color="text.secondary" sx={{ fontStyle: 'italic' }}>
                    No addresses configured (using default: LAN IPs on port 8080)
                  </Typography>
                )}
                <Box sx={{ display: 'flex', gap: 1 }}>
                  <TextField
                    label="Add Address"
                    size="small"
                    value={newListenAddr}
                    onChange={(e) => setNewListenAddr(e.target.value)}
                    placeholder="192.168.1.1:8080 or :8080"
                    helperText="IP:port or :port for all interfaces"
                    onKeyDown={(e) => e.key === 'Enter' && handleAddListenAddr()}
                    sx={{ flexGrow: 1 }}
                  />
                  <Button
                    variant="outlined"
                    onClick={handleAddListenAddr}
                    disabled={!newListenAddr.trim()}
                  >
                    Add
                  </Button>
                </Box>
                <Button
                  variant="contained"
                  onClick={handleSave}
                  disabled={isUpdating || isLoading}
                >
                  Save Settings
                </Button>
              </Box>
            </CardContent>
          </Card>
        </Grid>

        <Grid size={{ xs: 12, md: 6 }}>
          <Card>
            <CardContent>
              <Typography variant="h6" color="primary" gutterBottom>
                System Actions
              </Typography>
              <Box sx={{ display: 'flex', gap: 2, flexWrap: 'wrap' }}>
                <Button variant="outlined" component={Link} to="/">
                  Dashboard
                </Button>
                <Button
                  variant="outlined"
                  color="warning"
                  startIcon={<RestartIcon />}
                  onClick={() => setRebootDialogOpen(true)}
                  disabled={isRebooting}
                >
                  Reboot
                </Button>
                <Button
                  variant="outlined"
                  color="error"
                  startIcon={<PowerIcon />}
                  onClick={() => setShutdownDialogOpen(true)}
                  disabled={isShuttingDown}
                >
                  Shutdown
                </Button>
              </Box>
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>
                Safely power off or restart the system
              </Typography>
            </CardContent>
          </Card>
        </Grid>

        {/* Notifications Section */}
        <Grid size={12}>
          <Card>
            <CardContent>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 2 }}>
                <NotificationsIcon color="primary" />
                <Typography variant="h6" color="primary">
                  Push Notifications (ntfy.sh)
                </Typography>
              </Box>
              <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                Get push notifications for router events via ntfy.sh. You can use the public server or self-host.
              </Typography>

              <Grid container spacing={2}>
                <Grid size={{ xs: 12, sm: 6, md: 4 }}>
                  <FormControlLabel
                    control={
                      <Switch
                        checked={ntfyEnabled}
                        onChange={(e) => setNtfyEnabled(e.target.checked)}
                      />
                    }
                    label="Enable Notifications"
                  />
                </Grid>
                <Grid size={{ xs: 12, sm: 6, md: 4 }}>
                  <TextField
                    label="Server URL"
                    size="small"
                    fullWidth
                    value={ntfyServer}
                    onChange={(e) => setNtfyServer(e.target.value)}
                    placeholder="https://ntfy.sh"
                    disabled={!ntfyEnabled}
                  />
                </Grid>
                <Grid size={{ xs: 12, sm: 6, md: 4 }}>
                  <TextField
                    label="Topic"
                    size="small"
                    fullWidth
                    value={ntfyTopic}
                    onChange={(e) => setNtfyTopic(e.target.value)}
                    placeholder="my-router-alerts"
                    helperText="Unique topic name for your notifications"
                    disabled={!ntfyEnabled}
                  />
                </Grid>
                <Grid size={{ xs: 12, sm: 6, md: 4 }}>
                  <TextField
                    label="Access Token (optional)"
                    size="small"
                    fullWidth
                    type="password"
                    value={ntfyToken}
                    onChange={(e) => setNtfyToken(e.target.value)}
                    placeholder="tk_..."
                    helperText="For private topics"
                    disabled={!ntfyEnabled}
                  />
                </Grid>
              </Grid>

              <Divider sx={{ my: 2 }} />

              <Typography variant="subtitle2" gutterBottom>
                Event Types to Notify
              </Typography>
              <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 1 }}>
                <FormControlLabel
                  control={<Switch checked={notifyWifi} onChange={(e) => setNotifyWifi(e.target.checked)} size="small" />}
                  label="WiFi Clients"
                  disabled={!ntfyEnabled}
                />
                <FormControlLabel
                  control={<Switch checked={notifyHealth} onChange={(e) => setNotifyHealth(e.target.checked)} size="small" />}
                  label="Health Checks"
                  disabled={!ntfyEnabled}
                />
                <FormControlLabel
                  control={<Switch checked={notifyWan} onChange={(e) => setNotifyWan(e.target.checked)} size="small" />}
                  label="WAN Changes"
                  disabled={!ntfyEnabled}
                />
                <FormControlLabel
                  control={<Switch checked={notifyVpn} onChange={(e) => setNotifyVpn(e.target.checked)} size="small" />}
                  label="VPN Peers"
                  disabled={!ntfyEnabled}
                />
                <FormControlLabel
                  control={<Switch checked={notifySystem} onChange={(e) => setNotifySystem(e.target.checked)} size="small" />}
                  label="System Events"
                  disabled={!ntfyEnabled}
                />
              </Box>

              <Box sx={{ display: 'flex', gap: 2, mt: 2 }}>
                <Button
                  variant="contained"
                  onClick={handleSaveNotifications}
                  disabled={isUpdatingNotifications}
                >
                  Save Notification Settings
                </Button>
                <Button
                  variant="outlined"
                  startIcon={<SendIcon />}
                  onClick={handleTestNotification}
                  disabled={!ntfyEnabled || !ntfyTopic || isTesting}
                >
                  Send Test
                </Button>
              </Box>
            </CardContent>
          </Card>
        </Grid>

        {/* Devices Section */}
        <Grid size={12}>
          <Card>
            <CardContent>
              <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2 }}>
                <Typography variant="h6" color="primary">
                  Known Devices
                </Typography>
                <Button
                  size="small"
                  startIcon={<RefreshIcon />}
                  onClick={() => refetchDevices()}
                >
                  Refresh
                </Button>
              </Box>
              <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                Devices that have connected to your network. Set custom display names for easier identification.
              </Typography>

              <TableContainer>
                <Table size="small">
                  <TableHead>
                    <TableRow>
                      <TableCell>MAC Address</TableCell>
                      <TableCell>Display Name</TableCell>
                      <TableCell>Hostname</TableCell>
                      <TableCell>Last IP</TableCell>
                      <TableCell>Last Seen</TableCell>
                      <TableCell>Actions</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {devicesData?.devices?.length ? (
                      devicesData.devices.map((device: Device) => (
                        <TableRow key={device.mac}>
                          <TableCell sx={{ fontFamily: 'monospace', fontSize: '0.85rem' }}>
                            {device.mac}
                          </TableCell>
                          <TableCell>
                            {device.display_name || <Typography color="text.secondary" component="span">—</Typography>}
                          </TableCell>
                          <TableCell>{device.hostname || '—'}</TableCell>
                          <TableCell sx={{ fontFamily: 'monospace', fontSize: '0.85rem' }}>
                            {device.last_ip || '—'}
                          </TableCell>
                          <TableCell>{formatTimestamp(device.last_seen)}</TableCell>
                          <TableCell>
                            <IconButton size="small" onClick={() => handleEditDevice(device)}>
                              <EditIcon fontSize="small" />
                            </IconButton>
                            <IconButton size="small" onClick={() => handleDeleteDevice(device.mac)}>
                              <DeleteIcon fontSize="small" />
                            </IconButton>
                          </TableCell>
                        </TableRow>
                      ))
                    ) : (
                      <TableRow>
                        <TableCell colSpan={6} align="center">
                          <Typography color="text.secondary">No devices recorded yet</Typography>
                        </TableCell>
                      </TableRow>
                    )}
                  </TableBody>
                </Table>
              </TableContainer>
            </CardContent>
          </Card>
        </Grid>

        <Grid size={12}>
          <Card>
            <CardContent>
              <Typography variant="h6" color="primary" gutterBottom>
                About
              </Typography>
              <Typography variant="body2" color="text.secondary">
                Ubuntu Router - Network management application for Ubuntu-based routers.
              </Typography>
              <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
                Features: Interface management, DNS/DHCP (dnsmasq), WireGuard VPN, WiFi (hostapd), Firewall (iptables)
              </Typography>
            </CardContent>
          </Card>
        </Grid>

        <Grid size={12}>
          <Card>
            <CardContent>
              <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2 }}>
                <Typography variant="h6" color="primary">
                  Application Logs
                </Typography>
                <Button
                  size="small"
                  startIcon={<RefreshIcon />}
                  onClick={() => refetchLogs()}
                >
                  Refresh
                </Button>
              </Box>
              <Box
                sx={{
                  bgcolor: '#1e1e1e',
                  color: '#d4d4d4',
                  p: 2,
                  borderRadius: 1,
                  fontFamily: 'monospace',
                  fontSize: '0.85rem',
                  maxHeight: 400,
                  overflow: 'auto',
                }}
              >
                {logsData?.entries?.length ? (
                  [...logsData.entries].reverse().map((entry: LogEntry, i: number) => (
                    <Box key={i} sx={{ mb: 0.5, display: 'flex', gap: 1, alignItems: 'flex-start' }}>
                      <Typography
                        component="span"
                        sx={{ color: '#808080', fontSize: '0.8rem', flexShrink: 0 }}
                      >
                        {new Date(entry.time).toLocaleTimeString()}
                      </Typography>
                      <Chip
                        label={entry.level}
                        size="small"
                        color={getLevelColor(entry.level) as any}
                        sx={{ height: 18, fontSize: '0.7rem' }}
                      />
                      <Typography component="span" sx={{ wordBreak: 'break-word' }}>
                        {entry.message}
                      </Typography>
                    </Box>
                  ))
                ) : (
                  <Typography color="text.secondary">No logs available</Typography>
                )}
              </Box>
            </CardContent>
          </Card>
        </Grid>
      </Grid>

      {/* Device Edit Dialog */}
      <Dialog open={!!editDevice} onClose={() => setEditDevice(null)} maxWidth="sm" fullWidth>
        <DialogTitle>Edit Device</DialogTitle>
        <DialogContent>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 1 }}>
            <Typography variant="body2" color="text.secondary">
              MAC: {editDevice?.mac}
            </Typography>
            <Typography variant="body2" color="text.secondary">
              Hostname: {editDevice?.hostname || 'Unknown'}
            </Typography>
            <TextField
              label="Display Name"
              fullWidth
              value={editDisplayName}
              onChange={(e) => setEditDisplayName(e.target.value)}
              placeholder="e.g., Living Room TV, John's Phone"
              helperText="Custom name to identify this device"
            />
            <TextField
              label="Notes"
              fullWidth
              multiline
              rows={2}
              value={editNotes}
              onChange={(e) => setEditNotes(e.target.value)}
              placeholder="Optional notes about this device"
            />
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setEditDevice(null)}>Cancel</Button>
          <Button variant="contained" onClick={handleSaveDevice}>Save</Button>
        </DialogActions>
      </Dialog>

      {/* Shutdown Confirmation Dialog */}
      <Dialog open={shutdownDialogOpen} onClose={() => setShutdownDialogOpen(false)}>
        <DialogTitle>Confirm Shutdown</DialogTitle>
        <DialogContent>
          <Typography>
            Are you sure you want to shut down the system? You will need physical access to power it back on.
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setShutdownDialogOpen(false)}>Cancel</Button>
          <Button
            variant="contained"
            color="error"
            onClick={handleShutdown}
            disabled={isShuttingDown}
            startIcon={<PowerIcon />}
          >
            {isShuttingDown ? 'Shutting down...' : 'Shutdown'}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Reboot Confirmation Dialog */}
      <Dialog open={rebootDialogOpen} onClose={() => setRebootDialogOpen(false)}>
        <DialogTitle>Confirm Reboot</DialogTitle>
        <DialogContent>
          <Typography>
            Are you sure you want to reboot the system? The router will be unavailable for a short time.
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setRebootDialogOpen(false)}>Cancel</Button>
          <Button
            variant="contained"
            color="warning"
            onClick={handleReboot}
            disabled={isRebooting}
            startIcon={<RestartIcon />}
          >
            {isRebooting ? 'Rebooting...' : 'Reboot'}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}

export const Route = createFileRoute('/settings')({
  component: SettingsPage,
});
