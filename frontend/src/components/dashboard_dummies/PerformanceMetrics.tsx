import { useEffect, useState } from 'react';
import { apiService, wsManager } from '../../services/api';
import { useAuth } from '../../contexts/AuthContext';

function PerformanceMetrics() {
  const [metrics, setMetrics] = useState<any | null>(null);
  const { userData } = useAuth();

  useEffect(() => {
    let mounted = true;
    const orgId = userData?.organization_id;
    if (!orgId) return;
    apiService.getDatadogOverview(orgId).then((res:any) => {
      if (!mounted) return;
      setMetrics(res.metrics || null);
    }).catch(() => {
      // ignore
    });
    // subscribe to websocket datadog poll updates
    const unsub = wsManager.onEvent?.((payload: any) => {
      if (!mounted) return;
      try {
        if (payload && payload.type === 'datadog_poll_update' && payload.metrics) {
          setMetrics(payload.metrics);
        }
      } catch (e) {
        // ignore
      }
    });
    return () => { mounted = false; unsub && unsub(); };
  }, [userData?.organization_id]);

  const formatBytes = (b: number) => {
    if (!Number.isFinite(b)) return 'n/a';
    const gb = b / (1024 * 1024 * 1024);
    if (gb >= 1) return `${gb.toFixed(2)}GB`;
    const mb = b / (1024 * 1024);
    if (mb >= 1) return `${mb.toFixed(1)}MB`;
    const kb = b / 1024;
    return `${Math.round(kb)}KB`;
  };

  // (removed unused formatRateMBs) dynamic formatting used instead

  const formatKbps = (kbps?: number) => {
    if (!Number.isFinite(Number(kbps))) return 'n/a';
    const n = Number(kbps);
    if (Math.abs(n) >= 1000000) {
      return `${(n / 1000000).toFixed(2)} Gbps`;
    }
    if (Math.abs(n) >= 1000) {
      return `${(n / 1000).toFixed(2)} Mbps`;
    }
    return `${n.toFixed(2)} Kbps`;
  };

  const formatDynamicRate = (bytesPerSec?: number) => {
    if (!Number.isFinite(Number(bytesPerSec))) return 'n/a';
    const b = Number(bytesPerSec);
    const absb = Math.abs(b);
    if (absb >= 1024 * 1024 * 1024) {
      return `${(b / (1024 * 1024 * 1024)).toFixed(2)} GB/s`;
    }
    if (absb >= 1024 * 1024) {
      return `${(b / (1024 * 1024)).toFixed(2)} MB/s`;
    }
    if (absb >= 1024) {
      return `${(b / 1024).toFixed(2)} KB/s`;
    }
    return `${b.toFixed(0)} B/s`;
  };

  const toPercent = (v: any) => {
    if (v == null || v === '') return NaN;
    const n = Number(v);
    if (!Number.isFinite(n)) return NaN;
    // if value looks like a fraction (0..1) treat as fraction
    if (n > 0 && n <= 1) return n * 100;
    // if it's already a percent (0..100) return as-is
    return n;
  };

  const cpuPct = metrics ? toPercent(metrics.cpu_usage ?? metrics.cpu ?? NaN) : NaN;
  const memRaw = metrics ? (metrics.memory_usage || metrics.memory || NaN) : NaN;
  const memPct = metrics ? toPercent(metrics.memory_usage_pct || metrics.memory_pct || NaN) : NaN;
  // Disk: compute bytes/sec preferring `disk_bps`, fallback to `disk_mb_s` (MB/s)
  const diskBps = metrics
    ? (metrics.disk_bps ?? metrics.diskBps ?? (Number.isFinite(Number(metrics.disk_mb_s)) ? Number(metrics.disk_mb_s) * 1000000.0 : null))
    : null;
  const diskMBs = Number.isFinite(Number(diskBps)) ? Number(diskBps) / (1024 * 1024) : NaN;
  // Network: compute bits/sec preferring `network_bps` (bytes/sec) or `network_kbps` (kilobits/sec)
  const networkBps = metrics ? (metrics.network_bps ?? metrics.networkBps ?? null) : null; // bytes/sec
  const networkBitsPerSec = metrics
    ? (Number.isFinite(Number(metrics.network_kbps)) ? Number(metrics.network_kbps) * 1000.0 : (Number.isFinite(Number(networkBps)) ? Number(networkBps) * 8.0 : NaN))
    : NaN; // bits/sec
  const networkKbps = Number.isFinite(Number(networkBitsPerSec)) ? Number(networkBitsPerSec) / 1000.0 : NaN; // kilobits/sec

  const data: Array<{ label: string; value: number; display: string; color: string; max: number; unit: string }> = [
    { label: 'CPU Usage', value: Number.isFinite(cpuPct) ? Number(cpuPct) : 0, display: Number.isFinite(cpuPct) ? `${Number(cpuPct).toFixed(1)}%` : 'n/a', color: 'bg-orange-500', max: 100, unit: '%' },
    // Ensure value is always numeric (use 0 as fallback) to avoid TS null/undefined issues.
    { label: 'Memory Usage', value: Number.isFinite(memPct) ? Number(memPct) : (Number.isFinite(memRaw) ? 0 : 0), display: Number.isFinite(memPct) ? `${Number(memPct).toFixed(1)}%` : (Number.isFinite(memRaw) ? formatBytes(Number(memRaw)) : 'n/a'), color: 'bg-red-500', max: 100, unit: '%' },
    // Disk: value in MB/s, dynamic max buckets for reasonable bar scaling
    { label: 'Disk I/O', value: Number.isFinite(diskMBs) ? Number(diskMBs) : 0, display: Number.isFinite(Number(diskBps)) ? formatDynamicRate(Number(diskBps)) : 'n/a', color: 'bg-blue-500', max: ((): number => {
        const v = Number.isFinite(diskMBs) ? Number(diskMBs) : 0;
        if (v <= 0) return 1;
        if (v < 1) return 1;
        if (v < 10) return 10;
        if (v < 100) return 100;
        return Math.ceil(v * 1.5);
      })(), unit: 'MB/s' },
    // Network: value in Kbps (kilobits/sec) with adaptive buckets
    { label: 'Network Traffic', value: Number.isFinite(networkKbps) ? Number(networkKbps) : 0, display: Number.isFinite(networkKbps) ? formatKbps(Number(networkKbps)) : 'n/a', color: 'bg-green-500', max: ((): number => {
        const v = Number.isFinite(networkKbps) ? Number(networkKbps) : 0;
        if (v <= 0) return 10;
        if (v < 10) return 10;
        if (v < 100) return 100;
        if (v < 1000) return 1000;
        if (v < 10000) return 10000;
        return Math.ceil(v * 1.5);
      })(), unit: 'Kbps' },
  ];

  return (
    <div className='bg-white p-4 rounded-lg border-4 border-purple-600 shadow-lg'>
      <h3 className='text-xl font-bold mb-4 text-purple-700'>Performance Metrics</h3>
      <div className='grid grid-cols-2 gap-4'>
        {data.map((metric) => (
          <div key={metric.label} className='p-4 bg-gray-50 rounded-lg'>
            <div className='flex items-center justify-between mb-2'>
              <span className='text-sm font-semibold text-gray-700'>{metric.label}</span>
              <span className='text-lg font-bold text-gray-800'>
                {metric.display}
              </span>
            </div>
            <div className='relative w-full bg-gray-200 rounded-full h-2 overflow-hidden'>
              <div
                className={`${metric.color} h-full rounded-full transition-all duration-500`}
                style={{ width: `${Math.max(0, Math.min(100, (Number(metric.value) / (metric.max || 100)) * 100))}%` }}
              ></div>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

export default PerformanceMetrics;
