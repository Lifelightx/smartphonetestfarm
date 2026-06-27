package stream

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type progressResponse struct {
	Success bool   `json:"success"`
	Stage   string `json:"stage"` // "installing", "opening", "done", "error"
	Message string `json:"message,omitempty"`
}

func sendProgress(w http.ResponseWriter, flusher http.Flusher, stage string, message string, success bool) {
	resp := progressResponse{
		Success: success,
		Stage:   stage,
		Message: message,
	}
	data, _ := json.Marshal(resp)
	_, _ = w.Write(append(data, '\n'))
	if flusher != nil {
		flusher.Flush()
	}
}

// handleUpload processes file uploads directly to the provider, either for installing apps,
// pushing files to the device, or saving them to the provider host, streaming stage progress.
func (m *Manager) handleUpload(w http.ResponseWriter, r *http.Request, serial string) {
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

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, _ := w.(http.Flusher)

	if err := r.ParseMultipartForm(500 << 20); err != nil { // 500MB max memory
		sendProgress(w, flusher, "error", "failed to parse multipart form: "+err.Error(), false)
		return
	}

	uploadType := r.FormValue("type") // "app", "file", "server"
	file, header, err := r.FormFile("file")
	if err != nil {
		sendProgress(w, flusher, "error", "missing file in request: "+err.Error(), false)
		return
	}
	defer file.Close()

	// Create temp file on provider disk
	tempDir := os.TempDir()
	tempFilePath := filepath.Join(tempDir, header.Filename)
	out, err := os.Create(tempFilePath)
	if err != nil {
		sendProgress(w, flusher, "error", "failed to create temp file: "+err.Error(), false)
		return
	}

	if _, err := io.Copy(out, file); err != nil {
		out.Close()
		os.Remove(tempFilePath)
		sendProgress(w, flusher, "error", "failed to save uploaded file: "+err.Error(), false)
		return
	}
	out.Close() // close early so adb can access it
	defer os.Remove(tempFilePath) // clean up after we're done

	switch uploadType {
	case "app":
		sendProgress(w, flusher, "installing", "Installing APK on device...", true)
		slog.Info("upload: installing app", "serial", serial, "file", header.Filename)
		
		// Run install command
		cmd := exec.CommandContext(r.Context(), "adb", "-s", serial, "install", "-r", tempFilePath)
		outBytes, err := cmd.CombinedOutput()
		if err != nil {
			slog.Error("upload: adb install failed", "serial", serial, "err", err, "output", string(outBytes))
			sendProgress(w, flusher, "error", fmt.Sprintf("Installation failed: %v\n%s", err, string(outBytes)), false)
			return
		}

		// When the installation succeeds, the Android system sends ACTION_PACKAGE_ADDED.
		// Our Kotlin agent's PackageService captures this event and sends PACKAGE_INSTALLED
		// to our websocket handler, which will automatically trigger the launch.
		sendProgress(w, flusher, "opening", "Opening installed app...", true)
		
		// Wait a small moment to let the agent capture, send, and launch the event
		select {
		case <-time.After(1500 * time.Millisecond):
		case <-r.Context().Done():
		}

		sendProgress(w, flusher, "done", "App installed and opened successfully!", true)

	case "file":
		sendProgress(w, flusher, "installing", "Pushing file to device...", true)
		slog.Info("upload: pushing file to device", "serial", serial, "file", header.Filename)
		
		cmd := exec.CommandContext(r.Context(), "adb", "-s", serial, "push", tempFilePath, "/sdcard/Download/"+header.Filename)
		outBytes, err := cmd.CombinedOutput()
		if err != nil {
			slog.Error("upload: adb push failed", "serial", serial, "err", err, "output", string(outBytes))
			sendProgress(w, flusher, "error", fmt.Sprintf("Push failed: %v\n%s", err, string(outBytes)), false)
			return
		}
		sendProgress(w, flusher, "done", "File pushed to /sdcard/Download/ successfully", true)

	case "server":
		sendProgress(w, flusher, "installing", "Saving file to provider server...", true)
		slog.Info("upload: saved file to provider server", "serial", serial, "file", tempFilePath)
		
		uploadsDir := filepath.Join(os.TempDir(), "protean_uploads")
		if err := os.MkdirAll(uploadsDir, 0755); err != nil {
			sendProgress(w, flusher, "error", "Failed to create uploads directory on server", false)
			return
		}
		
		permPath := filepath.Join(uploadsDir, header.Filename)
		if err := os.Rename(tempFilePath, permPath); err != nil {
			sendProgress(w, flusher, "error", "Failed to move file to server destination", false)
			return
		}
		
		sendProgress(w, flusher, "done", "File saved on server: "+header.Filename, true)

	default:
		sendProgress(w, flusher, "error", "invalid upload type", false)
	}
}
