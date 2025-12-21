function StatusDistribution() {
  const statuses = [
    { status: 'Active', count: 8, color: 'bg-purple-500', percentage: 32 },
    { status: 'New', count: 5, color: 'bg-blue-500', percentage: 20 },
    { status: 'Resolved', count: 10, color: 'bg-green-500', percentage: 40 },
    { status: 'Ignored', count: 2, color: 'bg-gray-400', percentage: 8 },
  ];

  return (
    <div className='bg-white p-4 rounded-lg border-4 border-purple-600 shadow-lg'>
      <h3 className='text-xl font-bold mb-4 text-purple-700'>Incident Status Distribution</h3>
      <div className='space-y-4'>
        {statuses.map((status) => (
          <div key={status.status}>
            <div className='flex items-center justify-between mb-2'>
              <div className='flex items-center gap-2'>
                <div className={`w-3 h-3 rounded ${status.color}`}></div>
                <span className='font-semibold text-gray-800'>{status.status}</span>
              </div>
              <span className='text-sm font-semibold text-gray-600'>{status.count} incidents ({status.percentage}%)</span>
            </div>
            <div className='w-full bg-gray-200 rounded-full h-3 overflow-hidden'>
              <div
                className={`${status.color} h-full rounded-full transition-all duration-500`}
                style={{ width: `${status.percentage}%` }}
              ></div>
            </div>
          </div>
        ))}
      </div>
      <div className='mt-6 pt-4 border-t border-gray-200'>
        <p className='text-sm text-gray-600'>Total Incidents: <span className='font-bold text-gray-800'>25</span></p>
      </div>
    </div>
  );
}

export default StatusDistribution;
