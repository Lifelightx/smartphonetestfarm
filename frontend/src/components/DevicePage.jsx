import React, { useState, useEffect, useRef } from 'react';
function DevicePage({ device, onBack, onRelease }) {
  const canvasRef = useRef(null);
  const wsRef = useRef(null);
  const [errorMsg, setErrorMsg] = useState('');
  const [isPlaying, setIsPlaying] = useState(false);
  const [activeTab, setActiveTab] = useState('dashboard');
  const [videoWidth, setVideoWidth] = useState(0);
  const [videoHeight, setVideoHeight] = useState(0);

  // Parse initial aspect ratio from device display info (e.g. "1080x2400 @ 450dpi")
  const getInitialAspectRatio = () => {
    if (!device.display) return { ratio: '9 / 19.5', isLandscape: false };
    const match = device.display.match(/^(\d+)x(\d+)/);
    if (match) {
      const w = parseInt(match[1], 10);
      const h = parseInt(match[2], 10);
      if (w && h) {
        return { ratio: `${w} / ${h}`, isLandscape: w > h };
      }
    }
    return { ratio: '9 / 19.5', isLandscape: false };
  };

  const initial = getInitialAspectRatio();
  const [rotation, setRotation] = useState(initial.isLandscape ? 90 : 0);

  const streamPort = device.stream_port || device.streamPort;
  const wsUrl = device.provider_id && streamPort
    ? `ws://${device.provider_id}:${streamPort}/ws`
    : null;

  // ── Input event handlers ────────────────────────────────────────────────────

  const sendTouchEvent = (action, normX, normY, button = 0, buttons = 1, pressure = 1.0, pointerId = 0) => {
    if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({ type: 'touch', action, x: normX, y: normY, button, buttons, pressure, pointerId }));
    }
  };

  const sendScrollEvent = (normX, normY, vscroll) => {
    if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({ type: 'scroll', x: normX, y: normY, hscroll: 0, vscroll }));
    }
  };
  const sendControlKey = (keycode) => {
    if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({ type: 'key', keycode }));
    }
  };


  const getVideoNormCoords = (e) => {
    if (!canvasRef.current) return { x: 0.5, y: 0.5 };
    const rect = canvasRef.current.getBoundingClientRect();
    const clientX = e.clientX ?? e.touches?.[0]?.clientX;
    const clientY = e.clientY ?? e.touches?.[0]?.clientY;
    return {
      x: Math.max(0, Math.min(1, (clientX - rect.left) / rect.width)),
      y: Math.max(0, Math.min(1, (clientY - rect.top) / rect.height)),
    };
  };

  const isDragging = useRef(false);

  const handleMouseDown = (e) => {
    e.preventDefault();
    isDragging.current = true;
    const { x, y } = getVideoNormCoords(e);
    sendTouchEvent(0, x, y, e.button, e.buttons, 1.0, -1);
  };

  const handleMouseMove = (e) => {
    if (!isDragging.current) return;
    e.preventDefault();
    const { x, y } = getVideoNormCoords(e);
    sendTouchEvent(2, x, y, e.button, e.buttons, 1.0, -1);
  };

  const handleMouseUp = (e) => {
    if (!isDragging.current) return;
    isDragging.current = false;
    const { x, y } = getVideoNormCoords(e);
    sendTouchEvent(1, x, y, e.button, e.buttons, 0, -1);
  };

  const handleContextMenu = (e) => e.preventDefault();

  const handleKeyDown = (e) => {
    if (e.ctrlKey || e.altKey || e.metaKey) return;
    e.preventDefault();

    const controlKeyMap = {
      'Backspace': 67, 'Enter': 66, 'Tab': 61, 'Escape': 111,
      'ArrowLeft': 21, 'ArrowRight': 22, 'ArrowUp': 19, 'ArrowDown': 20,
      'Delete': 112,
    };

    const key = e.key;
    if (controlKeyMap[key] !== undefined) {
      sendControlKey(controlKeyMap[key]);
    } else if (key.length === 1 && wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({ type: 'text', text: key }));
    }
  };

  const handleTouchStart = (e) => {
    e.preventDefault();
    if (!canvasRef.current) return;
    const rect = canvasRef.current.getBoundingClientRect();
    for (let i = 0; i < e.changedTouches.length; i++) {
      const t = e.changedTouches[i];
      sendTouchEvent(0,
        Math.max(0, Math.min(1, (t.clientX - rect.left) / rect.width)),
        Math.max(0, Math.min(1, (t.clientY - rect.top) / rect.height)),
        0, 0, 1.0, t.identifier
      );
    }
  };

  const handleTouchMove = (e) => {
    e.preventDefault();
    if (!canvasRef.current) return;
    const rect = canvasRef.current.getBoundingClientRect();
    for (let i = 0; i < e.changedTouches.length; i++) {
      const t = e.changedTouches[i];
      sendTouchEvent(2,
        Math.max(0, Math.min(1, (t.clientX - rect.left) / rect.width)),
        Math.max(0, Math.min(1, (t.clientY - rect.top) / rect.height)),
        0, 0, 1.0, t.identifier
      );
    }
  };

  const handleTouchEnd = (e) => {
    e.preventDefault();
    if (!canvasRef.current) return;
    const rect = canvasRef.current.getBoundingClientRect();
    for (let i = 0; i < e.changedTouches.length; i++) {
      const t = e.changedTouches[i];
      sendTouchEvent(1,
        Math.max(0, Math.min(1, (t.clientX - rect.left) / rect.width)),
        Math.max(0, Math.min(1, (t.clientY - rect.top) / rect.height)),
        0, 0, 0, t.identifier
      );
    }
  };

  const handleWheel = (e) => {
    e.preventDefault();
    const { x, y } = getVideoNormCoords(e);
    sendScrollEvent(x, y, e.deltaY > 0 ? -1 : 1);
  };
  // ── WebCodecs streaming pipeline ────────────────────────────────────────────
  const videoWidthRef = useRef(0);
  const videoHeightRef = useRef(0);

  useEffect(() => {
    if (!wsUrl) return;
    let active = true;
    let decoder = null;
    let ws = null;

    if (!window.VideoDecoder) {
      setErrorMsg('WebCodecs is not supported in this browser. Ensure you are using a secure context (HTTPS) or accessing via localhost/127.0.0.1.');
      return;
    }
    let savedSPS = null;
    let savedPPS = null;
    let currentCodec = 'avc1.64002a';

    const processAnnexB = (chunk) => {
      const nals = [];
      let i = 0;
      const len = chunk.length;
      
      while (i < len) {
        if (i + 3 < len && chunk[i] === 0 && chunk[i+1] === 0 && chunk[i+2] === 0 && chunk[i+3] === 1) {
          nals.push({ start: i, header: i + 4 });
          i += 4;
        } else if (i + 2 < len && chunk[i] === 0 && chunk[i+1] === 0 && chunk[i+2] === 1) {
          nals.push({ start: i, header: i + 3 });
          i += 3;
        } else {
          i++;
        }
      }
      
      for (let k = 0; k < nals.length; k++) {
        nals[k].end = (k + 1 < nals.length) ? nals[k+1].start : len;
      }

      let hasIDR = false;
      
      for (const nal of nals) {
        if (nal.header >= len) continue;
        const nalType = chunk[nal.header] & 0x1f;
        if (nalType === 7) {
          savedSPS = chunk.slice(nal.start, nal.end);
        } else if (nalType === 8) {
          savedPPS = chunk.slice(nal.start, nal.end);
        } else if (nalType === 5) {
          hasIDR = true;
        }
      }

      const isConfigOnly = nals.length > 0 && nals.every(nal => {
        if (nal.header >= len) return true;
        const t = chunk[nal.header] & 0x1f;
        return t === 7 || t === 8 || t === 6 || t === 9; // SPS, PPS, SEI, AUD
      });

      return { hasIDR, isConfigOnly, nals };
    };

    const initDecoder = () => {
      try {
        decoder = new VideoDecoder({
          output: (frame) => {
            if (!active) {
              frame.close();
              return;
            }
            const w = frame.displayWidth;
            const h = frame.displayHeight;
            if (w && h && (w !== videoWidthRef.current || h !== videoHeightRef.current)) {
              videoWidthRef.current = w;
              videoHeightRef.current = h;
              setVideoWidth(w);
              setVideoHeight(h);
              setRotation(w > h ? 90 : 0);
            }

            const canvas = canvasRef.current;
            if (canvas) {
              if (canvas.width !== w || canvas.height !== h) {
                canvas.width = w;
                canvas.height = h;
              }
              const ctx = canvas.getContext('2d');
              ctx.drawImage(frame, 0, 0, w, h);
            }
            setIsPlaying(true);
            frame.close();
          },
          error: (e) => {
            console.error('VideoDecoder error:', e);
            if (active) {
              setErrorMsg(`Decoder error: ${e.message}`);
            }
          }
        });

        decoder.configure({
          codec: currentCodec,
          optimizeForLatency: true
        });
      } catch (err) {
        console.error('Failed to initialize VideoDecoder:', err);
        setErrorMsg(`Decoder initialization failed: ${err.message}`);
      }
    };

    const startWebSocket = () => {
      console.log(`Connecting to WebSocket stream at: ${wsUrl}`);
      ws = new WebSocket(wsUrl);
      wsRef.current = ws;
      ws.binaryType = 'arraybuffer';

      initDecoder();

      ws.onopen = () => {
        if (!active) {
          ws.close();
          return;
        }
        console.log(`WebSocket connected to ${wsUrl}`);
      };

      ws.onclose = () => {
        console.log('WebSocket closed');
        if (active) {
          setIsPlaying(false);
        }
      };

      ws.onerror = (err) => {
        console.error('WebSocket error:', err);
        if (active) {
          setErrorMsg('Stream connection lost or failed to connect');
        }
      };

      ws.onmessage = (event) => {
        if (!active || typeof event.data === 'string') return;
        const chunk = new Uint8Array(event.data);

        if (!decoder || decoder.state === 'closed') {
          return;
        }

        try {
          const { hasIDR, isConfigOnly, nals } = processAnnexB(chunk);
          
          if (savedSPS) {
            const skip = (savedSPS[2] === 1) ? 3 : 4;
            const profileIdc = savedSPS[skip + 1];
            const constraints = savedSPS[skip + 2];
            const levelIdc = savedSPS[skip + 3];
            const codecStr = `avc1.${profileIdc.toString(16).padStart(2, '0')}${constraints.toString(16).padStart(2, '0')}${levelIdc.toString(16).padStart(2, '0')}`;
            if (codecStr !== currentCodec) {
              console.log(`Configuring VideoDecoder with codec: ${codecStr}`);
              decoder.configure({
                codec: codecStr,
                optimizeForLatency: true
              });
              currentCodec = codecStr;
            }
          }

          if (isConfigOnly) {
            return;
          }

          let dataToDecode = chunk;
          if (hasIDR) {
            const alreadyHasSPS = nals.some(nal => {
              if (nal.header >= chunk.length) return false;
              return (chunk[nal.header] & 0x1f) === 7;
            });
            if (!alreadyHasSPS && savedSPS && savedPPS) {
              const combined = new Uint8Array(savedSPS.length + savedPPS.length + chunk.length);
              combined.set(savedSPS, 0);
              combined.set(savedPPS, savedSPS.length);
              combined.set(chunk, savedSPS.length + savedPPS.length);
              dataToDecode = combined;
            }
          }

          const timestamp = Math.floor(performance.now() * 1000);
          const encodedChunk = new EncodedVideoChunk({
            type: hasIDR ? 'key' : 'delta',
            timestamp: timestamp,
            data: dataToDecode
          });
          decoder.decode(encodedChunk);
        } catch (err) {
          console.error('Decode failed:', err);
        }
      };
    };

    startWebSocket();

    return () => {
      active = false;
      if (ws) {
        ws.close();
      }
      if (wsRef.current) {
        wsRef.current.close();
        wsRef.current = null;
      }
      if (decoder && decoder.state !== 'closed') {
        try {
          decoder.close();
        } catch (e) {
          console.warn('Error closing decoder:', e);
        }
      }
    };
  }, [wsUrl]);



  // ── Layout & aspect ratio ───────────────────────────────────────────────────

  const isLandscape = rotation === 90 || rotation === 270;

  let currentAspectRatio = initial.ratio;
  if (videoWidth && videoHeight) {
    currentAspectRatio = `${videoWidth} / ${videoHeight}`;
  } else if (isLandscape) {
    const parts = initial.ratio.split('/');
    if (parts.length === 2) currentAspectRatio = `${parts[1].trim()} / ${parts[0].trim()}`;
  }

  const screenStyle = isLandscape
    ? { width: '100%', maxWidth: '820px', aspectRatio: currentAspectRatio }
    : { height: '100%', maxHeight: 'min(680px, 78vh)', aspectRatio: currentAspectRatio };

  const handleResize = (e) => {
    const w = e.target.videoWidth;
    const h = e.target.videoHeight;
    if (w && h) { setVideoWidth(w); setVideoHeight(h); setRotation(w > h ? 90 : 0); }
  };

  if (device.model === 'Loading...') {
    return (
      <div className="device-page" style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', height: '80vh', gap: '1.5rem' }}>
        <span className="spinner"></span>
        <div style={{ color: 'var(--color-fg-muted)', fontSize: '14px' }}>Connecting to device session...</div>
        <button className="btn btn-ghost btn-sm" onClick={onBack}>Back to Dashboard</button>
      </div>
    );
  }

  return (
    <div className="device-page">

      {/* ── LEFT: Screen area with side controls ──────────────────────────── */}
      <div className="screen-column">
        <div className="phone-stage">
          {/* Vertical controls on the LEFT of the phone */}
          <div className="side-controls side-controls-left">
            <button className="side-btn" title="Rotate" onClick={() => setRotation(r => r === 0 ? 90 : 0)}>
              <span>⟳</span>
              <label>Rotate</label>
            </button>
            <button className="side-btn" title="Power / Wake" onClick={() => sendControlKey(224)}>
              <span>⏻</span>
              <label>Wake</label>
            </button>
          </div>

          {/* Phone mockup */}
          <div className="phone-container">
            <div className={`phone-wrapper ${isLandscape ? 'landscape' : ''}`}>
              <div className="phone-screen" style={screenStyle}>
                {/* Error overlay */}
                {errorMsg && (
                  <div className="phone-placeholder" style={{ color: 'var(--red)' }}>
                    <div style={{ fontSize: '28px' }}>⚠️</div>
                    <div style={{ marginTop: '10px', fontSize: '13px', textAlign: 'center', padding: '0 16px' }}>{errorMsg}</div>
                  </div>
                )}
                {/* Loading overlay — sits on top of the video (position:absolute z-index:10)
                    while no frame has been painted yet. NEVER use display:none on the video
                    itself because toggling visibility breaks the MediaSource pipeline. */}
                {!errorMsg && !isPlaying && (
                  <div className="phone-placeholder">
                    <span className="spinner"></span>
                    <div style={{ marginTop: '14px', fontSize: '13px' }}>Connecting to live stream...</div>
                  </div>
                )}
                <canvas
                  ref={canvasRef}
                  tabIndex={0}
                  className="phone-video"
                  onMouseDown={handleMouseDown}
                  onMouseMove={handleMouseMove}
                  onMouseUp={handleMouseUp}
                  onMouseLeave={handleMouseUp}
                  onTouchStart={handleTouchStart}
                  onTouchMove={handleTouchMove}
                  onTouchEnd={handleTouchEnd}
                  onWheel={handleWheel}
                  onKeyDown={handleKeyDown}
                  onContextMenu={handleContextMenu}
                  style={{
                    width: '100%',
                    height: '100%',
                    cursor: 'crosshair',
                    touchAction: 'none',
                    userSelect: 'none',
                    outline: 'none',
                  }}
                />

              </div>
            </div>
          </div>

          {/* Vertical controls on the RIGHT of the phone (Android nav) */}
          <div className="side-controls side-controls-right">
            <button className="side-btn" title="Home" onClick={() => sendControlKey(3)}>
              <span>◯</span>
              <label>Home</label>
            </button>
            <button className="side-btn" title="Back" onClick={() => sendControlKey(4)}>
              <span>◁</span>
              <label>Back</label>
            </button>
            <button className="side-btn" title="Recents" onClick={() => sendControlKey(187)}>
              <span>▣</span>
              <label>Recent</label>
            </button>
          </div>
        </div>
      </div>

      {/* ── RIGHT: Control & Overview Tabs ──────────────────────────── */}
      <div className="details-column">
        <div className="back-header">
          <button className="btn btn-ghost" onClick={onBack}>
            ← Dashboard
          </button>
          <span className="status-pill status-claimed" style={{ marginLeft: '0' }}>
            {device.model}
          </span>
        </div>

        <div className="tabs-header" style={{ marginBottom: 0 }}>
          <button className={`tab-btn ${activeTab === 'dashboard' ? 'active' : ''}`} onClick={() => setActiveTab('dashboard')}>
             Dashboard
          </button>
          <button className={`tab-btn ${activeTab === 'automation' ? 'active' : ''}`} onClick={() => setActiveTab('automation')}>
             Automation
          </button>
          <button className={`tab-btn ${activeTab === 'info' ? 'active' : ''}`} onClick={() => setActiveTab('info')}>
             Info
          </button>
        </div>

        <div className="tab-content" style={{ flex: 1, overflowY: 'auto', paddingTop: '16px', paddingBottom: '32px' }}>
          {activeTab === 'dashboard' && (
            <div className="dashboard-grid">
              
              {/* App Upload */}
              <div className="dashboard-card">
                <div className="card-header">
                  <span className="card-title" style={{ color: 'var(--text)' }}>🔖 App Upload</span>
                  <button className="btn btn-sm btn-danger">Clear</button>
                </div>
                <div className="card-body">
                  <div className="dropzone">
                    <span style={{ fontSize: '24px', display: 'block', marginBottom: '8px' }}>↑</span>
                    Drop file to upload
                  </div>
                </div>
              </div>

              {/* File Upload */}
              <div className="dashboard-card">
                <div className="card-header">
                  <span className="card-title" style={{ color: 'var(--green)' }}>🔖 File Upload</span>
                  <button className="btn btn-sm btn-danger">Clear</button>
                </div>
                <div className="card-body">
                  <div className="dropzone">
                    <span style={{ fontSize: '24px', display: 'block', marginBottom: '8px' }}>↑</span>
                    Drop file to upload
                  </div>
                </div>
              </div>

              {/* Maintenance */}
              <div className="dashboard-card">
                <div className="card-header">
                  <span className="card-title">⚙️ Maintenance</span>
                </div>
                <div className="card-body">
                  <button className="btn btn-danger" style={{ width: '100%' }}>Restart Device</button>
                </div>
              </div>

              {/* Navigation */}
              <div className="dashboard-card">
                <div className="card-header">
                  <span className="card-title" style={{ color: 'var(--accent)' }}>🧭 Navigation</span>
                  <button className="btn btn-sm btn-danger">Reset</button>
                </div>
                <div className="card-body">
                  <div className="nav-input-row">
                    <input type="text" className="nav-input" placeholder="http://..." />
                    <button className="btn btn-primary">Open</button>
                  </div>
                  <div className="browser-icons">
                     <button title="Chrome">🌐</button>
                     <button title="Firefox">🦊</button>
                  </div>
                </div>
              </div>

              {/* Shell */}
              <div className="dashboard-card">
                <div className="card-header">
                  <span className="card-title">🖥️ Shell</span>
                  <button className="btn btn-sm btn-danger">Clear</button>
                </div>
                <div className="card-body">
                  <div className="nav-input-row">
                    <input type="text" className="nav-input" placeholder="ls -la" />
                    <button className="btn btn-primary">▶</button>
                  </div>
                </div>
              </div>

              {/* Apps Shortcuts */}
              <div className="dashboard-card">
                <div className="card-header">
                  <span className="card-title" style={{ color: 'var(--accent)' }}>📱 Apps</span>
                </div>
                <div className="card-body">
                  <div className="app-grid">
                    <button className="app-icon-btn">
                      <span style={{ fontSize: '20px' }}>⚙️</span>
                      Settings
                    </button>
                    <button className="app-icon-btn">
                      <span style={{ fontSize: '20px' }}>🛒</span>
                      App Store
                    </button>
                    <button className="app-icon-btn">
                      <span style={{ fontSize: '20px' }}>🌐</span>
                      Language
                    </button>
                    <button className="app-icon-btn">
                      <span style={{ fontSize: '20px' }}>📶</span>
                      Wifi
                    </button>
                    <button className="app-icon-btn">
                      <span style={{ fontSize: '20px' }}>📦</span>
                      Manage Apps
                    </button>
                    <button className="app-icon-btn">
                      <span style={{ fontSize: '20px' }}>👨‍💻</span>
                      Developer
                    </button>
                  </div>
                </div>
              </div>

              {/* Advanced Input */}
              <div className="dashboard-card">
                <div className="card-header">
                  <span className="card-title" style={{ color: 'var(--text)' }}>🎛️ Advanced Input</span>
                </div>
                <div className="card-body">
                  <span style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '-4px' }}>Volume Control</span>
                  <div className="volume-row" style={{ display: 'flex', gap: '8px' }}>
                    <button className="btn btn-ghost" style={{ flex: 1 }}>Mute</button>
                    <button className="btn btn-ghost" style={{ flex: 1 }}>Vol -</button>
                    <button className="btn btn-ghost" style={{ flex: 1 }}>Vol +</button>
                  </div>
                </div>
              </div>
              
              {/* Upload File to Server */}
              <div className="dashboard-card">
                <div className="card-header">
                  <span className="card-title" style={{ color: 'var(--text-link)' }}>⬆️ Upload File To Server</span>
                </div>
                <div className="card-body">
                  <div className="dropzone">
                    <span style={{ fontSize: '24px', display: 'block', marginBottom: '8px' }}>↑</span>
                    Drop file to upload
                  </div>
                  <button className="btn btn-ghost" style={{ marginTop: '8px', width: '100%' }}>Show History</button>
                </div>
              </div>

            </div>
          )}

          {activeTab === 'automation' && (
            <div className="feature-placeholder" style={{ marginTop: '32px' }}>
              <div className="feature-placeholder-icon">🤖</div>
              <h4>Automation Studio</h4>
              <p style={{ fontSize: '12px', marginTop: '4px' }}>
                Create and run automated test scripts on this device.
              </p>
            </div>
          )}

          {activeTab === 'info' && (
            <div className="details-card">
              <h3>📱 {device.manufacturer} {device.model}</h3>
              <div className="meta-grid">
                <div className="meta-item">
                  <span className="meta-label">Serial</span>
                  <span className="meta-val" style={{ fontFamily: 'monospace' }}>{device.serial}</span>
                </div>
                <div className="meta-item">
                  <span className="meta-label">OS Version</span>
                  <span className="meta-val">Android {device.android} (SDK {device.sdk})</span>
                </div>
                <div className="meta-item">
                  <span className="meta-label">CPU ABI</span>
                  <span className="meta-val">{device.abi || 'Unknown'}</span>
                </div>
                <div className="meta-item">
                  <span className="meta-label">Resolution</span>
                  <span className="meta-val">{device.display || 'Unknown'}</span>
                </div>
                <div className="meta-item">
                  <span className="meta-label">Battery</span>
                  <span className="meta-val">{device.battery}%</span>
                </div>
                <div className="meta-item">
                  <span className="meta-label">WiFi</span>
                  <span className="meta-val">{device.wifi_ssid || 'Not connected'}</span>
                </div>
                {device.ip && (
                  <div className="meta-item">
                    <span className="meta-label">Device IP</span>
                    <span className="meta-val">{device.ip}</span>
                  </div>
                )}
                <div className="meta-item">
                  <span className="meta-label">Stream Port</span>
                  <span className="meta-val">{streamPort}</span>
                </div>
              </div>
              <div style={{ marginTop: '24px', borderTop: '1px solid var(--border)', paddingTop: '16px' }}>
                <button className="btn btn-danger" onClick={onRelease}>Release Device</button>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

export default DevicePage;
