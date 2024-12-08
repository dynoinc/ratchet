import { useRouter } from 'next/router';
import { useEffect, useCallback } from 'react';

export default function ChannelPage() {
  const router = useRouter();
  const { team } = router.query;

  const fetchChannelData = useCallback(async () => {
    try {
      const response = await fetch(`/api/channels/${team}`);
      const data = await response.json();
      console.log('Channel data:', data);
    } catch (error) {
      console.error('Error fetching channel:', error);
    }
  }, [team]);

  const fetchReports = useCallback(async () => {
    try {
      const response = await fetch(`/api/team/${team}/reports`);
      const data = await response.json();
      console.log('Reports:', data);
    } catch (error) {
      console.error('Error fetching reports:', error);
    }
  }, [team]);

  useEffect(() => {
    if (team) {
      console.log('Team name:', team);
      fetchChannelData();
      fetchReports();
    }
  }, [team, fetchChannelData, fetchReports]);

  return (
    <div>
      {/* Render your component content here */}
    </div>
  );
} 