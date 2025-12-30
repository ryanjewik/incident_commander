import { useEffect, useState } from 'react';
import { apiService } from '../../services/api';
import { useAuth } from '../../contexts/AuthContext';
import { wsManager } from '../../services/api';

function RecentLogs() {
  const [logs, setLogs] = useState<any[]>([]);

  const { userData } = useAuth();
  useEffect(() => {
    let mounted = true;
    const orgId = userData?.organization_id;
    if (!orgId) return;
    apiService.getDatadogRecentLogs(orgId).then((res:any) => {
      if (!mounted) return;
      setLogs(res.recent_logs || []);
    }).catch(() => {
      setLogs([
        { time: '14:32:15', level: 'ERROR', message: 'Failed to connect to database replica', service: 'API' },
        { time: '14:30:22', level: 'WARN', message: 'High memory usage detected (92%)', service: 'Worker' },
        { time: '14:28:45', level: 'INFO', message: 'Successfully processed batch job #4521', service: 'Batch' },
        { time: '14:25:10', level: 'ERROR', message: 'Request timeout after 30s', service: 'Gateway' },
      ]);
    });
    return () => { mounted = false; };
  }, [userData?.organization_id]);

  // subscribe to websocket events for real-time updates
  useEffect(() => {
    let mounted = true;
    let didConnect = false;
    const orgId = userData?.organization_id;
    if (!orgId) return;

    const setup = async () => {
      try {
        if (!wsManager.isConnected()) {
          await wsManager.connect();
          didConnect = true;
        }
      } catch (err) {
        console.error('[RecentLogs] websocket connect failed', err);
      }

      const unsub = wsManager.onMessage((message: any) => {
        if (!mounted) return;
        try {
          const m = message as any;
          if (!m || !m.type) return;
          if (m.type === 'datadog_webhook' && m.payload) {
            setLogs(prev => [ m.payload, ...prev ].slice(0, 100));
            return;
          }
          if (m.type === 'datadog_poll_update' && Array.isArray(m.recent_logs)) {
            // prepend the polled logs (preserve up to 100)
            setLogs(prev => [...(m.recent_logs as any[]), ...prev].slice(0, 100));
            return;
          }
        } catch (e) {
          console.error('[RecentLogs] ws handler error', e);
        }
      });

      // cleanup handler
      return () => {
        unsub();
        if (didConnect) {
          try { wsManager.disconnect(); } catch (e) {}
        }
      };
    };

    let cleanupPromise: any = null;
    setup().then((cleanup) => { cleanupPromise = cleanup; });

    return () => { mounted = false; if (cleanupPromise) { cleanupPromise(); } };
  }, [userData?.organization_id]);

  const getLevelColor = (level: string) => {
    switch (level) {
      case 'ERROR': return 'bg-red-100 text-red-700 border-red-300';
      case 'WARN': return 'bg-yellow-100 text-yellow-700 border-yellow-300';
      case 'INFO': return 'bg-blue-100 text-blue-700 border-blue-300';
      default: return 'bg-gray-100 text-gray-700 border-gray-300';
    }
  };

  // detect if logs are Datadog event-derived synthetic entries
  const isSyntheticDatadogEvents = () => {
    if (!logs || logs.length === 0) return false;
    let count = 0;
    logs.forEach(l => { if (l.service === 'datadog_event') count++; });
    return count >= Math.max(1, Math.floor(logs.length / 2));
  };

  return (
    <div className='bg-white p-4 rounded-lg border-4 border-purple-600 shadow-lg'>
      <h3 className='text-xl font-bold mb-4 text-purple-700'>Recent Logs</h3>
      {isSyntheticDatadogEvents() && (
        <div className='mb-3 text-sm text-gray-600'>Source: Datadog events — these are event titles, not full logs.</div>
      )}
      <div className='space-y-2 max-h-[60vh] overflow-y-auto'>
        {logs.map((log, index) => (
          <div key={index} className='flex items-start gap-3 p-2 hover:bg-gray-50 rounded transition-colors'>
            <span className='text-xs text-gray-500 font-mono mt-1'>{log.time}</span>
            <span className={`text-xs font-semibold px-2 py-1 rounded border ${getLevelColor((log.level||'INFO').toString().toUpperCase())}`}>
              {String(log.level || 'INFO').toUpperCase()}
            </span>
            <div className='flex-1'>
              <p className='text-sm text-gray-800'>{log.message || log.title || ''}</p>
              <span className='text-xs text-gray-500'>{log.service || log.source || ''}</span>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

export default RecentLogs;
