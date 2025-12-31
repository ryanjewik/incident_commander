import { getAuth } from "firebase/auth";
import { useState, useEffect } from 'react';
import { useAuth } from '../contexts/AuthContext';
import MainContent from '../components/MainContent';
import IncidentList from '../components/IncidentList';
// top cards replaced by PerformanceMetrics
import IncidentTimeline from '../components/dashboard_dummies/IncidentTimeline';
import SystemHealth from '../components/dashboard_dummies/SystemHealth';
import RecentLogs from '../components/dashboard_dummies/RecentLogs';
import StatusDistribution from '../components/dashboard_dummies/StatusDistribution';
import PerformanceMetrics from '../components/dashboard_dummies/PerformanceMetrics';
import OrganizationsModal from '../components/OrganizationsModal';
import MemberManagementModal from '../components/MemberManagementModal';
import type { Organization, Incident } from '../services/api';
import { apiService } from '../services/api';
import { User, ChevronDown } from 'lucide-react';

export default function Dashboard() {
    useEffect(() => {
      const auth = getAuth();
      if (auth.currentUser) {
        console.log("Firebase UID:", auth.currentUser.uid);
      } else {
        console.log("No user is logged in");
      }
    }, []);
  const { signOut, userData, leaveOrganization, refreshUserData, loading } = useAuth();
  
  const [selectedIncident, setSelectedIncident] = useState<string | null>(null);
  // showDashboard is read-only here (setter intentionally omitted to avoid unused variable)
  const showDashboard = useState(false)[0];
  const [showOrgsModal, setShowOrgsModal] = useState(false);
  const [showMemberModal, setShowMemberModal] = useState(false);
  const [showLeaveConfirm, setShowLeaveConfirm] = useState(false);
  const [leaving, setLeaving] = useState(false);
  const [userOrganizations, setUserOrganizations] = useState<Organization[]>([]);
  const [loadingOrgs, setLoadingOrgs] = useState(false);
  const [switchingOrg, setSwitchingOrg] = useState(false);
  const [selectedOrgId, setSelectedOrgId] = useState<string | undefined>(undefined);
  const [showProfileMenu, setShowProfileMenu] = useState(false);
  const [incidentData, setIncidentData] = useState<Incident[]>([]);
  const [loadingIncidents, setLoadingIncidents] = useState(false);
  const [ddConfigured, setDdConfigured] = useState<boolean>(false);

  useEffect(() => {
    if (userData?.organization_id) {
      loadUserOrganizations();
      loadIncidents();
      // Check if Datadog is configured for this org
      let mounted = true;
      // default to false (not configured) until we get a successful response
      setDdConfigured(false);
      apiService.getDatadogOverview(userData.organization_id).then((res:any) => {
        if (!mounted) return;
        // Inspect payload: if it contains any of the expected sections treat as configured
        const hasMetrics = Array.isArray(res?.metrics) && res.metrics.length > 0;
        const hasMonitors = Array.isArray(res?.monitors) && res.monitors.length > 0;
        const hasLogs = Array.isArray(res?.recent_logs) && res.recent_logs.length > 0;
        const hasAny = hasMetrics || hasMonitors || hasLogs || (res && Object.keys(res).length > 0 && (res.metrics || res.monitors || res.recent_logs));
        console.debug('[Dashboard] Datadog overview response:', res, 'configured:', hasAny);
        setDdConfigured(Boolean(hasAny));
      }).catch((err:any) => {
        if (!mounted) return;
        console.debug('[Dashboard] Datadog overview error:', err?.response?.status, err?.message);
        // any error => treat as not configured
        setDdConfigured(false);
      });
      return () => { mounted = false; };
    }
  }, [userData?.organization_id]);

  // Ensure incidents load after auth finishes — helps when auth/user data arrives slightly later
  useEffect(() => {
    let mounted = true;
    if (!loading && userData?.organization_id) {
      // small debounce to let tokens settle
      const t = setTimeout(() => {
        if (!mounted) return;
        loadUserOrganizations();
        loadIncidents();
      }, 150);
      return () => { mounted = false; clearTimeout(t); };
    }
    return () => { mounted = false };
  }, [loading, userData?.organization_id]);

  // Listen for global requests to open Organizations modal (used by child components)
  useEffect(() => {
    const handler = () => setShowOrgsModal(true);
    window.addEventListener('openOrganizations', handler as EventListener);
    return () => window.removeEventListener('openOrganizations', handler as EventListener);
  }, []);

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

  const loadIncidents = async () => {
    try {
      setLoadingIncidents(true);
      const incidents = await apiService.getIncidents();
      setIncidentData(incidents || []);
      return incidents || [];
    } catch (error) {
      console.error('Failed to load incidents:', error);
      return [];
    } finally {
      setLoadingIncidents(false);
    }
  };

  const handleSwitchOrganization = async (orgId: string) => {
    try {
      setSwitchingOrg(true);
      await apiService.setActiveOrganization(orgId);
      await refreshUserData();
      await loadIncidents();
    } catch (error) {
      console.error('Failed to switch organization:', error);
      alert('Failed to switch organization. Please try again.');
    } finally {
      setSwitchingOrg(false);
    }
  };

  const handleStatusChange = async (incidentId: string, newStatus: string) => {
    // Optimistic update + retry once if it fails (refresh auth tokens)
    setIncidentData(prevData => 
      prevData.map(incident => incident.id === incidentId ? { ...incident, status: newStatus } : incident)
    );

    try {
      await apiService.updateIncident(incidentId, { status: newStatus });
    } catch (error: any) {
      console.error('Failed to update incident status, retrying after refreshUserData:', error?.response?.status, error?.message);
      try {
        await refreshUserData();
        await apiService.updateIncident(incidentId, { status: newStatus });
      } catch (err) {
        console.error('Retry failed:', err);
        alert('Failed to update incident status. Please try again.');
        // revert optimistic update
        loadIncidents();
      }
    }
  };

  const handleSeverityChange = async (incidentId: string, newSeverity: string) => {
    // Optimistic update and retry similar to status
    setIncidentData(prevData => 
      prevData.map(incident => incident.id === incidentId ? { ...incident, severity_guess: newSeverity } : incident)
    );

    try {
      await apiService.updateIncident(incidentId, { severity_guess: newSeverity });
    } catch (error: any) {
      console.error('Failed to update incident severity, retrying after refreshUserData:', error?.response?.status, error?.message);
      try {
        await refreshUserData();
        await apiService.updateIncident(incidentId, { severity_guess: newSeverity });
      } catch (err) {
        console.error('Retry failed:', err);
        alert('Failed to update incident severity. Please try again.');
        loadIncidents();
      }
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
      <div className="bg-gradient-to-r from-pink-400 to-purple-700 w-screen h-16 flex items-center justify-between px-6 fixed top-0 left-0 right-0 z-10 border-b border-purple-300 shadow-sm">
        <div className="flex items-center gap-3">
          <h2 className="text-xl md:text-2xl font-semibold text-purple-700">
            Incident Command Center
          </h2>
          {hasOrganization && userOrganizations.length > 0 && (
            <span className="text-xs text-purple-600 bg-purple-100 px-2 py-1 rounded-full hidden md:inline-block">
              {userOrganizations.find(o => o.id === userData?.organization_id)?.name || 'Unknown'}
            </span>
          )}
        </div>
        <div className="flex items-center gap-3">
          {hasOrganization && userOrganizations.length > 0 && (
            <select
              value={userData?.organization_id || ''}
              onChange={(e) => handleSwitchOrganization(e.target.value)}
              disabled={switchingOrg || loadingOrgs}
              className="bg-gradient-to-br from-pink-500 to-purple-700 text-white px-2 py-1.5 text-sm rounded-md font-medium border-none focus:outline-none focus:ring-2 focus:ring-purple-300 disabled:opacity-60 transition-colors"
            >
              {userOrganizations.map((org) => (
                <option key={org.id} value={org.id} className="text-black">
                  {org.name}
                </option>
              ))}
            </select>
          )}
          {/* Dashboard toggle removed per UI request */}
          <button
            onClick={() => setShowOrgsModal(true)}
            className="bg-purple-700 text-white px-3 py-1.5 text-sm rounded-md hover:bg-purple-800 font-medium transition-colors"
          >
            Organizations
          </button>
          <div className="relative">
            <button
              onClick={() => setShowProfileMenu(!showProfileMenu)}
              className="flex items-center gap-2 bg-purple-700 text-white px-2 py-1.5 rounded-md hover:bg-purple-800 transition-colors"
            >
              <div className="w-8 h-8 rounded-full bg-gradient-to-br from-purple-500 to-pink-500 flex items-center justify-center">
                <User size={18} className="text-white" />
              </div>
              <ChevronDown size={16} className="hidden md:block text-white" />
            </button>
            {showProfileMenu && (
              <div className="absolute right-0 mt-2 w-48 bg-white rounded-md shadow-lg border border-purple-200 py-1">
                <div className="px-4 py-2 border-b border-purple-100">
                  <p className="text-sm font-medium text-gray-800">
                    {userData?.first_name && userData?.last_name
                      ? `${userData.first_name} ${userData.last_name}`
                      : userData?.email?.split('@')[0] || 'User'}
                  </p>
                  <p className="text-xs text-gray-500">{userData?.email}</p>
                </div>
                  <button
                    onClick={() => {
                      signOut();
                      setShowProfileMenu(false);
                    }}
                    className="w-full text-left px-4 py-2 text-sm text-white bg-purple-700 hover:bg-purple-800 transition-colors rounded-b-md"
                  >
                    Sign Out
                  </button>
              </div>
            )}
          </div>
        </div>
      </div>
      <div className="fixed top-16 left-0 right-0 bottom-0 overflow-hidden">
        {showDashboard ? (
          hasOrganization ? (
            // If Datadog isn't configured for this org, render the same dashboard layout
            // but replace each panel with a placeholder instructing admins to add keys.
            ddConfigured === false ? (
              <div className="bg-gradient-to-br from-purple-50 to-pink-50 h-full w-full p-6 overflow-y-auto">
                <h2 className="text-3xl font-bold text-purple-700 mb-6">Dashboard Overview</h2>
                <div className="space-y-6">
                  <div className='bg-white p-4 rounded-lg border-4 border-purple-600 shadow-lg'>
                    <h3 className='text-xl font-bold mb-2 text-purple-700'>Performance Metrics</h3>
                    <p className='text-gray-600'>Datadog API keys are not configured for this organization. Ask an administrator to add the Datadog API & APP keys in the Admin panel to enable this panel.</p>
                    <div className='mt-3'>
                      <button onClick={() => setShowOrgsModal(true)} className='bg-purple-700 text-white px-3 py-1 rounded-md hover:bg-purple-800'>Open Organizations</button>
                    </div>
                  </div>

                  <div className="grid grid-cols-2 gap-6">
                    <div className='bg-white p-4 rounded-lg border-4 border-purple-600 shadow-lg'>
                      <h3 className='text-xl font-bold mb-2 text-purple-700'>Incident Timeline (Last 7 Days)</h3>
                      <p className='text-gray-600'>Datadog keys missing — add keys in Admin to show timeline.</p>
                    </div>
                    <div className='bg-white p-4 rounded-lg border-4 border-purple-600 shadow-lg'>
                      <h3 className='text-xl font-bold mb-2 text-purple-700'>Incident Status Distribution</h3>
                      <p className='text-gray-600'>Datadog keys missing — add keys in Admin to show status distribution.</p>
                    </div>
                  </div>

                  <div className="grid grid-cols-2 gap-6">
                    <div className='bg-white p-4 rounded-lg border-4 border-purple-600 shadow-lg'>
                      <h3 className='text-xl font-bold mb-2 text-purple-700'>System Health</h3>
                      <p className='text-gray-600'>Datadog keys missing — add keys in Admin to show monitors and health.</p>
                    </div>
                    <div className='bg-white p-4 rounded-lg border-4 border-purple-600 shadow-lg'>
                      <h3 className='text-xl font-bold mb-2 text-purple-700'>Recent Logs</h3>
                      <p className='text-gray-600'>Datadog keys missing — add keys in Admin to show recent logs and events.</p>
                    </div>
                  </div>
                </div>
              </div>
            ) : (
              <div className="bg-gradient-to-br from-purple-50 to-pink-50 h-full w-full p-6 overflow-y-auto">
                <h2 className="text-3xl font-bold text-purple-700 mb-6">Dashboard Overview</h2>
                <div className="space-y-6">
                  <PerformanceMetrics />
                  <div className="grid grid-cols-2 gap-6">
                    <IncidentTimeline />
                    <StatusDistribution />
                  </div>
                  <div className="grid grid-cols-2 gap-6">
                    <SystemHealth />
                    <RecentLogs />
                  </div>
                </div>
              </div>
            )
          ) : (
            <div className="bg-gradient-to-br from-purple-50 to-pink-50 h-full w-full p-6 overflow-y-auto">
              <div className="max-w-md">
                <h2 className="text-2xl font-bold text-purple-700 mb-3">Join an organization to see your dashboard</h2>
                <p className="text-gray-600 mb-6">Your dashboards are organization-scoped. Create or join an organization to unlock incidents, metrics, and logs.</p>
                <button
                  onClick={() => setShowOrgsModal(true)}
                  className="bg-purple-700 text-white px-4 py-2 rounded-md hover:bg-purple-800 font-medium"
                >
                  Open Organizations
                </button>
              </div>
            </div>
          )
        ) : (
          hasOrganization ? (
            <div className='flex h-full'>
              {loadingIncidents ? (
                <div className="flex items-center justify-center w-full">
                  <p className="text-purple-700">Loading incidents...</p>
                </div>
              ) : (
                <>
                  <IncidentList
                    incidentData={incidentData}
                    selectedIncident={selectedIncident}
                    onSelectIncident={setSelectedIncident}
                    onStatusChange={handleStatusChange}
                    onSeverityChange={handleSeverityChange}
                  />
                  <MainContent 
                    selectedIncident={selectedIncident}
                    incidentData={incidentData}
                    onClose={() => setSelectedIncident(null)}
                    onStatusChange={handleStatusChange}
                    onSeverityChange={handleSeverityChange}
                    onSendIncident={async (id: string) => {
                      console.debug('[Dashboard] onSendIncident called with', id);
                      setSelectedIncident(id);
                      // Refresh incident list so the new incident appears in the sidebar
                      try {
                        const refreshed = await loadIncidents();
                        console.debug('[Dashboard] Refreshed incidents, count=', refreshed.length);
                      } catch (err) { console.error('Failed to refresh incidents after create:', err); }
                    }}
                  />
                </>
              )}
            </div>
          ) : (
            <div className="bg-gradient-to-br from-purple-50 to-pink-50 h-full w-full p-6 overflow-y-auto">
              <div className="max-w-md">
                <h2 className="text-2xl font-bold text-purple-700 mb-3">Incidents require an organization</h2>
                <p className="text-gray-600 mb-6">Join or create an organization to view and manage incidents.</p>
                <button
                  onClick={() => setShowOrgsModal(true)}
                  className="bg-purple-700 text-white px-4 py-2 rounded-md hover:bg-purple-800 font-medium"
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