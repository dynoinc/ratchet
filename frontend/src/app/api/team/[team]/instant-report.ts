import type { NextApiRequest, NextApiResponse } from 'next';

export default async function handler(
  req: NextApiRequest, 
  res: NextApiResponse<{data: Record<string, unknown> | null, error?: string}>
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

    const data = await response.json();
    res.status(response.status).json({ 
      data: data,
      error: response.ok ? undefined : 'Failed to generate report'
    });
  } catch (error) {
    console.error('Error generating report:', error);
    res.status(500).json({ 
      data: null, 
      error: 'Failed to generate report' 
    });
  }
} 