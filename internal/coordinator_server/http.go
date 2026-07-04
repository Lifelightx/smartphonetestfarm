package coordinator_server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"protean-provider/internal/automation"
	providerpb "protean-provider/pkg/protocol/provider"
)

type DeviceJSON struct {
	Serial            string          `json:"serial"`
	ProviderID        string          `json:"provider_id"`
	Model             string          `json:"model"`
	Manufacturer      string          `json:"manufacturer"`
	Platform          string          `json:"platform"`
	OSVersion         string          `json:"os_version"`
	Android           string          `json:"android"` // legacy compat
	SDK               int             `json:"sdk"`
	ABI               string          `json:"abi"`
	RAM               int64           `json:"ram_mb"`
	Storage           int64           `json:"storage_mb"`
	Display           string          `json:"display"`
	Battery           int             `json:"battery"`
	WiFi              string          `json:"wifi_ssid"`
	IP                string          `json:"ip"`
	Status            string          `json:"status"`
	StreamPort        int             `json:"stream_port"`
	ConnectedAt       time.Time       `json:"connected_at"`
	FileSystem        json.RawMessage `json:"file_system,omitempty"`
	InstalledBrowsers json.RawMessage `json:"installed_browsers,omitempty"`
}

func (s *Server) getDevice(serial string) (*DeviceJSON, error) {
	row := s.db.RawDB().QueryRow(`
		SELECT serial, provider_ip, model, manufacturer, platform, os_version, android, sdk, abi, ram_mb, storage_mb,
		       display_width || 'x' || display_height || ' @ ' || display_dpi || 'dpi',
		       battery, wifi_ssid, ip, status, stream_port, connected_at,
		       file_system, installed_browsers
		FROM devices
		WHERE serial = $1
	`, serial)

	var d DeviceJSON
	var fsJson sql.NullString
	var brJson sql.NullString
	err := row.Scan(&d.Serial, &d.ProviderID, &d.Model, &d.Manufacturer, &d.Platform, &d.OSVersion, &d.Android, &d.SDK, &d.ABI, &d.RAM, &d.Storage, &d.Display, &d.Battery, &d.WiFi, &d.IP, &d.Status, &d.StreamPort, &d.ConnectedAt, &fsJson, &brJson)
	if err != nil {
		return nil, err
	}
	if fsJson.Valid {
		d.FileSystem = json.RawMessage(fsJson.String)
	}
	if brJson.Valid {
		d.InstalledBrowsers = json.RawMessage(brJson.String)
	}
	return &d, nil
}

func (s *Server) getDevicesList() ([]DeviceJSON, error) {
	rows, err := s.db.RawDB().Query(`
		SELECT serial, provider_ip, model, manufacturer, platform, os_version, android, sdk, abi, ram_mb, storage_mb,
		       display_width || 'x' || display_height || ' @ ' || display_dpi || 'dpi',
		       battery, wifi_ssid, ip, status, stream_port, connected_at,
		       file_system, installed_browsers
		FROM devices
		ORDER BY CASE status
		    WHEN 'claimed' THEN 1
		    WHEN 'busy' THEN 1
		    WHEN 'idle' THEN 2
		    WHEN 'offline' THEN 3
		    ELSE 4
		END, connected_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []DeviceJSON
	for rows.Next() {
		var d DeviceJSON
		var fsJson sql.NullString
		var brJson sql.NullString
		err := rows.Scan(&d.Serial, &d.ProviderID, &d.Model, &d.Manufacturer, &d.Platform, &d.OSVersion, &d.Android, &d.SDK, &d.ABI, &d.RAM, &d.Storage, &d.Display, &d.Battery, &d.WiFi, &d.IP, &d.Status, &d.StreamPort, &d.ConnectedAt, &fsJson, &brJson)
		if err != nil {
			return nil, err
		}
		if fsJson.Valid {
			d.FileSystem = json.RawMessage(fsJson.String)
		}
		if brJson.Valid {
			d.InstalledBrowsers = json.RawMessage(brJson.String)
		}
		list = append(list, d)
	}
	if list == nil {
		list = []DeviceJSON{}
	}
	return list, nil
}

func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	list, err := s.getDevicesList()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

func (s *Server) broadcastFullList() {
	list, err := s.getDevicesList()
	if err == nil {
		s.wsManager.Broadcast("DEVICE_LIST_UPDATE", list)
	} else {
		slog.Error("coordinator: failed to get device list for broadcast", "err", err)
	}
}

func (s *Server) handleDeviceAction(w http.ResponseWriter, r *http.Request) {
	relPath := strings.TrimPrefix(r.URL.Path, "/api/v1/devices/")
	parts := strings.Split(relPath, "/")
	if len(parts) < 2 || parts[0] == "" || (parts[1] != "claim" && parts[1] != "release" && parts[1] != "control") {
		http.Error(w, "Invalid path structure. Use /api/v1/devices/{serial}/[claim|release|control]", http.StatusBadRequest)
		return
	}
	serial := parts[0]
	action := parts[1]

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if action == "claim" {
		claimedBy := r.URL.Query().Get("user")
		if claimedBy == "" {
			claimedBy = "admin@apmosys.com"
		}

		sessionID := uuidString()
		slog.Info("coordinator: claim requested", "serial", serial, "by", claimedBy)

		if err := s.db.CreateSession(sessionID, serial, claimedBy); err != nil {
			slog.Error("coordinator: claim failed", "serial", serial, "err", err)
			http.Error(w, fmt.Sprintf("Claim DB state transition failed: %v", err), http.StatusConflict)
			return
		}

		providerIP, _, err := s.db.GetDeviceProvider(serial)
		if err != nil {
			_ = s.db.CloseSession(serial)
			http.Error(w, "Failed to get provider details for device", http.StatusInternalServerError)
			return
		}

		pClient, conn, err := s.getProviderClient(providerIP, 9091)
		if err != nil {
			_ = s.db.CloseSession(serial)
			http.Error(w, fmt.Sprintf("Failed to connect to provider: %v", err), http.StatusBadGateway)
			return
		}
		defer conn.Close()

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		resp, err := pClient.ClaimDevice(ctx, &providerpb.ClaimDeviceRequest{
			Serial:    serial,
			ClaimedBy: claimedBy,
		})
		if err != nil || !resp.Success {
			_ = s.db.CloseSession(serial)
			msg := "Provider claim call failed"
			if err != nil {
				msg = fmt.Sprintf("Provider claim call error: %v", err)
			} else {
				msg = fmt.Sprintf("Provider claim rejected: %s", resp.Message)
			}
			http.Error(w, msg, http.StatusBadGateway)
			return
		}

		if dbErr := s.db.UpdateDeviceStreamPort(serial, int(resp.Port)); dbErr != nil {
			slog.Error("coordinator: failed to update device stream port in DB", "serial", serial, "port", resp.Port, "err", dbErr)
		} else {
			slog.Info("coordinator: device claimed successfully and stream port stored", "serial", serial, "port", resp.Port)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":    true,
			"session_id": sessionID,
			"port":       resp.Port,
			"message":    "Device claimed successfully",
		})

		s.wsManager.Broadcast("DEVICE_CLAIMED", map[string]interface{}{
			"serial":     serial,
			"session_id": sessionID,
			"port":       resp.Port,
			"claimed_by": claimedBy,
		})

	} else if action == "release" {
		slog.Info("coordinator: release requested", "serial", serial)

		providerIP, _, err := s.db.GetDeviceProvider(serial)
		if err != nil && err != sql.ErrNoRows {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if err := s.db.CloseSession(serial); err != nil {
			http.Error(w, fmt.Sprintf("Release DB transition failed: %v", err), http.StatusInternalServerError)
			return
		}

		if providerIP != "" {
			pClient, conn, err := s.getProviderClient(providerIP, 9091)
			if err == nil {
				defer conn.Close()
				ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
				defer cancel()
				_, _ = pClient.ReleaseDevice(ctx, &providerpb.ReleaseDeviceRequest{
					Serial: serial,
				})
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Device released successfully",
		})

		s.wsManager.Broadcast("DEVICE_RELEASED", map[string]interface{}{
			"serial": serial,
		})
	} else if action == "control" {
		var reqBody struct {
			Type    string `json:"type"`
			KeyCode int32  `json:"keycode,omitempty"`
			Text    string `json:"text,omitempty"`
			Command string `json:"command,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		providerIP, _, err := s.db.GetDeviceProvider(serial)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		pClient, conn, err := s.getProviderClient(providerIP, 9091)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer conn.Close()

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		stream, err := pClient.ControlDevice(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var req *providerpb.ControlRequest
		switch reqBody.Type {
		case "key":
			req = &providerpb.ControlRequest{
				Serial: serial,
				Event: &providerpb.ControlRequest_Key{
					Key: &providerpb.KeyEvent{
						Action:  providerpb.KeyEvent_DOWN,
						KeyCode: reqBody.KeyCode,
					},
				},
			}
		case "text":
			req = &providerpb.ControlRequest{
				Serial: serial,
				Event: &providerpb.ControlRequest_Text{
					Text: &providerpb.TextEvent{
						Text: reqBody.Text,
					},
				},
			}
		case "rotate":
			req = &providerpb.ControlRequest{
				Serial: serial,
				Event: &providerpb.ControlRequest_Rotate{
					Rotate: &providerpb.RotateEvent{
						Rotation: reqBody.KeyCode,
					},
				},
			}
		case "shell":
			req = &providerpb.ControlRequest{
				Serial: serial,
				Event: &providerpb.ControlRequest_Shell{
					Shell: &providerpb.ShellCommandRequest{
						Command: reqBody.Command,
					},
				},
			}
		default:
			http.Error(w, "Unsupported control type", http.StatusBadRequest)
			return
		}

		if err := stream.Send(req); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		resp, err := stream.Recv()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": resp.Success,
			"message": resp.Message,
		})
	}
}

func uuidString() string {
	return uuid.New().String()
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("coordinator: failed to upgrade to websocket", "err", err)
		return
	}

	s.wsManager.AddClient(conn)
	defer s.wsManager.RemoveClient(conn)

	list, err := s.getDevicesList()
	if err == nil {
		msg := WSEvent{
			Event: "DEVICE_LIST_UPDATE",
			Data:  list,
		}
		b, _ := json.Marshal(msg)
		conn.WriteMessage(websocket.TextMessage, b)
	}

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}

func (s *Server) handleScripts(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		list, err := s.db.ListScripts()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
		return
	}

	if r.Method == http.MethodPost {
		var reqBody struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}

		if reqBody.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}
		if reqBody.Content == "" {
			http.Error(w, "content is required", http.StatusBadRequest)
			return
		}

		_, err := automation.ParseScript(strings.NewReader(reqBody.Content))
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid script YAML: %v", err), http.StatusBadRequest)
			return
		}

		id := reqBody.ID
		if id == "" {
			id = uuid.New().String()
		}

		if err := s.db.SaveScript(id, reqBody.Name, reqBody.Content); err != nil {
			http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"id":      id,
		})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleScriptByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/automation/scripts/")
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, "invalid script ID", http.StatusBadRequest)
		return
	}

	if r.Method == http.MethodGet {
		name, content, err := s.db.GetScript(id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "script not found", http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      id,
			"name":    name,
			"content": content,
		})
		return
	}

	if r.Method == http.MethodDelete {
		if err := s.db.DeleteScript(id); err != nil {
			http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
		})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleRunScript(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var reqBody struct {
		ScriptID      string   `json:"script_id"`
		DeviceSerials []string `json:"device_serials"`
	}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if reqBody.ScriptID == "" {
		http.Error(w, "script_id is required", http.StatusBadRequest)
		return
	}
	if len(reqBody.DeviceSerials) == 0 {
		http.Error(w, "device_serials array is required and cannot be empty", http.StatusBadRequest)
		return
	}

	scriptName, scriptContent, err := s.db.GetScript(reqBody.ScriptID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "script not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	script, err := automation.ParseScript(strings.NewReader(scriptContent))
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to parse retrieved script YAML: %v", err), http.StatusInternalServerError)
		return
	}

	tasks := make([]automation.Task, len(reqBody.DeviceSerials))
	for i, serial := range reqBody.DeviceSerials {
		tasks[i] = automation.Task{
			Serial: serial,
			Script: script,
			ExecuteFn: func(ctx context.Context, taskSerial string, taskScript *automation.Script) (*automation.Report, error) {
				providerIP, _, err := s.db.GetDeviceProvider(taskSerial)
				if err != nil {
					return nil, fmt.Errorf("failed to locate provider for device %s: %w", taskSerial, err)
				}

				pClient, conn, err := s.getProviderClient(providerIP, 9091)
				if err != nil {
					return nil, fmt.Errorf("failed to dial provider %s: %w", providerIP, err)
				}
				defer conn.Close()

				resp, err := pClient.ExecuteScript(ctx, &providerpb.ExecuteScriptRequest{
					Serial:     taskSerial,
					ScriptYaml: scriptContent,
				})
				if err != nil {
					return nil, fmt.Errorf("execute script RPC failed: %w", err)
				}

				if !resp.Success && resp.ReportJson == "" {
					return nil, fmt.Errorf("execution error: %s", resp.Error)
				}

				var rep automation.Report
				if err := json.Unmarshal([]byte(resp.ReportJson), &rep); err != nil {
					return nil, fmt.Errorf("failed to unmarshal report json: %w", err)
				}

				return &rep, nil
			},
		}
	}

	slog.Info("coordinator: triggering parallel script run", "script_id", reqBody.ScriptID, "name", scriptName, "devices", len(reqBody.DeviceSerials))
	taskResults := s.scheduler.RunParallel(r.Context(), tasks)

	type RunResultJSON struct {
		Serial     string `json:"serial"`
		ReportID   string `json:"report_id"`
		Success    bool   `json:"success"`
		DurationMs int64  `json:"duration_ms"`
		Error      string `json:"error,omitempty"`
	}

	results := make([]RunResultJSON, len(taskResults))
	for i, res := range taskResults {
		reportID := uuid.New().String()
		results[i] = RunResultJSON{
			Serial:   res.Serial,
			ReportID: reportID,
		}

		if res.Err != nil {
			results[i].Success = false
			results[i].Error = res.Err.Error()

			now := time.Now()
			placeholderReport := &automation.Report{
				StartTime: now,
				EndTime:   now,
				Success:   false,
				Results:   []automation.StepResult{},
			}
			repBytes, _ := json.Marshal(placeholderReport)
			_ = s.db.SaveReport(reportID, reqBody.ScriptID, res.Serial, false, now, now, string(repBytes))
		} else {
			results[i].Success = res.Report.Success
			results[i].DurationMs = res.Report.DurationMs
			if !res.Report.Success {
				for _, stepRes := range res.Report.Results {
					if !stepRes.Success && stepRes.Error != "" {
						results[i].Error = stepRes.Error
						break
					}
				}
			}

			repBytes, _ := json.Marshal(res.Report)
			_ = s.db.SaveReport(reportID, reqBody.ScriptID, res.Serial, res.Report.Success, res.Report.StartTime, res.Report.EndTime, string(repBytes))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"results": results,
	})
}

func (s *Server) handleReports(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	list, err := s.db.ListReports()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

func (s *Server) handleReportByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/automation/reports/")
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, "invalid report ID", http.StatusBadRequest)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	scriptID, serial, success, startTime, endTime, resultsJSON, err := s.db.GetReport(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "report not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":         id,
		"script_id":  scriptID,
		"serial":     serial,
		"success":    success,
		"start_time": startTime,
		"end_time":   endTime,
		"results":    json.RawMessage(resultsJSON),
	})
}
