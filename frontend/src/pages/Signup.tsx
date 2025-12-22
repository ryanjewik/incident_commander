import { useState } from 'react';
import type { FormEvent } from 'react';
import { useNavigate, Link } from 'react-router-dom';
import { apiService } from '../services/api';
import { useAuth } from '../contexts/AuthContext';

export default function Signup() {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const { signIn } = useAuth();
  const navigate = useNavigate();

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    console.log('[Signup] Form submitted');
    setError('');
    setLoading(true);

    try {
      console.log('[Signup] Starting signup process for:', email);
      
      console.log('[Signup] Creating user via API');
      const user = await apiService.createUser(email, password, displayName);
      console.log('[Signup] User created successfully:', user);
      
      console.log('[Signup] Waiting 1000ms before sign in');
      await new Promise(resolve => setTimeout(resolve, 1000));
      
      console.log('[Signup] Calling signIn');
      await signIn(email, password);
      console.log('[Signup] SignIn completed successfully');
      
      // Additional wait to ensure auth state is fully propagated
      console.log('[Signup] Waiting 500ms before navigation');
      await new Promise(resolve => setTimeout(resolve, 500));
      
      console.log('[Signup] Navigating to /');
      navigate('/', { replace: true });
      console.log('[Signup] Navigation called');
    } catch (err: any) {
      console.error('[Signup] Error during signup:', err);
      console.error('[Signup] Error details:', {
        response: err.response?.data,
        message: err.message,
        stack: err.stack
      });
      setError(err.response?.data?.error || err.message || 'Failed to create account');
    } finally {
      console.log('[Signup] Setting loading to false');
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-purple-900 to-pink-900">
      <div className="bg-white p-8 rounded-lg shadow-2xl w-96">
        <h2 className="text-3xl font-bold text-center mb-6 text-gray-800">
          Create Account
        </h2>
        
        {error && (
          <div className="bg-red-100 border border-red-400 text-red-700 px-4 py-3 rounded mb-4">
            {error}
          </div>
        )}

        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">
              Display Name
            </label>
            <input
              type="text"
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
              required
              className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-purple-500"
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">
              Email
            </label>
            <input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
              className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-purple-500"
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">
              Password
            </label>
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
              minLength={6}
              className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-purple-500"
            />
          </div>

          <button
            type="submit"
            disabled={loading}
            className="w-full bg-gradient-to-r from-purple-600 to-pink-600 text-white py-2 rounded-md hover:from-purple-700 hover:to-pink-700 disabled:opacity-50 font-medium"
          >
            {loading ? 'Creating account...' : 'Sign Up'}
          </button>
        </form>

        <p className="mt-4 text-center text-sm text-gray-600">
          Already have an account?{' '}
          <Link to="/login" className="text-purple-600 hover:text-purple-800 font-medium">
            Sign in
          </Link>
        </p>
      </div>
    </div>
  );
}