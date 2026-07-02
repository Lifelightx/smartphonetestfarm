package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"protean-provider/internal/agent"
	"protean-provider/internal/automation"
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

// handleWS upgrades connections to WebSocket and routes video chunks and input events.
func (m *Manager) handleWS(w http.ResponseWriter, r *http.Request, serial string) {
	m.mu.Lock()
	s, ok := m.streams[serial]
	m.mu.Unlock()
	if !ok {
		http.Error(w, "no active stream", http.StatusNotFound)
		return
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("stream: websocket upgrade failed", "serial", serial, "err", err)
		return
	}
	defer ws.Close()

	slog.Info("stream: websocket client connected", "serial", serial, "remote_addr", r.RemoteAddr)
	defer slog.Info("stream: websocket client disconnected", "serial", serial, "remote_addr", r.RemoteAddr)

	ch := make(chan []byte, 120)
	cachedGOP := s.addClientAndGetCache(ch)
	defer s.removeClient(ch)

	for _, chunk := range cachedGOP {
		if err := ws.WriteMessage(websocket.BinaryMessage, chunk); err != nil {
			return
		}
	}

	// Channel loop to send video chunks as binary messages
	wsWriteDone := make(chan struct{})
	go func() {
		defer close(wsWriteDone)
		for {
			select {
			case chunk, more := <-ch:
				if !more {
					return
				}
				// Set a short write deadline so a slow browser can't block the
				// video goroutine and starve control-event processing.
				_ = ws.SetWriteDeadline(time.Now().Add(2 * time.Second))
				if err := ws.WriteMessage(websocket.BinaryMessage, chunk); err != nil {
					return
				}
				_ = ws.SetWriteDeadline(time.Time{}) // clear deadline
			case <-s.done:
				return
			}
		}
	}()

	// Dedicated goroutine for writing control events to the scrcpy socket.
	// Using a buffered channel ensures ws.ReadMessage() never blocks waiting
	// for conn.Write(), which is the primary cause of touch latency.
	controlCh := make(chan []byte, 64)
	go func() {
		for msg := range controlCh {
			s.controlMu.Lock()
			if s.controlConn != nil {
				_, werr := s.controlConn.Write(msg)
				if werr != nil {
					slog.Warn("stream: control write failed", "serial", serial, "err", werr)
				}
			}
			s.controlMu.Unlock()
		}
	}()
	defer close(controlCh)

	// Incoming loop for control messages
	for {
		messageType, payload, err := ws.ReadMessage()
		if err != nil {
			break
		}
		if messageType != websocket.TextMessage {
			continue
		}

		var ev ControlEvent
		if err := json.Unmarshal(payload, &ev); err != nil {
			slog.Warn("stream: ws bad json payload", "serial", serial, "err", err)
			continue
		}

		if ev.Type == "START_RECORDING" {
			var agt *agent.Agent
			if m.agentFn != nil {
				agt = m.agentFn(serial)
			}
			var pkg string
			if agt != nil {
				pkg = getForegroundPackage(r.Context(), agt, true)
			}
			m.recorder.StartRecording(serial, pkg)
			slog.Info("stream: started recording session", "serial", serial, "launchPackage", pkg)
			_ = ws.WriteJSON(map[string]interface{}{
				"type":    "RECORDING_STATUS",
				"status":  "recording",
				"message": "Recording started",
			})
			continue
		}

		if ev.Type == "STOP_RECORDING" {
			rawEvents, err := m.recorder.StopRecording(serial)
			if err != nil {
				_ = ws.WriteJSON(map[string]interface{}{
					"type":  "RECORDING_ERROR",
					"error": err.Error(),
				})
				continue
			}
			script := automation.CompileScript(rawEvents)
			yamlBytes, yerr := script.ToYAML()
			if yerr != nil {
				_ = ws.WriteJSON(map[string]interface{}{
					"type":  "RECORDING_ERROR",
					"error": "Failed to serialize script to YAML: " + yerr.Error(),
				})
				continue
			}
			slog.Info("stream: stopped recording session", "serial", serial, "raw_events", len(rawEvents))
			_ = ws.WriteJSON(map[string]interface{}{
				"type":   "RECORDING_COMPLETE",
				"yaml":   string(yamlBytes),
				"status": "idle",
			})
			continue
		}

		if ev.Type == "DUMP_UI" {
			go func() {
				var ctx context.Context = r.Context()
				var agt *agent.Agent
				if m.agentFn != nil {
					agt = m.agentFn(serial)
				}
				driver := automation.NewAndroidDriver(serial, agt)
				dump, err := driver.DumpUI(ctx)
				if err != nil {
					slog.Error("stream: DUMP_UI failed", "serial", serial, "err", err)
					_ = ws.WriteJSON(map[string]interface{}{
						"type":  "UI_DUMP_ERROR",
						"error": err.Error(),
					})
					return
				}
				_ = ws.WriteJSON(map[string]interface{}{
					"type": "UI_DUMP",
					"xml":  dump,
				})
			}()
			continue
		}

		if ev.Type == "GET_FOREGROUND_PACKAGE" {
			go func() {
				var ctx context.Context = r.Context()
				var agt *agent.Agent
				if m.agentFn != nil {
					agt = m.agentFn(serial)
				}
				var pkg string
				if agt != nil {
					pkg = getForegroundPackage(ctx, agt, true)
				}
				_ = ws.WriteJSON(map[string]interface{}{
					"type":    "FOREGROUND_PACKAGE",
					"package": pkg,
				})
			}()
			continue
		}

		if ev.Type == "LIST_DIRECTORY" {
			go func(path string) {
				var agt *agent.Agent
				if m.agentFn != nil {
					agt = m.agentFn(serial)
				}
				if agt != nil {
					cmdStr := fmt.Sprintf("am broadcast -a com.protean.agent.COMMAND -e command LIST_DIRECTORY -e path %q", path)
					_, err := agt.Shell(context.Background(), cmdStr)
					if err != nil {
						slog.Warn("stream: failed to broadcast LIST_DIRECTORY", "serial", serial, "path", path, "err", err)
					}
				} else {
					slog.Warn("stream: agent not available for LIST_DIRECTORY", "serial", serial, "path", path)
				}
			}(ev.Path)
			continue
		}

		if m.recorder.IsRecording(serial) {
			if ev.Type == "touch" {
				s.gestureMu.Lock()
				switch ev.Action {
				case 0: // DOWN
					s.gesture.startX = ev.X
					s.gesture.startY = ev.Y
					s.gesture.startTime = time.Now()
					s.gesture.isSwipe = false
				case 2: // MOVE
					dx := ev.X - s.gesture.startX
					dy := ev.Y - s.gesture.startY
					if dx*dx+dy*dy > 0.0025 {
						s.gesture.isSwipe = true
					}
				case 1: // UP
					durationMs := int(time.Since(s.gesture.startTime).Milliseconds())
					if durationMs <= 0 {
						durationMs = 100
					}
					startX := s.gesture.startX
					startY := s.gesture.startY
					endX := ev.X
					endY := ev.Y
					isSwipe := s.gesture.isSwipe

					s.gestureMu.Unlock()
					
					go func() {
						var ctx context.Context = r.Context()
						var agt *agent.Agent
						if m.agentFn != nil {
							agt = m.agentFn(serial)
						}
						driver := automation.NewAndroidDriver(serial, agt)
						if isSwipe {
							_ = m.recorder.RecordSwipe(serial, startX, startY, endX, endY, durationMs)
						} else {
							_ = m.recorder.RecordClick(ctx, serial, driver, startX, startY)
						}
					}()
					
					s.gestureMu.Lock()
				}
				s.gestureMu.Unlock()
			} else if ev.Type == "text" {
				_ = m.recorder.RecordTextInput(serial, ev.Text)
			}
		}

		s.controlMu.Lock()
		conn := s.controlConn
		vw := s.videoWidth
		vh := s.videoHeight
		s.controlMu.Unlock()

		if conn == nil {
			continue
		}

		msgs, err := SerializeControlEvent(&ev, vw, vh)
		if err != nil {
			slog.Warn("stream: control event serialization failed", "serial", serial, "err", err)
			continue
		}

		for _, msg := range msgs {
			if len(msg) > 0 {
				select {
				case controlCh <- msg:
				default:
					slog.Warn("stream: control channel full, dropping event", "serial", serial)
				}
			}
		}
	}
}

// getForegroundPackage returns the package name of the active foreground application via ADB.
func getForegroundPackage(ctx context.Context, agt *agent.Agent, raw bool) string {
	// Fallback 1: dumpsys window windows
	res, err := agt.Shell(ctx, "dumpsys window windows")
	if err == nil {
		if pkg := parsePackageFromDumpsys(res.Output, raw); pkg != "" {
			return pkg
		}
	}

	// Fallback 2: dumpsys activity activities
	res2, err2 := agt.Shell(ctx, "dumpsys activity activities")
	if err2 == nil {
		if pkg := parsePackageFromDumpsys(res2.Output, raw); pkg != "" {
			return pkg
		}
	}

	return ""
}

// parsePackageFromDumpsys extracts the package name from window focus / activity resume dumpsys lines.
func parsePackageFromDumpsys(output string, raw bool) string {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "mCurrentFocus") || strings.Contains(line, "mFocusedApp") || strings.Contains(line, "mResumedActivity") {
			slashIdx := strings.Index(line, "/")
			if slashIdx != -1 {
				left := line[:slashIdx]
				startIdx := -1
				for i := len(left) - 1; i >= 0; i-- {
					c := left[i]
					isPkgChar := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '.'
					if !isPkgChar {
						startIdx = i + 1
						break
					}
				}
				if startIdx == -1 {
					startIdx = 0
				}
				pkg := left[startIdx:]
				pkg = strings.Trim(pkg, " \t\n\r{}()=")
				if pkg != "" && strings.Contains(pkg, ".") {
					if raw || !isSystemOrLauncher(pkg) {
						return pkg
					}
				}
			}
		}
	}
	return ""
}

// isSystemOrLauncher checks if the package is a known launcher or system UI component.
func isSystemOrLauncher(pkg string) bool {
	pkg = strings.ToLower(pkg)
	return strings.Contains(pkg, "launcher") ||
		strings.Contains(pkg, "systemui") ||
		strings.Contains(pkg, "home") ||
		pkg == "android"
}
