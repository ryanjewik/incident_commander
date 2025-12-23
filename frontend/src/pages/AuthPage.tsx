import { useState, useEffect } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { useAuth } from '../contexts/AuthContext';
import Login from '../components/auth/Login';
import Signup from '../components/auth/Signup';

export default function AuthPage() {
  const location = useLocation();
  const navigate = useNavigate();
  const { firebaseUser, loading } = useAuth();
  const [mode, setMode] = useState<'login' | 'signup'>('login');

  useEffect(() => {
    if (location.pathname === '/signup') {
      setMode('signup');
    } else {
      setMode('login');
    }
  }, [location.pathname]);

  // Redirect authenticated users to dashboard
  useEffect(() => {
    if (!loading && firebaseUser) {
      navigate('/', { replace: true });
    }
  }, [firebaseUser, loading, navigate]);

  return (
    <div className="h-full w-full">
      {mode === 'login' ? (
        <Login onSwitchToSignup={() => setMode('signup')} />
      ) : (
        <Signup onSwitchToLogin={() => setMode('login')} />
      )}
    </div>
  );
}