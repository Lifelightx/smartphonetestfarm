package stream

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/jpeg" // register JPEG decoder
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"protean-provider/internal/agent"
	"protean-provider/internal/automation"
	"protean-provider/internal/config"
	"protean-provider/internal/domain"
	"protean-provider/internal/wda"
)

const scrcpyServerVersion = "4.0"

// Manager implements domain.StreamManager.
// It starts the scrcpy-server on-device, reads raw H.264, and broadcasts to clients.
type Manager struct {
	cfg      *config.Config
	registry domain.DeviceRegistry
	mu       sync.Mutex
	streams  map[string]*streamState
	recorder *automation.RecorderManager
	agentFn  func(serial string) *agent.Agent
	wdaFn    func(serial string) (*wda.Client, error)
}

// NewManager creates a new stream Manager.
func NewManager(cfg *config.Config, registry domain.DeviceRegistry) *Manager {
	return &Manager{
		cfg:      cfg,
		registry: registry,
		streams:  make(map[string]*streamState),
		recorder: automation.NewRecorderManager(),
	}
}

// SetAgentProvider registers a callback function to lookup the active agent.
func (m *Manager) SetAgentProvider(fn func(serial string) *agent.Agent) {
	m.agentFn = fn
}

// SetWDAClientProvider registers a callback function to lookup the active WDA client.
func (m *Manager) SetWDAClientProvider(fn func(serial string) (*wda.Client, error)) {
	m.wdaFn = fn
}

type gestureState struct {
	startX    float64
	startY    float64
	endX      float64 // last MOVE position (updated synchronously)
	endY      float64
	startTime time.Time
	isSwipe   bool
}

// streamState holds all live state for one device capture session.
type streamState struct {
	serial      string
	port        int
	serverCmd   *exec.Cmd // adb shell running scrcpy-server on-device
	cancel      context.CancelFunc
	done        chan struct{}
	gopCache    [][]byte // Cache starting with a keyframe
	controlMu   sync.Mutex
	controlConn net.Conn

	// videoWidth/videoHeight are updated from SPS parsing.
	videoWidth  uint16
	videoHeight uint16

	// clients: each connected client gets its own buffered channel of chunks.
	clientsMu sync.RWMutex
	clients   map[chan<- []byte]struct{}

	gestureMu sync.Mutex
	gesture   gestureState

	wdaClient     *wda.Client
	logicalWidth  float64
	logicalHeight float64
}

func (s *streamState) addClientAndGetCache(ch chan<- []byte) [][]byte {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	s.clients[ch] = struct{}{}

	var cachedGOP [][]byte
	if len(s.gopCache) > 0 {
		cachedGOP = make([][]byte, len(s.gopCache))
		copy(cachedGOP, s.gopCache)
	}
	return cachedGOP
}

func (s *streamState) removeClient(ch chan<- []byte) {
	s.clientsMu.Lock()
	delete(s.clients, ch)
	s.clientsMu.Unlock()
}

func (s *streamState) broadcast(chunk []byte) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	for ch := range s.clients {
		select {
		case ch <- chunk:
		default: // drop if client is too slow
		}
	}
}

// StartCapture starts the on-device scrcpy-server, reads its H.264 output,
// and serves it on port via HTTP.
func (m *Manager) StartCapture(ctx context.Context, serial string, port int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.streams[serial]; ok {
		slog.Warn("stream: capture already active", "serial", serial)
		return nil
	}

	device, err := m.registry.Get(serial)
	if err == nil && strings.EqualFold(device.Platform, "ios") {
		return m.startIOSCaptureLocked(ctx, serial, port)
	}

	maxFPS := m.cfg.Stream.MaxFPS
	if maxFPS <= 0 {
		maxFPS = 15
	}

	sCtx, cancel := context.WithCancel(context.Background())
	s := &streamState{
		serial:  serial,
		port:    port,
		cancel:  cancel,
		done:    make(chan struct{}),
		clients: make(map[chan<- []byte]struct{}),
	}

	// 1. Ensure the correct scrcpy-server.jar is pushed to the device
	if err := PushScrcpyServer(sCtx, serial); err != nil {
		slog.Warn("stream: failed to push scrcpy-server.jar to device (trying to run anyway)", "serial", serial, "err", err)
	}

	// 2. adb forward: host localVideoPort → device localabstract socket
	localVideoPort := port + 2000
	scidStr := fmt.Sprintf("%08x", port)
	remoteSocket := fmt.Sprintf("localabstract:scrcpy_%s", scidStr)
	if err := adbForward(sCtx, serial, localVideoPort, remoteSocket); err != nil {
		cancel()
		return fmt.Errorf("stream: adb forward: %w", err)
	}

	// 3. Start scrcpy-server on-device via adb shell
	serverCmd := exec.CommandContext(sCtx, "adb", "-s", serial,
		"shell",
		"CLASSPATH="+ScrcpyServerJarOnDevice,
		"app_process", "/",
		"com.genymobile.scrcpy.Server",
		scrcpyServerVersion,
		"scid="+scidStr,
		"video=true",
		"audio=false",
		"control=true",
		"cleanup=false",
		"send_dummy_byte=false",
		"send_device_meta=false",
		"send_stream_meta=false",
		"send_frame_meta=true",
		"video_codec=h264",
		"video_codec_options=i-frame-interval=1",
		"max_size=1080",
		"max_fps="+strconv.Itoa(maxFPS),
		"tunnel_forward=true",
	)

	// Set up pipes to redirect and log output
	stdoutPipe, err := serverCmd.StdoutPipe()
	if err == nil {
		go func() {
			scanner := bufio.NewScanner(stdoutPipe)
			for scanner.Scan() {
				slog.Info("scrcpy-server [stdout]", "serial", serial, "msg", scanner.Text())
			}
		}()
	}
	stderrPipe, err := serverCmd.StderrPipe()
	if err == nil {
		go func() {
			scanner := bufio.NewScanner(stderrPipe)
			for scanner.Scan() {
				slog.Info("scrcpy-server [stderr]", "serial", serial, "msg", scanner.Text())
			}
		}()
	}

	if err := serverCmd.Start(); err != nil {
		_ = adbForwardRemove(context.Background(), serial, localVideoPort)
		cancel()
		return fmt.Errorf("stream: start scrcpy-server: %w", err)
	}
	s.serverCmd = serverCmd

	// Monitor scrcpy process lifecycle
	go func() {
		waitErr := serverCmd.Wait()
		if waitErr != nil {
			slog.Error("scrcpy-server process exited", "serial", serial, "err", waitErr)
		} else {
			slog.Info("scrcpy-server process exited cleanly", "serial", serial)
		}
	}()

	// 3. Start HTTP server immediately so StartCapture returns fast (no gRPC timeout / 502).
	mux := http.NewServeMux()
	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		m.handleStreamClient(w, r, serial)
	})
	mux.HandleFunc("/control", func(w http.ResponseWriter, r *http.Request) {
		m.handleControl(w, r, serial)
	})
	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		m.handleUpload(w, r, serial)
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		m.handleWS(w, r, serial)
	})
	mux.HandleFunc("/state", func(w http.ResponseWriter, r *http.Request) {
		m.handleState(w, r, serial)
	})
	httpServer := &http.Server{Addr: fmt.Sprintf(":%d", port), Handler: mux}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		_ = serverCmd.Process.Kill()
		_ = adbForwardRemove(context.Background(), serial, localVideoPort)
		cancel()
		return fmt.Errorf("stream: listen port %d: %w", port, err)
	}

	go func() {
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			slog.Error("stream: HTTP server error", "serial", serial, "err", err)
		}
	}()

	// 4. Background goroutine: dial scrcpy sockets → read H.264 → broadcast.
	go func() {
		defer close(s.done)
		defer func() {
			_ = httpServer.Close()
			if serverCmd.Process != nil {
				_ = serverCmd.Process.Kill()
			}
			_ = adbForwardRemove(context.Background(), serial, localVideoPort)
			slog.Info("stream: capture goroutine exited", "serial", serial)
		}()

		// 4a. Connect VIDEO socket — scrcpy-server's 1st accept().
		var h264Conn io.Reader
		{
			addr := fmt.Sprintf("127.0.0.1:%d", localVideoPort)
			deadline := time.Now().Add(10 * time.Second)
			connected := false
			for !connected {
				if time.Now().After(deadline) {
					slog.Error("stream: timeout waiting for scrcpy video socket", "serial", serial)
					return
				}
				select {
				case <-sCtx.Done():
					return
				default:
				}
				c, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
				if err != nil {
					time.Sleep(300 * time.Millisecond)
					continue
				}
				_ = c.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
				probe := make([]byte, 1)
				_, rerr := c.Read(probe)
				_ = c.SetReadDeadline(time.Time{})
				if rerr == nil {
					h264Conn = io.MultiReader(bytes.NewReader(probe), c)
					slog.Info("stream: video socket connected (data)", "serial", serial)
					connected = true
				} else if netErr, ok := rerr.(net.Error); ok && netErr.Timeout() {
					h264Conn = c
					slog.Info("stream: video socket connected (awaiting control)", "serial", serial)
					connected = true
				} else {
					c.Close()
					time.Sleep(300 * time.Millisecond)
				}
			}
		}

		// 4b. Connect CONTROL socket — scrcpy-server's 2nd accept() (same forwarded port).
		{
			addr := fmt.Sprintf("127.0.0.1:%d", localVideoPort)
			deadline := time.Now().Add(5 * time.Second)
			var ctrlConn net.Conn
			for {
				if time.Now().After(deadline) {
					slog.Warn("stream: timeout connecting control socket; input disabled", "serial", serial)
					break
				}
				select {
				case <-sCtx.Done():
					return
				default:
				}
				c, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
				if err != nil {
					time.Sleep(200 * time.Millisecond)
					continue
				}
				ctrlConn = c
				slog.Info("stream: control socket connected", "serial", serial)
				break
			}
			if ctrlConn != nil {
				s.controlMu.Lock()
				s.controlConn = ctrlConn
				s.controlMu.Unlock()
				go func() {
					<-sCtx.Done()
					ctrlConn.Close()
				}()
			}
		}

		// 4c. Read H.264 frames → broadcast to HTTP clients
		m.readAndMux(sCtx, s, h264Conn, maxFPS)
	}()

	m.streams[serial] = s
	slog.Info("stream: capture started", "serial", serial, "port", port)
	return nil
}

// readAndMux reads framed H.264 from the scrcpy-server socket, builds GOP cache, and broadcasts to clients.
func (m *Manager) readAndMux(ctx context.Context, s *streamState, r io.Reader, maxFPS int) {
	var (
		seqNum   uint32
		sps, pps []byte
	)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Read 12-byte frame header: 8-byte PTS (µs) + 4-byte size
		hdr := make([]byte, 12)
		if _, err := io.ReadFull(r, hdr); err != nil {
			if ctx.Err() == nil {
				slog.Warn("stream: read frame header", "serial", s.serial, "err", err)
			}
			return
		}
		size := binary.BigEndian.Uint32(hdr[8:12])

		if size == 0 {
			continue
		}

		frameData := make([]byte, size)
		if _, err := io.ReadFull(r, frameData); err != nil {
			if ctx.Err() == nil {
				slog.Warn("stream: read frame data", "serial", s.serial, "err", err)
			}
			return
		}

		// Extract SPS/PPS from the first keyframe (or whenever they appear)
		newSPS, newPPS := extractSPSPPS(frameData)
		if newSPS != nil && newPPS != nil {
			sps, pps = newSPS, newPPS
			trackWidth, trackHeight := parseSPS(sps)
			slog.Info("track metadata", "width", trackWidth, "height", trackHeight)
			// Store video dimensions so /control can scale coordinates
			s.controlMu.Lock()
			s.videoWidth = trackWidth
			s.videoHeight = trackHeight
			s.controlMu.Unlock()
		}

		// Can't send media segments before we have SPS/PPS
		if sps == nil || pps == nil {
			continue
		}

		isKey := isKeyFrame(frameData)
		seqNum++

		if seqNum == 1 {
			slog.Info("stream: first frame parsed and broadcasted", "serial", s.serial, "size", size, "is_key", isKey)
		} else if seqNum%100 == 0 {
			slog.Info("stream: active streaming frames broadcasted", "serial", s.serial, "frame_count", seqNum)
		}

		// Prepend SPS/PPS to the keyframe if they are missing
		if isKey && sps != nil && pps != nil && (newSPS == nil || newPPS == nil) {
			prepended := make([]byte, 0, 4+len(sps)+4+len(pps)+len(frameData))
			prepended = append(prepended, 0, 0, 0, 1)
			prepended = append(prepended, sps...)
			prepended = append(prepended, 0, 0, 0, 1)
			prepended = append(prepended, pps...)
			prepended = append(prepended, frameData...)
			frameData = prepended
		}

		s.clientsMu.Lock()
		if isKey {
			s.gopCache = [][]byte{frameData}
		} else if len(s.gopCache) > 0 {
			s.gopCache = append(s.gopCache, frameData)
		}
		for ch := range s.clients {
			select {
			case ch <- frameData:
			default:
			}
		}
		s.clientsMu.Unlock()
	}
}

// isKeyFrame returns true if the Annex-B buffer contains an IDR, SPS, or PPS NAL unit.
func isKeyFrame(annexB []byte) bool {
	for _, nal := range annexBSplit(annexB) {
		if len(nal) > 0 {
			t := nalType(nal[0])
			if t == nalIDR || t == nalSPS || t == nalPPS {
				return true
			}
		}
	}
	return false
}

// StopCapture stops the capture for a device.
func (m *Manager) StopCapture(ctx context.Context, serial string) error {
	m.mu.Lock()
	s, ok := m.streams[serial]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	delete(m.streams, serial)
	m.mu.Unlock()

	slog.Info("stream: stopping capture", "serial", serial)
	s.cancel()
	<-s.done
	slog.Info("stream: capture stopped", "serial", serial)
	return nil
}

// IsCapturing returns true if capture is active for the device.
func (m *Manager) IsCapturing(serial string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.streams[serial]
	return ok
}


// scaleAndCompressJPEG decodes a JPEG, proportionally scales it so the longest
// dimension is at most maxPx, then re-encodes at the given quality (0-100).
// Returns the original bytes unchanged if any step fails.
func scaleAndCompressJPEG(data []byte, maxPx int, quality int) []byte {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return data
	}

	sb := src.Bounds()
	srcW, srcH := sb.Dx(), sb.Dy()

	// Determine scale factor so max(w,h) == maxPx.
	dstW, dstH := srcW, srcH
	if srcH > srcW {
		if srcH > maxPx {
			dstH = maxPx
			dstW = srcW * maxPx / srcH
		}
	} else {
		if srcW > maxPx {
			dstW = maxPx
			dstH = srcH * maxPx / srcW
		}
	}
	if dstW < 1 {
		dstW = 1
	}
	if dstH < 1 {
		dstH = 1
	}

	// Nearest-neighbour downscale using only stdlib (image/draw is unused to
	// avoid import; we iterate manually over dst pixels).
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	for y := 0; y < dstH; y++ {
		srcY := y * srcH / dstH
		for x := 0; x < dstW; x++ {
			srcX := x * srcW / dstW
			dst.Set(x, y, src.At(sb.Min.X+srcX, sb.Min.Y+srcY))
		}
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: quality}); err != nil {
		return data
	}
	return buf.Bytes()
}

func (m *Manager) startIOSCaptureLocked(ctx context.Context, serial string, port int) error {
	sCtx, cancel := context.WithCancel(context.Background())
	s := &streamState{
		serial:  serial,
		port:    port,
		cancel:  cancel,
		done:    make(chan struct{}),
		clients: make(map[chan<- []byte]struct{}),
	}

	// 1. Start HTTP server immediately so StartCapture returns fast.
	mux := http.NewServeMux()
	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		m.handleStreamClient(w, r, serial)
	})
	mux.HandleFunc("/control", func(w http.ResponseWriter, r *http.Request) {
		m.handleControl(w, r, serial)
	})
	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		m.handleUpload(w, r, serial)
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		m.handleWS(w, r, serial)
	})
	mux.HandleFunc("/state", func(w http.ResponseWriter, r *http.Request) {
		m.handleState(w, r, serial)
	})
	httpServer := &http.Server{Addr: fmt.Sprintf(":%d", port), Handler: mux}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		cancel()
		return fmt.Errorf("stream: listen port %d: %w", port, err)
	}

	go func() {
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			slog.Error("stream: HTTP server error", "serial", serial, "err", err)
		}
	}()

	s.wdaClient = wda.NewClient(port + 3000)

	go func() {
		// Try to ensure WDA session to trigger the screen updates/mirroring
		wdaCtx, wdaCancel := context.WithTimeout(sCtx, 30*time.Second)
		defer wdaCancel()
		for i := 0; i < 15; i++ {
			select {
			case <-sCtx.Done():
				return
			default:
			}
			if err := s.wdaClient.EnsureSession(wdaCtx); err == nil {
				slog.Info("stream: WDA session ensured successfully on startup", "serial", serial)
				break
			}
			time.Sleep(2 * time.Second)
		}
	}()

	// 2. Background goroutine: connect to WDA MJPEG stream endpoint -> read multipart -> broadcast.
	go func() {
		defer close(s.done)
		defer func() {
			_ = httpServer.Close()
			slog.Info("stream: iOS MJPEG capture goroutine exited", "serial", serial)
		}()

		wdaStreamURL := fmt.Sprintf("http://127.0.0.1:%d", port+3001)
		var resp *http.Response
		var dialErr error

		client := &http.Client{
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout: 5 * time.Second,
				}).DialContext,
				ResponseHeaderTimeout: 5 * time.Second,
			},
		}

		// Keep retrying to connect to WDA stream endpoint
		for i := 0; i < 60; i++ {
			select {
			case <-sCtx.Done():
				return
			default:
			}
			req, err := http.NewRequestWithContext(sCtx, "GET", wdaStreamURL, nil)
			if err != nil {
				slog.Error("stream: failed to create request to WDA stream", "err", err)
				return
			}
			resp, dialErr = client.Do(req)
			if dialErr != nil {
				time.Sleep(500 * time.Millisecond)
				continue
			}
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				dialErr = fmt.Errorf("bad status code: %d", resp.StatusCode)
				time.Sleep(500 * time.Millisecond)
				continue
			}
			break
		}

		if dialErr != nil || resp == nil {
			slog.Error("stream: could not connect to WDA MJPEG stream", "serial", serial, "url", wdaStreamURL, "err", dialErr)
			return
		}
		defer resp.Body.Close()
		slog.Info("stream: connected to WDA MJPEG stream", "serial", serial, "url", wdaStreamURL)

		contentType := resp.Header.Get("Content-Type")
		var boundary string
		if _, params, err := mime.ParseMediaType(contentType); err == nil {
			boundary = params["boundary"]
		}
		if boundary == "" {
			// Fallback simple parsing
			const bprefix = "boundary="
			if idx := strings.Index(contentType, bprefix); idx != -1 {
				boundary = contentType[idx+len(bprefix):]
				boundary = strings.Trim(boundary, "\r\n \"")
			}
		}

		if boundary == "" {
			// If still empty, assume standard WDA boundary
			boundary = "gmirendermjpegstream"
		}

		boundary = strings.TrimPrefix(boundary, "--")

		mr := multipart.NewReader(resp.Body, boundary)
		var seqNum uint32

		for {
			select {
			case <-sCtx.Done():
				return
			default:
			}

			part, err := mr.NextPart()
			if err != nil {
				if sCtx.Err() == nil {
					slog.Warn("stream: failed to read next multipart frame", "serial", serial, "err", err)
				}
				return
			}

			t1 := uint64(time.Now().UnixNano() / int64(time.Millisecond))
			imgBytes, err := io.ReadAll(part)
			_ = part.Close()
			if err != nil {
				if sCtx.Err() == nil {
					slog.Warn("stream: failed to read part bytes", "serial", serial, "err", err)
				}
				return
			}

			if len(imgBytes) == 0 {
				continue
			}

			// Prepend the 20-byte magic header:
			// Bytes 0-4: "JPEG"
			// Bytes 4-12: t1 (Ingress timestamp)
			// Bytes 12-20: t2 (Egress timestamp)
			t2 := uint64(time.Now().UnixNano() / int64(time.Millisecond))
			header := make([]byte, 20)
			copy(header[0:4], []byte("JPEG"))
			binary.BigEndian.PutUint64(header[4:12], t1)
			binary.BigEndian.PutUint64(header[12:20], t2)

			framePayload := make([]byte, len(header)+len(imgBytes))
			copy(framePayload[0:20], header)
			copy(framePayload[20:], imgBytes)

			seqNum++
			if seqNum == 1 {
				slog.Info("stream: first iOS JPEG frame parsed and broadcasted", "serial", serial, "size", len(framePayload))
			} else if seqNum%300 == 0 {
				slog.Info("stream: iOS JPEG stream active", "serial", serial, "frame_count", seqNum)
			}

			s.clientsMu.Lock()
			s.gopCache = [][]byte{framePayload} // JPEGs are independent, cache only the latest frame
			for ch := range s.clients {
				select {
				case ch <- framePayload:
				default:
				}
			}
			s.clientsMu.Unlock()
		}
	}()

	m.streams[serial] = s
	slog.Info("stream: iOS capture started (MJPEG via WDA)", "serial", serial, "port", port)
	return nil
}

