import { useState, useRef, useEffect } from 'react';
import { apiService, wsManager } from '../services/api';
import { useAuth } from '../contexts/AuthContext';
import PerformanceMetrics from './dashboard_dummies/PerformanceMetrics';
import IncidentTimeline from './dashboard_dummies/IncidentTimeline';
import StatusDistribution from './dashboard_dummies/StatusDistribution';
import SystemHealth from './dashboard_dummies/SystemHealth';
import RecentLogs from './dashboard_dummies/RecentLogs';
import ModeratorDecisionCard from './ModeratorDecisionCard';
import ModeratorDecisionModal from './ModeratorDecisionModal';
import type { Message } from '../services/api';

interface ChatPanelProps {
  incidentId?: string;
  title?: string;
  moderatorResult?: any;
  moderatorTimestamp?: string;
  onCreateIncident?: (incidentId: string) => void;
}

// Pastel color palette for different users
const pastelColors = [
  'bg-blue-200',
  'bg-green-200',
  'bg-yellow-200',
  'bg-pink-200',
  'bg-purple-200',
  'bg-indigo-200',
  'bg-red-200',
  'bg-orange-200',
  'bg-teal-200',
  'bg-cyan-200'
];

// Function to get consistent color for a user
const getUserColor = (userName: string): string => {
  const hash = userName.split('').reduce((acc, char) => acc + char.charCodeAt(0), 0);
  return pastelColors[hash % pastelColors.length];
};

function ChatPanel({ incidentId, title, moderatorResult, moderatorTimestamp, onCreateIncident }: ChatPanelProps) {
  const { userData } = useAuth();
  const [ddConfigured, setDdConfigured] = useState<boolean | null>(null);
  const [queryText, setQueryText] = useState('');
  const [isRecording, setIsRecording] = useState(false);
  const [messages, setMessages] = useState<Message[]>([]);
  const [loading, setLoading] = useState(false);
  const [isConnected, setIsConnected] = useState(false);
  const [isLoadingMessages, setIsLoadingMessages] = useState(true);
  const [currentUserName] = useState('You');
  const [showModeratorModal, setShowModeratorModal] = useState(false);
  const [localModeratorResult, setLocalModeratorResult] = useState<any | null>(null);
  const [localModeratorTimestamp, setLocalModeratorTimestamp] = useState<string | null>(null);
  const recognitionRef = useRef<any>(null);
  const mountedRef = useRef(true);
  const currentIncidentRef = useRef<string | undefined>(incidentId);

  // Initialize WebSocket connection and fetch initial messages
  useEffect(() => {
    mountedRef.current = true;
    currentIncidentRef.current = incidentId;
    
    // Set loading state but don't clear messages immediately
    setIsLoadingMessages(true);
    
    let unsubscribers: (() => void)[] = [];
    
    // Fetch messages first, then connect WebSocket
    const initializeChat = async () => {
      await fetchMessages();
      const unsubs = await connectWebSocket();
      if (unsubs) {
        unsubscribers = unsubs;
      }
    };
    
    initializeChat();

    return () => {
      mountedRef.current = false;
      currentIncidentRef.current = undefined;
      unsubscribers.forEach(unsub => unsub());
      wsManager.disconnect();
    };
  }, [incidentId]);

  // When no incident selected, check datadog config for organization to decide placeholders
  useEffect(() => {
    if (incidentId) return;
    let mounted = true;
    const orgId = userData?.organization_id;
    if (!orgId) {
      setDdConfigured(false);
      return;
    }
    setDdConfigured(null);
    // Fetch organization doc to read datadog_settings and datadog_secrets presence
    apiService.getOrganization(orgId).then((org: any) => {
      if (!mounted) return;
      const settings = (org && org.datadog_settings) ? org.datadog_settings : {};
      const secrets = (org && org.datadog_secrets) ? org.datadog_secrets : null;
      // If no secrets present at all, treat as not configured
      const hasSecrets = secrets && (secrets.encrypted_apiKey || secrets.encrypted_appKey || secrets.apiKey || secrets.appKey);
      // Determine which panels to show according to toggles
      // Map toggles: activityFeed -> IncidentTimeline/StatusDistribution
      // systemMetrics -> PerformanceMetrics
      // liveLogs -> RecentLogs
      // alertStatus -> SystemHealth
      const showActivity = Boolean(settings.activityFeed === true || settings.activityFeed === 'true');
      const showSystem = Boolean(settings.systemMetrics === true || settings.systemMetrics === 'true');
      const showLiveLogs = Boolean(settings.liveLogs === true || settings.liveLogs === 'true');
      const showAlertStatus = Boolean(settings.alertStatus === true || settings.alertStatus === 'true');
      // store these in ddConfigured as an object for rendering decisions
      setDdConfigured(hasSecrets ? { showActivity, showSystem, showLiveLogs, showAlertStatus } : false as any);
    }).catch(() => {
      if (!mounted) return;
      setDdConfigured(false);
    });
    return () => { mounted = false; };
  }, [incidentId, userData?.organization_id]);

  const connectWebSocket = async () => {
    try {
      await wsManager.connect();
      
      if (!mountedRef.current || currentIncidentRef.current !== incidentId) return;
      
      setIsConnected(true);

      // Listen for new messages
      const unsubscribeMessage = wsManager.onMessage((message) => {
        if (!mountedRef.current || currentIncidentRef.current !== incidentId) return;
        
        // Only show messages for this incident (or general chat if no incident)
        const currentIncident = incidentId || '';
        const messageIncident = message.incident_id || '';
        
        if (messageIncident === currentIncident) {
          setMessages((prev) => {
            // Check if message already exists to prevent duplicates
            const exists = prev.some(m => m.id === message.id);
            if (exists) {
              return prev;
            }
            // Add new message and sort by created_at
            const updated = [...prev, message];
            return updated.sort((a, b) => 
              new Date(a.created_at).getTime() - new Date(b.created_at).getTime()
            );
          });
        }
      });

      // Listen for connection changes
      const unsubscribeConnection = wsManager.onConnectionChange((connected) => {
        if (!mountedRef.current || currentIncidentRef.current !== incidentId) return;
        setIsConnected(connected);
        
        // Don't refresh messages on reconnect - WebSocket will deliver any missed messages
      });

      // Listen for generic events (e.g., moderator_decision) so we can update
      // the UI immediately without requiring a full incident refresh.
      const unsubscribeEvent = wsManager.onEvent((payload: any) => {
        try {
          if (!mountedRef.current) return;
          if (!payload || typeof payload !== 'object') return;

          if (payload.type === 'moderator_decision') {
            // Determine incident id from the event or nested chat_message as a fallback
            const eventIncident = payload.incident_id || '';
            const nestedChatIncident = payload.chat_message && (payload.chat_message.incident_id || payload.chat_message.incidentId) ? (payload.chat_message.incident_id || payload.chat_message.incidentId) : '';
            const targetIncident = incidentId || '';

            // If neither matches the currently displayed incident, ignore
            if (eventIncident !== targetIncident && nestedChatIncident !== targetIncident) {
              return;
            }

            // Update local moderator result and timestamp for immediate UI update
            setLocalModeratorResult(payload.moderator_result || null);
            setLocalModeratorTimestamp(payload.moderator_timestamp || null);

            // Also insert chat message if provided (prefer nested chat message)
            const message = payload.chat_message as any;
            if (message) {
              // If the top-level incident id was empty or mismatched, but the nested
              // chat message contains the correct incident id, normalize it here.
              if (!message.incident_id && payload.incident_id) {
                message.incident_id = payload.incident_id;
              }

              setMessages((prev) => {
                const exists = prev.some(m => m.id === message.id);
                if (exists) return prev;
                const updated = [...prev, message];
                return updated.sort((a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime());
              });
            }
          }
        } catch (e) {
          console.error('[ChatPanel] onEvent handler error', e);
        }
      });

      // Return cleanup function
      return [unsubscribeMessage, unsubscribeConnection, unsubscribeEvent];
    } catch (error) {
      console.error('[ChatPanel] Failed to connect WebSocket:', error);
      if (mountedRef.current && currentIncidentRef.current === incidentId) {
        setIsConnected(false);
      }
      return [];
    }
  };

  const fetchMessages = async () => {
    try {
      setIsLoadingMessages(true);
      const msgs = await apiService.getMessages(incidentId);
      
      if (mountedRef.current && currentIncidentRef.current === incidentId) {
        // Sort messages by created_at to ensure proper order
        const sortedMsgs = msgs.sort((a, b) => 
          new Date(a.created_at).getTime() - new Date(b.created_at).getTime()
        );
        setMessages(sortedMsgs);
      }
    } catch (error) {
      console.error('[ChatPanel] Error fetching messages:', error);
    } finally {
      if (mountedRef.current && currentIncidentRef.current === incidentId) {
        setIsLoadingMessages(false);
      }
    }
  };

  const handleSubmit = async () => {
    if (queryText.trim()) {
      setLoading(true);
      try {
        // If no incidentId, use NL Query endpoint to create incident
        if (!incidentId) {
          const res = await apiService.nlQuery({
            message: queryText,
            incident_data: {}
          });
          setQueryText('');
          // If backend returned an incident id, notify parent to select/open it
          if (res && res.incident_id) {
              console.debug('[ChatPanel] NL query created incident', res.incident_id);
              if (onCreateIncident) {
                console.debug('[ChatPanel] calling onCreateIncident callback with id', res.incident_id);
                onCreateIncident(res.incident_id);
              }
            // Parent will re-render ChatPanel with new incidentId and fetch messages
            return;
          }
        } else {
          // Send via WebSocket if connected, otherwise fall back to HTTP
          if (isConnected) {
            wsManager.sendMessage(queryText, incidentId);
            setQueryText('');
            // Message will be received via WebSocket, no need to fetch
          } else {
            await apiService.sendMessage({ text: queryText, incident_id: incidentId });
            setQueryText('');
            // Refresh messages after sending via HTTP
            await fetchMessages();
          }
        }
      } catch (error) {
        console.error('[ChatPanel] Error sending message:', error);
        alert('Failed to send message. Please try again.');
      } finally {
        setLoading(false);
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

  return (
    <div className='flex flex-col h-full bg-white rounded border-2 border-pink-300'>
      <div className='flex items-center justify-between p-2 border-b-2 border-pink-300 flex-shrink-0'>
        <h3 className='text-sm font-semibold text-purple-700'>
          {title || (incidentId ? 'Incident Chat' : '')}
        </h3>
        <div className='flex items-center gap-2'>
          <div className={`w-2 h-2 rounded-full ${isConnected ? 'bg-green-500' : 'bg-red-500'}`} />
          <span className='text-xs text-gray-600'>
            {isConnected ? 'Connected' : 'Disconnected'}
          </span>
        </div>
      </div>
      <div className='flex-1 min-h-0 overflow-y-auto p-3'>
        {isLoadingMessages ? (
          <p className='text-gray-500 text-center text-xs'>Loading messages...</p>
        ) : (
          <>
                {/* If no incident selected, render dashboard components or a placeholder if Datadog isn't configured */}
                {!incidentId && (
                  ddConfigured === false ? (
                    <div className='bg-white p-6 rounded border-2 border-purple-600 text-center'>
                      <h4 className='text-lg font-semibold text-purple-700 mb-2'>Datadog API keys not configured</h4>
                      <p className='text-gray-600 mb-4'>This organization does not have Datadog API keys set. Ask an administrator to add the Datadog API & APP keys in the Admin panel to enable dashboards and logs.</p>
                      <div className='flex justify-center'>
                        <button
                          onClick={() => window.dispatchEvent(new Event('openOrganizations'))}
                          className='bg-purple-700 text-white px-4 py-2 rounded-md hover:bg-purple-800'
                        >
                          Open Organizations
                        </button>
                      </div>
                    </div>
                  ) : ddConfigured == null ? (
                    <div className='text-center text-gray-500'>Checking organization configuration…</div>
                  ) : (
                    (() => {
                      // ddConfigured is an object with boolean flags
                      const cfg: any = ddConfigured as any;
                      const showSystem = !!cfg.showSystem;
                      const showActivity = !!cfg.showActivity;
                      const showLiveLogs = !!cfg.showLiveLogs;
                      const showAlertStatus = !!cfg.showAlertStatus;
                      return (
                        <div className='space-y-6'>
                          {showSystem ? <PerformanceMetrics /> : <div className='p-2 text-sm text-gray-500'>System metrics disabled</div>}
                          <div className='grid grid-cols-2 gap-6'>
                            {showActivity ? <IncidentTimeline /> : <div className='p-2 text-sm text-gray-500'>Activity feed disabled</div>}
                            {showActivity ? <StatusDistribution /> : <div className='p-2 text-sm text-gray-500'>Activity feed disabled</div>}
                          </div>
                          <div className='grid grid-cols-2 gap-6'>
                            {showAlertStatus ? <SystemHealth /> : <div className='p-2 text-sm text-gray-500'>Alert status disabled</div>}
                            {showLiveLogs ? <RecentLogs /> : <div className='p-2 text-sm text-gray-500'>Live logs disabled</div>}
                          </div>
                        </div>
                      );
                    })()
                  )
                )}

            {/* Messages list (for incident chats or general NL query responses) */}
            <div className='space-y-2 mt-4'>
              {/* Moderator card will be rendered inline where the moderator message appears in the feed */}

              {messages.map((msg) => {
                const isCurrentUser = msg.user_name === currentUserName;
                const bgColor = isCurrentUser ? 'bg-purple-300' : getUserColor(msg.user_name);
                // If this is a moderator chat message and we have a structured
                // `moderatorResult` (from local event or parent prop), render
                // the formatted card inline instead of the plain text bubble.
                const isModerator = /moderator/i.test(String(msg.user_name || ''));
                const displayModeratorResult = localModeratorResult || moderatorResult;
                const displayModeratorTimestamp = localModeratorTimestamp || moderatorTimestamp;
                if (isModerator && incidentId && displayModeratorResult) {
                  return (
                    <div key={msg.id} className={`flex ${isCurrentUser ? 'justify-end' : 'justify-start'}`}>
                      <div className='max-w-full w-full'>
                        <div className='flex items-center gap-2 mb-2'>
                          <span className='font-semibold text-gray-800 text-xs'>Moderator Bot</span>
                          <span className='text-[10px] text-gray-600'>
                            {displayModeratorTimestamp ? new Date(displayModeratorTimestamp).toLocaleString() : 'N/A'}
                          </span>
                        </div>
                        <ModeratorDecisionCard
                          moderatorResult={displayModeratorResult}
                          moderatorTimestamp={displayModeratorTimestamp}
                          onClick={() => setShowModeratorModal(true)}
                        />
                      </div>
                    </div>
                  );
                }

                return (
                  <div 
                    key={msg.id} 
                    className={`flex ${isCurrentUser ? 'justify-end' : 'justify-start'}`}
                  >
                    <div 
                      className={`${bgColor} p-2 rounded-lg border border-gray-200 max-w-[60%]`}
                      style={{ wordWrap: 'break-word' }}
                    >
                      <div className='flex items-center gap-2 mb-0.5'>
                        <span className='font-semibold text-gray-800 text-xs'>{msg.user_name}</span>
                        <span className='text-[10px] text-gray-600'>
                          {new Date(msg.created_at).toLocaleString()}
                        </span>
                      </div>
                      <p className={`text-gray-900 text-xs text-left ${msg.mentions_bot ? 'font-medium' : ''}`}>
                        {msg.text}
                      </p>
                    </div>
                  </div>
                );
              })}
            </div>
          </>
        )}
      </div>
      <div className='flex-shrink-0 bg-gradient-to-r from-purple-500 to-pink-500 p-3 rounded-b'>
        <div className='flex gap-2 items-center'>
          <button
            onClick={handleVoiceQuery}
            className={`h-12 w-12 rounded-lg transition-all flex items-center justify-center flex-shrink-0 ${
              isRecording 
                ? 'bg-red-500 hover:bg-red-600 animate-pulse ring-2 ring-red-300' 
                : 'bg-teal-500 hover:bg-teal-600 ring-2 ring-teal-300'
            }`}
          >
            <svg
              className="w-6 h-6 text-white"
              fill="currentColor"
              viewBox="0 0 20 20"
            >
              <path fillRule="evenodd" d="M7 4a3 3 0 016 0v4a3 3 0 11-6 0V4zm4 10.93A7.001 7.001 0 0017 8a1 1 0 10-2 0A5 5 0 015 8a1 1 0 00-2 0 7.001 7.001 0 006 6.93V17H6a1 1 0 100 2h8a1 1 0 100-2h-3v-2.07z" clipRule="evenodd" />
            </svg>
          </button>

          <input
            type='text'
            value={queryText}
            onChange={(e) => setQueryText(e.target.value)}
            onKeyPress={(e) => e.key === 'Enter' && !loading && handleSubmit()}
            placeholder={incidentId ? 'Type a message about this incident...' : 'Type a message or use @assistant for AI help...'}
            className='flex-grow h-10 px-3 rounded-md border-2 border-white focus:outline-none focus:ring-2 focus:ring-pink-300 text-xs'
            disabled={loading}
          />
          <button
            onClick={handleSubmit}
            disabled={loading || !queryText.trim()}
            className='h-10 px-3 bg-pink-600 text-white font-semibold rounded-md hover:bg-pink-700 transition-colors flex-shrink-0 disabled:bg-gray-400 text-xs'
          >
            {loading ? 'Sending...' : 'Send'}
          </button>
        </div>
      </div>

      {showModeratorModal && moderatorResult && (
        <ModeratorDecisionModal
          moderatorResult={moderatorResult}
          moderatorTimestamp={moderatorTimestamp}
          onClose={() => setShowModeratorModal(false)}
        />
      )}
    </div>
  );
}

export default ChatPanel;