import MetricsCards from './dashboard_dummies/MetricsCards';
import IncidentTimeline from './dashboard_dummies/IncidentTimeline';
import SystemHealth from './dashboard_dummies/SystemHealth';
import RecentLogs from './dashboard_dummies/RecentLogs';
import StatusDistribution from './dashboard_dummies/StatusDistribution';
import PerformanceMetrics from './dashboard_dummies/PerformanceMetrics';

function Dashboard(){
    return (    
        <div className="bg-gradient-to-br from-purple-50 to-pink-50 h-347 w-full p-6 overflow-y-auto">
            <h2 className="text-3xl font-bold text-purple-700 mb-6">Dashboard Overview</h2>
            
            <div className="space-y-6">
                {/* Top Metrics Row */}
                <MetricsCards />
                
                {/* Middle Section - Timeline and Status */}
                <div className="grid grid-cols-2 gap-6">
                    <IncidentTimeline />
                    <StatusDistribution />
                </div>
                
                {/* System Health and Performance */}
                <div className="grid grid-cols-2 gap-6">
                    <SystemHealth />
                    <PerformanceMetrics />
                </div>
                
                {/* Recent Logs */}
                <RecentLogs />
            </div>
        </div>
    );
}

export default Dashboard;