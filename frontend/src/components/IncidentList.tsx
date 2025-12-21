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
    <div className='w-1/5 p-4 bg-purple-100 border-3 border-purple-700 shadow-lg h-384 ml-2 overflow-y-auto'>
      <h3 className='text-2xl font-semibold mb-2'>Incidents & Queries</h3>
      
      <div className='mb-4 space-y-2'>
        <div>
          <label className='text-sm font-semibold mb-1 block'>Status</label>
          <select 
            value={statusFilter}
            onChange={(e) => setStatusFilter(e.target.value)}
            className='w-full p-2 rounded border-2 border-purple-700 bg-white text-sm'
          >
            <option value="All">All Statuses</option>
            <option value="Active">Active</option>
            <option value="New">New</option>
            <option value="Resolved">Resolved</option>
            <option value="Ignored">Ignored</option>
          </select>
        </div>
        
        <div>
          <label className='text-sm font-semibold mb-1 block'>Type</label>
          <select 
            value={typeFilter}
            onChange={(e) => setTypeFilter(e.target.value)}
            className='w-full p-2 rounded border-2 border-purple-700 bg-white text-sm'
          >
            <option value="All">All Types</option>
            <option value="Incident Report">Incident Report</option>
            <option value="NL Query">NL Query</option>
          </select>
        </div>
      </div>

      {[...filteredIncidents].sort((a, b) => new Date(b.date).getTime() - new Date(a.date).getTime()).map((incident) => (
        <div 
          key={incident.id} 
          onClick={() => onSelectIncident(incident.id)}
          className={`mb-1 border-3 border-purple-700 p-4 mb-3 hover:bg-purple-200 hover:scale-105 hover:shadow-xl cursor-pointer flex items-start ${
            selectedIncident === incident.id ? 'bg-purple-300 scale-105' : ''
          }`}
        >
          <div className='bg-purple-500 text-white font-bold rounded-sm h-12 w-12 flex flex-col items-center justify-center mr-4 flex-shrink-0'>
            <p className='text-xs'>{['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'][parseInt(incident.date.split('-')[1]) - 1]}</p>
            <p className='text-lg leading-none'>{incident.date.split('T')[0].split('-')[2]}</p>
          </div>
          <div className='flex flex-col'>
            <strong className="text-lg font-semibold">{incident.title}</strong>
            <div className='flex gap-2 mt-1 flex-wrap'>
              <span className={`px-2 py-1 rounded-sm text-xs font-semibold ${
                incident.status === 'Active' ? 'bg-purple-600 text-white' :
                incident.status === 'Ignored' ? 'bg-gray-400 text-white' :
                incident.status === 'New' ? 'bg-blue-500 text-white' :
                incident.status === 'Resolved' ? 'bg-green-500 text-white' : 'bg-gray-300'
              }`}>
                {incident.status}
              </span>
              <span className='px-2 py-1 rounded-sm text-xs font-semibold bg-orange-400 text-white'>
                {incident.date}
              </span>
              <span className={`px-2 py-1 rounded-sm text-xs font-semibold ${
                incident.type === 'Incident Report' ? 'bg-red-500 text-white' : 'bg-cyan-500 text-white'
              }`}>
                {incident.type}
              </span>
            </div>
          </div>
        </div>
      ))}
      {filteredIncidents.length === 0 && <p>No incidents or queries match the selected filters.</p>}
    </div>
  );
}

export default IncidentList;
