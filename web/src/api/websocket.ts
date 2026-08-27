import { useState, useEffect, useRef, useCallback } from 'react';
import type { LogEntry } from './client';

interface UseLogStreamOptions {
  maxEntries?: number;
  autoConnect?: boolean;
}

interface UseLogStreamResult {
  logs: LogEntry[];
  connected: boolean;
  connecting: boolean;
  error: string | null;
  connect: () => void;
  disconnect: () => void;
  clear: () => void;
}

export function useLogStream(options: UseLogStreamOptions = {}): UseLogStreamResult {
  const { maxEntries = 500, autoConnect = false } = options;

  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [connected, setConnected] = useState(false);
  const [connecting, setConnecting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const shouldReconnectRef = useRef(false);

  const connect = useCallback(() => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      return;
    }

    setConnecting(true);
    setError(null);
    shouldReconnectRef.current = true;

    // Build WebSocket URL
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/api/ws/logs`;

    const ws = new WebSocket(wsUrl);
    wsRef.current = ws;

    ws.onopen = () => {
      setConnected(true);
      setConnecting(false);
      setError(null);
    };

    ws.onmessage = (event) => {
      try {
        const entry: LogEntry = JSON.parse(event.data);
        setLogs((prev) => {
          const newLogs = [...prev, entry];
          // Keep only the last maxEntries
          if (newLogs.length > maxEntries) {
            return newLogs.slice(-maxEntries);
          }
          return newLogs;
        });
      } catch (e) {
        console.error('Failed to parse log entry:', e);
      }
    };

    ws.onerror = () => {
      setError('WebSocket connection error');
      setConnecting(false);
    };

    ws.onclose = () => {
      setConnected(false);
      setConnecting(false);
      wsRef.current = null;

      // Auto-reconnect if we should
      if (shouldReconnectRef.current) {
        reconnectTimeoutRef.current = setTimeout(() => {
          connect();
        }, 3000);
      }
    };
  }, [maxEntries]);

  const disconnect = useCallback(() => {
    shouldReconnectRef.current = false;

    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current);
      reconnectTimeoutRef.current = null;
    }

    if (wsRef.current) {
      wsRef.current.close();
      wsRef.current = null;
    }

    setConnected(false);
    setConnecting(false);
  }, []);

  const clear = useCallback(() => {
    setLogs([]);
  }, []);

  // Auto-connect on mount if enabled
  useEffect(() => {
    if (autoConnect) {
      connect();
    }

    return () => {
      disconnect();
    };
  }, [autoConnect, connect, disconnect]);

  return {
    logs,
    connected,
    connecting,
    error,
    connect,
    disconnect,
    clear,
  };
}
