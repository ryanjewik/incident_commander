import { useEffect, useState } from 'react';
import { apiService } from '../../services/api';
import { useAuth } from '../../contexts/AuthContext';

function StatusDistribution() {
  const [statuses, setStatuses] = useState<any[]>([]);

  const { userData } = useAuth();
  useEffect(() => {
    let mounted = true;
    const orgId = userData?.organization_id;
    if (!orgId) return;
    apiService.getDatadogStatusDistribution(orgId).then((res:any) => {
      if (!mounted) return;
      setStatuses(res.status_distribution || []);
    }).catch(() => {
      setStatuses([
        { status: 'Active', count: 8, color: 'bg-purple-500', percentage: 32 },
        { status: 'New', count: 5, color: 'bg-blue-500', percentage: 20 },
        { status: 'Resolved', count: 10, color: 'bg-green-500', percentage: 40 },
        { status: 'Ignored', count: 2, color: 'bg-gray-400', percentage: 8 },
      ]);
    });
    return () => { mounted = false; };
  }, [userData?.organization_id]);

  const total = statuses.reduce((acc, s) => acc + (s.count || 0), 0) || 1;

  // map known statuses to consistent hex colors (inline styles ensure colors render regardless of Tailwind purge)
  const statusHex = (statusLabel: string) => {
    const s = (statusLabel || '').toLowerCase();
    switch (s) {
      case 'new':
        return '#6366F1'; // indigo-500
      case 'active':
        return '#8B5CF6'; // purple-500
      case 'resolved':
        return '#10B981'; // green-500
      case 'ignored':
        return '#9CA3AF'; // gray-400
      case 'unknown':
        return '#F59E0B'; // amber-500
      default:
        return '#9CA3AF';
    }
  };

  return (
    <div className='bg-white p-4 rounded-lg border-4 border-purple-600 shadow-lg'>
      <h3 className='text-xl font-bold mb-4 text-purple-700'>Incident Status Distribution</h3>
      <div className='space-y-4'>
        {statuses.map((status) => (
          <div key={status.status || status.label}>
            <div className='flex items-center justify-between mb-2'>
              <div className='flex items-center gap-2'>
                <div style={{ width: 12, height: 12, borderRadius: 6, backgroundColor: status.colorHex || statusHex(status.status || status.label) || status.color }}></div>
                <span className='font-semibold text-gray-800'>{status.status || status.label}</span>
              </div>
              <span className='text-sm font-semibold text-gray-600'>{status.count || 0} incidents ({Math.round(((status.count||0)/total)*100) }%)</span>
            </div>
              <div className='w-full bg-gray-200 rounded-full h-3 overflow-hidden'>
              <div
                className={`h-full rounded-full transition-all duration-500`}
                style={{ width: `${Math.round(((status.count||0)/total)*100)}%`, backgroundColor: status.colorHex || statusHex(status.status || status.label) || status.color }}
              ></div>
            </div>
          </div>
        ))}
      </div>
      <div className='mt-6 pt-4 border-t border-gray-200'>
        <p className='text-sm text-gray-600'>Total Incidents: <span className='font-bold text-gray-800'>{total}</span></p>
      </div>
    </div>
  );
}

export default StatusDistribution;
