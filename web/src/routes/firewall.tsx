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
  FormControl,
  InputLabel,
  Select,
  MenuItem,
  FormControlLabel,
  Checkbox,
  IconButton,
  CircularProgress,
  Alert,
} from '@mui/material';
import { Delete as DeleteIcon, Add as AddIcon } from '@mui/icons-material';
import { useQuery, useMutation } from '../api/hooks';

function FirewallPage() {
  const { data: forwardsData, isLoading, refetch } = useQuery('listPortForwards');
  const { mutate: addForward, isLoading: isAdding } = useMutation('addPortForward');
  const { mutate: removeForward, isLoading: isRemoving } = useMutation('removePortForward');

  const [forward, setForward] = useState({
    name: '',
    enabled: true,
    protocol: 'tcp',
    externalPort: 0,
    internalIp: '',
    internalPort: 0,
  });
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  const handleAddForward = async () => {
    if (!forward.name || !forward.externalPort || !forward.internalIp || !forward.internalPort) {
      setError('All fields are required');
      return;
    }
    try {
      await addForward(forward);
      setForward({
        name: '',
        enabled: true,
        protocol: 'tcp',
        externalPort: 0,
        internalIp: '',
        internalPort: 0,
      });
      setSuccess('Port forward added');
      setError(null);
      refetch();
    } catch (e) {
      setError(String(e));
    }
  };

  const handleRemoveForward = async (name: string) => {
    try {
      await removeForward({ name });
      setSuccess('Port forward removed');
      refetch();
    } catch (e) {
      setError(String(e));
    }
  };

  return (
    <Box>
      <Typography variant="h4" gutterBottom>
        Firewall & Port Forwarding
      </Typography>

      {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}
      {success && <Alert severity="success" sx={{ mb: 2 }}>{success}</Alert>}

      <Card sx={{ mb: 3 }}>
        <CardContent>
          <Typography variant="h6" color="primary" gutterBottom>
            Add Port Forward
          </Typography>
          <Grid container spacing={2} alignItems="flex-end">
            <Grid size={{ xs: 12, md: 2 }}>
              <TextField
                fullWidth
                label="Name"
                size="small"
                value={forward.name}
                onChange={(e) => setForward({ ...forward, name: e.target.value })}
                placeholder="Web Server"
              />
            </Grid>
            <Grid size={{ xs: 12, md: 2 }}>
              <FormControl fullWidth size="small">
                <InputLabel>Protocol</InputLabel>
                <Select
                  value={forward.protocol}
                  label="Protocol"
                  onChange={(e) => setForward({ ...forward, protocol: e.target.value })}
                >
                  <MenuItem value="tcp">TCP</MenuItem>
                  <MenuItem value="udp">UDP</MenuItem>
                  <MenuItem value="both">Both</MenuItem>
                </Select>
              </FormControl>
            </Grid>
            <Grid size={{ xs: 12, md: 2 }}>
              <TextField
                fullWidth
                label="External Port"
                size="small"
                type="number"
                value={forward.externalPort || ''}
                onChange={(e) => setForward({ ...forward, externalPort: parseInt(e.target.value) || 0 })}
                placeholder="80"
              />
            </Grid>
            <Grid size={{ xs: 12, md: 2 }}>
              <TextField
                fullWidth
                label="Internal IP"
                size="small"
                value={forward.internalIp}
                onChange={(e) => setForward({ ...forward, internalIp: e.target.value })}
                placeholder="192.168.1.10"
              />
            </Grid>
            <Grid size={{ xs: 12, md: 2 }}>
              <TextField
                fullWidth
                label="Internal Port"
                size="small"
                type="number"
                value={forward.internalPort || ''}
                onChange={(e) => setForward({ ...forward, internalPort: parseInt(e.target.value) || 0 })}
                placeholder="80"
              />
            </Grid>
            <Grid size={{ xs: 12, md: 2 }}>
              <FormControlLabel
                control={
                  <Checkbox
                    checked={forward.enabled}
                    onChange={(e) => setForward({ ...forward, enabled: e.target.checked })}
                  />
                }
                label="Enabled"
              />
            </Grid>
            <Grid size={12}>
              <Button
                variant="contained"
                startIcon={<AddIcon />}
                onClick={handleAddForward}
                disabled={isAdding}
              >
                Add Port Forward
              </Button>
            </Grid>
          </Grid>
        </CardContent>
      </Card>

      <Card>
        <CardContent>
          <Typography variant="h6" color="primary" gutterBottom>
            Port Forwards
          </Typography>
          {isLoading ? (
            <CircularProgress />
          ) : (
            <TableContainer>
              <Table>
                <TableHead>
                  <TableRow>
                    <TableCell>Name</TableCell>
                    <TableCell>Protocol</TableCell>
                    <TableCell>External Port</TableCell>
                    <TableCell>Internal</TableCell>
                    <TableCell>Status</TableCell>
                    <TableCell>Actions</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {forwardsData?.portForwards?.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={6}>
                        <Typography color="text.secondary">No port forwards configured</Typography>
                      </TableCell>
                    </TableRow>
                  ) : (
                    forwardsData?.portForwards?.map((fwd) => (
                      <TableRow key={fwd.name}>
                        <TableCell>{fwd.name}</TableCell>
                        <TableCell>{fwd.protocol.toUpperCase()}</TableCell>
                        <TableCell>{fwd.externalPort}</TableCell>
                        <TableCell>
                          <code>{fwd.internalIp}:{fwd.internalPort}</code>
                        </TableCell>
                        <TableCell>
                          {fwd.enabled ? (
                            <Chip label="Active" color="success" size="small" />
                          ) : (
                            <Chip label="Disabled" color="warning" size="small" />
                          )}
                        </TableCell>
                        <TableCell>
                          <IconButton
                            size="small"
                            color="error"
                            onClick={() => handleRemoveForward(fwd.name)}
                            disabled={isRemoving}
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
          )}
        </CardContent>
      </Card>
    </Box>
  );
}

export const Route = createFileRoute('/firewall')({
  component: FirewallPage,
});
