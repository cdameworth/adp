'use client';

import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api, Approval } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { formatRelativeTime } from '@/lib/utils';
import { Check, X, AlertTriangle } from 'lucide-react';
import Link from 'next/link';

function StatusBadge({ status }: { status: Approval['status'] }) {
  const variants: Record<Approval['status'], 'warning' | 'success' | 'destructive' | 'secondary'> = {
    pending: 'warning',
    approved: 'success',
    denied: 'destructive',
    expired: 'secondary',
  };
  return <Badge variant={variants[status]}>{status}</Badge>;
}

interface ApprovalDialogProps {
  approval: Approval;
  onClose: () => void;
  onResolve: (status: 'approved' | 'denied', comment?: string) => void;
  isLoading: boolean;
}

function ApprovalDialog({ approval, onClose, onResolve, isLoading }: ApprovalDialogProps) {
  const [comment, setComment] = useState('');

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div className="bg-card rounded-lg shadow-lg max-w-lg w-full mx-4 p-6">
        <h2 className="text-xl font-bold mb-4">Review Approval Request</h2>

        <div className="space-y-4 mb-6">
          <div>
            <label className="text-sm font-medium text-muted-foreground">Action</label>
            <p className="mt-1">{approval.action}</p>
          </div>
          <div>
            <label className="text-sm font-medium text-muted-foreground">Reason</label>
            <p className="mt-1">{approval.reason}</p>
          </div>
          <div>
            <label className="text-sm font-medium text-muted-foreground">Session</label>
            <p className="mt-1">
              <Link href={`/sessions/${approval.session_id}`} className="text-primary hover:underline font-mono">
                {approval.session_id}
              </Link>
            </p>
          </div>
          <div>
            <label className="text-sm font-medium text-muted-foreground">Requested</label>
            <p className="mt-1">{formatRelativeTime(approval.requested_at)}</p>
          </div>
          <div>
            <label className="text-sm font-medium text-muted-foreground block mb-2">Comment (optional)</label>
            <textarea
              className="w-full rounded-md border bg-background px-3 py-2 text-sm"
              rows={3}
              value={comment}
              onChange={(e) => setComment(e.target.value)}
              placeholder="Add a comment..."
            />
          </div>
        </div>

        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={onClose} disabled={isLoading}>
            Cancel
          </Button>
          <Button
            variant="destructive"
            onClick={() => onResolve('denied', comment || undefined)}
            disabled={isLoading}
          >
            <X className="h-4 w-4 mr-2" />
            Deny
          </Button>
          <Button
            variant="success"
            onClick={() => onResolve('approved', comment || undefined)}
            disabled={isLoading}
          >
            <Check className="h-4 w-4 mr-2" />
            Approve
          </Button>
        </div>
      </div>
    </div>
  );
}

export default function ApprovalsPage() {
  const queryClient = useQueryClient();
  const [selectedApproval, setSelectedApproval] = useState<Approval | null>(null);

  const { data, isLoading, error } = useQuery({
    queryKey: ['pending-approvals'],
    queryFn: () => api.getPendingApprovals({ limit: 50 }),
  });

  const mutation = useMutation({
    mutationFn: ({ id, status, comment }: { id: string; status: 'approved' | 'denied'; comment?: string }) =>
      api.resolveApproval(id, { status, comment }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['pending-approvals'] });
      setSelectedApproval(null);
    },
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
        Failed to load approvals: {error instanceof Error ? error.message : 'Unknown error'}
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">Approvals</h1>
        <p className="text-muted-foreground">
          Review and resolve escalated agent actions
        </p>
      </div>

      {data?.items && data.items.length > 0 && (
        <div className="flex items-center gap-2 p-4 bg-warning/10 border border-warning/20 rounded-lg">
          <AlertTriangle className="h-5 w-5 text-warning" />
          <span className="text-sm">
            <strong>{data.items.length}</strong> pending approval{data.items.length !== 1 ? 's' : ''} requiring attention
          </span>
        </div>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Pending Approvals</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b">
                  <th className="text-left p-3 font-medium">Action</th>
                  <th className="text-left p-3 font-medium">Reason</th>
                  <th className="text-left p-3 font-medium">Session</th>
                  <th className="text-left p-3 font-medium">Status</th>
                  <th className="text-left p-3 font-medium">Requested</th>
                  <th className="text-left p-3 font-medium">Actions</th>
                </tr>
              </thead>
              <tbody>
                {data?.items?.map((approval) => (
                  <tr key={approval.id} className="border-b hover:bg-muted/50">
                    <td className="p-3">{approval.action}</td>
                    <td className="p-3 max-w-xs truncate">{approval.reason}</td>
                    <td className="p-3">
                      <Link
                        href={`/sessions/${approval.session_id}`}
                        className="font-mono text-primary hover:underline"
                      >
                        {approval.session_id.substring(0, 8)}...
                      </Link>
                    </td>
                    <td className="p-3">
                      <StatusBadge status={approval.status} />
                    </td>
                    <td className="p-3 text-muted-foreground">
                      {formatRelativeTime(approval.requested_at)}
                    </td>
                    <td className="p-3">
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => setSelectedApproval(approval)}
                      >
                        Review
                      </Button>
                    </td>
                  </tr>
                ))}
                {(!data?.items || data.items.length === 0) && (
                  <tr>
                    <td colSpan={6} className="p-8 text-center text-muted-foreground">
                      No pending approvals
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>

      {selectedApproval && (
        <ApprovalDialog
          approval={selectedApproval}
          onClose={() => setSelectedApproval(null)}
          onResolve={(status, comment) => mutation.mutate({ id: selectedApproval.id, status, comment })}
          isLoading={mutation.isPending}
        />
      )}
    </div>
  );
}
