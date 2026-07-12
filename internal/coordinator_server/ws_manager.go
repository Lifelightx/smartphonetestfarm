// Package coordinator_server implements coordinator HTTP, WebSockets, and administrative APIs.
//
// File: ws_manager.go
// This file contains implementation and helper structures for coordinator HTTP, WebSockets, and administrative APIs.

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

type ClientInfo struct {
	UserID  string
	IsAdmin bool
}

// WSManager handles WebSocket client connections and event broadcasting
type WSManager struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]ClientInfo
}

// NewWSManager creates a new WebSocket manager instance
func NewWSManager() *WSManager {
	return &WSManager{
		clients: make(map[*websocket.Conn]ClientInfo),
	}
}

// AddClient performs the add client operation.
func (m *WSManager) AddClient(conn *websocket.Conn, userID string, isAdmin bool) {
	m.mu.Lock()
	m.clients[conn] = ClientInfo{UserID: userID, IsAdmin: isAdmin}
	m.mu.Unlock()
}

// RemoveClient removes the client.
func (m *WSManager) RemoveClient(conn *websocket.Conn) {
	m.mu.Lock()
	delete(m.clients, conn)
	m.mu.Unlock()
	conn.Close()
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
