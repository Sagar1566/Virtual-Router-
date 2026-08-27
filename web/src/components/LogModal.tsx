import { useState, useEffect, useRef } from 'react';
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  Box,
  Typography,
  IconButton,
  TextField,
  FormControlLabel,
  Switch,
  Chip,
  CircularProgress,
  useMediaQuery,
  useTheme,
} from '@mui/material';
import {
  Close as CloseIcon,
  Delete as ClearIcon,
  VerticalAlignBottom as ScrollToBottomIcon,
} from '@mui/icons-material';
import type { LogEntry } from '../api/client';

interface LogModalProps {
  open: boolean;
  onClose: () => void;
  logs: LogEntry[];
  connected: boolean;
  connecting: boolean;
  onClear: () => void;
  title?: string;
}

function getLevelColor(level: string): string {
  switch (level.toUpperCase()) {
    case 'ERROR':
      return '#f44336';
    case 'WARN':
    case 'WARNING':
      return '#ff9800';
    case 'DEBUG':
      return '#9e9e9e';
    case 'INFO':
    default:
      return '#4caf50';
  }
}

function formatTime(timeStr: string): string {
  try {
    const date = new Date(timeStr);
    return date.toLocaleTimeString();
  } catch {
    return timeStr;
  }
}

export function LogModal({
  open,
  onClose,
  logs,
  connected,
  connecting,
  onClear,
  title = 'System Logs',
}: LogModalProps) {
  const theme = useTheme();
  const isMobile = useMediaQuery(theme.breakpoints.down('sm'));
  const [filter, setFilter] = useState('');
  const [autoScroll, setAutoScroll] = useState(true);
  const logContainerRef = useRef<HTMLDivElement>(null);

  // Auto-scroll to bottom when new logs arrive
  useEffect(() => {
    if (autoScroll && logContainerRef.current) {
      logContainerRef.current.scrollTop = logContainerRef.current.scrollHeight;
    }
  }, [logs, autoScroll]);

  // Sort logs by time (oldest first) and filter based on search
  const sortedLogs = [...logs].sort((a, b) =>
    new Date(a.time).getTime() - new Date(b.time).getTime()
  );

  const filteredLogs = filter
    ? sortedLogs.filter(
        (log) =>
          log.message.toLowerCase().includes(filter.toLowerCase()) ||
          log.level.toLowerCase().includes(filter.toLowerCase())
      )
    : sortedLogs;

  const scrollToBottom = () => {
    if (logContainerRef.current) {
      logContainerRef.current.scrollTop = logContainerRef.current.scrollHeight;
    }
  };

  return (
    <Dialog
      open={open}
      onClose={onClose}
      maxWidth="lg"
      fullWidth
      fullScreen={isMobile}
      PaperProps={{
        sx: { height: isMobile ? '100%' : '80vh', maxHeight: isMobile ? '100%' : '800px' },
      }}
    >
      <DialogTitle sx={{ display: 'flex', alignItems: 'center', gap: 1, pb: 1, px: { xs: 1, sm: 3 } }}>
        <Typography variant="h6" sx={{ flexGrow: 1, fontSize: { xs: '1rem', sm: '1.25rem' } }}>
          {title}
        </Typography>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
          {connecting ? (
            <Chip
              size="small"
              icon={<CircularProgress size={12} />}
              label={isMobile ? '...' : 'Connecting...'}
              color="warning"
            />
          ) : connected ? (
            <Chip size="small" label={isMobile ? 'On' : 'Connected'} color="success" />
          ) : (
            <Chip size="small" label={isMobile ? 'Off' : 'Disconnected'} color="error" />
          )}
        </Box>
        <IconButton onClick={onClose} size="small">
          <CloseIcon />
        </IconButton>
      </DialogTitle>

      <Box sx={{
        px: { xs: 1, sm: 3 },
        pb: 1,
        display: 'flex',
        alignItems: 'center',
        gap: { xs: 0.5, sm: 2 },
        flexWrap: { xs: 'wrap', sm: 'nowrap' },
      }}>
        <TextField
          size="small"
          placeholder="Filter..."
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          sx={{ flexGrow: 1, minWidth: { xs: '100%', sm: 'auto' }, order: { xs: 2, sm: 1 } }}
        />
        <Box sx={{
          display: 'flex',
          alignItems: 'center',
          gap: 0.5,
          order: { xs: 1, sm: 2 },
          width: { xs: '100%', sm: 'auto' },
          justifyContent: { xs: 'flex-end', sm: 'flex-start' },
          mb: { xs: 0.5, sm: 0 },
        }}>
          <FormControlLabel
            control={
              <Switch
                checked={autoScroll}
                onChange={(e) => setAutoScroll(e.target.checked)}
                size="small"
              />
            }
            label={<Typography variant="body2" sx={{ fontSize: { xs: '0.75rem', sm: '0.875rem' } }}>Auto</Typography>}
            sx={{ mr: 0 }}
          />
          <IconButton onClick={scrollToBottom} size="small" title="Scroll to bottom">
            <ScrollToBottomIcon fontSize={isMobile ? 'small' : 'medium'} />
          </IconButton>
          <IconButton onClick={onClear} size="small" title="Clear logs" color="error">
            <ClearIcon fontSize={isMobile ? 'small' : 'medium'} />
          </IconButton>
        </Box>
      </Box>

      <DialogContent sx={{ pt: 1, px: { xs: 1, sm: 3 } }}>
        <Box
          ref={logContainerRef}
          sx={{
            height: '100%',
            overflow: 'auto',
            bgcolor: '#1e1e1e',
            borderRadius: 1,
            p: 1,
            fontFamily: 'monospace',
            fontSize: { xs: '0.75rem', sm: '0.85rem' },
          }}
        >
          {filteredLogs.length === 0 ? (
            <Typography
              sx={{
                color: 'grey.500',
                textAlign: 'center',
                py: 4,
              }}
            >
              {filter ? 'No logs match the filter' : 'No logs yet. Waiting for log entries...'}
            </Typography>
          ) : (
            <Box sx={{ minWidth: 'max-content' }}>
              {filteredLogs.map((log, index) => (
                <Box
                  key={index}
                  sx={{
                    display: 'flex',
                    gap: 1,
                    py: 0.25,
                    whiteSpace: 'nowrap',
                    '&:hover': {
                      bgcolor: 'rgba(255,255,255,0.05)',
                    },
                  }}
                >
                  <Typography
                    component="span"
                    sx={{
                      color: 'grey.500',
                      flexShrink: 0,
                      fontSize: 'inherit',
                      fontFamily: 'inherit',
                    }}
                  >
                    {formatTime(log.time)}
                  </Typography>
                  <Typography
                    component="span"
                    sx={{
                      color: getLevelColor(log.level),
                      flexShrink: 0,
                      minWidth: '50px',
                      fontSize: 'inherit',
                      fontFamily: 'inherit',
                    }}
                  >
                    [{log.level}]
                  </Typography>
                  <Typography
                    component="span"
                    sx={{
                      color: '#e0e0e0',
                      fontSize: 'inherit',
                      fontFamily: 'inherit',
                    }}
                  >
                    {log.message}
                  </Typography>
                </Box>
              ))}
            </Box>
          )}
        </Box>
      </DialogContent>

      <DialogActions sx={{ px: { xs: 1, sm: 3 } }}>
        <Typography variant="body2" color="text.secondary" sx={{ flexGrow: 1, pl: 1, fontSize: { xs: '0.75rem', sm: '0.875rem' } }}>
          {filteredLogs.length} entries
          {filter && ` (of ${logs.length})`}
        </Typography>
        <Button onClick={onClose} size={isMobile ? 'small' : 'medium'}>Close</Button>
      </DialogActions>
    </Dialog>
  );
}
