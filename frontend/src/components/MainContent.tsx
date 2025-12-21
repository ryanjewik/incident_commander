import { useState, useRef } from 'react';
import Dashboard from './Dashboard';

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
}

function MainContent({ selectedIncident, incidentData, onClose, onStatusChange }: DashboardSectionProps) {
  const [queryText, setQueryText] = useState('');
  const [isRecording, setIsRecording] = useState(false);
  const recognitionRef = useRef<any>(null);

  const handleSubmit = () => {
    if (queryText.trim()) {
      alert(`Query submitted: ${queryText}`);
      setQueryText('');
    }
  };

  const handleVoiceQuery = () => {
    // If already recording, stop it
    if (isRecording && recognitionRef.current) {
      recognitionRef.current.stop();
      return;
    }

    const SpeechRecognition = (window as any).SpeechRecognition || (window as any).webkitSpeechRecognition;
    
    if (!SpeechRecognition) {
      alert('Speech recognition is not supported in your browser. Please try Chrome or Edge.');
      return;
    }

    const recognition = new SpeechRecognition();
    recognitionRef.current = recognition;
    
    recognition.lang = 'en-US';
    recognition.continuous = false;
    recognition.interimResults = false;

    recognition.onstart = () => {
      setIsRecording(true);
    };

    recognition.onresult = (event: any) => {
      const transcript = event.results[0][0].transcript;
      setQueryText(transcript);
      setIsRecording(false);
      recognitionRef.current = null;
    };

    recognition.onerror = (event: any) => {
      console.error('Speech recognition error:', event.error);
      setIsRecording(false);
      recognitionRef.current = null;
      
      if (event.error === 'aborted') {
        // User manually stopped - this is normal, don't show error
        return;
      }
      
      if (event.error === 'not-allowed') {
        alert('Microphone access denied. Please allow microphone access in your browser settings.');
      } else {
        alert(`Speech recognition error: ${event.error}`);
      }
    };

    recognition.onend = () => {
      setIsRecording(false);
      recognitionRef.current = null;
    };

    recognition.start();
  };

  return (
    <div className='w-4/5 p-4 bg-pink-100 border-3 border-pink-700 shadow-lg h-full mx-2 overflow-y-auto'>
      {selectedIncident ? (
        (() => {
          const incident = incidentData.find(i => i.id === selectedIncident);
          if (!incident) return <p>Incident not found</p>;
          return (
            <div>
              <div className='flex items-center justify-between mb-4'>
                <h3 className='text-3xl font-bold'>{incident.title}</h3>
                <div className='flex gap-2'>
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
              <div className='bg-white p-4 rounded border-2 border-pink-300'>
                <h4 className='text-xl font-semibold mb-2'>Description</h4>
                <p className='text-gray-700 leading-relaxed'>{incident.description}</p>
              </div>
              <div className='mt-4 bg-white p-4 rounded border-2 border-pink-300'>
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
          );
        })()
      ) : (
        <div className='flex flex-col items-center justify-between h-full'>
          <Dashboard />
          <div className='w-full bg-gradient-to-r from-purple-500 to-pink-500 p-6 rounded-md'>
            <div className='flex gap-4 items-center'>
              <button
                onClick={handleVoiceQuery}
                className={`h-16 w-16 rounded-md transition-colors flex items-center justify-center flex-shrink-0 ${
                  isRecording 
                    ? 'bg-red-500 hover:bg-red-600 animate-pulse' 
                    : 'bg-teal-500 hover:bg-teal-600'
                }`}
                title={isRecording ? 'Recording...' : 'Voice Query'}
              >
                <svg className='w-8 h-8 text-white' fill='currentColor' viewBox='0 0 20 20'>
                  <path fillRule='evenodd' d='M7 4a3 3 0 016 0v4a3 3 0 11-6 0V4zm4 10.93A7.001 7.001 0 0017 8a1 1 0 10-2 0A5 5 0 015 8a1 1 0 00-2 0 7.001 7.001 0 006 6.93V17H6a1 1 0 100 2h8a1 1 0 100-2h-3v-2.07z' clipRule='evenodd' />
                </svg>
              </button>
              <input
                type='text'
                value={queryText}
                onChange={(e) => setQueryText(e.target.value)}
                onKeyPress={(e) => e.key === 'Enter' && handleSubmit()}
                placeholder='Ask a question or describe an incident...'
                className='flex-grow h-16 px-4 rounded-md border-2 border-white focus:outline-none focus:ring-2 focus:ring-pink-300'
              />
              <button
                onClick={handleSubmit}
                className='h-16 px-6 bg-pink-600 text-white font-semibold rounded-md hover:bg-pink-700 transition-colors flex-shrink-0'
              >
                Send
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

export default MainContent;
