import { createFileRoute, Link } from '@tanstack/react-router';
import { useState, useEffect } from 'react';
import {
  Box,
  Card,
  CardContent,
  Grid,
  Typography,
  Chip,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  CircularProgress,
  TextField,
  Button,
  Alert,
  Stack,
  Dialog,
  DialogTitle,
  DialogContent,
  IconButton,
  Collapse,
} from '@mui/material';
import {
  CheckCircle as CheckIcon,
  Cancel as ErrorIcon,
  Router as RouterIcon,
  Dns as DnsIcon,
  VpnKey as VpnIcon,
  Wifi as WifiIcon,
  Search as SearchIcon,
  AltRoute as RouteIcon,
  Link as LinkIcon,
  Close as CloseIcon,
  ExpandMore as ExpandMoreIcon,
  ExpandLess as ExpandLessIcon,
  Devices as DevicesIcon,
} from '@mui/icons-material';
import { useQuery, useMutation } from '../api/hooks';
import { client } from '../api/client';
import type { RouteLookupResult, WiFiStation } from '../api/client';
import { ServiceStatusCard } from '../components/ServiceStatusCard';

interface ServiceStatusProps {
  name: string;
  running: boolean;
  icon: React.ReactNode;
  link: string;
  detail?: string;
}

function ServiceStatus({ name, running, icon, link, detail }: ServiceStatusProps) {
  return (
    <Link to={link} style={{ textDecoration: 'none', color: 'inherit' }}>
      <Box
        sx={{
          display: 'flex',
          alignItems: 'center',
          gap: 1.5,
          p: 1.5,
          borderRadius: 1,
          bgcolor: 'action.hover',
          '&:hover': { bgcolor: 'action.selected' },
          cursor: 'pointer',
        }}
      >
        <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', width: 24, flexShrink: 0 }}>
          {icon}
        </Box>
        <Box sx={{ flex: 1, minWidth: 0 }}>
          <Typography variant="body2" fontWeight="medium">
            {name}
          </Typography>
          {detail && (
            <Typography variant="caption" color="text.secondary" noWrap>
              {detail}
            </Typography>
          )}
        </Box>
        {running ? (
          <CheckIcon color="success" fontSize="small" />
        ) : (
          <ErrorIcon color="disabled" fontSize="small" />
        )}
      </Box>
    </Link>
  );
}

function Dashboard() {
  const { data: status, isLoading, error } = useQuery('getStatus');
  const { data: interfaces } = useQuery('listInterfaces');
  const { data: routes } = useQuery('getRoutes');
  const { data: p2pStatus } = useQuery('getP2PStatus');
  const { data: wifiStatus } = useQuery('getWiFiStatus');
  const { data: devicesData } = useQuery('listDevices');
  const { mutate: lookupRoute } = useMutation('lookupRoute');

  // Get display name for a MAC address from devices database
  const getDeviceName = (mac: string): string | null => {
    const device = devicesData?.devices?.find(
      d => d.mac.toLowerCase() === mac.toLowerCase()
    );
    if (!device) return null;
    return device.display_name || device.hostname || null;
  };

  // Route lookup modal state
  const [lookupOpen, setLookupOpen] = useState(false);
  const [lookupIP, setLookupIP] = useState('');
  const [lookupResult, setLookupResult] = useState<RouteLookupResult | null>(null);
  const [lookupError, setLookupError] = useState<string | null>(null);

  // WiFi clients state
  const [wifiStations, setWifiStations] = useState<Record<string, WiFiStation[]>>({});
  const [wifiClientsExpanded, setWifiClientsExpanded] = useState(false);

  // Fetch WiFi stations for all running interfaces
  useEffect(() => {
    const fetchStations = async () => {
      if (!wifiStatus?.interfaces) return;
      const newStations: Record<string, WiFiStation[]> = {};
      for (const iface of wifiStatus.interfaces) {
        if (iface.running) {
          try {
            const result = await client.listStations({ interface: iface.interface });
            newStations[iface.interface] = result.stations || [];
          } catch {
            newStations[iface.interface] = [];
          }
        }
      }
      setWifiStations(newStations);
    };
    fetchStations();
  }, [wifiStatus]);

  // Flatten all stations for display
  const allStations = Object.entries(wifiStations).flatMap(([iface, stations]) =>
    stations.map(s => ({ ...s, interface: iface }))
  );

  const formatBytes = (bytes: number): string => {
    if (!bytes || isNaN(bytes) || bytes < 0) return '0 B';
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
    return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
  };

  const handleRouteLookup = async () => {
    if (!lookupIP) return;
    try {
      setLookupError(null);
      const result = await lookupRoute({ destination: lookupIP });
      setLookupResult(result);
    } catch (e) {
      setLookupError(String(e));
      setLookupResult(null);
    }
  };

  const handleCloseLookup = () => {
    setLookupOpen(false);
    setLookupIP('');
    setLookupResult(null);
    setLookupError(null);
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
        <Typography color="error">Failed to load status: {String(error)}</Typography>
      </Box>
    );
  }

  // Count active P2P tunnels
  const activeP2PTunnels = p2pStatus?.tunnels?.filter(t => t.running)?.length || 0;
  const totalP2PTunnels = p2pStatus?.tunnels?.length || 0;

  // WiFi AP status - check if any interface is running
  const wifiRunning = wifiStatus?.interfaces?.some(i => i.running) || false;
  const wifiClients = wifiStatus?.interfaces?.reduce((sum, i) => sum + (i.stationCount || 0), 0) || 0;

  return (
    <Box>
      <Typography variant="h4" gutterBottom>
        Dashboard
      </Typography>

      <Grid container spacing={3}>
        {/* Services Status */}
        <Grid size={{ xs: 12, md: 6 }}>
          <Card>
            <CardContent>
              <Typography variant="h6" color="primary" gutterBottom>
                Services
              </Typography>
              <Stack spacing={1}>
                <ServiceStatus
                  name="DNS / DHCP"
                  running={status?.dnsStatus?.running || false}
                  icon={<DnsIcon color="primary" fontSize="small" />}
                  link="/dns"
                  detail={status?.dnsStatus?.running ? `${status.dnsStatus.dnsEntries ?? 0} DNS, ${status.dnsStatus.leaseCount ?? 0} leases` : undefined}
                />
                <ServiceStatus
                  name="WiFi AP"
                  running={wifiRunning}
                  icon={<WifiIcon color="primary" fontSize="small" />}
                  link="/wifi"
                  detail={wifiRunning ? `${wifiClients} clients` : undefined}
                />
                <ServiceStatus
                  name="WireGuard VPN"
                  running={status?.wireguardStatus?.running || false}
                  icon={<VpnIcon color="primary" fontSize="small" />}
                  link="/wireguard"
                  detail={status?.wireguardStatus?.running ? `${status.wireguardStatus.activePeers ?? 0}/${status.wireguardStatus.peerCount ?? 0} peers` : undefined}
                />
                <ServiceStatus
                  name="Site-to-Site"
                  running={activeP2PTunnels > 0}
                  icon={<LinkIcon color="primary" fontSize="small" />}
                  link="/wireguard-p2p"
                  detail={totalP2PTunnels > 0 ? `${activeP2PTunnels}/${totalP2PTunnels} tunnels` : undefined}
                />
              </Stack>
            </CardContent>
          </Card>
        </Grid>

        {/* System Info */}
        <Grid size={{ xs: 12, md: 6 }}>
          <Card>
            <CardContent>
              <Typography variant="h6" color="primary" gutterBottom>
                System
              </Typography>
              <Table size="small">
                <TableBody>
                  <TableRow>
                    <TableCell sx={{ border: 0, py: 0.5 }}>IP Forwarding</TableCell>
                    <TableCell sx={{ border: 0, py: 0.5 }}>
                      {status?.ipForwardEnabled ? (
                        <Chip label="Enabled" color="success" size="small" />
                      ) : (
                        <Chip label="Disabled" color="error" size="small" />
                      )}
                    </TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell sx={{ border: 0, py: 0.5 }}>WAN</TableCell>
                    <TableCell sx={{ border: 0, py: 0.5 }}>
                      <code>{status?.wanInterface || 'Not set'}</code>
                    </TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell sx={{ border: 0, py: 0.5 }}>Default Route</TableCell>
                    <TableCell sx={{ border: 0, py: 0.5 }}>
                      <code>{status?.defaultInterface || 'None'}</code>
                    </TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </Grid>

        {/* Systemd Service */}
        <Grid size={{ xs: 12, md: 6 }}>
          <ServiceStatusCard />
        </Grid>

        {/* WiFi Clients */}
        {wifiClients > 0 && (
          <Grid size={{ xs: 12, md: 6 }}>
            <Card>
              <CardContent>
                <Box
                  sx={{ display: 'flex', alignItems: 'center', gap: 1, cursor: 'pointer' }}
                  onClick={() => setWifiClientsExpanded(!wifiClientsExpanded)}
                >
                  <DevicesIcon color="primary" />
                  <Typography variant="h6" color="primary" sx={{ flex: 1 }}>
                    WiFi Clients ({wifiClients})
                  </Typography>
                  <IconButton size="small">
                    {wifiClientsExpanded ? <ExpandLessIcon /> : <ExpandMoreIcon />}
                  </IconButton>
                </Box>
                <Collapse in={wifiClientsExpanded}>
                  <TableContainer sx={{ mt: 2, overflowX: 'auto' }}>
                    <Table size="small" sx={{ minWidth: 400 }}>
                      <TableHead>
                        <TableRow>
                          <TableCell>MAC Address</TableCell>
                          <TableCell>Interface</TableCell>
                          <TableCell align="right">Signal</TableCell>
                          <TableCell align="right">RX / TX</TableCell>
                        </TableRow>
                      </TableHead>
                      <TableBody>
                        {allStations.map((station) => {
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
                              <TableCell><Chip label={station.interface} size="small" /></TableCell>
                              <TableCell align="right">{station.signal} dBm</TableCell>
                              <TableCell align="right">{formatBytes(station.rxBytes)} / {formatBytes(station.txBytes)}</TableCell>
                            </TableRow>
                          );
                        })}
                      </TableBody>
                    </Table>
                  </TableContainer>
                </Collapse>
              </CardContent>
            </Card>
          </Grid>
        )}

        {/* Routing Table */}
        <Grid size={12}>
          <Card>
            <CardContent>
              <Box sx={{ display: 'flex', alignItems: 'center', mb: 2, gap: 1 }}>
                <RouteIcon color="primary" />
                <Typography variant="h6" color="primary" sx={{ flex: 1 }}>
                  Routing Table
                </Typography>
                <Button
                  size="small"
                  startIcon={<SearchIcon />}
                  onClick={() => setLookupOpen(true)}
                >
                  Lookup
                </Button>
              </Box>
              <TableContainer>
                <Table size="small">
                  <TableHead>
                    <TableRow>
                      <TableCell>Destination</TableCell>
                      <TableCell>Gateway</TableCell>
                      <TableCell>Interface</TableCell>
                      <TableCell>Metric</TableCell>
                      <TableCell>Protocol</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {routes?.routes?.map((route, i) => (
                      <TableRow key={i}>
                        <TableCell><code>{route.destination}</code></TableCell>
                        <TableCell><code>{route.gateway || '-'}</code></TableCell>
                        <TableCell>
                          <Chip
                            label={route.interface}
                            size="small"
                            color={route.interface.startsWith('wg') ? 'secondary' : 'default'}
                          />
                        </TableCell>
                        <TableCell>{route.metric || '-'}</TableCell>
                        <TableCell>{route.protocol || '-'}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </TableContainer>
            </CardContent>
          </Card>
        </Grid>

        {/* Network Interfaces */}
        <Grid size={12}>
          <Card>
            <CardContent>
              <Box sx={{ display: 'flex', alignItems: 'center', mb: 2, gap: 1 }}>
                <RouterIcon color="primary" />
                <Typography variant="h6" color="primary">
                  Network Interfaces
                </Typography>
              </Box>
              <TableContainer>
                <Table size="small">
                  <TableHead>
                    <TableRow>
                      <TableCell>Name</TableCell>
                      <TableCell>Type</TableCell>
                      <TableCell>Status</TableCell>
                      <TableCell>MAC</TableCell>
                      <TableCell>IP Addresses</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {interfaces?.interfaces
                      ?.filter((iface) => iface.name !== 'lo') // Hide loopback
                      ?.map((iface) => {
                        // Determine if interface is really "up"
                        // WireGuard and virtual interfaces show "unknown" operstate even when working
                        const isUp = iface.state === 'up' ||
                          (iface.state === 'unknown' && (
                            iface.type === 'wireguard' ||
                            iface.type === 'bridge' ||
                            (iface.addresses && iface.addresses.length > 0)
                          ));

                        return (
                          <TableRow key={iface.name}>
                            <TableCell><code>{iface.name}</code></TableCell>
                            <TableCell>{iface.type}</TableCell>
                            <TableCell>
                              {isUp ? (
                                <Chip label="Up" color="success" size="small" />
                              ) : (
                                <Chip label="Down" color="default" size="small" />
                              )}
                            </TableCell>
                            <TableCell><code>{iface.mac}</code></TableCell>
                            <TableCell>
                              {iface.addresses?.map((addr, i) => (
                                <div key={i}><code>{addr.ip}/{addr.prefix}</code></div>
                              ))}
                            </TableCell>
                          </TableRow>
                        );
                      })}
                  </TableBody>
                </Table>
              </TableContainer>
            </CardContent>
          </Card>
        </Grid>
      </Grid>

      {/* Route Lookup Modal */}
      <Dialog open={lookupOpen} onClose={handleCloseLookup} maxWidth="sm" fullWidth>
        <DialogTitle sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <SearchIcon color="primary" />
          Route Lookup
          <IconButton onClick={handleCloseLookup} sx={{ ml: 'auto' }} size="small">
            <CloseIcon />
          </IconButton>
        </DialogTitle>
        <DialogContent>
          <Box sx={{ display: 'flex', gap: 1, mb: 2, mt: 1 }}>
            <TextField
              size="small"
              placeholder="Enter IP address (e.g., 8.8.8.8)"
              value={lookupIP}
              onChange={(e) => setLookupIP(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && handleRouteLookup()}
              sx={{ flex: 1 }}
              autoFocus
            />
            <Button
              variant="contained"
              onClick={handleRouteLookup}
              disabled={!lookupIP}
            >
              Lookup
            </Button>
          </Box>
          {lookupError && (
            <Alert severity="error" sx={{ mb: 2 }}>{lookupError}</Alert>
          )}
          {lookupResult && (
            <Table size="small">
              <TableBody>
                <TableRow>
                  <TableCell sx={{ border: 0 }}>Destination</TableCell>
                  <TableCell sx={{ border: 0 }}><code>{lookupResult.destination}</code></TableCell>
                </TableRow>
                <TableRow>
                  <TableCell sx={{ border: 0 }}>Interface</TableCell>
                  <TableCell sx={{ border: 0 }}>
                    <Chip label={lookupResult.interface} color="primary" size="small" />
                  </TableCell>
                </TableRow>
                {lookupResult.gateway && (
                  <TableRow>
                    <TableCell sx={{ border: 0 }}>Gateway</TableCell>
                    <TableCell sx={{ border: 0 }}><code>{lookupResult.gateway}</code></TableCell>
                  </TableRow>
                )}
                {lookupResult.source && (
                  <TableRow>
                    <TableCell sx={{ border: 0 }}>Source IP</TableCell>
                    <TableCell sx={{ border: 0 }}><code>{lookupResult.source}</code></TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          )}
        </DialogContent>
      </Dialog>
    </Box>
  );
}

export const Route = createFileRoute('/')({
  component: Dashboard,
});
