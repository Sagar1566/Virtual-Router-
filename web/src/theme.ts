import { createTheme } from '@mui/material/styles';

export const theme = createTheme({
  palette: {
    mode: 'dark',
    primary: {
      main: '#e94560',
      light: '#ff6b6b',
      dark: '#c23a52',
    },
    secondary: {
      main: '#00d9a5',
      light: '#33e1b7',
      dark: '#00a77f',
    },
    background: {
      default: '#1a1a2e',
      paper: '#0f3460',
    },
    error: {
      main: '#dc3545',
    },
    warning: {
      main: '#ffc107',
    },
    success: {
      main: '#00d9a5',
    },
    text: {
      primary: '#eaeaea',
      secondary: '#a0a0a0',
    },
  },
  typography: {
    fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif',
    h1: {
      fontSize: '2rem',
      fontWeight: 600,
    },
    h2: {
      fontSize: '1.5rem',
      fontWeight: 600,
    },
    h3: {
      fontSize: '1.25rem',
      fontWeight: 600,
    },
  },
  components: {
    MuiCard: {
      styleOverrides: {
        root: {
          backgroundColor: '#0f3460',
          borderRadius: 8,
        },
      },
    },
    MuiButton: {
      styleOverrides: {
        root: {
          textTransform: 'none',
          borderRadius: 4,
        },
      },
    },
    MuiChip: {
      styleOverrides: {
        root: {
          borderRadius: 12,
        },
      },
    },
    MuiTableCell: {
      styleOverrides: {
        root: {
          borderColor: '#333',
        },
        head: {
          fontWeight: 600,
          color: '#a0a0a0',
        },
      },
    },
  },
});
