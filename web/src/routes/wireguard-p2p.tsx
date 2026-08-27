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
  Grid,
  Chip,
  IconButton,
  CircularProgress,
  Alert,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
} from '@mui/material';
import {
  Delete as DeleteIcon,
  Add as AddIcon,
  PlayArrow as StartIcon,
  Stop as StopIcon,
  Refresh as RestartIcon,
  ContentCopy as CopyIcon,
  Link as LinkIcon,
  Upload as UploadIcon,
  Edit as EditIcon,
} from '@mui/icons-material';
import { useQuery, useMutation } from '../api/hooks';
import type { P2PStatus, WireGuardP2PTunnel } from '../api/client';

function WireGuardP2PPage() {
  const { data: statusData, isLoading: statusLoading, refetch: refetchStatus } = useQuery('getP2PStatus');
  const { data: tunnelsData, isLoading: tunnelsLoading, refetch: refetchTunnels } = useQuery('listP2PTunnels');
  const { mutate: createTunnel, isLoading: isCreating } = useMutation('createP2PTunnel');
  const { mutate: deleteTunnel, isLoading: isDeleting } = useMutation('deleteP2PTunnel');
  const { mutate: controlTunnel, isLoading: isControlling } = useMutation('controlP2PTunnel');
  const { mutate: getRemoteConfig } = useMutation('getP2PRemoteConfig');
  const { mutate: importConfig, isLoading: isImporting } = useMutation('importP2PConfig');
  const { mutate: updateTunnel, isLoading: isUpdating } = useMutation('updateP2PTunnel');

  // Form state
  const [name, setName] = useState('');
  const [address, setAddress] = useState('10.100.0.1/30');
  const [remotePublicKey, setRemotePublicKey] = useState('');
  const [remoteEndpoint, setRemoteEndpoint] = useState('');
  const [localSubnets, setLocalSubnets] = useState('');
  const [remoteSubnets, setRemoteSubnets] = useState('');

  // UI state
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  const [remoteConfig, setRemoteConfig] = useState<string | null>(null);
  const [showRemoteConfigDialog, setShowRemoteConfigDialog] = useState(false);
  const [endpointForRemote, setEndpointForRemote] = useState('');
  const [selectedTunnelId, setSelectedTunnelId] = useState<string | null>(null);
  const [showImportDialog, setShowImportDialog] = useState(false);
  const [importName, setImportName] = useState('');
  const [importConfigText, setImportConfigText] = useState('');

  // Edit dialog state
  const [showEditDialog, setShowEditDialog] = useState(false);
  const [editingTunnel, setEditingTunnel] = useState<WireGuardP2PTunnel | null>(null);
  const [editRemoteSubnets, setEditRemoteSubnets] = useState('');
  const [editForcedSubnets, setEditForcedSubnets] = useState('');
  const [editLocalSubnets, setEditLocalSubnets] = useState('');

  const handleCreateTunnel = async () => {
    if (!name) {
      setError('Name is required');
      return;
    }
    try {
      await createTunnel({
        name,
        enabled: true,
        address,
        remote_public_key: remotePublicKey,
        remote_endpoint: remoteEndpoint,
        local_subnets: localSubnets ? localSubnets.split(',').map(s => s.trim()) : [],
        remote_subnets: remoteSubnets ? remoteSubnets.split(',').map(s => s.trim()) : [],
        forced_subnets: [],
        persistent_keepalive: 25,
      });
      setName('');
      setAddress('10.100.0.1/30');
      setRemotePublicKey('');
      setRemoteEndpoint('');
      setLocalSubnets('');
      setRemoteSubnets('');
      setSuccess('Tunnel created');
      setError(null);
      refetchTunnels();
      refetchStatus();
    } catch (e) {
      setError(String(e));
    }
  };

  const handleDeleteTunnel = async (id: string) => {
    try {
      await deleteTunnel({ id });
      setSuccess('Tunnel deleted');
      refetchTunnels();
      refetchStatus();
    } catch (e) {
      setError(String(e));
    }
  };

  const handleControlTunnel = async (id: string, action: 'start' | 'stop' | 'restart') => {
    try {
      const result = await controlTunnel({ id, action });
      if (result.pending_confirmation) {
        setSuccess(result.message || `Tunnel ${action}ed. Confirm within 30 seconds or changes will be reverted.`);
      } else {
        setSuccess(result.message || `Tunnel ${action}ed`);
      }
      setError(null);
      refetchStatus();
    } catch (e) {
      setError(String(e));
    }
  };

  const handleGetRemoteConfig = async () => {
    if (!selectedTunnelId || !endpointForRemote) {
      setError('Please enter your public endpoint');
      return;
    }
    try {
      const config = await getRemoteConfig({ id: selectedTunnelId, endpoint: endpointForRemote });
      setRemoteConfig(config.wireguard_config);
    } catch (e) {
      setError(String(e));
    }
  };

  const openRemoteConfigDialog = (tunnelId: string) => {
    setSelectedTunnelId(tunnelId);
    setRemoteConfig(null);
    setEndpointForRemote('');
    setShowRemoteConfigDialog(true);
  };

  const handleImportConfig = async () => {
    if (!importName) {
      setError('Name is required');
      return;
    }
    if (!importConfigText) {
      setError('Please paste a WireGuard config');
      return;
    }
    try {
      await importConfig({ name: importName, config: importConfigText });
      setImportName('');
      setImportConfigText('');
      setShowImportDialog(false);
      setSuccess('Tunnel imported successfully');
      setError(null);
      refetchTunnels();
      refetchStatus();
    } catch (e) {
      setError(String(e));
    }
  };

  const openEditDialog = (tunnel: WireGuardP2PTunnel) => {
    setEditingTunnel(tunnel);
    setEditRemoteSubnets(tunnel.remote_subnets?.join(', ') || '');
    setEditForcedSubnets(tunnel.forced_subnets?.join(', ') || '');
    setEditLocalSubnets(tunnel.local_subnets?.join(', ') || '');
    setShowEditDialog(true);
  };

  const handleUpdateTunnel = async () => {
    if (!editingTunnel) return;
    try {
      await updateTunnel({
        id: editingTunnel.id,
        remote_subnets: editRemoteSubnets ? editRemoteSubnets.split(',').map(s => s.trim()).filter(Boolean) : [],
        forced_subnets: editForcedSubnets ? editForcedSubnets.split(',').map(s => s.trim()).filter(Boolean) : [],
        local_subnets: editLocalSubnets ? editLocalSubnets.split(',').map(s => s.trim()).filter(Boolean) : [],
      });
      setShowEditDialog(false);
      setEditingTunnel(null);
      setSuccess('Tunnel updated. Restart the tunnel to apply changes.');
      refetchTunnels();
      refetchStatus();
    } catch (e) {
      setError(String(e));
    }
  };

  // Find status for a tunnel
  const getStatusForTunnel = (tunnelId: string): P2PStatus | undefined => {
    return statusData?.tunnels?.find(s => s.tunnel_id === tunnelId);
  };

  return (
    <Box>
      <Typography variant="h4" gutterBottom>
        Site-to-Site VPN
      </Typography>

      {error && <Alert severity="error" sx={{ mb: 2 }} onClose={() => setError(null)}>{error}</Alert>}
      {success && <Alert severity="success" sx={{ mb: 2 }} onClose={() => setSuccess(null)}>{success}</Alert>}

      <Grid container spacing={3}>
        <Grid size={{ xs: 12, md: 6 }}>
          <Card>
            <CardContent>
              <Typography variant="h6" color="primary" gutterBottom>
                Create Site-to-Site Tunnel
              </Typography>
              <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                <TextField
                  label="Tunnel Name"
                  size="small"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="office-to-datacenter"
                />
                <TextField
                  label="Tunnel Address"
                  size="small"
                  value={address}
                  onChange={(e) => setAddress(e.target.value)}
                  placeholder="10.100.0.1/30"
                  helperText="Local endpoint IP for the tunnel (use /30 for point-to-point)"
                />
                <TextField
                  label="Remote Public Key"
                  size="small"
                  value={remotePublicKey}
                  onChange={(e) => setRemotePublicKey(e.target.value)}
                  placeholder="Peer's WireGuard public key"
                  helperText="Leave empty to generate keys first, then share with peer"
                />
                <TextField
                  label="Remote Endpoint"
                  size="small"
                  value={remoteEndpoint}
                  onChange={(e) => setRemoteEndpoint(e.target.value)}
                  placeholder="remote.example.com:51821"
                  helperText="Peer's public IP:port (optional if they connect to us)"
                />
                <TextField
                  label="Local Subnets"
                  size="small"
                  value={localSubnets}
                  onChange={(e) => setLocalSubnets(e.target.value)}
                  placeholder="192.168.1.0/24"
                  helperText="Local LAN subnets to share with peer (comma-separated)"
                />
                <TextField
                  label="Remote Subnets"
                  size="small"
                  value={remoteSubnets}
                  onChange={(e) => setRemoteSubnets(e.target.value)}
                  placeholder="192.168.2.0/24"
                  helperText="Remote LAN subnets to route through tunnel (comma-separated)"
                />
                <Box sx={{ display: 'flex', gap: 1 }}>
                  <Button
                    variant="contained"
                    startIcon={<AddIcon />}
                    onClick={handleCreateTunnel}
                    disabled={isCreating}
                  >
                    Create Tunnel
                  </Button>
                  <Button
                    variant="outlined"
                    startIcon={<UploadIcon />}
                    onClick={() => setShowImportDialog(true)}
                  >
                    Import Config
                  </Button>
                </Box>
              </Box>
            </CardContent>
          </Card>
        </Grid>

        <Grid size={{ xs: 12, md: 6 }}>
          <Card>
            <CardContent>
              <Typography variant="h6" color="primary" gutterBottom>
                Tunnel Overview
              </Typography>
              {statusLoading ? (
                <CircularProgress />
              ) : (
                <Table size="small">
                  <TableBody>
                    <TableRow>
                      <TableCell>Total Tunnels</TableCell>
                      <TableCell>{tunnelsData?.tunnels?.length || 0}</TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell>Running</TableCell>
                      <TableCell>
                        {statusData?.tunnels?.filter(t => t.running).length || 0}
                      </TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell>Connected</TableCell>
                      <TableCell>
                        <Chip
                          label={statusData?.tunnels?.filter(t => t.connected).length || 0}
                          color={statusData?.tunnels?.some(t => t.connected) ? 'success' : 'default'}
                          size="small"
                        />
                      </TableCell>
                    </TableRow>
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </Card>
        </Grid>

        <Grid size={12}>
          <Card>
            <CardContent>
              <Typography variant="h6" color="primary" gutterBottom>
                Site-to-Site Tunnels
              </Typography>
              {tunnelsLoading ? (
                <CircularProgress />
              ) : (
                <TableContainer>
                  <Table>
                    <TableHead>
                      <TableRow>
                        <TableCell>Name</TableCell>
                        <TableCell>Interface</TableCell>
                        <TableCell>Status</TableCell>
                        <TableCell>Address</TableCell>
                        <TableCell>Remote Endpoint</TableCell>
                        <TableCell>Transfer</TableCell>
                        <TableCell>Actions</TableCell>
                      </TableRow>
                    </TableHead>
                    <TableBody>
                      {tunnelsData?.tunnels?.length === 0 ? (
                        <TableRow>
                          <TableCell colSpan={7}>
                            <Typography color="text.secondary">No tunnels configured</Typography>
                          </TableCell>
                        </TableRow>
                      ) : (
                        tunnelsData?.tunnels?.map((tunnel) => {
                          const status = getStatusForTunnel(tunnel.id);
                          return (
                            <TableRow key={tunnel.id}>
                              <TableCell>{tunnel.name}</TableCell>
                              <TableCell><code>{tunnel.interface}</code></TableCell>
                              <TableCell>
                                {status?.running ? (
                                  status?.connected ? (
                                    <Chip label="Connected" color="success" size="small" />
                                  ) : (
                                    <Chip label="Running" color="warning" size="small" />
                                  )
                                ) : (
                                  <Chip label="Stopped" color="default" size="small" />
                                )}
                              </TableCell>
                              <TableCell><code>{tunnel.address}</code></TableCell>
                              <TableCell>
                                <code style={{ fontSize: '0.75rem' }}>
                                  {tunnel.remote_endpoint || '-'}
                                </code>
                              </TableCell>
                              <TableCell>
                                {status?.running ? (
                                  <Typography variant="body2">
                                    {formatBytes(status.transfer_rx)} / {formatBytes(status.transfer_tx)}
                                  </Typography>
                                ) : '-'}
                              </TableCell>
                              <TableCell>
                                <Box sx={{ display: 'flex', gap: 0.5 }}>
                                  {status?.running ? (
                                    <>
                                      <IconButton
                                        size="small"
                                        color="error"
                                        onClick={() => handleControlTunnel(tunnel.id, 'stop')}
                                        disabled={isControlling}
                                        title="Stop"
                                      >
                                        <StopIcon fontSize="small" />
                                      </IconButton>
                                      <IconButton
                                        size="small"
                                        color="warning"
                                        onClick={() => handleControlTunnel(tunnel.id, 'restart')}
                                        disabled={isControlling}
                                        title="Restart"
                                      >
                                        <RestartIcon fontSize="small" />
                                      </IconButton>
                                    </>
                                  ) : (
                                    <IconButton
                                      size="small"
                                      color="success"
                                      onClick={() => handleControlTunnel(tunnel.id, 'start')}
                                      disabled={isControlling}
                                      title="Start"
                                    >
                                      <StartIcon fontSize="small" />
                                    </IconButton>
                                  )}
                                  <IconButton
                                    size="small"
                                    color="primary"
                                    onClick={() => openRemoteConfigDialog(tunnel.id)}
                                    title="Get Remote Config"
                                  >
                                    <LinkIcon fontSize="small" />
                                  </IconButton>
                                  <IconButton
                                    size="small"
                                    color="info"
                                    onClick={() => openEditDialog(tunnel)}
                                    title="Edit Subnets"
                                  >
                                    <EditIcon fontSize="small" />
                                  </IconButton>
                                  <IconButton
                                    size="small"
                                    color="error"
                                    onClick={() => handleDeleteTunnel(tunnel.id)}
                                    disabled={isDeleting}
                                    title="Delete"
                                  >
                                    <DeleteIcon fontSize="small" />
                                  </IconButton>
                                </Box>
                              </TableCell>
                            </TableRow>
                          );
                        })
                      )}
                    </TableBody>
                  </Table>
                </TableContainer>
              )}
            </CardContent>
          </Card>
        </Grid>
      </Grid>

      <Dialog open={showRemoteConfigDialog} onClose={() => setShowRemoteConfigDialog(false)} maxWidth="md" fullWidth>
        <DialogTitle>Remote Peer Configuration</DialogTitle>
        <DialogContent>
          {!remoteConfig ? (
            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 1 }}>
              <Typography variant="body2">
                Enter your public IP or hostname so the remote peer knows how to connect back:
              </Typography>
              <TextField
                label="Your Public Endpoint"
                size="small"
                fullWidth
                value={endpointForRemote}
                onChange={(e) => setEndpointForRemote(e.target.value)}
                placeholder="vpn.example.com or 203.0.113.1"
                helperText="Your public IP or hostname (port will be added automatically)"
              />
              <Button variant="contained" onClick={handleGetRemoteConfig}>
                Generate Config
              </Button>
            </Box>
          ) : (
            <>
              <Typography variant="body2" gutterBottom>
                Share this configuration with the remote peer. They need to:
              </Typography>
              <Typography variant="body2" component="ol" sx={{ pl: 2, mb: 2 }}>
                <li>Generate their own private key and replace &lt;REMOTE_PRIVATE_KEY&gt;</li>
                <li>Save the config to /etc/wireguard/wgX.conf</li>
                <li>Provide you their public key to complete the tunnel</li>
              </Typography>
              <Box
                component="pre"
                sx={{
                  bgcolor: 'background.default',
                  p: 2,
                  borderRadius: 1,
                  overflow: 'auto',
                  fontSize: '0.8rem',
                  maxHeight: '400px',
                }}
              >
                {remoteConfig}
              </Box>
            </>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setShowRemoteConfigDialog(false)}>Close</Button>
          {remoteConfig && (
            <Button
              variant="contained"
              startIcon={<CopyIcon />}
              onClick={() => {
                navigator.clipboard.writeText(remoteConfig);
                setSuccess('Copied to clipboard');
              }}
            >
              Copy
            </Button>
          )}
        </DialogActions>
      </Dialog>

      <Dialog open={showImportDialog} onClose={() => setShowImportDialog(false)} maxWidth="md" fullWidth>
        <DialogTitle>Import WireGuard Config</DialogTitle>
        <DialogContent>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 1 }}>
            <Typography variant="body2">
              Paste a WireGuard configuration file (.conf) to create a new tunnel. The config will be
              parsed and imported automatically.
            </Typography>
            <TextField
              label="Tunnel Name"
              size="small"
              fullWidth
              value={importName}
              onChange={(e) => setImportName(e.target.value)}
              placeholder="my-vpn-tunnel"
              helperText="Give this tunnel a descriptive name"
            />
            <TextField
              label="WireGuard Config"
              size="small"
              fullWidth
              multiline
              rows={12}
              value={importConfigText}
              onChange={(e) => setImportConfigText(e.target.value)}
              placeholder={`[Interface]
PrivateKey = ...
Address = 10.0.0.2/24

[Peer]
PublicKey = ...
AllowedIPs = 0.0.0.0/0
Endpoint = vpn.example.com:51820`}
              sx={{
                '& .MuiInputBase-input': {
                  fontFamily: 'monospace',
                  fontSize: '0.85rem',
                },
              }}
            />
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setShowImportDialog(false)}>Cancel</Button>
          <Button
            variant="contained"
            onClick={handleImportConfig}
            disabled={isImporting || !importName || !importConfigText}
          >
            {isImporting ? 'Importing...' : 'Import'}
          </Button>
        </DialogActions>
      </Dialog>

      {/* Edit Tunnel Dialog */}
      <Dialog open={showEditDialog} onClose={() => setShowEditDialog(false)} maxWidth="md" fullWidth>
        <DialogTitle>
          Edit Tunnel: {editingTunnel?.name}
        </DialogTitle>
        <DialogContent>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 1 }}>
            <Alert severity="info" sx={{ mb: 1 }}>
              <strong>Auto-filtered subnets:</strong> Subnets that overlap with local networks are automatically
              removed from WireGuard routing to preserve local connectivity.
              <br /><br />
              <strong>Forced subnets:</strong> Use this to explicitly route specific addresses through the tunnel,
              even if they conflict with local networks. Use /32 for specific hosts.
            </Alert>

            <TextField
              label="Remote Subnets (auto-filtered)"
              size="small"
              fullWidth
              value={editRemoteSubnets}
              onChange={(e) => setEditRemoteSubnets(e.target.value)}
              placeholder="192.168.2.0/24, 10.0.0.0/24"
              helperText="Subnets to route through tunnel. Conflicting subnets are filtered automatically."
            />

            <TextField
              label="Forced Subnets (bypass filter)"
              size="small"
              fullWidth
              value={editForcedSubnets}
              onChange={(e) => setEditForcedSubnets(e.target.value)}
              placeholder="192.168.1.50/32, 192.168.1.51/32"
              helperText="Force these addresses through the tunnel even if they conflict. Use /32 for specific hosts."
              sx={{
                '& .MuiOutlinedInput-root': {
                  '& fieldset': { borderColor: 'warning.main' },
                },
              }}
            />

            <TextField
              label="Local Subnets (advertise to peer)"
              size="small"
              fullWidth
              value={editLocalSubnets}
              onChange={(e) => setEditLocalSubnets(e.target.value)}
              placeholder="192.168.1.0/24"
              helperText="Your local LAN subnets to share with the remote peer."
            />

            {editingTunnel && (
              <Box sx={{ bgcolor: 'background.default', p: 2, borderRadius: 1 }}>
                <Typography variant="subtitle2" gutterBottom>Current Configuration:</Typography>
                <Typography variant="body2" component="div">
                  <strong>Interface:</strong> {editingTunnel.interface}<br />
                  <strong>Address:</strong> {editingTunnel.address}<br />
                  <strong>Remote Endpoint:</strong> {editingTunnel.remote_endpoint || 'Not set'}<br />
                  <strong>Listen Port:</strong> {editingTunnel.listen_port}
                </Typography>
              </Box>
            )}
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setShowEditDialog(false)}>Cancel</Button>
          <Button
            variant="contained"
            onClick={handleUpdateTunnel}
            disabled={isUpdating}
          >
            {isUpdating ? 'Saving...' : 'Save Changes'}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}

export const Route = createFileRoute('/wireguard-p2p')({
  component: WireGuardP2PPage,
});
