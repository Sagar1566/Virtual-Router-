import { createFileRoute, useNavigate } from '@tanstack/react-router';
import {
  Box,
  Card,
  CardContent,
  TextField,
  Button,
  Typography,
  Alert,
  CircularProgress,
} from '@mui/material';
import { LockOutlined as LockIcon } from '@mui/icons-material';
import { useState } from 'react';
import { client } from '../api/client';

function LoginPage() {
  const navigate = useNavigate();
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setLoading(true);

    try {
      const result = await client.login({ password });
      if (result.success) {
        navigate({ to: '/' });
      } else {
        setError(result.error || 'Login failed');
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Login failed');
    } finally {
      setLoading(false);
    }
  };

  return (
    <Box
      sx={{
        minHeight: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        bgcolor: 'background.default',
        p: 2,
      }}
    >
      <Card
        sx={{
          maxWidth: 400,
          width: '100%',
          bgcolor: 'background.paper',
        }}
      >
        <CardContent sx={{ p: 4 }}>
          <Box
            sx={{
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'center',
              mb: 3,
            }}
          >
            <Box
              sx={{
                width: 56,
                height: 56,
                borderRadius: '50%',
                bgcolor: 'primary.main',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                mb: 2,
              }}
            >
              <LockIcon sx={{ fontSize: 28, color: 'white' }} />
            </Box>
            <Typography variant="h5" component="h1" gutterBottom>
              Ubuntu Router
            </Typography>
            <Typography variant="body2" color="text.secondary">
              Enter your admin password to continue
            </Typography>
          </Box>

          {error && (
            <Alert severity="error" sx={{ mb: 2 }}>
              {error}
            </Alert>
          )}

          <form onSubmit={handleSubmit}>
            <TextField
              fullWidth
              type="password"
              label="Admin Password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              disabled={loading}
              autoFocus
              sx={{ mb: 3 }}
            />
            <Button
              fullWidth
              type="submit"
              variant="contained"
              size="large"
              disabled={loading || !password}
              sx={{ py: 1.5 }}
            >
              {loading ? <CircularProgress size={24} /> : 'Sign In'}
            </Button>
          </form>

          <Typography
            variant="caption"
            color="text.secondary"
            sx={{ display: 'block', mt: 3, textAlign: 'center' }}
          >
            The admin password is shown in the console when the router starts.
          </Typography>
        </CardContent>
      </Card>
    </Box>
  );
}

export const Route = createFileRoute('/login')({
  component: LoginPage,
});
