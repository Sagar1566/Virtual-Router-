import { createFileRoute } from '@tanstack/react-router';
import { useState, useEffect, useCallback } from 'react';
import {
  Alert,
  Box,
  Card,
  CardContent,
  Chip,
  CircularProgress,
  List,
  Paper,
  Typography,
  Divider,
  Button,
  Tooltip,
  Collapse,
  IconButton,
  useMediaQuery,
  useTheme,
} from '@mui/material';
import {
  CheckCircle as InstalledIcon,
  Error as MissingIcon,
  Warning as OptionalIcon,
  Refresh as RefreshIcon,
  ContentCopy as CopyIcon,
  Build as FixIcon,
  ExpandMore as ExpandMoreIcon,
  ExpandLess as ExpandLessIcon,
  Info as InfoIcon,
} from '@mui/icons-material';
import { client, type SystemDependency, type WiFiCard, type HealthCheckResult, type HealthIssue } from '../api/client';
import { OperationConfirmModal, type OperationConfig } from '../components/OperationConfirmModal';
import { SetupListItem } from '../components/SetupListItem';
import { useLogModal } from '../context/LogModalContext';

function SetupPage() {
  const theme = useTheme();
  const isMobile = useMediaQuery(theme.breakpoints.down('sm'));

  const [deps, setDeps] = useState<{
    dependencies: SystemDependency[];
    summary: { installed: number; missing: number; optional: number; ready: boolean };
  } | null>(null);
  const [cards, setCards] = useState<{ cards: WiFiCard[] } | null>(null);
  const [healthData, setHealthData] = useState<{
    results: HealthCheckResult[];
    summary: { total: number; ok: number; warnings: number; errors: number; skipped: number; ready: boolean };
  } | null>(null);
  const [depsLoading, setDepsLoading] = useState(true);
  const [cardsLoading, setCardsLoading] = useState(true);
  const [healthLoading, setHealthLoading] = useState(true);
  const [fixingId, setFixingId] = useState<string | null>(null);
  const [expandedChecks, setExpandedChecks] = useState<Set<string>>(new Set());
  const [fixAllLoading, setFixAllLoading] = useState(false);

  // Confirmation modal state
  const [fixConfirmOpen, setFixConfirmOpen] = useState(false);
  const [pendingFixer, setPendingFixer] = useState<{ id: string; description: string } | null>(null);
  const [fixAllConfirmOpen, setFixAllConfirmOpen] = useState(false);

  // Log modal
  const { open: openLogs } = useLogModal();

  const fetchDeps = useCallback(async () => {
    setDepsLoading(true);
    try {
      const result = await client.getSystemDependencies();
      setDeps(result);
    } catch (e) {
      console.error('Failed to fetch dependencies:', e);
    } finally {
      setDepsLoading(false);
    }
  }, []);

  const fetchCards = useCallback(async () => {
    setCardsLoading(true);
    try {
      const result = await client.getWiFiCards(false);
      setCards(result);
    } catch (e) {
      console.error('Failed to fetch WiFi cards:', e);
    } finally {
      setCardsLoading(false);
    }
  }, []);

  const fetchHealth = useCallback(async () => {
    setHealthLoading(true);
    try {
      const result = await client.runHealthChecks();
      setHealthData(result);
      // Auto-expand checks that have issues
      const checksWithIssues = new Set<string>();
      result.results.forEach((check: HealthCheckResult) => {
        if (check.issues && check.issues.length > 0) {
          checksWithIssues.add(check.check_id);
        }
      });
      setExpandedChecks(checksWithIssues);
    } catch (e) {
      console.error('Failed to run health checks:', e);
    } finally {
      setHealthLoading(false);
    }
  }, []);

  // Open confirmation modal for single fixer
  const openFixConfirm = (fixerId: string, description: string) => {
    setPendingFixer({ id: fixerId, description });
    setFixConfirmOpen(true);
  };

  // Preview single fixer (dry run)
  const handleFixPreview = async () => {
    if (!pendingFixer) return;
    openLogs();
    await client.runHealthFix({ fixer_id: pendingFixer.id, dry_run: true });
  };

  // Run single fixer for real
  const handleFixRun = async () => {
    if (!pendingFixer) return;
    openLogs();
    setFixingId(pendingFixer.id);
    try {
      await client.runHealthFix({ fixer_id: pendingFixer.id, dry_run: false });
      // Re-run health checks after fix
      await fetchHealth();
    } catch (e) {
      console.error('Failed to run fixer:', e);
    } finally {
      setFixingId(null);
      setFixConfirmOpen(false);
      setPendingFixer(null);
    }
  };

  // Get all available fixers from health data
  const getAllFixers = (): { fixerId: string; description: string }[] => {
    if (!healthData) return [];
    const fixers: { fixerId: string; description: string }[] = [];
    healthData.results.forEach((check: HealthCheckResult) => {
      check.issues?.forEach((issue: HealthIssue) => {
        if (issue.fixer_id) {
          fixers.push({ fixerId: issue.fixer_id, description: issue.fixer || issue.message });
        }
      });
    });
    return fixers;
  };

  // Preview all fixers (dry run)
  const handleFixAllPreview = async () => {
    const fixers = getAllFixers();
    if (fixers.length === 0) return;
    openLogs();
    for (const fixer of fixers) {
      await client.runHealthFix({ fixer_id: fixer.fixerId, dry_run: true });
    }
  };

  // Run all fixers for real
  const handleFixAllRun = async () => {
    const fixers = getAllFixers();
    if (fixers.length === 0) return;

    openLogs();
    setFixAllLoading(true);
    try {
      for (const fixer of fixers) {
        await client.runHealthFix({ fixer_id: fixer.fixerId, dry_run: false });
      }
      // Re-run health checks after all fixes
      await fetchHealth();
    } catch (e) {
      console.error('Failed to run fixers:', e);
    } finally {
      setFixAllLoading(false);
      setFixAllConfirmOpen(false);
    }
  };

  // Config for single fixer modal
  const fixConfig: OperationConfig = {
    title: 'Run Health Fix',
    description: pendingFixer ? `This will run the fixer: "${pendingFixer.description}"` : '',
    warningMessage: 'This operation may modify system configuration or restart services.',
    runButtonText: 'Run Fix',
    previewButtonText: 'Preview Fix',
  };

  // Config for fix all modal
  const fixAllConfig: OperationConfig = {
    title: 'Run All Fixes',
    description: `This will run ${getAllFixers().length} health fixes to resolve detected issues.`,
    warningMessage: 'This operation will modify system configuration and may restart services.',
    runButtonText: 'Fix All Issues',
    previewButtonText: 'Preview All Fixes',
  };

  const toggleExpanded = (checkId: string) => {
    setExpandedChecks(prev => {
      const next = new Set(prev);
      if (next.has(checkId)) {
        next.delete(checkId);
      } else {
        next.add(checkId);
      }
      return next;
    });
  };

  useEffect(() => {
    fetchDeps();
    fetchCards();
    fetchHealth();
  }, [fetchDeps, fetchCards, fetchHealth]);

  const copyInstallCommand = () => {
    if (!deps) return;
    const missing = deps.dependencies
      .filter((d: SystemDependency) => !d.installed && d.install_cmd)
      .map((d: SystemDependency) => d.install_cmd);
    if (missing.length > 0) {
      const cmd = `sudo ${missing.join(' && sudo ')}`;
      navigator.clipboard.writeText(cmd);
    }
  };

  const getStateIcon = (state: string) => {
    switch (state) {
      case 'ok': return <InstalledIcon color="success" />;
      case 'warning': return <OptionalIcon color="warning" />;
      case 'error': return <MissingIcon color="error" />;
      case 'skipped': return <InfoIcon color="disabled" />;
      default: return <InfoIcon color="disabled" />;
    }
  };

  const getStateColor = (state: string): 'success' | 'warning' | 'error' | 'default' => {
    switch (state) {
      case 'ok': return 'success';
      case 'warning': return 'warning';
      case 'error': return 'error';
      default: return 'default';
    }
  };

  const getSeverityColor = (severity: string): 'info' | 'warning' | 'error' => {
    switch (severity) {
      case 'critical': return 'error';
      case 'warning': return 'warning';
      default: return 'info';
    }
  };

  if (healthLoading && depsLoading) {
    return (
      <Box display="flex" justifyContent="center" alignItems="center" minHeight="200px">
        <CircularProgress />
      </Box>
    );
  }

  const missingRequired = deps?.dependencies.filter((d: SystemDependency) => d.required && !d.installed) || [];
  const hasAllRequired = missingRequired.length === 0;
  const systemReady = healthData?.summary?.ready && hasAllRequired;

  return (
    <>
    <Box>
      <Box display="flex" justifyContent="space-between" alignItems="center" mb={3} flexWrap="wrap" gap={1}>
        <Typography variant="h4">System Setup</Typography>
        <Button
          startIcon={<RefreshIcon />}
          onClick={() => { fetchDeps(); fetchCards(); fetchHealth(); }}
          variant="outlined"
          size={isMobile ? 'small' : 'medium'}
        >
          Refresh
        </Button>
      </Box>

      {/* Status Banner */}
      {systemReady ? (
        <Alert severity="success" sx={{ mb: 3 }}>
          All system checks passed. Your system is ready to run as a router.
        </Alert>
      ) : (
        <Alert severity="warning" sx={{ mb: 3 }}>
          Some system checks failed or have warnings. Review the issues below.
        </Alert>
      )}

      {/* Health Checks Card */}
      <Card sx={{ mb: 3 }}>
        <CardContent sx={{ px: { xs: 1, sm: 2 } }}>
          <Box
            display="flex"
            justifyContent="space-between"
            alignItems={{ xs: 'flex-start', sm: 'center' }}
            flexDirection={{ xs: 'column', sm: 'row' }}
            gap={1}
            mb={2}
          >
            <Typography variant="h6">System Health Checks</Typography>
            <Box display="flex" gap={1} alignItems="center" flexWrap="wrap">
              {healthData?.summary && (
                <>
                  <Chip label={`${healthData.summary.ok} OK`} color="success" size="small" />
                  {healthData.summary.warnings > 0 && (
                    <Chip label={`${healthData.summary.warnings} warn`} color="warning" size="small" />
                  )}
                  {healthData.summary.errors > 0 && (
                    <Chip label={`${healthData.summary.errors} err`} color="error" size="small" />
                  )}
                </>
              )}
              {getAllFixers().length > 0 && (
                <Button
                  variant="contained"
                  color="primary"
                  size="small"
                  startIcon={fixAllLoading ? <CircularProgress size={16} color="inherit" /> : <FixIcon />}
                  onClick={() => setFixAllConfirmOpen(true)}
                  disabled={fixAllLoading || fixingId !== null}
                >
                  Fix All ({getAllFixers().length})
                </Button>
              )}
            </Box>
          </Box>

          {healthLoading ? (
            <CircularProgress size={24} />
          ) : (
            <List disablePadding>
              {healthData?.results.map((check: HealthCheckResult) => {
                const hasIssues = check.issues && check.issues.length > 0;
                const isExpanded = expandedChecks.has(check.check_id);

                return (
                  <Box key={check.check_id}>
                    <SetupListItem
                      icon={getStateIcon(check.state)}
                      name={check.name}
                      chips={[{ label: check.state, color: getStateColor(check.state), variant: 'outlined' }]}
                      secondary={check.message}
                      onClick={hasIssues ? () => toggleExpanded(check.check_id) : undefined}
                      endAction={hasIssues ? (
                        <IconButton
                          size="small"
                          onClick={(e) => { e.stopPropagation(); toggleExpanded(check.check_id); }}
                          sx={{ position: 'absolute', right: 8 }}
                        >
                          {isExpanded ? <ExpandLessIcon /> : <ExpandMoreIcon />}
                        </IconButton>
                      ) : undefined}
                    />

                    {hasIssues && (
                      <Collapse in={isExpanded}>
                        <Box sx={{ pl: { xs: 1, sm: 9 }, pr: { xs: 1, sm: 2 }, pb: 2 }}>
                          {check.issues!.map((issue: HealthIssue, idx: number) => (
                            <Paper
                              key={idx}
                              sx={{
                                p: 1.5,
                                mb: 1,
                                bgcolor: issue.severity === 'critical' ? 'error.dark' :
                                         issue.severity === 'warning' ? 'warning.dark' : 'grey.800',
                                '&:last-child': { mb: 0 },
                              }}
                            >
                              <Box
                                display="flex"
                                justifyContent="space-between"
                                alignItems={{ xs: 'flex-start', sm: 'flex-start' }}
                                flexDirection={{ xs: 'column', sm: 'row' }}
                                gap={1}
                              >
                                <Box flex={1}>
                                  <Typography variant="body2" fontWeight="medium" sx={{ wordBreak: 'break-word' }}>
                                    {issue.message}
                                  </Typography>
                                  {issue.details && (
                                    <Typography
                                      variant="caption"
                                      color="text.secondary"
                                      component="div"
                                      sx={{ mt: 0.5, wordBreak: 'break-word' }}
                                    >
                                      {issue.details}
                                    </Typography>
                                  )}
                                </Box>
                                {issue.fixer_id && (
                                  <Button
                                    size="small"
                                    variant="contained"
                                    color={getSeverityColor(issue.severity)}
                                    startIcon={fixingId === issue.fixer_id ? <CircularProgress size={16} /> : <FixIcon />}
                                    onClick={(e) => {
                                      e.stopPropagation();
                                      openFixConfirm(issue.fixer_id!, issue.fixer || issue.message);
                                    }}
                                    disabled={fixingId !== null}
                                    sx={{ whiteSpace: 'nowrap', flexShrink: 0 }}
                                  >
                                    Fix
                                  </Button>
                                )}
                              </Box>
                            </Paper>
                          ))}
                        </Box>
                      </Collapse>
                    )}
                    <Divider component="li" />
                  </Box>
                );
              })}
            </List>
          )}
        </CardContent>
      </Card>

      {/* Dependencies Card */}
      <Card sx={{ mb: 3 }}>
        <CardContent sx={{ px: { xs: 1, sm: 2 } }}>
          <Box
            display="flex"
            justifyContent="space-between"
            alignItems={{ xs: 'flex-start', sm: 'center' }}
            flexDirection={{ xs: 'column', sm: 'row' }}
            gap={1}
            mb={2}
          >
            <Typography variant="h6">System Dependencies</Typography>
            {deps?.summary && (
              <Box display="flex" gap={1} flexWrap="wrap">
                <Chip
                  label={`${deps.summary.installed} installed`}
                  color="success"
                  size="small"
                />
                {deps.summary.missing > 0 && (
                  <Chip
                    label={`${deps.summary.missing} missing`}
                    color="error"
                    size="small"
                  />
                )}
              </Box>
            )}
          </Box>

          <List disablePadding>
            {deps?.dependencies.map((dep: SystemDependency) => (
              <SetupListItem
                key={dep.name}
                icon={
                  dep.installed ? (
                    <InstalledIcon color="success" />
                  ) : dep.required ? (
                    <MissingIcon color="error" />
                  ) : (
                    <OptionalIcon color="warning" />
                  )
                }
                name={dep.name}
                monospace
                chips={dep.required ? [{ label: 'required', color: 'primary', variant: 'outlined' }] : []}
                secondary={
                  <>
                    {dep.version && (
                      <Typography variant="caption" color="text.secondary" component="span">
                        v{dep.version} —{' '}
                      </Typography>
                    )}
                    {dep.description}
                  </>
                }
              />
            ))}
          </List>

          {missingRequired.length > 0 && (
            <>
              <Divider sx={{ my: 2 }} />
              <Typography variant="subtitle2" gutterBottom>
                Install missing dependencies:
              </Typography>
              <Paper
                sx={{
                  p: 2,
                  bgcolor: 'grey.900',
                  fontFamily: 'monospace',
                  fontSize: { xs: '0.75rem', sm: '0.875rem' },
                  position: 'relative',
                }}
              >
                <Box component="code" sx={{ whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>
                  sudo {missingRequired.map((d: SystemDependency) => d.install_cmd).filter(Boolean).join(' && sudo ')}
                </Box>
                <Tooltip title="Copy to clipboard">
                  <Button
                    size="small"
                    sx={{ position: 'absolute', top: 8, right: 8 }}
                    onClick={copyInstallCommand}
                  >
                    <CopyIcon fontSize="small" />
                  </Button>
                </Tooltip>
              </Paper>
            </>
          )}
        </CardContent>
      </Card>

      {/* WiFi Cards */}
      <Card>
        <CardContent sx={{ px: { xs: 1, sm: 2 } }}>
          <Typography variant="h6" gutterBottom>
            WiFi Adapters
          </Typography>

          {cardsLoading ? (
            <CircularProgress size={24} />
          ) : cards?.cards && cards.cards.length > 0 ? (
            <List disablePadding>
              {cards.cards.map((card: WiFiCard) => (
                <SetupListItem
                  key={card.interface}
                  icon={card.supports_ap ? <InstalledIcon color="success" /> : <OptionalIcon color="warning" />}
                  name={card.interface}
                  chips={[
                    ...(card.supports_ap ? [{ label: 'AP', color: 'success' as const }] : []),
                    ...(card.in_use ? [{ label: 'In use', color: 'primary' as const }] : []),
                  ]}
                  secondary={
                    <Box component="span">
                      <Typography
                        variant="body2"
                        component="span"
                        color="text.secondary"
                        sx={{ fontSize: { xs: '0.75rem', sm: '0.875rem' } }}
                      >
                        {card.driver || 'unknown'} | {card.mac}
                      </Typography>
                      {(card.ht_capable || card.vht_capable || card.he_capable) && (
                        <>
                          <br />
                          <Typography
                            variant="body2"
                            component="span"
                            color="text.secondary"
                            sx={{ fontSize: { xs: '0.75rem', sm: '0.875rem' } }}
                          >
                            {card.ht_capable && 'n '}
                            {card.vht_capable && 'ac '}
                            {card.he_capable && 'ax'}
                          </Typography>
                        </>
                      )}
                    </Box>
                  }
                />
              ))}
            </List>
          ) : (
            <Alert severity="info">
              No WiFi adapters detected. Connect a WiFi adapter to enable access point functionality.
            </Alert>
          )}
        </CardContent>
      </Card>
    </Box>

    {/* Single Fix Confirmation Modal */}
    <OperationConfirmModal
      open={fixConfirmOpen}
      onClose={() => { setFixConfirmOpen(false); setPendingFixer(null); }}
      onPreview={handleFixPreview}
      onRun={handleFixRun}
      config={fixConfig}
    />

    {/* Fix All Confirmation Modal */}
    <OperationConfirmModal
      open={fixAllConfirmOpen}
      onClose={() => setFixAllConfirmOpen(false)}
      onPreview={handleFixAllPreview}
      onRun={handleFixAllRun}
      config={fixAllConfig}
    />
    </>
  );
}

export const Route = createFileRoute('/setup')({
  component: SetupPage,
});
