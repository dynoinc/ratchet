'use client'

import { useState, useEffect } from 'react'
import { Channel, Report } from '../types'

export function Channels() {
  const [channels, setChannels] = useState<Channel[]>([])
  const [selectedChannel, setSelectedChannel] = useState<string | null>(null)
  const [reports, setReports] = useState<Report[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    fetchChannels()
  }, [])

  useEffect(() => {
    if (selectedChannel) {
      fetchReports(selectedChannel)
    }
  }, [selectedChannel])

  const fetchChannels = async () => {
    try {
      setLoading(true)
      setError(null)
      const response = await fetch(`${process.env.NEXT_PUBLIC_API_URL}/api/channels`)
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`)
      }
      const data = await response.json()
      setChannels(data || [])
    } catch (error) {
      console.error('Error fetching channels:', error)
      setError('Failed to load channels')
    } finally {
      setLoading(false)
    }
  }

  const fetchReports = async (channelId: string) => {
    try {
      setLoading(true)
      setError(null)
      const response = await fetch(`${process.env.NEXT_PUBLIC_API_URL}/api/channels/${channelId}/reports`)
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`)
      }
      const data = await response.json()
      setReports(data || [])
    } catch (error) {
      console.error('Error fetching reports:', error)
      setError('Failed to load reports')
    } finally {
      setLoading(false)
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center min-h-[200px]">
        <div className="text-gray-600">Loading...</div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex items-center justify-center min-h-[200px]">
        <div className="text-red-600">{error}</div>
      </div>
    )
  }

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
      <div className="bg-white shadow rounded-lg p-6">
        <h2 className="text-xl font-semibold mb-4">Registered Channels</h2>
        <div className="space-y-2">
          {channels.length === 0 ? (
            <div className="text-gray-500">No channels found</div>
          ) : (
            channels.map((channel) => (
              <button
                key={channel.id}
                onClick={() => setSelectedChannel(channel.id)}
                className={`w-full text-left px-4 py-2 rounded ${
                  selectedChannel === channel.id
                    ? 'bg-blue-500 text-white'
                    : 'bg-gray-100 hover:bg-gray-200'
                }`}
              >
                #{channel.name}
              </button>
            ))
          )}
        </div>
      </div>

      {selectedChannel && (
        <div className="bg-white shadow rounded-lg p-6">
          <h2 className="text-xl font-semibold mb-4">Channel Reports</h2>
          <div className="space-y-4">
            {reports.length === 0 ? (
              <div className="text-gray-500">No reports found</div>
            ) : (
              reports.map((report) => (
                <div key={report.id} className="border rounded p-4">
                  <pre className="whitespace-pre-wrap">{JSON.stringify(report, null, 2)}</pre>
                </div>
              ))
            )}
          </div>
        </div>
      )}
    </div>
  )
}