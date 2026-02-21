'use client';

import { useQuery } from '@tanstack/react-query';
import { api, Session } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { formatRelativeTime, getTrustLevelLabel } from '@/lib/utils';
import Link from 'next/link';

function StatusBadge({ status }: { status: Session['status'] }) {
  const variants: Record<Session['status'], 'success' | 'secondary' | 'destructive'> = {
    active: 'success',
    ended: 'secondary',
    expired: 'destructive',
  };
  return <Badge variant={variants[status]}>{status}</Badge>;
}

function TrustBadge({ level }: { level: number }) {
  return (
    <Badge variant={level >= 4 ? 'default' : 'outline'}>
      {getTrustLevelLabel(level)}
    </Badge>
  );
}

export default function SessionsPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['sessions'],
    queryFn: () => api.getSessions({ limit: 50 }),
  });

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="text-destructive p-4">
        Failed to load sessions: {error instanceof Error ? error.message : 'Unknown error'}
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">Sessions</h1>
        <p className="text-muted-foreground">
          View and manage active agent sessions
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Agent Sessions</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b">
                  <th className="text-left p-3 font-medium">Session ID</th>
                  <th className="text-left p-3 font-medium">Agent Tool</th>
                  <th className="text-left p-3 font-medium">User</th>
                  <th className="text-left p-3 font-medium">Service</th>
                  <th className="text-left p-3 font-medium">Trust Level</th>
                  <th className="text-left p-3 font-medium">Status</th>
                  <th className="text-left p-3 font-medium">Started</th>
                  <th className="text-left p-3 font-medium">Last Heartbeat</th>
                </tr>
              </thead>
              <tbody>
                {data?.items?.map((session) => (
                  <tr key={session.id} className="border-b hover:bg-muted/50">
                    <td className="p-3">
                      <Link
                        href={`/sessions/${session.id}`}
                        className="font-mono text-primary hover:underline"
                      >
                        {session.id.substring(0, 8)}...
                      </Link>
                    </td>
                    <td className="p-3">
                      <Badge variant="outline">{session.agent_tool}</Badge>
                    </td>
                    <td className="p-3">{session.user_id || '-'}</td>
                    <td className="p-3">{session.service_id || '-'}</td>
                    <td className="p-3">
                      <TrustBadge level={session.trust_level} />
                    </td>
                    <td className="p-3">
                      <StatusBadge status={session.status} />
                    </td>
                    <td className="p-3 text-muted-foreground">
                      {formatRelativeTime(session.started_at)}
                    </td>
                    <td className="p-3 text-muted-foreground">
                      {session.last_heartbeat
                        ? formatRelativeTime(session.last_heartbeat)
                        : '-'}
                    </td>
                  </tr>
                ))}
                {(!data?.items || data.items.length === 0) && (
                  <tr>
                    <td colSpan={8} className="p-8 text-center text-muted-foreground">
                      No sessions found
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
