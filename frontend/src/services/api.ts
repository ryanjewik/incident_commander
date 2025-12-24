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
  private connectionHandlers: Set<(connected: boolean) => void> = new Set();
  private shouldReconnect = true;
  private reconnectTimeout: number | null = null;
  private pingInterval: number | null = null;

  async connect(): Promise<void> {
    // Clear any existing connection first
    if (this.ws) {
      this.shouldReconnect = false;
      this.ws.close();
      this.ws = null;
      await new Promise(resolve => setTimeout(resolve, 100)); // Brief delay
    }

    this.shouldReconnect = true;

    const user = auth.currentUser;
    if (!user) {
      throw new Error('User not authenticated');
    }

    const token = await user.getIdToken(false);
    const wsUrl = `${WS_URL}/api/chat/ws?token=${token}`;

    return new Promise((resolve, reject) => {
      this.ws = new WebSocket(wsUrl);

      this.ws.onopen = () => {
        this.reconnectAttempts = 0;
        this.connectionHandlers.forEach(handler => handler(true));
        
        // Start ping interval
        this.startPingInterval();
        
        resolve();
      };

      this.ws.onmessage = (event) => {
        try {
          const message = JSON.parse(event.data) as Message;
          this.messageHandlers.forEach(handler => handler(message));
        } catch (error) {
          console.error('[WebSocketManager] Failed to parse message:', error);
        }
      };

      this.ws.onerror = (error) => {
        console.error('[WebSocketManager] Error:', error);
        reject(error);
      };

      this.ws.onclose = () => {
        this.stopPingInterval();
        this.connectionHandlers.forEach(handler => handler(false));
        
        if (this.shouldReconnect) {
          this.attemptReconnect();
        }
      };
    });
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

  onConnectionChange(handler: (connected: boolean) => void): () => void {
    this.connectionHandlers.add(handler);
    return () => this.connectionHandlers.delete(handler);
  }

  disconnect(): void {
    this.shouldReconnect = false;
    
    if (this.reconnectTimeout !== null) {
      clearTimeout(this.reconnectTimeout);
      this.reconnectTimeout = null;
    }
    
    this.stopPingInterval();
    
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
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
  
  getOrgUsers: () => {
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

  approveJoinRequest: (requestId: string) => {
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
};

export default api;