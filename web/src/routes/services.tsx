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
  Collapse,
  Paper,
  Divider,
} from '@mui/material';
import {
  Delete as DeleteIcon,
  Add as AddIcon,
  Sync as SyncIcon,
  Refresh as RefreshIcon,
  Edit as EditIcon,
  ExpandMore as ExpandMoreIcon,
  ExpandLess as ExpandLessIcon,
  CheckCircle as CheckCircleIcon,
  Error as ErrorIcon,
  Cloud as CloudIcon,
  Dns as DnsIcon,
  Security as SecurityIcon,
} from '@mui/icons-material';
import { useQuery, useMutation } from '../api/hooks';
import type { Service } from '../api/client';

function ServicesPage() {
  const { data: status, refetch: refetchStatus } = useQuery('getServicesStatus');
  const { data: settings, refetch: refetchSettings } = useQuery('getServicesSettings');
  const { data: servicesData, refetch: refetchServices } = useQuery('listServices');
  const { data: zonesData } = useQuery('listExternalDNSZones');
  const { data: haproxyStatus, refetch: refetchHAProxy } = useQuery('getHAProxyStatus');
  const { data: haproxyConfig } = useQuery('getHAProxyConfig');

  const { mutate: updateSettings } = useMutation('updateServicesSettings');
  const { mutate: createService } = useMutation('createService');
  const { mutate: updateService } = useMutation('updateService');
  const { mutate: deleteService } = useMutation('deleteService');
  const { mutate: syncService, isLoading: isSyncing } = useMutation('syncService');
  const { mutate: syncAll, isLoading: isSyncingAll } = useMutation('syncAllServices');
  const { mutate: reloadHAProxy, isLoading: isReloading } = useMutation('reloadHAProxy');

  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  const [serviceDialogOpen, setServiceDialogOpen] = useState(false);
  const [editingService, setEditingService] = useState<Service | null>(null);
  const [haproxyExpanded, setHaproxyExpanded] = useState(false);

  // Service form state
  const [serviceName, setServiceName] = useState('');
  const [dnsName, setDnsName] = useState('');
  const [zoneId, setZoneId] = useState('');
  const [serviceEnabled, setServiceEnabled] = useState(true);

  // Internal DNS
  const [internalEnabled, setInternalEnabled] = useState(false);
  const [internalIP, setInternalIP] = useState('');

  // External DNS
  const [externalEnabled, setExternalEnabled] = useState(false);
  const [externalIP, setExternalIP] = useState('');
  const [externalAutoSync, setExternalAutoSync] = useState(true);

  // Proxy
  const [proxyEnabled, setProxyEnabled] = useState(false);
  const [proxyBackend, setProxyBackend] = useState('');
  const [proxyHealthCheck, setProxyHealthCheck] = useState('');

  // SSL
  const [sslEnabled, setSslEnabled] = useState(false);
  const [sslSubdomains, setSslSubdomains] = useState('');

  const zones = zonesData?.zones || [];
  const services = servicesData?.services || [];

  const resetForm = () => {
    setServiceName('');
    setDnsName('');
    setZoneId(zones.length > 0 ? zones[0].id : '');
    setServiceEnabled(true);
    setInternalEnabled(false);
    setInternalIP('');
    setExternalEnabled(false);
    setExternalIP('');
    setExternalAutoSync(true);
    setProxyEnabled(false);
    setProxyBackend('');
    setProxyHealthCheck('');
    setSslEnabled(false);
    setSslSubdomains('');
    setEditingService(null);
  };

  const loadServiceToForm = (svc: Service) => {
    setServiceName(svc.name);
    setDnsName(svc.dns_name);
    setZoneId(svc.zone_id);
    setServiceEnabled(svc.enabled);

    if (svc.internal_dns) {
      setInternalEnabled(true);
      setInternalIP(svc.internal_dns.ip);
    } else {
      setInternalEnabled(false);
      setInternalIP('');
    }

    if (svc.external_dns) {
      setExternalEnabled(true);
      setExternalIP(svc.external_dns.ip || '');
      setExternalAutoSync(svc.external_dns.auto_sync || false);
    } else {
      setExternalEnabled(false);
      setExternalIP('');
      setExternalAutoSync(true);
    }

    if (svc.proxy) {
      setProxyEnabled(true);
      setProxyBackend(svc.proxy.backend);
      setProxyHealthCheck(svc.proxy.health_check || '');
    } else {
      setProxyEnabled(false);
      setProxyBackend('');
      setProxyHealthCheck('');
    }

    if (svc.ssl) {
      setSslEnabled(svc.ssl.enabled);
      setSslSubdomains(svc.ssl.subdomains?.join(', ') || '');
    } else {
      setSslEnabled(false);
      setSslSubdomains('');
    }

    setEditingService(svc);
  };

  const handleOpenDialog = (svc?: Service) => {
    if (svc) {
      loadServiceToForm(svc);
    } else {
      resetForm();
    }
    setServiceDialogOpen(true);
  };

  const handleCloseDialog = () => {
    setServiceDialogOpen(false);
    resetForm();
  };

  const handleToggleEnabled = async () => {
    try {
      await updateSettings({ enabled: !settings?.enabled });
      refetchSettings();
      refetchStatus();
      setSuccess(`Services ${settings?.enabled ? 'disabled' : 'enabled'}`);
      setError(null);
    } catch (e) {
      setError(String(e));
      setSuccess(null);
    }
  };

  const handleSaveService = async () => {
    try {
      const service: Partial<Service> = {
        name: serviceName,
        dns_name: dnsName,
        zone_id: zoneId,
        enabled: serviceEnabled,
      };

      if (internalEnabled && internalIP) {
        service.internal_dns = { ip: internalIP };
      }

      if (externalEnabled) {
        service.external_dns = {
          ip: externalIP || '',
          auto_sync: externalAutoSync,
        };
      }

      if (proxyEnabled && proxyBackend) {
        service.proxy = {
          backend: proxyBackend,
          health_check: proxyHealthCheck || undefined,
        };
      }

      if (sslEnabled) {
        service.ssl = {
          enabled: true,
          subdomains: sslSubdomains
            .split(',')
            .map((s) => s.trim())
            .filter((s) => s),
        };
      }

      if (editingService) {
        await updateService({ id: editingService.id, service });
        setSuccess('Service updated');
      } else {
        await createService(service);
        setSuccess('Service created');
      }

      refetchServices();
      refetchStatus();
      handleCloseDialog();
      setError(null);
    } catch (e) {
      setError(String(e));
      setSuccess(null);
    }
  };

  const handleDeleteService = async (id: string) => {
    if (!confirm('Are you sure you want to delete this service?')) return;

    try {
      await deleteService(id);
      refetchServices();
      refetchStatus();
      setSuccess('Service deleted');
      setError(null);
    } catch (e) {
      setError(String(e));
      setSuccess(null);
    }
  };

  const handleSyncService = async (id: string) => {
    try {
      const result = await syncService(id);
      if (result.success) {
        setSuccess(`Synced ${result.service_name}`);
      } else {
        setError(result.error || 'Sync failed');
      }
      refetchServices();
      refetchHAProxy();
    } catch (e) {
      setError(String(e));
    }
  };

  const handleSyncAll = async () => {
    try {
      const result = await syncAll(undefined);
      const results = result.results || [];
      const failed = results.filter((r) => !r.success);
      if (failed.length === 0) {
        setSuccess(`Synced ${results.length} services`);
      } else {
        setError(`${failed.length} services failed to sync`);
      }
      refetchServices();
      refetchHAProxy();
    } catch (e) {
      setError(String(e));
    }
  };

  const handleReloadHAProxy = async () => {
    try {
      await reloadHAProxy(undefined);
      refetchHAProxy();
      setSuccess('HAProxy reloaded');
      setError(null);
    } catch (e) {
      setError(String(e));
    }
  };

  const getZoneName = (zoneId: string) => {
    const zone = zones.find((z) => z.id === zoneId);
    return zone?.name || 'Unknown';
  };

  // Set default zone when zones load
  useEffect(() => {
    if (zones.length > 0 && !zoneId) {
      setZoneId(zones[0].id);
    }
  }, [zones, zoneId]);

  return (
    <Box>
      <Typography variant="h5" sx={{ mb: 3 }}>
        Services
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

      {/* Status Card */}
      <Card sx={{ mb: 3 }}>
        <CardContent>
          <Grid container spacing={2} alignItems="center">
            <Grid size={{ xs: 12, md: 6 }}>
              <Typography variant="h6">Services Status</Typography>
              <Box sx={{ display: 'flex', gap: 1, mt: 1, flexWrap: 'wrap' }}>
                <Chip
                  label={settings?.enabled ? 'Enabled' : 'Disabled'}
                  color={settings?.enabled ? 'success' : 'default'}
                  size="small"
                />
                <Chip
                  label={`${status?.service_count || 0} Services`}
                  size="small"
                  variant="outlined"
                />
                <Chip
                  label={`${status?.active_count || 0} Active`}
                  color="primary"
                  size="small"
                  variant="outlined"
                />
                <Chip
                  icon={status?.haproxy_ok ? <CheckCircleIcon /> : <ErrorIcon />}
                  label={status?.haproxy_ok ? 'HAProxy OK' : 'HAProxy Error'}
                  color={status?.haproxy_ok ? 'success' : 'error'}
                  size="small"
                  variant="outlined"
                />
              </Box>
            </Grid>
            <Grid size={{ xs: 12, md: 6 }}>
              <Box sx={{ display: 'flex', gap: 1, justifyContent: 'flex-end', flexWrap: 'wrap' }}>
                <FormControlLabel
                  control={
                    <Switch
                      checked={settings?.enabled || false}
                      onChange={handleToggleEnabled}
                    />
                  }
                  label="Enable Services"
                />
                <Button
                  startIcon={<SyncIcon />}
                  onClick={handleSyncAll}
                  disabled={isSyncingAll || !settings?.enabled}
                >
                  {isSyncingAll ? 'Syncing...' : 'Sync All'}
                </Button>
                <Button
                  variant="contained"
                  startIcon={<AddIcon />}
                  onClick={() => handleOpenDialog()}
                  disabled={zones.length === 0}
                >
                  Add Service
                </Button>
              </Box>
              {zones.length === 0 && (
                <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1, textAlign: 'right' }}>
                  Add a zone in External DNS first
                </Typography>
              )}
            </Grid>
          </Grid>
        </CardContent>
      </Card>

      {/* Services Table */}
      <Card sx={{ mb: 3 }}>
        <CardContent>
          <Typography variant="h6" sx={{ mb: 2 }}>
            Managed Services
          </Typography>
          <TableContainer>
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell>Name</TableCell>
                  <TableCell>Domain</TableCell>
                  <TableCell>Internal DNS</TableCell>
                  <TableCell>External DNS</TableCell>
                  <TableCell>Proxy</TableCell>
                  <TableCell>SSL</TableCell>
                  <TableCell>Status</TableCell>
                  <TableCell align="right">Actions</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {services.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={8} align="center">
                      No services configured
                    </TableCell>
                  </TableRow>
                ) : (
                  services.map((svc) => (
                    <TableRow key={svc.id}>
                      <TableCell>{svc.name}</TableCell>
                      <TableCell>
                        <Typography variant="body2" sx={{ fontFamily: 'monospace' }}>
                          {svc.dns_name}.{getZoneName(svc.zone_id)}
                        </Typography>
                      </TableCell>
                      <TableCell>
                        {svc.internal_dns ? (
                          <Chip
                            icon={<DnsIcon />}
                            label={svc.internal_dns.ip}
                            size="small"
                            variant="outlined"
                          />
                        ) : (
                          <Typography variant="body2" color="text.secondary">-</Typography>
                        )}
                      </TableCell>
                      <TableCell>
                        {svc.external_dns ? (
                          <Chip
                            icon={<CloudIcon />}
                            label={svc.external_dns.auto_sync ? 'Auto' : svc.external_dns.ip || 'Auto'}
                            size="small"
                            variant="outlined"
                          />
                        ) : (
                          <Typography variant="body2" color="text.secondary">-</Typography>
                        )}
                      </TableCell>
                      <TableCell>
                        {svc.proxy ? (
                          <Chip
                            label={svc.proxy.backend.replace(/^https?:\/\//, '').split('/')[0]}
                            size="small"
                            variant="outlined"
                          />
                        ) : (
                          <Typography variant="body2" color="text.secondary">-</Typography>
                        )}
                      </TableCell>
                      <TableCell>
                        {svc.ssl?.enabled ? (
                          <Chip
                            icon={<SecurityIcon />}
                            label={svc.ssl.subdomains?.length ? svc.ssl.subdomains.join(', ') : 'Yes'}
                            size="small"
                            color="success"
                            variant="outlined"
                          />
                        ) : (
                          <Typography variant="body2" color="text.secondary">-</Typography>
                        )}
                      </TableCell>
                      <TableCell>
                        <Chip
                          label={svc.enabled ? 'Active' : 'Disabled'}
                          color={svc.enabled ? 'success' : 'default'}
                          size="small"
                        />
                      </TableCell>
                      <TableCell align="right">
                        <IconButton
                          size="small"
                          onClick={() => handleSyncService(svc.id)}
                          disabled={isSyncing}
                          title="Sync"
                        >
                          <SyncIcon fontSize="small" />
                        </IconButton>
                        <IconButton
                          size="small"
                          onClick={() => handleOpenDialog(svc)}
                          title="Edit"
                        >
                          <EditIcon fontSize="small" />
                        </IconButton>
                        <IconButton
                          size="small"
                          onClick={() => handleDeleteService(svc.id)}
                          color="error"
                          title="Delete"
                        >
                          <DeleteIcon fontSize="small" />
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

      {/* HAProxy Status Card */}
      <Card>
        <CardContent>
          <Box
            sx={{ display: 'flex', alignItems: 'center', cursor: 'pointer' }}
            onClick={() => setHaproxyExpanded(!haproxyExpanded)}
          >
            <Typography variant="h6" sx={{ flexGrow: 1 }}>
              HAProxy Status
            </Typography>
            <Box sx={{ display: 'flex', gap: 1, mr: 2 }}>
              <Chip
                label={haproxyStatus?.installed ? 'Installed' : 'Not Installed'}
                color={haproxyStatus?.installed ? 'success' : 'error'}
                size="small"
              />
              <Chip
                label={haproxyStatus?.running ? 'Running' : 'Stopped'}
                color={haproxyStatus?.running ? 'success' : 'default'}
                size="small"
              />
              <Chip
                label={haproxyStatus?.config_valid ? 'Config Valid' : 'Config Invalid'}
                color={haproxyStatus?.config_valid ? 'success' : 'error'}
                size="small"
                variant="outlined"
              />
            </Box>
            <IconButton size="small">
              {haproxyExpanded ? <ExpandLessIcon /> : <ExpandMoreIcon />}
            </IconButton>
          </Box>

          <Collapse in={haproxyExpanded}>
            <Divider sx={{ my: 2 }} />
            <Box sx={{ display: 'flex', gap: 1, mb: 2 }}>
              <Button
                startIcon={<RefreshIcon />}
                onClick={handleReloadHAProxy}
                disabled={isReloading || !haproxyStatus?.installed}
              >
                {isReloading ? 'Reloading...' : 'Reload HAProxy'}
              </Button>
            </Box>
            {haproxyStatus?.version && (
              <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                Version: {haproxyStatus.version}
              </Typography>
            )}
            {haproxyStatus?.error && (
              <Alert severity="error" sx={{ mb: 2 }}>
                {haproxyStatus.error}
              </Alert>
            )}
            {haproxyConfig?.config && (
              <Paper sx={{ p: 2, bgcolor: 'grey.100', maxHeight: 400, overflow: 'auto' }}>
                <Typography
                  component="pre"
                  sx={{ fontFamily: 'monospace', fontSize: '0.75rem', whiteSpace: 'pre-wrap', m: 0 }}
                >
                  {haproxyConfig.config}
                </Typography>
              </Paper>
            )}
          </Collapse>
        </CardContent>
      </Card>

      {/* Add/Edit Service Dialog */}
      <Dialog open={serviceDialogOpen} onClose={handleCloseDialog} maxWidth="md" fullWidth>
        <DialogTitle>{editingService ? 'Edit Service' : 'Add Service'}</DialogTitle>
        <DialogContent>
          <Box sx={{ pt: 1 }}>
            <Grid container spacing={2}>
              {/* Basic Info */}
              <Grid size={12}>
                <Typography variant="subtitle2" sx={{ mb: 1 }}>
                  Basic Information
                </Typography>
              </Grid>
              <Grid size={{ xs: 12, md: 6 }}>
                <TextField
                  label="Service Name"
                  value={serviceName}
                  onChange={(e) => setServiceName(e.target.value)}
                  fullWidth
                  required
                  placeholder="e.g., Home Assistant"
                />
              </Grid>
              <Grid size={{ xs: 12, md: 6 }}>
                <FormControlLabel
                  control={
                    <Switch
                      checked={serviceEnabled}
                      onChange={(e) => setServiceEnabled(e.target.checked)}
                    />
                  }
                  label="Enabled"
                />
              </Grid>
              <Grid size={{ xs: 12, md: 6 }}>
                <TextField
                  label="DNS Name (subdomain)"
                  value={dnsName}
                  onChange={(e) => setDnsName(e.target.value)}
                  fullWidth
                  required
                  placeholder="e.g., homeassistant"
                  helperText="Just the subdomain, without the zone"
                />
              </Grid>
              <Grid size={{ xs: 12, md: 6 }}>
                <FormControl fullWidth>
                  <InputLabel>Zone</InputLabel>
                  <Select
                    value={zoneId}
                    onChange={(e) => setZoneId(e.target.value)}
                    label="Zone"
                    required
                  >
                    {zones.map((zone) => (
                      <MenuItem key={zone.id} value={zone.id}>
                        {zone.name}
                      </MenuItem>
                    ))}
                  </Select>
                </FormControl>
              </Grid>

              {/* Internal DNS */}
              <Grid size={12}>
                <Divider sx={{ my: 1 }} />
                <FormControlLabel
                  control={
                    <Switch
                      checked={internalEnabled}
                      onChange={(e) => setInternalEnabled(e.target.checked)}
                    />
                  }
                  label="Internal DNS (dnsmasq)"
                />
              </Grid>
              {internalEnabled && (
                <Grid size={{ xs: 12, md: 6 }}>
                  <TextField
                    label="Internal IP"
                    value={internalIP}
                    onChange={(e) => setInternalIP(e.target.value)}
                    fullWidth
                    placeholder="e.g., 192.168.1.50"
                    helperText="IP address for internal DNS resolution"
                  />
                </Grid>
              )}

              {/* External DNS */}
              <Grid size={12}>
                <Divider sx={{ my: 1 }} />
                <FormControlLabel
                  control={
                    <Switch
                      checked={externalEnabled}
                      onChange={(e) => setExternalEnabled(e.target.checked)}
                    />
                  }
                  label="External DNS"
                />
              </Grid>
              {externalEnabled && (
                <>
                  <Grid size={{ xs: 12, md: 6 }}>
                    <TextField
                      label="External IP (optional)"
                      value={externalIP}
                      onChange={(e) => setExternalIP(e.target.value)}
                      fullWidth
                      placeholder="Leave empty for auto-detect"
                      helperText="IP for external DNS record, or leave empty for public IP"
                    />
                  </Grid>
                  <Grid size={{ xs: 12, md: 6 }}>
                    <FormControlLabel
                      control={
                        <Switch
                          checked={externalAutoSync}
                          onChange={(e) => setExternalAutoSync(e.target.checked)}
                        />
                      }
                      label="Auto-sync with public IP"
                    />
                  </Grid>
                </>
              )}

              {/* Proxy */}
              <Grid size={12}>
                <Divider sx={{ my: 1 }} />
                <FormControlLabel
                  control={
                    <Switch
                      checked={proxyEnabled}
                      onChange={(e) => setProxyEnabled(e.target.checked)}
                    />
                  }
                  label="HAProxy Backend"
                />
              </Grid>
              {proxyEnabled && (
                <>
                  <Grid size={{ xs: 12, md: 6 }}>
                    <TextField
                      label="Backend URL"
                      value={proxyBackend}
                      onChange={(e) => setProxyBackend(e.target.value)}
                      fullWidth
                      required
                      placeholder="e.g., http://192.168.1.50:8123"
                      helperText="URL of the backend service"
                    />
                  </Grid>
                  <Grid size={{ xs: 12, md: 6 }}>
                    <TextField
                      label="Health Check Path (optional)"
                      value={proxyHealthCheck}
                      onChange={(e) => setProxyHealthCheck(e.target.value)}
                      fullWidth
                      placeholder="e.g., /api/health"
                      helperText="Path for HTTP health checks"
                    />
                  </Grid>
                </>
              )}

              {/* SSL */}
              <Grid size={12}>
                <Divider sx={{ my: 1 }} />
                <FormControlLabel
                  control={
                    <Switch
                      checked={sslEnabled}
                      onChange={(e) => setSslEnabled(e.target.checked)}
                    />
                  }
                  label="SSL Certificate"
                />
              </Grid>
              {sslEnabled && (
                <Grid size={12}>
                  <TextField
                    label="Subdomains (comma-separated)"
                    value={sslSubdomains}
                    onChange={(e) => setSslSubdomains(e.target.value)}
                    fullWidth
                    placeholder="e.g., *, *.api"
                    helperText="Wildcard subdomains for SSL certificate (e.g., * for *.service.zone.com)"
                  />
                </Grid>
              )}
            </Grid>
          </Box>
        </DialogContent>
        <DialogActions>
          <Button onClick={handleCloseDialog}>Cancel</Button>
          <Button
            onClick={handleSaveService}
            variant="contained"
            disabled={!serviceName || !dnsName || !zoneId}
          >
            {editingService ? 'Save' : 'Create'}
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}

export const Route = createFileRoute('/services')({
  component: ServicesPage,
});
