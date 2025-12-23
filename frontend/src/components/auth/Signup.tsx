import { useState, useMemo } from 'react';
import type { FormEvent } from 'react';
import { useAuth } from '../../contexts/AuthContext';
import PasswordRequirements, { validatePassword } from '../PasswordRequirements';

interface SignupProps {
  onSwitchToLogin?: () => void;
}

export default function Signup({ onSwitchToLogin }: SignupProps) {
  const [firstName, setFirstName] = useState('');
  const [lastName, setLastName] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [showPasswordReqs, setShowPasswordReqs] = useState(false);
  const { signUp } = useAuth();

  const emailValid = useMemo(() => {
    const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
    return email === '' || emailRegex.test(email);
  }, [email]);

  const passwordValidation = useMemo(() => validatePassword(password), [password]);

  const formValid = useMemo(() => {
    return (
      firstName.trim() !== '' &&
      lastName.trim() !== '' &&
      emailValid &&
      email !== '' &&
      passwordValidation.isValid &&
      password === confirmPassword
    );
  }, [firstName, lastName, emailValid, email, passwordValidation.isValid, password, confirmPassword]);

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    
    // Validate all requirements before submission
    if (!formValid) {
      if (!passwordValidation.isValid) {
        return setError('Password does not meet all requirements');
      }
      if (password !== confirmPassword) {
        return setError('Passwords do not match');
      }
      if (!emailValid) {
        return setError('Please enter a valid email address');
      }
      return setError('Please fill in all fields correctly');
    }

    setError('');
    setLoading(true);

    try {
      await signUp(email, password, firstName, lastName);
      // Don't navigate here - let AuthPage handle redirect after auth state updates
    } catch (err: any) {
      setError(err.message || 'Failed to create account');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="h-full w-full flex items-center justify-center bg-gradient-to-br from-purple-900 to-pink-900">
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
              First Name
            </label>
            <input
              type="text"
              value={firstName}
              onChange={(e) => setFirstName(e.target.value)}
              required
              className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-purple-500"
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">
              Last Name
            </label>
            <input
              type="text"
              value={lastName}
              onChange={(e) => setLastName(e.target.value)}
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
              className={`w-full px-3 py-2 border rounded-md focus:outline-none focus:ring-2 ${
                emailValid
                  ? 'border-gray-300 focus:ring-purple-500'
                  : 'border-red-500 focus:ring-red-500'
              }`}
            />
            {!emailValid && email !== '' && (
              <p className="text-xs text-red-600 mt-1">Please enter a valid email address</p>
            )}
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">
              Password
            </label>
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              onFocus={() => setShowPasswordReqs(true)}
              required
              className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-purple-500"
            />
            <PasswordRequirements password={password} show={showPasswordReqs && password !== ''} />
          </div>

          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">
              Confirm Password
            </label>
            <input
              type="password"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
              required
              className={`w-full px-3 py-2 border rounded-md focus:outline-none focus:ring-2 ${
                confirmPassword === '' || password === confirmPassword
                  ? 'border-gray-300 focus:ring-purple-500'
                  : 'border-red-500 focus:ring-red-500'
              }`}
            />
            {confirmPassword !== '' && password !== confirmPassword && (
              <p className="text-xs text-red-600 mt-1">Passwords do not match</p>
            )}
          </div>

          <button
            type="submit"
            disabled={loading || !formValid}
            className="w-full bg-gradient-to-r from-purple-600 to-pink-600 text-white py-2 rounded-md hover:from-purple-700 hover:to-pink-700 disabled:opacity-50 font-medium"
          >
            {loading ? 'Creating account...' : 'Sign Up'}
          </button>
        </form>

        <p className="mt-4 text-center text-sm text-gray-600">
          Already have an account?{' '}
          <button
            onClick={onSwitchToLogin}
            className="text-purple-600 hover:text-purple-800 font-medium"
          >
            Sign in
          </button>
        </p>
      </div>
    </div>
  );
}