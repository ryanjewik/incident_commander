import { useState } from 'react'
import reactLogo from './assets/react.svg'
import viteLogo from '/vite.svg'
import './App.css'
import MainContent from './components/MainContent'
import IncidentList from './components/IncidentList'

function App() {

  const [selectedIncident, setSelectedIncident] = useState<number | null>(null);

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

  return (
    <>
      <div className="bg-gradient-to-r from-purple-500 to-pink-500 w-screen h-50 flex items-center justify-center fixed top-0 left-0 right-0 z-10 border-3 border-purple-700 shadow-lg">
        <h2 className="text-5xl font-bold text-white">
          Incident Command Center
        </h2>
      </div>
      <div className="pt-45 text-left relative left-1/2 right-1/2 -mx-[50vw] w-screen h-425 px-2">
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
      </div>
    </>
  )
}

export default App
