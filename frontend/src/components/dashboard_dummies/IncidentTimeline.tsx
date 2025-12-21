function IncidentTimeline() {
  const timelineData = [
    { date: 'Mon', incidents: 12 },
    { date: 'Tue', incidents: 19 },
    { date: 'Wed', incidents: 8 },
    { date: 'Thu', incidents: 15 },
    { date: 'Fri', incidents: 22 },
    { date: 'Sat', incidents: 10 },
    { date: 'Sun', incidents: 7 },
  ];

  const maxIncidents = Math.max(...timelineData.map(d => d.incidents));

  return (
    <div className='bg-white p-4 rounded-lg border-4 border-purple-600 shadow-lg'>
      <h3 className='text-xl font-bold mb-4 text-purple-700'>Incident Timeline (Last 7 Days)</h3>
      <div className='flex items-end justify-between h-48 gap-2'>
        {timelineData.map((day) => (
          <div key={day.date} className='flex flex-col items-center flex-1'>
            <div className='relative w-full flex items-end justify-center' style={{ height: '160px' }}>
              <div
                className='bg-gradient-to-t from-purple-500 to-purple-300 w-full rounded-t hover:from-purple-600 hover:to-purple-400 transition-colors'
                style={{ height: `${(day.incidents / maxIncidents) * 100}%` }}
              />
              <span className='absolute -top-6 text-sm font-semibold text-purple-700'>{day.incidents}</span>
            </div>
            <p className='mt-2 text-sm font-semibold text-gray-600'>{day.date}</p>
          </div>
        ))}
      </div>
    </div>
  );
}

export default IncidentTimeline;
