import { Channel } from '../types';

interface ChannelListProps {
  channels: Channel[];
  onChannelSelect: (channelId: string) => void;
  selectedChannels: Set<string>;
}

export default function ChannelList({ channels, onChannelSelect, selectedChannels }: ChannelListProps) {
  return (
    <div className="space-y-2">
      {channels.map(channel => (
        <div 
          key={channel.id}
          className={`p-4 border rounded-lg transition-colors cursor-pointer
            ${selectedChannels.has(channel.id) 
              ? 'bg-blue-50 border-blue-200' 
              : 'hover:bg-gray-50'}`}
          onClick={() => onChannelSelect(channel.id)}
        >
          <div className="flex items-center justify-between">
            <div className="flex items-center space-x-3">
              <input
                type="checkbox"
                checked={selectedChannels.has(channel.id)}
                onChange={() => onChannelSelect(channel.id)}
                className="h-4 w-4 text-blue-600 focus:ring-blue-500 border-gray-300 rounded"
              />
              <span className="font-medium">#{channel.name}</span>
            </div>
          </div>
        </div>
      ))}
    </div>
  );
} 