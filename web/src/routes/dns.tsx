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
} from '@mui/material';
import { Delete as DeleteIcon, Add as AddIcon, Save as SaveIcon } from '@mui/icons-material';
import { useQuery, useMutation } from '../api/hooks';

function DNSPage() {
  const { data: statusData } = useQuery('getDNSStatus');
  const { data: entriesData, isLoading, refetch } = useQuery('listDNSEntries');
  const { data: settingsData, refetch: refetchSettings } = useQuery('getDNSSettings');
  const { mutate: addEntry, isLoading: isAdding } = useMutation('addDNSEntry');
  const { mutate: removeEntry, isLoading: isRemoving } = useMutation('removeDNSEntry');
  const { mutate: updateSettings, isLoading: isSaving } = useMutation('updateDNSSettings');

  const [hostname, setHostname] = useState('');
  const [ip, setIp] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  // DNS settings state
  const [upstreamDns, setUpstreamDns] = useState('');
  const [localZone, setLocalZone] = useState('');

  // Load settings into form when data arrives
  useEffect(() => {
    if (settingsData) {
      setUpstreamDns(settingsData.upstream_dns?.join(', ') || '');
      setLocalZone(settingsData.dns_local_zone || '');
    }
  }, [settingsData]);

  const handleSaveSettings = async () => {
    try {
      const servers = upstreamDns.split(',').map(s => s.trim()).filter(s => s);
      await updateSettings({
        upstream_dns: servers,
        dns_local_zone: localZone,
      });
      setSuccess('DNS settings saved');
      setError(null);
      refetchSettings();
    } catch (e) {
      setError(String(e));
      setSuccess(null);
    }
  };

  const handleAddEntry = async () => {
    if (!hostname || !ip) {
      setError('Both hostname and IP are required');
      return;
    }

    try {
      await addEntry({ hostname, ip });
      setHostname('');
      setIp('');
      setSuccess('DNS entry added');
      setError(null);
      refetch();
    } catch (e) {
      setError(String(e));
      setSuccess(null);
    }
  };

  const handleRemoveEntry = async (hostname: string) => {
    try {
      await removeEntry({ hostname });
      setSuccess('DNS entry removed');
      setError(null);
      refetch();
    } catch (e) {
      setError(String(e));
      setSuccess(null);
    }
  };

  return (
    <Box>
      <Typography variant="h4" gutterBottom>
        DNS Management
      </Typography>

      {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}
      {success && <Alert severity="success" sx={{ mb: 2 }}>{success}</Alert>}

      <Grid container spacing={3}>
        <Grid size={{ xs: 12, md: 6 }}>
          <Card>
            <CardContent>
              <Typography variant="h6" color="primary" gutterBottom>
                DNS Status
              </Typography>
              {statusData ? (
                <Table size="small">
                  <TableBody>
                    <TableRow>
                      <TableCell>Service</TableCell>
                      <TableCell>
                        {statusData.running ? (
                          <Chip label="Running" color="success" size="small" />
                        ) : (
                          <Chip label="Stopped" color="error" size="small" />
                        )}
                      </TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell>Config Valid</TableCell>
                      <TableCell>
                        {statusData.configValid ? (
                          <Chip label="Yes" color="success" size="small" />
                        ) : (
                          <Chip label="No" color="error" size="small" />
                        )}
                      </TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell>DNS Entries</TableCell>
                      <TableCell>{statusData.dnsEntries ?? 0}</TableCell>
                    </TableRow>
                    <TableRow>
                      <TableCell>DHCP Leases</TableCell>
                      <TableCell>{statusData.leaseCount ?? 0}</TableCell>
                    </TableRow>
                  </TableBody>
                </Table>
              ) : (
                <Typography color="text.secondary">Loading...</Typography>
              )}
            </CardContent>
          </Card>
        </Grid>

        <Grid size={{ xs: 12, md: 6 }}>
          <Card>
            <CardContent>
              <Typography variant="h6" color="primary" gutterBottom>
                DNS Settings
              </Typography>
              <TextField
                label="Upstream DNS Servers"
                fullWidth
                size="small"
                value={upstreamDns}
                onChange={(e) => setUpstreamDns(e.target.value)}
                placeholder="8.8.8.8, 1.1.1.1"
                helperText="Comma-separated list of DNS servers for external lookups"
                sx={{ mb: 2 }}
              />
              <TextField
                label="Local Domain"
                fullWidth
                size="small"
                value={localZone}
                onChange={(e) => setLocalZone(e.target.value)}
                placeholder="lan"
                helperText="Domain suffix for local hostnames (e.g., server.lan)"
                sx={{ mb: 2 }}
              />
              <Button
                variant="contained"
                startIcon={<SaveIcon />}
                onClick={handleSaveSettings}
                disabled={isSaving}
              >
                Save Settings
              </Button>
            </CardContent>
          </Card>
        </Grid>

        <Grid size={{ xs: 12, md: 6 }}>
          <Card>
            <CardContent>
              <Typography variant="h6" color="primary" gutterBottom>
                Add DNS Entry
              </Typography>
              <Box sx={{ display: 'flex', gap: 2, alignItems: 'flex-end' }}>
                <TextField
                  label="Hostname"
                  size="small"
                  value={hostname}
                  onChange={(e) => setHostname(e.target.value)}
                  placeholder="server.lan"
                />
                <TextField
                  label="IP Address"
                  size="small"
                  value={ip}
                  onChange={(e) => setIp(e.target.value)}
                  placeholder="192.168.1.10"
                />
                <Button
                  variant="contained"
                  startIcon={<AddIcon />}
                  onClick={handleAddEntry}
                  disabled={isAdding}
                >
                  Add
                </Button>
              </Box>
            </CardContent>
          </Card>
        </Grid>

        <Grid size={{ xs: 12, md: 6 }}>
          <Card>
            <CardContent>
              <Typography variant="h6" color="primary" gutterBottom>
                DNS Entries
              </Typography>
              {isLoading ? (
                <CircularProgress />
              ) : (
                <TableContainer>
                  <Table size="small">
                    <TableHead>
                      <TableRow>
                        <TableCell>Hostname</TableCell>
                        <TableCell>IP Address</TableCell>
                        <TableCell>Actions</TableCell>
                      </TableRow>
                    </TableHead>
                    <TableBody>
                      {entriesData?.entries?.length === 0 ? (
                        <TableRow>
                          <TableCell colSpan={3}>
                            <Typography color="text.secondary">No DNS entries configured</Typography>
                          </TableCell>
                        </TableRow>
                      ) : (
                        entriesData?.entries?.map((entry) => (
                          <TableRow key={entry.hostname}>
                            <TableCell>
                              <code>{entry.hostname}</code>
                            </TableCell>
                            <TableCell>
                              <code>{entry.ip}</code>
                            </TableCell>
                            <TableCell>
                              <IconButton
                                size="small"
                                color="error"
                                onClick={() => handleRemoveEntry(entry.hostname)}
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
      </Grid>
    </Box>
  );
}

export const Route = createFileRoute('/dns')({
  component: DNSPage,
});
