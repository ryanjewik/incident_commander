import { Navigate } from 'react-router-dom';
import { useAuth } from '../contexts/AuthContext';
import type { ReactNode } from 'react';

interface ProtectedRouteProps {
  children: ReactNode;
}

export default function ProtectedRoute({ children }: ProtectedRouteProps) {
  const { firebaseUser, loading, userData } = useAuth();
  
  console.log('[ProtectedRoute] Rendering - loading:', loading, 'firebaseUser:', !!firebaseUser, 'userData:', !!userData);
  console.log('[ProtectedRoute] User email:', firebaseUser?.email || 'none');
  console.log('[ProtectedRoute] Current path:', window.location.pathname);
  
  if (loading) {
    console.log('[ProtectedRoute] Still loading, showing loading screen');
    return (
      <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-purple-900 to-pink-900">
        <div className="text-white text-xl">Loading...</div>
      </div>
    );
  }

  if (!firebaseUser) {
    console.log('[ProtectedRoute] No user, redirecting to login');
    console.log('[ProtectedRoute] Stack trace:', new Error().stack);
    return <Navigate to="/login" replace />;
  }

  console.log('[ProtectedRoute] User authenticated, rendering children');
  console.log('[ProtectedRoute] About to render:', typeof children);
  return <>{children}</>;
}