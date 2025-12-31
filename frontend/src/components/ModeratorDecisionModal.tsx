interface ModeratorDecisionModalProps {
  moderatorResult: {
    confidence?: number;
    incident_summary: string;
    most_likely_root_cause: string;
    reasoning: string;
    severity_guess: string;
    do_this_now?: string[];
    next_60_minutes?: string[];
    what_to_monitor?: string[];
    tradeoffs_and_risks?: string[];
    agent_disagreements?: Array<{
      agent: string;
      disagreement: string;
      confidence: number;
    }>;
  };
  moderatorTimestamp?: string;
  onClose: () => void;
}

function ModeratorDecisionModal({ moderatorResult, moderatorTimestamp, onClose }: ModeratorDecisionModalProps) {
  return (
    <div className="fixed inset-0 flex items-center justify-center z-50 p-8">
      <div 
        className="absolute inset-0 bg-black/30 backdrop-blur-sm"
        onClick={onClose}
      />
      <div className="relative bg-white rounded-lg shadow-2xl max-w-4xl w-full max-h-[85vh] overflow-y-auto">
        <div className="sticky top-0 bg-gradient-to-r from-purple-600 to-pink-600 text-white p-4 rounded-t-lg flex items-center justify-between">
          <div>
            <h2 className="text-2xl font-bold">Moderator Analysis</h2>
            {moderatorTimestamp && (
              <p className="text-sm opacity-90">
                {new Date(moderatorTimestamp).toLocaleString()}
              </p>
            )}
          </div>
          <button
            onClick={onClose}
            className="text-white hover:bg-white hover:bg-opacity-20 rounded-full p-2 transition-colors"
          >
            <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        <div className="p-6 space-y-6">
          {/* Summary Section */}
          <div className="bg-purple-50 p-4 rounded-lg border-2 border-purple-200">
            <h3 className="text-lg font-bold text-purple-700 mb-2">Incident Summary</h3>
            <p className="text-gray-800">{moderatorResult.incident_summary}</p>
          </div>

          {/* Severity and Confidence */}
          <div className="grid grid-cols-2 gap-4">
            <div className="bg-pink-50 p-4 rounded-lg border-2 border-pink-200">
              <h4 className="text-sm font-bold text-pink-700 mb-2">Severity Assessment</h4>
              <span className={`inline-block px-3 py-1 rounded text-sm font-semibold ${
                moderatorResult.severity_guess === 'Critical' ? 'bg-red-500 text-white' :
                moderatorResult.severity_guess === 'High' ? 'bg-orange-500 text-white' :
                moderatorResult.severity_guess === 'Medium' ? 'bg-yellow-500 text-white' :
                'bg-green-500 text-white'
              }`}>
                {moderatorResult.severity_guess}
              </span>
            </div>
            {moderatorResult.confidence !== undefined && (
              <div className="bg-purple-50 p-4 rounded-lg border-2 border-purple-200">
                <h4 className="text-sm font-bold text-purple-700 mb-2">Confidence Level</h4>
                <div className="flex items-center gap-2">
                  <div className="flex-1 bg-gray-200 rounded-full h-3">
                    <div 
                      className="bg-purple-600 h-3 rounded-full transition-all"
                      style={{ width: `${moderatorResult.confidence * 100}%` }}
                    />
                  </div>
                  <span className="text-sm font-semibold text-gray-700">
                    {(moderatorResult.confidence * 100).toFixed(0)}%
                  </span>
                </div>
              </div>
            )}
          </div>

          {/* Root Cause */}
          <div className="bg-red-50 p-4 rounded-lg border-2 border-red-200">
            <h3 className="text-lg font-bold text-red-700 mb-2">Most Likely Root Cause</h3>
            <p className="text-gray-800">{moderatorResult.most_likely_root_cause}</p>
          </div>

          {/* Reasoning */}
          <div className="bg-blue-50 p-4 rounded-lg border-2 border-blue-200">
            <h3 className="text-lg font-bold text-blue-700 mb-2">Detailed Reasoning</h3>
            <p className="text-gray-800 whitespace-pre-wrap">{moderatorResult.reasoning}</p>
          </div>

          {/* Action Items */}
          {Array.isArray(moderatorResult.do_this_now) && moderatorResult.do_this_now.length > 0 && (
            <div className="bg-orange-50 p-4 rounded-lg border-2 border-orange-200">
              <h3 className="text-lg font-bold text-orange-700 mb-2">Immediate Actions</h3>
              <ul className="list-disc list-inside space-y-1">
                {moderatorResult.do_this_now.map((action, idx) => (
                  <li key={idx} className="text-gray-800">{action}</li>
                ))}
              </ul>
            </div>
          )}

          {/* Next 60 Minutes */}
          {Array.isArray(moderatorResult.next_60_minutes) && moderatorResult.next_60_minutes.length > 0 && (
            <div className="bg-yellow-50 p-4 rounded-lg border-2 border-yellow-200">
              <h3 className="text-lg font-bold text-yellow-700 mb-2">Next 60 Minutes</h3>
              <ul className="list-disc list-inside space-y-1">
                {moderatorResult.next_60_minutes.map((item, idx) => (
                  <li key={idx} className="text-gray-800">{item}</li>
                ))}
              </ul>
            </div>
          )}

          {/* Monitoring */}
          {Array.isArray(moderatorResult.what_to_monitor) && moderatorResult.what_to_monitor.length > 0 && (
            <div className="bg-green-50 p-4 rounded-lg border-2 border-green-200">
              <h3 className="text-lg font-bold text-green-700 mb-2">What to Monitor</h3>
              <ul className="list-disc list-inside space-y-1">
                {moderatorResult.what_to_monitor.map((item, idx) => (
                  <li key={idx} className="text-gray-800">{item}</li>
                ))}
              </ul>
            </div>
          )}

          {/* Tradeoffs and Risks */}
          {Array.isArray(moderatorResult.tradeoffs_and_risks) && moderatorResult.tradeoffs_and_risks.length > 0 && (
            <div className="bg-red-50 p-4 rounded-lg border-2 border-red-200">
              <h3 className="text-lg font-bold text-red-700 mb-2">Tradeoffs and Risks</h3>
              <ul className="list-disc list-inside space-y-1">
                {moderatorResult.tradeoffs_and_risks.map((item, idx) => (
                  <li key={idx} className="text-gray-800">{item}</li>
                ))}
              </ul>
            </div>
          )}

          {/* Agent Disagreements */}
          {Array.isArray(moderatorResult.agent_disagreements) && moderatorResult.agent_disagreements.length > 0 && (
            <div className="bg-gray-50 p-4 rounded-lg border-2 border-gray-300">
              <h3 className="text-lg font-bold text-gray-700 mb-3">Agent Disagreements</h3>
              <div className="space-y-3">
                {moderatorResult.agent_disagreements.map((disagreement, idx) => (
                  <div key={idx} className="bg-white p-3 rounded border border-gray-200">
                    <div className="flex items-center justify-between mb-1">
                      <span className="font-semibold text-gray-800">{disagreement.agent}</span>
                      <span className="text-sm text-gray-600">
                        Confidence: {(disagreement.confidence * 100).toFixed(0)}%
                      </span>
                    </div>
                    <p className="text-gray-700 text-sm">{disagreement.disagreement}</p>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>

        <div className="sticky bottom-0 bg-gray-100 p-4 rounded-b-lg border-t-2 border-gray-200">
          <button
            onClick={onClose}
            className="w-full bg-gradient-to-r from-purple-600 to-pink-600 text-white py-2 rounded-md hover:from-purple-700 hover:to-pink-700 font-semibold transition-colors"
          >
            Close
          </button>
        </div>
      </div>
    </div>
  );
}

export default ModeratorDecisionModal;