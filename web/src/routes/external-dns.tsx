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
  Alert,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  Switch,
  FormControlLabel,
  Tooltip,
} from '@mui/material';
import {
  Delete as DeleteIcon,
  Add as AddIcon,
  Sync as SyncIcon,
  Refresh as RefreshIcon,
  Security as SSLIcon,
  Public as PublicIcon,
} from '@mui/icons-material';
import { useQuery, useMutation } from '../api/hooks';
import type { DNSProviderType, DNSProviderConfig, ExternalDNSZone, ExternalDNSRecord } from '../api/client';
import { OperationConfirmModal, type OperationConfig } from '../components/OperationConfirmModal';
import { useLogModal } from '../context/LogModalContext';

const DNS_PROVIDERS: { value: DNSProviderType; label: string }[] = [
  { value: 'cloudflare', label: 'Cloudflare' },
  { value: 'route53', label: 'AWS Route53' },
  { value: 'digitalocean', label: 'DigitalOcean' },
  { value: 'hetzner', label: 'Hetzner' },
  { value: 'gandi', label: 'Gandi' },
  { value: 'googlecloud', label: 'Google Cloud DNS' },
  { value: 'duckdns', label: 'DuckDNS' },
  { value: 'namecom', label: 'Name.com' },
];

function ExternalDNSPage() {
  const { data: status, refetch: refetchStatus } = useQuery('getExternalDNSStatus');
  const { data: settings, refetch: refetchSettings } = useQuery('getExternalDNSSettings');
  const { data: zonesData, refetch: refetchZones } = useQuery('listExternalDNSZones');
  const { data: recordsData, refetch: refetchRecords } = useQuery('listExternalDNSRecords');

  const { mutate: updateSettings } = useMutation('updateExternalDNSSettings');
  const { mutate: refreshIP } = useMutation('refreshPublicIP');
  const { mutate: addZone } = useMutation('addExternalDNSZone');
  const { mutate: deleteZone } = useMutation('deleteExternalDNSZone');
  const { mutate: addRecord } = useMutation('addExternalDNSRecord');
  const { mutate: deleteRecord } = useMutation('deleteExternalDNSRecord');
  const { mutate: syncRecords, isLoading: isSyncing } = useMutation('syncExternalDNS');
  const { mutate: requestCert, isLoading: isRequestingCert } = useMutation('requestSSLCertificate');

  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  const [zoneDialogOpen, setZoneDialogOpen] = useState(false);
  const [recordDialogOpen, setRecordDialogOpen] = useState(false);

  // Confirmation modal state
  const [syncConfirmOpen, setSyncConfirmOpen] = useState(false);
  const [sslConfirmOpen, setSslConfirmOpen] = useState(false);
  const [sslZoneId, setSslZoneId] = useState<string | null>(null);

  // Log modal
  const { open: openLogs } = useLogModal();

  // Zone form state
  const [zoneName, setZoneName] = useState('');
  const [providerType, setProviderType] = useState<DNSProviderType>('cloudflare');
  const [apiToken, setApiToken] = useState('');
  const [awsAccessKey, setAwsAccessKey] = useState('');
  const [awsSecretKey, setAwsSecretKey] = useState('');
  const [awsRegion, setAwsRegion] = useState('us-east-1');
  const [awsProfile, setAwsProfile] = useState('');
  const [sslEnabled, setSslEnabled] = useState(false);
  const [sslEmail, setSslEmail] = useState('');

  // Record form state
  const [recordName, setRecordName] = useState('');
  const [recordType, setRecordType] = useState<'A' | 'AAAA' | 'CNAME' | 'TXT'>('A');
  const [recordValue, setRecordValue] = useState('');
  const [recordTTL, setRecordTTL] = useState(300);
  const [recordZoneId, setRecordZoneId] = useState('');
  const [recordAutoSync, setRecordAutoSync] = useState(true);

  const handleRefreshIP = async () => {
    try {
      await refreshIP(undefined);
      refetchStatus();
      setSuccess('Public IP refreshed');
      setError(null);
    } catch (e) {
      setError(String(e));
      setSuccess(null);
    }
  };

  const handleToggleEnabled = async () => {
    try {
      await updateSettings({ enabled: !settings?.enabled });
      refetchSettings();
      refetchStatus();
      setSuccess(`External DNS ${settings?.enabled ? 'disabled' : 'enabled'}`);
      setError(null);
    } catch (e) {
      setError(String(e));
      setSuccess(null);
    }
  };

  const handleToggleAutoSync = async () => {
    try {
      await updateSettings({ auto_sync_ip: !settings?.auto_sync_ip });
      refetchSettings();
      setSuccess(`Auto-sync ${settings?.auto_sync_ip ? 'disabled' : 'enabled'}`);
      setError(null);
    } catch (e) {
      setError(String(e));
      setSuccess(null);
    }
  };

  const handleAddZone = async () => {
    try {
      const provider: DNSProviderConfig = { type: providerType };

      if (providerType === 'route53') {
        if (awsProfile) {
          provider.aws_profile = awsProfile;
        } else {
          provider.aws_access_key_id = awsAccessKey;
          provider.aws_secret_access_key = awsSecretKey;
        }
        provider.aws_region = awsRegion;
      } else if (providerType === 'cloudflare') {
        provider.cloudflare_api_token = apiToken;
      } else {
        provider.api_token = apiToken;
      }

      await addZone({
        name: zoneName,
        provider,
        ssl_enabled: sslEnabled,
        ssl_email: sslEnabled ? sslEmail : undefined,
      });

      setZoneDialogOpen(false);
      resetZoneForm();
      refetchZones();
      refetchStatus();
      setSuccess('Zone added');
      setError(null);
    } catch (e) {
      setError(String(e));
      setSuccess(null);
    }
  };

  const handleDeleteZone = async (id: string) => {
    try {
      await deleteZone(id);
      refetchZones();
      refetchStatus();
      setSuccess('Zone deleted');
      setError(null);
    } catch (e) {
      setError(String(e));
      setSuccess(null);
    }
  };

  const handleAddRecord = async () => {
    try {
      await addRecord({
        name: recordName,
        type: recordType,
        value: recordValue,
        ttl: recordTTL,
        zone_id: recordZoneId,
        auto_sync: recordAutoSync,
      });

      setRecordDialogOpen(false);
      resetRecordForm();
      refetchRecords();
      refetchStatus();
      setSuccess('Record added');
      setError(null);
    } catch (e) {
      setError(String(e));
      setSuccess(null);
    }
  };

  const handleDeleteRecord = async (id: string) => {
    try {
      await deleteRecord(id);
      refetchRecords();
      refetchStatus();
      setSuccess('Record deleted');
      setError(null);
    } catch (e) {
      setError(String(e));
      setSuccess(null);
    }
  };

  // Sync operation handlers
  const handleSyncPreview = async () => {
    openLogs();
    await syncRecords({ dry_run: true });
  };

  const handleSyncRun = async () => {
    openLogs();
    const result = await syncRecords({ dry_run: false });
    refetchStatus();
    const changed = result.results?.filter((r: { changed?: boolean }) => r.changed).length || 0;
    setSuccess(`Synced ${result.results?.length || 0} records (${changed} changed)`);
    setError(null);
    setSyncConfirmOpen(false);
  };

  const handleOpenSyncConfirm = () => {
    setError(null);
    setSuccess(null);
    setSyncConfirmOpen(true);
  };

  // SSL operation handlers
  const handleSSLPreview = async () => {
    if (!sslZoneId) return;
    openLogs();
    await requestCert({ zone_id: sslZoneId, dry_run: true });
  };

  const handleSSLRun = async () => {
    if (!sslZoneId) return;
    openLogs();
    await requestCert({ zone_id: sslZoneId, dry_run: false });
    refetchZones();
    setSuccess('Certificate requested successfully');
    setError(null);
    setSslConfirmOpen(false);
    setSslZoneId(null);
  };

  const handleOpenSSLConfirm = (zoneId: string) => {
    setError(null);
    setSuccess(null);
    setSslZoneId(zoneId);
    setSslConfirmOpen(true);
  };

  // Sync config for confirmation modal
  const syncConfig: OperationConfig = {
    title: 'Sync DNS Records',
    description: 'This will update your external DNS records with your current public IP address.',
    warningMessage: 'This operation will modify records at your DNS provider. Changes may take time to propagate.',
    runButtonText: 'Sync Now',
    previewButtonText: 'Preview Changes',
  };

  // SSL config for confirmation modal
  const sslConfig: OperationConfig = {
    title: 'Request SSL Certificate',
    description: 'This will request a Let\'s Encrypt SSL certificate for your domain using DNS-01 challenge.',
    warningMessage: 'This will create temporary DNS records for verification and may take a few minutes.',
    runButtonText: 'Request Certificate',
    previewButtonText: 'Preview Process',
  };

  const resetZoneForm = () => {
    setZoneName('');
    setProviderType('cloudflare');
    setApiToken('');
    setAwsAccessKey('');
    setAwsSecretKey('');
    setAwsRegion('us-east-1');
    setAwsProfile('');
    setSslEnabled(false);
    setSslEmail('');
  };

  const resetRecordForm = () => {
    setRecordName('');
    setRecordType('A');
    setRecordValue('');
    setRecordTTL(300);
    setRecordZoneId('');
    setRecordAutoSync(true);
  };

  const zones = zonesData?.zones || [];
  const records = recordsData?.records || [];

  return (
    <Box>
      <Typography variant="h4" gutterBottom>
        External DNS
      </Typography>

      {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}
      {success && <Alert severity="success" sx={{ mb: 2 }}>{success}</Alert>}

      <Grid container spacing={3}>
        {/* Status Card */}
        <Grid size={{ xs: 12, md: 4 }}>
          <Card>
            <CardContent>
              <Typography variant="h6" color="primary" gutterBottom>
                Status
              </Typography>
              <Table size="small">
                <TableBody>
                  <TableRow>
                    <TableCell>Enabled</TableCell>
                    <TableCell>
                      <Switch
                        checked={settings?.enabled || false}
                        onChange={handleToggleEnabled}
                      />
                    </TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell>Public IP</TableCell>
                    <TableCell>
                      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                        <Chip
                          icon={<PublicIcon />}
                          label={status?.public_ip || 'Unknown'}
                          size="small"
                          color={status?.public_ip ? 'success' : 'default'}
                        />
                        <IconButton size="small" onClick={handleRefreshIP}>
                          <RefreshIcon fontSize="small" />
                        </IconButton>
                      </Box>
                    </TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell>Auto-Sync IP</TableCell>
                    <TableCell>
                      <Switch
                        checked={settings?.auto_sync_ip || false}
                        onChange={handleToggleAutoSync}
                      />
                    </TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell>Zones</TableCell>
                    <TableCell>{zones.length}</TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell>Records</TableCell>
                    <TableCell>{records.length}</TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </Grid>

        {/* Quick Actions */}
        <Grid size={{ xs: 12, md: 8 }}>
          <Card>
            <CardContent>
              <Typography variant="h6" color="primary" gutterBottom>
                Quick Actions
              </Typography>
              <Box sx={{ display: 'flex', gap: 2, flexWrap: 'wrap' }}>
                <Button
                  variant="contained"
                  startIcon={<SyncIcon />}
                  onClick={handleOpenSyncConfirm}
                  disabled={isSyncing || records.length === 0}
                >
                  {isSyncing ? 'Syncing...' : 'Sync All Records'}
                </Button>
                <Button
                  variant="outlined"
                  startIcon={<AddIcon />}
                  onClick={() => setZoneDialogOpen(true)}
                >
                  Add Zone
                </Button>
                <Button
                  variant="outlined"
                  startIcon={<AddIcon />}
                  onClick={() => setRecordDialogOpen(true)}
                  disabled={zones.length === 0}
                >
                  Add Record
                </Button>
              </Box>
              <Typography variant="body2" color="text.secondary" sx={{ mt: 2 }}>
                External DNS allows you to automatically update DNS records with your public IP address.
                Configure zones with your DNS provider credentials, then add records to sync.
              </Typography>
            </CardContent>
          </Card>
        </Grid>

        {/* Zones Table */}
        <Grid size={{ xs: 12 }}>
          <Card>
            <CardContent>
              <Typography variant="h6" color="primary" gutterBottom>
                DNS Zones
              </Typography>
              <TableContainer>
                <Table>
                  <TableHead>
                    <TableRow>
                      <TableCell>Zone Name</TableCell>
                      <TableCell>Provider</TableCell>
                      <TableCell>SSL</TableCell>
                      <TableCell>Last Synced</TableCell>
                      <TableCell>Actions</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {zones.length === 0 ? (
                      <TableRow>
                        <TableCell colSpan={5} align="center">
                          No zones configured. Click "Add Zone" to get started.
                        </TableCell>
                      </TableRow>
                    ) : (
                      zones.map((zone: ExternalDNSZone) => (
                        <TableRow key={zone.id}>
                          <TableCell>{zone.name}</TableCell>
                          <TableCell>
                            <Chip
                              label={DNS_PROVIDERS.find(p => p.value === zone.provider.type)?.label || zone.provider.type}
                              size="small"
                            />
                          </TableCell>
                          <TableCell>
                            {zone.ssl_enabled ? (
                              <Tooltip title={zone.cert_path ? 'Certificate exists' : 'No certificate'}>
                                <Chip
                                  icon={<SSLIcon />}
                                  label={zone.cert_path ? 'Active' : 'Pending'}
                                  size="small"
                                  color={zone.cert_path ? 'success' : 'warning'}
                                />
                              </Tooltip>
                            ) : (
                              <Chip label="Disabled" size="small" />
                            )}
                          </TableCell>
                          <TableCell>
                            {zone.last_synced ? new Date(zone.last_synced).toLocaleString() : 'Never'}
                          </TableCell>
                          <TableCell>
                            {zone.ssl_enabled && !zone.cert_path && (
                              <Tooltip title="Request SSL Certificate">
                                <IconButton
                                  size="small"
                                  onClick={() => handleOpenSSLConfirm(zone.id)}
                                  disabled={isRequestingCert}
                                >
                                  <SSLIcon />
                                </IconButton>
                              </Tooltip>
                            )}
                            <IconButton
                              size="small"
                              color="error"
                              onClick={() => handleDeleteZone(zone.id)}
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
            </CardContent>
          </Card>
        </Grid>

        {/* Records Table */}
        <Grid size={{ xs: 12 }}>
          <Card>
            <CardContent>
              <Typography variant="h6" color="primary" gutterBottom>
                DNS Records
              </Typography>
              <TableContainer>
                <Table>
                  <TableHead>
                    <TableRow>
                      <TableCell>Name</TableCell>
                      <TableCell>Type</TableCell>
                      <TableCell>Value</TableCell>
                      <TableCell>TTL</TableCell>
                      <TableCell>Zone</TableCell>
                      <TableCell>Auto-Sync</TableCell>
                      <TableCell>Actions</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {records.length === 0 ? (
                      <TableRow>
                        <TableCell colSpan={7} align="center">
                          No records configured. Add a zone first, then add records.
                        </TableCell>
                      </TableRow>
                    ) : (
                      records.map((rec: ExternalDNSRecord) => (
                        <TableRow key={rec.id}>
                          <TableCell>{rec.name}</TableCell>
                          <TableCell>
                            <Chip label={rec.type} size="small" />
                          </TableCell>
                          <TableCell>
                            {rec.auto_sync && rec.type === 'A' ? (
                              <Chip
                                label={status?.public_ip || 'Auto'}
                                size="small"
                                color="info"
                              />
                            ) : (
                              rec.value || '-'
                            )}
                          </TableCell>
                          <TableCell>{rec.ttl}s</TableCell>
                          <TableCell>
                            {zones.find((z: ExternalDNSZone) => z.id === rec.zone_id)?.name || rec.zone_id}
                          </TableCell>
                          <TableCell>
                            <Chip
                              label={rec.auto_sync ? 'Yes' : 'No'}
                              size="small"
                              color={rec.auto_sync ? 'success' : 'default'}
                            />
                          </TableCell>
                          <TableCell>
                            <IconButton
                              size="small"
                              color="error"
                              onClick={() => handleDeleteRecord(rec.id)}
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
            </CardContent>
          </Card>
        </Grid>
      </Grid>

      {/* Add Zone Dialog */}
      <Dialog open={zoneDialogOpen} onClose={() => setZoneDialogOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>Add DNS Zone</DialogTitle>
        <DialogContent>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 1 }}>
            <TextField
              label="Zone Name"
              value={zoneName}
              onChange={(e) => setZoneName(e.target.value)}
              placeholder="example.com"
              fullWidth
            />
            <FormControl fullWidth>
              <InputLabel>DNS Provider</InputLabel>
              <Select
                value={providerType}
                label="DNS Provider"
                onChange={(e) => setProviderType(e.target.value as DNSProviderType)}
              >
                {DNS_PROVIDERS.map((p) => (
                  <MenuItem key={p.value} value={p.value}>{p.label}</MenuItem>
                ))}
              </Select>
            </FormControl>

            {providerType === 'route53' ? (
              <>
                <TextField
                  label="AWS Profile (optional)"
                  value={awsProfile}
                  onChange={(e) => setAwsProfile(e.target.value)}
                  placeholder="default"
                  helperText="Use AWS credentials file profile, or enter keys below"
                  fullWidth
                />
                <TextField
                  label="AWS Access Key ID"
                  value={awsAccessKey}
                  onChange={(e) => setAwsAccessKey(e.target.value)}
                  disabled={!!awsProfile}
                  fullWidth
                />
                <TextField
                  label="AWS Secret Access Key"
                  value={awsSecretKey}
                  onChange={(e) => setAwsSecretKey(e.target.value)}
                  type="password"
                  disabled={!!awsProfile}
                  fullWidth
                />
                <TextField
                  label="AWS Region"
                  value={awsRegion}
                  onChange={(e) => setAwsRegion(e.target.value)}
                  fullWidth
                />
              </>
            ) : (
              <TextField
                label="API Token"
                value={apiToken}
                onChange={(e) => setApiToken(e.target.value)}
                type="password"
                fullWidth
              />
            )}

            <FormControlLabel
              control={
                <Switch
                  checked={sslEnabled}
                  onChange={(e) => setSslEnabled(e.target.checked)}
                />
              }
              label="Enable Let's Encrypt SSL"
            />

            {sslEnabled && (
              <TextField
                label="Let's Encrypt Email"
                value={sslEmail}
                onChange={(e) => setSslEmail(e.target.value)}
                type="email"
                fullWidth
                required
              />
            )}
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setZoneDialogOpen(false)}>Cancel</Button>
          <Button
            variant="contained"
            onClick={handleAddZone}
            disabled={!zoneName || (providerType !== 'route53' && !apiToken) || (sslEnabled && !sslEmail)}
          >
            Add Zone
          </Button>
        </DialogActions>
      </Dialog>

      {/* Add Record Dialog */}
      <Dialog open={recordDialogOpen} onClose={() => setRecordDialogOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>Add DNS Record</DialogTitle>
        <DialogContent>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 1 }}>
            <FormControl fullWidth>
              <InputLabel>Zone</InputLabel>
              <Select
                value={recordZoneId}
                label="Zone"
                onChange={(e) => setRecordZoneId(e.target.value)}
              >
                {zones.map((z: ExternalDNSZone) => (
                  <MenuItem key={z.id} value={z.id}>{z.name}</MenuItem>
                ))}
              </Select>
            </FormControl>

            <TextField
              label="Record Name"
              value={recordName}
              onChange={(e) => setRecordName(e.target.value)}
              placeholder="vpn.example.com"
              fullWidth
            />

            <FormControl fullWidth>
              <InputLabel>Type</InputLabel>
              <Select
                value={recordType}
                label="Type"
                onChange={(e) => setRecordType(e.target.value as 'A' | 'AAAA' | 'CNAME' | 'TXT')}
              >
                <MenuItem value="A">A (IPv4)</MenuItem>
                <MenuItem value="AAAA">AAAA (IPv6)</MenuItem>
                <MenuItem value="CNAME">CNAME</MenuItem>
                <MenuItem value="TXT">TXT</MenuItem>
              </Select>
            </FormControl>

            <FormControlLabel
              control={
                <Switch
                  checked={recordAutoSync}
                  onChange={(e) => setRecordAutoSync(e.target.checked)}
                />
              }
              label="Auto-sync with public IP (A records)"
            />

            {!recordAutoSync && (
              <TextField
                label="Value"
                value={recordValue}
                onChange={(e) => setRecordValue(e.target.value)}
                placeholder={recordType === 'A' ? '1.2.3.4' : recordType === 'CNAME' ? 'target.example.com' : 'value'}
                fullWidth
              />
            )}

            <TextField
              label="TTL (seconds)"
              value={recordTTL}
              onChange={(e) => setRecordTTL(parseInt(e.target.value) || 300)}
              type="number"
              fullWidth
            />
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setRecordDialogOpen(false)}>Cancel</Button>
          <Button
            variant="contained"
            onClick={handleAddRecord}
            disabled={!recordZoneId || !recordName || (!recordAutoSync && !recordValue)}
          >
            Add Record
          </Button>
        </DialogActions>
      </Dialog>

      {/* Sync Confirmation Modal */}
      <OperationConfirmModal
        open={syncConfirmOpen}
        onClose={() => setSyncConfirmOpen(false)}
        onPreview={handleSyncPreview}
        onRun={handleSyncRun}
        config={syncConfig}
      />

      {/* SSL Certificate Confirmation Modal */}
      <OperationConfirmModal
        open={sslConfirmOpen}
        onClose={() => {
          setSslConfirmOpen(false);
          setSslZoneId(null);
        }}
        onPreview={handleSSLPreview}
        onRun={handleSSLRun}
        config={sslConfig}
      />
    </Box>
  );
}

export const Route = createFileRoute('/external-dns')({
  component: ExternalDNSPage,
});
