function RecentLogs() {
  const logs = [
    { time: '14:32:15', level: 'ERROR', message: 'Failed to connect to database replica', service: 'API' },
    { time: '14:30:22', level: 'WARN', message: 'High memory usage detected (92%)', service: 'Worker' },
    { time: '14:28:45', level: 'INFO', message: 'Successfully processed batch job #4521', service: 'Batch' },
    { time: '14:25:10', level: 'ERROR', message: 'Request timeout after 30s', service: 'Gateway' },
    { time: '14:22:33', level: 'INFO', message: 'Cache cleared successfully', service: 'Cache' },
    { time: '14:20:18', level: 'WARN', message: 'SSL certificate expires in 7 days', service: 'Security' },
  ];

  const getLevelColor = (level: string) => {
    switch (level) {
      case 'ERROR': return 'bg-red-100 text-red-700 border-red-300';
      case 'WARN': return 'bg-yellow-100 text-yellow-700 border-yellow-300';
      case 'INFO': return 'bg-blue-100 text-blue-700 border-blue-300';
      default: return 'bg-gray-100 text-gray-700 border-gray-300';
    }
  };

  return (
    <div className='bg-white p-4 rounded-lg border-4 border-purple-600 shadow-lg'>
      <h3 className='text-xl font-bold mb-4 text-purple-700'>Recent Logs</h3>
      <div className='space-y-2 max-h-64 overflow-y-auto'>
        {logs.map((log, index) => (
          <div key={index} className='flex items-start gap-3 p-2 hover:bg-gray-50 rounded transition-colors'>
            <span className='text-xs text-gray-500 font-mono mt-1'>{log.time}</span>
            <span className={`text-xs font-semibold px-2 py-1 rounded border ${getLevelColor(log.level)}`}>
              {log.level}
            </span>
            <div className='flex-1'>
              <p className='text-sm text-gray-800'>{log.message}</p>
              <span className='text-xs text-gray-500'>{log.service}</span>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

export default RecentLogs;
