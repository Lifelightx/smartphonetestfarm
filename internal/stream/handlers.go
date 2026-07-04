package stream

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for enterprise-grade provider portal API
	},
}

// handleState returns the full domain.Device state including FileSystem and Browsers.
func (m *Manager) handleState(w http.ResponseWriter, r *http.Request, serial string) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	device, err := m.registry.Get(serial)
	if err != nil {
		http.Error(w, "device not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(device.State); err != nil {
		slog.Error("stream: failed to encode device state", "serial", serial, "err", err)
	}
}

// handleStreamClient serves one HTTP client with raw H.264 stream.
func (m *Manager) handleStreamClient(w http.ResponseWriter, r *http.Request, serial string) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	m.mu.Lock()
	s, ok := m.streams[serial]
	m.mu.Unlock()
	if !ok {
		http.Error(w, "no active stream", http.StatusNotFound)
		return
	}

	slog.Info("stream: client connected to stream", "serial", serial, "remote_addr", r.RemoteAddr)
	defer slog.Info("stream: client disconnected from stream", "serial", serial, "remote_addr", r.RemoteAddr)

	w.Header().Set("Content-Type", "video/h264")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ch := make(chan []byte, 60)
	cachedGOP := s.addClientAndGetCache(ch)
	defer s.removeClient(ch)

	for _, chunk := range cachedGOP {
		if _, err := w.Write(chunk); err != nil {
			return
		}
	}
	flusher.Flush()

	for {
		select {
		case chunk, more := <-ch:
			if !more {
				return
			}
			if _, err := w.Write(chunk); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		case <-s.done:
			return
		}
	}
}

// handleControl handles POST /control and sends a scrcpy control message to the device.
func (m *Manager) handleControl(w http.ResponseWriter, r *http.Request, serial string) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	m.mu.Lock()
	s, ok := m.streams[serial]
	m.mu.Unlock()
	if !ok {
		http.Error(w, "no active stream", http.StatusNotFound)
		return
	}

	var ev ControlEvent
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}

	s.controlMu.Lock()
	conn := s.controlConn
	vw := s.videoWidth
	vh := s.videoHeight
	s.controlMu.Unlock()

	if conn == nil {
		http.Error(w, "control connection not ready", http.StatusServiceUnavailable)
		return
	}

	msgs, err := SerializeControlEvent(&ev, vw, vh)
	if err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}

	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	if s.controlConn == nil {
		http.Error(w, "control connection closed", http.StatusServiceUnavailable)
		return
	}

	for _, msg := range msgs {
		if _, err := s.controlConn.Write(msg); err != nil {
			slog.Warn("stream: control write failed", "serial", serial, "err", err)
			http.Error(w, "write failed", http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
