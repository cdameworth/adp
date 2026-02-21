import type { NextConfig } from 'next';

const nextConfig: NextConfig = {
  // Enable strict mode for better React practices
  reactStrictMode: true,

  // Output standalone for Docker deployment
  output: 'standalone',

  // Environment variables
  env: {
    NEXT_PUBLIC_ADP_API_URL: process.env.NEXT_PUBLIC_ADP_API_URL || 'http://localhost:8080',
  },

  // Generate unique build ID to prevent caching issues
  generateBuildId: async () => {
    return `build-${Date.now()}`;
  },

  // Add cache control headers
  async headers() {
    return [
      {
        source: '/:path*',
        headers: [
          {
            key: 'Cache-Control',
            value: 'no-store, must-revalidate',
          },
        ],
      },
    ];
  },

  // Rewrites for API proxy in development
  async rewrites() {
    return [
      {
        source: '/api/v1/:path*',
        destination: `${process.env.ADP_API_URL || 'http://localhost:8080'}/v1/:path*`,
      },
    ];
  },
};

export default nextConfig;
