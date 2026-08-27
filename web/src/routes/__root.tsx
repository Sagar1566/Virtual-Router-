import { createRootRoute, Link, Outlet, useNavigate, useLocation } from '@tanstack/react-router';
import {
  AppBar,
  Box,
  Drawer,
  List,
  ListItem,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  Toolbar,
  Typography,
  IconButton,
  Badge,
  Tabs,
  Tab,
  CircularProgress,
  Button,
} from '@mui/material';
import {
  Dashboard as DashboardIcon,
  Public as WANIcon,
  Router as RouterIcon,
  Dns as DnsIcon,
  DeviceHub as DhcpIcon,
  VpnKey as VpnIcon,
  Link as LinkIcon,
  Wifi as WifiIcon,
  Security as FirewallIcon,
  Settings as SettingsIcon,
  Menu as MenuIcon,
  Build as SetupIcon,
  BugReport as DebugIcon,
  Close as CloseIcon,
  Refresh as RefreshIcon,
  ContentCopy as CopyIcon,
  Speed as StatsIcon,
  Tune as QoSIcon,
  Logout as LogoutIcon,
  Apps as ServicesIcon,
} from '@mui/icons-material';
import { useState, useEffect, useCallback } from 'react';
import { client } from '../api/client';
import { PendingConfirmBanner } from '../components/PendingConfirmBanner';
import { LogModalProvider } from '../context/LogModalContext';

const drawerWidth = 240;
const debugDrawerWidth = 600;

const navItems = [
  { path: '/', label: 'Dashboard', icon: <DashboardIcon /> },
  { path: '/stats', label: 'Statistics', icon: <StatsIcon /> },
  { path: '/qos', label: 'QoS', icon: <QoSIcon /> },
  { path: '/setup', label: 'Setup', icon: <SetupIcon /> },
  { path: '/wan', label: 'WAN', icon: <WANIcon /> },
  { path: '/interfaces', label: 'Interfaces', icon: <RouterIcon /> },
  { path: '/dns', label: 'DNS', icon: <DnsIcon /> },
  { path: '/external-dns', label: 'External DNS', icon: <WANIcon /> },
  { path: '/services', label: 'Services', icon: <ServicesIcon /> },
  { path: '/dhcp', label: 'DHCP', icon: <DhcpIcon /> },
  { path: '/wireguard', label: 'WireGuard', icon: <VpnIcon /> },
  { path: '/wireguard-p2p', label: 'Site-to-Site', icon: <LinkIcon /> },
  { path: '/wifi', label: 'WiFi', icon: <WifiIcon /> },
  { path: '/firewall', label: 'Firewall', icon: <FirewallIcon /> },
  { path: '/settings', label: 'Settings', icon: <SettingsIcon /> },
];

interface LogEntry {
  time: string;
  level: string;
  message: string;
}

function RootLayout() {
  const navigate = useNavigate();
  const location = useLocation();
  const [mobileOpen, setMobileOpen] = useState(false);
  const [debugOpen, setDebugOpen] = useState(false);
  const [debugTab, setDebugTab] = useState(0);
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [errorCount, setErrorCount] = useState(0);
  const [exportConfig, setExportConfig] = useState<string>('');
  const [exportLoading, setExportLoading] = useState(false);
  const [copied, setCopied] = useState(false);
  const [authChecked, setAuthChecked] = useState(false);
  const [isAuthenticated, setIsAuthenticated] = useState(false);

  // Check authentication status
  useEffect(() => {
    const checkAuth = async () => {
      try {
        const status = await client.getAuthStatus();
        setIsAuthenticated(status.authenticated);
        if (!status.authenticated && location.pathname !== '/login') {
          navigate({ to: '/login' });
        }
      } catch {
        // If auth check fails, redirect to login
        if (location.pathname !== '/login') {
          navigate({ to: '/login' });
        }
      } finally {
        setAuthChecked(true);
      }
    };
    checkAuth();
  }, [navigate, location.pathname]);

  const handleLogout = async () => {
    try {
      await client.logout();
      setIsAuthenticated(false);
      navigate({ to: '/login' });
    } catch (e) {
      console.error('Logout failed:', e);
    }
  };

  const fetchLogs = useCallback(async () => {
    try {
      const data = await client.getSystemLogs({ limit: 200 });
      setLogs(data.entries || []);
      setErrorCount(data.entries?.filter((e: LogEntry) => e.level === 'ERROR').length || 0);
    } catch (e) {
      console.error('Failed to fetch logs:', e);
    }
  }, []);

  const fetchExportConfig = useCallback(async () => {
    setExportLoading(true);
    try {
      const [config, routes, p2pStatus, interfaces] = await Promise.all([
        client.getConfig().catch(() => null),
        client.getRoutes().catch(() => null),
        client.getP2PStatus().catch(() => null),
        client.listInterfaces().catch(() => null),
      ]);

      // Sanitize config - remove sensitive data
      // Cast to any for dynamic property access
      const cfg = config as Record<string, unknown> | null;
      const sanitizedConfig = cfg ? {
        ...cfg,
        // Remove any private keys or tokens
        wireguard: cfg.wireguard ? {
          ...(cfg.wireguard as Record<string, unknown>),
          privateKey: (cfg.wireguard as Record<string, unknown>).privateKey ? '[REDACTED]' : undefined,
        } : undefined,
        wireguardP2P: cfg.wireguardP2P ? {
          ...(cfg.wireguardP2P as Record<string, unknown>),
          tunnels: ((cfg.wireguardP2P as Record<string, unknown>).tunnels as Array<Record<string, unknown>>)?.map((t) => ({
            ...t,
            privateKey: t.privateKey ? '[REDACTED]' : undefined,
            presharedKey: t.presharedKey ? '[REDACTED]' : undefined,
          })),
        } : undefined,
      } : null;

      const exportData = {
        timestamp: new Date().toISOString(),
        config: sanitizedConfig,
        routes: routes?.routes || [],
        p2pTunnels: p2pStatus?.tunnels || [],
        interfaces: interfaces?.interfaces || [],
      };

      setExportConfig(JSON.stringify(exportData, null, 2));
    } catch (e) {
      console.error('Failed to fetch export config:', e);
      setExportConfig('Error fetching configuration');
    } finally {
      setExportLoading(false);
    }
  }, []);

  const configPreRef = useCallback((node: HTMLPreElement | null) => {
    if (node) {
      (window as unknown as { configPreElement: HTMLPreElement }).configPreElement = node;
    }
  }, []);

  const handleCopyConfig = async () => {
    try {
      // Try clipboard API first (requires HTTPS)
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(exportConfig);
        setCopied(true);
        setTimeout(() => setCopied(false), 2000);
        return;
      }

      // Fallback: select all text in the pre element
      const preElement = (window as unknown as { configPreElement?: HTMLPreElement }).configPreElement;
      if (preElement) {
        const range = document.createRange();
        range.selectNodeContents(preElement);
        const selection = window.getSelection();
        if (selection) {
          selection.removeAllRanges();
          selection.addRange(range);
          // Try execCommand as fallback
          try {
            document.execCommand('copy');
            setCopied(true);
            setTimeout(() => setCopied(false), 2000);
          } catch {
            // Selection is still active, user can manually copy
            setCopied(false);
          }
        }
      }
    } catch (e) {
      console.error('Failed to copy:', e);
    }
  };

  useEffect(() => {
    if (!isAuthenticated) return;
    fetchLogs();
    const interval = setInterval(fetchLogs, 5000);
    return () => clearInterval(interval);
  }, [fetchLogs, isAuthenticated]);

  // Fetch export config when switching to that tab
  useEffect(() => {
    if (debugOpen && debugTab === 1 && !exportConfig) {
      fetchExportConfig();
    }
  }, [debugOpen, debugTab, exportConfig, fetchExportConfig]);

  const getLevelColor = (level: string) => {
    switch (level) {
      case 'ERROR': return '#f44336';
      case 'WARN': return '#ff9800';
      case 'INFO': return '#4caf50';
      case 'DEBUG': return '#9e9e9e';
      default: return '#fff';
    }
  };

  const formatTime = (time: string) => {
    try {
      return new Date(time).toLocaleTimeString();
    } catch {
      return time;
    }
  };

  const drawer = (
    <Box>
      <Toolbar>
        <Typography variant="h6" noWrap component="div" sx={{ color: 'primary.main' }}>
          Ubuntu Router
        </Typography>
      </Toolbar>
      <List>
        {navItems.map((item) => (
          <ListItem key={item.path} disablePadding>
            <ListItemButton
              component={Link}
              to={item.path}
              onClick={() => setMobileOpen(false)}
            >
              <ListItemIcon sx={{ color: 'text.secondary' }}>
                {item.icon}
              </ListItemIcon>
              <ListItemText primary={item.label} />
            </ListItemButton>
          </ListItem>
        ))}
      </List>
    </Box>
  );

  // Show loading spinner while checking auth
  if (!authChecked) {
    return (
      <Box
        sx={{
          minHeight: '100vh',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          bgcolor: 'background.default',
        }}
      >
        <CircularProgress />
      </Box>
    );
  }

  // If on login page, just render the outlet (login page) without the main layout
  if (location.pathname === '/login') {
    return <Outlet />;
  }

  // If not authenticated and not on login, the useEffect will redirect
  if (!isAuthenticated) {
    return null;
  }

  return (
    <LogModalProvider>
    <Box sx={{ display: 'flex' }}>
      <AppBar
        position="fixed"
        sx={{
          width: { sm: `calc(100% - ${drawerWidth}px)` },
          ml: { sm: `${drawerWidth}px` },
          bgcolor: 'background.paper',
        }}
      >
        <Toolbar>
          <IconButton
            color="inherit"
            edge="start"
            onClick={() => setMobileOpen(!mobileOpen)}
            sx={{ mr: 2, display: { sm: 'none' } }}
          >
            <MenuIcon />
          </IconButton>
          <Typography variant="h6" noWrap component="div" sx={{ flexGrow: 1 }}>
            Router Management
          </Typography>
          <IconButton
            color="inherit"
            onClick={() => setDebugOpen(true)}
            title="Debug"
          >
            <Badge badgeContent={errorCount} color="error">
              <DebugIcon />
            </Badge>
          </IconButton>
          <IconButton
            color="inherit"
            onClick={handleLogout}
            title="Logout"
          >
            <LogoutIcon />
          </IconButton>
        </Toolbar>
      </AppBar>

      <Box
        component="nav"
        sx={{ width: { sm: drawerWidth }, flexShrink: { sm: 0 } }}
      >
        <Drawer
          variant="temporary"
          open={mobileOpen}
          onClose={() => setMobileOpen(false)}
          ModalProps={{ keepMounted: true }}
          sx={{
            display: { xs: 'block', sm: 'none' },
            '& .MuiDrawer-paper': {
              boxSizing: 'border-box',
              width: drawerWidth,
              bgcolor: 'background.paper',
            },
          }}
        >
          {drawer}
        </Drawer>
        <Drawer
          variant="permanent"
          sx={{
            display: { xs: 'none', sm: 'block' },
            '& .MuiDrawer-paper': {
              boxSizing: 'border-box',
              width: drawerWidth,
              bgcolor: 'background.paper',
              borderRight: '1px solid #333',
            },
          }}
          open
        >
          {drawer}
        </Drawer>
      </Box>

      {/* Debug Drawer */}
      <Drawer
        anchor="right"
        open={debugOpen}
        onClose={() => setDebugOpen(false)}
        sx={{
          '& .MuiDrawer-paper': {
            width: { xs: '100%', sm: debugDrawerWidth },
            bgcolor: '#0d1117',
          },
        }}
      >
        <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
          <Toolbar sx={{ borderBottom: '1px solid #333', justifyContent: 'space-between', minHeight: 56 }}>
            <Typography variant="h6" sx={{ color: '#fff' }}>
              Debug
            </Typography>
            <Box>
              {debugTab === 0 && (
                <IconButton color="inherit" onClick={fetchLogs} title="Refresh Logs">
                  <RefreshIcon />
                </IconButton>
              )}
              {debugTab === 1 && (
                <>
                  <IconButton color="inherit" onClick={() => { setExportConfig(''); fetchExportConfig(); }} title="Refresh Config">
                    <RefreshIcon />
                  </IconButton>
                  <IconButton
                    color="inherit"
                    onClick={handleCopyConfig}
                    title={copied ? 'Copied!' : 'Copy to Clipboard'}
                    disabled={!exportConfig || exportLoading}
                  >
                    <CopyIcon />
                  </IconButton>
                </>
              )}
              <IconButton color="inherit" onClick={() => setDebugOpen(false)} title="Close">
                <CloseIcon />
              </IconButton>
            </Box>
          </Toolbar>
          <Tabs
            value={debugTab}
            onChange={(_, v) => setDebugTab(v)}
            sx={{
              borderBottom: '1px solid #333',
              '& .MuiTab-root': { color: '#8b949e' },
              '& .Mui-selected': { color: '#fff' },
            }}
          >
            <Tab label="Logs" />
            <Tab label="Export Config" />
          </Tabs>

          {/* Logs Tab */}
          {debugTab === 0 && (
            <Box
              component="pre"
              sx={{
                flex: 1,
                m: 0,
                p: 1.5,
                overflow: 'auto',
                fontFamily: '"JetBrains Mono", "Fira Code", "Consolas", monospace',
                fontSize: '12px',
                lineHeight: 1.5,
                bgcolor: '#0d1117',
                color: '#c9d1d9',
                '&::-webkit-scrollbar': {
                  width: 8,
                },
                '&::-webkit-scrollbar-track': {
                  bgcolor: '#161b22',
                },
                '&::-webkit-scrollbar-thumb': {
                  bgcolor: '#30363d',
                  borderRadius: 4,
                },
              }}
            >
              {logs.length === 0 ? (
                <span style={{ color: '#8b949e' }}>No logs available</span>
              ) : (
                [...logs].reverse().map((entry, i) => (
                  <div key={i} style={{ marginBottom: 4 }}>
                    <span style={{ color: '#8b949e' }}>{formatTime(entry.time)}</span>
                    {' '}
                    <span style={{
                      color: getLevelColor(entry.level),
                      fontWeight: entry.level === 'ERROR' ? 'bold' : 'normal',
                    }}>
                      [{entry.level.padEnd(5)}]
                    </span>
                    {' '}
                    <span style={{
                      color: entry.level === 'ERROR' ? '#f85149' : '#c9d1d9',
                    }}>
                      {entry.message}
                    </span>
                  </div>
                ))
              )}
            </Box>
          )}

          {/* Export Config Tab */}
          {debugTab === 1 && (
            <Box sx={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
              <Box sx={{ p: 1.5, borderBottom: '1px solid #333', display: 'flex', flexDirection: 'column', gap: 1 }}>
                <Typography variant="body2" sx={{ color: '#8b949e' }}>
                  Copy this configuration to share for debugging. Sensitive data (private keys) is redacted.
                </Typography>
                <Button
                  variant="contained"
                  color={copied ? 'success' : 'primary'}
                  startIcon={<CopyIcon />}
                  onClick={handleCopyConfig}
                  disabled={!exportConfig || exportLoading}
                  fullWidth
                  sx={{ py: 1.5 }}
                >
                  {copied ? 'Copied! (or selected - use Ctrl+C)' : 'Copy Configuration'}
                </Button>
              </Box>
              <Box
                component="pre"
                ref={configPreRef}
                sx={{
                  flex: 1,
                  m: 0,
                  p: 1.5,
                  overflow: 'auto',
                  fontFamily: '"JetBrains Mono", "Fira Code", "Consolas", monospace',
                  fontSize: '11px',
                  lineHeight: 1.4,
                  bgcolor: '#0d1117',
                  color: '#c9d1d9',
                  userSelect: 'text',
                  WebkitUserSelect: 'text',
                  '&::-webkit-scrollbar': {
                    width: 8,
                  },
                  '&::-webkit-scrollbar-track': {
                    bgcolor: '#161b22',
                  },
                  '&::-webkit-scrollbar-thumb': {
                    bgcolor: '#30363d',
                    borderRadius: 4,
                  },
                }}
              >
                {exportLoading ? (
                  <Box sx={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '100%' }}>
                    <CircularProgress size={24} />
                  </Box>
                ) : (
                  exportConfig || 'No configuration loaded'
                )}
              </Box>
            </Box>
          )}
        </Box>
      </Drawer>

      <Box
        component="main"
        sx={{
          flexGrow: 1,
          p: { xs: 1.5, sm: 3 },
          width: { xs: '100%', sm: `calc(100% - ${drawerWidth}px)` },
          mt: '64px',
          minHeight: 'calc(100vh - 64px)',
          overflow: 'auto',
        }}
      >
        {/* Global Pending Configuration Banner */}
        <PendingConfirmBanner
          onConfirmed={() => {
            // Refresh the current page data after confirming
            window.location.reload();
          }}
          onCancelled={() => {
            // Refresh after rollback
            window.location.reload();
          }}
        />
        <Outlet />
      </Box>
    </Box>
    </LogModalProvider>
  );
}

export const Route = createRootRoute({
  component: RootLayout,
});
