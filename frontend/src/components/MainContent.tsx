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
    <div className='w-4/5 p-4 bg-pink-100 border-3 border-purple-700 shadow-lg h-full mx-2 overflow-y-auto'>
      {selectedIncident && incident ? (
        <div className='flex flex-col h-full'>
          <div className='flex-shrink-0'>
            <div className='flex items-center justify-between mb-4'>
              <h3 className='text-3xl font-bold'>{incident.title}</h3>
              <div className='flex gap-2'>
                {onSendIncident && (
                  <button
                    onClick={() => onSendIncident(incident.id)}
                    className='px-4 py-2 bg-green-600 text-white rounded hover:bg-green-700 font-semibold'
                  >
                    Send to Firestore
                  </button>
                )}
                <select
                  value={incident.status}
                  onChange={(e) => onStatusChange(incident.id, e.target.value)}
                  className='px-4 py-2 border-3 border-purple-600 rounded font-semibold bg-white hover:bg-gray-50 focus:outline-none focus:ring-2 focus:ring-purple-600'
                >
                  <option value='New'>New</option>
                  <option value='Active'>Active</option>
                  <option value='Resolved'>Resolved</option>
                  <option value='Ignored'>Ignored</option>
                </select>
                <button 
                  onClick={onClose}
                  className='px-4 py-2 bg-gray-500 text-white rounded hover:bg-gray-600'
                >
                  Close
                </button>
              </div>
            </div>
            <div className='mb-4 flex gap-2 flex-wrap'>
              <span className={`px-3 py-2 rounded-sm text-sm font-semibold ${
                incident.status === 'Active' ? 'bg-purple-600 text-white' :
                incident.status === 'Ignored' ? 'bg-gray-400 text-white' :
                incident.status === 'New' ? 'bg-blue-500 text-white' :
                incident.status === 'Resolved' ? 'bg-green-500 text-white' : 'bg-gray-300'
              }`}>
                Status: {incident.status}
              </span>
              <span className='px-3 py-2 rounded-sm text-sm font-semibold bg-orange-400 text-white'>
                Date: {new Date(incident.date).toLocaleString()}
              </span>
              <span className={`px-3 py-2 rounded-sm text-sm font-semibold ${
                incident.type === 'Incident Report' ? 'bg-red-500 text-white' : 'bg-cyan-500 text-white'
              }`}>
                Type: {incident.type}
              </span>
            </div>
            <div className='bg-white p-4 rounded border-2 border-pink-300 mb-4'>
              <h4 className='text-xl font-semibold mb-2'>Description</h4>
              <p className='text-gray-700 leading-relaxed'>{incident.description}</p>
            </div>
            <div className='bg-white p-4 rounded border-2 border-pink-300 mb-4'>
              <h4 className='text-xl font-semibold mb-2'>Incident Details</h4>
              <div className='grid grid-cols-2 gap-4'>
                <div>
                  <p className='font-semibold text-gray-600'>Incident ID:</p>
                  <p className='text-gray-800'>#{incident.id}</p>
                </div>
                <div>
                  <p className='font-semibold text-gray-600'>Created:</p>
                  <p className='text-gray-800'>{new Date(incident.date).toLocaleDateString()}</p>
                </div>
                <div>
                  <p className='font-semibold text-gray-600'>Time:</p>
                  <p className='text-gray-800'>{new Date(incident.date).toLocaleTimeString()}</p>
                </div>
                <div>
                  <p className='font-semibold text-gray-600'>Priority:</p>
                  <p className='text-gray-800'>{incident.status === 'Active' ? 'High' : incident.status === 'New' ? 'Medium' : 'Low'}</p>
                </div>
              </div>
            </div>
          </div>
          <div className='flex-1 min-h-0'>
            <ChatPanel key={incident.id} incidentId={incident.id.toString()} title={`Chat: ${incident.title}`} />
          </div>
        </div>
      ) : (
        <ChatPanel key="general" />
      )}
    </div>
  );
}

export default MainContent;