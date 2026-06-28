import React from 'react';
import { ChevronLeft, Cpu, FileText, FolderOpen, LayoutGrid, MonitorPlay, PlaySquare, WandSparkles } from 'lucide-react';

const tabs = [
  { id: 'dashboard', label: 'Dashboard', icon: LayoutGrid },
  { id: 'automation', label: 'Automation', icon: PlaySquare },
  { id: 'media', label: 'Media', icon: MonitorPlay },
  { id: 'logs', label: 'Logs & PT', icon: FileText },
  { id: 'files', label: 'Files', icon: FolderOpen },
  { id: 'info', label: 'Info', icon: Cpu },
];

function DeviceTabsHeader({ deviceModel, activeTab, onTabChange, onBack }) {
  return (
    <div className="tabs-shell">
      <button className="tabs-back-btn" onClick={onBack} title="Back to dashboard">
        <ChevronLeft size={18} />
      </button>

      <div className="tabs-scroll">
        <div className="device-chip">{deviceModel || 'Device'}</div>
        {tabs.map(({ id, label, icon: Icon }) => (
          <button
            key={id}
            className={`tab-btn ${activeTab === id ? 'active' : ''}`}
            onClick={() => onTabChange(id)}
          >
            <Icon size={16} />
            <span>{label}</span>
          </button>
        ))}
      </div>
    </div>
  );
}

export default DeviceTabsHeader;
