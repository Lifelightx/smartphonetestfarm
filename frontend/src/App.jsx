import React, { useState, useEffect } from 'react';
import Header from './components/Header';
import StatsBar from './components/StatsBar';
import DeviceCard from './components/DeviceCard';
import DevicePage from './components/DevicePage';

// Coordinator's API address (usually GRPCPort + 2, e.g. 9002)
const COORDINATOR_API = import.meta.env.VITE_COORDINATOR_API || `${window.location.protocol}//${window.location.hostname}:9002`;

function App() {
  const [devices, setDevices] = useState([]);
  const [loading, setLoading] = useState(false);
  const [currentPath, setCurrentPath] = useState(window.location.pathname);
  const [toasts, setToasts] = useState([]);
  const [theme, setTheme] = useState(localStorage.getItem('theme') || 'dark');

  useEffect(() => {
    const handlePopState = () => {
      setCurrentPath(window.location.pathname);
    };
    window.addEventListener('popstate', handlePopState);
    return () => window.removeEventListener('popstate', handlePopState);
  }, []);

  const navigate = (path) => {
    window.history.pushState({}, '', path);
    setCurrentPath(path);
  };

  // Derive activeDevice from path `/device/:serial`
  let activeDevice = null;
  const pathMatch = currentPath.match(/^\/device\/([^/]+)/);
  if (pathMatch) {
    const serial = pathMatch[1];
    const found = devices.find((d) => d.serial === serial);
    if (found) {
      activeDevice = {
        ...found,
        streamPort: found.stream_port || 0,
      };
    } else {
      activeDevice = {
        serial,
        model: 'Loading...',
        manufacturer: '',
        status: 'claimed',
        streamPort: 0,
      };
    }
  }

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme);
    localStorage.setItem('theme', theme);
  }, [theme]);

  const toggleTheme = () => {
    setTheme((t) => (t === 'dark' ? 'light' : 'dark'));
  };

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
        setDevices((prev) =>
          prev.map((d) =>
            d.serial === device.serial
              ? { ...d, status: 'claimed', stream_port: data.port }
              : d
          )
        );
        navigate(`/device/${device.serial}`);
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
          navigate('/');
        }
        fetchDevices();
      }
    } catch (err) {
      showToast(`Release failed: ${err.message}`, 'error');
    }
  };

  return (
    <div className={`layout ${activeDevice ? 'has-active-device' : ''}`}>
      <Header theme={theme} toggleTheme={toggleTheme} />

      <main className="main">
        {activeDevice ? (
          <DevicePage
            device={activeDevice}
            onBack={() => navigate('/')}
            onRelease={() => handleRelease(activeDevice.serial)}
          />
        ) : (
          <>
            <StatsBar devices={devices} />

            <div className="toolbar">
              <h2>Connected Devices</h2>
              <button className="btn btn-ghost" onClick={fetchDevices} disabled={loading}>
                {loading ? <span className="spinner"></span> : 'Refresh'}
              </button>
            </div>

            <div className="device-grid">
              {devices.length === 0 ? (
                <div className="empty">
                  <div className="empty-icon">
                    <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" style={{ margin: '0 auto 12px auto', display: 'block', color: 'var(--text-muted)' }}>
                      <rect x="5" y="2" width="14" height="20" rx="2.5" ry="2.5" />
                      <line x1="12" y1="18" x2="12.01" y2="18" strokeWidth="2.5" />
                    </svg>
                  </div>
                  <h3>No Devices Detected</h3>
                  <p>Ensure adb is running and your Android devices are connected.</p>
                </div>
              ) : (
                devices.map((device) => (
                  <DeviceCard
                    key={device.serial}
                    device={device}
                    onClaim={handleClaim}
                    onViewStream={(serial) => navigate(`/device/${serial}`)}
                    onRelease={handleRelease}
                  />
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

export default App;
