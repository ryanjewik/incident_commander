import { useState } from 'react';
import { useAuth } from '../contexts/AuthContext';
import MainContent from '../components/MainContent';
import IncidentList from '../components/IncidentList';
import MetricsCards from '../components/dashboard_dummies/MetricsCards';
import IncidentTimeline from '../components/dashboard_dummies/IncidentTimeline';
import SystemHealth from '../components/dashboard_dummies/SystemHealth';
import RecentLogs from '../components/dashboard_dummies/RecentLogs';
import StatusDistribution from '../components/dashboard_dummies/StatusDistribution';
import PerformanceMetrics from '../components/dashboard_dummies/PerformanceMetrics';
import OrganizationModal from '../components/OrganizationModal';
import MemberManagementModal from '../components/MemberManagementModal';

export default function Dashboard() {
  const { signOut, userData } = useAuth();
  
  const [selectedIncident, setSelectedIncident] = useState<number | null>(null);
  const [showDashboard, setShowDashboard] = useState(false);
  const [showOrgModal, setShowOrgModal] = useState(false);
  const [showMemberModal, setShowMemberModal] = useState(false);

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

  const hasOrganization = userData?.organization_id && userData.organization_id !== '' && userData.organization_id !== 'default';
  const isAdmin = userData?.role === 'admin';

  return (
    <>
      <div className="bg-gradient-to-r from-purple-500 to-pink-500 w-screen h-50 flex items-center justify-between px-8 fixed top-0 left-0 right-0 z-10 border-3 border-purple-700 shadow-lg">
        <h2 className="text-5xl font-bold text-white">
          Incident Command Center
        </h2>
        <div className="flex items-center gap-4">
          <button
            onClick={() => setShowDashboard(!showDashboard)}
            className="bg-white text-purple-600 px-4 py-2 rounded-md hover:bg-gray-100 font-medium"
          >
            {showDashboard ? 'Incidents' : 'Dashboard'}
          </button>
          {!hasOrganization && (
            <button
              onClick={() => setShowOrgModal(true)}
              className="bg-yellow-400 text-purple-900 px-4 py-2 rounded-md hover:bg-yellow-300 font-medium"
            >
              Create Organization
            </button>
          )}
          {hasOrganization && isAdmin && (
            <button
              onClick={() => setShowMemberModal(true)}
              className="bg-white text-purple-600 px-4 py-2 rounded-md hover:bg-gray-100 font-medium"
            >
              Manage Members
            </button>
          )}
          <span className="text-white font-medium">
            {userData?.display_name}
            {userData?.role && ` (${userData.role})`}
          </span>
          <button
            onClick={signOut}
            className="bg-white text-purple-600 px-4 py-2 rounded-md hover:bg-gray-100 font-medium"
          >
            Sign Out
          </button>
        </div>
      </div>
      <div className="pt-45 text-left relative left-1/2 right-1/2 -mx-[50vw] w-screen h-425 px-2">
        {showDashboard ? (
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
            />
          </div>
        )}
      </div>

      {showOrgModal && (
        <OrganizationModal onClose={() => setShowOrgModal(false)} />
      )}
      
      {showMemberModal && (
        <MemberManagementModal onClose={() => setShowMemberModal(false)} />
      )}
    </>
  );
}