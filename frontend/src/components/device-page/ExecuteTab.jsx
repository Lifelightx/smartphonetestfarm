import React, { useState, useEffect } from 'react';
import {
  Play, CheckCircle2, XCircle, AlertCircle, Clock, Calendar, RefreshCw, Trash2, Edit2, Settings
} from 'lucide-react';

export default function ExecuteTab({
  scripts,
  selectedScript,
  onSelectScript,
  onEditScript,
  onDeleteScript,
  currentDevice,
  coordinatorApi,
  fetchReports
}) {
  const [iterations, setIterations] = useState(1);
  const [scheduledTime, setScheduledTime] = useState('');
  const [showConfig, setShowConfig] = useState(false);

  // Execution status
  const [isRunning, setIsRunning] = useState(false);
  const [countdownText, setCountdownText] = useState('');
  const [runError, setRunError] = useState('');
  const [runResults, setRunResults] = useState(null); // Map of serial -> results
  const [currentIteration, setCurrentIteration] = useState(1);
  const [allIterationResults, setAllIterationResults] = useState([]); // List of runResults maps
  const [selectedIterationIndex, setSelectedIterationIndex] = useState(0);

  // Handle scheduled countdown
  useEffect(() => {
    if (!scheduledTime) {
      setCountdownText('');
      return;
    }

    const interval = setInterval(() => {
      const target = new Date(scheduledTime).getTime();
      const diff = target - Date.now();
      if (diff <= 0) {
        setCountdownText('Executing now...');
        clearInterval(interval);
      } else {
        const secs = Math.floor(diff / 1000) % 60;
        const mins = Math.floor(diff / 60000) % 60;
        const hours = Math.floor(diff / 3600000);
        setCountdownText(`Scheduled in ${hours}h ${mins}m ${secs}s`);
      }
    }, 1000);

    return () => clearInterval(interval);
  }, [scheduledTime]);

  const executeSingleRun = async (scriptId) => {
    const res = await fetch(`${coordinatorApi}/api/v1/automation/run`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        script_id: scriptId,
        device_serials: [currentDevice.serial]
      })
    });

    if (!res.ok) {
      const text = await res.text();
      throw new Error(text || 'Failed to trigger run request');
    }

    const data = await res.json();
    const resultsMap = {};

    for (const runRes of (data.results || [])) {
      if (runRes.success && runRes.report_id) {
        try {
          const repRes = await fetch(`${coordinatorApi}/api/v1/automation/reports/${runRes.report_id}`);
          if (repRes.ok) {
            const repData = await repRes.json();
            resultsMap[runRes.serial] = {
              success: true,
              durationMs: runRes.duration_ms,
              report: repData
            };
          } else {
            resultsMap[runRes.serial] = {
              success: false,
              error: `Failed to fetch report (HTTP ${repRes.status})`
            };
          }
        } catch (err) {
          resultsMap[runRes.serial] = {
            success: false,
            error: `Network error loading report: ${err.message}`
          };
        }
      } else {
        resultsMap[runRes.serial] = {
          success: false,
          error: runRes.error || 'Execution failed'
        };
      }
    }
    return resultsMap;
  };

  const handleStartRun = async () => {
    if (!selectedScript) {
      alert('Please select a script to execute.');
      return;
    }

    const runFunc = async () => {
      setIsRunning(true);
      setRunError('');
      setRunResults(null);
      setAllIterationResults([]);
      setSelectedIterationIndex(0);

      const numIterations = Math.max(1, parseInt(iterations, 10) || 1);
      const tempResults = [];

      try {
        for (let i = 1; i <= numIterations; i++) {
          setCurrentIteration(i);
          const resultsMap = await executeSingleRun(selectedScript.id);
          tempResults.push(resultsMap);
          setAllIterationResults([...tempResults]);
          setRunResults(resultsMap); // show last execution immediately
          setSelectedIterationIndex(i - 1);
        }
      } catch (err) {
        setRunError(err.message);
      } finally {
        setIsRunning(false);
        setScheduledTime(''); // Reset schedule once run finishes
        if (fetchReports) fetchReports();
      }
    };

    if (scheduledTime) {
      const targetTime = new Date(scheduledTime).getTime();
      const delay = targetTime - Date.now();
      if (delay > 0) {
        setIsRunning(true);
        setRunError('');
        setRunResults(null);
        setTimeout(() => {
          runFunc();
        }, delay);
      } else {
        // Scheduled time is in the past, run immediately
        runFunc();
      }
    } else {
      runFunc();
    }
  };

  const currentResults = allIterationResults[selectedIterationIndex] || runResults;
  const activeRes = currentResults ? currentResults[currentDevice.serial] : null;

  return (
    <div className="automation-split-pane">
      {/* Sidebar: Script List */}
      <div className="scripts-sidebar">
        <span className="scripts-sidebar-title">Saved Scripts</span>
        <div className="scripts-list">
          {scripts.length === 0 ? (
            <div className="no-scripts-placeholder">No scripts saved yet.</div>
          ) : (
            scripts.map(s => (
              <button
                key={s.id}
                className={`script-item-btn ${selectedScript?.id === s.id ? 'selected' : ''}`}
                onClick={() => {
                  onSelectScript(s);
                  setRunResults(null);
                  setAllIterationResults([]);
                  setRunError('');
                }}
              >
                <span className="script-name-txt">{s.name}</span>
                <span className="script-item-meta">{new Date(s.created_at).toLocaleDateString()}</span>
              </button>
            ))
          )}
        </div>
      </div>

      {/* Main Panel */}
      <div className="editor-form" style={{ display: 'flex', flexDirection: 'column', height: '100%', overflowY: 'auto' }}>
        {!selectedScript ? (
          <div style={{ color: 'var(--text-muted)', textAlign: 'center', padding: '48px', flex: 1, display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: '8px' }}>
            <AlertCircle size={24} style={{ opacity: 0.5 }} />
            <span>Select a saved script from the left panel to execute.</span>
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '14px', flex: 1 }}>
            {/* Header / Title */}
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', borderBottom: '1px solid var(--border)', paddingBottom: '8px' }}>
              <span className="section-header-title" style={{ fontSize: '15px', fontWeight: 'bold' }}>{selectedScript.name}</span>
              <div style={{ display: 'flex', gap: '6px' }}>
                <button
                  className="btn btn-sm btn-primary"
                  onClick={handleStartRun}
                  disabled={isRunning}
                  style={{ display: 'flex', alignItems: 'center', gap: '4px' }}
                >
                  <Play size={14} /> Run
                </button>
                <button
                  className={`btn btn-sm ${showConfig ? 'btn-primary' : 'btn-secondary'}`}
                  onClick={() => setShowConfig(!showConfig)}
                  style={{ display: 'flex', alignItems: 'center', gap: '4px' }}
                >
                  <Settings size={14} /> Configure
                </button>
                <button
                  className="btn btn-sm btn-secondary"
                  onClick={() => onEditScript(selectedScript)}
                  style={{ display: 'flex', alignItems: 'center', gap: '4px' }}
                >
                  <Edit2 size={14} /> Edit
                </button>
                <button
                  className="btn btn-sm btn-danger"
                  onClick={onDeleteScript}
                  style={{ display: 'flex', alignItems: 'center', gap: '4px' }}
                >
                  <Trash2 size={14} /> Delete
                </button>
              </div>
            </div>

            {/* Collapsible Configure Section */}
            {showConfig && (
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '16px', background: 'var(--surface2)', padding: '12px', borderRadius: 'var(--radius-sm)', border: '1px solid var(--border)' }}>
                <div>
                  <label className="inspector-label" style={{ fontSize: '11px', marginBottom: '4px', display: 'block' }}>Iterations (Loop Count)</label>
                  <input
                    type="number"
                    className="editor-input"
                    style={{ width: '100%' }}
                    min="1"
                    max="50"
                    value={iterations}
                    onChange={(e) => setIterations(Math.max(1, parseInt(e.target.value, 10) || 1))}
                  />
                </div>
                <div>
                  <label className="inspector-label" style={{ fontSize: '11px', marginBottom: '4px', display: 'block' }}>Schedule Time (Optional)</label>
                  <input
                    type="datetime-local"
                    className="editor-input"
                    style={{ width: '100%' }}
                    value={scheduledTime}
                    onChange={(e) => setScheduledTime(e.target.value)}
                  />
                </div>
              </div>
            )}

            {/* Countdown / Scheduling message */}
            {countdownText && (
              <div className="recording-status-alert" style={{ background: 'var(--surface2)', borderColor: 'var(--accent)' }}>
                <Clock size={16} className="color-accent" style={{ marginRight: '8px' }} />
                <span className="recording-alert-msg" style={{ color: 'var(--text)' }}>
                  <strong>Scheduled Execution:</strong> {countdownText}
                </span>
              </div>
            )}

            {/* Execution Loader */}
            {isRunning && !runResults && !countdownText && (
              <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', padding: '48px', gap: '16px', flex: 1 }}>
                <span className="spinner" style={{ width: '28px', height: '28px' }}></span>
                <span style={{ color: 'var(--text-muted)', fontSize: '13px' }}>
                  Executing on device... {iterations > 1 ? `(Iteration ${currentIteration}/${iterations})` : ''}
                </span>
              </div>
            )}

            {/* Error Message */}
            {runError && (
              <div className="execution-log-container">
                <div className="status-banner failed">
                  <div className="status-title-row">
                    <AlertCircle size={16} />
                    <span>Execution Failed</span>
                  </div>
                </div>
                <div className="step-error-message" style={{ margin: '16px 0' }}>
                  {runError}
                </div>
              </div>
            )}

            {/* Results Logs & Steps Timeline */}
            {currentResults && (
              <div style={{ display: 'flex', flexDirection: 'column', gap: '12px', flex: 1 }}>
                {/* Iteration Selector */}
                {allIterationResults.length > 1 && (
                  <div style={{ display: 'flex', gap: '6px', overflowX: 'auto', paddingBottom: '6px', borderBottom: '1px solid var(--border)' }}>
                    {allIterationResults.map((_, idx) => (
                      <button
                        key={idx}
                        className={`subtab-btn ${selectedIterationIndex === idx ? 'active' : ''}`}
                        onClick={() => setSelectedIterationIndex(idx)}
                        style={{ fontSize: '11px', padding: '4px 8px' }}
                      >
                        Iteration {idx + 1}
                      </button>
                    ))}
                  </div>
                )}

                {activeRes ? (
                  <div className="execution-log-container" style={{ overflowY: 'auto' }}>
                    {activeRes.success ? (
                      <div className="status-banner passed">
                        <div className="status-title-row">
                          <CheckCircle2 size={16} />
                          <span>Execution Passed</span>
                        </div>
                        <span className="status-duration">Duration: {activeRes.report?.results?.durationMs || activeRes.report?.durationMs || activeRes.durationMs}ms</span>
                      </div>
                    ) : (
                      <div className="status-banner failed">
                        <div className="status-title-row">
                          <XCircle size={16} />
                          <span>Execution Failed</span>
                        </div>
                        {activeRes.error && <span className="status-duration">{activeRes.error}</span>}
                      </div>
                    )}

                    {activeRes.report?.results?.results && (
                      <div className="timeline" style={{ marginTop: '14px' }}>
                        {activeRes.report.results.results.map((step, idx) => (
                          <div key={idx} className={`timeline-step ${step.success ? 'passed' : 'failed'}`}>
                            <div className="step-icon-col">{idx + 1}</div>
                            <div className="step-details-col">
                              <div className="step-header">
                                <span className="step-title">{step.action.toUpperCase()} Step</span>
                                <span className="step-duration-label">{step.durationMs}ms</span>
                              </div>
                              {!step.success && step.error && (
                                <div className="step-error-message">{step.error}</div>
                              )}
                              {step.screenshot && (
                                <div className="step-screenshot-preview">
                                  <img src={`data:image/jpeg;base64,${step.screenshot}`} alt="Step Screenshot" />
                                </div>
                              )}
                            </div>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                ) : (
                  !isRunning && (
                    <div style={{ color: 'var(--text-muted)', fontSize: '12px', textAlign: 'center', padding: '24px' }}>
                      No active execution results. Click Run to begin.
                    </div>
                  )
                )}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
