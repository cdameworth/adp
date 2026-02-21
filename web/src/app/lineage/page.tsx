'use client';

import { useQuery } from '@tanstack/react-query';
import Link from 'next/link';
import { api, Decision } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { GitGraph, ArrowRight, Clock, CheckCircle, XCircle, AlertTriangle } from 'lucide-react';

const resultIcons: Record<string, React.ReactNode> = {
  allowed: <CheckCircle className="h-4 w-4 text-green-500" />,
  denied: <XCircle className="h-4 w-4 text-red-500" />,
  escalated: <AlertTriangle className="h-4 w-4 text-yellow-500" />,
};

const resultColors: Record<string, string> = {
  allowed: 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200',
  denied: 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200',
  escalated: 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200',
};

export default function LineagePage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['decisions', 'lineage-list'],
    queryFn: () => api.getDecisions({ limit: 50 }),
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
        Failed to load decisions: {error instanceof Error ? error.message : 'Unknown error'}
      </div>
    );
  }

  const decisions = data?.items || [];

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">Decision Lineage</h1>
        <p className="text-muted-foreground">
          Explore decision chains and their relationships across sessions, services, and policies
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <GitGraph className="h-5 w-5" />
            Recent Decisions
          </CardTitle>
        </CardHeader>
        <CardContent>
          {decisions.length === 0 ? (
            <div className="text-center py-8 text-muted-foreground">
              <GitGraph className="h-12 w-12 mx-auto mb-4 opacity-50" />
              <p>No decisions recorded yet.</p>
              <p className="text-sm mt-2">
                Decisions will appear here as agents interact with the governance system.
              </p>
            </div>
          ) : (
            <div className="space-y-3">
              {decisions.map((decision: Decision) => (
                <Link
                  key={decision.id}
                  href={`/lineage/${decision.id}`}
                  className="block p-4 rounded-lg border bg-card hover:bg-muted/50 transition-colors"
                >
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-3">
                      {resultIcons[decision.result]}
                      <div>
                        <div className="font-medium">
                          {decision.action_type}
                          <span className="text-muted-foreground font-normal ml-2">
                            on {decision.target}
                          </span>
                        </div>
                        <div className="text-sm text-muted-foreground flex items-center gap-2 mt-1">
                          <Clock className="h-3 w-3" />
                          {new Date(decision.created_at).toLocaleString()}
                          {decision.session_id && (
                            <>
                              <span className="mx-1">•</span>
                              <span className="font-mono text-xs">
                                Session: {decision.session_id.slice(0, 8)}...
                              </span>
                            </>
                          )}
                        </div>
                      </div>
                    </div>
                    <div className="flex items-center gap-3">
                      <Badge className={resultColors[decision.result]}>
                        {decision.result}
                      </Badge>
                      {decision.confidence_score !== undefined && (
                        <span className="text-sm text-muted-foreground">
                          {Math.round(decision.confidence_score * 100)}% confidence
                        </span>
                      )}
                      <ArrowRight className="h-4 w-4 text-muted-foreground" />
                    </div>
                  </div>
                  {decision.reasoning && (
                    <p className="text-sm text-muted-foreground mt-2 line-clamp-2">
                      {decision.reasoning}
                    </p>
                  )}
                  {decision.policy_names && decision.policy_names.length > 0 && (
                    <div className="flex gap-1 mt-2">
                      {decision.policy_names.map((policy) => (
                        <Badge key={policy} variant="outline" className="text-xs">
                          {policy}
                        </Badge>
                      ))}
                    </div>
                  )}
                </Link>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
