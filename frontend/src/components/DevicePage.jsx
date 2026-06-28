import React, { useState, useEffect, useRef } from 'react';
import { AlertTriangle, Camera, ChevronLeft, Circle, FileText, Power, RotateCw, Square, WandSparkles } from 'lucide-react';
import './DevicePage.css';
import DashboardTab from './device-page/DashboardTab';
import DeviceTabsHeader from './device-page/DeviceTabsHeader';
import FilesTab from './device-page/FilesTab';
import InfoTab from './device-page/InfoTab';
import PlaceholderTab from './device-page/PlaceholderTab';
function DevicePage({ device, onBack, onRelease }) {
  const canvasRef = useRef(null);
  const wsRef = useRef(null);
  const [errorMsg, setErrorMsg] = useState('');
  const [isPlaying, setIsPlaying] = useState(false);
  const [activeTab, setActiveTab] = useState('dashboard');
  const [videoWidth, setVideoWidth] = useState(0);
  const [videoHeight, setVideoHeight] = useState(0);

  const [shellCmd, setShellCmd] = useState('');
  const [navUrl, setNavUrl] = useState('');

  const COORDINATOR_API = import.meta.env.VITE_COORDINATOR_API || `${window.location.protocol}//${window.location.hostname}:9002`;

  const execShell = async (command) => {
    try {
      const res = await fetch(`${COORDINATOR_API}/api/v1/devices/${device.serial}/control`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ type: 'shell', command })
      });
      const data = await res.json();
      return data;
    } catch (err) {
      console.error("Shell error", err);
      return { success: false, message: err.message };
    }
  };

  const [uploadProgress, setUploadProgress] = useState({
    active: false,
    stage: '', // 'uploading', 'installing', 'opening', 'done', 'error'
    percent: 0,
    message: '',
    type: ''
  });

  const handleFileUpload = (file, type) => {
    if (!wsUrl) return;
    const uploadUrl = wsUrl.replace(/^ws(s?):\/\//i, 'http$1://').replace(/\/ws$/, '/upload');
    const formData = new FormData();
    formData.append('file', file);
    formData.append('type', type);

    setUploadProgress({
      active: true,
      stage: 'uploading',
      percent: 0,
      message: 'Preparing upload...',
      type: type
    });

    const xhr = new XMLHttpRequest();
    xhr.open('POST', uploadUrl, true);

    // Track upload progress (network phase, 0% to 50% of the progress bar)
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) {
        const uploadPercent = Math.round((e.loaded / e.total) * 100);
        const overallPercent = Math.round(uploadPercent * 0.5);
        setUploadProgress({
          active: true,
          stage: 'uploading',
          percent: overallPercent,
          message: `Uploading file (${uploadPercent}%)`,
          type: type
        });
      }
    };

    let lastIndex = 0;

    const handleChunk = (chunk) => {
      const lines = chunk.split('\n');
      for (const line of lines) {
        if (!line.trim()) continue;
        try {
          const data = JSON.parse(line);
          if (data.stage === 'installing') {
            setUploadProgress({
              active: true,
              stage: 'installing',
              percent: 75,
              message: data.message || 'Installing on device...',
              type: type
            });
          } else if (data.stage === 'opening') {
            setUploadProgress({
              active: true,
              stage: 'opening',
              percent: 90,
              message: data.message || 'Opening application...',
              type: type
            });
          } else if (data.stage === 'done') {
            setUploadProgress({
              active: true,
              stage: 'done',
              percent: 100,
              message: data.message || 'Completed!',
              type: type
            });
            setTimeout(() => {
              setUploadProgress(prev => {
                if (prev.type === type && prev.stage === 'done') {
                  return { active: false, stage: '', percent: 0, message: '', type: '' };
                }
                return prev;
              });
            }, 3000);
          } else if (data.stage === 'error') {
            setUploadProgress({
              active: true,
              stage: 'error',
              percent: 0,
              message: data.message || 'An error occurred',
              type: type
            });
            alert(`Upload Failed: ${data.message}`);
            setTimeout(() => {
              setUploadProgress(prev => {
                if (prev.type === type && prev.stage === 'error') {
                  return { active: false, stage: '', percent: 0, message: '', type: '' };
                }
                return prev;
              });
            }, 4000);
          }
        } catch (err) {
          console.error("Failed to parse progress chunk:", err);
        }
      }
    };

    xhr.onreadystatechange = () => {
      if (xhr.readyState === 3 || xhr.readyState === 4) {
        const newText = xhr.responseText.substring(lastIndex);
        lastIndex = xhr.responseText.length;
        if (newText) {
          handleChunk(newText);
        }
      }

      if (xhr.readyState === 4) {
        if (xhr.status < 200 || xhr.status >= 300) {
          setUploadProgress(prev => {
            if (prev.type === type && prev.stage !== 'error' && prev.stage !== 'done') {
              alert(`Upload Error: Status ${xhr.status} ${xhr.statusText}`);
              return { active: false, stage: '', percent: 0, message: '', type: '' };
            }
            return prev;
          });
        }
      }
    };

    xhr.onerror = () => {
      setUploadProgress({
        active: true,
        stage: 'error',
        percent: 0,
        message: 'Network error occurred.',
        type: type
      });
      alert('Upload Error: Network failure');
      setTimeout(() => {
        setUploadProgress({ active: false, stage: '', percent: 0, message: '', type: '' });
      }, 4000);
    };

    xhr.send(formData);
  };

  const handleDrop = (e, type) => {
    e.preventDefault();
    if (e.dataTransfer.files && e.dataTransfer.files.length > 0) {
      handleFileUpload(e.dataTransfer.files[0], type);
    }
  };

  const handleDragOver = (e) => {
    e.preventDefault();
  };

  const handleDropzoneClick = (type) => {
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = type === 'app' ? '.apk' : '*/*';
    input.onchange = (e) => {
      if (e.target.files && e.target.files.length > 0) {
        handleFileUpload(e.target.files[0], type);
      }
    };
    input.click();
  };

  const [cardOrder, setCardOrder] = useState(() => {
    try {
      const saved = localStorage.getItem('devicePageOrder');
      return saved ? JSON.parse(saved) : [
        'app_upload', 'file_upload', 'maintenance', 'navigation', 
        'shell', 'apps', 'advanced_input', 'upload_server'
      ];
    } catch {
      return [
        'app_upload', 'file_upload', 'maintenance', 'navigation', 
        'shell', 'apps', 'advanced_input', 'upload_server'
      ];
    }
  });
  const [draggedCardIndex, setDraggedCardIndex] = useState(null);

  const handleCardDragStart = (e, index) => {
    setDraggedCardIndex(index);
    e.dataTransfer.effectAllowed = 'move';
  };

  const handleCardDragOver = (e, index) => {
    e.preventDefault();
    if (draggedCardIndex === null || draggedCardIndex === index) return;
    
    const newOrder = [...cardOrder];
    const draggedItem = newOrder[draggedCardIndex];
    newOrder.splice(draggedCardIndex, 1);
    newOrder.splice(index, 0, draggedItem);
    
    setCardOrder(newOrder);
    localStorage.setItem('devicePageOrder', JSON.stringify(newOrder));
    setDraggedCardIndex(index);
  };

  const handleCardDragEnd = () => {
    setDraggedCardIndex(null);
  };

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
  const stateUrl = device.provider_id && streamPort
    ? `http://${device.provider_id}:${streamPort}/state`
    : null;

  // We no longer poll stateUrl because state is delivered via websocket (DEVICE_LIST_UPDATE)


  // ── Input event handlers ────────────────────────────────────────────────────

  const handleFolderClick = (path) => {
    execShell(`am broadcast -a com.protean.agent.COMMAND -e command "LIST_DIRECTORY" -e path "${path}"`);
  };

  const handleBackClick = (currentPath) => {
    if (!currentPath) return;
    const parts = currentPath.split('/');
    if (parts.length <= 2) return; // e.g. ["", "storage"] -> can't go higher
    parts.pop();
    const parentPath = parts.join('/');
    handleFolderClick(parentPath);
  };

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
              <RotateCw size={20} />
              <label>Rotate</label>
            </button>
            <button className="side-btn" title="Power / Wake" onClick={() => sendControlKey(224)}>
              <Power size={20} />
              <label>Wake</label>
            </button>
          </div>

          {/* Phone mockup */}
          <div className="phone-container">
            <div className={`phone-wrapper ${isLandscape ? 'landscape' : ''}`}>
              <div className="phone-screen" style={screenStyle}>
                {/* Error overlay */}
                {errorMsg && (
                  <div className="phone-placeholder error">
                    <AlertTriangle size={28} />
                    <div className="phone-placeholder-message">{errorMsg}</div>
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
              <Circle size={20} />
              <label>Home</label>
            </button>
            <button className="side-btn" title="Back" onClick={() => sendControlKey(4)}>
              <ChevronLeft size={20} />
              <label>Back</label>
            </button>
            <button className="side-btn" title="Recents" onClick={() => sendControlKey(187)}>
              <Square size={20} />
              <label>Recent</label>
            </button>
          </div>
        </div>
      </div>

      {/* ── RIGHT: Control & Overview Tabs ──────────────────────────── */}
      <div className="details-column">
        <DeviceTabsHeader
          deviceModel={device.model}
          activeTab={activeTab}
          onTabChange={setActiveTab}
          onBack={onBack}
        />

        <div className="tab-content">
          {activeTab === 'dashboard' && (
            <DashboardTab
              cardOrder={cardOrder}
              draggedCardIndex={draggedCardIndex}
              onCardDragStart={handleCardDragStart}
              onCardDragOver={handleCardDragOver}
              onCardDragEnd={handleCardDragEnd}
              uploadProgress={uploadProgress}
              onDropzoneClick={handleDropzoneClick}
              onDrop={handleDrop}
              onDragOver={handleDragOver}
              navUrl={navUrl}
              setNavUrl={setNavUrl}
              execShell={execShell}
              shellCmd={shellCmd}
              setShellCmd={setShellCmd}
              sendControlKey={sendControlKey}
            />
          )}

          {activeTab === 'automation' && (
            <PlaceholderTab
              icon={WandSparkles}
              title="Automation Studio"
              description="Create and run automated test scripts on this device."
              colorClass="color-cyan"
            />
          )}

          {activeTab === 'media' && (
            <PlaceholderTab
              icon={Camera}
              title="Media Gallery"
              description="View screenshots and screen recordings from this device."
              colorClass="color-blue"
            />
          )}

          {activeTab === 'logs' && (
            <PlaceholderTab
              icon={FileText}
              title="Logs & PT"
              description="View Logcat logs and PT parameters of the device."
              colorClass="color-emerald"
            />
          )}

          {activeTab === 'files' && (
            <FilesTab device={device} onFolderClick={handleFolderClick} onBackClick={handleBackClick} />
          )}

          {activeTab === 'info' && (
            <InfoTab device={device} streamPort={streamPort} onRelease={onRelease} />
          )}
        </div>
      </div>
    </div>
  );
}

export default DevicePage;
