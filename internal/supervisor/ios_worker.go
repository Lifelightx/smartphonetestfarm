package supervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// IOSWorker manages the lifecycle of the background WebDriverAgent and port forward tunnels.
type IOSWorker struct {
	serial string
	port   int
	cancel context.CancelFunc
	done   chan struct{}
}

// NewIOSWorker instantiates a new IOSWorker for a given device serial and local proxy port.
func NewIOSWorker(serial string, port int) *IOSWorker {
	return &IOSWorker{
		serial: serial,
		port:   port,
		done:   make(chan struct{}),
	}
}

// Start launches runwda and port forwarding tunnels in background goroutines.
func (w *IOSWorker) Start(ctx context.Context) error {
	wCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel

	// Clean up any existing/orphaned ios processes for this device before starting
	slog.Info("ios worker: cleaning up any orphaned ios processes for device", "serial", w.serial)
	cleanupCmd := exec.Command("pkill", "-f", "ios --udid="+w.serial)
	_ = cleanupCmd.Run()
	time.Sleep(500 * time.Millisecond)

	// Check if Developer Disk Image is mounted and log a diagnostic warning if missing.
	imgCmd := exec.Command("ios", "--udid="+w.serial, "image", "list")
	if imgOut, err := imgCmd.Output(); err == nil {
		if strings.Contains(string(imgOut), `"none"`) || !strings.Contains(string(imgOut), `"signature"`) {
			slog.Warn("ios worker: WARNING - No Developer Disk Image (DDI) is mounted on this device. WebDriverAgent (WDA) will fail to start. Please mount the DDI (e.g., using Xcode, or by running 'ios image auto') and ensure Developer Mode is enabled on the device.", "serial", w.serial)
		}
	}

	// Calculate ports for the tunnel (using +1000 and +2000 offsets).
	// Since base ports are spaced by 10, these will never overlap.
	tunnelInfoPort := w.port + 1000
	userspacePort := w.port + 2000

	slog.Info("ios worker: starting userspace tunnel, WDA and port forwarding",
		"serial", w.serial, "port", w.port, "tunnelInfoPort", tunnelInfoPort, "userspacePort", userspacePort)

	var wg sync.WaitGroup

	// 1. Run Tunnel start: ios --udid=<udid> tunnel start --userspace
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-wCtx.Done():
				return
			default:
			}
			cmd := exec.CommandContext(wCtx, "ios", "--udid="+w.serial,
				fmt.Sprintf("--tunnel-info-port=%d", tunnelInfoPort),
				fmt.Sprintf("--userspace-port=%d", userspacePort),
				"tunnel", "start", "--userspace",
			)
			slog.Debug("ios worker: running tunnel start command", "serial", w.serial)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			err := cmd.Run()
			if wCtx.Err() != nil {
				return
			}
			slog.Warn("ios worker: tunnel process exited", "serial", w.serial, "err", err, "stderr", strings.TrimSpace(stderr.String()))
			select {
			case <-wCtx.Done():
				return
			case <-time.After(1 * time.Second):
			}
		}
	}()

	// Coordinator goroutine that waits for tunnel negotiation before starting dependent commands.
	wg.Add(1)
	go func() {
		defer wg.Done()

		slog.Info("ios worker: waiting for tunnel negotiation...", "serial", w.serial)
		if err := w.waitUntilTunnelReady(wCtx, tunnelInfoPort); err != nil {
			slog.Error("ios worker: tunnel negotiation failed or context canceled", "serial", w.serial, "err", err)
			cancel()
			return
		}

		var subWg sync.WaitGroup

		// 2. Run WebDriverAgent: ios --udid=<udid> runwda
		subWg.Add(1)
		go func() {
			defer subWg.Done()
			failCount := 0
			for {
				select {
				case <-wCtx.Done():
					return
				default:
				}
				cmd := exec.CommandContext(wCtx, "ios", "--udid="+w.serial,
					fmt.Sprintf("--tunnel-info-port=%d", tunnelInfoPort),
					"runwda",
				)
				slog.Debug("ios worker: running runwda command", "serial", w.serial)
				var stderr bytes.Buffer
				cmd.Stderr = &stderr
				err := cmd.Run()
				if wCtx.Err() != nil {
					return
				}
				slog.Warn("ios worker: runwda process exited", "serial", w.serial, "err", err, "stderr", strings.TrimSpace(stderr.String()))

				failCount++
				if failCount >= 5 {
					slog.Error("ios worker: runwda failed 5 times. WebDriverAgent/WDA might not be installed on this phone. Stopping all worker services for this device.", "serial", w.serial)
					cancel()
					return
				}

				select {
				case <-wCtx.Done():
					return
				case <-time.After(1 * time.Second):
				}
			}
		}()

		// 3. Run port forward: ios --udid=<udid> forward <port> 8100
		subWg.Add(1)
		go func() {
			defer subWg.Done()
			// Small delay to let WDA service initialize
			select {
			case <-wCtx.Done():
				return
			case <-time.After(2 * time.Second):
			}

			for {
				select {
				case <-wCtx.Done():
					return
				default:
				}
				cmd := exec.CommandContext(wCtx, "ios", "--udid="+w.serial,
					fmt.Sprintf("--tunnel-info-port=%d", tunnelInfoPort),
					"forward", fmt.Sprintf("%d", w.port), "8100",
				)
				slog.Debug("ios worker: running port forward command", "serial", w.serial, "localPort", w.port)
				var stderr bytes.Buffer
				cmd.Stderr = &stderr
				err := cmd.Run()
				if wCtx.Err() != nil {
					return
				}
				slog.Warn("ios worker: port forward process exited", "serial", w.serial, "err", err, "stderr", strings.TrimSpace(stderr.String()))
				select {
				case <-wCtx.Done():
					return
				case <-time.After(1 * time.Second):
				}
			}
		}()

		// 4. Run MJPEG port forward: ios --udid=<udid> forward <port+1> 9100
		subWg.Add(1)
		go func() {
			defer subWg.Done()
			// Small delay to let WDA service initialize
			select {
			case <-wCtx.Done():
				return
			case <-time.After(2 * time.Second):
			}

			for {
				select {
				case <-wCtx.Done():
					return
				default:
				}
				cmd := exec.CommandContext(wCtx, "ios", "--udid="+w.serial,
					fmt.Sprintf("--tunnel-info-port=%d", tunnelInfoPort),
					"forward", fmt.Sprintf("%d", w.port+1), "9100",
				)
				slog.Debug("ios worker: running MJPEG port forward command", "serial", w.serial, "localPort", w.port+1)
				var stderr bytes.Buffer
				cmd.Stderr = &stderr
				err := cmd.Run()
				if wCtx.Err() != nil {
					return
				}
				slog.Warn("ios worker: MJPEG port forward process exited", "serial", w.serial, "err", err, "stderr", strings.TrimSpace(stderr.String()))
				select {
				case <-wCtx.Done():
					return
				case <-time.After(1 * time.Second):
				}
			}
		}()

		// 5. Poll WDA status and create session
		subWg.Add(1)
		go func() {
			defer subWg.Done()
			// Wait for WDA and forwarding to establish
			select {
			case <-wCtx.Done():
				return
			case <-time.After(4 * time.Second):
			}

			// Poll WDA status and create session
			wdaURL := fmt.Sprintf("http://127.0.0.1:%d", w.port)
			for i := 0; i < 30; i++ {
				if wCtx.Err() != nil {
					return
				}
				resp, err := http.Get(wdaURL + "/status")
				if err == nil {
					resp.Body.Close()
					if resp.StatusCode == http.StatusOK {
						// WDA is ready, create session
						req, _ := http.NewRequestWithContext(wCtx, "POST", wdaURL+"/session", strings.NewReader(`{"capabilities":{}}`))
						req.Header.Set("Content-Type", "application/json")
						sResp, sErr := http.DefaultClient.Do(req)
						if sErr == nil {
							sResp.Body.Close()
							slog.Info("ios worker: created wda session, waking up device", "serial", w.serial)
							break
						} else {
							slog.Debug("ios worker: failed to create wda session, retrying", "err", sErr)
						}
					}
				}
				time.Sleep(1 * time.Second)
			}
		}()

		subWg.Wait()
	}()

	go func() {
		wg.Wait()
		close(w.done)
	}()

	return nil
}

// waitUntilTunnelReady polls the tunnel info server until the tunnel for w.serial is negotiated.
func (w *IOSWorker) waitUntilTunnelReady(ctx context.Context, tunnelInfoPort int) error {
	client := &http.Client{Timeout: 1 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/tunnels", tunnelInfoPort)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err == nil {
			resp, rErr := client.Do(req)
			if rErr == nil {
				var tunnels []struct {
					Address          string `json:"address"`
					RsdPort          int    `json:"rsdPort"`
					Udid             string `json:"udid"`
					UserspaceTun     bool   `json:"userspaceTun"`
					UserspaceTunPort int    `json:"userspaceTunPort"`
				}
				decodeErr := json.NewDecoder(resp.Body).Decode(&tunnels)
				resp.Body.Close()
				if decodeErr == nil {
					for _, t := range tunnels {
						if strings.EqualFold(t.Udid, w.serial) && t.Address != "" && t.RsdPort > 0 {
							slog.Info("ios worker: tunnel successfully negotiated and ready", "serial", w.serial, "address", t.Address, "rsdPort", t.RsdPort)
							return nil
						}
					}
				}
			}
		}

		time.Sleep(500 * time.Millisecond)
	}
}

// Stop terminates WDA and port forwarding.
func (w *IOSWorker) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
	<-w.done
	// Clean up any remaining processes to be 100% sure nothing is left orphaned
	cleanupCmd := exec.Command("pkill", "-f", "ios --udid="+w.serial)
	_ = cleanupCmd.Run()
	slog.Info("ios worker: stopped", "serial", w.serial)
}

