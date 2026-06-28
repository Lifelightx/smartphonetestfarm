import React from 'react';
import {
  BatteryFull,
  Cpu,
  Globe,
  HardDrive,
  MemoryStick,
  MonitorSmartphone,
  RadioTower,
  Router,
  ShieldCheck,
  Smartphone,
  Wifi,
} from 'lucide-react';

const hardwareItems = [
  {
    key: 'serial',
    label: 'Serial',
    getValue: (device) => device.serial,
    icon: ShieldCheck,
    colorClass: 'color-emerald',
    mono: true,
  },
  {
    key: 'os',
    label: 'OS Version',
    getValue: (device) => `Android ${device.android} (SDK ${device.sdk})`,
    icon: Smartphone,
    colorClass: 'color-blue',
  },
  {
    key: 'ram',
    label: 'Total RAM',
    getValue: (device) => (device.ram_mb ? `${(device.ram_mb / 1024).toFixed(1)} GB` : 'Unknown'),
    icon: MemoryStick,
    colorClass: 'color-orange',
  },
  {
    key: 'storage',
    label: 'Total Storage',
    getValue: (device) => (device.storage_mb ? `${(device.storage_mb / 1024).toFixed(1)} GB` : 'Unknown'),
    icon: HardDrive,
    colorClass: 'color-cyan',
  },
];

const liveItems = [
  {
    key: 'battery',
    label: 'Battery',
    getValue: (device) => `${device.battery ?? 'Unknown'}%`,
    icon: BatteryFull,
    colorClass: 'color-lime',
  },
  {
    key: 'wifi',
    label: 'WiFi SSID',
    getValue: (device) => device.wifi_ssid || 'Not connected',
    icon: Wifi,
    colorClass: 'color-sky',
  },
  {
    key: 'ip',
    label: 'Provider IP',
    getValue: (device) => device.ip || 'Unknown',
    icon: RadioTower,
    colorClass: 'color-rose',
  },
  {
    key: 'host',
    label: 'Provider Host',
    getValue: (device) => device.provider_id || 'Unknown',
    icon: Router,
    colorClass: 'color-violet',
  },
  {
    key: 'stream',
    label: 'Stream Port',
    getValue: (_, streamPort) => streamPort || 'Unavailable',
    icon: MonitorSmartphone,
    colorClass: 'color-amber',
  },
];

function SpecCard({ label, value, icon: Icon, colorClass, mono = false }) {
  return (
    <div className="spec-card">
      <div className={`spec-card-icon ${colorClass}`}>
        <Icon size={18} />
      </div>
      <div className="spec-card-copy">
        <span className="spec-card-label">{label}</span>
        <span className={`spec-card-value ${mono ? 'mono' : ''}`}>{value}</span>
      </div>
    </div>
  );
}

function InfoTab({ device, streamPort, onRelease }) {
  return (
    <div className="info-layout">
      <section className="details-card hero-card">
        <div className="hero-card-header">
          <div className="hero-card-title">
            <div className="hero-icon color-blue">
              <Cpu size={22} />
            </div>
            <div>
              <h3>{device.manufacturer} {device.model}</h3>
              <p>Device details grouped into cleaner cards for faster scanning.</p>
            </div>
          </div>
        </div>

        <div className="info-section">
          <div className="section-heading">Hardware Specifications</div>
          <div className="spec-grid">
            {hardwareItems.map((item) => (
              <SpecCard
                key={item.key}
                label={item.label}
                value={item.getValue(device)}
                icon={item.icon}
                colorClass={item.colorClass}
                mono={item.mono}
              />
            ))}
          </div>
        </div>

        <div className="info-section">
          <div className="section-heading">Live Status</div>
          <div className="spec-grid">
            {liveItems.map((item) => (
              <SpecCard
                key={item.key}
                label={item.label}
                value={item.getValue(device, streamPort)}
                icon={item.icon}
                colorClass={item.colorClass}
              />
            ))}
          </div>
        </div>
      </section>

      {device.installed_browsers?.length > 0 && (
        <section className="details-card">
          <div className="info-card-header">
            <div className="hero-icon color-cyan">
              <Globe size={18} />
            </div>
            <div>
              <h4>Installed Browsers</h4>
              <p>Available packages detected on this device.</p>
            </div>
          </div>

          <div className="browser-list">
            {device.installed_browsers.map((browser, index) => (
              <div key={`${browser}-${index}`} className="browser-pill">
                <div className="browser-pill-icon color-cyan">
                  <Globe size={15} />
                </div>
                <span>{browser}</span>
              </div>
            ))}
          </div>
        </section>
      )}

      <button className="btn btn-danger info-release-btn" onClick={onRelease}>
        Release Device
      </button>
    </div>
  );
}

export default InfoTab;
