import { useState } from 'react';

interface Incident {
  id: number;
  title: string;
  status: string;
  date: string;
  type: string;
  description: string;
}

interface IncidentListProps {
  incidentData: Incident[];
  selectedIncident: number | null;
  onSelectIncident: (id: number) => void;
}

function IncidentList({ incidentData, selectedIncident, onSelectIncident }: IncidentListProps) {
  const [statusFilter, setStatusFilter] = useState<string>('All');
  const [typeFilter, setTypeFilter] = useState<string>('All');

  const filteredIncidents = incidentData.filter(incident => {
    const matchesStatus = statusFilter === 'All' || incident.status === statusFilter;
    const matchesType = typeFilter === 'All' || incident.type === typeFilter;
    return matchesStatus && matchesType;
  });

  return (
    <div className='w-full md:w-1/5 p-1 md:p-2 bg-purple-100 border-3 border-purple-700 shadow-lg ml-0 md:ml-1 h-full flex flex-col overflow-hidden'>
      <h3 className='text-lg md:text-xl font-semibold mb-1 flex-shrink-0'>Incidents & Queries</h3>
      
      <div className='mb-2 space-y-1 flex-shrink-0'>
        <div>
          <label className='text-xs md:text-sm font-semibold mb-0.5 block'>Status</label>
          <select 
            value={statusFilter}
            onChange={(e) => setStatusFilter(e.target.value)}
            className='w-full p-1 md:p-1.5 rounded border-2 border-purple-700 bg-white text-xs md:text-sm'
          >
            <option value="All">All Statuses</option>
            <option value="Active">Active</option>
            <option value="New">New</option>
            <option value="Resolved">Resolved</option>
            <option value="Ignored">Ignored</option>
          </select>
        </div>
        
        <div>
          <label className='text-xs md:text-sm font-semibold mb-0.5 block'>Type</label>
          <select 
            value={typeFilter}
            onChange={(e) => setTypeFilter(e.target.value)}
            className='w-full p-1 md:p-1.5 rounded border-2 border-purple-700 bg-white text-xs md:text-sm'
          >
            <option value="All">All Types</option>
            <option value="Incident Report">Incident Report</option>
            <option value="NL Query">NL Query</option>
          </select>
        </div>
      </div>

      <div className='flex-1 overflow-y-auto'>
        {[...filteredIncidents].sort((a, b) => new Date(b.date).getTime() - new Date(a.date).getTime()).map((incident) => (
          <div 
            key={incident.id} 
            onClick={() => onSelectIncident(incident.id)}
            className={`mb-1.5 border-2 md:border-3 border-purple-700 p-1 md:p-1.5 hover:bg-purple-200 hover:scale-105 hover:shadow-xl cursor-pointer flex items-start ${
              selectedIncident === incident.id ? 'bg-purple-300 scale-105' : ''
            }`}
          >
            <div className='bg-purple-500 text-white font-bold rounded-sm h-7 w-7 md:h-8 md:w-8 flex flex-col items-center justify-center mr-1 md:mr-1.5 flex-shrink-0'>
              <p className='text-[8px] md:text-[9px]'>{['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'][parseInt(incident.date.split('-')[1]) - 1]}</p>
              <p className='text-xs md:text-sm leading-none'>{incident.date.split('T')[0].split('-')[2]}</p>
            </div>
            <div className='flex flex-col min-w-0 flex-1'>
              <strong className="text-[11px] md:text-xs font-semibold truncate">{incident.title}</strong>
              <div className='flex gap-0.5 mt-0.5 flex-wrap'>
                <span className={`px-1 py-0.5 rounded-sm text-[8px] md:text-[9px] font-semibold ${
                  incident.status === 'Active' ? 'bg-purple-600 text-white' :
                  incident.status === 'Ignored' ? 'bg-gray-400 text-white' :
                  incident.status === 'New' ? 'bg-blue-500 text-white' :
                  incident.status === 'Resolved' ? 'bg-green-500 text-white' : 'bg-gray-300'
                }`}>
                  {incident.status}
                </span>
                <span className='px-1 py-0.5 rounded-sm text-[8px] md:text-[9px] font-semibold bg-orange-400 text-white hidden md:inline-block truncate'>
                  {incident.date}
                </span>
                <span className={`px-1 py-0.5 rounded-sm text-[8px] md:text-[9px] font-semibold ${
                  incident.type === 'Incident Report' ? 'bg-red-500 text-white' : 'bg-cyan-500 text-white'
                }`}>
                  {incident.type}
                </span>
              </div>
            </div>
          </div>
        ))}
        {filteredIncidents.length === 0 && <p className='text-xs md:text-sm'>No incidents or queries match the selected filters.</p>}
      </div>
    </div>
  );
}

export default IncidentList;