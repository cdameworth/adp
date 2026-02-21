export { default } from 'next-auth/middleware';

export const config = {
  matcher: [
    /*
     * Match all paths except:
     * - /login (auth page)
     * - /api/auth/* (NextAuth API routes)
     * - /_next/* (Next.js internals)
     * - /favicon.png, /logo.png (static assets)
     */
    '/((?!login|api/auth|_next/static|_next/image|favicon\\.png|logo\\.png).*)',
  ],
};
