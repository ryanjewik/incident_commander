import ChatPanel from './ChatPanel';
import { useEffect, useRef } from 'react';

interface Incident {
  id: number;
  title: string;
  status: string;
  date: string;
  type: string;
  description: string;
}

interface DashboardSectionProps {
  selectedIncident: number | null;
  incidentData: Incident[];
  onClose: () => void;
  onStatusChange: (incidentId: number, newStatus: string) => void;
  onSendIncident?: (incidentId: number) => void;
}

function MainContent({ selectedIncident, incidentData, onClose, onStatusChange, onSendIncident }: DashboardSectionProps) {
  const prevIncidentRef = useRef<number | null>(null);

  useEffect(() => {
    prevIncidentRef.current = selectedIncident;
  }, [selectedIncident]);

  const incident = selectedIncident ? incidentData.find(i => i.id === selectedIncident) : null;

  return (
    <div className='w-4/5 p-2 bg-pink-100 border-3 border-purple-700 shadow-lg mx-1 flex flex-col h-full overflow-y-auto'>
      {selectedIncident && incident ? (
        <>
          <div className='flex-shrink-0'>
            <div className='flex items-center justify-between mb-2'>
              <h3 className='text-2xl font-bold'>{incident.title}</h3>
              <div className='flex gap-1'>
                {onSendIncident && (
                  <button
                    onClick={() => onSendIncident(incident.id)}
                    className='px-3 py-1 bg-green-600 text-white rounded hover:bg-green-700 font-semibold text-sm'
                  >
                    Send to Firestore
                  </button>
                )}
                <select
                  value={incident.status}
                  onChange={(e) => onStatusChange(incident.id, e.target.value)}
                  className='px-3 py-1 border-3 border-purple-600 rounded font-semibold bg-white hover:bg-gray-50 focus:outline-none focus:ring-2 focus:ring-purple-600 text-sm'
                >
                  <option value='New'>New</option>
                  <option value='Active'>Active</option>
                  <option value='Resolved'>Resolved</option>
                  <option value='Ignored'>Ignored</option>
                </select>
                <button 
                  onClick={onClose}
                  className='px-3 py-1 bg-gray-500 text-white rounded hover:bg-gray-600 text-sm'
                >
                  Close
                </button>
              </div>
            </div>
            <div className='mb-2 flex gap-1 flex-wrap'>
              <span className={`px-2 py-1 rounded-sm text-xs font-semibold ${
                incident.status === 'Active' ? 'bg-purple-600 text-white' :
                incident.status === 'Ignored' ? 'bg-gray-400 text-white' :
                incident.status === 'New' ? 'bg-blue-500 text-white' :
                incident.status === 'Resolved' ? 'bg-green-500 text-white' : 'bg-gray-300'
              }`}>
                Status: {incident.status}
              </span>
              <span className='px-2 py-1 rounded-sm text-xs font-semibold bg-orange-400 text-white'>
                Date: {new Date(incident.date).toLocaleString()}
              </span>
              <span className={`px-2 py-1 rounded-sm text-xs font-semibold ${
                incident.type === 'Incident Report' ? 'bg-red-500 text-white' : 'bg-cyan-500 text-white'
              }`}>
                Type: {incident.type}
              </span>
            </div>
          </div>
          <div className='flex-1 min-h-0 space-y-2 flex flex-col'>
            <div className='bg-white p-3 rounded border border-pink-300 flex-shrink-0' style={{height: '25%'}}>
              <h4 className='text-sm font-semibold mb-1'>Description</h4>
              <p className='text-gray-700 text-xs leading-snug text-left'>{incident.description}</p>
            </div>
            <div className='bg-white p-3 rounded border border-pink-300 flex-shrink-0' style={{height: '25%'}}>
              <h4 className='text-sm font-semibold mb-1'>Incident Details</h4>
              <div className='flex gap-4 text-left'>
                <div>
                  <p className='font-semibold text-gray-600 text-xs'>Incident ID:</p>
                  <p className='text-gray-800 text-xs'>#{incident.id}</p>
                </div>
                <div>
                  <p className='font-semibold text-gray-600 text-xs'>Created:</p>
                  <p className='text-gray-800 text-xs'>{new Date(incident.date).toLocaleDateString()}</p>
                </div>
                <div>
                  <p className='font-semibold text-gray-600 text-xs'>Time:</p>
                  <p className='text-gray-800 text-xs'>{new Date(incident.date).toLocaleTimeString()}</p>
                </div>
                <div>
                  <p className='font-semibold text-gray-600 text-xs'>Priority:</p>
                  <p className='text-gray-800 text-xs'>{incident.status === 'Active' ? 'High' : incident.status === 'New' ? 'Medium' : 'Low'}</p>
                </div>
              </div>
            </div>
            <div className='flex-1 min-h-0'>
              <ChatPanel key={incident.id} incidentId={incident.id.toString()} title={`Chat: ${incident.title}`} />
            </div>
          </div>
        </>
      ) : (
        <div className='h-full'>
          <ChatPanel key="general" />
        </div>
      )}
    </div>
  );
}

export default MainContent;