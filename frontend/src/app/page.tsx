'use client';

import { useState, useEffect } from 'react';
import Link from 'next/link';
import Layout from '../components/Layout';
import { Channel } from '../types';

export default function Home() {
  const [channels, setChannels] = useState<Channel[]>([]);
  const [filteredChannels, setFilteredChannels] = useState<Channel[]>([]);
  const [searchQuery, setSearchQuery] = useState('');

  useEffect(() => {
    fetchChannels();
  }, []);

  const fetchChannels = async () => {
    try {
      const response = await fetch('/api/channels');
      const text = await response.text();
      
      let data;
      try {
        data = JSON.parse(text);
      } catch (parseError) {
        console.error('Failed to parse response as JSON:', parseError);
        return;
      }
      
      if (Array.isArray(data)) {
        setChannels(data);
        setFilteredChannels(data);
      } else {
        console.error('Data is not an array:', data);
      }
    } catch (error) {
      console.error('Error fetching channels:', error);
    }
  };

  const handleSearch = (query: string) => {
    setSearchQuery(query);
    const filtered = channels.filter(channel => 
      channel.name.toLowerCase().includes(query.toLowerCase())
    );
    setFilteredChannels(filtered);
  };

  return (
    <Layout>
      <header className="mb-8">
        <h1 className="text-3xl font-bold text-gray-900">
          Ratchet
        </h1>
      </header>

      <div className="mb-6">
        <input
          type="text"
          placeholder="Search channels..."
          className="w-full px-4 py-2 border rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
          value={searchQuery}
          onChange={(e) => handleSearch(e.target.value)}
        />
      </div>

      <div className="grid grid-cols-1 gap-8 md:grid-cols-2">
        <section className="bg-white rounded-lg shadow-sm p-6">
          <h2 className="text-xl font-semibold text-gray-900 mb-4">
            Registered Channels
          </h2>
          <div className="space-y-2">
            {(filteredChannels.length > 0 ? filteredChannels : channels).map(channel => (
              <div key={channel.id} className="flex items-center justify-between p-4 border rounded-lg hover:bg-gray-50">
                <Link 
                  href={`/team/${channel.name}`}
                  className="flex-1 font-medium text-blue-600 hover:text-blue-700"
                >
                  #{channel.name}
                </Link>
                <Link
                  href={`/team/${channel.name}`}
                  className="ml-4 px-3 py-1 text-sm text-blue-600 hover:text-blue-700 border border-blue-200 rounded-md hover:border-blue-300"
                >
                  View Reports
                </Link>
              </div>
            ))}
          </div>
        </section>

        <section className="bg-white rounded-lg shadow-sm p-6">
          <h2 className="text-xl font-semibold text-gray-900 mb-4">
            Channel Reports
          </h2>
          <p className="text-gray-500">Select a channel to view or generate reports.</p>
        </section>
      </div>
    </Layout>
  );
}
