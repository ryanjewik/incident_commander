import ChatPanel from './ChatPanel';
import ModeratorDecisionCard from './ModeratorDecisionCard';
import { useState, useRef } from 'react';
import React from 'react';

interface Incident {
  id: string;
  title: string;
  status: string;
  date: string;
  type: string;
  description: string;
  severity_guess?: string;
  // Add event property to match Firebase structure
  event?: {
    moderator_result?: {
      confidence?: number;
      incident_summary: string;
      most_likely_root_cause: string;
      reasoning: string;
      severity_guess: string;
      agent_disagreements?: Array<{
        agent: string;
        disagreement: string;
        confidence: number;
      }>;
      do_this_now?: string[];
      next_60_minutes?: string[];
      tradeoffs_and_risks?: string[];
      what_to_monitor?: string[];
    };
    moderator_timestamp?: string;
    title?: string;
    type?: string;
  };
  // Keep these for backward compatibility if some incidents have data at top level
  moderator_result?: {
    confidence?: number;
    incident_summary: string;
    most_likely_root_cause: string;
    reasoning: string;
    severity_guess: string;
  };
  moderator_timestamp?: string;
}

interface DashboardSectionProps {
  selectedIncident: string | null;
  incidentData: Incident[];
  onClose: () => void;
  onStatusChange: (incidentId: string, newStatus: string) => void;
  onSeverityChange?: (incidentId: string, newSeverity: string) => void;
  onSendIncident?: (incidentId: string) => void;
}

function formatDatadogDescription(description: string): React.ReactElement {
  if (!description) return <></>;
  
  // Remove leading %%% markers
  let formatted = description.replace(/^%%%\s*/gm, '');
  
  // Extract and format the main alert message (first line)
  const lines = formatted.split('\n').filter(line => line.trim());
  const alertTitle = lines[0]?.trim() || '';
  
  // Extract metric details
  const metricMatch = formatted.match(/\*\*([^*]+)\*\* over \*\*([^*]+)\*\* was \*\*([^*]+)\*\*/);
  const timeframeMatch = formatted.match(/during the \*\*([^*]+)\*\*/);
  const timestampMatch = formatted.match(/at (.+?)\. -/);
  
  // Extract threshold from title
  const thresholdMatch = alertTitle.match(/Threshold=(\S+)/);
  
  // Extract links
  const monitorStatusMatch = formatted.match(/\[\[Monitor Status\]\(([^)]+)\)\]/);
  const editMonitorMatch = formatted.match(/\[\[Edit Monitor\]\(([^)]+)\)\]/);
  const graphMatch = formatted.match(/\[!\[Metric Graph\]\([^)]+\)\]\(([^)]+)\)/);
  
  return (
    <div className="space-y-2">
      <div className="font-semibold text-gray-800 border-b border-gray-300 pb-1">
        {alertTitle.split('Threshold=')[0].trim()}
      </div>
      
      {(metricMatch || thresholdMatch) && (
        <div className="bg-blue-50 p-2 rounded border border-blue-200">
          <div className="font-semibold text-blue-900 text-xs mb-1">📊 Metric Details</div>
          <div className="text-xs space-y-0.5 text-gray-700">
            {metricMatch && (
              <>
                <div><span className="font-medium">Metric:</span> {metricMatch[1]}</div>
                <div><span className="font-medium">Scope:</span> {metricMatch[2]}</div>
                <div><span className="font-medium">Condition:</span> {metricMatch[3]}</div>
              </>
            )}
            {thresholdMatch && (
              <div><span className="font-medium">Threshold:</span> {thresholdMatch[1]}</div>
            )}
            {timeframeMatch && (
              <div><span className="font-medium">Timeframe:</span> {timeframeMatch[1]}</div>
            )}
          </div>
        </div>
      )}
      
      {timestampMatch && (
        <div className="bg-orange-50 p-2 rounded border border-orange-200">
          <div className="text-xs text-gray-700">
            <span className="font-medium">⏰ Last Triggered:</span> {timestampMatch[1]}
          </div>
        </div>
      )}
      
      {(monitorStatusMatch || editMonitorMatch || graphMatch) && (
        <div className="bg-purple-50 p-2 rounded border border-purple-200">
          <div className="font-semibold text-purple-900 text-xs mb-1">🔗 Quick Actions</div>
          <div className="text-xs space-y-0.5">
            {graphMatch && (
              <div>
                <a href={graphMatch[1]} target="_blank" rel="noopener noreferrer" 
                   className="text-blue-600 hover:text-blue-800 underline">
                  View Metric Graph →
                </a>
              </div>
            )}
            {monitorStatusMatch && (
              <div>
                <a href={monitorStatusMatch[1]} target="_blank" rel="noopener noreferrer" 
                   className="text-blue-600 hover:text-blue-800 underline">
                  Monitor Status →
                </a>
              </div>
            )}
            {editMonitorMatch && (
              <div>
                <a href={editMonitorMatch[1]} target="_blank" rel="noopener noreferrer" 
                   className="text-blue-600 hover:text-blue-800 underline">
                  Edit Monitor →
                </a>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

function MainContent({ selectedIncident, incidentData, onClose, onStatusChange, onSendIncident, onSeverityChange }: DashboardSectionProps) {
  const [queryText, setQueryText] = useState('');
  const [isRecording, setIsRecording] = useState(false);
  const recognitionRef = useRef<any>(null);

  const incident = incidentData.find(inc => inc.id === selectedIncident);

  // Extract moderator data from correct location (event.moderator_result or top-level fallback)
  const moderatorResult = incident?.event?.moderator_result || incident?.moderator_result;
  const moderatorTimestamp = incident?.event?.moderator_timestamp || incident?.moderator_timestamp;
  const incidentType = incident?.event?.type || incident?.type;

  // Get severity_guess from incident or moderator_result
  const getSeverityGuess = (): string => {
    if (incident?.severity_guess) {
      return incident.severity_guess;
    }
    if (moderatorResult?.severity_guess) {
      return moderatorResult.severity_guess;
    }
    return '';
  };

  const handleSubmit = async () => {
    if (queryText.trim()) {
      try {
        const response = await fetch('http://localhost:8080/NL_query', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({
            message: queryText,
            incident_id: selectedIncident ? selectedIncident.toString() : '',
          }),
        });

        if (!response.ok) {
          throw new Error('Network response was not ok');
        }

        const data = await response.json();
        alert(`Response: ${data.response}`);
        setQueryText('');
      } catch (error) {
        console.error('Error sending query:', error);
        alert('Failed to send query. Please try again.');
      }
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

  // Suppress unused variable warnings - these functions are kept for future use
  void handleSubmit;
  void handleVoiceQuery;

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
              <div className='flex items-center gap-2'>
                <label className='text-xs md:text-sm font-semibold hidden md:block'>Severity</label>
                <select
                  value={getSeverityGuess()}
                  onChange={(e) => onSeverityChange && onSeverityChange(incident.id, e.target.value)}
                  className={`px-2 py-1 rounded-sm text-xs md:text-sm font-semibold border border-transparent focus:outline-none focus:ring-1 ${
                    getSeverityGuess() === 'critical' ? 'bg-red-700 text-white' :
                    getSeverityGuess() === 'high' ? 'bg-red-500 text-white' :
                    getSeverityGuess() === 'medium' ? 'bg-yellow-500 text-white' :
                    getSeverityGuess() === 'low' ? 'bg-green-500 text-white' : 'bg-gray-200 text-black'
                  }`}
                >
                  <option value="">Auto</option>
                  <option value="low">Low</option>
                  <option value="medium">Medium</option>
                  <option value="high">High</option>
                  <option value="critical">Critical</option>
                </select>
              </div>
              <span className='px-2 py-1 rounded-sm text-xs font-semibold bg-orange-400 text-white'>
                Date: {new Date(incident.date).toLocaleString()}
              </span>
              <span className={`px-2 py-1 rounded-sm text-xs font-semibold ${
                incidentType === 'Incident Report' ? 'bg-red-500 text-white' : 'bg-cyan-500 text-white'
              }`}>
                Type: {incidentType}
              </span>
            </div>
          </div>
          <div className='flex-1 min-h-0 space-y-2 flex flex-col'>
            {incident.moderator_result && (
              <div className='flex-shrink-0'>
                <ModeratorDecisionCard 
                  moderatorResult={incident.moderator_result}
                  moderatorTimestamp={incident.moderator_timestamp}
                />
              </div>
            )}
            <div className='bg-white p-3 rounded border border-pink-300 flex-shrink-0 overflow-y-auto' style={{height: '30%'}}>
              <h4 className='text-sm font-semibold mb-2'>Incident Description</h4>
              <div className='text-left'>
                {formatDatadogDescription(incident.description)}
              </div>
            </div>
            <div className='bg-white p-3 rounded border border-pink-300 flex-shrink-0' style={{height: '12%'}}>
              <h4 className='text-sm font-semibold mb-1'>Incident Metadata</h4>
              <div className='flex gap-4 text-left'>
                <div>
                  <p className='font-semibold text-gray-600 text-xs'>Incident ID:</p>
                  <p className='text-gray-800 text-xs'>#{incident.id}</p>
                </div>
                <div>
                  <p className='font-semibold text-gray-600 text-xs'>Created Date:</p>
                  <p className='text-gray-800 text-xs'>{new Date(incident.date).toLocaleDateString()}</p>
                </div>
                <div>
                  <p className='font-semibold text-gray-600 text-xs'>Created Time:</p>
                  <p className='text-gray-800 text-xs'>{new Date(incident.date).toLocaleTimeString()}</p>
                </div>
              </div>
            </div>
            <div className='flex-1 min-h-0'>
              <ChatPanel 
                key={incident.id} 
                incidentId={incident.id.toString()} 
                title={`Chat: ${incident.title}`}
                incidentType={incidentType}
                moderatorResult={moderatorResult}
                moderatorTimestamp={moderatorTimestamp}
              />
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