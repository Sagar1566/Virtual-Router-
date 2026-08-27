package logger

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Subscriber receives log entries via a channel
type Subscriber struct {
	ch     chan LogEntry
	closed bool
	mu     sync.Mutex
}

// Ch returns the channel to receive log entries
func (s *Subscriber) Ch() <-chan LogEntry {
	return s.ch
}

// Close unsubscribes and closes the channel
func (s *Subscriber) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.ch)
	}
}

// Logger provides file-based logging with in-memory buffer for viewing
type Logger struct {
	mu          sync.RWMutex
	file        *os.File
	buffer      []LogEntry
	maxSize     int
	subscribers []*Subscriber
	subMu       sync.RWMutex
}

// LogEntry represents a single log entry
type LogEntry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}

var (
	defaultLogger *Logger
	once          sync.Once
)

// Init initializes the default logger
func Init(logPath string) error {
	var err error
	once.Do(func() {
		defaultLogger, err = New(logPath, 1000)
	})
	return err
}

// New creates a new logger
func New(logPath string, bufferSize int) (*Logger, error) {
	l := &Logger{
		buffer:  make([]LogEntry, 0, bufferSize),
		maxSize: bufferSize,
	}

	if logPath != "" {
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, fmt.Errorf("failed to open log file: %w", err)
		}
		l.file = f
	}

	return l, nil
}

// log writes a log entry
func (l *Logger) log(level, format string, args ...interface{}) {
	l.mu.Lock()

	msg := fmt.Sprintf(format, args...)
	entry := LogEntry{
		Time:    time.Now(),
		Level:   level,
		Message: msg,
	}

	// Add to buffer
	if len(l.buffer) >= l.maxSize {
		// Remove oldest entry
		l.buffer = l.buffer[1:]
	}
	l.buffer = append(l.buffer, entry)

	// Write to file
	line := fmt.Sprintf("%s [%s] %s\n", entry.Time.Format("2006-01-02 15:04:05"), level, msg)
	if l.file != nil {
		l.file.WriteString(line)
	}

	// Also write to stdout
	fmt.Print(line)

	l.mu.Unlock()

	// Broadcast to subscribers (non-blocking)
	l.broadcast(entry)
}

// broadcast sends entry to all subscribers (non-blocking, drops if full)
func (l *Logger) broadcast(entry LogEntry) {
	l.subMu.RLock()
	defer l.subMu.RUnlock()

	for _, sub := range l.subscribers {
		sub.mu.Lock()
		if !sub.closed {
			select {
			case sub.ch <- entry:
			default:
				// Channel full, drop the entry
			}
		}
		sub.mu.Unlock()
	}
}

// Subscribe creates a new subscriber that receives log entries
func (l *Logger) Subscribe() *Subscriber {
	sub := &Subscriber{
		ch: make(chan LogEntry, 100), // Buffered to prevent blocking
	}

	l.subMu.Lock()
	l.subscribers = append(l.subscribers, sub)
	l.subMu.Unlock()

	return sub
}

// Unsubscribe removes a subscriber
func (l *Logger) Unsubscribe(sub *Subscriber) {
	l.subMu.Lock()
	defer l.subMu.Unlock()

	for i, s := range l.subscribers {
		if s == sub {
			l.subscribers = append(l.subscribers[:i], l.subscribers[i+1:]...)
			sub.Close()
			break
		}
	}
}

// Info logs an info message
func (l *Logger) Info(format string, args ...interface{}) {
	l.log("INFO", format, args...)
}

// Error logs an error message
func (l *Logger) Error(format string, args ...interface{}) {
	l.log("ERROR", format, args...)
}

// Warn logs a warning message
func (l *Logger) Warn(format string, args ...interface{}) {
	l.log("WARN", format, args...)
}

// Debug logs a debug message
func (l *Logger) Debug(format string, args ...interface{}) {
	l.log("DEBUG", format, args...)
}

// GetEntries returns the buffered log entries
func (l *Logger) GetEntries(limit int) []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if limit <= 0 || limit > len(l.buffer) {
		limit = len(l.buffer)
	}

	// Return most recent entries
	start := len(l.buffer) - limit
	if start < 0 {
		start = 0
	}

	result := make([]LogEntry, limit)
	copy(result, l.buffer[start:])
	return result
}

// Clear clears the log buffer
func (l *Logger) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buffer = l.buffer[:0]
}

// Close closes the log file
func (l *Logger) Close() error {
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// Writer returns an io.Writer for the logger at INFO level
func (l *Logger) Writer() io.Writer {
	return &logWriter{l: l, level: "INFO"}
}

type logWriter struct {
	l     *Logger
	level string
}

func (w *logWriter) Write(p []byte) (n int, err error) {
	w.l.log(w.level, "%s", string(p))
	return len(p), nil
}

// Package-level functions for default logger

// Info logs an info message to the default logger
func Info(format string, args ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.Info(format, args...)
	}
}

// Error logs an error message to the default logger
func Error(format string, args ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.Error(format, args...)
	}
}

// Warn logs a warning message to the default logger
func Warn(format string, args ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.Warn(format, args...)
	}
}

// Debug logs a debug message to the default logger
func Debug(format string, args ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.Debug(format, args...)
	}
}

// GetEntries returns entries from the default logger
func GetEntries(limit int) []LogEntry {
	if defaultLogger != nil {
		return defaultLogger.GetEntries(limit)
	}
	return nil
}

// Default returns the default logger
func Default() *Logger {
	return defaultLogger
}

// Subscribe creates a new subscriber for the default logger
func Subscribe() *Subscriber {
	if defaultLogger != nil {
		return defaultLogger.Subscribe()
	}
	return nil
}

// Unsubscribe removes a subscriber from the default logger
func Unsubscribe(sub *Subscriber) {
	if defaultLogger != nil {
		defaultLogger.Unsubscribe(sub)
	}
}

// Clear clears the log buffer of the default logger
func Clear() {
	if defaultLogger != nil {
		defaultLogger.Clear()
	}
}
