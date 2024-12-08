import type { NextApiRequest, NextApiResponse } from 'next';
import type { Channel } from '../../types/index';

export default async function handler(
  req: NextApiRequest,
  res: NextApiResponse<{data: Channel[] | null, error?: string}>
) {
  try {
    const response = await fetch(`${process.env.NEXT_PUBLIC_API_URL}/api/channels`);
    const data = await response.json();
    res.status(200).json({ data, error: undefined });
  } catch (err) {
    const errorMessage = err instanceof Error ? err.message : 'Unknown error';
    console.error('Error fetching channels:', errorMessage);
    res.status(500).json({ data: null, error: 'Failed to fetch channels' });
  }
} 