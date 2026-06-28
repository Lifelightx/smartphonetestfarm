import React from 'react';
import {
  AppWindow,
  Code,
  Compass,
  Download,
  FolderUp,
  Globe,
  MonitorPlay,
  Package,
  PackagePlus,
  Play,
  Server,
  Settings,
  Settings2,
  ShoppingCart,
  Terminal,
  UploadCloud,
  Volume2,
  Wifi,
} from 'lucide-react';

function UploadProgress({ uploadProgress }) {
  return (
    <div className="upload-progress-container">
      <div className="upload-progress-info">
        <span className="upload-progress-message">{uploadProgress.message}</span>
        <span className="upload-progress-percent">{uploadProgress.percent}%</span>
      </div>
      <div className="upload-progress-bar-bg">
        <div
          className={`upload-progress-bar-fill ${uploadProgress.stage}`}
          style={{ width: `${uploadProgress.percent}%` }}
        />
      </div>
    </div>
  );
}

function UploadCard({ title, icon: Icon, colorClass, uploadType, uploadProgress, onDropzoneClick, onDrop, onDragOver, copy }) {
  return (
    <div className="card-body">
      {uploadProgress.active && uploadProgress.type === uploadType ? (
        <UploadProgress uploadProgress={uploadProgress} />
      ) : (
        <button
          type="button"
          className="dropzone"
          onClick={() => onDropzoneClick(uploadType)}
          onDrop={(event) => onDrop(event, uploadType)}
          onDragOver={onDragOver}
        >
          <span className={`dropzone-icon ${colorClass}`}>
            <Icon size={22} />
          </span>
          <span>{copy}</span>
        </button>
      )}
    </div>
  );
}

const appShortcuts = [
  { label: 'Settings', icon: Settings, color: '#94a3b8', command: 'am start -a android.settings.SETTINGS' },
  { label: 'App Store', icon: ShoppingCart, color: '#22c55e', command: 'am start -a android.intent.action.VIEW -d "market://details?id=com.android.chrome"' },
  { label: 'Language', icon: Globe, color: '#38bdf8', command: 'am start -a android.settings.LOCALE_SETTINGS' },
  { label: 'WiFi', icon: Wifi, color: '#a78bfa', command: 'am start -a android.settings.WIFI_SETTINGS' },
  { label: 'Manage Apps', icon: Package, color: '#fb923c', command: 'am start -a android.settings.MANAGE_APPLICATIONS_SETTINGS' },
  { label: 'Developer', icon: Code, color: '#f87171', command: 'am start -a android.settings.APPLICATION_DEVELOPMENT_SETTINGS' },
];

function DashboardTab(props) {
  const {
    cardOrder,
    draggedCardIndex,
    onCardDragStart,
    onCardDragOver,
    onCardDragEnd,
    uploadProgress,
    onDropzoneClick,
    onDrop,
    onDragOver,
    navUrl,
    setNavUrl,
    execShell,
    shellCmd,
    setShellCmd,
    sendControlKey,
  } = props;

  const renderCard = (id, index) => {
    const isDragging = draggedCardIndex === index;
    const dragProps = {
      draggable: true,
      onDragStart: (event) => onCardDragStart(event, index),
      onDragOver: (event) => onCardDragOver(event, index),
      onDragEnd: onCardDragEnd,
      style: {
        cursor: isDragging ? 'grabbing' : 'grab',
        opacity: isDragging ? 0.35 : 1,
        transform: isDragging ? 'scale(0.98)' : 'none',
        border: isDragging ? '2px dashed var(--accent)' : '',
      },
    };

    switch (id) {
      case 'app_upload':
        return (
          <div key={id} className="dashboard-card" {...dragProps}>
            <div className="card-header">
              <span className="card-title color-blue"><PackagePlus size={16} /> App Upload</span>
            </div>
            <UploadCard
              title="App Upload"
              icon={Download}
              colorClass="color-blue"
              uploadType="app"
              uploadProgress={uploadProgress}
              onDropzoneClick={onDropzoneClick}
              onDrop={onDrop}
              onDragOver={onDragOver}
              copy="Click or drop APK to install"
            />
          </div>
        );
      case 'file_upload':
        return (
          <div key={id} className="dashboard-card" {...dragProps}>
            <div className="card-header">
              <span className="card-title color-emerald"><UploadCloud size={16} /> File Upload</span>
            </div>
            <UploadCard
              title="File Upload"
              icon={FolderUp}
              colorClass="color-emerald"
              uploadType="file"
              uploadProgress={uploadProgress}
              onDropzoneClick={onDropzoneClick}
              onDrop={onDrop}
              onDragOver={onDragOver}
              copy="Click or drop file to upload"
            />
          </div>
        );
      case 'maintenance':
        return (
          <div key={id} className="dashboard-card" {...dragProps}>
            <div className="card-header">
              <span className="card-title color-orange"><Settings2 size={16} /> Maintenance</span>
            </div>
            <div className="card-body">
              <button className="btn btn-danger" style={{ width: '100%' }} onClick={() => execShell('reboot')}>
                Restart Device
              </button>
            </div>
          </div>
        );
      case 'navigation':
        return (
          <div key={id} className="dashboard-card" {...dragProps}>
            <div className="card-header">
              <span className="card-title color-cyan"><Compass size={16} /> Navigation</span>
              <button className="btn btn-sm btn-danger" onClick={() => setNavUrl('')}>Reset</button>
            </div>
            <div className="card-body">
              <div className="nav-input-row">
                <input
                  type="text"
                  className="nav-input"
                  placeholder="http://..."
                  value={navUrl}
                  onChange={(event) => setNavUrl(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter' && navUrl) {
                      execShell(`am start -a android.intent.action.VIEW -d "${navUrl}"`);
                    }
                  }}
                />
                <button className="btn btn-primary" onClick={() => navUrl && execShell(`am start -a android.intent.action.VIEW -d "${navUrl}"`)}>
                  Open
                </button>
              </div>
              <div className="browser-icons">
                <button title="Chrome" onClick={() => execShell(`am start -n com.android.chrome/com.google.android.apps.chrome.Main -d "${navUrl || 'https://google.com'}"`)}>
                  <MonitorPlay size={16} color="#60a5fa" />
                </button>
                <button title="Browser" onClick={() => execShell(`am start -a android.intent.action.VIEW -d "${navUrl || 'https://google.com'}"`)}>
                  <Globe size={16} color="#34d399" />
                </button>
              </div>
            </div>
          </div>
        );
      case 'shell':
        return (
          <div key={id} className="dashboard-card" {...dragProps}>
            <div className="card-header">
              <span className="card-title color-violet"><Terminal size={16} /> Shell</span>
              <button className="btn btn-sm btn-danger" onClick={() => setShellCmd('')}>Clear</button>
            </div>
            <div className="card-body">
              <div className="nav-input-row">
                <input
                  type="text"
                  className="nav-input"
                  placeholder="ls -la"
                  value={shellCmd}
                  onChange={(event) => setShellCmd(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter' && shellCmd) {
                      execShell(shellCmd).then((result) => {
                        if (result.message) alert(result.message);
                      });
                    }
                  }}
                />
                <button
                  className="btn btn-primary"
                  onClick={() => shellCmd && execShell(shellCmd).then((result) => {
                    if (result.message) alert(result.message);
                  })}
                >
                  <Play size={14} />
                </button>
              </div>
            </div>
          </div>
        );
      case 'apps':
        return (
          <div key={id} className="dashboard-card" {...dragProps}>
            <div className="card-header">
              <span className="card-title color-amber"><AppWindow size={16} /> Apps</span>
            </div>
            <div className="card-body">
              <div className="app-grid">
                {appShortcuts.map(({ label, icon: Icon, color, command }) => (
                  <button key={label} className="app-icon-btn" onClick={() => execShell(command)}>
                    <Icon size={20} color={color} />
                    {label}
                  </button>
                ))}
              </div>
            </div>
          </div>
        );
      case 'advanced_input':
        return (
          <div key={id} className="dashboard-card" {...dragProps}>
            <div className="card-header">
              <span className="card-title color-rose"><Volume2 size={16} /> Advanced Input</span>
            </div>
            <div className="card-body">
              <span className="card-subtitle">Volume Control</span>
              <div className="volume-row">
                <button className="btn btn-ghost" style={{ flex: 1 }} onClick={() => sendControlKey(164)}>Mute</button>
                <button className="btn btn-ghost" style={{ flex: 1 }} onClick={() => sendControlKey(25)}>Vol -</button>
                <button className="btn btn-ghost" style={{ flex: 1 }} onClick={() => sendControlKey(24)}>Vol +</button>
              </div>
            </div>
          </div>
        );
      case 'upload_server':
        return (
          <div key={id} className="dashboard-card" {...dragProps}>
            <div className="card-header">
              <span className="card-title color-cyan"><Server size={16} /> Upload To Server</span>
            </div>
            <div className="card-body">
              {uploadProgress.active && uploadProgress.type === 'server' ? (
                <UploadProgress uploadProgress={uploadProgress} />
              ) : (
                <button
                  type="button"
                  className="dropzone"
                  onClick={() => onDropzoneClick('server')}
                  onDrop={(event) => onDrop(event, 'server')}
                  onDragOver={onDragOver}
                >
                  <span className="dropzone-icon color-cyan">
                    <Server size={22} />
                  </span>
                  <span>Click or drop file to save on server</span>
                </button>
              )}
              <button className="btn btn-ghost" style={{ marginTop: '8px', width: '100%' }} onClick={() => alert('No history available yet.')}>
                Show History
              </button>
            </div>
          </div>
        );
      default:
        return null;
    }
  };

  return <div className="dashboard-grid">{cardOrder.map((id, index) => renderCard(id, index))}</div>;
}

export default DashboardTab;
