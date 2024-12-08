'use client';

import { useState, useEffect, useCallback } from 'react';
import { useParams } from 'next/navigation';
import Layout from '../../../components/Layout';
import { Channel, Report } from '../../../types/index';

export default function TeamPage() {
  const params = useParams();
  const [reports, setReports] = useState<Report[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [generating, setGenerating] = useState(false);

  const fetchReports = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      
      // First get the channel ID for the team name
      const channelResponse = await fetch(`/api/channels`);
      if (!channelResponse.ok) {
        throw new Error('Failed to fetch channel information');
      }
      const channels = await channelResponse.json();
      const channel = channels.find((c: Channel) => c.name === params.team);
      
      if (!channel) {
        setError('Channel not found');
        return;
      }

      // Then fetch the reports using the channel ID
      const reportsResponse = await fetch(`/api/channels/${channel.id}/reports`);
      if (!reportsResponse.ok) {
        if (reportsResponse.status === 404) {
          // No reports is a valid state
          setReports([]);
          return;
        }
        throw new Error(`Failed to fetch reports`);
      }
      
      const data = await reportsResponse.json();
      // Ensure we always set an array, even if empty
      setReports(Array.isArray(data) ? data : []);
      
    } catch (error) {
      console.error('Error fetching reports:', error);
      setError('Failed to fetch reports');
    } finally {
      setLoading(false);
    }
  }, [params.team]);

  
  useEffect(() => {
    fetchReports();
  }, [params.team, fetchReports]);
  const generateReport = async () => {
    try {
      setGenerating(true);
      setError(null);

      const channelResponse = await fetch(`/api/channels`);
      if (!channelResponse.ok) {
        throw new Error('Failed to fetch channel information');
      }
      
      const channels = await channelResponse.json();
      const channel = channels.find((c: Channel) => c.name === params.team);
      
      if (!channel) {
        setError('Channel not found');
        return;
      }

      const response = await fetch(`/api/channels/${channel.id}/instant-report`, {
        headers: {
          'Accept': 'application/json',
        },
      });
      
      console.log('Instant report response:', {
        status: response.status,
        statusText: response.statusText,
        headers: Object.fromEntries(response.headers),
      });
      
      if (!response.ok) {
        throw new Error('Failed to generate report');
      }

      await fetchReports();
    } catch (error) {
      console.error('Error generating report:', error);
      setError('Failed to generate report');
    } finally {
      setGenerating(false);
    }
  };

  return (
    <Layout>
      <div className="min-h-screen bg-gray-50 py-8 px-4 sm:px-6 lg:px-8">
        <header className="mb-8">
          <h1 className="text-3xl font-bold text-gray-900">
            Reports for #{params.team}
          </h1>
        </header>

        {error && (
          <div className="mb-4 p-4 bg-red-50 text-red-700 rounded-lg">
            {error}
          </div>
        )}

        <div className="mb-6">
          <button
            onClick={generateReport}
            disabled={generating}
            className={`px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:opacity-50 disabled:cursor-not-allowed`}
          >
            {generating ? 'Generating Report...' : 'Generate New Report'}
          </button>
        </div>

        <div className="space-y-6">
          {loading ? (
            <div className="bg-white shadow-sm rounded-lg p-6">
              <p>Loading reports...</p>
            </div>
          ) : reports.length === 0 ? (
            <div className="bg-white shadow-sm rounded-lg p-6">
              <p className="text-gray-500">No reports available for this channel yet.</p>
              <p className="text-gray-500 mt-2">Click the &quot;Generate New Report&quot; button above to create one.</p>
            </div>
          ) : (
            reports.map(report => (
              <div key={report.id} className="bg-white shadow-sm rounded-lg p-6">
                <div className="mb-2 text-sm text-gray-500">
                  {new Date(report.createdAt).toLocaleString()}
                </div>
                <pre className="whitespace-pre-wrap font-mono text-sm">
                  {report.content}
                </pre>
              </div>
            ))
          )}
        </div>
      </div>
    </Layout>
  );
} 