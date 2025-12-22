import { useState, useEffect } from 'react';
import { useLocation } from 'react-router-dom';
import Login from '../components/auth/Login';
import Signup from '../components/auth/Signup';

export default function AuthPage() {
  const location = useLocation();
  const [mode, setMode] = useState<'login' | 'signup'>('login');

  useEffect(() => {
    if (location.pathname === '/signup') {
      setMode('signup');
    } else {
      setMode('login');
    }
  }, [location.pathname]);

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