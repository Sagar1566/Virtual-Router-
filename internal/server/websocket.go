package server

import (
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/iodesystems/ubuntu-router/internal/logger"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Allow connections from any origin (same-origin enforcement handled by auth)
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// handleWebSocketLogs handles WebSocket connections for real-time log streaming
func (s *Server) handleWebSocketLogs(w http.ResponseWriter, r *http.Request) {
	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	// Subscribe to log entries
	sub := logger.Subscribe()
	if sub == nil {
		log.Printf("Failed to subscribe to logger")
		return
	}
	defer logger.Unsubscribe(sub)

	// Send existing log entries first (last 100)
	existingEntries := logger.GetEntries(100)
	for _, entry := range existingEntries {
		if err := conn.WriteJSON(entry); err != nil {
			log.Printf("WebSocket write error (initial): %v", err)
			return
		}
	}

	// Set up ping/pong to detect dead connections
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// Start a goroutine to handle pings and read messages
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}()

	// Start ping ticker
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Stream new log entries
	for {
		select {
		case entry, ok := <-sub.Ch():
			if !ok {
				return
			}
			if err := conn.WriteJSON(entry); err != nil {
				log.Printf("WebSocket write error: %v", err)
				return
			}
		case <-ticker.C:
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-done:
			return
		}
	}
}
