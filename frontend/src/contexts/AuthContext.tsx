import { createContext, useContext, useEffect, useState, useCallback, useMemo } from 'react';
import type { ReactNode } from 'react';
import { onAuthStateChanged, signInWithEmailAndPassword, signOut as firebaseSignOut } from 'firebase/auth';
import type { User as FirebaseUser } from 'firebase/auth';
import { auth } from '../config/firebase';
import { apiService } from '../services/api';
import type { User, Organization } from '../services/api';

interface AuthContextType {
  firebaseUser: FirebaseUser | null;
  userData: User | null;
  organization: Organization | null;
  loading: boolean;
  signIn: (email: string, password: string) => Promise<void>;
  signOut: () => Promise<void>;
  createOrganization: (name: string) => Promise<Organization>;
  addMember: (email: string, role: string) => Promise<void>;
  refreshUserData: () => Promise<void>;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [firebaseUser, setFirebaseUser] = useState<FirebaseUser | null>(null);
  const [userData, setUserData] = useState<User | null>(null);
  const [organization, setOrganization] = useState<Organization | null>(null);
  const [loading, setLoading] = useState(true);

  const fetchUserData = useCallback(async (user: FirebaseUser) => {
    try {
      await new Promise(resolve => setTimeout(resolve, 100));
      
      const data = await apiService.getMe();
      setUserData(data);
      
      if (data.organization_id && data.organization_id !== '' && data.organization_id !== 'default') {
        try {
          const orgData = await apiService.getOrganization(data.organization_id);
          setOrganization(orgData);
        } catch (orgError) {
          setOrganization(null);
        }
      } else {
        setOrganization(null);
      }
    } catch (error) {
      setUserData(null);
      setOrganization(null);
    }
  }, []);

  useEffect(() => {
    const unsubscribe = onAuthStateChanged(auth, async (user) => {
      setFirebaseUser(user);
      
      try {
        if (user) {
          await fetchUserData(user);
        } else {
          setUserData(null);
          setOrganization(null);
        }
      } finally {
        setLoading(false);
      }
    });

    return () => unsubscribe();
  }, [fetchUserData]);

  const signIn = useCallback(async (email: string, password: string) => {
    await signInWithEmailAndPassword(auth, email, password);
  }, []);

  const signOut = useCallback(async () => {
    await firebaseSignOut(auth);
  }, []);

  const createOrganization = useCallback(async (name: string) => {
    const org = await apiService.createOrganization(name);
    setOrganization(org);
    if (firebaseUser) {
      await fetchUserData(firebaseUser);
    }
    return org;
  }, [firebaseUser, fetchUserData]);

  const addMember = useCallback(async (email: string, role: string) => {
    await apiService.addMember(email, role);
  }, []);

  const refreshUserData = useCallback(async () => {
    if (firebaseUser) {
      await fetchUserData(firebaseUser);
    }
  }, [firebaseUser, fetchUserData]);

  const contextValue = useMemo(() => ({
    firebaseUser, 
    userData, 
    organization, 
    loading, 
    signIn, 
    signOut,
    createOrganization,
    addMember,
    refreshUserData
  }), [firebaseUser, userData, organization, loading, signIn, signOut, createOrganization, addMember, refreshUserData]);

  return (
    <AuthContext.Provider value={contextValue}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  const context = useContext(AuthContext);
  if (context === undefined) {
    throw new Error('useAuth must be used within AuthProvider');
  }
  return context;
}