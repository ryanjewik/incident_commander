import { createContext, useContext, useEffect, useState, useCallback, useMemo } from 'react';
import type { ReactNode } from 'react';
import { onAuthStateChanged, signInWithEmailAndPassword, createUserWithEmailAndPassword, signOut as firebaseSignOut } from 'firebase/auth';
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
  signUp: (email: string, password: string, firstName: string, lastName: string) => Promise<void>;
  signOut: () => Promise<void>;
  createOrganization: (name: string) => Promise<Organization>;
  addMember: (email: string, role: string) => Promise<void>;
  removeMember: (userId: string) => Promise<void>;
  leaveOrganization: () => Promise<void>;
  refreshUserData: () => Promise<void>;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [firebaseUser, setFirebaseUser] = useState<FirebaseUser | null>(null);
  const [userData, setUserData] = useState<User | null>(null);
  const [organization, setOrganization] = useState<Organization | null>(null);
  const [loading, setLoading] = useState(true);

  const fetchUserData = useCallback(async (user: FirebaseUser | null) => {
    if (!user) {
      setUserData(null);
      setOrganization(null);
      return;
    }

    try {
      await new Promise(resolve => setTimeout(resolve, 100));
      
      const data = await apiService.getMe();
      setUserData(data);
      setOrganization(null); // We don't need to fetch org details separately
    } catch (error: any) {
      console.error('[AuthContext] Error fetching user data:', error);
      // If user doesn't exist in backend (404), create them
      if (error.response?.status === 404 && user) {
        console.log('[AuthContext] User not found in backend, creating user record');
        try {
          const emailName = user.email?.split('@')[0] || 'User';
          await apiService.createUser(user.email!, '', emailName, '');
          // Retry fetching user data
          const data = await apiService.getMe();
          setUserData(data);
          return;
        } catch (createError) {
          console.error('[AuthContext] Failed to create user:', createError);
        }
      }
      setUserData(null);
      setOrganization(null);
    }
  }, []);

  useEffect(() => {
    const unsubscribe = onAuthStateChanged(auth, async (user) => {
      setFirebaseUser(user);
      
      try {
        await fetchUserData(user);
      } finally {
        setLoading(false);
      }
    });

    return () => unsubscribe();
  }, [fetchUserData]);

  const signIn = useCallback(async (email: string, password: string) => {
    await signInWithEmailAndPassword(auth, email, password);
    // User data will be fetched by onAuthStateChanged
  }, []);

  const signUp = useCallback(async (email: string, password: string, firstName: string, lastName: string) => {
    try {
      await createUserWithEmailAndPassword(auth, email, password);
      // Create backend user record
      try {
        await apiService.createUser(email, password, firstName, lastName);
      } catch (error) {
        console.error('[AuthContext] Failed to create backend user:', error);
        // If it fails, the fetchUserData will retry
      }
    } catch (error: any) {
      console.error('[AuthContext] Signup error:', error);
      // Provide more helpful error messages
      if (error.code === 'auth/email-already-in-use') {
        throw new Error('This email is already registered. Please sign in instead or use a different email.');
      } else if (error.code === 'auth/weak-password') {
        throw new Error('Password should be at least 6 characters.');
      } else if (error.code === 'auth/invalid-email') {
        throw new Error('Invalid email address.');
      }
      throw error;
    }
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

  const removeMember = useCallback(async (userId: string) => {
    await apiService.removeMember(userId);
    if (firebaseUser) {
      await fetchUserData(firebaseUser);
    }
  }, [firebaseUser, fetchUserData]);

  const leaveOrganization = useCallback(async () => {
    await apiService.leaveOrganization();
    if (firebaseUser) {
      await fetchUserData(firebaseUser);
    }
  }, [firebaseUser, fetchUserData]);

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
    signUp,
    signOut,
    createOrganization,
    addMember,
    removeMember,
    leaveOrganization,
    refreshUserData
  }), [firebaseUser, userData, organization, loading, signIn, signUp, signOut, createOrganization, addMember, removeMember, leaveOrganization, refreshUserData]);

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