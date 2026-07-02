import React, { useState, useEffect } from 'react';
import {
  Code,
  Play,
  Save,
  FileText,
  CheckCircle2,
  XCircle,
  AlertCircle,
  Clock,
  Plus,
  History,
  Sparkles,
  ChevronRight,
  Copy,
  Download,
  Layers,
  AlertTriangle,
  RotateCcw,
  Trash2,
  Circle
} from 'lucide-react';
import './AutomationTab.css';
import EditorTab from './EditorTab';
import ExecuteTab from './ExecuteTab';

const TEMPLATES = {
  settings: {
    name: 'Launch Settings App',
    content: `steps:
  - launch:
      package: com.android.settings
  - wait:
      class: android.widget.TextView
      condition: visible
      timeoutMs: 3000
  - assert:
      text: Settings
      condition: contains
`
  },
  chrome: {
    name: 'Search Google Chrome',
    content: `steps:
  - launch:
      package: com.android.chrome
  - wait:
      resourceId: com.android.chrome:id/search_box_text
      condition: visible
      timeoutMs: 5000
  - click:
      resourceId: com.android.chrome:id/search_box_text
  - input:
      text: https://google.com
  - click:
      x: 0.9
      y: 0.9
`
  },
  calculator: {
    name: 'Simple Calculator Tap',
    content: `steps:
  - launch:
      package: com.google.android.calculator
  - click:
      resourceId: com.google.android.calculator:id/digit_7
  - click:
      resourceId: com.google.android.calculator:id/op_add
  - click:
      resourceId: com.google.android.calculator:id/digit_5
  - click:
      resourceId: com.google.android.calculator:id/eq
  - assert:
      resourceId: com.google.android.calculator:id/result_final
      condition: equals
      value: "12"
`
  }
};

const parseBounds = (boundsStr) => {
  if (!boundsStr) return null;
  const match = boundsStr.match(/^\[(-?\d+),(-?\d+)\]\[(-?\d+),(-?\d+)\]$/);
  if (!match) return null;
  return {
    left: parseInt(match[1], 10),
    top: parseInt(match[2], 10),
    right: parseInt(match[3], 10),
    bottom: parseInt(match[4], 10)
  };
};

const findNodeAtCoords = (node, x, y) => {
  const matches = [];
  
  const collectMatches = (currNode) => {
    if (!currNode || currNode.nodeType !== 1) return;
    
    const boundsStr = currNode.getAttribute('bounds');
    const bounds = parseBounds(boundsStr);
    
    if (bounds) {
      if (x >= bounds.left && x <= bounds.right && y >= bounds.top && y <= bounds.bottom) {
        matches.push({ node: currNode, bounds });
      } else {
        return; // If parent bounds don't match, children won't either
      }
    }
    
    const children = Array.from(currNode.childNodes).filter(c => c.nodeType === 1);
    for (const child of children) {
      collectMatches(child);
    }
  };
  
  collectMatches(node);
  
  if (matches.length === 0) return null;
  
  let bestMatch = null;
  let minArea = Infinity;
  
  for (const m of matches) {
    const width = m.bounds.right - m.bounds.left;
    const height = m.bounds.bottom - m.bounds.top;
    const area = width * height;
    
    if (area >= 0 && area <= minArea) {
      minArea = area;
      bestMatch = m.node;
    }
  }
  
  return bestMatch;
};

const isAncestorOf = (parent, child) => {
  let curr = child;
  while (curr) {
    if (curr === parent) return true;
    curr = curr.parentNode;
  }
  return false;
};

function AutomationTab({ device, coordinatorApi, deviceWsRef, onWSMessageRef, canvasClickHandlerRef }) {
  const [activeSubtab, setActiveSubtab] = useState('editor');
  const [scripts, setScripts] = useState([]);
  const [selectedScript, setSelectedScript] = useState(null);
  const [scriptName, setScriptName] = useState('');
  const [scriptContent, setScriptContent] = useState(TEMPLATES.settings.content);

  const [isRecording, setIsRecording] = useState(false);

  // Parallel Replay & Selection state
  const [availableDevices, setAvailableDevices] = useState([]);
  const [selectedSerials, setSelectedSerials] = useState([device.serial]);

  // Multi-device execution results
  const [runResults, setRunResults] = useState(null);
  const [activeResultSerial, setActiveResultSerial] = useState(device.serial);
  const [isRunning, setIsRunning] = useState(false);
  const [runError, setRunError] = useState('');

  // Syntax Validation State
  const [yamlError, setYamlError] = useState('');

  // Step Inspector State
  const [inspectorAction, setInspectorAction] = useState('click');
  const [inspectorResourceId, setInspectorResourceId] = useState('');
  const [inspectorText, setInspectorText] = useState('');
  const [inspectorClass, setInspectorClass] = useState('');
  const [inspectorXPath, setInspectorXPath] = useState('');
  const [inspectorValue, setInspectorValue] = useState('');
  const [inspectorTimeout, setInspectorTimeout] = useState('3000');
  const [inspectorAssertCond, setInspectorAssertCond] = useState('contains');
  const [inspectorWaitCond, setInspectorWaitCond] = useState('visible');

  // Step Builder extensions (wait per step, assert per step, delay after step)
  const [inspectorAddWait, setInspectorAddWait] = useState(false);
  const [inspectorStepWaitCond, setInspectorStepWaitCond] = useState('visible');
  const [inspectorStepWaitTimeout, setInspectorStepWaitTimeout] = useState('3000');
  const [inspectorAddAssert, setInspectorAddAssert] = useState(false);
  const [inspectorStepAssertCond, setInspectorStepAssertCond] = useState('contains');
  const [inspectorStepAssertValue, setInspectorStepAssertValue] = useState('');
  const [inspectorStepAssertTimeout, setInspectorStepAssertTimeout] = useState('3000');
  const [inspectorStepDelay, setInspectorStepDelay] = useState('');

  // UI Inspector & Tree State
  const [uiXml, setUiXml] = useState('');
  const [parsedTree, setParsedTree] = useState(null);
  const [selectedTreeNode, setSelectedTreeNode] = useState(null);
  const [isRefreshingTree, setIsRefreshingTree] = useState(false);
  const [treeViewMode, setTreeViewMode] = useState('full');
  const [inspectMode, setInspectMode] = useState(true);

  const [isSaving, setIsSaving] = useState(false);
  const [reports, setReports] = useState([]);
  const [selectedReport, setSelectedReport] = useState(null);
  const [detailedReportLoading, setDetailedReportLoading] = useState(false);

  useEffect(() => {
    if (onWSMessageRef) {
      onWSMessageRef.current = (dataStr) => {
        try {
          const msg = JSON.parse(dataStr);
          if (msg.type === 'RECORDING_STATUS') {
            setIsRecording(msg.status === 'recording');
          } else if (msg.type === 'RECORDING_COMPLETE') {
            setIsRecording(false);
            if (msg.yaml) {
              setScriptContent(msg.yaml);
              alert('Recording finished! Script generated.');
            }
          } else if (msg.type === 'RECORDING_ERROR') {
            setIsRecording(false);
            alert(`Recording error: ${msg.error}`);
          } else if (msg.type === 'UI_DUMP') {
            setIsRefreshingTree(false);
            setUiXml(msg.xml);
            parseAndSetTree(msg.xml);
          } else if (msg.type === 'UI_DUMP_ERROR') {
            setIsRefreshingTree(false);
            alert(`UI Dump failed: ${msg.error}`);
          } else if (msg.type === 'FOREGROUND_PACKAGE') {
            if (msg.package) {
              setInspectorValue(msg.package);
            }
          }
        } catch (e) {
          console.error('Failed to parse WS text message:', e);
        }
      };
    }
    return () => {
      if (onWSMessageRef) {
        onWSMessageRef.current = null;
      }
    };
  }, [onWSMessageRef]);

  useEffect(() => {
    fetchScripts();
    fetchReports();
    fetchDevices();
  }, [device.serial]);

  useEffect(() => {
    if (activeSubtab === 'inspector' && inspectorAction === 'launch') {
      const ws = deviceWsRef?.current;
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'GET_FOREGROUND_PACKAGE' }));
      }
    }
  }, [inspectorAction, activeSubtab, deviceWsRef]);

  useEffect(() => {
    if (canvasClickHandlerRef) {
      if (activeSubtab === 'inspector' && inspectMode) {
        canvasClickHandlerRef.current = (normX, normY) => {
          if (!parsedTree) return;
          
          let screenWidth = 1080;
          let screenHeight = 2400;
          const rootBoundsStr = parsedTree.getAttribute('bounds');
          const rootBounds = parseBounds(rootBoundsStr);
          if (rootBounds) {
            screenWidth = rootBounds.right;
            screenHeight = rootBounds.bottom;
          } else {
            const w = parsedTree.getAttribute('width');
            const h = parsedTree.getAttribute('height');
            if (w && h) {
              screenWidth = parseInt(w, 10);
              screenHeight = parseInt(h, 10);
            }
          }
          
          const clickX = normX * screenWidth;
          const clickY = normY * screenHeight;
          
          const matchedNode = findNodeAtCoords(parsedTree, clickX, clickY);
          if (matchedNode) {
            handleSelectNode(matchedNode);
            setTreeViewMode('focused');
          }
        };
      } else {
        canvasClickHandlerRef.current = null;
      }
    }
    return () => {
      if (canvasClickHandlerRef) {
        canvasClickHandlerRef.current = null;
      }
    };
  }, [canvasClickHandlerRef, activeSubtab, parsedTree, inspectMode]);

  // Update screen highlight overlay based on selected node
  useEffect(() => {
    const overlay = document.getElementById('highlight-overlay');
    if (!overlay) return;

    if (!selectedTreeNode || !parsedTree || activeSubtab !== 'inspector' || !inspectMode) {
      overlay.style.display = 'none';
      return;
    }

    const boundsStr = selectedTreeNode.getAttribute('bounds');
    const bounds = parseBounds(boundsStr);
    if (!bounds) {
      overlay.style.display = 'none';
      return;
    }

    let screenWidth = 1080;
    let screenHeight = 2400;
    const rootBoundsStr = parsedTree.getAttribute('bounds');
    const rootBounds = parseBounds(rootBoundsStr);
    if (rootBounds) {
      screenWidth = rootBounds.right;
      screenHeight = rootBounds.bottom;
    } else {
      const w = parsedTree.getAttribute('width');
      const h = parsedTree.getAttribute('height');
      if (w && h) {
        screenWidth = parseInt(w, 10);
        screenHeight = parseInt(h, 10);
      }
    }

    const leftPct = (bounds.left / screenWidth) * 100;
    const topPct = (bounds.top / screenHeight) * 100;
    const widthPct = ((bounds.right - bounds.left) / screenWidth) * 100;
    const heightPct = ((bounds.bottom - bounds.top) / screenHeight) * 100;

    overlay.style.display = 'block';
    overlay.style.left = `${leftPct}%`;
    overlay.style.top = `${topPct}%`;
    overlay.style.width = `${widthPct}%`;
    overlay.style.height = `${heightPct}%`;
    
    return () => {
      overlay.style.display = 'none';
    };
  }, [selectedTreeNode, parsedTree, activeSubtab, inspectMode]);

  // Real-time simple YAML validation
  useEffect(() => {
    if (!scriptContent.trim()) {
      setYamlError('Script is empty');
      return;
    }
    if (!scriptContent.includes('steps:')) {
      setYamlError('Missing root element "steps:"');
      return;
    }
    setYamlError('');
  }, [scriptContent]);

  const toggleRecording = () => {
    const ws = deviceWsRef?.current;
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      alert('Device connection not active. Cannot start/stop recording.');
      return;
    }
    if (isRecording) {
      ws.send(JSON.stringify({ type: 'STOP_RECORDING' }));
    } else {
      ws.send(JSON.stringify({ type: 'START_RECORDING' }));
      setIsRecording(true);
    }
  };

  const fetchScripts = async () => {
    try {
      const res = await fetch(`${coordinatorApi}/api/v1/automation/scripts`);
      if (res.ok) {
        const data = await res.json();
        setScripts(data);
      }
    } catch (err) {
      console.error('Failed to fetch automation scripts:', err);
    }
  };

  const fetchReports = async () => {
    try {
      const res = await fetch(`${coordinatorApi}/api/v1/automation/reports`);
      if (res.ok) {
        const data = await res.json();
        // Filter reports specifically for this device
        const filtered = data.filter(r => r.serial === device.serial);
        setReports(filtered);
      }
    } catch (err) {
      console.error('Failed to fetch execution reports:', err);
    }
  };

  const fetchDevices = async () => {
    try {
      const res = await fetch(`${coordinatorApi}/api/v1/devices`);
      if (res.ok) {
        const data = await res.json();
        // List claimed or idle devices first, sorting the current device to the top
        const sorted = (data || [])
          .filter(d => d.status !== 'offline')
          .sort((a, b) => {
            if (a.serial === device.serial) return -1;
            if (b.serial === device.serial) return 1;
            return a.serial.localeCompare(b.serial);
          });
        setAvailableDevices(sorted);
      }
    } catch (err) {
      console.error('Failed to fetch online devices:', err);
    }
  };

  const handleSelectScript = (script) => {
    setSelectedScript(script);
    setScriptName(script.name);
    setScriptContent(script.content);
  };

  const handleNewScript = () => {
    const randNum = Math.floor(1000 + Math.random() * 99000);
    setSelectedScript(null);
    setScriptName(`New Automation Script ${randNum}`);
    setScriptContent(TEMPLATES.settings.content);
  };

  const handleResetScript = () => {
    if (confirm('Are you sure you want to reset the editor? Unsaved changes will be lost.')) {
      if (selectedScript) {
        setScriptName(selectedScript.name);
        setScriptContent(selectedScript.content);
      } else {
        const randNum = Math.floor(1000 + Math.random() * 99000);
        setScriptName(`New Automation Script ${randNum}`);
        setScriptContent(TEMPLATES.settings.content);
      }
    }
  };

  const handleDeleteScript = async () => {
    if (!selectedScript) return;
    if (!confirm(`Are you sure you want to delete the script "${selectedScript.name}"?`)) {
      return;
    }
    try {
      const res = await fetch(`${coordinatorApi}/api/v1/automation/scripts/${selectedScript.id}`, {
        method: 'DELETE'
      });
      if (res.ok) {
        alert('Script deleted successfully');
        setSelectedScript(null);
        const randNum = Math.floor(1000 + Math.random() * 99000);
        setScriptName(`New Automation Script ${randNum}`);
        setScriptContent(TEMPLATES.settings.content);
        fetchScripts();
      } else {
        const txt = await res.text();
        alert(`Failed to delete script: ${txt}`);
      }
    } catch (err) {
      console.error('Delete script failed:', err);
      alert(`Delete script error: ${err.message}`);
    }
  };

  const handleTemplateChange = (e) => {
    const key = e.target.value;
    if (TEMPLATES[key]) {
      setScriptName(TEMPLATES[key].name);
      setScriptContent(TEMPLATES[key].content);
    }
  };

  const handleSaveScript = async () => {
    if (!scriptName.trim()) {
      alert('Please provide a script name');
      return;
    }
    setIsSaving(true);
    try {
      const res = await fetch(`${coordinatorApi}/api/v1/automation/scripts`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          id: selectedScript?.id || '',
          name: scriptName,
          content: scriptContent
        })
      });
      if (res.ok) {
        const data = await res.json();
        alert('Script saved successfully!');
        fetchScripts();
        if (!selectedScript) {
          setSelectedScript({ id: data.id, name: scriptName, content: scriptContent });
        }
      } else {
        const text = await res.text();
        alert(`Failed to save script: ${text}`);
      }
    } catch (err) {
      alert(`Save error: ${err.message}`);
    } finally {
      setIsSaving(false);
    }
  };

  const handleRunScript = async (scriptIdToRun) => {
    const id = scriptIdToRun || selectedScript?.id;
    if (!id) {
      alert('Please save the script first before running it');
      return;
    }

    setIsRunning(true);
    setRunError('');
    setRunResults(null);
    setActiveSubtab('logs');

    try {
      const res = await fetch(`${coordinatorApi}/api/v1/automation/run`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          script_id: id,
          device_serials: selectedSerials
        })
      });

      if (res.ok) {
        const data = await res.json();
        const resultsMap = {};
        let firstSerial = '';

        for (const runRes of (data.results || [])) {
          if (!firstSerial) firstSerial = runRes.serial;

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
                  error: `Failed to fetch report from server (HTTP ${repRes.status})`
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
        setRunResults(resultsMap);
        if (firstSerial) {
          setActiveResultSerial(firstSerial);
        }
      } else {
        const text = await res.text();
        setRunError(text || 'Failed to trigger run request');
      }
    } catch (err) {
      setRunError(err.message);
    } finally {
      setIsRunning(false);
      fetchReports();
    }
  };

  const fetchDetailedReport = async (reportId) => {
    setDetailedReportLoading(true);
    try {
      const res = await fetch(`${coordinatorApi}/api/v1/automation/reports/${reportId}`);
      if (res.ok) {
        const data = await res.json();
        setSelectedReport(data);
      }
    } catch (err) {
      console.error('Error fetching detailed report:', err);
    } finally {
      setDetailedReportLoading(false);
    }
  };

  const handleCopyScript = () => {
    navigator.clipboard.writeText(scriptContent);
    alert('YAML Script copied to clipboard!');
  };

  const handleDownloadScript = () => {
    const blob = new Blob([scriptContent], { type: 'text/yaml' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `${scriptName.replace(/\s+/g, '_') || 'script'}.yaml`;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    URL.revokeObjectURL(url);
  };

  const handleInsertInspectorStep = () => {
    let stepYaml = '';

    // 1. Wait step before action (if checked)
    if (inspectorAddWait && (inspectorAction === 'click' || inspectorAction === 'input')) {
      stepYaml += `  - wait:
      condition: ${inspectorStepWaitCond}
      timeoutMs: ${inspectorStepWaitTimeout || 3000}
`;
      if (inspectorResourceId) stepYaml += `      resourceId: ${inspectorResourceId}\n`;
      if (inspectorText) stepYaml += `      text: ${inspectorText}\n`;
      if (inspectorClass) stepYaml += `      class: ${inspectorClass}\n`;
      if (inspectorXPath) stepYaml += `      xpath: ${inspectorXPath}\n`;
    }

    // 2. Main action step
    let mainStepYaml = '';
    if (inspectorAction === 'launch') {
      mainStepYaml = `  - launch:
      package: ${inspectorValue || 'com.android.settings'}
`;
    } else if (inspectorAction === 'click') {
      mainStepYaml = `  - click:
`;
      if (inspectorResourceId || inspectorText || inspectorClass || inspectorXPath) {
        mainStepYaml += `      target:
`;
        if (inspectorResourceId) mainStepYaml += `        resourceId: ${inspectorResourceId}\n`;
        if (inspectorText) mainStepYaml += `        text: ${inspectorText}\n`;
        if (inspectorClass) mainStepYaml += `        class: ${inspectorClass}\n`;
        if (inspectorXPath) mainStepYaml += `        xpath: ${inspectorXPath}\n`;
      } else {
        mainStepYaml += `      x: 0.5
      y: 0.5
`;
      }
    } else if (inspectorAction === 'input') {
      mainStepYaml = `  - input:
      text: ${inspectorValue || 'text'}
`;
      if (inspectorResourceId || inspectorText || inspectorClass || inspectorXPath) {
        mainStepYaml += `      target:
`;
        if (inspectorResourceId) mainStepYaml += `        resourceId: ${inspectorResourceId}\n`;
        if (inspectorText) mainStepYaml += `        text: ${inspectorText}\n`;
        if (inspectorClass) mainStepYaml += `        class: ${inspectorClass}\n`;
        if (inspectorXPath) mainStepYaml += `        xpath: ${inspectorXPath}\n`;
      }
    } else if (inspectorAction === 'wait') {
      mainStepYaml = `  - wait:
      condition: ${inspectorWaitCond}
      timeoutMs: ${inspectorTimeout || 3000}
`;
      if (inspectorResourceId) mainStepYaml += `      resourceId: ${inspectorResourceId}\n`;
      if (inspectorText) mainStepYaml += `      text: ${inspectorText}\n`;
      if (inspectorClass) mainStepYaml += `      class: ${inspectorClass}\n`;
      if (inspectorXPath) mainStepYaml += `      xpath: ${inspectorXPath}\n`;
    } else if (inspectorAction === 'assert') {
      mainStepYaml = `  - assert:
      condition: ${inspectorAssertCond}
      timeoutMs: ${inspectorTimeout || 3000}
`;
      if (inspectorValue) mainStepYaml += `      value: "${inspectorValue}"\n`;
      if (inspectorResourceId) mainStepYaml += `      resourceId: ${inspectorResourceId}\n`;
      if (inspectorText) mainStepYaml += `      text: ${inspectorText}\n`;
      if (inspectorClass) mainStepYaml += `      class: ${inspectorClass}\n`;
      if (inspectorXPath) mainStepYaml += `      xpath: ${inspectorXPath}\n`;
    }

    // Apply delay to the main step if defined
    if (inspectorStepDelay && (inspectorAction === 'click' || inspectorAction === 'input' || inspectorAction === 'launch' || inspectorAction === 'terminate')) {
      mainStepYaml = mainStepYaml.trimEnd() + `\n      delayMs: ${parseInt(inspectorStepDelay, 10) || 0}\n`;
    }
    stepYaml += mainStepYaml;

    // 3. Assertion step after action (if checked)
    if (inspectorAddAssert && (inspectorAction === 'click' || inspectorAction === 'input')) {
      stepYaml += `  - assert:
      condition: ${inspectorStepAssertCond}
      timeoutMs: ${inspectorStepAssertTimeout || 3000}
`;
      if (inspectorStepAssertValue) stepYaml += `      value: "${inspectorStepAssertValue}"\n`;
      if (inspectorResourceId) stepYaml += `      resourceId: ${inspectorResourceId}\n`;
      if (inspectorText) stepYaml += `      text: ${inspectorText}\n`;
      if (inspectorClass) stepYaml += `      class: ${inspectorClass}\n`;
      if (inspectorXPath) stepYaml += `      xpath: ${inspectorXPath}\n`;
    }

    // Append to current scriptContent
    let current = scriptContent.trim();
    if (!current.includes('steps:')) {
      current = 'steps:\n' + current;
    }
    // Make sure we have a newline at the end
    if (!current.endsWith('\n')) {
      current += '\n';
    }
    setScriptContent(current + stepYaml);
    
    // Clear inputs for next usage
    setInspectorResourceId('');
    setInspectorClass('');
    setInspectorXPath('');
    setInspectorValue('');
    setInspectorAddWait(false);
    setInspectorAddAssert(false);
    setInspectorStepDelay('');
    
    // Automatically switch back to Interact mode so the user can continue navigating the screen
    setInspectMode(false);
  };

  const generateXPath = (el) => {
    if (!el || el.nodeName !== 'node') return '';
    const resId = el.getAttribute('resource-id');
    const text = el.getAttribute('text');
    const contentDesc = el.getAttribute('content-desc');

    if (resId) {
      return `//node[@resource-id='${resId}']`;
    }
    if (text) {
      return `//node[@text='${text}']`;
    }
    if (contentDesc) {
      return `//node[@content-desc='${contentDesc}']`;
    }

    let path = [];
    let current = el;
    while (current && current.nodeName === 'node') {
      const index = current.getAttribute('index') || '0';
      const clsName = current.getAttribute('class') || 'node';
      path.unshift(`node[@index='${index}' and @class='${clsName}']`);
      current = current.parentNode;
    }
    return '/' + path.join('/');
  };

  const parseAndSetTree = (xmlStr) => {
    try {
      const parser = new DOMParser();
      const doc = parser.parseFromString(xmlStr, 'text/xml');
      const rootNode = doc.documentElement;
      if (rootNode) {
        setParsedTree(rootNode);
      }
    } catch (e) {
      console.error('Failed to parse XML hierarchy tree:', e);
    }
  };

  const refreshUiTree = () => {
    const ws = deviceWsRef?.current;
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      alert('Device connection not active. Cannot refresh UI tree.');
      return;
    }
    setIsRefreshingTree(true);
    ws.send(JSON.stringify({ type: 'DUMP_UI' }));
  };

  const handleSelectNode = (node) => {
    setSelectedTreeNode(node);
    const resId = node.getAttribute('resource-id') || '';
    const text = node.getAttribute('text') || '';
    const cls = node.getAttribute('class') || '';
    const xpathVal = generateXPath(node);

    setInspectorResourceId(resId);
    setInspectorText(text);
    setInspectorClass(cls);
    setInspectorXPath(xpathVal);
    setInspectorStepAssertValue(text);
  };

  const renderInspector = () => (
    <div className="automation-split-pane inspector-tab-split">
      <div className="ui-tree-pane">
        <div className="ui-tree-header">
          <div className="ui-tree-mode-selector" style={{ display: 'flex', gap: '4px', alignItems: 'center' }}>
            <div style={{ display: 'flex', gap: '2px', marginRight: '8px', background: 'var(--surface2)', padding: '2px', borderRadius: 'var(--radius-sm)', border: '1px solid var(--border)' }}>
              <button
                className={`subtab-btn ${inspectMode ? 'active' : ''}`}
                onClick={() => setInspectMode(true)}
                style={{ fontSize: '10px', padding: '2px 8px', height: '22px', border: 'none', borderRadius: '4px' }}
                title="Inspect elements on screen"
              >
                Inspect
              </button>
              <button
                className={`subtab-btn ${!inspectMode ? 'active' : ''}`}
                onClick={() => setInspectMode(false)}
                style={{ fontSize: '10px', padding: '2px 8px', height: '22px', border: 'none', borderRadius: '4px' }}
                title="Control device on screen"
              >
                Interact
              </button>
            </div>
            <button
              className={`subtab-btn ${treeViewMode === 'focused' ? 'active' : ''}`}
              onClick={() => setTreeViewMode('focused')}
              disabled={!selectedTreeNode}
              title={!selectedTreeNode ? "Select a node or click the screen to use Focused Path" : ""}
              style={{ fontSize: '11px', padding: '4px 10px', height: '28px', border: '1px solid var(--border)' }}
            >
              Focused Path
            </button>
            <button
              className={`subtab-btn ${treeViewMode === 'full' ? 'active' : ''}`}
              onClick={() => setTreeViewMode('full')}
              style={{ fontSize: '11px', padding: '4px 10px', height: '28px', border: '1px solid var(--border)' }}
            >
              Full Tree
            </button>
            <button
              className="btn btn-sm btn-secondary"
              onClick={refreshUiTree}
              disabled={isRefreshingTree}
              style={{ marginLeft: '8px', height: '28px', display: 'flex', alignItems: 'center' }}
            >
              {isRefreshingTree ? 'Refreshing...' : 'Refresh Screen Tree'}
            </button>
          </div>
        </div>
        <div className="ui-tree-content">
          {isRefreshingTree ? (
            <div className="ui-tree-loading">
              <span className="spinner"></span>
              <span>Fetching UI dump from device...</span>
            </div>
          ) : parsedTree ? (
            <TreeNode
              node={parsedTree}
              onSelect={handleSelectNode}
              selectedNode={selectedTreeNode}
              treeViewMode={treeViewMode}
            />
          ) : (
            <div className="ui-tree-placeholder">
              No UI Tree loaded. Click "Refresh Screen Tree" to fetch the current device screen hierarchy.
            </div>
          )}
        </div>
      </div>

      <div className="inspector-form-pane">
        <span className="section-header-title">Step Builder</span>
        <div className="inspector-form">
          <div className="inspector-field">
            <label className="inspector-label">Action Type</label>
            <select
              className="editor-select inspector-input-field"
              value={inspectorAction}
              onChange={(e) => setInspectorAction(e.target.value)}
            >
              <option value="click">Click Element</option>
              <option value="input">Input Text</option>
              <option value="wait">Wait Condition</option>
              <option value="assert">Assert State</option>
              <option value="launch">Launch App</option>
            </select>
          </div>

          {inspectorAction === 'launch' ? (
            <div className="inspector-field">
              <label className="inspector-label">Package Name</label>
              <input
                type="text"
                className="editor-input inspector-input-field"
                placeholder="e.g. com.android.settings"
                value={inspectorValue}
                onChange={(e) => setInspectorValue(e.target.value)}
              />
            </div>
          ) : (
            <>
              <div className="inspector-field">
                <label className="inspector-label">Resource ID</label>
                <input
                  type="text"
                  className="editor-input inspector-input-field"
                  placeholder="e.g. com.android.settings:id/title"
                  value={inspectorResourceId}
                  onChange={(e) => setInspectorResourceId(e.target.value)}
                />
              </div>

              <div className="inspector-field">
                <label className="inspector-label">Text</label>
                <input
                  type="text"
                  className="editor-input inspector-input-field"
                  placeholder="e.g. Settings"
                  value={inspectorText}
                  onChange={(e) => setInspectorText(e.target.value)}
                />
              </div>

              <div className="inspector-field">
                <label className="inspector-label">Class</label>
                <input
                  type="text"
                  className="editor-input inspector-input-field"
                  placeholder="e.g. android.widget.TextView"
                  value={inspectorClass}
                  onChange={(e) => setInspectorClass(e.target.value)}
                />
              </div>

              <div className="inspector-field">
                <label className="inspector-label">XPath</label>
                <input
                  type="text"
                  className="editor-input inspector-input-field"
                  placeholder="e.g. //node[@text='Settings']"
                  value={inspectorXPath}
                  onChange={(e) => setInspectorXPath(e.target.value)}
                />
              </div>

              {inspectorAction === 'input' && (
                <div className="inspector-field">
                  <label className="inspector-label">Text to Type</label>
                  <input
                    type="text"
                    className="editor-input inspector-input-field"
                    placeholder="e.g. youtube"
                    value={inspectorValue}
                    onChange={(e) => setInspectorValue(e.target.value)}
                  />
                </div>
              )}

              {inspectorAction === 'assert' && (
                <>
                  <div className="inspector-field">
                    <label className="inspector-label">Assert Value</label>
                    <input
                      type="text"
                      className="editor-input inspector-input-field"
                      placeholder="e.g. Settings"
                      value={inspectorValue}
                      onChange={(e) => setInspectorValue(e.target.value)}
                    />
                  </div>
                  <div className="inspector-field">
                    <label className="inspector-label">Assert Condition</label>
                    <select
                      className="editor-select inspector-input-field"
                      value={inspectorAssertCond}
                      onChange={(e) => setInspectorAssertCond(e.target.value)}
                    >
                      <option value="contains">Contains</option>
                      <option value="equals">Equals</option>
                      <option value="visible">Visible</option>
                      <option value="hidden">Hidden</option>
                    </select>
                  </div>
                </>
              )}

              {inspectorAction === 'wait' && (
                <div className="inspector-field">
                  <label className="inspector-label">Wait Condition</label>
                  <select
                    className="editor-select inspector-input-field"
                    value={inspectorWaitCond}
                    onChange={(e) => setInspectorWaitCond(e.target.value)}
                  >
                    <option value="visible">Visible</option>
                    <option value="hidden">Hidden</option>
                    <option value="present">Present</option>
                  </select>
                </div>
              )}

              {(inspectorAction === 'wait' || inspectorAction === 'assert') && (
                <div className="inspector-field">
                  <label className="inspector-label">Timeout (ms)</label>
                  <input
                    type="number"
                    className="editor-input inspector-input-field"
                    placeholder="3000"
                    value={inspectorTimeout}
                    onChange={(e) => setInspectorTimeout(e.target.value)}
                  />
                </div>
              )}

              {(inspectorAction === 'click' || inspectorAction === 'input') && (
                <>
                  {/* Delay Section */}
                  <div className="inspector-field" style={{ borderTop: '1px solid var(--border)', paddingTop: '10px', marginBottom: '10px' }}>
                    <label className="inspector-label" style={{ fontWeight: 'bold' }}>Delay After Step (ms)</label>
                    <input
                      type="number"
                      className="editor-input inspector-input-field"
                      placeholder="e.g. 1000"
                      value={inspectorStepDelay}
                      onChange={(e) => setInspectorStepDelay(e.target.value)}
                    />
                  </div>
                </>
              )}
            </>
          )}

          <button className="btn btn-primary btn-sm insert-step-btn" onClick={handleInsertInspectorStep}>
            <Plus size={14} style={{ marginRight: '6px' }} /> Append Step to Script
          </button>
        </div>
      </div>
    </div>
  );



  const renderHistory = () => (
    <div className="history-container">
      {reports.length === 0 ? (
        <div style={{ color: 'var(--text-muted)', textAlign: 'center', padding: '48px' }}>
          No execution history found for this device.
        </div>
      ) : (
        reports.map(rep => (
          <div
            key={rep.id}
            className="report-card"
            onClick={() => fetchDetailedReport(rep.id)}
          >
            <div className="report-card-left">
              <span className="report-card-title">
                {rep.success ? <CheckCircle2 size={14} className="color-green" /> : <XCircle size={14} className="color-red" />}
                Execution Report
              </span>
              <div className="report-card-meta">
                <span>Start: {new Date(rep.start_time).toLocaleString()}</span>
                <span>Duration: {new Date(rep.end_time).getTime() - new Date(rep.start_time).getTime()}ms</span>
              </div>
            </div>
            <ChevronRight size={16} style={{ color: 'var(--text-muted)' }} />
          </div>
        ))
      )}
    </div>
  );

  return (
    <div className="automation-tab-container">
      <div className="automation-header">
        <div className="automation-subtabs">
          <button
            className={`subtab-btn ${activeSubtab === 'editor' ? 'active' : ''}`}
            onClick={() => setActiveSubtab('editor')}
          >
            <Code size={14} /> Editor
          </button>
          <button
            className={`subtab-btn ${activeSubtab === 'inspector' ? 'active' : ''}`}
            onClick={() => setActiveSubtab('inspector')}
          >
            <Layers size={14} /> Inspector
          </button>
          <button
            className={`subtab-btn ${activeSubtab === 'logs' ? 'active' : ''}`}
            onClick={() => setActiveSubtab('logs')}
          >
            <Play size={14} /> Execution
          </button>
          <button
            className={`subtab-btn ${activeSubtab === 'history' ? 'active' : ''}`}
            onClick={() => setActiveSubtab('history')}
          >
            <History size={14} /> History
          </button>
        </div>
      </div>

      <div className="automation-content-area">
        {activeSubtab === 'editor' && (
          <EditorTab
            scripts={scripts}
            selectedScript={selectedScript}
            scriptName={scriptName}
            scriptContent={scriptContent}
            isRecording={isRecording}
            isSaving={isSaving}
            yamlError={yamlError}
            onNewScript={handleNewScript}
            onSelectScript={handleSelectScript}
            onSave={handleSaveScript}
            onReset={handleResetScript}
            onDelete={handleDeleteScript}
            onToggleRecording={toggleRecording}
            onContentChange={setScriptContent}
            onNameChange={setScriptName}
          />
        )}
        {activeSubtab === 'inspector' && renderInspector()}
        {activeSubtab === 'logs' && (
          <ExecuteTab
            scripts={scripts}
            selectedScript={selectedScript}
            onSelectScript={handleSelectScript}
            onEditScript={(script) => {
              handleSelectScript(script);
              setActiveSubtab('editor');
            }}
            onDeleteScript={handleDeleteScript}
            currentDevice={device}
            coordinatorApi={coordinatorApi}
            fetchReports={fetchReports}
          />
        )}
        {activeSubtab === 'history' && renderHistory()}
      </div>

      {/* Report Detail Modal */}
      {selectedReport && (
        <div className="modal-overlay" onClick={() => setSelectedReport(null)}>
          <div className="modal-content" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <span className="modal-title">Execution Detail</span>
              <button className="btn btn-ghost btn-sm" onClick={() => setSelectedReport(null)}>✕</button>
            </div>
            <div className="modal-body">
              {selectedReport.isLocalPreview && selectedReport.currentScreenshot ? (
                <div style={{ textAlign: 'center' }}>
                  <span style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '8px', display: 'block' }}>Failure Screenshot</span>
                  <img
                    src={`data:image/jpeg;base64,${selectedReport.currentScreenshot}`}
                    alt="Failure Detail Screenshot"
                    style={{ maxWidth: '100%', maxHeight: '60vh', border: '1px solid var(--border)', borderRadius: 'var(--radius-sm)' }}
                  />
                </div>
              ) : (
                <>
                  <div className={`status-banner ${(selectedReport.results?.success || selectedReport.success) ? 'passed' : 'failed'}`}>
                    <div className="status-title-row">
                      {(selectedReport.results?.success || selectedReport.success) ? <CheckCircle2 size={16} /> : <XCircle size={16} />}
                      <span>{(selectedReport.results?.success || selectedReport.success) ? 'Passed' : 'Failed'}</span>
                    </div>
                    <span className="status-duration">Duration: {selectedReport.results?.durationMs || 0}ms</span>
                  </div>

                  <div className="timeline">
                    {(selectedReport.results?.results || []).map((step, idx) => (
                      <div key={idx} className={`timeline-step ${step.success ? 'passed' : 'failed'}`}>
                        <div className="step-icon-col">{idx + 1}</div>
                        <div className="step-details-col">
                          <div className="step-header">
                            <span className="step-title">{step.action?.toUpperCase()} Step</span>
                            <span className="step-duration-label">{step.durationMs}ms</span>
                          </div>
                          {!step.success && step.error && (
                            <div className="step-error-message">{step.error}</div>
                          )}
                          {step.screenshot && (
                            <div className="step-screenshot-preview" style={{ cursor: 'default' }}>
                              <img src={`data:image/jpeg;base64,${step.screenshot}`} alt="Failure Screenshot" />
                            </div>
                          )}
                        </div>
                      </div>
                    ))}
                  </div>
                </>
              )}
            </div>
            <div className="modal-footer">
              <button className="btn" onClick={() => setSelectedReport(null)}>Close</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// Recursive Tree Node component for parsing and selecting XML nodes
const TreeNode = ({ node, onSelect, selectedNode, treeViewMode }) => {
  const [expanded, setExpanded] = React.useState(true);

  React.useEffect(() => {
    if (selectedNode && isAncestorOf(node, selectedNode)) {
      setExpanded(true);
    }
  }, [selectedNode, node]);

  if (!node || node.nodeType !== 1) return null; // ELEMENT_NODE = 1

  const children = Array.from(node.childNodes).filter(
    (c) => c.nodeType === 1
  );

  const text = node.getAttribute('text') || '';
  const className = node.getAttribute('class') || '';
  const resourceId = node.getAttribute('resource-id') || '';
  const isSelected = selectedNode === node;

  let shortLabel = className.split('.').pop() || 'node';
  if (text) {
    shortLabel += ` ("${text}")`;
  } else if (resourceId) {
    shortLabel += ` (${resourceId.split('/').pop()})`;
  }

  const toggleExpand = (e) => {
    e.stopPropagation();
    setExpanded(!expanded);
  };

  const handleSelect = (e) => {
    e.stopPropagation();
    onSelect(node);
  };

  let childrenToRender = children;
  if (treeViewMode === 'focused' && selectedNode) {
    if (node === selectedNode) {
      childrenToRender = children;
    } else if (isAncestorOf(node, selectedNode)) {
      childrenToRender = children.filter(c => isAncestorOf(c, selectedNode));
    } else {
      childrenToRender = [];
    }
  }

  return (
    <div className="tree-node-wrapper">
      <div
        className={`tree-node-row ${isSelected ? 'selected' : ''}`}
        onClick={handleSelect}
      >
        {children.length > 0 ? (
          <span className={`tree-node-arrow ${expanded ? 'expanded' : ''}`} onClick={toggleExpand}>
            ▶
          </span>
        ) : (
          <span className="tree-node-bullet">•</span>
        )}
        <span className="tree-node-tag-name">&lt;{node.nodeName}</span>
        <span className="tree-node-class-name"> {shortLabel}</span>
        <span className="tree-node-tag-name">&gt;</span>
      </div>

      {childrenToRender.length > 0 && expanded && (
        <div className="tree-node-children">
          {childrenToRender.map((child, idx) => (
            <TreeNode
              key={idx}
              node={child}
              onSelect={onSelect}
              selectedNode={selectedNode}
              treeViewMode={treeViewMode}
            />
          ))}
        </div>
      )}
    </div>
  );
};

export default AutomationTab;
