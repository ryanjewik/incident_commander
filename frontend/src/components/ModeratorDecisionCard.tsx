interface ModeratorDecisionCardProps {
  moderatorResult: {
    confidence?: number;
    incident_summary: string;
    most_likely_root_cause: string;
    reasoning: string;
    severity_guess: string;
  };
  moderatorTimestamp?: string;
  onClick?: () => void;
}

function ModeratorDecisionCard({ moderatorResult, moderatorTimestamp, onClick }: ModeratorDecisionCardProps) {
  const severityGuess = (moderatorResult.severity_guess || '').toLowerCase();
  
  return (
    <div 
      className={`bg-gradient-to-br from-purple-100 to-pink-100 p-3 rounded-lg border-2 border-purple-400 shadow-md ${onClick ? 'cursor-pointer hover:shadow-lg transition-shadow' : ''}`}
      onClick={onClick}
    >
      <div className='flex items-center justify-between mb-2'>
        <h4 className='text-sm font-bold text-purple-700'>Moderator Analysis</h4>
        {moderatorTimestamp && (
          <span className='text-[10px] text-gray-600'>
            {new Date(moderatorTimestamp).toLocaleString()}
          </span>
        )}
      </div>
      <div className='space-y-2'>
        <div>
          <p className='text-xs font-semibold text-gray-700'>Summary:</p>
          <p className='text-xs text-gray-800'>{moderatorResult.incident_summary}</p>
        </div>
        <div>
          <p className='text-xs font-semibold text-gray-700'>Root Cause:</p>
          <p className='text-xs text-gray-800'>{moderatorResult.most_likely_root_cause}</p>
        </div>
        <div className='flex gap-2 items-center'>
          <span className={`px-2 py-1 rounded text-[10px] font-semibold ${
            severityGuess === 'critical' ? 'bg-red-700 text-white' :
            severityGuess === 'high' ? 'bg-red-500 text-white' :
            severityGuess === 'medium' ? 'bg-yellow-500 text-white' :
            severityGuess === 'low' ? 'bg-green-500 text-white' : 'bg-gray-200 text-black'
          }`}>
            Recommended Severity: {moderatorResult.severity_guess}
          </span>
          {moderatorResult.confidence !== undefined && (
            <span className='text-[10px] text-gray-600'>
              Confidence: {(moderatorResult.confidence * 100).toFixed(0)}%
            </span>
          )}
        </div>
        {onClick && (
          <p className='text-[10px] text-purple-600 font-medium mt-2'>
            Click to view full analysis →
          </p>
        )}
      </div>
    </div>
  );
}

export default ModeratorDecisionCard;