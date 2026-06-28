import React, { useState } from 'react';
import { Search } from 'lucide-react';
import './DeviceDetailsTable.css';

function DeviceDetailsTable({ devices, onClaim, onViewStream, onRelease }) {
  const [searchQuery, setSearchQuery] = useState('');

  const filteredDevices = devices.filter(d => 
    d.serial?.toLowerCase().includes(searchQuery.toLowerCase()) ||
    d.model?.toLowerCase().includes(searchQuery.toLowerCase()) ||
    d.manufacturer?.toLowerCase().includes(searchQuery.toLowerCase()) ||
    d.provider_id?.toLowerCase().includes(searchQuery.toLowerCase()) ||
    d.android?.toLowerCase().includes(searchQuery.toLowerCase())
  );

  return (
    <div className="details-container">
      <div className="details-toolbar">
        <div className="search-box">
          <Search size={18} />
          <input 
            type="text" 
            placeholder="Search devices..." 
            value={searchQuery} 
            onChange={(e) => setSearchQuery(e.target.value)}
          />
        </div>
      </div>
      <div className="table-wrapper">
        <table className="details-table">
          <thead>
            <tr>
              <th>Action</th>
              <th>Serial</th>
              <th>Android</th>
              <th>Battery</th>
              <th>Provider IP</th>
              <th>Manufacturer</th>
              <th>Status</th>
            </tr>
          </thead>
          <tbody>
            {filteredDevices.length === 0 ? (
              <tr>
                <td colSpan="7" className="empty-cell">No devices matching search</td>
              </tr>
            ) : (
              filteredDevices.map(device => (
                <tr key={device.serial}>
                  <td>
                    {device.status === 'idle' ? (
                      <button className="btn btn-sm" onClick={() => onClaim(device)}>Claim</button>
                    ) : device.status === 'claimed' ? (
                      <button className="btn btn-sm btn-ghost" onClick={() => onRelease(device.serial)}>Release</button>
                    ) : (
                      <button className="btn btn-sm btn-ghost" disabled>None</button>
                    )}
                  </td>
                  <td>{device.serial}</td>
                  <td>{device.android}</td>
                  <td>{device.battery}%</td>
                  <td>{device.provider_id}</td>
                  <td>{device.manufacturer}</td>
                  <td>
                    <span className={`status-pill-minimal status-${device.status?.toLowerCase()}`}>
                      {device.status}
                    </span>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}

export default DeviceDetailsTable;
