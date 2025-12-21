function SystemHealth() {
  const systems = [
    { name: 'API Server', status: 'healthy', uptime: '99.9%', responseTime: '45ms' },
    { name: 'Database', status: 'healthy', uptime: '99.8%', responseTime: '12ms' },
    { name: 'Load Balancer', status: 'warning', uptime: '98.5%', responseTime: '23ms' },
    { name: 'Cache Layer', status: 'healthy', uptime: '99.9%', responseTime: '5ms' },
    { name: 'Message Queue', status: 'healthy', uptime: '99.7%', responseTime: '8ms' },
  ];

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
      <div className='space-y-3'>
        {systems.map((system) => (
          <div key={system.name} className='flex items-center justify-between p-3 bg-gray-50 rounded hover:bg-gray-100 transition-colors'>
            <div className='flex items-center gap-3'>
              <div className={`w-3 h-3 rounded-full ${getStatusColor(system.status)} animate-pulse`}></div>
              <span className='font-semibold text-gray-800'>{system.name}</span>
            </div>
            <div className='flex gap-6 text-sm'>
              <span className='text-gray-600'>Uptime: <span className='font-semibold text-gray-800'>{system.uptime}</span></span>
              <span className='text-gray-600'>Response: <span className='font-semibold text-gray-800'>{system.responseTime}</span></span>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

export default SystemHealth;
