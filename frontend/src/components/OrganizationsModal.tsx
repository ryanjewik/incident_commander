import { useState, useEffect } from 'react';
import { useAuth } from '../contexts/AuthContext';
import { apiService } from '../services/api';
import type { Organization, JoinRequest } from '../services/api';

interface OrganizationsModalProps {
  onClose: () => void;
}

export default function OrganizationsModal({ onClose }: OrganizationsModalProps) {
  const { createOrganization } = useAuth();
  const [activeTab, setActiveTab] = useState<'create' | 'join'>('create');
  
  // Create tab state
  const [orgName, setOrgName] = useState('');
  const [creating, setCreating] = useState(false);
  
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
    loadMyRequests();
    if (activeTab === 'join') {
      searchOrganizations();
    }
  }, [activeTab]);

  const loadMyRequests = async () => {
    try {
      const requests = await apiService.getJoinRequests();
      console.log('[OrganizationsModal] Loaded requests:', requests);
      console.log('[OrganizationsModal] Number of requests:', requests?.length);
      if (requests && requests.length > 0) {
        console.log('[OrganizationsModal] First request org_id:', requests[0].organization_id);
      }
      setMyRequests(requests || []);
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
      
      // Add the new request to state immediately
      setMyRequests(prev => [...prev, newRequest]);
      
      // Also reload from API to ensure sync
      setTimeout(() => loadMyRequests(), 500);
    } catch (err: any) {
      setError(err.response?.data?.error || 'Failed to send join request');
    } finally {
      setRequestingOrgId(null);
    }
  };

  const getRequestStatus = (orgId: string): JoinRequest | undefined => {
    // Only disable the button for pending requests
    const request = myRequests.find(req => req.organization_id === orgId && req.status === 'pending');
    return request;
  };

  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
      <div className="bg-white p-8 rounded-lg shadow-2xl w-[600px] max-h-[80vh] overflow-y-auto">
        <h2 className="text-2xl font-bold text-gray-800 mb-4">Organizations</h2>
        
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

        {/* Tabs */}
        <div className="flex border-b mb-6">
          <button
            onClick={() => {
              setActiveTab('create');
              setError('');
              setSuccess('');
            }}
            className={`flex-1 py-2 text-center font-medium transition-colors ${
              activeTab === 'create'
                ? 'border-b-2 border-purple-600 text-purple-600'
                : 'text-gray-500 hover:text-gray-700'
            }`}
          >
            Create Organization
          </button>
          <button
            onClick={() => {
              setActiveTab('join');
              setError('');
              setSuccess('');
            }}
            className={`flex-1 py-2 text-center font-medium transition-colors ${
              activeTab === 'join'
                ? 'border-b-2 border-purple-600 text-purple-600'
                : 'text-gray-500 hover:text-gray-700'
            }`}
          >
            Join Organization
          </button>
        </div>

        {/* Create Tab Content */}
        {activeTab === 'create' && (
          <form onSubmit={handleCreateOrg}>
            <div className="mb-4">
              <label className="block text-gray-700 font-medium mb-2">
                Organization Name
              </label>
              <input
                type="text"
                value={orgName}
                onChange={(e) => setOrgName(e.target.value)}
                placeholder="Enter organization name"
                required
                className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-purple-500"
              />
            </div>
            <button
              type="submit"
              disabled={creating}
              className="w-full bg-gradient-to-r from-purple-600 to-pink-600 text-white py-2 rounded-md hover:from-purple-700 hover:to-pink-700 disabled:opacity-50 font-medium"
            >
              {creating ? 'Creating...' : 'Create Organization'}
            </button>
          </form>
        )}

        {/* Join Tab Content */}
        {activeTab === 'join' && (
          <>
            <form onSubmit={handleSearch} className="mb-6">
              <div className="flex gap-2">
                <input
                  type="text"
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  placeholder="Search organizations..."
                  className="flex-1 px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-purple-500"
                />
                <button
                  type="submit"
                  disabled={loading}
                  className="bg-gradient-to-r from-purple-600 to-pink-600 text-white px-4 py-2 rounded-md hover:from-purple-700 hover:to-pink-700 disabled:opacity-50 font-medium"
                >
                  {loading ? 'Searching...' : 'Search'}
                </button>
              </div>
            </form>

            <div>
              <h3 className="text-lg font-semibold text-gray-800 mb-3">Available Organizations</h3>
              {organizations.length === 0 ? (
                <p className="text-gray-600">
                  {searchQuery ? 'No organizations found matching your search.' : 'Search to find organizations.'}
                </p>
              ) : (
                <div className="space-y-2">
                  {organizations.map((org) => {
                    const request = getRequestStatus(org.id);
                    return (
                      <div
                        key={org.id}
                        className="flex items-center justify-between p-3 bg-gray-50 rounded-md"
                      >
                        <div className="flex-1">
                          <p className="font-medium text-gray-800">{org.name}</p>
                          <p className="text-sm text-gray-600">
                            Created: {new Date(org.created_at).toLocaleDateString()}
                          </p>
                        </div>
                        {request ? (
                          <button
                            disabled
                            className={`px-4 py-2 rounded-md text-sm font-medium cursor-not-allowed ${
                              request.status === 'pending' 
                                ? 'bg-yellow-200 text-yellow-800' 
                                : request.status === 'approved'
                                ? 'bg-green-200 text-green-800'
                                : 'bg-red-200 text-red-800'
                            }`}
                          >
                            {request.status === 'pending' ? 'Pending' : request.status.charAt(0).toUpperCase() + request.status.slice(1)}
                          </button>
                        ) : (
                          <button
                            onClick={() => handleJoinRequest(org.id)}
                            disabled={requestingOrgId === org.id}
                            className="px-4 py-2 bg-purple-600 text-white rounded-md hover:bg-purple-700 disabled:opacity-50 text-sm font-medium"
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

        <button
          onClick={onClose}
          className="mt-6 w-full bg-gray-300 text-gray-700 py-2 rounded-md hover:bg-gray-400 font-medium"
        >
          Close
        </button>
      </div>
    </div>
  );
}
