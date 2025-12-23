import { useState, useEffect } from 'react';
import { useAuth } from '../contexts/AuthContext';
import { apiService } from '../services/api';
import type { Organization, JoinRequest } from '../services/api';

interface OrganizationsModalProps {
  onClose: () => void;
  onOpenMemberModal?: (orgId: string) => void;
}

export default function OrganizationsModal({ onClose, onOpenMemberModal }: OrganizationsModalProps) {
  const { createOrganization } = useAuth();
  const { userData } = useAuth();
  const [activeTab, setActiveTab] = useState<'create' | 'join' | 'my-orgs'>(() => {
    // Start with 'my-orgs' tab if user already has an active org; otherwise default to create
    return userData?.organization_id ? 'my-orgs' : 'create';
  });
  
  // Create tab state
  const [orgName, setOrgName] = useState('');
  const [creating, setCreating] = useState(false);
  
  // My Organizations tab state
  const [myOrganizations, setMyOrganizations] = useState<Organization[]>([]);
  const [loadingMyOrgs, setLoadingMyOrgs] = useState(false);
  const [hasMemberships, setHasMemberships] = useState(false);
  const [hasUserTabSelection, setHasUserTabSelection] = useState(false);
  
  // Join tab state
  const [searchQuery, setSearchQuery] = useState('');
  const [organizations, setOrganizations] = useState<Organization[]>([]);
  const [myRequests, setMyRequests] = useState<JoinRequest[]>([]);
  const [loading, setLoading] = useState(false);
  const [requestingOrgId, setRequestingOrgId] = useState<string | null>(null);
  
  // Shared state
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');


  useEffect(() => {
    // Always load pending requests on modal open if initial tab is 'join'
    if (activeTab === 'join') {
      loadMyRequests();
      searchOrganizations();
    }
    // Always load user orgs
    loadUserOrganizations();
  }, []);

  useEffect(() => {
    // When switching tabs, reload requests/orgs as needed
    loadUserOrganizations();
    if (activeTab === 'join') {
      loadMyRequests();
      searchOrganizations();
    }
  }, [activeTab]);

  const loadUserOrganizations = async () => {
    try {
      setLoadingMyOrgs(true);
      const orgs = await apiService.getMyOrganizations();
      setMyOrganizations(orgs || []);
      const hasOrgs = !!orgs && orgs.length > 0;
      setHasMemberships(hasOrgs);
      // If the user has memberships but the active tab isn't showing them yet, switch to the tab
      if (hasOrgs && activeTab === 'create' && !hasUserTabSelection) {
        setActiveTab('my-orgs');
      }
    } catch (err) {
      console.error('Failed to load user organizations:', err);
      setMyOrganizations([]);
      setHasMemberships(false);
    } finally {
      setLoadingMyOrgs(false);
    }
  };

  const loadMyRequests = async () => {
    try {
      const requests = await apiService.getJoinRequests();
      console.log('[OrganizationsModal] Loaded requests:', requests);
      console.log('[OrganizationsModal] Number of requests:', requests?.length);
      if (requests && requests.length > 0) {
        console.log('[OrganizationsModal] First request org_id:', requests[0].organization_id);
      }
      setMyRequests(prev => {
        // If there are any optimistic pending requests not returned by backend, keep them
        const optimisticPending = prev.filter(
          req => req.status === 'pending' && !requests.some(r => r.organization_id === req.organization_id && r.status === 'pending')
        );
        return [...requests, ...optimisticPending];
      });
    } catch (err) {
      console.error('Failed to load requests:', err);
      setMyRequests([]);
    }
  };

  const searchOrganizations = async () => {
    setLoading(true);
    try {
      const orgs = await apiService.searchOrganizations(searchQuery);
      setOrganizations(orgs || []);
    } catch (err: any) {
      setError(err.response?.data?.error || 'Failed to search organizations');
      setOrganizations([]);
    } finally {
      setLoading(false);
    }
  };

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault();
    searchOrganizations();
  };

  const handleCreateOrg = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setSuccess('');
    setCreating(true);

    try {
      await createOrganization(orgName);
      setSuccess('Organization created successfully!');
      setTimeout(() => {
        window.location.reload();
      }, 1500);
    } catch (err: any) {
      setError(err.response?.data?.error || err.message || 'Failed to create organization');
    } finally {
      setCreating(false);
    }
  };

  const handleJoinRequest = async (orgId: string) => {
    setError('');
    setSuccess('');
    setRequestingOrgId(orgId);

    try {
      const newRequest = await apiService.createJoinRequest(orgId);
      console.log('[OrganizationsModal] Join request created:', newRequest);
      setSuccess('Join request sent successfully!');

      // Optimistically add the new request to state and keep it until backend confirms otherwise
      setMyRequests(prev => {
        // Remove any previous pending request for this org (shouldn't be, but just in case)
        const filtered = prev.filter(req => !(req.organization_id === orgId && req.status === 'pending'));
        return [...filtered, newRequest];
      });

      // Wait a bit longer before reloading from backend to allow backend to update
      setTimeout(() => loadMyRequests(), 1500);
    } catch (err: any) {
      const errorMsg = err.response?.data?.error || 'Failed to send join request';
      setError(errorMsg);
      // If join request already exists, optimistically add a pending request to state
      if (errorMsg.toLowerCase().includes('already exists')) {
        setMyRequests(prev => {
          if (!prev.some(req => req.organization_id === orgId && req.status === 'pending')) {
            const now = new Date().toISOString();
            return [
              ...prev,
              {
                id: '',
                user_id: '',
                user_email: '',
                user_first_name: '',
                user_last_name: '',
                organization_id: orgId,
                status: 'pending',
                created_at: now,
                updated_at: now
              }
            ];
          }
          return prev;
        });
        setTimeout(() => loadMyRequests(), 1500);
      }
    } finally {
      setRequestingOrgId(null);
    }
  };

  const getRequestStatus = (orgId: string): JoinRequest | undefined => {
    // Only disable the button for pending requests
    const request = myRequests.find(req => req.organization_id === orgId && req.status === 'pending');
    return request;
  };

  const isUserMemberOfOrg = (orgId: string): boolean => {
    return myOrganizations.some(org => org.id === orgId);
  };

  return (
    // Overlay
    <div
      className="fixed inset-0 z-40 flex items-center justify-center"
      style={{ background: 'rgba(0,0,0,0.4)', backdropFilter: 'blur(2px)' }}
    >
      {/* Modal Container */}
      <div
        className="relative z-50 p-8 bg-white rounded-lg shadow-lg  w-[600px] max-h-[80vh] overflow-y-auto"
        style={{ minHeight: '350px' }}
      >
        <h2 className="text-3xl font-extrabold text-gray-900 mb-6 tracking-tight">Organizations</h2>
        
        {error && (
          <div className="bg-red-100 border border-red-400 text-red-800 px-5 py-4 rounded-lg mb-5 text-base font-semibold">
            {error}
          </div>
        )}
        
        {success && (
          <div className="bg-green-100 border border-green-400 text-green-800 px-5 py-4 rounded-lg mb-5 text-base font-semibold">
            {success}
          </div>
        )}

        {/* Tabs */}
        <div className="flex border-b-2 border-gray-200 mb-8 overflow-x-auto gap-2 pb-2">
          <button
            onClick={() => {
              setHasUserTabSelection(true);
              setActiveTab('create');
              setError('');
              setSuccess('');
            }}
            className={`flex-1 py-3 text-center font-semibold transition-colors whitespace-nowrap rounded-t-lg ${
              activeTab === 'create'
                ? 'border-b-2 border-purple-600 text-purple-400 bg-purple-50'
                : 'text-white hover:text-gray-700 bg-gray-100'
            }`}
          >
            Create
          </button>
          {(userData?.organization_id || hasMemberships) && (
            <button
              onClick={() => {
                setHasUserTabSelection(true);
                setActiveTab('my-orgs');
                setError('');
                setSuccess('');
              }}
              className={`flex-1 py-3 text-center font-semibold transition-colors whitespace-nowrap rounded-t-lg ${
                activeTab === 'my-orgs'
                  ? 'border-b-2 border-purple-600 text-purple-400 bg-purple-50'
                  : 'text-white hover:text-gray-700 bg-gray-100'
              }`}
            >
              My Orgs
            </button>
          )}
          <button
            onClick={() => {
              setHasUserTabSelection(true);
              setActiveTab('join');
              setError('');
              setSuccess('');
            }}
            className={`flex-1 py-3 text-center font-semibold transition-colors whitespace-nowrap rounded-t-lg ${
              activeTab === 'join'
                ? 'border-b-2 border-purple-600 text-purple-400 bg-purple-50'
                : 'text-white hover:text-gray-700 bg-gray-100'
            }`}
          >
            Join More
          </button>
        </div>

        {/* Create Tab Content */}
        {activeTab === 'create' && (
          <form onSubmit={handleCreateOrg}>
            <div className="mb-6">
              <label className="block text-gray-900 font-semibold mb-3">
                Organization Name
              </label>
              <input
                type="text"
                value={orgName}
                onChange={(e) => setOrgName(e.target.value)}
                placeholder="Enter organization name"
                required
                className="w-full px-4 py-3 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500 text-gray-900 bg-gray-50 placeholder-gray-400"
              />
            </div>
            <button
              type="submit"
              disabled={creating}
              className="w-full bg-gradient-to-r from-purple-700 to-pink-700 text-white py-3 rounded-lg hover:from-purple-800 hover:to-pink-800 disabled:opacity-50 font-bold text-lg mt-2 shadow"
            >
              {creating ? 'Creating...' : 'Create Organization'}
            </button>
          </form>
        )}

        {/* Join Tab Content */}
        {activeTab === 'join' && (
          <>
            <form onSubmit={handleSearch} className="mb-8">
              <div className="flex gap-3">
                <input
                  type="text"
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  placeholder="Search organizations..."
                  className="flex-1 px-4 py-3 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500 text-gray-900 bg-gray-50 placeholder-gray-400"
                />
                <button
                  type="submit"
                  disabled={loading}
                  className="bg-gradient-to-r from-purple-700 to-pink-700 text-white px-5 py-3 rounded-lg hover:from-purple-800 hover:to-pink-800 disabled:opacity-50 font-bold"
                >
                  {loading ? 'Searching...' : 'Search'}
                </button>
              </div>
            </form>

            <div>
              <h3 className="text-xl font-bold text-gray-900 mb-4">Available Organizations</h3>
              {organizations.length === 0 ? (
                <p className="text-gray-500">
                  {searchQuery ? 'No organizations found matching your search.' : 'Search to find organizations.'}
                </p>
              ) : (
                <div className="space-y-3">
                  {organizations.map((org) => {
                    const request = getRequestStatus(org.id);
                    const isMember = isUserMemberOfOrg(org.id);
                    return (
                      <div
                        key={org.id}
                        className="flex text-left text-white items-center justify-between p-4 bg-gray-50 rounded-lg border border-gray-200"
                      >
                        <div className="flex-1">
                          <p className="font-semibold text-gray-900 text-lg">{org.name}</p>
                          <p className="text-sm text-gray-500 mt-1">
                            Created: {new Date(org.created_at).toLocaleDateString()}
                          </p>
                        </div>
                        {isMember ? (
                          <button
                            disabled
                            className="px-5 py-2 rounded-lg text-sm font-semibold cursor-not-allowed bg-green-200 text-green-600"
                          >
                            Joined
                          </button>
                        ) : request ? (
                          <button
                            disabled
                            className={`px-5 py-2 rounded-lg text-sm font-semibold cursor-not-allowed ${
                              request.status === 'pending' 
                                ? 'bg-yellow-200 text-yellow-600' 
                                : request.status === 'approved'
                                ? 'bg-green-200 text-green-600'
                                : 'bg-red-200 text-red-600'
                            }`}
                          >
                            {request.status === 'pending' ? 'Pending' : request.status.charAt(0).toUpperCase() + request.status.slice(1)}
                          </button>
                        ) : (
                          <button
                            onClick={() => handleJoinRequest(org.id)}
                            disabled={requestingOrgId === org.id}
                            className="px-5 py-2 bg-purple-700 text-white rounded-lg hover:bg-purple-800 disabled:opacity-50 text-sm font-bold shadow"
                          >
                            {requestingOrgId === org.id ? 'Requesting...' : 'Request to Join'}
                          </button>
                        )}
                      </div>
                    );
                  })}
                </div>
              )}
            </div>
          </>
        )}

        {/* My Organizations Tab Content */}
        {activeTab === 'my-orgs' && (
          <>
            <h3 className="text-xl font-bold text-gray-900 mb-4">Your Organizations</h3>
            {loadingMyOrgs ? (
              <p className="text-gray-500">Loading...</p>
            ) : myOrganizations.length === 0 ? (
              <p className="text-gray-500">You are not a member of any organizations yet.</p>
            ) : (
              <div className="space-y-3">
                {myOrganizations.map((org) => (
                  <button
                    key={org.id}
                    onClick={() => onOpenMemberModal?.(org.id)}
                    className={`w-full flex items-center justify-between p-4 bg-gray-50 rounded-lg border hover:bg-gray-100 transition-colors cursor-pointer text-left text-white ${
                      org.id === userData?.organization_id ? 'border-purple-600 bg-purple-50 hover:bg-purple-100 border-2' : 'border-gray-200'
                    }`}
                  >
                    <div className="flex-1">
                      <p className="font-semibold text-white text-lg">{org.name}</p>
                      <p className="text-sm text-gray-500 mt-1">
                        {org.id === userData?.organization_id && <span className="font-bold text-green-600">Active • </span>}
                        Created: {new Date(org.created_at).toLocaleDateString()}
                      </p>
                    </div>
                    <svg className="w-5 h-5 text-gray-400 ml-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
                    </svg>
                  </button>
                ))}
              </div>
            )}
          </>
        )}

        <button
          className="mt-8 w-full py-2 px-4 bg-purple-700 text-white rounded focus:outline-none focus:ring-2 focus:ring-purple-400"
          onClick={onClose}
        >
          Close
        </button>
      </div>
    </div>
  );
}
