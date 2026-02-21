'use client';

import { useQuery } from '@tanstack/react-query';
import { useSession, signOut } from 'next-auth/react';
import { api } from '@/lib/api';
import { Bell, User, Search, LogOut } from 'lucide-react';
import { Button } from '@/components/ui/button';
import Link from 'next/link';

export function Header() {
  const { data: session } = useSession();
  const { data: approvals } = useQuery({
    queryKey: ['pending-approvals'],
    queryFn: () => api.getPendingApprovals({ limit: 1 }),
    refetchInterval: 30000, // Refresh every 30 seconds
  });

  const pendingCount = approvals?.total ?? 0;

  return (
    <header className="flex h-16 items-center justify-between border-b bg-card px-6">
      {/* Search */}
      <div className="flex items-center gap-4 flex-1">
        <div className="relative max-w-md flex-1">
          <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <input
            type="search"
            placeholder="Search sessions, decisions, services..."
            className="h-9 w-full rounded-md border bg-background pl-9 pr-4 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
          />
        </div>
      </div>

      {/* Actions */}
      <div className="flex items-center gap-2">
        {/* Notifications */}
        <Link href="/approvals">
          <Button variant="ghost" size="icon" className="relative">
            <Bell className="h-5 w-5" />
            {pendingCount > 0 && (
              <span className="absolute -right-1 -top-1 flex h-5 w-5 items-center justify-center rounded-full bg-destructive text-xs text-destructive-foreground">
                {pendingCount > 9 ? '9+' : pendingCount}
              </span>
            )}
          </Button>
        </Link>

        {/* User info + logout */}
        {session?.user ? (
          <div className="flex items-center gap-2">
            <Link href="/profile">
              <Button variant="ghost" size="sm" className="gap-2">
                <User className="h-4 w-4" />
                <span className="text-sm">{session.user.name || session.user.email}</span>
                {session.user.role === 'admin' && (
                  <span className="rounded bg-primary/10 px-1.5 py-0.5 text-xs font-medium text-primary">
                    Admin
                  </span>
                )}
              </Button>
            </Link>
            <Button
              variant="ghost"
              size="icon"
              onClick={() => signOut({ callbackUrl: '/login' })}
              title="Sign out"
            >
              <LogOut className="h-4 w-4" />
            </Button>
          </div>
        ) : (
          <Link href="/profile">
            <Button variant="ghost" size="icon">
              <User className="h-5 w-5" />
            </Button>
          </Link>
        )}
      </div>
    </header>
  );
}
