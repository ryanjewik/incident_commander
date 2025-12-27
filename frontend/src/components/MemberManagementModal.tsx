import { useState, useEffect } from 'react';
import type { FormEvent } from 'react';
import { useAuth } from '../contexts/AuthContext';
import { apiService } from '../services/api';
import type { User, JoinRequest, Organization } from '../services/api';

// Add types for Datadog secrets
interface DatadogSecrets {
  apiKey?: string;
  appKey?: string;
  webhookSecret?: string;
  clientId?: string;
  // clientSecret removed: webhookSecret is used as the single secret
  // encrypted blobs returned from backend (optional)
  encrypted_apiKey?: string;
  encrypted_appKey?: string;
  encrypted_apiKeyDek?: string;
}

interface DatadogSettings {
  systemMetrics: boolean;
  alertStatus: boolean;
  activityFeed: boolean;
  securitySignals: boolean;
  liveLogs: boolean;
}
// Extend Organization type to include optional datadog_secrets
interface OrganizationWithDatadog extends Organization {
  datadog_secrets?: DatadogSecrets;
  datadog_settings?: DatadogSettings;
}

// Simple Toggle component
function Toggle({ label, checked, onChange, disabled }: { label: string; checked: boolean; onChange: (v: boolean) => void; disabled?: boolean }) {
  return (
    <label className="flex items-center justify-between bg-transparent px-1 py-1">
      <span className="text-sm text-gray-800">{label}</span>
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        disabled={disabled}
        onClick={() => !disabled && onChange(!checked)}
        className={`relative inline-flex items-center h-8 w-14 rounded-full transition-colors duration-250 ease-in-out focus:outline-none focus:ring-2 focus:ring-offset-2 ${checked ? 'bg-gradient-to-r from-purple-600 to-pink-500 shadow-md' : 'bg-gray-300'} ${disabled ? 'opacity-50 cursor-not-allowed' : ''}`}
      >
        {/* sliding knob */}
        <span className={`absolute left-1 top-1 w-6 h-6 bg-white rounded-full shadow transform transition-transform duration-300 ease-in-out ${checked ? 'translate-x-6 rotate-[12deg]' : 'translate-x-0 rotate-0'}`} />
        {/* subtle glow when on */}
        <span className={`absolute -left-1 top-0 bottom-0 w-3 rounded-full transition-opacity duration-300 ${checked ? 'opacity-30 bg-pink-400 blur-sm' : 'opacity-0'}`} />
      </button>
    </label>
  );
}

interface MemberManagementModalProps {
  onClose: () => void;
  orgId?: string; // Optional: if provided, show this org instead of active org
  onBack?: () => void; // Optional: callback to go back to previous view
}

export default function MemberManagementModal({ onClose, orgId, onBack }: MemberManagementModalProps) {
  const [email, setEmail] = useState('');
  const [role, setRole] = useState('member');
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [loading, setLoading] = useState(false);
  const [members, setMembers] = useState<User[]>([]);
  const [loadingMembers, setLoadingMembers] = useState(true);
  const [removingUserId, setRemovingUserId] = useState<string | null>(null);
  const [confirmRemove, setConfirmRemove] = useState<string | null>(null);
  const [showLeaveConfirm, setShowLeaveConfirm] = useState(false);
  const [leaving, setLeaving] = useState(false);
  const [joinRequests, setJoinRequests] = useState<JoinRequest[]>([]);
  const [loadingRequests, setLoadingRequests] = useState(true);
  const [processingRequestId, setProcessingRequestId] = useState<string | null>(null);
  const [organization, setOrganization] = useState<OrganizationWithDatadog | null>(null);
  const [loadingOrg, setLoadingOrg] = useState(true);
  const { addMember, removeMember, leaveOrganization, userData } = useAuth();
  const [isAdmin, setIsAdmin] = useState(false);
  // Datadog secrets state
  const [datadogSecrets, setDatadogSecrets] = useState<DatadogSecrets>({
    apiKey: '',
    appKey: '',
    webhookSecret: '',
    clientId: '',
  });
  const [savingSecrets, setSavingSecrets] = useState(false);
  const [datadogSettings, setDatadogSettings] = useState<DatadogSettings>({
    systemMetrics: false,
    alertStatus: false,
    activityFeed: false,
    securitySignals: false,
    liveLogs: false,
  });
  const [toast, setToast] = useState<{ message: string; type: 'success' | 'error' } | null>(null);
  
  // Use provided orgId or fallback to user's active organization
  const currentOrgId = orgId || userData?.organization_id || '';

  useEffect(() => {
    // Clear previous data immediately when org changes to avoid displaying stale data
    setMembers([]);
    setLoadingMembers(true);
    setJoinRequests([]);
    setLoadingRequests(true);
    setOrganization(null);
    setLoadingOrg(true);
    setIsAdmin(false);

    loadMembers();
    loadOrganization();
  }, [currentOrgId]);

  // Always load join requests for admins after admin status/org is determined
  useEffect(() => {
    if (isAdmin && currentOrgId) {
      setLoadingRequests(true);
      loadJoinRequests();
    } else {
      setJoinRequests([]);
      setLoadingRequests(false);
    }
  }, [isAdmin, currentOrgId]);

  const loadOrganization = async () => {
    try {
      const orgs = await apiService.searchOrganizations('');
      const userOrg = orgs.find((org: Organization) => org.id === currentOrgId);
      setOrganization(userOrg || null);
      // Optionally, load existing Datadog secrets for this org if available
      const orgWithDatadog = userOrg as OrganizationWithDatadog;
      console.debug('[MemberManagementModal] loaded org:', orgWithDatadog);
      if (orgWithDatadog) {
        // load secrets if present
        if (orgWithDatadog.datadog_secrets) {
          // If any secret is present in the stored object, show masked placeholders for all three
          const hasApiEnc = !!orgWithDatadog.datadog_secrets.encrypted_apiKey || !!orgWithDatadog.datadog_secrets.apiKey || false;
          const hasAppEnc = !!orgWithDatadog.datadog_secrets.encrypted_appKey || !!orgWithDatadog.datadog_secrets.appKey || false;
          const hasWebhook = !!orgWithDatadog.datadog_secrets.webhookSecret || false;
          const hasClientId = !!(orgWithDatadog.datadog_secrets as any).client_id || false;
          if (hasApiEnc || hasAppEnc || hasWebhook) {
            setDatadogSecrets({ apiKey: '********', appKey: '********', webhookSecret: '********', clientId: hasClientId ? ((orgWithDatadog.datadog_secrets as any).client_id as string) : '' });
          } else {
            setDatadogSecrets({ apiKey: '', appKey: '', webhookSecret: '', clientId: '' });
          }
        } else {
          setDatadogSecrets({ apiKey: '', appKey: '', webhookSecret: '', clientId: '' });
        }

        // load settings independently of secrets presence
        if (orgWithDatadog.datadog_settings) {
          const toBool = (v: any) => {
            if (typeof v === 'boolean') return v;
            if (typeof v === 'string') return v.toLowerCase() === 'true';
            if (typeof v === 'number') return v !== 0;
            return false;
          };
          setDatadogSettings({
            systemMetrics: toBool(orgWithDatadog.datadog_settings.systemMetrics),
            alertStatus: toBool(orgWithDatadog.datadog_settings.alertStatus),
            activityFeed: toBool(orgWithDatadog.datadog_settings.activityFeed),
            securitySignals: toBool(orgWithDatadog.datadog_settings.securitySignals),
            liveLogs: toBool(orgWithDatadog.datadog_settings.liveLogs),
          });
        } else {
          setDatadogSettings({ systemMetrics: false, alertStatus: false, activityFeed: false, securitySignals: false, liveLogs: false });
        }
      } else {
        // No stored datadog config — keep inputs empty and toggles at their initial (false) state
          setDatadogSecrets({ apiKey: '', appKey: '', webhookSecret: '', clientId: '' });
        setDatadogSettings({ systemMetrics: false, alertStatus: false, activityFeed: false, securitySignals: false, liveLogs: false });
      }
    } catch (err) {
      console.error('Failed to load organization:', err);
      setOrganization(null);
    } finally {
      setLoadingOrg(false);
    }
  };

  const loadMembers = async () => {
    try {
      setLoadingMembers(true);
      const data = await apiService.getOrgUsers(currentOrgId);
      setMembers(data || []);
      const self = data?.find((m: User) => m.id === userData?.id);
      setIsAdmin(self?.role === 'admin');
    } catch (err) {
      console.error('Failed to load members:', err);
      setMembers([]);
      setIsAdmin(false);
      setLoadingRequests(false);
    } finally {
      setLoadingMembers(false);
    }
  };

  const loadJoinRequests = async () => {
    try {
      let requests: JoinRequest[] = [];
      if (isAdmin && currentOrgId) {
        requests = await apiService.getOrgJoinRequests(currentOrgId);
      } else {
        requests = await apiService.getJoinRequests();
      }
      setJoinRequests(requests || []);
    } catch (err) {
      console.error('Failed to load join requests:', err);
      setJoinRequests([]);
    } finally {
      setLoadingRequests(false);
    }
  };

  // Save Datadog secrets handler (move to component scope)
  const handleSaveDatadogSecrets = async () => {
    setError('');
    setSuccess('');
    setSavingSecrets(true);
    try {
      // Build payload: include settings always, but only include keys/secrets when user provided
      // This prevents overwriting existing encrypted values with empty strings when adjusting toggles
      const payload: any = { settings: datadogSettings };
      const apiKey = datadogSecrets.apiKey?.trim();
      const appKey = datadogSecrets.appKey?.trim();
      const webhookSecret = datadogSecrets.webhookSecret?.trim();
      const clientId = datadogSecrets.clientId?.trim();
      // If input is non-empty and not the masked placeholder, include it
      if (apiKey && apiKey !== '********') payload.apiKey = apiKey;
      if (appKey && appKey !== '********') payload.appKey = appKey;
      if (webhookSecret && webhookSecret !== '********') payload.webhookSecret = webhookSecret;
      if (clientId && clientId !== '********') payload.client_id = clientId;

      await apiService.saveDatadogSecrets(currentOrgId, payload as any);
      setSuccess('Datadog secrets and settings saved successfully!');
      setToast({ message: 'Datadog settings saved', type: 'success' });
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to save Datadog secrets');
      setToast({ message: 'Failed to save Datadog settings', type: 'error' });
    } finally {
      setSavingSecrets(false);
    }
  };

  // auto-hide toast
  useEffect(() => {
    if (!toast) return;
    const id = setTimeout(() => setToast(null), 3000);
    return () => clearTimeout(id);
  }, [toast]);

  const handleApproveRequest = async (requestId: string) => {
    setError('');
    setSuccess('');
    setProcessingRequestId(requestId);

    try {
      await apiService.approveJoinRequest(currentOrgId, requestId);
      setSuccess('Join request approved!');
      await loadJoinRequests();
      await loadMembers();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to approve request');
    } finally {
      setProcessingRequestId(null);
    }
  };

  const handleRejectRequest = async (requestId: string) => {
    setError('');
    setSuccess('');
    setProcessingRequestId(requestId);

    try {
      await apiService.rejectJoinRequest(requestId);
      setSuccess('Join request rejected');
      await loadJoinRequests();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to reject request');
    } finally {
      setProcessingRequestId(null);
    }
  };

  const handleCleanupRequests = async () => {
    setError('');
    setSuccess('');
    try {
      const result = await apiService.cleanupJoinRequests();
      setSuccess(`Cleaned up ${result.deleted} legacy join request(s).`);
      await loadJoinRequests();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to clean up join requests');
    }
  };

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setError('');
    setSuccess('');
    setLoading(true);

    try {
      await addMember(email, role);
      setSuccess('Member added successfully!');
      setEmail('');
      setRole('member');
      await loadMembers();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to add member');
    } finally {
      setLoading(false);
    }
  };

  const handleRemoveMember = async (userId: string) => {
    setError('');
    setSuccess('');
    setRemovingUserId(userId);

    try {
      await removeMember(userId);
      setSuccess('Member removed successfully!');
      await loadMembers();
      setConfirmRemove(null);
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to remove member');
    } finally {
      setRemovingUserId(null);
    }
  };

  const handleLeaveOrganization = async () => {
    setLeaving(true);
    setError('');
    setSuccess('');
    
    try {
      await leaveOrganization();
      onClose();
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to leave organization');
    } finally {
      setLeaving(false);
      setShowLeaveConfirm(false);
    }
  };

  return (
    <div className="fixed inset-0 flex items-center justify-center z-50" style={{ background: 'rgba(0,0,0,0.4)', backdropFilter: 'blur(2px)' }}>
      <div className="bg-white p-8 rounded-lg shadow-2xl w-[600px] max-h-[80vh] overflow-y-auto">
        <div className="mb-4">
          <div className="flex items-center gap-3 mb-2">
            {onBack && (
              <button
                onClick={onBack}
                className="text-white hover:text-gray-800 transition-colors"
                title="Back to Organizations"
              >
                <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
                </svg>
              </button>
            )}
            <h2 className="text-2xl font-bold text-gray-800">
              {isAdmin ? 'Manage Organization' : 'Organization'}
            </h2>
          </div>
          {loadingOrg ? (
            <p className="text-sm text-gray-500 mt-1">Loading...</p>
          ) : organization ? (
            <p className="text-lg text-purple-600 font-semibold mt-1">{organization.name}</p>
          ) : (
            <p className="text-sm text-gray-500 mt-1">No organization</p>
          )}
        </div>
        
        {error && (
          <div className="bg-red-100 border border-red-400 text-red-700 px-4 py-3 rounded mb-4">
            {error}
          </div>
        )}
        
        {success && (
          <div className="bg-green-100 border border-green-400 text-green-700 px-4 py-3 rounded mb-4">
            {success}
          </div>
        )}

        {isAdmin && (
          <>
            <div className="border-t pt-4 mb-6">
              <h3 className="text-lg font-semibold text-gray-800 mb-3">Datadog Integration</h3>
              <div className="space-y-3">
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">Datadog API Key</label>
                  <input
                    type="password"
                    value={datadogSecrets.apiKey}
                    onChange={e => setDatadogSecrets(s => ({ ...s, apiKey: e.target.value }))}
                    className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-purple-500"
                    placeholder="********"
                    autoComplete="off"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">Datadog APP Key</label>
                  <input
                    type="password"
                    value={datadogSecrets.appKey}
                    onChange={e => setDatadogSecrets(s => ({ ...s, appKey: e.target.value }))}
                    className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-purple-500"
                    placeholder="********"
                    autoComplete="off"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">Webhook Secret</label>
                  <input
                    type="password"
                    value={datadogSecrets.webhookSecret}
                    onChange={e => setDatadogSecrets(s => ({ ...s, webhookSecret: e.target.value }))}
                    className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-purple-500"
                    placeholder="********"
                    autoComplete="off"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-1">Webhook Client ID</label>
                  <input
                    type="text"
                    value={datadogSecrets.clientId}
                    onChange={e => setDatadogSecrets(s => ({ ...s, clientId: e.target.value }))}
                    className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-purple-500"
                    placeholder="Optional client id for OAuth"
                    autoComplete="off"
                  />
                </div>
                <div className="pt-2">
                  <h4 className="text-sm font-medium text-gray-800 mb-2">Dashboard Toggles</h4>
                  <div className="grid grid-cols-1 gap-3">
                    <Toggle label="System Metrics" checked={datadogSettings.systemMetrics} onChange={v => setDatadogSettings(s => ({ ...s, systemMetrics: v }))} disabled={loadingOrg} />
                    <Toggle label="Alert Status" checked={datadogSettings.alertStatus} onChange={v => setDatadogSettings(s => ({ ...s, alertStatus: v }))} disabled={loadingOrg} />
                    <Toggle label="Activity Feed" checked={datadogSettings.activityFeed} onChange={v => setDatadogSettings(s => ({ ...s, activityFeed: v }))} disabled={loadingOrg} />
                    <Toggle label="Security Signals" checked={datadogSettings.securitySignals} onChange={v => setDatadogSettings(s => ({ ...s, securitySignals: v }))} disabled={loadingOrg} />
                    <Toggle label="Live Logs" checked={datadogSettings.liveLogs} onChange={v => setDatadogSettings(s => ({ ...s, liveLogs: v }))} disabled={loadingOrg} />
                  </div>
                </div>
                <button
                  type="button"
                  onClick={handleSaveDatadogSecrets}
                  disabled={savingSecrets}
                  className="w-full bg-gradient-to-r from-purple-600 to-pink-600 text-white py-2 rounded-md hover:from-purple-700 hover:to-pink-700 disabled:opacity-50 font-medium"
                >
                  {savingSecrets ? 'Saving...' : 'Save Datadog Secrets'}
                </button>
              </div>
            </div>
            <form onSubmit={handleSubmit} className="space-y-4 mb-6">
              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                  Member Email
                </label>
                <input
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  required
                  className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-purple-500"
                  placeholder="user@example.com"
                />
              </div>

              <div>
                <label className="block text-sm font-medium text-gray-700 mb-1">
                  Role
                </label>
                <select
                  value={role}
                  onChange={(e) => setRole(e.target.value)}
                  className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-purple-500"
                >
                  <option value="member">Member</option>
                  <option value="admin">Admin</option>
                </select>
              </div>

              <button
                type="submit"
                disabled={loading}
                className="w-full bg-gradient-to-r from-purple-600 to-pink-600 text-white py-2 rounded-md hover:from-purple-700 hover:to-pink-700 disabled:opacity-50 font-medium"
              >
                {loading ? 'Adding...' : 'Add Member'}
              </button>
            </form>
          </>
        )}

        {isAdmin && (
          <div className="border-t pt-4 mb-6">
            <div className="flex items-center justify-between mb-3">
              <h3 className="text-lg font-semibold text-gray-800">Join Requests</h3>
              <button
                type="button"
                onClick={handleCleanupRequests}
                className="px-3 py-1 text-xs bg-gray-200 text-gray-800 rounded hover:bg-gray-300 text-white"
              >
                Clean up legacy
              </button>
            </div>
            {loadingRequests ? (
              <p className="text-gray-600">Loading requests...</p>
            ) : joinRequests.length === 0 ? (
              <p className="text-gray-600 text-sm">No pending join requests</p>
            ) : (
              <div className="space-y-2">
                {joinRequests.map((request) => (
                  <div
                    key={request.id}
                    className="flex items-center justify-between p-3 bg-yellow-50 border border-yellow-200 rounded-md"
                  >
                    <div className="flex-1">
                      <p className="font-medium text-gray-800">
                        {request.user_first_name} {request.user_last_name}
                      </p>
                      <p className="text-sm text-gray-600">{request.user_email}</p>
                      <p className="text-xs text-gray-500">
                        Requested: {new Date(request.created_at).toLocaleDateString()}
                      </p>
                    </div>
                    <div className="flex gap-2">
                      <button
                        onClick={() => handleApproveRequest(request.id)}
                        disabled={processingRequestId === request.id}
                        className="px-3 py-1 text-sm bg-green-600 text-white rounded hover:bg-green-700 disabled:opacity-50 font-medium"
                      >
                        {processingRequestId === request.id ? 'Processing...' : 'Approve'}
                      </button>
                      <button
                        onClick={() => handleRejectRequest(request.id)}
                        disabled={processingRequestId === request.id}
                        className="px-3 py-1 text-sm bg-red-600 text-white rounded hover:bg-red-700 disabled:opacity-50 font-medium"
                      >
                        Reject
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}

        <div className={isAdmin ? "border-t pt-4" : ""}>
          <h3 className="text-lg font-semibold text-gray-800 mb-3">
            {isAdmin ? 'Current Members' : 'Organization Members'}
          </h3>
          {loadingMembers ? (
            <p className="text-gray-600">Loading members...</p>
          ) : members.length === 0 ? (
            <p className="text-gray-600">No members yet.</p>
          ) : (
            <div className="space-y-2">
              {members.map((member) => (
                <div
                  key={member.id}
                  className="flex items-center justify-between p-3 bg-gray-50 rounded-md"
                >
                  <div className="flex-1">
                    <p className="font-medium text-gray-800 text-left">
                      {member.first_name} {member.last_name}
                    </p>
                    <p className="text-sm text-gray-600 text-left">{member.email}</p>
                  </div>
                  <div className="flex items-center gap-2">
                    <span className={`px-3 py-1 rounded-full text-xs font-semibold ${
                      member.role === 'admin' 
                        ? 'bg-purple-200 text-purple-800' 
                        : 'bg-gray-200 text-gray-800'
                    }`}>
                      {member.role || 'member'}
                    </span>
                    {isAdmin && userData?.id && member.id !== userData.id && (
                      confirmRemove === member.id ? (
                        <div className="flex gap-1">
                          <button
                            onClick={() => handleRemoveMember(member.id)}
                            disabled={removingUserId === member.id}
                            className="px-2 py-1 text-xs bg-red-600 text-white rounded hover:bg-red-700 disabled:opacity-50"
                          >
                            {removingUserId === member.id ? 'Removing...' : 'Confirm'}
                          </button>
                          <button
                            onClick={() => setConfirmRemove(null)}
                            disabled={removingUserId === member.id}
                            className="px-2 py-1 text-xs bg-gray-400 text-white rounded hover:bg-gray-500 disabled:opacity-50"
                          >
                            Cancel
                          </button>
                        </div>
                      ) : (
                        <button
                          onClick={() => setConfirmRemove(member.id)}
                          className="px-3 py-1 text-xs bg-red-500 text-white rounded hover:bg-red-600"
                        >
                          Remove
                        </button>
                      )
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>

        {!isAdmin && (
          <button
            onClick={() => setShowLeaveConfirm(true)}
            className="mt-4 w-full bg-red-500 text-white py-2 rounded-md hover:bg-red-600 font-medium"
          >
            Leave Organization
          </button>
        )}

        <button
          onClick={onClose}
          className="mt-4 w-full bg-gray-300 text-white py-2 rounded-md hover:bg-gray-400 font-medium"
        >
          Close
        </button>
      </div>

      {showLeaveConfirm && (
        <div className="fixed inset-0 flex items-center justify-center z-60" style={{ background: 'rgba(0,0,0,0.6)', backdropFilter: 'blur(2px)' }}>
          <div className="bg-white p-8 rounded-lg shadow-2xl max-w-md">
            <h2 className="text-2xl font-bold text-gray-800 mb-4">Leave Organization?</h2>
            <p className="text-gray-600 mb-6">
              Are you sure you want to leave this organization? You will lose access to all organization resources and data.
            </p>
            <div className="flex gap-4">
              <button
                onClick={handleLeaveOrganization}
                disabled={leaving}
                className="flex-1 bg-red-600 text-white py-2 rounded-md hover:bg-red-700 disabled:opacity-50 font-medium"
              >
                {leaving ? 'Leaving...' : 'Yes, Leave'}
              </button>
              <button
                onClick={() => setShowLeaveConfirm(false)}
                disabled={leaving}
                className="flex-1 bg-gray-300 text-gray-700 py-2 rounded-md hover:bg-gray-400 disabled:opacity-50 font-medium"
              >
                Cancel
              </button>
            </div>
          </div>
        </div>
      )}
      {toast && (
        <div className="fixed top-6 right-6 z-60">
          <div className={`${toast?.type === 'success' ? 'bg-green-600' : 'bg-red-600'} text-white px-4 py-2 rounded shadow-md`}>
            {toast?.message}
          </div>
        </div>
      )}
    </div>
  );
}