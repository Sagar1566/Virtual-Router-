import { useState } from 'react';
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  Typography,
  Box,
  Alert,
  CircularProgress,
} from '@mui/material';
import {
  PlayArrow as RunIcon,
  Preview as PreviewIcon,
  Cancel as CancelIcon,
} from '@mui/icons-material';

export interface OperationConfig {
  title: string;
  description: string;
  warningMessage?: string;
  runButtonText?: string;
  previewButtonText?: string;
}

interface OperationConfirmModalProps {
  open: boolean;
  onClose: () => void;
  onPreview: () => Promise<void>;
  onRun: () => Promise<void>;
  config: OperationConfig;
  previewCompleted?: boolean;
}

type OperationState = 'idle' | 'previewing' | 'running' | 'preview-complete' | 'complete' | 'error';

export function OperationConfirmModal({
  open,
  onClose,
  onPreview,
  onRun,
  config,
  previewCompleted = false,
}: OperationConfirmModalProps) {
  const [state, setState] = useState<OperationState>(previewCompleted ? 'preview-complete' : 'idle');
  const [error, setError] = useState<string | null>(null);

  const handlePreview = async () => {
    setState('previewing');
    setError(null);
    try {
      await onPreview();
      setState('preview-complete');
    } catch (e) {
      setState('error');
      setError(e instanceof Error ? e.message : String(e));
    }
  };

  const handleRun = async () => {
    setState('running');
    setError(null);
    try {
      await onRun();
      setState('complete');
      // Auto-close after success
      setTimeout(() => {
        onClose();
        setState('idle');
      }, 1500);
    } catch (e) {
      setState('error');
      setError(e instanceof Error ? e.message : String(e));
    }
  };

  const handleClose = () => {
    if (state !== 'previewing' && state !== 'running') {
      setState('idle');
      setError(null);
      onClose();
    }
  };

  const isLoading = state === 'previewing' || state === 'running';

  return (
    <Dialog open={open} onClose={handleClose} maxWidth="sm" fullWidth>
      <DialogTitle>{config.title}</DialogTitle>
      <DialogContent>
        <Typography variant="body1" sx={{ mb: 2 }}>
          {config.description}
        </Typography>

        {config.warningMessage && (
          <Alert severity="warning" sx={{ mb: 2 }}>
            {config.warningMessage}
          </Alert>
        )}

        {error && (
          <Alert severity="error" sx={{ mb: 2 }}>
            {error}
          </Alert>
        )}

        {state === 'complete' && (
          <Alert severity="success" sx={{ mb: 2 }}>
            Operation completed successfully.
          </Alert>
        )}

        {state === 'preview-complete' && (
          <Alert severity="info" sx={{ mb: 2 }}>
            Preview complete. Check the log window for details. Click "Run Now" to execute for real.
          </Alert>
        )}

        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2, mt: 3 }}>
          {(state === 'idle' || state === 'error') && (
            <>
              <Typography variant="body2" color="text.secondary">
                You can preview this operation first to see what changes will be made, or run it immediately.
              </Typography>
            </>
          )}

          {state === 'previewing' && (
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 2 }}>
              <CircularProgress size={24} />
              <Typography>Running preview (dry-run mode)...</Typography>
            </Box>
          )}

          {state === 'running' && (
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 2 }}>
              <CircularProgress size={24} />
              <Typography>Executing operation...</Typography>
            </Box>
          )}
        </Box>
      </DialogContent>

      <DialogActions sx={{ px: 3, pb: 2, gap: 1 }}>
        <Button
          onClick={handleClose}
          disabled={isLoading}
          startIcon={<CancelIcon />}
        >
          Cancel
        </Button>

        {(state === 'idle' || state === 'error') && (
          <Button
            onClick={handlePreview}
            variant="outlined"
            startIcon={<PreviewIcon />}
            disabled={isLoading}
          >
            {config.previewButtonText || 'Preview (Dry Run)'}
          </Button>
        )}

        {(state === 'idle' || state === 'preview-complete' || state === 'error') && (
          <Button
            onClick={handleRun}
            variant="contained"
            color="primary"
            startIcon={<RunIcon />}
            disabled={isLoading}
          >
            {config.runButtonText || 'Run Now'}
          </Button>
        )}
      </DialogActions>
    </Dialog>
  );
}
