import { useState, useRef, useEffect } from 'react';
import { apiService, wsManager } from '../services/api';
import type { Message } from '../services/api';

interface ChatPanelProps {
  incidentId?: string;
  title?: string;
}

function ChatPanel({ incidentId, title }: ChatPanelProps) {
  const [queryText, setQueryText] = useState('');
  const [isRecording, setIsRecording] = useState(false);
  const [messages, setMessages] = useState<Message[]>([]);
  const [loading, setLoading] = useState(false);
  const [isConnected, setIsConnected] = useState(false);
  const [isLoadingMessages, setIsLoadingMessages] = useState(true);
  const recognitionRef = useRef<any>(null);
  const mountedRef = useRef(true);

  // Initialize WebSocket connection and fetch initial messages
  useEffect(() => {
    mountedRef.current = true;
    
    // Clear messages immediately when incident changes
    setMessages([]);
    setIsLoadingMessages(true);
    
    // Fetch messages first, then connect WebSocket
    fetchMessages().then(() => {
      connectWebSocket();
    });

    return () => {
      mountedRef.current = false;
      wsManager.disconnect();
    };
  }, [incidentId]);

  const connectWebSocket = async () => {
    try {
      await wsManager.connect();
      
      if (!mountedRef.current) return;
      
      setIsConnected(true);

      // Listen for new messages
      const unsubscribeMessage = wsManager.onMessage((message) => {
        if (!mountedRef.current) return;
        
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
        if (!mountedRef.current) return;
        setIsConnected(connected);
        
        // Refresh messages when reconnected to catch any missed messages
        if (connected) {
          fetchMessages();
        }
      });

      // Return cleanup function
      return () => {
        unsubscribeMessage();
        unsubscribeConnection();
      };
    } catch (error) {
      console.error('[ChatPanel] Failed to connect WebSocket:', error);
      if (mountedRef.current) {
        setIsConnected(false);
      }
    }
  };

  const fetchMessages = async () => {
    try {
      setIsLoadingMessages(true);
      const msgs = await apiService.getMessages(incidentId);
      
      if (mountedRef.current) {
        // Sort messages by created_at to ensure proper order
        const sortedMsgs = msgs.sort((a, b) => 
          new Date(a.created_at).getTime() - new Date(b.created_at).getTime()
        );
        setMessages(sortedMsgs);
      }
    } catch (error) {
      console.error('[ChatPanel] Error fetching messages:', error);
    } finally {
      if (mountedRef.current) {
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
          await apiService.nlQuery({
            message: queryText,
            incident_data: {}
          });
          setQueryText('');
          // Optionally refresh messages or redirect to new incident
          await fetchMessages();
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
    <div className='flex flex-col h-full'>
      <div className='flex-1 overflow-y-auto mb-4 bg-white rounded border-2 border-pink-300 p-4'>
        <div className='flex items-center justify-between mb-4'>
          <h3 className='text-2xl font-bold text-purple-700'>
            {title || (incidentId ? 'Incident Chat' : 'Team Chat')}
          </h3>
          <div className='flex items-center gap-2'>
            <div className={`w-3 h-3 rounded-full ${isConnected ? 'bg-green-500' : 'bg-red-500'}`} />
            <span className='text-sm text-gray-600'>
              {isConnected ? 'Connected' : 'Disconnected'}
            </span>
          </div>
        </div>
        {isLoadingMessages ? (
          <p className='text-gray-500 text-center'>Loading messages...</p>
        ) : messages.length === 0 ? (
          <p className='text-gray-500 text-center'>No messages yet. Start the conversation!</p>
        ) : (
          <div className='space-y-3'>
            {messages.map((msg) => (
              <div key={msg.id} className='bg-gray-50 p-3 rounded border border-gray-200'>
                <div className='flex items-center justify-between mb-1'>
                  <span className='font-semibold text-purple-700'>{msg.user_name}</span>
                  <span className='text-xs text-gray-500'>
                    {new Date(msg.created_at).toLocaleString()}
                  </span>
                </div>
                <p className={`text-gray-800 ${msg.mentions_bot ? 'bg-yellow-50 p-2 rounded border border-yellow-200' : ''}`}>
                  {msg.text}
                </p>
              </div>
            ))}
          </div>
        )}
      </div>
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
            onKeyPress={(e) => e.key === 'Enter' && !loading && handleSubmit()}
            placeholder={incidentId ? 'Type a message about this incident...' : 'Type a message or use @assistant for AI help...'}
            className='flex-grow h-16 px-4 rounded-md border-2 border-white focus:outline-none focus:ring-2 focus:ring-pink-300'
            disabled={loading}
          />
          <button
            onClick={handleSubmit}
            disabled={loading || !queryText.trim()}
            className='h-16 px-6 bg-pink-600 text-white font-semibold rounded-md hover:bg-pink-700 transition-colors flex-shrink-0 disabled:bg-gray-400'
          >
            {loading ? 'Sending...' : 'Send'}
          </button>
        </div>
      </div>
    </div>
  );
}

export default ChatPanel;