import { useState } from 'react';
import { Channel } from '../types';

interface ChannelSearchProps {
  onSearch: (query: string) => void;
  onGenerateInstantReport: (selectedChannels: string[]) => Promise<void>;
  isLoading: boolean;
  selectedChannels: Channel[];
}

export default function ChannelSearch({ 
  onSearch, 
  onGenerateInstantReport, 
  isLoading,
  selectedChannels 
}: ChannelSearchProps) {
  const [searchQuery, setSearchQuery] = useState('');

  const handleGenerateReport = () => {
    const channelIds = selectedChannels.map(channel => channel.id);
    onGenerateInstantReport(channelIds);
  };

  return (
    <div className="mb-6 space-y-4">
      <div className="flex gap-4">
        <input
          type="text"
          placeholder="Search channels..."
          className="flex-1 px-4 py-2 border rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
          value={searchQuery}
          onChange={(e) => {
            setSearchQuery(e.target.value);
            onSearch(e.target.value);
          }}
        />
        <button
          onClick={handleGenerateReport}
          disabled={isLoading || selectedChannels.length === 0}
          className={`px-6 py-2 text-white rounded-lg focus:ring-2 focus:ring-blue-500 focus:ring-offset-2
            ${isLoading || selectedChannels.length === 0 
              ? 'bg-blue-300 cursor-not-allowed' 
              : 'bg-blue-600 hover:bg-blue-700'}`}
        >
          {isLoading ? (
            <span className="flex items-center">
              <svg className="animate-spin -ml-1 mr-3 h-5 w-5 text-white" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
              </svg>
              Generating...
            </span>
          ) : (
            'Generate Instant Report'
          )}
        </button>
      </div>
    </div>
  );
} 