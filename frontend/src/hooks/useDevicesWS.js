import { useState, useEffect, useRef } from 'react';

const COORDINATOR_API = import.meta.env.VITE_COORDINATOR_API || `${window.location.protocol}//${window.location.hostname}:9002`;

export function useDevicesWS() {
  const [devices, setDevices] = useState([]);
  const [loading, setLoading] = useState(true);
  const [wsError, setWsError] = useState(null);
  const wsRef = useRef(null);

  useEffect(() => {
    let isMounted = true;
    let reconnectTimer;

    const connectWS = () => {
      const wsUrl = new URL(COORDINATOR_API);
      wsUrl.protocol = wsUrl.protocol === 'https:' ? 'wss:' : 'ws:';
      wsUrl.pathname = '/api/v1/devices/ws';

      const ws = new WebSocket(wsUrl.toString());
      wsRef.current = ws;

      ws.onopen = () => {
        console.log('WebSocket connected');
        if (isMounted) setWsError(null);
      };

      ws.onmessage = (event) => {
        try {
          const payload = JSON.parse(event.data);
          
          if (!isMounted) return;

          switch (payload.event) {
            case 'DEVICE_LIST_UPDATE':
              setDevices(payload.data || []);
              setLoading(false);
              break;
            case 'DEVICE_CLAIMED':
              console.log('Device claimed:', payload.data);
              // Optimistically update device list if needed, though DEVICE_LIST_UPDATE will catch it shortly
              setDevices(prev => prev.map(d => 
                d.serial === payload.data.serial 
                  ? { ...d, status: 'claimed', stream_port: payload.data.port }
                  : d
              ));
              break;
            case 'DEVICE_RELEASED':
              console.log('Device released:', payload.data);
              // Optimistically update device list
              setDevices(prev => prev.map(d => 
                d.serial === payload.data.serial 
                  ? { ...d, status: 'idle', stream_port: 0 }
                  : d
              ));
              break;
            default:
              console.log('Unknown WS event:', payload.event);
          }
        } catch (err) {
          console.error('Failed to parse WebSocket message:', err);
        }
      };

      ws.onclose = () => {
        console.log('WebSocket disconnected, reconnecting in 2s...');
        if (isMounted) {
          reconnectTimer = setTimeout(connectWS, 2000);
        }
      };

      ws.onerror = (err) => {
        console.error('WebSocket error:', err);
        if (isMounted) setWsError(err);
        ws.close();
      };
    };

    connectWS();

    return () => {
      isMounted = false;
      clearTimeout(reconnectTimer);
      if (wsRef.current) {
        wsRef.current.close();
      }
    };
  }, []);

  return { devices, loading, wsError, setDevices };
}
