interface ChannelHeaderProps {
  channelName: string;
  onGenerateReport: () => void;
  isLoading: boolean;
}

export default function ChannelHeader({ 
  channelName, 
  onGenerateReport, 
  isLoading 
}: ChannelHeaderProps) {
  return (
    <div className="flex justify-between items-center mb-8">
      <h1 className="text-3xl font-bold text-gray-900">
        Reports for #{channelName}
      </h1>
      <div className="text-sm text-gray-500">
        {isLoading ? 'Loading...' : 'Ready to generate'}
      </div>
      <button
        onClick={onGenerateReport}
        disabled={isLoading}
        className={`px-4 py-2 rounded-lg text-white font-medium
          ${isLoading 
            ? 'bg-blue-400 cursor-not-allowed' 
            : 'bg-blue-600 hover:bg-blue-700'}`}
      >
        {isLoading ? (
          <span className="flex items-center">
            <svg className="animate-spin -ml-1 mr-2 h-4 w-4 text-white" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
              <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
              <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
            </svg>
            Generating...
          </span>
        ) : 'Generate Report'}
      </button>
    </div>
  );
} 