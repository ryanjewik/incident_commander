import { useState, useEffect } from 'react';
import { apiService } from '../services/api';
import type { Organization, JoinRequest } from '../services/api';

interface JoinOrganizationModalProps {
  onClose: () => void;
}

export default function JoinOrganizationModal({ onClose }: JoinOrganizationModalProps) {
  const [searchQuery, setSearchQuery] = useState('');
  const [organizations, setOrganizations] = useState<Organization[]>([]);
  const [myRequests, setMyRequests] = useState<JoinRequest[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [requestingOrgId, setRequestingOrgId] = useState<string | null>(null);

  useEffect(() => {
    loadMyRequests();
    searchOrganizations();
  }, []);

  const loadMyRequests = async () => {
    try {
      const requests = await apiService.getJoinRequests();
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

  const handleJoinRequest = async (orgId: string) => {
    setError('');
    setSuccess('');
    setRequestingOrgId(orgId);

    try {
      await apiService.createJoinRequest(orgId);
      setSuccess('Join request sent successfully!');
      await loadMyRequests();
    } catch (err: any) {
      setError(err.response?.data?.error || 'Failed to send join request');
    } finally {
      setRequestingOrgId(null);
    }
  };

  const getRequestStatus = (orgId: string): JoinRequest | undefined => {
    return myRequests.find(req => req.organization_id === orgId);
  };

  const getStatusBadge = (status: string) => {
    const styles = {
      pending: 'bg-yellow-200 text-yellow-800',
      approved: 'bg-green-200 text-green-800',
      rejected: 'bg-red-200 text-red-800',
    };
    return styles[status as keyof typeof styles] || 'bg-gray-200 text-gray-800';
  };

  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
      <div className="bg-white p-8 rounded-lg shadow-2xl w-[600px] max-h-[80vh] overflow-y-auto">
        <h2 className="text-2xl font-bold text-gray-800 mb-4">Join an Organization</h2>
        
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

        {myRequests.length > 0 && (
          <div className="mb-6">
            <h3 className="text-lg font-semibold text-gray-800 mb-3">My Requests</h3>
            <div className="space-y-2">
              {myRequests.map((request) => (
                <div
                  key={request.id}
                  className="flex items-center justify-between p-3 bg-gray-50 rounded-md"
                >
                  <div className="flex-1">
                    <p className="font-medium text-gray-800">Organization ID: {request.organization_id}</p>
                    <p className="text-sm text-gray-600">
                      Requested: {new Date(request.created_at).toLocaleDateString()}
                    </p>
                  </div>
                  <span className={`px-3 py-1 rounded-full text-xs font-semibold ${getStatusBadge(request.status)}`}>
                    {request.status}
                  </span>
                </div>
              ))}
            </div>
          </div>
        )}

        <div>
          <h3 className="text-lg font-semibold text-gray-800 mb-3">Available Organizations</h3>
          {organizations.length === 0 ? (
            <p className="text-gray-600">No organizations found.</p>
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
                      <span className={`px-3 py-1 rounded-full text-xs font-semibold ${getStatusBadge(request.status)}`}>
                        {request.status}
                      </span>
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
