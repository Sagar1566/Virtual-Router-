import { useState, useEffect, useCallback } from 'react';
import {
  Alert,
  AlertTitle,
  Box,
  Button,
  LinearProgress,
  Stack,
  Typography,
} from '@mui/material';
import {
  CheckCircle as ConfirmIcon,
  Cancel as CancelIcon,
  Warning as WarningIcon,
} from '@mui/icons-material';
import { client } from '../api/client';
import type { PendingStatus } from '../api/client';

interface PendingConfirmBannerProps {
  // Called when changes are confirmed
  onConfirmed?: () => void;
  // Called when changes are cancelled or rolled back
  onCancelled?: () => void;
  // Poll interval in ms (default: 1000)
  pollInterval?: number;
}

export function PendingConfirmBanner({
  onConfirmed,
  onCancelled,
  pollInterval = 1000,
}: PendingConfirmBannerProps) {
  const [pending, setPending] = useState<PendingStatus | null>(null);
  const [isConfirming, setIsConfirming] = useState(false);
  const [isCancelling, setIsCancelling] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Poll for pending status
  const checkPending = useCallback(async () => {
    try {
      const status = await client.getPendingStatus();
      setPending(status);

      // If was pending but now isn't, it was rolled back (timer expired)
      if (!status.pending && pending?.pending) {
        onCancelled?.();
      }
    } catch (e) {
      console.error('Failed to check pending status:', e);
    }
  }, [pending?.pending, onCancelled]);

  // Start polling when component mounts
  useEffect(() => {
    checkPending();
    const interval = setInterval(checkPending, pollInterval);
    return () => clearInterval(interval);
  }, [checkPending, pollInterval]);

  const handleConfirm = async () => {
    setIsConfirming(true);
    setError(null);
    try {
      const result = await client.confirmChanges();
      if (result.success) {
        setPending({ pending: false });
        onConfirmed?.();
      } else {
        setError(result.error || 'Failed to confirm changes');
      }
    } catch (e) {
      setError(String(e));
    } finally {
      setIsConfirming(false);
    }
  };

  const handleCancel = async () => {
    setIsCancelling(true);
    setError(null);
    try {
      const result = await client.cancelChanges();
      if (result.success) {
        setPending({ pending: false });
        onCancelled?.();
      } else {
        setError(result.error || 'Failed to cancel changes');
      }
    } catch (e) {
      setError(String(e));
    } finally {
      setIsCancelling(false);
    }
  };

  // Don't render if nothing is pending
  if (!pending?.pending) {
    return null;
  }

  const secondsLeft = pending.seconds_left ?? 0;
  const progress = Math.max(0, (secondsLeft / 30) * 100);

  return (
    <Alert
      severity="warning"
      icon={<WarningIcon />}
      sx={{ mb: 2 }}
    >
      <AlertTitle>Configuration Change Pending</AlertTitle>
      <Box>
        <Typography variant="body2" gutterBottom>
          Changes have been applied. Confirm within <strong>{secondsLeft} seconds</strong> or
          they will be automatically reverted to protect network connectivity.
        </Typography>

        <LinearProgress
          variant="determinate"
          value={progress}
          color={secondsLeft <= 10 ? 'error' : 'warning'}
          sx={{ my: 1.5, height: 8, borderRadius: 4 }}
        />

        {pending.reason && (
          <Typography variant="caption" color="text.secondary" display="block" gutterBottom>
            Change type: {pending.reason}
          </Typography>
        )}

        {error && (
          <Typography color="error" variant="body2" gutterBottom>
            {error}
          </Typography>
        )}

        <Stack direction="row" spacing={2} sx={{ mt: 2 }}>
          <Button
            variant="contained"
            color="success"
            startIcon={<ConfirmIcon />}
            onClick={handleConfirm}
            disabled={isConfirming || isCancelling}
          >
            {isConfirming ? 'Confirming...' : 'Confirm Changes'}
          </Button>
          <Button
            variant="outlined"
            color="error"
            startIcon={<CancelIcon />}
            onClick={handleCancel}
            disabled={isConfirming || isCancelling}
          >
            {isCancelling ? 'Cancelling...' : 'Cancel & Rollback'}
          </Button>
        </Stack>
      </Box>
    </Alert>
  );
}

// Hook for checking if there are pending changes
export function usePendingStatus() {
  const [pending, setPending] = useState<PendingStatus | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  const refresh = useCallback(async () => {
    try {
      const status = await client.getPendingStatus();
      setPending(status);
    } catch (e) {
      console.error('Failed to check pending status:', e);
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  return { pending, isLoading, refresh };
}

export default PendingConfirmBanner;
