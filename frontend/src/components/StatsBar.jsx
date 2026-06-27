import React from 'react';
import './StatsBar.css';

function StatsBar({ devices }) {
  const total = devices.length;
  const available = devices.filter((d) => d.status?.toLowerCase() === 'idle').length;
  const inUse = devices.filter((d) => d.status?.toLowerCase() === 'claimed' || d.status?.toLowerCase() === 'busy').length;

  return (
    <div className="stats-bar">
      <div className="stat-card">
        <div className="stat-value">{total}</div>
        <div className="stat-label">Total Devices</div>
      </div>
      <div className="stat-card">
        <div className="stat-value">{available}</div>
        <div className="stat-label">Available</div>
      </div>
      <div className="stat-card">
        <div className="stat-value">{inUse}</div>
        <div className="stat-label">In Use</div>
      </div>
    </div>
  );
}

export default StatsBar;
