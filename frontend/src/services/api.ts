import axios from 'axios';
import { auth } from '../config/firebase';

const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080';

console.log('[API] Initializing with base URL:', API_URL);

const api = axios.create({
  baseURL: API_URL,
  headers: {
    'Content-Type': 'application/json',
  },
});

api.interceptors.request.use(async (config) => {
  console.log('[API] Request to:', config.url);
  const user = auth.currentUser;
  console.log('[API] Current user exists:', !!user);
  if (user) {
    try {
      console.log('[API] Getting ID token with forceRefresh=false');
      const token = await user.getIdToken(false);
      console.log('[API] Token obtained, length:', token.length);
      config.headers.Authorization = `Bearer ${token}`;
    } catch (error) {
      console.error('[API] Failed to get ID token:', error);
    }
  } else {
    console.log('[API] No current user, skipping token');
  }
  return config;
});

api.interceptors.response.use(
  (response) => {
    console.log('[API] Response from:', response.config.url, 'Status:', response.status);
    return response;
  },
  async (error) => {
    console.error('[API] Response error from:', error.config?.url);
    console.error('[API] Error status:', error.response?.status);
    console.error('[API] Error data:', error.response?.data);
    
    if (error.response?.status === 401) {
      const currentPath = window.location.pathname;
      console.log('[API] 401 error, current path:', currentPath);
      
      // Only sign out if we're not on auth pages
      // Let the caller (AuthContext) handle the error appropriately
      if (currentPath !== '/login' && currentPath !== '/signup') {
        console.log('[API] 401 on protected route - signing out user');
        await auth.signOut();
      } else {
        console.log('[API] 401 on auth page, letting caller handle it');
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

export const apiService = {
  getMe: () => {
    console.log('[API Service] Calling getMe');
    return api.get<User>('/api/auth/me').then(res => {
      console.log('[API Service] getMe response:', res.data);
      return res.data;
    });
  },
  
  getOrgUsers: () => {
    console.log('[API Service] Calling getOrgUsers');
    return api.get<User[]>('/api/auth/users').then(res => {
      console.log('[API Service] getOrgUsers response:', res.data);
      return res.data;
    });
  },
  
  createOrganization: (name: string) => {
    console.log('[API Service] Creating organization:', name);
    return api.post<Organization>('/api/auth/organizations', { name }).then(res => {
      console.log('[API Service] Organization created:', res.data);
      return res.data;
    });
  },
  
  createUser: (email: string, password: string, firstName: string, lastName: string) => {
    console.log('[API Service] Creating user:', email);
    return api.post<User>('/api/auth/users', { email, password, first_name: firstName, last_name: lastName }).then(res => {
      console.log('[API Service] User created:', res.data);
      return res.data;
    });
  },
  
  addMember: (email: string, role: string) => {
    console.log('[API Service] Adding member:', email, role);
    return api.post('/api/auth/organizations/members', { email, role }).then(res => {
      console.log('[API Service] Member added:', res.data);
      return res.data;
    });
  },

  removeMember: (userId: string) => {
    console.log('[API Service] Removing member:', userId);
    return api.delete(`/api/auth/organizations/members/${userId}`).then(res => {
      console.log('[API Service] Member removed:', res.data);
      return res.data;
    });
  },

  leaveOrganization: () => {
    console.log('[API Service] Leaving organization');
    return api.post('/api/auth/organizations/leave').then(res => {
      console.log('[API Service] Left organization:', res.data);
      return res.data;
    });
  },

  searchOrganizations: (query: string) => {
    console.log('[API Service] Searching organizations:', query);
    return api.get<Organization[]>('/api/auth/organizations/search', { params: { q: query } }).then(res => {
      console.log('[API Service] Organizations found:', res.data);
      return res.data;
    });
  },

  createJoinRequest: (organizationId: string) => {
    console.log('[API Service] Creating join request for org:', organizationId);
    return api.post<JoinRequest>('/api/auth/organizations/join-requests', { organization_id: organizationId }).then(res => {
      console.log('[API Service] Join request created:', res.data);
      return res.data;
    });
  },

  getJoinRequests: () => {
    console.log('[API Service] Getting join requests');
    return api.get<JoinRequest[]>('/api/auth/organizations/join-requests').then(res => {
      console.log('[API Service] Join requests:', res.data);
      return res.data;
    });
  },

  approveJoinRequest: (requestId: string) => {
    console.log('[API Service] Approving join request:', requestId);
    return api.put(`/api/auth/organizations/join-requests/${requestId}/approve`).then(res => {
      console.log('[API Service] Join request approved:', res.data);
      return res.data;
    });
  },

  rejectJoinRequest: (requestId: string) => {
    console.log('[API Service] Rejecting join request:', requestId);
    return api.put(`/api/auth/organizations/join-requests/${requestId}/reject`).then(res => {
      console.log('[API Service] Join request rejected:', res.data);
      return res.data;
    });
  },

  cleanupJoinRequests: () => {
    console.log('[API Service] Cleaning up legacy join requests');
    return api.delete<{ deleted: number }>(`/api/auth/organizations/join-requests/cleanup`).then(res => {
      console.log('[API Service] Cleanup result:', res.data);
      return res.data;
    });
  },
};

export default api;