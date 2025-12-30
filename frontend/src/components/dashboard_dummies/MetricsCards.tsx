import { useEffect, useState } from 'react';
import { apiService } from '../../services/api';
import { useAuth } from '../../contexts/AuthContext';

function MetricsCards() {
  const [metrics, setMetrics] = useState<any | null>(null);

  const { userData } = useAuth();
  useEffect(() => {
    let mounted = true;
    const orgId = userData?.organization_id;
    if (!orgId) return;
    apiService.getDatadogOverview(orgId).then((res) => {
      if (!mounted) return;
      setMetrics(res.metrics || null);
    }).catch(() => {
      // ignore and keep synthetic values if API unavailable
    });
    return () => { mounted = false; };
  }, [userData?.organization_id]);

  const display = metrics ? [
    { title: 'Active Incidents', value: String(metrics.active_incidents || 0), change: '+12%', color: 'bg-red-500', textColor: 'text-red-500' },
    { title: 'Resolved Today', value: String(metrics.resolved_today || 0), change: '+8%', color: 'bg-green-500', textColor: 'text-green-500' },
    { title: 'Avg Response Time', value: String(metrics.avg_response_time || '12m'), change: '-5%', color: 'bg-blue-500', textColor: 'text-blue-500' },
    { title: 'System Uptime', value: String(metrics.system_uptime || '99.8%'), change: '+0.2%', color: 'bg-purple-500', textColor: 'text-purple-500' },
  ] : [
    { title: 'Active Incidents', value: '8', change: '+12%', color: 'bg-red-500', textColor: 'text-red-500' },
    { title: 'Resolved Today', value: '24', change: '+8%', color: 'bg-green-500', textColor: 'text-green-500' },
    { title: 'Avg Response Time', value: '12m', change: '-5%', color: 'bg-blue-500', textColor: 'text-blue-500' },
    { title: 'System Uptime', value: '99.8%', change: '+0.2%', color: 'bg-purple-500', textColor: 'text-purple-500' },
  ];

  return (
    <div className='grid grid-cols-4 gap-4'>
      {display.map((metric) => (
        <div key={metric.title} className='bg-white p-6 rounded-lg border-4 border-purple-600 shadow-lg hover:shadow-xl transition-shadow'>
          <div className='flex items-center justify-between mb-2'>
            <h4 className='text-sm font-semibold text-gray-600'>{metric.title}</h4>
            <div className={`w-3 h-3 rounded-full ${metric.color}`}></div>
          </div>
          <p className='text-3xl font-bold text-gray-800 mb-1'>{metric.value}</p>
          <p className={`text-sm font-semibold ${metric.textColor}`}>{metric.change} from yesterday</p>
        </div>
      ))}
    </div>
  );
}

export default MetricsCards;
