import { useMemo } from 'react';

interface PasswordRequirementsProps {
  password: string;
  show?: boolean;
}

export interface PasswordValidation {
  minLength: boolean;
  maxLength: boolean;
  hasUpperCase: boolean;
  hasLowerCase: boolean;
  hasNumber: boolean;
  hasSpecialChar: boolean;
  isValid: boolean;
}

export const validatePassword = (password: string): PasswordValidation => {
  const minLength = password.length >= 8;
  const maxLength = password.length <= 30;
  const hasUpperCase = /[A-Z]/.test(password);
  const hasLowerCase = /[a-z]/.test(password);
  const hasNumber = /[0-9]/.test(password);
  const hasSpecialChar = /[!@#$%^&*()_+\-=\[\]{};':"\\|,.<>\/?]/.test(password);

  return {
    minLength,
    maxLength,
    hasUpperCase,
    hasLowerCase,
    hasNumber,
    hasSpecialChar,
    isValid: minLength && maxLength && hasUpperCase && hasLowerCase && hasNumber && hasSpecialChar,
  };
};

export default function PasswordRequirements({ password, show = true }: PasswordRequirementsProps) {
  const validation = useMemo(() => validatePassword(password), [password]);

  if (!show) return null;

  const requirements = [
    { label: 'At least 8 characters', met: validation.minLength },
    { label: 'No more than 128 characters', met: validation.maxLength },
    { label: 'One uppercase letter', met: validation.hasUpperCase },
    { label: 'One lowercase letter', met: validation.hasLowerCase },
    { label: 'One number', met: validation.hasNumber },
    { label: 'One special character (!@#$%^&*...)', met: validation.hasSpecialChar },
  ];

  return (
    <div className="mt-2 p-3 bg-gray-50 rounded-md border border-gray-200">
      <p className="text-xs font-semibold text-gray-700 mb-2">Password Requirements:</p>
      <ul className="space-y-1">
        {requirements.map((req, index) => (
          <li key={index} className="flex items-center text-xs">
            {req.met ? (
              <svg className="w-4 h-4 mr-2 text-green-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
              </svg>
            ) : (
              <svg className="w-4 h-4 mr-2 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <circle cx="12" cy="12" r="10" strokeWidth={2} />
              </svg>
            )}
            <span className={req.met ? 'text-green-700' : 'text-gray-600'}>{req.label}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}
