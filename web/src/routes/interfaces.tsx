import { createFileRoute } from '@tanstack/react-router';
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
  Chip,
  Button,
  CircularProgress,
} from '@mui/material';
import { useQuery, useMutation } from '../api/hooks';

function InterfacesPage() {
  const { data: interfacesData, isLoading, error, refetch } = useQuery('listInterfaces');
  const { data: routesData } = useQuery('getRoutes');
  const { mutate: setInterfaceState, isLoading: isUpdating } = useMutation('setInterfaceState');

  const handleToggleInterface = async (name: string, currentState: string) => {
    try {
      await setInterfaceState({ name, up: currentState !== 'up' });
      refetch();
    } catch (e) {
      console.error('Failed to toggle interface:', e);
    }
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
      <Typography color="error">Failed to load interfaces: {String(error)}</Typography>
    );
  }

  return (
    <Box>
      <Typography variant="h4" gutterBottom>
        Network Interfaces
      </Typography>

      <Card sx={{ mb: 3 }}>
        <CardContent>
          <Typography variant="h6" color="primary" gutterBottom>
            Interfaces
          </Typography>
          <TableContainer>
            <Table>
              <TableHead>
                <TableRow>
                  <TableCell>Name</TableCell>
                  <TableCell>Type</TableCell>
                  <TableCell>Status</TableCell>
                  <TableCell>Link</TableCell>
                  <TableCell>MAC</TableCell>
                  <TableCell>Speed</TableCell>
                  <TableCell>IP Addresses</TableCell>
                  <TableCell>Actions</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {interfacesData?.interfaces?.map((iface) => (
                  <TableRow key={iface.name}>
                    <TableCell>
                      <code>{iface.name}</code>
                      {iface.master && (
                        <Typography variant="caption" color="text.secondary" sx={{ ml: 1 }}>
                          ({iface.master})
                        </Typography>
                      )}
                    </TableCell>
                    <TableCell>{iface.type}</TableCell>
                    <TableCell>
                      {iface.state === 'up' ? (
                        <Chip label="Up" color="success" size="small" />
                      ) : (
                        <Chip label="Down" color="default" size="small" />
                      )}
                    </TableCell>
                    <TableCell>
                      {iface.linkState === 'up' ? (
                        <Chip label="Connected" color="success" size="small" variant="outlined" />
                      ) : iface.linkState === 'down' ? (
                        <Chip label="No carrier" color="error" size="small" variant="outlined" />
                      ) : (
                        <Chip label="Unknown" color="warning" size="small" variant="outlined" />
                      )}
                    </TableCell>
                    <TableCell>
                      <code>{iface.mac}</code>
                    </TableCell>
                    <TableCell>
                      {iface.speed ? `${iface.speed} Mbps` : '-'}
                    </TableCell>
                    <TableCell>
                      {iface.addresses?.map((addr, i) => (
                        <div key={i}>
                          <code>{addr.ip}/{addr.prefix}</code>
                        </div>
                      ))}
                    </TableCell>
                    <TableCell>
                      <Button
                        size="small"
                        variant="outlined"
                        color={iface.state === 'up' ? 'warning' : 'success'}
                        onClick={() => handleToggleInterface(iface.name, iface.state)}
                        disabled={isUpdating}
                      >
                        {iface.state === 'up' ? 'Down' : 'Up'}
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </TableContainer>
        </CardContent>
      </Card>

      <Card>
        <CardContent>
          <Typography variant="h6" color="primary" gutterBottom>
            Routing Table
          </Typography>
          <TableContainer>
            <Table>
              <TableHead>
                <TableRow>
                  <TableCell>Destination</TableCell>
                  <TableCell>Gateway</TableCell>
                  <TableCell>Interface</TableCell>
                  <TableCell>Metric</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {routesData?.routes?.map((route, i) => (
                  <TableRow key={i}>
                    <TableCell>
                      <code>{route.destination}</code>
                    </TableCell>
                    <TableCell>
                      {route.gateway ? <code>{route.gateway}</code> : '-'}
                    </TableCell>
                    <TableCell>
                      <code>{route.interface}</code>
                    </TableCell>
                    <TableCell>{route.metric ?? '-'}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </TableContainer>
        </CardContent>
      </Card>
    </Box>
  );
}

export const Route = createFileRoute('/interfaces')({
  component: InterfacesPage,
});
