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
  IconButton,
  CircularProgress,
  Alert,
  Switch,
  FormControlLabel,
  Chip,
  Tooltip,
} from '@mui/material';
import { Delete as DeleteIcon, Add as AddIcon, Save as SaveIcon, PushPin as PushPinIcon } from '@mui/icons-material';
import { useQuery, useMutation } from '../api/hooks';

function DHCPPage() {
  const { data: leasesData, isLoading: leasesLoading } = useQuery('listLeases');
  const { data: reservationsData, isLoading: reservationsLoading, refetch } = useQuery('listReservations');
  const { data: settingsData, isLoading: settingsLoading, refetch: refetchSettings } = useQuery('getDHCPSettings');
  const { data: devicesData } = useQuery('listDevices');
  const { mutate: addReservation, isLoading: isAdding } = useMutation('addReservation');
  const { mutate: removeReservation, isLoading: isRemoving } = useMutation('removeReservation');
  const { mutate: updateSettings, isLoading: isSaving } = useMutation('updateDHCPSettings');

  // Get device info for a MAC address from devices database
  const getDeviceInfo = (mac: string): { displayName: string | null; hostname: string | null } => {
    const device = devicesData?.devices?.find(
      d => d.mac.toLowerCase() === mac.toLowerCase()
    );
    if (!device) return { displayName: null, hostname: null };
    return {
      displayName: device.display_name || null,
      hostname: device.hostname || null,
    };
  };

  const [name, setName] = useState('');
  const [mac, setMac] = useState('');
  const [ip, setIp] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  // DHCP settings state
  const [dhcpEnabled, setDhcpEnabled] = useState(true);
  const [gateway, setGateway] = useState('');
  const [rangeStart, setRangeStart] = useState('');
  const [rangeEnd, setRangeEnd] = useState('');
  const [leaseTime, setLeaseTime] = useState('');

  // Load settings into form when data arrives
  useEffect(() => {
    if (settingsData) {
      setDhcpEnabled(settingsData.enabled);
      setGateway(settingsData.gateway);
      setRangeStart(settingsData.start);
      setRangeEnd(settingsData.end);
      setLeaseTime(settingsData.lease);
    }
  }, [settingsData]);

  const handleSaveSettings = async () => {
    try {
      await updateSettings({
        enabled: dhcpEnabled,
        gateway,
        start: rangeStart,
        end: rangeEnd,
        lease: leaseTime,
      });
      setSuccess('DHCP settings saved');
      setError(null);
      refetchSettings();
    } catch (e) {
      setError(String(e));
    }
  };

  const handleAddReservation = async () => {
    if (!name || !mac || !ip) {
      setError('All fields are required');
      return;
    }
    try {
      await addReservation({ name, mac, ip });
      setName('');
      setMac('');
      setIp('');
      setSuccess('Reservation added');
      setError(null);
      refetch();
    } catch (e) {
      setError(String(e));
    }
  };

  const handleRemoveReservation = async (mac: string) => {
    try {
      await removeReservation({ mac });
      setSuccess('Reservation removed');
      refetch();
    } catch (e) {
      setError(String(e));
    }
  };

  return (
    <Box>
      <Typography variant="h4" gutterBottom>
        DHCP Management
      </Typography>

      {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}
      {success && <Alert severity="success" sx={{ mb: 2 }}>{success}</Alert>}

      <Card sx={{ mb: 3 }}>
        <CardContent>
          <Typography variant="h6" color="primary" gutterBottom>
            DHCP Server Settings
          </Typography>
          {settingsLoading ? (
            <CircularProgress />
          ) : (
            <Box>
              <FormControlLabel
                control={
                  <Switch
                    checked={dhcpEnabled}
                    onChange={(e) => setDhcpEnabled(e.target.checked)}
                  />
                }
                label="DHCP Server Enabled"
                sx={{ mb: 2 }}
              />
              <Grid container spacing={2} sx={{ mb: 2 }}>
                <Grid size={{ xs: 12, sm: 6, md: 3 }}>
                  <TextField
                    label="Gateway IP"
                    fullWidth
                    size="small"
                    value={gateway}
                    onChange={(e) => setGateway(e.target.value)}
                    placeholder="192.168.1.1"
                    helperText="Router's LAN IP address"
                  />
                </Grid>
                <Grid size={{ xs: 12, sm: 6, md: 3 }}>
                  <TextField
                    label="Range Start"
                    fullWidth
                    size="small"
                    value={rangeStart}
                    onChange={(e) => setRangeStart(e.target.value)}
                    placeholder="192.168.1.100"
                  />
                </Grid>
                <Grid size={{ xs: 12, sm: 6, md: 3 }}>
                  <TextField
                    label="Range End"
                    fullWidth
                    size="small"
                    value={rangeEnd}
                    onChange={(e) => setRangeEnd(e.target.value)}
                    placeholder="192.168.1.200"
                  />
                </Grid>
                <Grid size={{ xs: 12, sm: 6, md: 3 }}>
                  <TextField
                    label="Lease Time"
                    fullWidth
                    size="small"
                    value={leaseTime}
                    onChange={(e) => setLeaseTime(e.target.value)}
                    placeholder="24h"
                    helperText="e.g., 1h, 12h, 24h, 7d"
                  />
                </Grid>
              </Grid>
              <Button
                variant="contained"
                startIcon={<SaveIcon />}
                onClick={handleSaveSettings}
                disabled={isSaving}
              >
                Save Settings
              </Button>
            </Box>
          )}
        </CardContent>
      </Card>

      <Card sx={{ mb: 3 }}>
        <CardContent>
          <Typography variant="h6" color="primary" gutterBottom>
            Add DHCP Reservation
          </Typography>
          <Box sx={{ display: 'flex', gap: 2, alignItems: 'flex-end', flexWrap: 'wrap' }}>
            <TextField
              label="Name"
              size="small"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="server"
            />
            <TextField
              label="MAC Address"
              size="small"
              value={mac}
              onChange={(e) => setMac(e.target.value)}
              placeholder="aa:bb:cc:dd:ee:ff"
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
              onClick={handleAddReservation}
              disabled={isAdding}
            >
              Add
            </Button>
          </Box>
        </CardContent>
      </Card>

      <Card>
        <CardContent>
          <Typography variant="h6" color="primary" gutterBottom>
            DHCP Clients
          </Typography>
          {leasesLoading || reservationsLoading ? (
            <CircularProgress />
          ) : (
            <TableContainer>
              <Table size="small">
                <TableHead>
                  <TableRow>
                    <TableCell>Name</TableCell>
                    <TableCell>IP</TableCell>
                    <TableCell>MAC</TableCell>
                    <TableCell>Status</TableCell>
                    <TableCell>Actions</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {(() => {
                    const reservationsByMac = new Map(
                      reservationsData?.reservations?.map((r) => [r.mac.toLowerCase(), r]) || []
                    );
                    const leasesByMac = new Map(
                      leasesData?.leases?.map((l) => [l.mac.toLowerCase(), l]) || []
                    );

                    // Build unified list: all reservations + leases not in reservations
                    const allMacs = new Set([...reservationsByMac.keys(), ...leasesByMac.keys()]);
                    const rows = Array.from(allMacs).map((mac) => {
                      const reservation = reservationsByMac.get(mac);
                      const lease = leasesByMac.get(mac);
                      const deviceInfo = getDeviceInfo(mac);
                      return {
                        mac: reservation?.mac || lease?.mac || mac,
                        // Priority: reservation name > device display_name > device hostname > lease hostname
                        name: reservation?.name || deviceInfo.displayName || deviceInfo.hostname || lease?.hostname || '',
                        ip: reservation?.ip || lease?.ip || '',
                        isStatic: !!reservation,
                        hasLease: !!lease,
                      };
                    });

                    // Sort: static first, then by name/hostname
                    rows.sort((a, b) => {
                      if (a.isStatic !== b.isStatic) return a.isStatic ? -1 : 1;
                      return a.name.localeCompare(b.name);
                    });

                    if (rows.length === 0) {
                      return (
                        <TableRow>
                          <TableCell colSpan={5}>
                            <Typography color="text.secondary">No DHCP clients</Typography>
                          </TableCell>
                        </TableRow>
                      );
                    }

                    return rows.map((row) => (
                      <TableRow key={row.mac}>
                        <TableCell>{row.name || <Typography color="text.secondary" component="span">—</Typography>}</TableCell>
                        <TableCell><code>{row.ip}</code></TableCell>
                        <TableCell><code>{row.mac.toUpperCase()}</code></TableCell>
                        <TableCell>
                          {row.isStatic ? (
                            <Chip label="Static" size="small" color="primary" />
                          ) : (
                            <Chip label="Dynamic" size="small" variant="outlined" />
                          )}
                        </TableCell>
                        <TableCell>
                          {row.isStatic ? (
                            <Tooltip title="Remove reservation">
                              <IconButton
                                size="small"
                                color="error"
                                onClick={() => handleRemoveReservation(row.mac)}
                                disabled={isRemoving}
                              >
                                <DeleteIcon />
                              </IconButton>
                            </Tooltip>
                          ) : (
                            <Tooltip title="Make static reservation">
                              <IconButton
                                size="small"
                                color="primary"
                                onClick={async () => {
                                  try {
                                    await addReservation({ name: row.name || row.mac, mac: row.mac, ip: row.ip });
                                    setSuccess(`Made ${row.name || row.mac} static`);
                                    setError(null);
                                    refetch();
                                  } catch (e) {
                                    setError(String(e));
                                  }
                                }}
                                disabled={isAdding}
                              >
                                <PushPinIcon />
                              </IconButton>
                            </Tooltip>
                          )}
                        </TableCell>
                      </TableRow>
                    ));
                  })()}
                </TableBody>
              </Table>
            </TableContainer>
          )}
        </CardContent>
      </Card>
    </Box>
  );
}

export const Route = createFileRoute('/dhcp')({
  component: DHCPPage,
});
