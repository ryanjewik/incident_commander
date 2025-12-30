import { useEffect, useState } from 'react';
import { apiService, wsManager } from '../../services/api';
import { useAuth } from '../../contexts/AuthContext';

function SystemHealth() {
  const [systems, setSystems] = useState<any[]>([]);
  const [lastUpdated, setLastUpdated] = useState<string | null>(null);
  const [loading, setLoading] = useState<boolean>(false);

  const { userData } = useAuth();
  useEffect(() => {
    let mounted = true;
    const orgId = userData?.organization_id;
    if (!orgId) return;
    setLoading(true);
    // try monitors endpoint first
    apiService.getDatadogMonitors(orgId).then((mres:any) => {
      if (!mounted) return;
      if (mres && Array.isArray(mres.monitors) && mres.monitors.length > 0) {
          const mapMonitorState = (state: string) => {
            const s = (state || '').toLowerCase();
            if (s === 'ok') return { label: 'OK', cls: 'healthy' };
            if (s === 'no data' || s === 'nodata' || s === 'no_data') return { label: 'NO DATA', cls: 'warning' };
            if (s === 'alert' || s === 'critical') return { label: 'ALERT', cls: 'critical' };
            if (s === 'warn' || s === 'warning') return { label: 'WARNING', cls: 'warning' };
            return { label: (state || 'UNKNOWN').toString().toUpperCase(), cls: 'unknown' };
          };
          const monitors = mres.monitors.map((mon: any) => {
            const raw = mon.overall_state || mon.state || '';
            const mapped = mapMonitorState(raw);
            return {
              name: mon.name || `monitor-${mon.id}`,
              statusLabel: mapped.label,
              status: mapped.cls,
              id: mon.id,
            };
          });
          setSystems(monitors);
          setLoading(false);
          return;
        }

      // fallback to overview if monitors not available
      return apiService.getDatadogOverview(orgId);
    }).then((res:any) => {
      if (!mounted || !res) return;
      const m = res.metrics || {};
      if (res.last_updated) setLastUpdated(res.last_updated);

      // helpers to parse values
      const parsePercent = (v: any) => {
        if (v == null) return NaN;
        if (typeof v === 'number') return v;
        const parsed = parseFloat(String(v).replace('%', '').trim());
        return Number.isFinite(parsed) ? parsed : NaN;
      };

      const parseTimeMs = (v: any) => {
        if (v == null) return NaN;
        if (typeof v === 'number') return v;
        const s = String(v).trim();
        if (!s) return NaN;
        // examples: '45ms', '12m', '1.2s'
        const msMatch = s.match(/^([0-9.]+)\s*ms$/i);
        if (msMatch) return Number(msMatch[1]);
        const sMatch = s.match(/^([0-9.]+)\s*s(ec)?s?$/i);
        if (sMatch) return Number(sMatch[1]) * 1000;
        const mMatch = s.match(/^([0-9.]+)\s*m(in)?s?$/i);
        if (mMatch) return Number(mMatch[1]) * 60000;
        const asNum = parseFloat(s);
        return Number.isFinite(asNum) ? asNum : NaN;
      };

      const deriveStatus = (uptimeVal: any, respVal: any, extraWarn = false) => {
        const up = parsePercent(uptimeVal);
        const respMs = parseTimeMs(respVal);
        if (!Number.isNaN(up)) {
          if (up < 95) return 'critical';
          if (up < 99) return 'warning';
        }
        if (!Number.isNaN(respMs)) {
          if (respMs > 2000) return 'critical';
          if (respMs > 500) return 'warning';
        }
        if (extraWarn) return 'warning';
        return 'healthy';
      };

      // decide per-service values (prefer explicit per-service metrics, fallback to global)
      const systemUptime = m.system_uptime || m.uptime || null;
      const overallResp = m.avg_response_time || m.api_response_time || null;
      const cpu = Number(m.cpu_usage || m.cpu || NaN);
      const memPct = Number(m.memory_usage || m.memory || NaN);

      const apiUptime = m.api_uptime || systemUptime;
      const apiResp = m.api_response_time || overallResp;
      const dbUptime = m.db_uptime || systemUptime;
      const dbResp = m.db_response_time || overallResp;
      const lbUptime = m.lb_uptime || systemUptime;
      const lbResp = m.lb_response_time || overallResp;
      const cacheUptime = m.cache_uptime || systemUptime;
      const cacheResp = m.cache_response_time || overallResp;
      const mqUptime = m.mq_uptime || systemUptime;
      const mqResp = m.mq_response_time || overallResp;

      const mapDerived = (s: string) => {
        if (s === 'healthy') return { label: 'OK', cls: 'healthy' };
        if (s === 'warning') return { label: 'WARNING', cls: 'warning' };
        if (s === 'critical') return { label: 'ALERT', cls: 'critical' };
        return { label: 'UNKNOWN', cls: 'unknown' };
      };

      setSystems([
        (() => { const d = mapDerived(deriveStatus(apiUptime, apiResp, cpu > 85)); return { name: 'API Server', statusLabel: d.label, status: d.cls, id: 'api' }; })(),
        (() => { const d = mapDerived(deriveStatus(dbUptime, dbResp, memPct > 85)); return { name: 'Database', statusLabel: d.label, status: d.cls, id: 'db' }; })(),
        (() => { const d = mapDerived(deriveStatus(lbUptime, lbResp, cpu > 90)); return { name: 'Load Balancer', statusLabel: d.label, status: d.cls, id: 'lb' }; })(),
        (() => { const d = mapDerived(deriveStatus(cacheUptime, cacheResp, memPct > 80)); return { name: 'Cache Layer', statusLabel: d.label, status: d.cls, id: 'cache' }; })(),
        (() => { const d = mapDerived(deriveStatus(mqUptime, mqResp, cpu > 80)); return { name: 'Message Queue', statusLabel: d.label, status: d.cls, id: 'mq' }; })(),
      ]);
    }).catch(() => {
      setSystems([
        { name: 'API Server', statusLabel: 'OK', status: 'healthy', id: 'api' },
        { name: 'Database', statusLabel: 'OK', status: 'healthy', id: 'db' },
        { name: 'Load Balancer', statusLabel: 'WARNING', status: 'warning', id: 'lb' },
        { name: 'Cache Layer', statusLabel: 'OK', status: 'healthy', id: 'cache' },
        { name: 'Message Queue', statusLabel: 'OK', status: 'healthy', id: 'mq' },
      ]);
    }).finally(() => { setLoading(false); });
    // subscribe to websocket monitor/poll updates
    const unsubEvent = (wsManager as any).onEvent?.((payload: any) => {
      if (!mounted) return;
      try {
        if (payload && payload.type === 'datadog_poll_update') {
          // if monitors present in payload, prefer them
          if (payload.monitors && Array.isArray(payload.monitors)) {
            const mapMonitorState = (state: string) => {
              const s = (state || '').toLowerCase();
              if (s === 'ok') return { label: 'OK', cls: 'healthy' };
              if (s === 'no data' || s === 'nodata' || s === 'no_data') return { label: 'NO DATA', cls: 'warning' };
              if (s === 'alert' || s === 'critical') return { label: 'ALERT', cls: 'critical' };
              if (s === 'warn' || s === 'warning') return { label: 'WARNING', cls: 'warning' };
              return { label: (state || 'UNKNOWN').toString().toUpperCase(), cls: 'unknown' };
            };
            const monitors = payload.monitors.map((mon: any) => {
              const raw = mon.overall_state || mon.state || '';
              const mapped = mapMonitorState(raw);
              return {
                name: mon.name || `monitor-${mon.id}`,
                statusLabel: mapped.label,
                status: mapped.cls,
                id: mon.id,
              };
            });
            setSystems(monitors);
            return;
          }
          // otherwise if metrics exist, recalc derived statuses
          if (payload.metrics) {
            const m = payload.metrics || {};
            const parsePercent = (v: any) => {
              if (v == null) return NaN;
              if (typeof v === 'number') return v;
              const parsed = parseFloat(String(v).replace('%', '').trim());
              return Number.isFinite(parsed) ? parsed : NaN;
            };
            const parseTimeMs = (v: any) => {
              if (v == null) return NaN;
              if (typeof v === 'number') return v;
              const s = String(v).trim();
              if (!s) return NaN;
              const msMatch = s.match(/^([0-9.]+)\s*ms$/i);
              if (msMatch) return Number(msMatch[1]);
              const sMatch = s.match(/^([0-9.]+)\s*s(ec)?s?$/i);
              if (sMatch) return Number(sMatch[1]) * 1000;
              const mMatch = s.match(/^([0-9.]+)\s*m(in)?s?$/i);
              if (mMatch) return Number(mMatch[1]) * 60000;
              const asNum = parseFloat(s);
              return Number.isFinite(asNum) ? asNum : NaN;
            };
            const deriveStatus = (uptimeVal: any, respVal: any, extraWarn = false) => {
              const up = parsePercent(uptimeVal);
              const respMs = parseTimeMs(respVal);
              if (!Number.isNaN(up)) {
                if (up < 95) return 'critical';
                if (up < 99) return 'warning';
              }
              if (!Number.isNaN(respMs)) {
                if (respMs > 2000) return 'critical';
                if (respMs > 500) return 'warning';
              }
              if (extraWarn) return 'warning';
              return 'healthy';
            };
            const systemUptime = m.system_uptime || m.uptime || null;
            const overallResp = m.avg_response_time || m.api_response_time || null;
            const cpu = Number(m.cpu_usage || m.cpu || NaN);
            const memPct = Number(m.memory_usage || m.memory || NaN);
            const apiUptime = m.api_uptime || systemUptime;
            const apiResp = m.api_response_time || overallResp;
            const dbUptime = m.db_uptime || systemUptime;
            const dbResp = m.db_response_time || overallResp;
            const lbUptime = m.lb_uptime || systemUptime;
            const lbResp = m.lb_response_time || overallResp;
            const cacheUptime = m.cache_uptime || systemUptime;
            const cacheResp = m.cache_response_time || overallResp;
            const mqUptime = m.mq_uptime || systemUptime;
            const mqResp = m.mq_response_time || overallResp;
            const mapDerived = (s: string) => {
              if (s === 'healthy') return { label: 'OK', cls: 'healthy' };
              if (s === 'warning') return { label: 'WARNING', cls: 'warning' };
              if (s === 'critical') return { label: 'ALERT', cls: 'critical' };
              return { label: 'UNKNOWN', cls: 'unknown' };
            };
            setSystems([
              (() => { const d = mapDerived(deriveStatus(apiUptime, apiResp, cpu > 85)); return { name: 'API Server', statusLabel: d.label, status: d.cls, id: 'api' }; })(),
              (() => { const d = mapDerived(deriveStatus(dbUptime, dbResp, memPct > 85)); return { name: 'Database', statusLabel: d.label, status: d.cls, id: 'db' }; })(),
              (() => { const d = mapDerived(deriveStatus(lbUptime, lbResp, cpu > 90)); return { name: 'Load Balancer', statusLabel: d.label, status: d.cls, id: 'lb' }; })(),
              (() => { const d = mapDerived(deriveStatus(cacheUptime, cacheResp, memPct > 80)); return { name: 'Cache Layer', statusLabel: d.label, status: d.cls, id: 'cache' }; })(),
              (() => { const d = mapDerived(deriveStatus(mqUptime, mqResp, cpu > 80)); return { name: 'Message Queue', statusLabel: d.label, status: d.cls, id: 'mq' }; })(),
            ]);
          }
        }
        if (payload && payload.type === 'datadog_webhook' && payload.payload) {
          // lightweight webhook arrived — mark first system as alerted briefly
          setSystems((prev) => {
            // mark first system as alerted briefly to draw attention
            if (!prev || prev.length === 0) return prev;
            const copy = JSON.parse(JSON.stringify(prev));
            copy[0].status = 'critical';
            copy[0].statusLabel = 'ALERT';
            return copy;
          });
        }
      } catch (e) {
        // noop
      }
    });

    return () => { mounted = false; unsubEvent && unsubEvent(); };
  }, [userData?.organization_id]);

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'healthy': return 'bg-green-500';
      case 'warning': return 'bg-yellow-500';
      case 'critical': return 'bg-red-500';
      default: return 'bg-gray-500';
    }
  };

  return (
    <div className='bg-white p-4 rounded-lg border-4 border-purple-600 shadow-lg'>
      <h3 className='text-xl font-bold mb-4 text-purple-700'>System Health</h3>
      <div className='flex items-center justify-between mb-3'>
        <div className='text-sm text-gray-600'>Last update: <span className='font-medium text-gray-800'>{lastUpdated ? new Date(lastUpdated).toLocaleString() : 'n/a'}</span></div>
        <div className='text-sm text-gray-500'>{loading ? 'Refreshing…' : ''}</div>
      </div>
      <div className='space-y-3 max-h-[60vh] overflow-y-auto'>
        {systems.map((system) => (
          <div key={system.name} className='flex items-center justify-between p-3 bg-gray-50 rounded hover:bg-gray-100 transition-colors'>
            <div className='flex items-center gap-3'>
              <div className={`px-2 py-1 rounded-full text-xs font-semibold text-white ${getStatusColor(system.status)}`}>{system.statusLabel || 'UNKNOWN'}</div>
              <span className='font-semibold text-gray-800'>{system.name}</span>
            </div>
            <div className='flex items-center text-sm text-gray-500'>
              {system.id ? <span className='px-2 py-0.5 rounded bg-gray-100'>{String(system.id)}</span> : null}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

export default SystemHealth;
