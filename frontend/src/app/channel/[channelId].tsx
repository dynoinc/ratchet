/* eslint-disable react-hooks/exhaustive-deps */
import { useState, useEffect } from 'react';
import { useRouter } from 'next/router';
import Layout from '../../components/Layout';
import BackButton from '../../components/BackButton';
import ChannelHeader from '../../components/ChannelHeader';
import ReportList from '../../components/ReportList';
import { Channel, Report } from '../../types';
import ErrorBoundary from '../../components/ErrorBoundary';

export default function ChannelPage() {
  const router = useRouter();
  const { channelId } = router.query;
  const [channel, setChannel] = useState<Channel | null>(null);
  const [reports, setReports] = useState<Report[]>([]);
  const [isLoading, setIsLoading] = useState(false);

  const fetchChannelData = async () => {
    try {
      const response = await fetch(`/api/channels/${channelId}`);
      const data = await response.json();
      setChannel(data.data);
    } catch (error) {
      console.error('Error fetching channel:', error);
    }
  };

  const fetchReports = async () => {
    try {
      const response = await fetch(`/api/channels/${channelId}/reports`);
      const data = await response.json();
      setReports(data.data || []);
    } catch (error) {
      console.error('Error fetching reports:', error);
    }
  };

  useEffect(() => {
    if (channelId) {
      fetchChannelData();
      fetchReports();
    }
  }, [channelId, fetchChannelData, fetchReports]);

  const handleGenerateReport = async () => {
    setIsLoading(true);
    try {
      const response = await fetch(`/api/channels/${channelId}/instant-report`, {
        method: 'GET',
        headers: { 'Content-Type': 'application/json' }
      });
      
      if (!response.ok) throw new Error('Failed to generate report');
      
      const data = await response.json();
      
      if (!data.incidents?.length && !data.topAlerts?.length) {
        alert('No incidents or alerts found for this time period.');
        return;
      }
      
      setReports(prevReports => [{
        id: Date.now().toString(),
        channelId: channelId as string,
        content: JSON.stringify(data),
        createdAt: data.createdAt || new Date().toISOString()
      }, ...prevReports]);
      
      alert('Report generated successfully!');
    } catch (error) {
      console.error('Error generating report:', error);
      alert('Failed to generate report. Please try again.');
    } finally {
      setIsLoading(false);
    }
  };

  if (!channel) return <div>Loading...</div>;
  return (
    <Layout>
      <ErrorBoundary>
        <BackButton />
        <ChannelHeader 
          channelName={channel.name}
          onGenerateReport={handleGenerateReport}
          isLoading={isLoading}
        />
        <ReportList reports={reports} />
      </ErrorBoundary>
    </Layout>
  );
} 