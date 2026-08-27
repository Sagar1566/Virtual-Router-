import { createFileRoute } from '@tanstack/react-router';
import { useState, useEffect } from 'react';
import {
  Box,
  Card,
  CardContent,
  Grid,
  Typography,
  CircularProgress,
  Switch,
  FormControlLabel,
  TextField,
  Button,
  Select,
  MenuItem,
  FormControl,
  InputLabel,
  Alert,
  Table,
  TableBody,
  TableCell,
  TableRow,
  Chip,
  Stack,
  Divider,
} from '@mui/material';
import {
  Speed as QoSIcon,
  CloudDownload as DownloadIcon,
  CloudUpload as UploadIcon,
  Warning as WarningIcon,
} from '@mui/icons-material';
import { client } from '../api/client';
import type { QoSStatus, QoSConfigRequest } from '../api/client';

// Format bytes to human readable
function formatBytes(bytes: number, decimals = 2): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(decimals)) + ' ' + sizes[i];
}

// Format microseconds to readable
function formatMicroseconds(us: number): string {
  if (us < 1000) return `${us} us`;
  if (us < 1000000) return `${(us / 1000).toFixed(2)} ms`;
  return `${(us / 1000000).toFixed(2)} s`;
}

function QoSPage() {
  const [status, setStatus] = useState<QoSStatus | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  // Form state
  const [enabled, setEnabled] = useState(false);
  const [mode, setMode] = useState('cake');
  const [downloadSpeed, setDownloadSpeed] = useState('100mbit');
  const [uploadSpeed, setUploadSpeed] = useState('20mbit');
  const [perHostFairness, setPerHostFairness] = useState(true);
  const [rttCompensation, setRttCompensation] = useState('internet');
  const [wanType, setWanType] = useState('ethernet');

  const fetchStatus = async () => {
    try {
      const data = await client.getQoSStatus();
      setStatus(data);

      // Update form with current config
      setEnabled(data.enabled);
      if (data.download_speed) setDownloadSpeed(data.download_speed);
      if (data.upload_speed) setUploadSpeed(data.upload_speed);

      setError(null);
    } catch (e) {
      setError(String(e));
    } finally {
      setIsLoading(false);
    }
  };

  useEffect(() => {
    fetchStatus();
    // Poll for status updates
    const interval = setInterval(fetchStatus, 5000);
    return () => clearInterval(interval);
  }, []);

  const handleSave = async () => {
    setIsSaving(true);
    setError(null);
    setSuccess(null);

    const config: QoSConfigRequest = {
      enabled,
      mode,
      download_speed: downloadSpeed,
      upload_speed: uploadSpeed,
      per_host_fairness: perHostFairness,
      rtt_compensation: rttCompensation,
      wan_type: wanType,
    };

    try {
      await client.configureQoS(config);
      setSuccess('QoS configuration applied successfully');
      await fetchStatus();
    } catch (e) {
      setError(String(e));
    } finally {
      setIsSaving(false);
    }
  };

  if (isLoading) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', mt: 4 }}>
        <CircularProgress />
      </Box>
    );
  }

  return (
    <Box>
      <Typography variant="h4" gutterBottom>
        Quality of Service (QoS)
      </Typography>

      {error && (
        <Alert severity="error" sx={{ mb: 2 }} onClose={() => setError(null)}>
          {error}
        </Alert>
      )}

      {success && (
        <Alert severity="success" sx={{ mb: 2 }} onClose={() => setSuccess(null)}>
          {success}
        </Alert>
      )}

      <Grid container spacing={3}>
        {/* Configuration */}
        <Grid size={{ xs: 12, md: 6 }}>
          <Card>
            <CardContent>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 2 }}>
                <QoSIcon color="primary" />
                <Typography variant="h6" color="primary">
                  Configuration
                </Typography>
              </Box>

              <Stack spacing={2}>
                <FormControlLabel
                  control={
                    <Switch
                      checked={enabled}
                      onChange={(e) => setEnabled(e.target.checked)}
                    />
                  }
                  label="Enable QoS"
                />

                <FormControl fullWidth size="small">
                  <InputLabel>Mode</InputLabel>
                  <Select
                    value={mode}
                    label="Mode"
                    onChange={(e) => setMode(e.target.value)}
                    disabled={!enabled}
                  >
                    <MenuItem value="cake">CAKE (Recommended)</MenuItem>
                    <MenuItem value="htb" disabled>HTB (Coming Soon)</MenuItem>
                  </Select>
                </FormControl>

                <Divider />

                <Box sx={{ display: 'flex', gap: 2 }}>
                  <TextField
                    size="small"
                    label="Download Speed"
                    value={downloadSpeed}
                    onChange={(e) => setDownloadSpeed(e.target.value)}
                    disabled={!enabled}
                    fullWidth
                    helperText="e.g., 100mbit, 50mbit, 1gbit"
                    slotProps={{
                      input: {
                        startAdornment: <DownloadIcon color="success" sx={{ mr: 1 }} />,
                      },
                    }}
                  />
                  <TextField
                    size="small"
                    label="Upload Speed"
                    value={uploadSpeed}
                    onChange={(e) => setUploadSpeed(e.target.value)}
                    disabled={!enabled}
                    fullWidth
                    helperText="e.g., 20mbit, 10mbit"
                    slotProps={{
                      input: {
                        startAdornment: <UploadIcon color="primary" sx={{ mr: 1 }} />,
                      },
                    }}
                  />
                </Box>

                <Alert severity="info" icon={<WarningIcon />}>
                  Set speeds to ~90-95% of your actual connection speed for best bufferbloat control.
                </Alert>

                <Divider />

                <FormControlLabel
                  control={
                    <Switch
                      checked={perHostFairness}
                      onChange={(e) => setPerHostFairness(e.target.checked)}
                      disabled={!enabled}
                    />
                  }
                  label="Per-Host Fairness"
                />
                <Typography variant="caption" color="text.secondary" sx={{ mt: -1 }}>
                  Each device on your network gets an equal share of bandwidth
                </Typography>

                <FormControl fullWidth size="small">
                  <InputLabel>RTT Compensation</InputLabel>
                  <Select
                    value={rttCompensation}
                    label="RTT Compensation"
                    onChange={(e) => setRttCompensation(e.target.value)}
                    disabled={!enabled}
                  >
                    <MenuItem value="regional">Regional (low latency)</MenuItem>
                    <MenuItem value="internet">Internet (typical)</MenuItem>
                    <MenuItem value="oceanic">Oceanic (high latency)</MenuItem>
                    <MenuItem value="satellite">Satellite (very high latency)</MenuItem>
                  </Select>
                </FormControl>

                <FormControl fullWidth size="small">
                  <InputLabel>WAN Type</InputLabel>
                  <Select
                    value={wanType}
                    label="WAN Type"
                    onChange={(e) => setWanType(e.target.value)}
                    disabled={!enabled}
                  >
                    <MenuItem value="ethernet">Ethernet</MenuItem>
                    <MenuItem value="docsis">DOCSIS (Cable)</MenuItem>
                    <MenuItem value="pppoe-ptm">PPPoE over PTM (VDSL)</MenuItem>
                    <MenuItem value="pppoe-llc">PPPoE over LLC (ADSL)</MenuItem>
                    <MenuItem value="bridged-ptm">Bridged PTM</MenuItem>
                    <MenuItem value="bridged-llc">Bridged LLC</MenuItem>
                  </Select>
                </FormControl>

                <Button
                  variant="contained"
                  onClick={handleSave}
                  disabled={isSaving}
                  fullWidth
                >
                  {isSaving ? 'Applying...' : 'Apply Configuration'}
                </Button>
              </Stack>
            </CardContent>
          </Card>
        </Grid>

        {/* Status */}
        <Grid size={{ xs: 12, md: 6 }}>
          <Card>
            <CardContent>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 2 }}>
                <QoSIcon color="primary" />
                <Typography variant="h6" color="primary">
                  Status
                </Typography>
                <Chip
                  label={status?.enabled ? 'Active' : 'Disabled'}
                  color={status?.enabled ? 'success' : 'default'}
                  size="small"
                  sx={{ ml: 'auto' }}
                />
              </Box>

              <Table size="small">
                <TableBody>
                  <TableRow>
                    <TableCell sx={{ border: 0, py: 1 }}>Mode</TableCell>
                    <TableCell sx={{ border: 0, py: 1 }}>
                      <Chip
                        label={status?.mode?.toUpperCase() || 'None'}
                        size="small"
                        color={status?.enabled ? 'primary' : 'default'}
                      />
                    </TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell sx={{ border: 0, py: 1 }}>WAN Interface</TableCell>
                    <TableCell sx={{ border: 0, py: 1 }}>
                      <code>{status?.wan_interface || 'Not configured'}</code>
                    </TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell sx={{ border: 0, py: 1 }}>Download Limit</TableCell>
                    <TableCell sx={{ border: 0, py: 1 }}>
                      {status?.download_speed || '-'}
                    </TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell sx={{ border: 0, py: 1 }}>Upload Limit</TableCell>
                    <TableCell sx={{ border: 0, py: 1 }}>
                      {status?.upload_speed || '-'}
                    </TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            </CardContent>
          </Card>

          {/* Egress (Upload) Stats */}
          {status?.wan_qdisc && (
            <Card sx={{ mt: 2 }}>
              <CardContent>
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 2 }}>
                  <UploadIcon color="primary" />
                  <Typography variant="h6" color="primary">
                    Upload Queue ({status.wan_qdisc.type})
                  </Typography>
                </Box>

                <Table size="small">
                  <TableBody>
                    <TableRow>
                      <TableCell sx={{ border: 0, py: 0.5 }}>Bandwidth</TableCell>
                      <TableCell sx={{ border: 0, py: 0.5 }}>
                        {status.wan_qdisc.bandwidth || '-'}
                      </TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell sx={{ border: 0, py: 0.5 }}>Packets</TableCell>
                      <TableCell sx={{ border: 0, py: 0.5 }}>
                        {status.wan_qdisc.packets.toLocaleString()}
                      </TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell sx={{ border: 0, py: 0.5 }}>Bytes</TableCell>
                      <TableCell sx={{ border: 0, py: 0.5 }}>
                        {formatBytes(status.wan_qdisc.bytes)}
                      </TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell sx={{ border: 0, py: 0.5 }}>Dropped</TableCell>
                      <TableCell sx={{ border: 0, py: 0.5 }}>
                        <Chip
                          label={status.wan_qdisc.dropped.toLocaleString()}
                          size="small"
                          color={status.wan_qdisc.dropped > 0 ? 'warning' : 'success'}
                        />
                      </TableCell>
                    </TableRow>
                    {status.wan_qdisc.avg_delay_us !== undefined && status.wan_qdisc.avg_delay_us > 0 && (
                      <TableRow>
                        <TableCell sx={{ border: 0, py: 0.5 }}>Avg Delay</TableCell>
                        <TableCell sx={{ border: 0, py: 0.5 }}>
                          {formatMicroseconds(status.wan_qdisc.avg_delay_us)}
                        </TableCell>
                      </TableRow>
                    )}
                    {status.wan_qdisc.target_delay && (
                      <TableRow>
                        <TableCell sx={{ border: 0, py: 0.5 }}>Target Delay</TableCell>
                        <TableCell sx={{ border: 0, py: 0.5 }}>
                          {status.wan_qdisc.target_delay}
                        </TableCell>
                      </TableRow>
                    )}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>
          )}

          {/* Ingress (Download) Stats */}
          {status?.lan_qdisc && (
            <Card sx={{ mt: 2 }}>
              <CardContent>
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 2 }}>
                  <DownloadIcon color="success" />
                  <Typography variant="h6" color="primary">
                    Download Queue ({status.lan_qdisc.type})
                  </Typography>
                </Box>

                <Table size="small">
                  <TableBody>
                    <TableRow>
                      <TableCell sx={{ border: 0, py: 0.5 }}>Bandwidth</TableCell>
                      <TableCell sx={{ border: 0, py: 0.5 }}>
                        {status.lan_qdisc.bandwidth || '-'}
                      </TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell sx={{ border: 0, py: 0.5 }}>Packets</TableCell>
                      <TableCell sx={{ border: 0, py: 0.5 }}>
                        {status.lan_qdisc.packets.toLocaleString()}
                      </TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell sx={{ border: 0, py: 0.5 }}>Bytes</TableCell>
                      <TableCell sx={{ border: 0, py: 0.5 }}>
                        {formatBytes(status.lan_qdisc.bytes)}
                      </TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell sx={{ border: 0, py: 0.5 }}>Dropped</TableCell>
                      <TableCell sx={{ border: 0, py: 0.5 }}>
                        <Chip
                          label={status.lan_qdisc.dropped.toLocaleString()}
                          size="small"
                          color={status.lan_qdisc.dropped > 0 ? 'warning' : 'success'}
                        />
                      </TableCell>
                    </TableRow>
                    {status.lan_qdisc.avg_delay_us !== undefined && status.lan_qdisc.avg_delay_us > 0 && (
                      <TableRow>
                        <TableCell sx={{ border: 0, py: 0.5 }}>Avg Delay</TableCell>
                        <TableCell sx={{ border: 0, py: 0.5 }}>
                          {formatMicroseconds(status.lan_qdisc.avg_delay_us)}
                        </TableCell>
                      </TableRow>
                    )}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>
          )}
        </Grid>

        {/* Info Card */}
        <Grid size={12}>
          <Card>
            <CardContent>
              <Typography variant="h6" color="primary" gutterBottom>
                About CAKE
              </Typography>
              <Typography variant="body2" color="text.secondary" paragraph>
                CAKE (Common Applications Kept Enhanced) is a modern queuing discipline that combines shaping,
                prioritization, and active queue management. It's designed to solve bufferbloat - the latency
                spikes that occur when buffers fill up during heavy traffic.
              </Typography>
              <Typography variant="body2" color="text.secondary">
                <strong>Key features:</strong>
              </Typography>
              <Box component="ul" sx={{ mt: 1, color: 'text.secondary' }}>
                <li>Automatic queue management to reduce latency</li>
                <li>Per-flow and per-host fairness</li>
                <li>Proper overhead handling for different link types</li>
                <li>ECN support for congestion signaling</li>
                <li>ACK filtering for asymmetric links</li>
              </Box>
            </CardContent>
          </Card>
        </Grid>
      </Grid>
    </Box>
  );
}

export const Route = createFileRoute('/qos')({
  component: QoSPage,
});
