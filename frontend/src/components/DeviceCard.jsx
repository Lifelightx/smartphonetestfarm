import React from 'react';
import './DeviceCard.css';

const PhoneIcon = () => (
  <svg className="device-svg-icon" width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.25" strokeLinecap="round" strokeLinejoin="round">
    <rect x="5" y="2" width="14" height="20" rx="2.5" ry="2.5" />
    <path d="M12 18h.01" strokeWidth="2" strokeLinecap="round" />
  </svg>
);

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
      className={`device-card-minimal ${device.status?.toLowerCase()}`}
      onClick={handleCardClick}
    >
      <div className="card-top-bar">
        <span className="card-serial">{device.serial}</span>
        <span className={`status-pill-minimal ${getStatusClass(device.status)}`}>
          {device.status}
        </span>
      </div>

      <div className="device-visual">
        <PhoneIcon />
      </div>

      <div className="device-info">
        <h3 className="device-model">{device.manufacturer} {device.model}</h3>
      </div>

      {device.status?.toLowerCase() === 'claimed' && (
        <button
          className="btn-card-release"
          onClick={(e) => {
            e.stopPropagation();
            onRelease(device.serial);
          }}
        >
          Release
        </button>
      )}
    </div>
  );
}

export default DeviceCard;
