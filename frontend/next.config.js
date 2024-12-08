/** @type {import('next').NextConfig} */
const nextConfig = {
  output: 'standalone',
  async rewrites() {
    return [
      {
        source: '/api/channels',
        destination: 'http://app:5001/api/channels',
      },
      {
        source: '/api/channels/:channelId/reports',
        destination: 'http://app:5001/api/channels/:channelId/reports',
      },
      {
        source: '/api/channels/:channelId/instant-report',
        destination: 'http://app:5001/api/channels/:channelId/instant-report'
      }
    ];
  },
  async headers() {
    return [
      {
        source: '/:path*',
        headers: [
          {
            key: 'Content-Security-Policy',
            value: `
              default-src 'self';
              script-src 'self' 'unsafe-inline' 'unsafe-eval';
              style-src 'self' 'unsafe-inline';
              img-src 'self' data: https:;
              font-src 'self';
              connect-src 'self' http://app:5001 *.sentry.io;
              worker-src 'self' blob:;
            `.replace(/\s+/g, ' ').trim()
          }
        ],
      }
    ];
  }
};

module.exports = nextConfig;