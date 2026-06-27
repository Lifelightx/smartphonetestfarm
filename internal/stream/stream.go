package stream

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"protean-provider/internal/config"
)

const (
	scrcpyServerJarOnDevice = "/data/local/tmp/scrcpy-server.jar"
	scrcpyServerVersion     = "4.0"
)

// Manager implements domain.StreamManager.
// It starts the scrcpy-server on-device, reads raw H.264, muxes it into
// fMP4, and broadcasts to clients over chunked HTTP — no ffmpeg required.
type Manager struct {
	cfg     *config.Config
	mu      sync.Mutex
	streams map[string]*streamState
}

func NewManager(cfg *config.Config) *Manager {
	return &Manager{
		cfg:     cfg,
		streams: make(map[string]*streamState),
	}
}

// streamState holds all live state for one device capture session.
type streamState struct {
	serial    string
	port      int
	serverCmd *exec.Cmd      // adb shell running scrcpy-server on-device
	cancel    context.CancelFunc
	done      chan struct{}
	gopCache  [][]byte // Cache starting with a keyframe
	// controlConn is the underlying TCP conn to the scrcpy-server.
	// It is used to write control messages (touch, key events) back to the device.
	controlMu   sync.Mutex
	controlConn net.Conn

	// videoWidth/videoHeight are updated from SPS parsing.
	videoWidth  uint16
	videoHeight uint16

	// clients: each connected HTTP client gets its own buffered channel of fMP4 chunks.
	clientsMu sync.RWMutex
	clients   map[chan<- []byte]struct{}
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
// muxes it to fMP4, and serves it on port via HTTP.
func (m *Manager) StartCapture(ctx context.Context, serial string, port int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.streams[serial]; ok {
		slog.Warn("stream: capture already active", "serial", serial)
		return nil
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
	if err := pushScrcpyServer(sCtx, serial); err != nil {
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
		"CLASSPATH="+scrcpyServerJarOnDevice,
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
	//    Socket dialing happens inside the background goroutine below.
	mux := http.NewServeMux()
	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		m.handleStreamClient(w, r, serial)
	})
	mux.HandleFunc("/control", func(w http.ResponseWriter, r *http.Request) {
		m.handleControl(w, r, serial)
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		m.handleWS(w, r, serial)
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

	// 4. Background goroutine: dial scrcpy sockets → read H.264 → mux fMP4 → broadcast.
	//    Clients connecting to /stream before sockets are ready simply wait for the init segment.
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
		// ADB always accepts at the host TCP layer even before the device-side abstract
		// socket is ready. Use a probe-read to distinguish alive vs dropped connections:
		//   • EOF within 500ms  → ADB dropped us (scrcpy not ready yet), retry
		//   • Read timeout      → connection alive, scrcpy waiting for control, SUCCESS
		//   • Data received     → scrcpy already streaming, preserve byte, SUCCESS
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
		// Probe-read: timeout=alive (SUCCESS), EOF=dropped (retry).
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

		// 4c. Read H.264 frames → mux fMP4 → broadcast to HTTP clients
		m.readAndMux(sCtx, s, h264Conn, maxFPS)
	}()

	m.streams[serial] = s
	slog.Info("stream: capture started", "serial", serial, "port", port)
	return nil
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
// The request body is JSON:
//   {"type":"touch","action":0,"x":0.5,"y":0.3,"pressure":1.0}   // x,y are normalized [0,1]
//   {"type":"key","keycode":4}
//   {"type":"scroll","x":0.5,"y":0.5,"hscroll":0,"vscroll":-1}
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

	var ev struct {
		Type      string  `json:"type"`
		Action    int     `json:"action"`   // 0=down,1=up,2=move
		X         float64 `json:"x"`        // normalized [0,1]
		Y         float64 `json:"y"`        // normalized [0,1]
		Pressure  float64 `json:"pressure"` // [0,1], default 1
		Keycode   int     `json:"keycode"`
		HScroll   float64 `json:"hscroll"`
		VScroll   float64 `json:"vscroll"`
		Text      string  `json:"text"`
		Button    int     `json:"button"`
		Buttons   int     `json:"buttons"`
		PointerID int64   `json:"pointerId"`
	}
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
	if vw == 0 || vh == 0 {
		vw, vh = 1080, 1920 // fallback
	}

	var msg []byte
	switch ev.Type {
	case "touch":
		if ev.Pressure == 0 && ev.Action == 0 {
			ev.Pressure = 1.0
		}
		// Map JS mouse buttons to Android MotionEvent button states
		var actionButton uint32
		var buttons uint32

		if ev.Buttons > 0 { // Click event has buttons pressed
			if ev.Action == 0 || ev.Action == 1 { // ACTION_DOWN or ACTION_UP
				switch ev.Button {
				case 0:
					actionButton = 1 // BUTTON_PRIMARY
				case 1:
					actionButton = 4 // BUTTON_TERTIARY
				case 2:
					actionButton = 2 // BUTTON_SECONDARY
				}
			}
			if ev.Buttons&1 != 0 {
				buttons |= 1 // BUTTON_PRIMARY
			}
			if ev.Buttons&2 != 0 {
				buttons |= 2 // BUTTON_SECONDARY
			}
			if ev.Buttons&4 != 0 {
				buttons |= 4 // BUTTON_TERTIARY
			}
		} else {
			// For touch events, default to primary mouse if action is down/up to be safe
			if ev.Action == 0 || ev.Action == 1 {
				actionButton = 1
				buttons = 1
			}
		}

		pointerID := ev.PointerID
		if ev.Buttons > 0 && pointerID == 0 {
			// If not specified by the frontend but buttons are active, treat as mouse (-1)
			pointerID = -1
		}

		// SC_CONTROL_MSG_TYPE_INJECT_TOUCH_EVENT = 2
		// struct: [1 type][1 action][8 pointer_id][4 x][4 y][2 w][2 h][2 pressure][4 actionButton][4 buttons]
		// Total payload: 1 + 8 + 4 + 4 + 2 + 2 + 2 + 4 + 4 = 31 bytes. Total msg = 32 bytes.
		absX := int32(ev.X * float64(vw))
		absY := int32(ev.Y * float64(vh))
		pressure := uint16(ev.Pressure * 0xFFFF)
		msg = make([]byte, 32)
		msg[0] = 2                                                    // type: INJECT_TOUCH_EVENT
		msg[1] = byte(ev.Action)                                      // action
		binary.BigEndian.PutUint64(msg[2:10], uint64(pointerID))      // pointer_id
		binary.BigEndian.PutUint32(msg[10:14], uint32(absX))          // x
		binary.BigEndian.PutUint32(msg[14:18], uint32(absY))          // y
		binary.BigEndian.PutUint16(msg[18:20], vw)                    // screen width
		binary.BigEndian.PutUint16(msg[20:22], vh)                    // screen height
		binary.BigEndian.PutUint16(msg[22:24], pressure)              // pressure
		binary.BigEndian.PutUint32(msg[24:28], actionButton)          // actionButton
		binary.BigEndian.PutUint32(msg[28:32], buttons)               // buttons
	case "scroll":
		// SC_CONTROL_MSG_TYPE_INJECT_SCROLL_EVENT = 3
		// struct: [1 type][4 x][4 y][2 w][2 h][2 hscroll][2 vscroll][4 buttons]
		// Total payload: 4 + 4 + 2 + 2 + 2 + 2 + 4 = 20 bytes. Total msg = 21 bytes.
		absX := int32(ev.X * float64(vw))
		absY := int32(ev.Y * float64(vh))
		
		hVal := ev.HScroll
		if hVal > 1 { hVal = 1 } else if hVal < -1 { hVal = -1 }
		vVal := ev.VScroll
		if vVal > 1 { vVal = 1 } else if vVal < -1 { vVal = -1 }

		var hscroll int16
		if hVal == 1.0 {
			hscroll = 0x7fff
		} else {
			hscroll = int16(hVal * 32768.0)
		}

		var vscroll int16
		if vVal == 1.0 {
			vscroll = 0x7fff
		} else {
			vscroll = int16(vVal * 32768.0)
		}

		msg = make([]byte, 21)
		msg[0] = 3
		binary.BigEndian.PutUint32(msg[1:5], uint32(absX))
		binary.BigEndian.PutUint32(msg[5:9], uint32(absY))
		binary.BigEndian.PutUint16(msg[9:11], vw)
		binary.BigEndian.PutUint16(msg[11:13], vh)
		binary.BigEndian.PutUint16(msg[13:15], uint16(hscroll))
		binary.BigEndian.PutUint16(msg[15:17], uint16(vscroll))
		binary.BigEndian.PutUint32(msg[17:21], 0)                     // buttons = 0
	case "text":
		// SC_CONTROL_MSG_TYPE_INJECT_TEXT = 1
		// struct: [1 type][4 length][length string]
		textBytes := []byte(ev.Text)
		msg = make([]byte, 1+4+len(textBytes))
		msg[0] = 1
		binary.BigEndian.PutUint32(msg[1:5], uint32(len(textBytes)))
		copy(msg[5:], textBytes)
	case "key":
		// SC_CONTROL_MSG_TYPE_INJECT_KEYCODE = 0
		// struct: [1 type][1 action][4 keycode][4 repeat][4 metastate]
		msg = make([]byte, 14)
		msg[0] = 0
		msg[1] = 0 // action: down
		binary.BigEndian.PutUint32(msg[2:6], uint32(ev.Keycode))
		binary.BigEndian.PutUint32(msg[6:10], 0) // repeat
		binary.BigEndian.PutUint32(msg[10:14], 0) // metastate
		s.controlMu.Lock()
		_, _ = conn.Write(msg)
		s.controlMu.Unlock()
		// Also send key-up
		msg[1] = 1
		s.controlMu.Lock()
		_, _ = conn.Write(msg)
		s.controlMu.Unlock()
		w.WriteHeader(http.StatusNoContent)
		return
	default:
		http.Error(w, "unknown event type", http.StatusBadRequest)
		return
	}

	s.controlMu.Lock()
	_, err := conn.Write(msg)
	s.controlMu.Unlock()
	if err != nil {
		slog.Warn("stream: control write failed", "serial", serial, "err", err)
		http.Error(w, "write failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
		// pts := int64(binary.BigEndian.Uint64(hdr[0:8])) // available if needed
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

func adbForward(ctx context.Context, serial string, local int, remote string) error {
	out, err := exec.CommandContext(ctx, "adb", "-s", serial,
		"forward", fmt.Sprintf("tcp:%d", local), remote,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb forward tcp:%d→%s: %w (out: %s)", local, remote, err, out)
	}
	return nil
}

func adbForwardRemove(ctx context.Context, serial string, local int) error {
	_, err := exec.CommandContext(ctx, "adb", "-s", serial,
		"forward", "--remove", fmt.Sprintf("tcp:%d", local),
	).CombinedOutput()
	return err
}

func dialWithRetry(ctx context.Context, port int, timeout time.Duration) (io.Reader, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			// Set a short read deadline to detect immediate ADB forward connection drops.
			_ = conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
			oneByte := make([]byte, 1)
			_, err = conn.Read(oneByte)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					// Timeout means the connection was kept open by ADB, so device-side is ready!
					_ = conn.SetReadDeadline(time.Time{})
					slog.Info("stream: connected to scrcpy-server (video)", "addr", addr)
					return conn, nil
				}
				// Any other error (like EOF) means device-side is not ready yet.
				conn.Close()
			} else {
				// Read 1 byte successfully. Return MultiReader to preserve the read byte.
				_ = conn.SetReadDeadline(time.Time{})
				slog.Info("stream: connected to scrcpy-server (video, data present)", "addr", addr)
				return io.MultiReader(bytes.NewReader(oneByte), conn), nil
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	return nil, fmt.Errorf("timeout after %s waiting for scrcpy-server on %s", timeout, addr)
}

func scrcpyServerJar() (string, error) {
	// 1. Next to the binary: <exe-dir>/scrcpy-server.jar
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "scrcpy-server.jar")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	// 2. Source-tree location: internal/stream/scrcpy-server.jar
	p := filepath.Join("internal", "stream", "scrcpy-server.jar")
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}

	return "", fmt.Errorf("scrcpy-server.jar not found next to binary or in internal/stream/")
}

func pushScrcpyServer(ctx context.Context, serial string) error {
	jarPath, err := scrcpyServerJar()
	if err != nil {
		return fmt.Errorf("pushScrcpyServer: %w", err)
	}

	slog.Info("stream: pushing scrcpy-server to device", "serial", serial, "src", jarPath)

	out, err := exec.CommandContext(ctx, "adb", "-s", serial, "push", jarPath, scrcpyServerJarOnDevice).CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb push: %w (out: %s)", err, out)
	}

	// Ensure the file is readable by the app on-device.
	_ = exec.CommandContext(ctx, "adb", "-s", serial, "shell", "chmod", "644", scrcpyServerJarOnDevice).Run()
	return nil
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for enterprise-grade provider portal API
	},
}

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
			_, werr := s.controlConn.Write(msg)
			s.controlMu.Unlock()
			if werr != nil {
				slog.Warn("stream: control write failed", "serial", serial, "err", werr)
			}
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

		var ev struct {
			Type      string  `json:"type"`
			Action    int     `json:"action"`
			X         float64 `json:"x"`
			Y         float64 `json:"y"`
			Pressure  float64 `json:"pressure"`
			Keycode   int     `json:"keycode"`
			HScroll   float64 `json:"hscroll"`
			VScroll   float64 `json:"vscroll"`
			Text      string  `json:"text"`
			Button    int     `json:"button"`
			Buttons   int     `json:"buttons"`
			PointerID int64   `json:"pointerId"`
		}
		if err := json.Unmarshal(payload, &ev); err != nil {
			slog.Warn("stream: ws bad json payload", "serial", serial, "err", err)
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
		if vw == 0 || vh == 0 {
			vw, vh = 1080, 1920
		}

		var msg []byte
		switch ev.Type {
		case "touch":
			if ev.Pressure == 0 && ev.Action == 0 {
				ev.Pressure = 1.0
			}
			var actionButton uint32
			var buttons uint32
			if ev.Buttons > 0 {
				if ev.Action == 0 || ev.Action == 1 {
					switch ev.Button {
					case 0: actionButton = 1
					case 1: actionButton = 4
					case 2: actionButton = 2
					}
				}
				if ev.Buttons&1 != 0 { buttons |= 1 }
				if ev.Buttons&2 != 0 { buttons |= 2 }
				if ev.Buttons&4 != 0 { buttons |= 4 }
			} else {
				if ev.Action == 0 || ev.Action == 1 {
					actionButton = 1
					buttons = 1
				}
			}

			pointerID := ev.PointerID
			if ev.Buttons > 0 && pointerID == 0 {
				pointerID = -1
			}

			absX := int32(ev.X * float64(vw))
			absY := int32(ev.Y * float64(vh))
			pressure := uint16(ev.Pressure * 0xFFFF)
			msg = make([]byte, 32)
			msg[0] = 2
			msg[1] = byte(ev.Action)
			binary.BigEndian.PutUint64(msg[2:10], uint64(pointerID))
			binary.BigEndian.PutUint32(msg[10:14], uint32(absX))
			binary.BigEndian.PutUint32(msg[14:18], uint32(absY))
			binary.BigEndian.PutUint16(msg[18:20], vw)
			binary.BigEndian.PutUint16(msg[20:22], vh)
			binary.BigEndian.PutUint16(msg[22:24], pressure)
			binary.BigEndian.PutUint32(msg[24:28], actionButton)
			binary.BigEndian.PutUint32(msg[28:32], buttons)

		case "scroll":
			absX := int32(ev.X * float64(vw))
			absY := int32(ev.Y * float64(vh))
			hVal := ev.HScroll
			if hVal > 1 { hVal = 1 } else if hVal < -1 { hVal = -1 }
			vVal := ev.VScroll
			if vVal > 1 { vVal = 1 } else if vVal < -1 { vVal = -1 }

			var hscroll int16
			if hVal == 1.0 { hscroll = 0x7fff } else { hscroll = int16(hVal * 32768.0) }
			var vscroll int16
			if vVal == 1.0 { vscroll = 0x7fff } else { vscroll = int16(vVal * 32768.0) }

			msg = make([]byte, 21)
			msg[0] = 3
			binary.BigEndian.PutUint32(msg[1:5], uint32(absX))
			binary.BigEndian.PutUint32(msg[5:9], uint32(absY))
			binary.BigEndian.PutUint16(msg[9:11], vw)
			binary.BigEndian.PutUint16(msg[11:13], vh)
			binary.BigEndian.PutUint16(msg[13:15], uint16(hscroll))
			binary.BigEndian.PutUint16(msg[15:17], uint16(vscroll))
			binary.BigEndian.PutUint32(msg[17:21], 0)

		case "text":
			textBytes := []byte(ev.Text)
			msg = make([]byte, 1+4+len(textBytes))
			msg[0] = 1
			binary.BigEndian.PutUint32(msg[1:5], uint32(len(textBytes)))
			copy(msg[5:], textBytes)

		case "key":
			msg = make([]byte, 14)
			msg[0] = 0
			msg[1] = 0
			binary.BigEndian.PutUint32(msg[2:6], uint32(ev.Keycode))
			binary.BigEndian.PutUint32(msg[6:10], 0)
			binary.BigEndian.PutUint32(msg[10:14], 0)

			s.controlMu.Lock()
			_, _ = conn.Write(msg)
			s.controlMu.Unlock()

			msg[1] = 1 // key-up
		}

		if len(msg) > 0 {
			// Non-blocking send to control goroutine — never stalls ReadMessage
			select {
			case controlCh <- msg:
			default:
				slog.Warn("stream: control channel full, dropping event", "serial", serial)
			}
		}
	}
}


