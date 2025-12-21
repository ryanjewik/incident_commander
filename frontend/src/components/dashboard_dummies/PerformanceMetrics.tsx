function PerformanceMetrics() {
  const metrics = [
    { label: 'CPU Usage', value: 68, color: 'bg-orange-500', max: 100, unit: '%' },
    { label: 'Memory Usage', value: 82, color: 'bg-red-500', max: 100, unit: '%' },
    { label: 'Disk I/O', value: 45, color: 'bg-blue-500', max: 100, unit: '%' },
    { label: 'Network Traffic', value: 3.2, color: 'bg-green-500', max: 10, unit: 'Gbps' },
  ];

  return (
    <div className='bg-white p-4 rounded-lg border-4 border-purple-600 shadow-lg'>
      <h3 className='text-xl font-bold mb-4 text-purple-700'>Performance Metrics</h3>
      <div className='grid grid-cols-2 gap-4'>
        {metrics.map((metric) => (
          <div key={metric.label} className='p-4 bg-gray-50 rounded-lg'>
            <div className='flex items-center justify-between mb-2'>
              <span className='text-sm font-semibold text-gray-700'>{metric.label}</span>
              <span className='text-lg font-bold text-gray-800'>
                {metric.value}{metric.unit}
              </span>
            </div>
            <div className='relative w-full bg-gray-200 rounded-full h-2 overflow-hidden'>
              <div
                className={`${metric.color} h-full rounded-full transition-all duration-500`}
                style={{ width: `${(metric.value / metric.max) * 100}%` }}
              ></div>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

export default PerformanceMetrics;
