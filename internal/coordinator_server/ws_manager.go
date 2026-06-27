package coordinator_server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for development
	},
}

// WSEvent represents a standard WebSocket event structure
type WSEvent struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data"`
}

// WSManager handles WebSocket client connections and event broadcasting
type WSManager struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]bool
}

// NewWSManager creates a new WebSocket manager instance
func NewWSManager() *WSManager {
	return &WSManager{
		clients: make(map[*websocket.Conn]bool),
	}
}

// HandleWS upgrades HTTP connection to WebSocket and manages client lifecycle
func (m *WSManager) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("coordinator: failed to upgrade to websocket", "err", err)
		return
	}

	m.mu.Lock()
	m.clients[conn] = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.clients, conn)
		m.mu.Unlock()
		conn.Close()
	}()

	// Keep connection alive and detect client disconnects
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}

// Broadcast sends a typed event to all connected WebSocket clients
func (m *WSManager) Broadcast(event string, data interface{}) {
	msg := WSEvent{
		Event: event,
		Data:  data,
	}
	b, err := json.Marshal(msg)
	if err != nil {
		slog.Error("coordinator: ws marshal error", "err", err)
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for conn := range m.clients {
		if err := conn.WriteMessage(websocket.TextMessage, b); err != nil {
			conn.Close()
			delete(m.clients, conn)
		}
	}
}
