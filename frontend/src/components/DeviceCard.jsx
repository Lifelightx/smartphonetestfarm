import React from 'react';

function DeviceCard({ device, onClaim, onViewStream, onRelease }) {
  const getStatusClass = (status) => {
    switch (status?.toLowerCase()) {
      case 'idle': return 'status-idle';
      case 'claimed': return 'status-claimed';
      case 'busy': return 'status-busy';
      default: return 'status-offline';
    }
  };

  const handleCardClick = () => {
    const status = device.status?.toLowerCase();
    if (status === 'idle') {
      onClaim(device);
    } else if (status === 'claimed' || status === 'busy') {
      onViewStream(device.serial);
    }
  };

  return (
    <div
      className={`device-card ${device.status?.toLowerCase()}`}
      onClick={handleCardClick}
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
          <button className="btn btn-primary btn-sm" onClick={() => onClaim(device)}>
            Claim & Control
          </button>
        )}
        {(device.status?.toLowerCase() === 'claimed' || device.status?.toLowerCase() === 'busy') && (
          <>
            <button className="btn btn-ghost btn-sm" onClick={() => onViewStream(device.serial)}>
              View Stream
            </button>
            <button className="btn btn-danger btn-sm" onClick={() => onRelease(device.serial)}>
              Release
            </button>
          </>
        )}
      </div>
    </div>
  );
}

export default DeviceCard;
