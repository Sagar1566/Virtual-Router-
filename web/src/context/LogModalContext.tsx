import { createContext, useContext, useState, useCallback, type ReactNode } from 'react';
import { LogModal } from '../components/LogModal';
import { useLogStream } from '../api/websocket';

interface LogModalContextType {
  open: () => void;
  close: () => void;
  isOpen: boolean;
  clear: () => void;
}

const LogModalContext = createContext<LogModalContextType | null>(null);

interface LogModalProviderProps {
  children: ReactNode;
}

export function LogModalProvider({ children }: LogModalProviderProps) {
  const [isOpen, setIsOpen] = useState(false);
  const { logs, connected, connecting, connect, disconnect, clear } = useLogStream();

  const open = useCallback(() => {
    connect();
    setIsOpen(true);
  }, [connect]);

  const close = useCallback(() => {
    setIsOpen(false);
    // Don't disconnect immediately - keep connection for a bit in case they reopen
    setTimeout(() => {
      // Only disconnect if still closed
      if (!isOpen) {
        disconnect();
      }
    }, 5000);
  }, [disconnect, isOpen]);

  const handleClear = useCallback(() => {
    clear();
  }, [clear]);

  return (
    <LogModalContext.Provider value={{ open, close, isOpen, clear: handleClear }}>
      {children}
      <LogModal
        open={isOpen}
        onClose={close}
        logs={logs}
        connected={connected}
        connecting={connecting}
        onClear={handleClear}
        title="System Logs"
      />
    </LogModalContext.Provider>
  );
}

export function useLogModal(): LogModalContextType {
  const context = useContext(LogModalContext);
  if (!context) {
    throw new Error('useLogModal must be used within a LogModalProvider');
  }
  return context;
}
