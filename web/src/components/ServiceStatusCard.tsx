import { useState } from 'react';
import {
  Box,
  Card,
  CardContent,
  Typography,
  Button,
  Chip,
  Stack,
  Alert,
  CircularProgress,
  Collapse,
  IconButton,
} from '@mui/material';
import {
  CheckCircle as CheckIcon,
  Cancel as ErrorIcon,
  Warning as WarningIcon,
  Refresh as RefreshIcon,
  PlayArrow as StartIcon,
  RestartAlt as RestartIcon,
  ExpandMore as ExpandMoreIcon,
  ExpandLess as ExpandLessIcon,
} from '@mui/icons-material';
import { useQuery, useMutation } from '../api/hooks';

export function ServiceStatusCard() {
  const { data: status, isLoading, error, refetch } = useQuery('getServiceStatus');
  const { mutate: installService, isLoading: installing } = useMutation('installService');
  const { mutate: enableService, isLoading: enabling } = useMutation('enableService');
  const { mutate: startService, isLoading: starting } = useMutation('startService');
  const { mutate: restartService, isLoading: restarting } = useMutation('restartService');

  const [showDetails, setShowDetails] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const [actionSuccess, setActionSuccess] = useState<string | null>(null);

  const handleAction = async (
    action: () => Promise<unknown>,
    successMsg: string
  ) => {
    setActionError(null);
    setActionSuccess(null);
    try {
      await action();
      setActionSuccess(successMsg);
      refetch();
    } catch (e) {
      setActionError(String(e));
    }
  };

  if (isLoading) {
    return (
      <Card>
        <CardContent>
          <Box sx={{ display: 'flex', justifyContent: 'center', py: 2 }}>
            <CircularProgress size={24} />
          </Box>
        </CardContent>
      </Card>
    );
  }

  if (error) {
    return (
      <Card>
        <CardContent>
          <Alert severity="error">Failed to load service status: {error.message}</Alert>
        </CardContent>
      </Card>
    );
  }

  if (!status) return null;

  const isActionLoading = installing || enabling || starting || restarting;

  // Determine overall status
  const isHealthy = status.installed && status.enabled && !status.needs_update;
  const needsAttention = !status.installed || !status.enabled || status.needs_update;

  return (
    <Card>
      <CardContent>
        <Box sx={{ display: 'flex', alignItems: 'center', mb: 2, gap: 1 }}>
          {isHealthy ? (
            <CheckIcon color="success" />
          ) : needsAttention ? (
            <WarningIcon color="warning" />
          ) : (
            <ErrorIcon color="error" />
          )}
          <Typography variant="h6" color="primary" sx={{ flex: 1 }}>
            Systemd Service
          </Typography>
          <IconButton onClick={() => refetch()} size="small" disabled={isActionLoading}>
            <RefreshIcon />
          </IconButton>
        </Box>

        {actionError && (
          <Alert severity="error" sx={{ mb: 2 }} onClose={() => setActionError(null)}>
            {actionError}
          </Alert>
        )}

        {actionSuccess && (
          <Alert severity="success" sx={{ mb: 2 }} onClose={() => setActionSuccess(null)}>
            {actionSuccess}
          </Alert>
        )}

        <Stack direction="row" spacing={1} sx={{ mb: 2, flexWrap: 'wrap', gap: 1 }}>
          <Chip
            label={status.installed ? 'Installed' : 'Not Installed'}
            color={status.installed ? 'success' : 'default'}
            size="small"
          />
          {status.installed && (
            <>
              <Chip
                label={status.enabled ? 'Auto-start' : 'Manual'}
                color={status.enabled ? 'success' : 'warning'}
                size="small"
              />
              <Chip
                label={status.running ? 'Running' : 'Stopped'}
                color={status.running ? 'success' : 'default'}
                size="small"
              />
              {status.needs_update && (
                <Chip label="Update Available" color="warning" size="small" />
              )}
              {!status.verify_ok && status.verify_output && (
                <Chip label="Config Issue" color="error" size="small" />
              )}
            </>
          )}
        </Stack>

        <Stack direction="row" spacing={1} sx={{ mb: 2 }}>
          {!status.installed && (
            <Button
              size="small"
              variant="contained"
              onClick={() => handleAction(() => installService(undefined as never), 'Service installed')}
              disabled={isActionLoading}
            >
              {installing ? <CircularProgress size={16} /> : 'Install'}
            </Button>
          )}

          {status.installed && status.needs_update && (
            <Button
              size="small"
              variant="contained"
              color="warning"
              onClick={() => handleAction(() => installService(undefined as never), 'Service updated')}
              disabled={isActionLoading}
            >
              {installing ? <CircularProgress size={16} /> : 'Update'}
            </Button>
          )}

          {status.installed && !status.enabled && (
            <Button
              size="small"
              variant="outlined"
              onClick={() => handleAction(() => enableService(undefined as never), 'Service enabled for auto-start')}
              disabled={isActionLoading}
            >
              {enabling ? <CircularProgress size={16} /> : 'Enable Auto-start'}
            </Button>
          )}

          {status.installed && !status.running && (
            <Button
              size="small"
              variant="outlined"
              startIcon={starting ? <CircularProgress size={16} /> : <StartIcon />}
              onClick={() => handleAction(() => startService(undefined as never), 'Service started')}
              disabled={isActionLoading}
            >
              Start
            </Button>
          )}

          {status.installed && status.running && (
            <Button
              size="small"
              variant="outlined"
              startIcon={restarting ? <CircularProgress size={16} /> : <RestartIcon />}
              onClick={() => handleAction(() => restartService(undefined as never), 'Service restarted')}
              disabled={isActionLoading}
            >
              Restart
            </Button>
          )}
        </Stack>

        <Button
          size="small"
          onClick={() => setShowDetails(!showDetails)}
          endIcon={showDetails ? <ExpandLessIcon /> : <ExpandMoreIcon />}
        >
          {showDetails ? 'Hide Details' : 'Show Details'}
        </Button>

        <Collapse in={showDetails}>
          <Box sx={{ mt: 2 }}>
            <Typography variant="body2" color="text.secondary" gutterBottom>
              <strong>Binary:</strong> <code>{status.binary_path}</code>
            </Typography>
            {status.config_path && (
              <Typography variant="body2" color="text.secondary" gutterBottom>
                <strong>Config:</strong> <code>{status.config_path}</code>
              </Typography>
            )}
            <Typography variant="body2" color="text.secondary" gutterBottom>
              <strong>Working Dir:</strong> <code>{status.working_directory}</code>
            </Typography>

            {status.verify_output && (
              <Alert severity="warning" sx={{ mt: 1 }}>
                <Typography variant="caption" component="pre" sx={{ whiteSpace: 'pre-wrap' }}>
                  {status.verify_output}
                </Typography>
              </Alert>
            )}

            {status.needs_update && status.installed && (
              <Alert severity="info" sx={{ mt: 1 }}>
                The installed service file differs from the expected configuration.
                This can happen if the binary location or config path changed.
                Click "Update" to sync the service file.
              </Alert>
            )}
          </Box>
        </Collapse>
      </CardContent>
    </Card>
  );
}
