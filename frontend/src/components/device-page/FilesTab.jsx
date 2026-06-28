import React from 'react';
import { ChevronLeft, FileText, FolderOpen, HardDrive } from 'lucide-react';

function FilesTab({ device, onFolderClick, onBackClick }) {
  if (!device.file_system?.files) {
    return (
      <div className="feature-placeholder">
        <div className="feature-placeholder-icon color-cyan">
          <HardDrive size={32} />
        </div>
        <h4>File Manager</h4>
        <p>Loading file system data...</p>
      </div>
    );
  }

  return (
    <div className="files-panel">
      <div className="files-header">
        {device.file_system.root !== '/storage/emulated/0' && (
          <button className="files-back-btn" onClick={() => onBackClick(device.file_system.root)}>
            <ChevronLeft size={16} />
            <span>Back</span>
          </button>
        )}
        <div className="files-path">
          <FolderOpen size={16} />
          <span>{device.file_system.root}</span>
        </div>
      </div>

      <div className="file-list">
        {device.file_system.files.map((file, index) => (
          <button
            key={`${file.path}-${index}`}
            className={`file-item ${file.isDirectory ? 'clickable' : ''}`}
            onClick={() => file.isDirectory && onFolderClick(file.path)}
            type="button"
          >
            <div className={`file-icon ${file.isDirectory ? 'folder' : 'document'}`}>
              {file.isDirectory ? <FolderOpen size={18} /> : <FileText size={18} />}
            </div>
            <div className="file-copy">
              <div className="file-name">{file.name}</div>
              <div className="file-meta">
                {file.isDirectory ? 'Directory' : `${(file.size / 1024).toFixed(1)} KB`}
              </div>
            </div>
          </button>
        ))}
      </div>
    </div>
  );
}

export default FilesTab;
