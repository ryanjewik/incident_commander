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
      if (currentPath !== '/login' && currentPath !== '/signup') {
        console.log('[API] Signing out and redirecting to login');
        await auth.signOut();
        window.location.href = '/login';
      } else {
        console.log('[API] Already on login/signup page, not redirecting');
      }
    }
    return Promise.reject(error);
  }
);

export interface User {
  id: string;
  email: string;
  display_name: string;
  organization_id: string;
  role: string;
  created_at: string;
}

export interface Organization {
  id: string;
  name: string;
  created_at: string;
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
  
  getOrganization: (orgId: string) => {
    console.log('[API Service] Calling getOrganization with id:', orgId);
    return api.get<Organization>(`/api/auth/organizations/${orgId}`).then(res => {
      console.log('[API Service] getOrganization response:', res.data);
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
  
  createUser: (email: string, password: string, display_name: string) => {
    console.log('[API Service] Creating user:', email);
    return api.post<User>('/api/auth/users', { email, password, display_name }).then(res => {
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
};

export default api;