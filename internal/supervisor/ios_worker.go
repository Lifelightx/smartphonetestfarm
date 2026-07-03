package supervisor

import (
	"context"
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

	slog.Info("ios worker: starting userspace tunnel, WDA and port forwarding", "serial", w.serial, "port", w.port)

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
			cmd := exec.CommandContext(wCtx, "ios", "--udid="+w.serial, "tunnel", "start", "--userspace")
			slog.Debug("ios worker: running tunnel start command", "serial", w.serial)
			err := cmd.Run()
			if wCtx.Err() != nil {
				return
			}
			slog.Warn("ios worker: tunnel process exited", "serial", w.serial, "err", err)
			select {
			case <-wCtx.Done():
				return
			case <-time.After(1 * time.Second):
			}
		}
	}()

	// 2. Run WebDriverAgent: ios --udid=<udid> runwda
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Wait for tunnel negotiation
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
			cmd := exec.CommandContext(wCtx, "ios", "--udid="+w.serial, "runwda")
			slog.Debug("ios worker: running runwda command", "serial", w.serial)
			err := cmd.Run()
			if wCtx.Err() != nil {
				return
			}
			slog.Warn("ios worker: runwda process exited", "serial", w.serial, "err", err)
			select {
			case <-wCtx.Done():
				return
			case <-time.After(1 * time.Second):
			}
		}
	}()

	// 3. Run port forward: ios --udid=<udid> forward <port> 8100
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Wait for WDA startup
		select {
		case <-wCtx.Done():
			return
		case <-time.After(4 * time.Second):
		}

		for {
			select {
			case <-wCtx.Done():
				return
			default:
			}
			cmd := exec.CommandContext(wCtx, "ios", "--udid="+w.serial, "forward", fmt.Sprintf("%d", w.port), "8100")
			slog.Debug("ios worker: running port forward command", "serial", w.serial, "localPort", w.port)
			err := cmd.Run()
			if wCtx.Err() != nil {
				return
			}
			slog.Warn("ios worker: port forward process exited", "serial", w.serial, "err", err)
			select {
			case <-wCtx.Done():
				return
			case <-time.After(1 * time.Second):
			}
		}
	}()

	// 4. Run MJPEG port forward: ios --udid=<udid> forward <port+1> 9100
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Wait for WDA startup
		select {
		case <-wCtx.Done():
			return
		case <-time.After(4 * time.Second):
		}

		for {
			select {
			case <-wCtx.Done():
				return
			default:
			}
			cmd := exec.CommandContext(wCtx, "ios", "--udid="+w.serial, "forward", fmt.Sprintf("%d", w.port+1), "9100")
			slog.Debug("ios worker: running MJPEG port forward command", "serial", w.serial, "localPort", w.port+1)
			err := cmd.Run()
			if wCtx.Err() != nil {
				return
			}
			slog.Warn("ios worker: MJPEG port forward process exited", "serial", w.serial, "err", err)
			select {
			case <-wCtx.Done():
				return
			case <-time.After(1 * time.Second):
			}
		}
	}()

	// 4. Poll WDA status and create session
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Wait for WDA and tunnel to be ready
		select {
		case <-wCtx.Done():
			return
		case <-time.After(5 * time.Second):
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

	go func() {
		wg.Wait()
		close(w.done)
	}()

	return nil
}

// Stop terminates WDA and port forwarding.
func (w *IOSWorker) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
	<-w.done
	slog.Info("ios worker: stopped", "serial", w.serial)
}

