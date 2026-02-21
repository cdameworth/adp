'use client';

import { useQuery } from '@tanstack/react-query';
import { api } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { StatCard } from '@/components/dashboard/stat-card';
import { AdoptionChart } from '@/components/dashboard/adoption-chart';
import { Activity, Users, AlertTriangle, Shield, Info } from 'lucide-react';

export default function DashboardPage() {
  const { data: summary, isLoading, error } = useQuery({
    queryKey: ['report-summary'],
    queryFn: () => api.getReportSummary(),
    retry: false,
  });

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary" />
      </div>
    );
  }

  // Handle missing reports endpoint gracefully
  const hasData = summary && !error;

  const decisionChange = hasData && summary.decisions_average_7d
    ? ((summary.decisions_today - summary.decisions_average_7d) / summary.decisions_average_7d) * 100
    : 0;

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">Dashboard</h1>
        <p className="text-muted-foreground">
          Overview of agent activity and governance metrics
        </p>
      </div>

      {error && (
        <div className="flex items-center gap-2 p-4 bg-muted/50 border rounded-lg">
          <Info className="h-5 w-5 text-muted-foreground" />
          <span className="text-sm text-muted-foreground">
            Analytics data will appear here once the reporting service is enabled.
          </span>
        </div>
      )}

      {/* Key Metrics */}
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <StatCard
          title="Active Sessions"
          value={hasData ? summary.active_sessions : '-'}
          icon={Activity}
          description="Currently active agent sessions"
        />
        <StatCard
          title="Decisions Today"
          value={hasData ? summary.decisions_today : '-'}
          icon={Users}
          trend={hasData ? decisionChange : undefined}
          description="vs 7-day average"
        />
        <StatCard
          title="Pending Escalations"
          value={hasData ? summary.escalation_queue_depth : '-'}
          icon={AlertTriangle}
          description="Awaiting human approval"
          variant={hasData && summary.escalation_queue_depth > 10 ? 'warning' : 'default'}
        />
        <StatCard
          title="Policy Health"
          value={hasData ? `${summary.policy_health_score}%` : '-'}
          icon={Shield}
          description={
            hasData
              ? (summary.policy_health_score >= 80 ? 'Healthy' :
                 summary.policy_health_score >= 60 ? 'Fair' : 'Needs Attention')
              : 'No data'
          }
          variant={
            hasData
              ? (summary.policy_health_score >= 80 ? 'success' :
                 summary.policy_health_score >= 60 ? 'default' : 'warning')
              : 'default'
          }
        />
      </div>

      {/* Adoption Trend */}
      <Card>
        <CardHeader>
          <CardTitle>30-Day Adoption Trend</CardTitle>
        </CardHeader>
        <CardContent>
          {hasData && summary.adoption_trend_30d?.length > 0 ? (
            <AdoptionChart data={summary.adoption_trend_30d} />
          ) : (
            <div className="h-64 flex items-center justify-center text-muted-foreground">
              <p className="text-sm">No adoption data available yet.</p>
            </div>
          )}
        </CardContent>
      </Card>

      {/* Quick Stats */}
      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>7-Day Average</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-center py-4">
              <div className="text-4xl font-bold text-primary">
                {hasData ? summary.decisions_average_7d?.toFixed(1) : '-'}
              </div>
              <p className="text-muted-foreground text-sm mt-1">
                decisions per day
              </p>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Escalation Queue</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-center py-4">
              <div className={`text-4xl font-bold ${
                hasData && summary.escalation_queue_depth > 10 ? 'text-destructive' : 'text-primary'
              }`}>
                {hasData ? summary.escalation_queue_depth : '-'}
              </div>
              <p className="text-muted-foreground text-sm mt-1">
                pending approvals
              </p>
              {hasData && summary.escalation_queue_depth > 10 && (
                <p className="text-destructive text-xs mt-2">
                  High queue - please review
                </p>
              )}
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
