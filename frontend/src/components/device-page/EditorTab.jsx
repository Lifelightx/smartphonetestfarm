import React from 'react';
import {
  Plus, Save, RotateCcw, Trash2, Circle,
  Copy, Download, AlertTriangle
} from 'lucide-react';

const TEMPLATES = {
  settings: {
    name: 'Launch Settings App',
    content: `steps:\n  - launch:\n      package: com.android.settings\n  - wait:\n      class: android.widget.TextView\n      condition: visible\n      timeoutMs: 3000\n  - assert:\n      text: Settings\n      condition: contains\n`
  },
  chrome: {
    name: 'Search Google Chrome',
    content: `steps:\n  - launch:\n      package: com.android.chrome\n  - wait:\n      resourceId: com.android.chrome:id/search_box_text\n      condition: visible\n      timeoutMs: 5000\n  - click:\n      resourceId: com.android.chrome:id/search_box_text\n  - input:\n      text: https://google.com\n`
  },
  calculator: {
    name: 'Simple Calculator Tap',
    content: `steps:\n  - launch:\n      package: com.google.android.calculator\n  - click:\n      resourceId: com.google.android.calculator:id/digit_7\n  - click:\n      resourceId: com.google.android.calculator:id/op_add\n  - click:\n      resourceId: com.google.android.calculator:id/digit_5\n  - click:\n      resourceId: com.google.android.calculator:id/eq\n  - assert:\n      resourceId: com.google.android.calculator:id/result_final\n      condition: equals\n      value: "12"\n`
  }
};

export default function EditorTab({
  selectedScript,
  scriptName,
  scriptContent,
  isRecording,
  isSaving,
  yamlError,
  onNewScript,
  onSave,
  onReset,
  onDelete,
  onToggleRecording,
  onContentChange,
  onNameChange,
}) {
  const handleTemplateChange = (e) => {
    const key = e.target.value;
    if (TEMPLATES[key]) {
      onNameChange(TEMPLATES[key].name);
      onContentChange(TEMPLATES[key].content);
    }
  };

  const handleCopy = () => {
    navigator.clipboard.writeText(scriptContent);
  };

  const handleDownload = () => {
    const blob = new Blob([scriptContent], { type: 'text/yaml' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `${(scriptName || 'script').replace(/\s+/g, '_')}.yaml`;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    URL.revokeObjectURL(url);
  };

  return (
    <div className="editor-form" style={{ width: '100%', height: '100%', display: 'flex', flexDirection: 'column' }}>
      {isRecording && (
        <div className="recording-status-alert">
          <span className="recording-pulse"></span>
          <span className="recording-alert-msg">
            <strong>Recording active:</strong> Interact with the live screen to capture steps in real-time.
          </span>
        </div>
      )}

      {/* Action Buttons */}
      <div className="editor-top-actions">
        <button className="btn btn-primary btn-sm" onClick={onNewScript}>
          <Plus size={14} style={{ marginRight: '4px' }} /> New Script
        </button>
        <button
          className={`btn btn-sm ${isRecording ? 'btn-danger' : 'btn-secondary'}`}
          onClick={onToggleRecording}
          style={isRecording ? { backgroundColor: '#ef4444', color: '#fff' } : {}}
        >
          {isRecording ? (
            <><span className="blink-dot"></span>Stop Recording</>
          ) : (
            <><Circle fill="currentColor" size={12} style={{ marginRight: '6px', color: '#ef4444' }} />Record</>
          )}
        </button>
        <button className="btn btn-sm" onClick={onSave} disabled={isSaving}>
          <Save size={14} style={{ marginRight: '4px' }} /> Save
        </button>
        <button className="btn btn-sm btn-secondary" onClick={onReset} title="Reset editor or revert">
          <RotateCcw size={14} style={{ marginRight: '4px' }} /> Reset
        </button>
        {selectedScript && (
          <button className="btn btn-sm btn-danger" onClick={onDelete} disabled={isSaving}>
            <Trash2 size={14} style={{ marginRight: '4px' }} /> Delete
          </button>
        )}
      </div>

      {/* Name + Template Row */}
      <div className="editor-field-row">
        <input
          type="text"
          className="editor-input"
          placeholder="Script Name"
          value={scriptName}
          onChange={(e) => onNameChange(e.target.value)}
        />
        <select className="editor-select" onChange={handleTemplateChange} defaultValue="">
          <option value="" disabled>Load Template...</option>
          <option value="settings">Settings App</option>
          <option value="chrome">Google Chrome</option>
          <option value="calculator">Calculator</option>
        </select>
      </div>

      {/* YAML Editor */}
      <div className="editor-textarea-container" style={{ flex: 1, minHeight: 0 }}>
        <div className="editor-textarea-header">
          <span className="editor-tech-label">YAML DSL Engine</span>
          <div className="editor-mini-controls">
            {yamlError ? (
              <span className="yaml-badge invalid">
                <AlertTriangle size={12} /> {yamlError}
              </span>
            ) : (
              <span className="yaml-badge valid">✓ Syntactically Valid</span>
            )}
            <button className="btn btn-ghost btn-sm icon-only-btn" title="Copy YAML" onClick={handleCopy}>
              <Copy size={13} />
            </button>
            <button className="btn btn-ghost btn-sm icon-only-btn" title="Download YAML" onClick={handleDownload}>
              <Download size={13} />
            </button>
          </div>
        </div>
        <textarea
          className="editor-textarea"
          placeholder="YAML Steps..."
          value={scriptContent}
          onChange={(e) => onContentChange(e.target.value)}
        />
      </div>
    </div>
  );
}
