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
	"protean-provider/internal/domain"
	"protean-provider/internal/wda"
)

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
				device, rerr := m.registry.Get(serial)
				if rerr != nil {
					slog.Error("stream: DUMP_UI failed to get device", "serial", serial, "err", rerr)
					_ = ws.WriteJSON(map[string]interface{}{
						"type":  "UI_DUMP_ERROR",
						"error": "Device not found",
					})
					return
				}

				var driver domain.Driver
				if strings.EqualFold(device.Platform, "ios") {
					driver = automation.NewIOSDriver(s.port + 3000)
				} else {
					var agt *agent.Agent
					if m.agentFn != nil {
						agt = m.agentFn(serial)
					}
					driver = automation.NewAndroidDriver(serial, agt)
				}

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
					s.gesture.endX = ev.X
					s.gesture.endY = ev.Y
				case 1: // UP
					durationMs := int(time.Since(s.gesture.startTime).Milliseconds())
					if durationMs <= 0 {
						durationMs = 100
					}
					startX := s.gesture.startX
					startY := s.gesture.startY
					endX := ev.X
					endY := ev.Y
					dx := endX - startX
					dy := endY - startY
					isSwipe := dx*dx+dy*dy > 0.0025

					s.gestureMu.Unlock()
					
					go func() {
						var ctx context.Context = r.Context()
						device, rerr := m.registry.Get(serial)
						if rerr != nil {
							slog.Error("stream: record action failed to get device", "serial", serial, "err", rerr)
							return
						}

						var driver domain.Driver
						if strings.EqualFold(device.Platform, "ios") {
							driver = automation.NewIOSDriver(s.port + 3000)
						} else {
							var agt *agent.Agent
							if m.agentFn != nil {
								agt = m.agentFn(serial)
							}
							driver = automation.NewAndroidDriver(serial, agt)
						}

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

		device, err := m.registry.Get(serial)
		if err == nil && strings.EqualFold(device.Platform, "ios") {
			s.gestureMu.Lock()
			if s.wdaClient == nil {
				if m.wdaFn != nil {
					client, err := m.wdaFn(serial)
					if err == nil {
						s.wdaClient = client
					}
				}
				if s.wdaClient == nil {
					s.wdaClient = wda.NewClient(s.port + 3000)
				}
			}
			wdaClient := s.wdaClient
			s.gestureMu.Unlock()

			switch ev.Type {
			case "touch":
				switch ev.Action {
				case 0: // DOWN
					s.gestureMu.Lock()
					s.gesture.startX = ev.X
					s.gesture.startY = ev.Y
					s.gesture.startTime = time.Now()
					s.gesture.isSwipe = false
					s.gestureMu.Unlock()

				case 2: // MOVE
					s.gestureMu.Lock()
					dx := ev.X - s.gesture.startX
					dy := ev.Y - s.gesture.startY
					if dx*dx+dy*dy > 0.0025 {
						s.gesture.isSwipe = true
					}
					s.gesture.endX = ev.X
					s.gesture.endY = ev.Y
					s.gestureMu.Unlock()

				case 1: // UP
					s.gestureMu.Lock()
					durationMs := int(time.Since(s.gesture.startTime).Milliseconds())
					if durationMs <= 0 {
						durationMs = 100
					}
					snapStartX := s.gesture.startX
					snapStartY := s.gesture.startY
					snapEndX := ev.X
					snapEndY := ev.Y
					dx := snapEndX - snapStartX
					dy := snapEndY - snapStartY
					isSwipe := dx*dx+dy*dy > 0.0025
					lw := s.logicalWidth
					lh := s.logicalHeight
					s.gestureMu.Unlock()

					go func() {
						ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
						defer cancel()

						if err := wdaClient.EnsureSession(ctx); err != nil {
							slog.Error("stream: failed to ensure WDA session", "serial", serial, "err", err)
							return
						}

						if lw <= 0 || lh <= 0 {
							if w, h, sizeErr := wdaClient.GetWindowSize(ctx); sizeErr == nil && w > 0 && h > 0 {
								s.gestureMu.Lock()
								s.logicalWidth, s.logicalHeight = w, h
								s.gestureMu.Unlock()
								lw, lh = w, h
							} else {
								lw, lh = 375.0, 812.0
							}
						}

						absStartX := snapStartX * lw
						absStartY := snapStartY * lh
						absEndX := snapEndX * lw
						absEndY := snapEndY * lh

						if isSwipe {
							durationSec := float64(durationMs) / 1000.0
							if durationSec <= 0 {
								durationSec = 0.5
							}
							if serr := wdaClient.Swipe(ctx, absStartX, absStartY, absEndX, absEndY, durationSec); serr != nil {
								slog.Warn("stream: ios swipe failed", "serial", serial, "err", serr)
							}
						} else {
							if terr := wdaClient.Tap(ctx, absStartX, absStartY); terr != nil {
								slog.Warn("stream: ios tap failed", "serial", serial, "err", terr)
							}
						}
					}()
				}

			case "key":
				go func(keycode int) {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()

					if err := wdaClient.EnsureSession(ctx); err != nil {
						slog.Error("stream: failed to ensure WDA session for key event", "serial", serial, "err", err)
						return
					}

					switch keycode {
					case 3: // Home key
						if herr := wdaClient.Request(ctx, "POST", "/wda/homescreen", nil, nil); herr != nil {
							slog.Warn("stream: ios home gesture failed", "serial", serial, "err", herr)
						}
					case 24: // Volume Up
						if herr := wdaClient.PressButton(ctx, "volumeup"); herr != nil {
							slog.Warn("stream: ios volume up failed", "serial", serial, "err", herr)
						}
					case 25: // Volume Down
						if herr := wdaClient.PressButton(ctx, "volumedown"); herr != nil {
							slog.Warn("stream: ios volume down failed", "serial", serial, "err", herr)
						}
					case 164: // Volume Mute
						_ = wdaClient.PressButton(ctx, "mute")
					case 4: // Back button
						s.gestureMu.Lock()
						lw, lh := s.logicalWidth, s.logicalHeight
						s.gestureMu.Unlock()
						if lw <= 0 || lh <= 0 {
							if w, h, sizeErr := wdaClient.GetWindowSize(ctx); sizeErr == nil && w > 0 && h > 0 {
								s.gestureMu.Lock()
								s.logicalWidth, s.logicalHeight = w, h
								s.gestureMu.Unlock()
								lw, lh = w, h
							} else {
								lw, lh = 375.0, 812.0
							}
						}
						if serr := wdaClient.Swipe(ctx, 5.0, lh/2.0, lw*0.7, lh/2.0, 0.4); serr != nil {
							slog.Warn("stream: ios swipe-back failed", "serial", serial, "err", serr)
						}
					case 187: // Recents key (App Switcher)
						s.gestureMu.Lock()
						lw, lh := s.logicalWidth, s.logicalHeight
						s.gestureMu.Unlock()
						if lw <= 0 || lh <= 0 {
							if w, h, sizeErr := wdaClient.GetWindowSize(ctx); sizeErr == nil && w > 0 && h > 0 {
								s.gestureMu.Lock()
								s.logicalWidth, s.logicalHeight = w, h
								s.gestureMu.Unlock()
								lw, lh = w, h
							} else {
								lw, lh = 375.0, 812.0
							}
						}
						// On modern iOS, entering the App Switcher requires swiping up and holding/pausing before releasing
						if serr := wdaClient.SwipeAndHold(ctx, lw/2.0, lh - 5.0, lw/2.0, lh*0.5, 0.4, 1000); serr != nil {
							slog.Warn("stream: ios app-switcher swipe failed", "serial", serial, "err", serr)
						}
					case 67: // Backspace
						if terr := wdaClient.SendKeys(ctx, "\b"); terr != nil {
							slog.Warn("stream: ios send backspace failed", "serial", serial, "err", terr)
						}
					case 66: // Enter
						if terr := wdaClient.SendKeys(ctx, "\n"); terr != nil {
							slog.Warn("stream: ios send enter failed", "serial", serial, "err", terr)
						}
					}
				}(ev.Keycode)

			case "text":
				go func(text string) {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if terr := wdaClient.SendKeys(ctx, text); terr != nil {
						slog.Warn("stream: ios send keys failed", "serial", serial, "err", terr)
					}
				}(ev.Text)
			}
			continue
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
