import axios, { AxiosError, AxiosHeaders } from 'axios';
import type { AxiosResponse } from 'axios';
import type { InternalAxiosRequestConfig } from 'axios';
import { auth } from '../config/firebase';

const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080';
const WS_URL = import.meta.env.VITE_WS_URL || 'ws://localhost:8080';

const api = axios.create({
  baseURL: API_URL,
  headers: {
    'Content-Type': 'application/json',
  },
});

api.interceptors.request.use(async (config: InternalAxiosRequestConfig) => {
  const user = auth.currentUser;
  if (user) {
    try {
      const token = await user.getIdToken(false);
      if (!config.headers) {
        config.headers = new AxiosHeaders();
      }
      // AxiosHeaders supports set()
      (config.headers as AxiosHeaders).set('Authorization', `Bearer ${token}`);
    } catch (error) {
      console.error('[API] Failed to get ID token:', error);
    }
  }
  return config;
});

api.interceptors.response.use(
  (response: AxiosResponse) => {
    return response;
  },
  async (error: AxiosError) => {
    console.error('[API] Response error:', error.response?.status);
    
    if (error.response?.status === 401) {
      const currentPath = window.location.pathname;
      
      // Only sign out if we're not on auth pages
      // Let the caller (AuthContext) handle the error appropriately
      if (currentPath !== '/login' && currentPath !== '/signup') {
        await auth.signOut();
      }
    }
    return Promise.reject(error);
  }
);

export interface User {
  id: string;
  email: string;
  first_name: string;
  last_name: string;
  organization_id: string;
  role: string;
  created_at: string;
}

export interface Organization {
  id: string;
  name: string;
  created_at: string;
}

// Add DatadogSecrets type for API
export interface DatadogSecrets {
  apiKey: string;
  appKey: string;
  webhookSecret: string;
}

export interface JoinRequest {
  id: string;
  user_id: string;
  user_email: string;
  user_first_name: string;
  user_last_name: string;
  organization_id: string;
  status: 'pending' | 'approved' | 'rejected';
  created_at: string;
  updated_at: string;
}

export interface ModeratorResult {
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
}

export interface Incident {
  id: string;
  organization_id: string;
  title: string;
  status: string;
  date: string;
  type: string;
  description: string;
  created_by: string;
  created_at: string;
  updated_at: string;
  metadata?: Record<string, any>;
  moderator_result?: ModeratorResult;
  moderator_timestamp?: string;
  // Add event property to match Firebase structure
  event?: {
    moderator_result?: ModeratorResult;
    moderator_timestamp?: string;
    title?: string;
    type?: string;
    date_ms?: number;
    date_posix_s?: number;
    id?: string;
    last_updated_ms?: number;
    last_updated_posix_s?: number;
    link?: string;
    message?: string;
    snapshot_url?: string;
    tags?: string[];
    text_only_message?: string;
    alert_cycle_key?: string;
    alert_id?: string;
    alert_priority?: string;
    alert_status_summary?: string;
    alert_title?: string;
    alert_transition?: string;
    alert_type?: string;
    metric?: string;
    metric_namespace?: string;
    query?: string;
    scope?: string;
  };
}

export interface CreateIncidentRequest {
  title: string;
  status?: string;
  type: string;
  description: string;
  metadata?: Record<string, any>;
}

export interface UpdateIncidentRequest {
  title?: string;
  status?: string;
  description?: string;
  metadata?: Record<string, any>;
}

export interface Message {
  id: string;
  organization_id: string;
  incident_id: string;
  user_id: string;
  user_name: string;
  text: string;
  mentions_bot: boolean;
  created_at: string;
}

export interface SendMessageRequest {
  text: string;
  incident_id?: string;
}

export interface NLQueryRequest {
  message: string;
  incident_id?: string;
  incident_data?: Record<string, any>;
}

export interface NLQueryResponse {
  response: string;
  success: boolean;
  incident_id?: string;
}

// WebSocket connection manager
class WebSocketManager {
  private ws: WebSocket | null = null;
  private reconnectAttempts = 0;
  private maxReconnectAttempts = 5;
  private reconnectDelay = 1000;
  private messageHandlers: Set<(message: Message) => void> = new Set();
  private eventHandlers: Set<(payload: any) => void> = new Set();
  private connectionHandlers: Set<(connected: boolean) => void> = new Set();
  private shouldReconnect = true;
  private reconnectTimeout: number | null = null;
  private pingInterval: number | null = null;
  private isConnecting = false;
  private disconnectPromise: Promise<void> | null = null;

  async connect(): Promise<void> {
    // Wait for any pending disconnect to complete
    if (this.disconnectPromise) {
      await this.disconnectPromise;
    }

    // If already connecting, wait for that to complete
    if (this.isConnecting) {
      return;
    }

    // Clear any existing connection first
    if (this.ws) {
      await this.forceDisconnect();
    }

    this.isConnecting = true;
    this.shouldReconnect = true;

    try {
      const user = auth.currentUser;
      if (!user) {
        throw new Error('User not authenticated');
      }

      const token = await user.getIdToken(false);
      const wsUrl = `${WS_URL}/api/chat/ws?token=${token}`;

      await new Promise<void>((resolve, reject) => {
        this.ws = new WebSocket(wsUrl);

        const connectionTimeout = setTimeout(() => {
          if (this.ws && this.ws.readyState !== WebSocket.OPEN) {
            this.ws.close();
            reject(new Error('WebSocket connection timeout'));
          }
        }, 5000);

        this.ws.onopen = () => {
          clearTimeout(connectionTimeout);
          this.reconnectAttempts = 0;
          this.connectionHandlers.forEach(handler => handler(true));
          
          // Start ping interval
          this.startPingInterval();
          
          resolve();
        };

        this.ws.onmessage = (event) => {
          try {
            const payload = JSON.parse(event.data);
            // If payload looks like a chat Message, notify message handlers
            const isMessage = payload && (payload.id || payload.text || payload.user_id);
            if (isMessage) {
              try {
                const message = payload as Message;
                this.messageHandlers.forEach(handler => handler(message));
              } catch (err) {
                // fallthrough to event handlers
              }
            }
            // Notify generic event handlers for all payloads
            this.eventHandlers.forEach(handler => handler(payload));
          } catch (error) {
            console.error('[WebSocketManager] Failed to parse message:', error);
          }
        };

        this.ws.onerror = (error) => {
          clearTimeout(connectionTimeout);
          console.error('[WebSocketManager] Error:', error);
          reject(error);
        };

        this.ws.onclose = () => {
          clearTimeout(connectionTimeout);
          this.stopPingInterval();
          this.connectionHandlers.forEach(handler => handler(false));
          
          if (this.shouldReconnect) {
            this.attemptReconnect();
          }
        };
      });
    } finally {
      this.isConnecting = false;
    }
  }

  private async forceDisconnect(): Promise<void> {
    this.shouldReconnect = false;
    
    if (this.reconnectTimeout !== null) {
      clearTimeout(this.reconnectTimeout);
      this.reconnectTimeout = null;
    }
    
    this.stopPingInterval();
    
    if (this.ws) {
      const ws = this.ws;
      this.ws = null;
      
      if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) {
        ws.close();
        
        // Wait for close event with timeout
        await Promise.race([
          new Promise<void>(resolve => {
            const checkClosed = () => {
              if (ws.readyState === WebSocket.CLOSED) {
                resolve();
              } else {
                setTimeout(checkClosed, 50);
              }
            };
            checkClosed();
          }),
          new Promise<void>(resolve => setTimeout(resolve, 500))
        ]);
      }
    }
  }

  private startPingInterval(): void {
    this.stopPingInterval();
    
    // Send ping every 25 seconds (server expects pong within 60s)
    this.pingInterval = window.setInterval(() => {
      if (this.ws && this.ws.readyState === WebSocket.OPEN) {
        // WebSocket ping/pong is handled automatically by browser
      }
    }, 25000);
  }

  private stopPingInterval(): void {
    if (this.pingInterval !== null) {
      clearInterval(this.pingInterval);
      this.pingInterval = null;
    }
  }

  private attemptReconnect(): void {
    if (!this.shouldReconnect) {
      return;
    }

    if (this.reconnectAttempts >= this.maxReconnectAttempts) {
      console.log('[WebSocketManager] Max reconnection attempts reached');
      return;
    }

    // Clear any existing reconnect timeout
    if (this.reconnectTimeout !== null) {
      clearTimeout(this.reconnectTimeout);
    }

    this.reconnectAttempts++;
    const delay = this.reconnectDelay * Math.pow(2, this.reconnectAttempts - 1);

    this.reconnectTimeout = window.setTimeout(() => {
      this.connect().catch(error => {
        console.error('[WebSocketManager] Reconnection failed:', error);
      });
    }, delay);
  }

  sendMessage(text: string, incidentId?: string): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      throw new Error('WebSocket not connected');
    }

    const message = { text, incident_id: incidentId || '' };
    this.ws.send(JSON.stringify(message));
  }

  onMessage(handler: (message: Message) => void): () => void {
    this.messageHandlers.add(handler);
    return () => this.messageHandlers.delete(handler);
  }

  // Subscribe to generic websocket events (any payload)
  onEvent(handler: (payload: any) => void): () => void {
    this.eventHandlers.add(handler);
    return () => this.eventHandlers.delete(handler);
  }

  onConnectionChange(handler: (connected: boolean) => void): () => void {
    this.connectionHandlers.add(handler);
    return () => this.connectionHandlers.delete(handler);
  }

  disconnect(): void {
    this.disconnectPromise = this.forceDisconnect();
  }

  isConnected(): boolean {
    return this.ws !== null && this.ws.readyState === WebSocket.OPEN;
  }
}

export const wsManager = new WebSocketManager();

export const apiService = {
  getMe: () => {
    return api.get<User>('/api/auth/me').then((res: AxiosResponse<User>) => {
      return res.data;
    });
  },
  
  getOrgUsers: (orgId?: string) => {
    if (orgId) {
      return api.get<User[]>(`/api/auth/organizations/${orgId}/users`).then((res: AxiosResponse<User[]>) => {
        return res.data;
      });
    }
    return api.get<User[]>('/api/auth/users').then((res: AxiosResponse<User[]>) => {
      return res.data;
    });
  },
  
  createOrganization: (name: string) => {
    return api.post<Organization>('/api/auth/organizations', { name }).then((res: AxiosResponse<Organization>) => {
      return res.data;
    });
  },
  
  createUser: (email: string, password: string, firstName: string, lastName: string) => {
    return api.post<User>('/api/auth/users', { email, password, first_name: firstName, last_name: lastName }).then((res: AxiosResponse<User>) => {
      return res.data;
    });
  },
  
  addMember: (email: string, role: string) => {
    return api.post('/api/auth/organizations/members', { email, role }).then((res: AxiosResponse) => {
      return res.data;
    });
  },

  removeMember: (userId: string) => {
    return api.delete(`/api/auth/organizations/members/${userId}`).then((res: AxiosResponse) => {
      return res.data;
    });
  },

  leaveOrganization: () => {
    return api.post('/api/auth/organizations/leave').then((res: AxiosResponse) => {
      return res.data;
    });
  },

  searchOrganizations: (query: string) => {
    return api.get<Organization[]>('/api/auth/organizations/search', { params: { q: query } }).then((res: AxiosResponse<Organization[]>) => {
      return res.data;
    });
  },

  createJoinRequest: (organizationId: string) => {
    return api.post<JoinRequest>('/api/auth/organizations/join-requests', { organization_id: organizationId }).then((res: AxiosResponse<JoinRequest>) => {
      return res.data;
    });
  },

  getJoinRequests: () => {
    return api.get<JoinRequest[]>('/api/auth/organizations/join-requests').then((res: AxiosResponse<JoinRequest[]>) => {
      return res.data;
    });
  },

  approveJoinRequest: (requestId: string, orgId?: string) => {
    // If orgId provided, use the org-scoped endpoint, otherwise use legacy endpoint
    if (orgId) {
      return api.put(`/api/auth/organizations/${orgId}/join-requests/${requestId}/approve`).then((res: AxiosResponse) => {
        return res.data;
      });
    }
    return api.put(`/api/auth/organizations/join-requests/${requestId}/approve`).then((res: AxiosResponse) => {
      return res.data;
    });
  },

  rejectJoinRequest: (requestId: string) => {
    return api.put(`/api/auth/organizations/join-requests/${requestId}/reject`).then((res: AxiosResponse) => {
      return res.data;
    });
  },

  cleanupJoinRequests: () => {
    return api.delete<{ deleted: number }>(`/api/auth/organizations/join-requests/cleanup`).then((res: AxiosResponse<{ deleted: number }>) => {
      return res.data;
    });
  },

  getMyOrganizations: () => {
    return api.get<Organization[]>(`/api/auth/my-organizations`).then((res: AxiosResponse<Organization[]>) => {
      return res.data;
    });
  },

  getOrganization: (orgId: string) => {
    // Some deployments may not expose a direct GET /organizations/:orgId endpoint.
    // Fall back to fetching the user's organizations and find the matching ID.
    return api.get<Organization[]>(`/api/auth/my-organizations`).then((res: AxiosResponse<Organization[]>) => {
      const orgs = res.data || [];
      const found = orgs.find(o => o.id === orgId);
      return found || null;
    });
  },

  setActiveOrganization: (organizationId: string) => {
    return api.post(`/api/auth/organizations/active`, { organization_id: organizationId }).then((res: AxiosResponse) => {
      return res.data;
    });
  },

  // Incident endpoints
  getIncidents: () => {
    return api.get<Incident[]>('/api/incidents').then((res: AxiosResponse<Incident[]>) => {
      return res.data;
    });
  },

  getIncident: (id: string) => {
    return api.get<Incident>(`/api/incidents/${id}`).then((res: AxiosResponse<Incident>) => {
      return res.data;
    });
  },

  createIncident: (data: CreateIncidentRequest) => {
    return api.post<Incident>('/api/incidents', data).then((res: AxiosResponse<Incident>) => {
      return res.data;
    });
  },

  updateIncident: (id: string, data: UpdateIncidentRequest) => {
    return api.put<Incident>(`/api/incidents/${id}`, data).then((res: AxiosResponse<Incident>) => {
      return res.data;
    });
  },

  deleteIncident: (id: string) => {
    return api.delete(`/api/incidents/${id}`).then((res: AxiosResponse) => {
      return res.data;
    });
  },

  getOrgJoinRequests: (organizationId: string) => {
    return api.get<JoinRequest[]>(`/api/auth/organizations/${organizationId}/join-requests`).then((res: AxiosResponse<JoinRequest[]>) => {
      return res.data;
    });
  },

  // Chat endpoints
  sendMessage: (data: SendMessageRequest) => {
    return api.post<Message>('/api/chat/messages', data).then((res: AxiosResponse<Message>) => {
      return res.data;
    });
  },

  getMessages: (incidentId?: string) => {
    const params = incidentId ? { incident_id: incidentId } : {};
    return api.get<Message[]>('/api/chat/messages', { params }).then((res: AxiosResponse<Message[]>) => {
      return res.data;
    });
  },

  // NL Query endpoint
  nlQuery: (data: NLQueryRequest) => {
    return api.post<NLQueryResponse>('/NL_query', data).then((res: AxiosResponse<NLQueryResponse>) => {
      return res.data;
    });
  },

  // Datadog dashboard endpoints (org-scoped)
  getDatadogOverview: (orgId: string) => {
    return api.get<any>(`/api/datadog/${orgId}/overview`).then((res: AxiosResponse<any>) => res.data);
  },

  getDatadogTimeline: (orgId: string) => {
    return api.get<any>(`/api/datadog/${orgId}/timeline`).then((res: AxiosResponse<any>) => res.data);
  },

  getDatadogRecentLogs: (orgId: string) => {
    return api.get<any>(`/api/datadog/${orgId}/recent_logs`).then((res: AxiosResponse<any>) => res.data);
  },

  getDatadogStatusDistribution: (orgId: string) => {
    return api.get<any>(`/api/datadog/${orgId}/status_distribution`).then((res: AxiosResponse<any>) => res.data);
  },

  getDatadogMonitors: (orgId: string) => {
    return api.get<any>(`/api/datadog/${orgId}/monitors`).then((res: AxiosResponse<any>) => res.data);
  },

  // Add saveDatadogSecrets method (accepts secrets + optional settings payload)
  saveDatadogSecrets: (orgId: string, secrets: any) => {
    console.log('[API Service] Saving Datadog secrets for org:', orgId, secrets);
    return api.post(`/api/auth/organizations/${orgId}/datadog-secrets`, secrets).then((res: AxiosResponse) => {
      console.log('[API Service] Datadog secrets saved:', res.data);
      return res.data;
    });
  },
};

export default api;