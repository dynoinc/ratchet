import type { NextApiRequest, NextApiResponse } from 'next';
import type { Report } from '../../../types/index';

export default async function handler(
  req: NextApiRequest,
  res: NextApiResponse<{data: Report | null, error?: string}>
) {
  if (req.method !== 'POST') {
    return res.status(405).json({ 
      data: null, 
      error: 'Method not allowed' 
    });
  }

  const { team } = req.query;

  try {
    const response = await fetch(`${process.env.NEXT_PUBLIC_API_URL}/team/${team}/instant-report`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
    });

    if (!response.ok) {
      throw new Error('Failed to generate report');
    }

    const data = await response.json();
    res.status(200).json({ data, error: undefined });
  } catch (err) {
    const errorMessage = err instanceof Error ? err.message : 'Unknown error';
    console.error('Error generating report:', errorMessage);
    res.status(500).json({ 
      data: null, 
      error: 'Failed to generate report' 
    });
  }
} 