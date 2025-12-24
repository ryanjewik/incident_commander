import { getAuth } from "firebase/auth";
import { useState, useEffect } from 'react';
import { useAuth } from '../contexts/AuthContext';
import MainContent from '../components/MainContent';
import IncidentList from '../components/IncidentList';
import MetricsCards from '../components/dashboard_dummies/MetricsCards';
import IncidentTimeline from '../components/dashboard_dummies/IncidentTimeline';
import SystemHealth from '../components/dashboard_dummies/SystemHealth';
import RecentLogs from '../components/dashboard_dummies/RecentLogs';
import StatusDistribution from '../components/dashboard_dummies/StatusDistribution';
import PerformanceMetrics from '../components/dashboard_dummies/PerformanceMetrics';
import OrganizationsModal from '../components/OrganizationsModal';
import MemberManagementModal from '../components/MemberManagementModal';
import type { Organization } from '../services/api';
import { apiService } from '../services/api';

export default function Dashboard() {
    useEffect(() => {
      const auth = getAuth();
      if (auth.currentUser) {
        console.log("Firebase UID:", auth.currentUser.uid);
      } else {
        console.log("No user is logged in");
      }
    }, []);
  const { signOut, userData, leaveOrganization, refreshUserData } = useAuth();
  
  const [selectedIncident, setSelectedIncident] = useState<number | null>(null);
  const [showDashboard, setShowDashboard] = useState(false);
  const [showOrgsModal, setShowOrgsModal] = useState(false);
  const [showMemberModal, setShowMemberModal] = useState(false);
  const [showLeaveConfirm, setShowLeaveConfirm] = useState(false);
  const [leaving, setLeaving] = useState(false);
  const [userOrganizations, setUserOrganizations] = useState<Organization[]>([]);
  const [loadingOrgs, setLoadingOrgs] = useState(false);
  const [switchingOrg, setSwitchingOrg] = useState(false);
  const [selectedOrgId, setSelectedOrgId] = useState<string | undefined>(undefined);

  useEffect(() => {
    if (userData?.organization_id) {
      loadUserOrganizations();
    }
  }, [userData?.organization_id]);

  const loadUserOrganizations = async () => {
    try {
      setLoadingOrgs(true);
      const orgs = await apiService.getMyOrganizations();
      setUserOrganizations(orgs || []);
    } catch (error) {
      console.error('Failed to load organizations:', error);
    } finally {
      setLoadingOrgs(false);
    }
  };

  const handleSwitchOrganization = async (orgId: string) => {
    try {
      setSwitchingOrg(true);
      await apiService.setActiveOrganization(orgId);
      await refreshUserData();
    } catch (error) {
      console.error('Failed to switch organization:', error);
      alert('Failed to switch organization. Please try again.');
    } finally {
      setSwitchingOrg(false);
    }
  };

  const [incidentData, setIncidentData] = useState([
    { id: 1, title: 'Network Outage', status: 'Active', date: '2025-06-01T14:32:15', type: 'Incident Report', description: 'Multiple reports of network connectivity issues affecting the east coast data center. Primary and backup routers are experiencing intermittent failures.' },
    { id: 2, title: 'Database Latency', status: 'Ignored', date: '2024-11-02T09:15:42', type: 'NL Query', description: 'Increased query response times observed on the production database. Average latency has risen from 50ms to 250ms over the past hour.' },
    { id: 3, title: 'Service Degradation', status: 'Active', date: '2025-03-03T18:45:30', type: 'NL Query', description: 'API response times are 3x slower than baseline. Users reporting timeout errors when accessing the dashboard.' },
    { id: 4, title: 'API Errors', status: 'New', date: '2024-06-04T22:10:05', type: 'NL Query', description: 'Sudden spike in 500 error responses from the payment processing API. Affecting approximately 15% of transaction attempts.' },
    { id: 5, title: 'Security Breach', status: 'Resolved', date: '2024-07-05T03:28:19', type: 'Incident Report', description: 'Unauthorized access attempt detected from unknown IP addresses. Firewall rules have been updated and threat has been mitigated.' },
    { id: 6, title: 'Power Failure', status: 'Active', date: '2024-06-16T11:55:33', type: 'Incident Report', description: 'Primary power supply failure in building C. Systems running on backup generators. Expected restoration in 2 hours.' },
    { id: 7, title: 'Hardware Malfunction', status: 'Active', date: '2024-06-07T16:20:47', type: 'NL Query', description: 'Storage array controller reporting critical errors. RAID array degraded, replacement hardware has been ordered.' },
    { id: 8, title: 'SSL Certificate Expiration', status: 'New', date: '2025-05-12T07:30:22', type: 'Incident Report', description: 'SSL certificate for api.example.com will expire in 7 days. Renewal process needs to be initiated immediately.' },
    { id: 9, title: 'Memory Leak Detected', status: 'Active', date: '2025-04-20T13:42:18', type: 'NL Query', description: 'Application server memory usage growing continuously. Current usage at 92% and climbing. Service restart may be required.' },
    { id: 10, title: 'DDoS Attack Attempt', status: 'Resolved', date: '2024-12-15T20:15:55', type: 'Incident Report', description: 'Large-scale DDoS attack detected targeting web servers. Traffic filtering and rate limiting successfully prevented service disruption.' },
    { id: 11, title: 'Disk Space Critical', status: 'Active', date: '2025-01-08T05:12:40', type: 'NL Query', description: 'Log partition on server-prod-03 at 98% capacity. Automated cleanup scripts failed to run. Manual intervention required.' },
    { id: 12, title: 'Load Balancer Failure', status: 'New', date: '2025-02-14T10:08:12', type: 'Incident Report', description: 'Primary load balancer unresponsive. Traffic automatically failed over to secondary. Investigation into root cause ongoing.' },
    { id: 13, title: 'Cache Server Down', status: 'Ignored', date: '2024-10-22T15:33:27', type: 'NL Query', description: 'Redis cache cluster node 3 is offline. Cluster operating in degraded mode but still functional. Planned maintenance window scheduled.' },
    { id: 14, title: 'Authentication Service Timeout', status: 'Active', date: '2025-05-28T19:47:51', type: 'Incident Report', description: 'OAuth authentication service experiencing timeout errors. Users unable to log in. Backend team investigating database connection pool exhaustion.' },
    { id: 15, title: 'CDN Performance Issues', status: 'Resolved', date: '2024-09-11T12:25:09', type: 'NL Query', description: 'CDN edge nodes reporting degraded performance in Asia-Pacific region. Issue resolved after cache purge and configuration update.' },
  ]);

  const handleStatusChange = (incidentId: number, newStatus: string) => {
    setIncidentData(prevData => 
      prevData.map(incident => 
        incident.id === incidentId 
          ? { ...incident, status: newStatus }
          : incident
      )
    );
  };

  const handleSendIncident = async (incidentId: number) => {
    const incident = incidentData.find(i => i.id === incidentId);
    if (!incident) return;

    try {
      await apiService.createIncident({
        title: incident.title,
        status: incident.status,
        type: incident.type,
        description: incident.description,
      });
      alert('Incident sent to Firestore successfully!');
    } catch (error) {
      console.error('Failed to send incident:', error);
      alert('Failed to send incident to Firestore. Please try again.');
    }
  };

  const hasOrganization = userData?.organization_id && userData.organization_id !== '' && userData.organization_id !== 'default';

  const handleLeaveOrganization = async () => {
    setLeaving(true);
    try {
      await leaveOrganization();
      setShowLeaveConfirm(false);
    } catch (error) {
      console.error('Failed to leave organization:', error);
      alert('Failed to leave organization. Please try again.');
    } finally {
      setLeaving(false);
    }
  };

  return (
    <>
      <div className="bg-gradient-to-r from-purple-500 to-pink-500 w-screen h-50 flex items-center justify-between px-8 fixed top-0 left-0 right-0 z-10 border-3 border-purple-700 shadow-lg">
        <div>
          <h2 className="text-5xl font-bold text-white">
            Incident Command Center
          </h2>
          {hasOrganization && userOrganizations.length > 0 && (
            <p className="text-sm text-purple-100 mt-1">
              Organization: <span className="font-semibold">{userOrganizations.find(o => o.id === userData?.organization_id)?.name || 'Unknown'}</span>
            </p>
          )}
        </div>
        <div className="flex items-center gap-4">
          <button
            onClick={() => setShowDashboard(!showDashboard)}
            className="bg-white text-white px-4 py-2 rounded-md hover:bg-gray-100 font-medium"
          >
            {showDashboard ? 'Incidents' : 'Dashboard'}
          </button>
          {userOrganizations.length > 1 && hasOrganization && (
            <select
              value={userData?.organization_id || ''}
              onChange={(e) => handleSwitchOrganization(e.target.value)}
              disabled={switchingOrg || loadingOrgs}
              className="bg-white text-purple-600 px-3 py-2 rounded-md hover:bg-gray-100 font-medium border border-purple-300 focus:outline-none focus:ring-2 focus:ring-purple-500 disabled:opacity-50"
            >
              {userOrganizations.map((org) => (
                <option key={org.id} value={org.id}>
                  {org.name}
                </option>
              ))}
            </select>
          )}
          <button
            onClick={() => setShowOrgsModal(true)}
            className="bg-white text-white px-4 py-2 rounded-md hover:bg-gray-100 font-medium"
          >
            Organizations
          </button>
          <span className="text-white font-medium">
            {userData?.first_name && userData?.last_name
              ? `${userData.first_name} ${userData.last_name}`
              : userData?.email?.split('@')[0] || 'User'}
          </span>
          <button
            onClick={signOut}
            className="bg-white text-white px-4 py-2 rounded-md hover:bg-gray-100 font-medium"
          >
            Sign Out
          </button>
        </div>
      </div>
      <div className="pt-45 text-left relative left-1/2 right-1/2 -mx-[50vw] w-screen h-425 px-2">
        {showDashboard ? (
          hasOrganization ? (
            <div className="bg-gradient-to-br from-purple-50 to-pink-50 h-346.5 w-full p-6 overflow-y-auto">
              <h2 className="text-3xl font-bold text-purple-700 mb-6">Dashboard Overview</h2>
              <div className="space-y-6">
                <MetricsCards />
                <div className="grid grid-cols-2 gap-6">
                  <IncidentTimeline />
                  <StatusDistribution />
                </div>
                <div className="grid grid-cols-2 gap-6">
                  <SystemHealth />
                  <PerformanceMetrics />
                </div>
                <RecentLogs />
              </div>
            </div>
          ) : (
            <div className="bg-gradient-to-br from-purple-50 to-pink-50 h-346.5 w-full p-6 overflow-y-auto flex items-center justify-center">
              <div className="text-center max-w-md">
                <h2 className="text-2xl font-bold text-purple-700 mb-3">Join an organization to see your dashboard</h2>
                <p className="text-gray-600 mb-6">Your dashboards are organization-scoped. Create or join an organization to unlock incidents, metrics, and logs.</p>
                <button
                  onClick={() => setShowOrgsModal(true)}
                  className="bg-yellow-400 text-purple-900 px-4 py-2 rounded-md hover:bg-yellow-300 font-medium"
                >
                  Open Organizations
                </button>
              </div>
            </div>
          )
        ) : (
          hasOrganization ? (
            <div className='flex'>
              <IncidentList
                incidentData={incidentData}
                selectedIncident={selectedIncident}
                onSelectIncident={setSelectedIncident}
              />
              <MainContent 
                selectedIncident={selectedIncident}
                incidentData={incidentData}
                onClose={() => setSelectedIncident(null)}
                onStatusChange={handleStatusChange}
                onSendIncident={handleSendIncident}
              />
            </div>
          ) : (
            <div className="bg-gradient-to-br from-purple-50 to-pink-50 h-346.5 w-full p-6 overflow-y-auto flex items-center justify-center">
              <div className="text-center max-w-md">
                <h2 className="text-2xl font-bold text-purple-700 mb-3">Incidents require an organization</h2>
                <p className="text-gray-600 mb-6">Join or create an organization to view and manage incidents.</p>
                <button
                  onClick={() => setShowOrgsModal(true)}
                  className="bg-yellow-400 text-purple-900 px-4 py-2 rounded-md hover:bg-yellow-300 font-medium"
                >
                  Open Organizations
                </button>
              </div>
            </div>
          )
        )}
      </div>

      {showOrgsModal && (
        <OrganizationsModal 
          onClose={() => setShowOrgsModal(false)} 
          onOpenMemberModal={(orgId) => {
            setSelectedOrgId(orgId);
            setShowOrgsModal(false);
            setShowMemberModal(true);
          }}
        />
      )}
      
      {showMemberModal && (
        <MemberManagementModal 
          onClose={() => {
            setShowMemberModal(false);
            setSelectedOrgId(undefined);
          }} 
          orgId={selectedOrgId}
          onBack={selectedOrgId ? () => {
            setShowMemberModal(false);
            setSelectedOrgId(undefined);
            setShowOrgsModal(true);
          } : undefined}
        />
      )}

      {showLeaveConfirm && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
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
    </>
  );
}