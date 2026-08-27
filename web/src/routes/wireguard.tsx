import { createFileRoute } from '@tanstack/react-router';
import { useState, useEffect } from 'react';
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
  Grid,
  Chip,
  IconButton,
  CircularProgress,
  Alert,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Switch,
  FormControlLabel,
} from '@mui/material';
import {
  Delete as DeleteIcon,
  Add as AddIcon,
  PlayArrow as StartIcon,
  Stop as StopIcon,
  Refresh as RestartIcon,
  Settings as SettingsIcon,
  Save as SaveIcon,
} from '@mui/icons-material';
import { useQuery, useMutation } from '../api/hooks';
import type { WireGuardSettings } from '../api/client';

function WireGuardPage() {
  const { data: statusData, isLoading: statusLoading, refetch: refetchStatus } = useQuery('getWireGuardStatus');
  const { data: settingsData, isLoading: settingsLoading, refetch: refetchSettings } = useQuery('getWireGuardSettings');
  const { data: peersData, isLoading: peersLoading, refetch: refetchPeers } = useQuery('listPeers');
  const { mutate: addPeer, isLoading: isAdding } = useMutation('addPeer');
  const { mutate: removePeer, isLoading: isRemoving } = useMutation('removePeer');
  const { mutate: control, isLoading: isControlling } = useMutation('wireGuardControl');
  const { mutate: updateSettings, isLoading: isSaving } = useMutation('updateWireGuardSettings');

  const [name, setName] = useState('');
  const [endpoint, setEndpoint] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  const [clientConfig, setClientConfig] = useState<string | null>(null);

  // Settings form state
  const [settings, setSettings] = useState<Partial<WireGuardSettings>>({});
  const [settingsChanged, setSettingsChanged] = useState(false);

  // Load settings into form when data changes
  useEffect(() => {
    if (settingsData) {
      setSettings({
        enabled: settingsData.enabled,
        address: settingsData.address,
        listen_port: settingsData.listen_port,
        dns: settingsData.dns,
        route_all_traffic: settingsData.route_all_traffic,
        route_lan: settingsData.route_lan,
        lan_subnets: settingsData.lan_subnets || [],
      });
      setSettingsChanged(false);
    }
  }, [settingsData]);

  const handleSettingChange = (key: keyof WireGuardSettings, value: unknown) => {
    setSettings(prev => ({ ...prev, [key]: value }));
    setSettingsChanged(true);
  };

  const handleSaveSettings = async () => {
    try {
      await updateSettings(settings);
      setSuccess('Settings saved');
      setSettingsChanged(false);
      refetchSettings();
      refetchStatus();
    } catch (e) {
      setError(String(e));
    }
  };

  const handleAddPeer = async () => {
    if (!name || !endpoint) {
      setError('Name and endpoint are required');
      return;
    }
    try {
      const result = await addPeer({ name, serverEndpoint: endpoint });
      setName('');
      setEndpoint('');
      setSuccess('Peer added');
      setError(null);
      if (result.clientConfig) {
        setClientConfig(result.clientConfig);
      }
      refetchPeers();
    } catch (e) {
      setError(String(e));
    }
  };

  const handleRemovePeer = async (publicKey: string) => {
    try {
      await removePeer({ publicKey });
      setSuccess('Peer removed');
      refetchPeers();
    } catch (e) {
      setError(String(e));
    }
  };

  const handleControl = async (action: 'start' | 'stop' | 'restart') => {
    try {
      await control({ action });
      setSuccess(`WireGuard ${action}ed`);
      refetchStatus();
    } catch (e) {
      setError(String(e));
    }
  };

  return (
    <Box>
      <Typography variant="h4" gutterBottom>
        WireGuard VPN
      </Typography>

      {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}
      {success && <Alert severity="success" sx={{ mb: 2 }}>{success}</Alert>}

      <Grid container spacing={3}>
        <Grid size={{ xs: 12, md: 6 }}>
          <Card>
            <CardContent>
              <Typography variant="h6" color="primary" gutterBottom>
                WireGuard Status
              </Typography>
              {statusLoading ? (
                <CircularProgress />
              ) : statusData ? (
                <>
                  <Table size="small">
                    <TableBody>
                      <TableRow>
                        <TableCell>Interface</TableCell>
                        <TableCell><code>{statusData.interface}</code></TableCell>
                      </TableRow>
                      <TableRow>
                        <TableCell>Status</TableCell>
                        <TableCell>
                          {statusData.running ? (
                            <Chip label="Running" color="success" size="small" />
                          ) : (
                            <Chip label="Stopped" color="error" size="small" />
                          )}
                        </TableCell>
                      </TableRow>
                      <TableRow>
                        <TableCell>Listen Port</TableCell>
                        <TableCell>{statusData.listenPort}</TableCell>
                      </TableRow>
                      <TableRow>
                        <TableCell>Address</TableCell>
                        <TableCell><code>{statusData.address}</code></TableCell>
                      </TableRow>
                      <TableRow>
                        <TableCell>Peers</TableCell>
                        <TableCell>{statusData.activePeers} / {statusData.peerCount} active</TableCell>
                      </TableRow>
                    </TableBody>
                  </Table>
                  <Box sx={{ mt: 2, display: 'flex', gap: 1 }}>
                    {statusData.running ? (
                      <>
                        <Button
                          size="small"
                          variant="outlined"
                          color="error"
                          startIcon={<StopIcon />}
                          onClick={() => handleControl('stop')}
                          disabled={isControlling}
                        >
                          Stop
                        </Button>
                        <Button
                          size="small"
                          variant="outlined"
                          color="warning"
                          startIcon={<RestartIcon />}
                          onClick={() => handleControl('restart')}
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
                        onClick={() => handleControl('start')}
                        disabled={isControlling}
                      >
                        Start
                      </Button>
                    )}
                  </Box>
                </>
              ) : (
                <Typography color="text.secondary">WireGuard not configured</Typography>
              )}
            </CardContent>
          </Card>
        </Grid>

        <Grid size={{ xs: 12, md: 6 }}>
          <Card>
            <CardContent>
              <Typography variant="h6" color="primary" gutterBottom>
                Add Peer
              </Typography>
              <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                <TextField
                  label="Name"
                  size="small"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="phone"
                />
                <TextField
                  label="Server Endpoint"
                  size="small"
                  value={endpoint}
                  onChange={(e) => setEndpoint(e.target.value)}
                  placeholder="vpn.example.com"
                  helperText="Your public IP or hostname"
                />
                <Button
                  variant="contained"
                  startIcon={<AddIcon />}
                  onClick={handleAddPeer}
                  disabled={isAdding}
                >
                  Generate Peer Config
                </Button>
              </Box>
            </CardContent>
          </Card>
        </Grid>

        <Grid size={12}>
          <Card>
            <CardContent>
              <Typography variant="h6" color="primary" gutterBottom>
                Peers
              </Typography>
              {peersLoading ? (
                <CircularProgress />
              ) : (
                <TableContainer>
                  <Table>
                    <TableHead>
                      <TableRow>
                        <TableCell>Name</TableCell>
                        <TableCell>Public Key</TableCell>
                        <TableCell>Allowed IPs</TableCell>
                        <TableCell>Actions</TableCell>
                      </TableRow>
                    </TableHead>
                    <TableBody>
                      {peersData?.peers?.length === 0 ? (
                        <TableRow>
                          <TableCell colSpan={4}>
                            <Typography color="text.secondary">No peers configured</Typography>
                          </TableCell>
                        </TableRow>
                      ) : (
                        peersData?.peers?.map((peer) => (
                          <TableRow key={peer.publicKey}>
                            <TableCell>{peer.name}</TableCell>
                            <TableCell>
                              <code style={{ fontSize: '0.75rem' }}>{peer.publicKey}</code>
                            </TableCell>
                            <TableCell>
                              {peer.allowedIps?.map((ip, i) => (
                                <code key={i} style={{ marginRight: 4 }}>{ip}</code>
                              ))}
                            </TableCell>
                            <TableCell>
                              <IconButton
                                size="small"
                                color="error"
                                onClick={() => handleRemovePeer(peer.publicKey)}
                                disabled={isRemoving}
                              >
                                <DeleteIcon />
                              </IconButton>
                            </TableCell>
                          </TableRow>
                        ))
                      )}
                    </TableBody>
                  </Table>
                </TableContainer>
              )}
            </CardContent>
          </Card>
        </Grid>

        <Grid size={12}>
          <Card>
            <CardContent>
              <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 2 }}>
                <Typography variant="h6" color="primary">
                  <SettingsIcon sx={{ mr: 1, verticalAlign: 'middle' }} />
                  VPN Settings
                </Typography>
                <Button
                  variant="contained"
                  startIcon={<SaveIcon />}
                  onClick={handleSaveSettings}
                  disabled={isSaving || !settingsChanged}
                  size="small"
                >
                  {isSaving ? 'Saving...' : 'Save Settings'}
                </Button>
              </Box>
              {settingsLoading ? (
                <CircularProgress />
              ) : (
                <Grid container spacing={2}>
                  <Grid size={{ xs: 12, sm: 6, md: 3 }}>
                    <FormControlLabel
                      control={
                        <Switch
                          checked={settings.enabled || false}
                          onChange={(e) => handleSettingChange('enabled', e.target.checked)}
                        />
                      }
                      label="Enable VPN"
                    />
                  </Grid>
                  <Grid size={{ xs: 12, sm: 6, md: 3 }}>
                    <TextField
                      label="VPN Address"
                      size="small"
                      fullWidth
                      value={settings.address || ''}
                      onChange={(e) => handleSettingChange('address', e.target.value)}
                      placeholder="10.0.0.1/24"
                      helperText="Server IP and subnet"
                    />
                  </Grid>
                  <Grid size={{ xs: 12, sm: 6, md: 3 }}>
                    <TextField
                      label="Listen Port"
                      size="small"
                      fullWidth
                      type="number"
                      value={settings.listen_port || ''}
                      onChange={(e) => handleSettingChange('listen_port', parseInt(e.target.value) || 0)}
                      placeholder="51820"
                    />
                  </Grid>
                  <Grid size={{ xs: 12, sm: 6, md: 3 }}>
                    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
                      <TextField
                        label="DNS Server"
                        size="small"
                        fullWidth
                        value={settings.dns || ''}
                        onChange={(e) => handleSettingChange('dns', e.target.value)}
                        placeholder={settingsData?.vpn_server_ip || 'VPN Server IP'}
                        helperText="DNS for clients"
                      />
                      <Box sx={{ display: 'flex', gap: 0.5, flexWrap: 'wrap' }}>
                        <Button
                          size="small"
                          variant="outlined"
                          onClick={() => handleSettingChange('dns', settingsData?.vpn_server_ip || '')}
                          disabled={!settingsData?.vpn_server_ip}
                        >
                          Use VPN Server
                        </Button>
                        <Button
                          size="small"
                          variant="outlined"
                          onClick={() => handleSettingChange('dns', '8.8.8.8')}
                        >
                          Google
                        </Button>
                        <Button
                          size="small"
                          variant="outlined"
                          onClick={() => handleSettingChange('dns', '1.1.1.1')}
                        >
                          Cloudflare
                        </Button>
                      </Box>
                    </Box>
                  </Grid>
                  <Grid size={12}>
                    <Typography variant="subtitle2" color="text.secondary" sx={{ mb: 1 }}>
                      Client Routing Options
                    </Typography>
                    <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 2 }}>
                      <FormControlLabel
                        control={
                          <Switch
                            checked={settings.route_all_traffic || false}
                            onChange={(e) => {
                              handleSettingChange('route_all_traffic', e.target.checked);
                              if (e.target.checked) {
                                handleSettingChange('route_lan', false);
                              }
                            }}
                          />
                        }
                        label="Route all client traffic through VPN"
                      />
                      <FormControlLabel
                        control={
                          <Switch
                            checked={settings.route_lan || false}
                            onChange={(e) => {
                              handleSettingChange('route_lan', e.target.checked);
                              if (e.target.checked) {
                                handleSettingChange('route_all_traffic', false);
                              }
                            }}
                            disabled={settings.route_all_traffic}
                          />
                        }
                        label="Allow clients to access LAN"
                      />
                    </Box>
                  </Grid>
                  {settings.route_lan && settingsData?.available_subnets && (
                    <Grid size={12}>
                      <Typography variant="subtitle2" color="text.secondary" sx={{ mb: 1 }}>
                        Select LAN Subnets for Client Access
                      </Typography>
                      <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
                        {settingsData.available_subnets
                          .filter(s => s.source !== 'wireguard') // Don't show WireGuard's own subnet
                          .map((subnetInfo) => {
                            const isSelected = (settings.lan_subnets || []).includes(subnetInfo.subnet);
                            return (
                              <FormControlLabel
                                key={subnetInfo.subnet}
                                control={
                                  <Switch
                                    checked={isSelected}
                                    onChange={(e) => {
                                      const current = settings.lan_subnets || [];
                                      if (e.target.checked) {
                                        handleSettingChange('lan_subnets', [...current, subnetInfo.subnet]);
                                      } else {
                                        handleSettingChange('lan_subnets', current.filter(s => s !== subnetInfo.subnet));
                                      }
                                    }}
                                    size="small"
                                  />
                                }
                                label={
                                  <Box>
                                    <Typography variant="body2" component="span">
                                      <code>{subnetInfo.subnet}</code>
                                    </Typography>
                                    <Typography variant="caption" color="text.secondary" sx={{ ml: 1 }}>
                                      {subnetInfo.description}
                                    </Typography>
                                  </Box>
                                }
                              />
                            );
                          })}
                      </Box>
                      <TextField
                        label="Additional Subnets"
                        size="small"
                        fullWidth
                        sx={{ mt: 2 }}
                        value={(settings.lan_subnets || [])
                          .filter(s => !settingsData.available_subnets.some(as => as.subnet === s))
                          .join(', ')}
                        onChange={(e) => {
                          // Keep selected predefined subnets, add custom ones
                          const predefined = (settings.lan_subnets || [])
                            .filter(s => settingsData.available_subnets.some(as => as.subnet === s));
                          const custom = e.target.value.split(',').map(s => s.trim()).filter(s => s);
                          handleSettingChange('lan_subnets', [...predefined, ...custom]);
                        }}
                        placeholder="Add custom subnets (comma-separated)"
                        helperText="Add additional subnets not listed above"
                      />
                    </Grid>
                  )}
                </Grid>
              )}
            </CardContent>
          </Card>
        </Grid>
      </Grid>

      <Dialog open={!!clientConfig} onClose={() => setClientConfig(null)} maxWidth="md">
        <DialogTitle>Client Configuration</DialogTitle>
        <DialogContent>
          <Typography variant="body2" gutterBottom>
            Copy this configuration to your WireGuard client:
          </Typography>
          <Box
            component="pre"
            sx={{
              bgcolor: 'background.default',
              p: 2,
              borderRadius: 1,
              overflow: 'auto',
              fontSize: '0.85rem',
            }}
          >
            {clientConfig}
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setClientConfig(null)}>Close</Button>
          <Button
            variant="contained"
            onClick={() => {
              navigator.clipboard.writeText(clientConfig || '');
              setSuccess('Copied to clipboard');
            }}
          >
            Copy
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}

export const Route = createFileRoute('/wireguard')({
  component: WireGuardPage,
});
