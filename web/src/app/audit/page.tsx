'use client';

import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api, Decision } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { formatRelativeTime } from '@/lib/utils';
import { ChevronDown, ChevronUp, GitGraph } from 'lucide-react';
import Link from 'next/link';

function ResultBadge({ result }: { result: Decision['result'] }) {
  const variants: Record<Decision['result'], 'success' | 'destructive' | 'warning'> = {
    allowed: 'success',
    denied: 'destructive',
    escalated: 'warning',
  };
  return <Badge variant={variants[result]}>{result}</Badge>;
}

function DecisionCard({ decision }: { decision: Decision }) {
  const [expanded, setExpanded] = useState(false);

  return (
    <div className="border rounded-lg p-4 hover:bg-muted/30 transition-colors">
      <div className="flex items-start justify-between">
        <div className="space-y-1">
          <div className="flex items-center gap-2">
            <span className="font-medium">{decision.action_type}</span>
            <ResultBadge result={decision.result} />
          </div>
          <p className="text-sm text-muted-foreground font-mono">{decision.target}</p>
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <span>{formatRelativeTime(decision.created_at)}</span>
            {decision.confidence_score !== undefined && (
              <Badge variant="outline" className="text-xs">
                Confidence: {(decision.confidence_score * 100).toFixed(0)}%
              </Badge>
            )}
            {decision.policy_names?.map((policy) => (
              <Badge key={policy} variant="outline" className="text-xs">
                {policy}
              </Badge>
            ))}
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Link href={`/lineage/${decision.id}`}>
            <Button variant="outline" size="sm">
              <GitGraph className="h-4 w-4 mr-1" />
              Lineage
            </Button>
          </Link>
          {decision.reasoning && (
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setExpanded(!expanded)}
            >
              {expanded ? (
                <ChevronUp className="h-4 w-4" />
              ) : (
                <ChevronDown className="h-4 w-4" />
              )}
            </Button>
          )}
        </div>
      </div>

      {expanded && decision.reasoning && (
        <div className="mt-4 p-3 bg-muted rounded-md">
          <p className="text-sm font-medium mb-1">Reasoning:</p>
          <pre className="text-xs text-muted-foreground whitespace-pre-wrap font-mono">
            {decision.reasoning}
          </pre>
        </div>
      )}
    </div>
  );
}

export default function AuditPage() {
  const [filter, setFilter] = useState<string | undefined>();

  const { data, isLoading, error } = useQuery({
    queryKey: ['decisions', filter],
    queryFn: () => api.getDecisions({ limit: 50, result: filter }),
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

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">Audit Log</h1>
        <p className="text-muted-foreground">
          Explore agent decisions with full reasoning traces
        </p>
      </div>

      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle>Decisions</CardTitle>
            <div className="flex gap-2">
              <Button
                variant={filter === undefined ? 'default' : 'outline'}
                size="sm"
                onClick={() => setFilter(undefined)}
              >
                All
              </Button>
              <Button
                variant={filter === 'allowed' ? 'default' : 'outline'}
                size="sm"
                onClick={() => setFilter('allowed')}
              >
                Allowed
              </Button>
              <Button
                variant={filter === 'denied' ? 'default' : 'outline'}
                size="sm"
                onClick={() => setFilter('denied')}
              >
                Denied
              </Button>
              <Button
                variant={filter === 'escalated' ? 'default' : 'outline'}
                size="sm"
                onClick={() => setFilter('escalated')}
              >
                Escalated
              </Button>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <div className="space-y-3">
            {data?.items?.map((decision) => (
              <DecisionCard key={decision.id} decision={decision} />
            ))}
            {(!data?.items || data.items.length === 0) && (
              <div className="p-8 text-center text-muted-foreground">
                No decisions found
              </div>
            )}
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
