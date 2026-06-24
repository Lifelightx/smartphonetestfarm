import React, { useState, useEffect, useRef } from 'react';

// Replace with your coordinator's API address (usually GRPCPort + 2, e.g. 9002)
const COORDINATOR_API = 'http://localhost:9002';

function App() {
  const [devices, setDevices] = useState([]);
  const [loading, setLoading] = useState(false);
  const [activeDevice, setActiveDevice] = useState(null);
  const [toasts, setToasts] = useState([]);

  // Fetch device list
  const fetchDevices = async () => {
    setLoading(true);
    try {
      const res = await fetch(`${COORDINATOR_API}/api/v1/devices`);
      if (!res.ok) throw new Error(`HTTP error ${res.status}`);
      const data = await res.json();
      setDevices(data || []);
    } catch (err) {
      showToast(`Failed to fetch devices: ${err.message}`, 'error');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchDevices();
    const interval = setInterval(fetchDevices, 5000);
    return () => clearInterval(interval);
  }, []);

  const showToast = (message, type = 'success') => {
    const id = Date.now();
    setToasts((prev) => [...prev, { id, message, type }]);
    setTimeout(() => {
      setToasts((prev) => prev.filter((t) => t.id !== id));
    }, 4000);
  };

  const handleClaim = async (device) => {
    try {
      showToast(`Claiming ${device.model}...`, 'success');
      const res = await fetch(`${COORDINATOR_API}/api/v1/devices/${device.serial}/claim?user=dev-user`, {
        method: 'POST',
      });
      if (!res.ok) {
        const txt = await res.text();
        throw new Error(txt || `HTTP error ${res.status}`);
      }
      const data = await res.json();
      if (data.success) {
        showToast('Device claimed successfully!', 'success');
        // Let's set active device to show the streaming dialog
        setActiveDevice({
          ...device,
          streamPort: data.port,
          sessionID: data.session_id,
        });
        fetchDevices();
      }
    } catch (err) {
      showToast(`Claim failed: ${err.message}`, 'error');
    }
  };

  const handleRelease = async (serial) => {
    try {
      showToast(`Releasing device...`, 'success');
      const res = await fetch(`${COORDINATOR_API}/api/v1/devices/${serial}/release`, {
        method: 'POST',
      });
      if (!res.ok) {
        const txt = await res.text();
        throw new Error(txt || `HTTP error ${res.status}`);
      }
      const data = await res.json();
      if (data.success) {
        showToast('Device released!', 'success');
        if (activeDevice && activeDevice.serial === serial) {
          setActiveDevice(null);
        }
        fetchDevices();
      }
    } catch (err) {
      showToast(`Release failed: ${err.message}`, 'error');
    }
  };

  // Helper to determine status color
  const getStatusClass = (status) => {
    switch (status?.toLowerCase()) {
      case 'idle': return 'status-idle';
      case 'claimed': return 'status-claimed';
      case 'busy': return 'status-busy';
      default: return 'status-offline';
    }
  };

  return (
    <div className="layout">
      <header className="header">
        <div className="header-logo">P</div>
        <h1 className="header-title">Protean STF Provider Portal</h1>
        <div className="header-subtitle">Live Device Farm Dashboard</div>
      </header>

      <main className="main">
        {activeDevice ? (
          <DevicePage
            device={activeDevice}
            onBack={() => setActiveDevice(null)}
            onRelease={() => handleRelease(activeDevice.serial)}
          />
        ) : (
          <>
            <div className="stats-bar">
              <div className="stat-card">
                <div className="stat-value">{devices.length}</div>
                <div className="stat-label">Total Devices</div>
              </div>
              <div className="stat-card">
                <div className="stat-value">
                  {devices.filter((d) => d.status?.toLowerCase() === 'idle').length}
                </div>
                <div className="stat-label">Available</div>
              </div>
              <div className="stat-card">
                <div className="stat-value">
                  {devices.filter((d) => d.status?.toLowerCase() === 'claimed' || d.status?.toLowerCase() === 'busy').length}
                </div>
                <div className="stat-label">In Use</div>
              </div>
            </div>

            <div className="toolbar">
              <h2>Connected Devices</h2>
              <button className="btn btn-ghost" onClick={fetchDevices} disabled={loading}>
                {loading ? <span className="spinner"></span> : 'Refresh'}
              </button>
            </div>

            <div className="device-grid">
              {devices.length === 0 ? (
                <div className="empty">
                  <div className="empty-icon">📱</div>
                  <h3>No Devices Detected</h3>
                  <p>Ensure adb is running and your Android devices are connected.</p>
                </div>
              ) : (
                devices.map((device) => (
                  <div
                    key={device.serial}
                    className={`device-card ${device.status?.toLowerCase()}`}
                    onClick={() => {
                      if (device.status?.toLowerCase() === 'idle') {
                        handleClaim(device);
                      } else if (device.status?.toLowerCase() === 'claimed' || device.status?.toLowerCase() === 'busy') {
                        setActiveDevice({
                          ...device,
                          streamPort: device.stream_port || 7500
                        });
                      }
                    }}
                  >
                    <div className="card-header">
                      <div className="device-icon">🤖</div>
                      <div>
                        <div className="card-title">{device.manufacturer} {device.model}</div>
                        <div className="card-serial">{device.serial}</div>
                      </div>
                      <span className={`status-pill ${getStatusClass(device.status)}`}>
                        {device.status}
                      </span>
                    </div>
                    <div className="card-body">
                      <div className="info-row">
                        <div className="info-chip">OS: <span>Android {device.android} (SDK {device.sdk})</span></div>
                        <div className="info-chip">ABI: <span>{device.abi}</span></div>
                        <div className="info-chip">Screen: <span>{device.display}</span></div>
                      </div>
                      <div className="battery-row">
                        <span className="info-chip">Battery</span>
                        <div className="battery-bar">
                          <div
                            className={`battery-fill ${device.battery <= 20 ? 'low' : device.battery <= 50 ? 'mid' : ''}`}
                            style={{ width: `${device.battery}%` }}
                          ></div>
                        </div>
                        <span className="battery-label">{device.battery}%</span>
                      </div>
                      {device.wifi_ssid && (
                        <div className="info-row">
                          <div className="info-chip">WiFi: <span>{device.wifi_ssid}</span></div>
                          <div className="info-chip">IP: <span>{device.ip}</span></div>
                        </div>
                      )}
                    </div>
                    <div className="card-footer" onClick={(e) => e.stopPropagation()}>
                      {device.status?.toLowerCase() === 'idle' && (
                        <button className="btn btn-primary btn-sm" onClick={() => handleClaim(device)}>
                          Claim & Control
                        </button>
                      )}
                      {(device.status?.toLowerCase() === 'claimed' || device.status?.toLowerCase() === 'busy') && (
                        <>
                          <button className="btn btn-ghost btn-sm" onClick={() => setActiveDevice({
                            ...device,
                            streamPort: device.stream_port || 7500
                          })}>
                            View Stream
                          </button>
                          <button className="btn btn-danger btn-sm" onClick={() => handleRelease(device.serial)}>
                            Release
                          </button>
                        </>
                      )}
                    </div>
                  </div>
                ))
              )}
            </div>
          </>
        )}
      </main>

      {/* Toasts */}
      <div className="toast-wrap">
        {toasts.map((t) => (
          <div key={t.id} className={`toast ${t.type}`}>
            {t.message}
          </div>
        ))}
      </div>
    </div>
  );
}

// ── Device Page (Split View) using MediaSource Extensions (MSE) ──────────────

function DevicePage({ device, onBack, onRelease }) {
  const videoRef = useRef(null);
  const mediaSourceRef = useRef(null);
  const sourceBufferRef = useRef(null);
  const queueRef = useRef([]);
  const [errorMsg, setErrorMsg] = useState('');
  const [isPlaying, setIsPlaying] = useState(false);
  const [activeTab, setActiveTab] = useState('actions');
  const [videoWidth, setVideoWidth] = useState(0);
  const [videoHeight, setVideoHeight] = useState(0);
  const [rotation, setRotation] = useState(0);

  const streamPort = device.streamPort || 7500;
  const streamUrl = `http://localhost:${streamPort}/stream`;
  const controlUrl = `http://localhost:${streamPort}/control`;

  const sendTouchEvent = async (action, normX, normY, pressure = 1.0) => {
    try {
      await fetch(controlUrl, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ type: 'touch', action, x: normX, y: normY, pressure }),
      });
    } catch (err) {
      // Silently ignore control failures to avoid spamming errors during drag
    }
  };

  const sendScrollEvent = async (normX, normY, vscroll) => {
    try {
      await fetch(controlUrl, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ type: 'scroll', x: normX, y: normY, hscroll: 0, vscroll }),
      });
    } catch (err) {}
  };

  const getVideoNormCoords = (e) => {
    const rect = videoRef.current.getBoundingClientRect();
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
    sendTouchEvent(0, x, y, 1.0);
  };

  const handleMouseMove = (e) => {
    if (!isDragging.current) return;
    e.preventDefault();
    const { x, y } = getVideoNormCoords(e);
    sendTouchEvent(2, x, y, 1.0);
  };

  const handleMouseUp = (e) => {
    if (!isDragging.current) return;
    isDragging.current = false;
    const { x, y } = getVideoNormCoords(e);
    sendTouchEvent(1, x, y, 0);
  };

  const handleTouchStart = (e) => {
    e.preventDefault();
    const touch = e.touches[0];
    const rect = videoRef.current.getBoundingClientRect();
    const x = Math.max(0, Math.min(1, (touch.clientX - rect.left) / rect.width));
    const y = Math.max(0, Math.min(1, (touch.clientY - rect.top) / rect.height));
    sendTouchEvent(0, x, y, 1.0);
  };

  const handleTouchMove = (e) => {
    e.preventDefault();
    const touch = e.touches[0];
    const rect = videoRef.current.getBoundingClientRect();
    const x = Math.max(0, Math.min(1, (touch.clientX - rect.left) / rect.width));
    const y = Math.max(0, Math.min(1, (touch.clientY - rect.top) / rect.height));
    sendTouchEvent(2, x, y, 1.0);
  };

  const handleTouchEnd = (e) => {
    e.preventDefault();
    const touch = e.changedTouches[0];
    const rect = videoRef.current.getBoundingClientRect();
    const x = Math.max(0, Math.min(1, (touch.clientX - rect.left) / rect.width));
    const y = Math.max(0, Math.min(1, (touch.clientY - rect.top) / rect.height));
    sendTouchEvent(1, x, y, 0);
  };

  const handleWheel = (e) => {
    e.preventDefault();
    const { x, y } = getVideoNormCoords(e);
    const delta = e.deltaY > 0 ? -1 : 1;
    sendScrollEvent(x, y, delta);
  };
  const sendControlKey = async (keycode) => {
    try {
      await fetch(controlUrl, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ type: 'key', keycode }),
      });
    } catch (err) {
      console.error('Failed to send key event:', err);
    }
  };

  const sendRotation = (degrees) => {
    setRotation(degrees);
  };

  useEffect(() => {
    let active = true;
    const abortController = new AbortController();
    let objectUrl = '';
    let reader = null;

    const resetStreamState = () => {
      queueRef.current = [];
      sourceBufferRef.current = null;
      mediaSourceRef.current = null;
    };

    const appendNextChunk = (sourceBuffer) => {
      if (!active || !sourceBuffer || sourceBuffer.updating || queueRef.current.length === 0) {
        return;
      }

      const chunk = queueRef.current.shift();
      try {
        sourceBuffer.appendBuffer(chunk);
      } catch (e) {
        console.error('Failed to append buffer', e);
      }
    };

    const getCodecFromInitSegment = (chunk) => {
      // Search for the 'avcC' box (signature [0x61, 0x76, 0x63, 0x43])
      for (let i = 0; i < chunk.length - 8; i++) {
        if (
          chunk[i] === 0x61 &&
          chunk[i + 1] === 0x76 &&
          chunk[i + 2] === 0x63 &&
          chunk[i + 3] === 0x43
        ) {
          const profile = chunk[i + 5];
          const compat = chunk[i + 6];
          const level = chunk[i + 7];
          const profileHex = profile.toString(16).padStart(2, '0');
          const compatHex = compat.toString(16).padStart(2, '0');
          const levelHex = level.toString(16).padStart(2, '0');
          return `avc1.${profileHex}${compatHex}${levelHex}`;
        }
      }
      return null;
    };

    const startAndMuxStream = async () => {
      try {
        console.log(`Connecting to stream at: ${streamUrl}`);
        const response = await fetch(streamUrl, { signal: abortController.signal });
        if (!response.ok) {
          throw new Error(`Failed to fetch stream: ${response.statusText}`);
        }
        setIsPlaying(true);

        reader = response.body.getReader();

        let codec = 'avc1.42c01e'; // Default fallback
        let firstChunk = null;
        const accumulated = [];
        let accumulatedLength = 0;

        // Read chunks until we have enough to find 'avcC' or hit 8KB
        while (active) {
          const { done, value } = await reader.read();
          if (done) break;

          accumulated.push(value);
          accumulatedLength += value.length;

          // Merge accumulated chunks
          const tempBuffer = new Uint8Array(accumulatedLength);
          let offset = 0;
          for (const chunk of accumulated) {
            tempBuffer.set(chunk, offset);
            offset += chunk.length;
          }

          const detectedCodec = getCodecFromInitSegment(tempBuffer);
          if (detectedCodec) {
            codec = detectedCodec;
            firstChunk = tempBuffer;
            console.log(`Detected stream codec: ${codec}`);
            break;
          }

          if (accumulatedLength >= 8192) {
            firstChunk = tempBuffer;
            console.warn(`Could not detect avcC box in first ${accumulatedLength} bytes, falling back to ${codec}`);
            break;
          }
        }

        if (!active) return;

        // Initialize MediaSource
        const ms = new MediaSource();
        mediaSourceRef.current = ms;

        if (videoRef.current) {
          objectUrl = URL.createObjectURL(ms);
          videoRef.current.src = objectUrl;
        }

        // Wait for sourceopen
        await new Promise((resolve, reject) => {
          const onOpen = () => {
            ms.removeEventListener('sourceopen', onOpen);
            resolve();
          };
          ms.addEventListener('sourceopen', onOpen);
          abortController.signal.addEventListener('abort', () => reject(new Error('Aborted')));
        });

        if (!active) return;

        const mime = `video/mp4; codecs="${codec}"`;
        if (!MediaSource.isTypeSupported(mime)) {
          throw new Error(`MIME type ${mime} is not supported by your browser.`);
        }

        const sb = ms.addSourceBuffer(mime);
        sourceBufferRef.current = sb;

        // Handle buffer queueing when previous appends finish
        sb.addEventListener('updateend', () => {
          appendNextChunk(sb);

          if (videoRef.current && videoRef.current.buffered.length > 0) {
            const end = videoRef.current.buffered.end(videoRef.current.buffered.length - 1);
            if (end - videoRef.current.currentTime > 0.5) {
              videoRef.current.currentTime = end - 0.1;
            }
          }
        });

        // Append the accumulated first chunk
        if (firstChunk && firstChunk.length > 0) {
          queueRef.current.push(firstChunk);
          appendNextChunk(sb);
        }

        // Start reading the rest of the stream
        while (active) {
          const { done, value } = await reader.read();
          if (done) break;

          if (sb.updating || queueRef.current.length > 0) {
            queueRef.current.push(value);
          } else {
            queueRef.current.push(value);
            appendNextChunk(sb);
          }
        }
      } catch (err) {
        if (err.name !== 'AbortError' && active) {
          console.error(err);
          setErrorMsg(`Stream connection lost: ${err.message}`);
        }
      }
    };

    startAndMuxStream();

    return () => {
      active = false;
      abortController.abort();
      if (reader) {
        reader.cancel().catch(() => {});
      }

      const ms = mediaSourceRef.current;
      const sb = sourceBufferRef.current;
      if (ms && sb && ms.readyState === 'open') {
        try {
          if (sb.updating) sb.abort();
        } catch {
          // Ignore teardown races while the browser is closing the MediaSource.
        }

        try {
          if (Array.from(ms.sourceBuffers).includes(sb)) {
            ms.removeSourceBuffer(sb);
          }
        } catch {
          // Ignore teardown races while the browser is closing the MediaSource.
        }
      }

      if (videoRef.current) {
        videoRef.current.removeAttribute('src');
        videoRef.current.load();
      }
      if (objectUrl) {
        URL.revokeObjectURL(objectUrl);
      }
      resetStreamState();
    };
  }, [streamUrl]);

  const isLandscape = videoWidth && videoHeight && videoWidth > videoHeight;
  const currentAspectRatio = isLandscape && videoWidth && videoHeight
    ? `${videoWidth} / ${videoHeight}`
    : '9 / 18.6';

  const containerStyle = {
    aspectRatio: currentAspectRatio,
    height: isLandscape
      ? 'clamp(300px, calc(100vh - 165px), 440px)'
      : 'clamp(480px, calc(100vh - 145px), 600px)',
    maxWidth: isLandscape ? '680px' : '318px',
  };

  const handleResize = (e) => {
    const w = e.target.videoWidth;
    const h = e.target.videoHeight;
    setVideoWidth(w);
    setVideoHeight(h);
    if (w > h) {
      setRotation(90);
    } else {
      setRotation(0);
    }
  };

  return (
    <div className="device-page">
      {/* Left Column: Screen Mockup */}
      <div className="screen-column">
        <div 
          className={`phone-container ${isLandscape ? 'landscape' : ''}`}
          style={containerStyle}
        >
          <div className="phone-screen">
            {errorMsg && (
              <div className="phone-placeholder" style={{ color: 'var(--red)' }}>
                <div style={{ fontSize: '24px' }}>⚠️</div>
                <div style={{ marginTop: '8px', fontSize: '12px' }}>{errorMsg}</div>
              </div>
            )}
            {!errorMsg && !isPlaying && (
              <div className="phone-placeholder">
                <span className="spinner"></span>
                <div style={{ marginTop: '12px', fontSize: '12px' }}>Connecting to live stream...</div>
              </div>
            )}
            {!errorMsg && (
              <video
                ref={videoRef}
                autoPlay
                playsInline
                muted
                controls={false}
                className="phone-video"
                onLoadedMetadata={handleResize}
                onResize={handleResize}
                onError={() => {
                  if (videoRef.current?.error) {
                    setErrorMsg(`Video playback/decoding error: ${videoRef.current.error.message || `Code ${videoRef.current.error.code}`}`);
                  }
                }}
                onMouseDown={handleMouseDown}
                onMouseMove={handleMouseMove}
                onMouseUp={handleMouseUp}
                onMouseLeave={handleMouseUp}
                onTouchStart={handleTouchStart}
                onTouchMove={handleTouchMove}
                onTouchEnd={handleTouchEnd}
                onWheel={handleWheel}
                style={{
                  display: isPlaying ? 'block' : 'none',
                  width: '100%',
                  height: '100%',
                  objectFit: isLandscape ? 'contain' : 'cover',
                  cursor: 'crosshair',
                  touchAction: 'none',
                  userSelect: 'none',
                }}
              />
            )}
          </div>
        </div>
        <div className="phone-controls">
          <button className="phone-control-btn" onClick={() => sendRotation(rotation === 0 ? 90 : 0)}>
            🔄 Rotate
          </button>
          <button className="phone-control-btn" onClick={() => sendControlKey(3)}>
            🏠 Home
          </button>
          <button className="phone-control-btn" onClick={() => sendControlKey(4)}>
            ◀ Back
          </button>
        </div>
      </div>

      {/* Right Column: Details & Features */}
      <div className="details-column">
        <div className="back-header">
          <button className="btn btn-ghost" onClick={onBack}>
            ← Back to Dashboard
          </button>
          <span className="status-pill status-claimed" style={{ marginLeft: '0' }}>
            Active Session
          </span>
        </div>

        <div className="details-card">
          <h3>📱 {device.manufacturer} {device.model} Details</h3>
          <div className="meta-grid">
            <div className="meta-item">
              <span className="meta-label">Serial Number</span>
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
              <span className="meta-label">Screen Resolution</span>
              <span className="meta-val">{device.display || 'Unknown'}</span>
            </div>
            <div className="meta-item">
              <span className="meta-label">Battery Level</span>
              <span className="meta-val">{device.battery}%</span>
            </div>
            <div className="meta-item">
              <span className="meta-label">WiFi Connection</span>
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
        </div>

        {/* Action / Future Feature Tabs */}
        <div className="details-card" style={{ flex: 1 }}>
          <div className="tabs-header">
            <button
              className={`tab-btn ${activeTab === 'actions' ? 'active' : ''}`}
              onClick={() => setActiveTab('actions')}
            >
              Control Panel
            </button>
            <button
              className={`tab-btn ${activeTab === 'shell' ? 'active' : ''}`}
              onClick={() => setActiveTab('shell')}
            >
              Terminal Shell
            </button>
            <button
              className={`tab-btn ${activeTab === 'apps' ? 'active' : ''}`}
              onClick={() => setActiveTab('apps')}
            >
              Apps & Files
            </button>
          </div>

          <div className="tab-content">
            {activeTab === 'actions' && (
              <div style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
                <p>Perform basic interactions and lifecycle operations on this device.</p>
                <div style={{ display: 'flex', gap: '12px', flexWrap: 'wrap', marginTop: '4px' }}>
                  <button className="btn btn-danger" onClick={onRelease}>
                    Release Device
                  </button>
                  <button className="btn btn-ghost" onClick={() => sendControlKey(224)}>
                    Wake Screen
                  </button>
                  <button className="btn btn-ghost" onClick={() => sendControlKey(3)}>
                    Home
                  </button>
                  <button className="btn btn-ghost" onClick={() => sendControlKey(4)}>
                    Back
                  </button>
                </div>
              </div>
            )}

            {activeTab === 'shell' && (
              <div className="feature-placeholder">
                <div className="feature-placeholder-icon">💻</div>
                <h4>Interactive ADB Shell</h4>
                <p style={{ fontSize: '12px', marginTop: '4px' }}>
                  Execute shell commands directly on the Android device. This feature will be added in a future update.
                </p>
              </div>
            )}

            {activeTab === 'apps' && (
              <div className="feature-placeholder">
                <div className="feature-placeholder-icon">📂</div>
                <h4>File Manager & APK Installer</h4>
                <p style={{ fontSize: '12px', marginTop: '4px' }}>
                  Drag and drop APK files to install them, or browse internal storage. This feature will be added in a future update.
                </p>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

export default App;
