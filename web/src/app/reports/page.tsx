'use client';

import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { AlertCircle, BarChart3 } from 'lucide-react';
import {
  PieChart,
  Pie,
  Cell,
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  LineChart,
  Line,
} from 'recharts';

const COLORS = ['#22c55e', '#ef4444', '#f59e0b'];

export default function ReportsPage() {
  const [tab, setTab] = useState<'governance' | 'escalations'>('governance');

  const { data: govReport, isLoading: govLoading, error: govError } = useQuery({
    queryKey: ['governance-report'],
    queryFn: () => api.getGovernanceReport({
      start: new Date(Date.now() - 30 * 24 * 60 * 60 * 1000).toISOString(),
      end: new Date().toISOString(),
    }),
    retry: false,
  });

  const { data: escReport, isLoading: escLoading, error: escError } = useQuery({
    queryKey: ['escalation-report'],
    queryFn: () => api.getEscalationReport({
      start: new Date(Date.now() - 30 * 24 * 60 * 60 * 1000).toISOString(),
      end: new Date().toISOString(),
    }),
    retry: false,
  });

  const isLoading = govLoading || escLoading;
  const hasGovData = govReport && !govError;
  const hasEscData = escReport && !escError;

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary" />
      </div>
    );
  }

  const pieData = hasGovData ? [
    { name: 'Allowed', value: govReport.policy_evaluations?.allowed || 0, color: '#22c55e' },
    { name: 'Denied', value: govReport.policy_evaluations?.denied || 0, color: '#ef4444' },
    { name: 'Escalated', value: govReport.policy_evaluations?.escalated || 0, color: '#f59e0b' },
  ].filter(d => d.value > 0) : [];

  const total = hasGovData ? (govReport.policy_evaluations?.total || 1) : 1;
  const allowedPct = hasGovData ? ((govReport.policy_evaluations?.allowed || 0) / total * 100).toFixed(1) : '0';
  const deniedPct = hasGovData ? ((govReport.policy_evaluations?.denied || 0) / total * 100).toFixed(1) : '0';
  const escalatedPct = hasGovData ? ((govReport.policy_evaluations?.escalated || 0) / total * 100).toFixed(1) : '0';

  const NoDataMessage = ({ title, description }: { title: string; description: string }) => (
    <div className="text-center py-12 text-muted-foreground">
      <BarChart3 className="h-12 w-12 mx-auto mb-4 opacity-50" />
      <h3 className="font-medium text-lg mb-2">{title}</h3>
      <p className="text-sm">{description}</p>
    </div>
  );

  const ErrorMessage = ({ message }: { message: string }) => (
    <div className="text-center py-12">
      <AlertCircle className="h-12 w-12 mx-auto mb-4 text-muted-foreground opacity-50" />
      <h3 className="font-medium text-lg mb-2 text-muted-foreground">Data Unavailable</h3>
      <p className="text-sm text-muted-foreground">{message}</p>
    </div>
  );

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">Reports</h1>
        <p className="text-muted-foreground">
          Analytics on governance effectiveness and escalation patterns
        </p>
      </div>

      {/* Tabs */}
      <div className="flex gap-2">
        <Button
          variant={tab === 'governance' ? 'default' : 'outline'}
          onClick={() => setTab('governance')}
        >
          Governance
        </Button>
        <Button
          variant={tab === 'escalations' ? 'default' : 'outline'}
          onClick={() => setTab('escalations')}
        >
          Escalations
        </Button>
      </div>

      {tab === 'governance' && (
        <div className="grid gap-6 lg:grid-cols-2">
          {/* Policy Breakdown */}
          <Card>
            <CardHeader>
              <CardTitle>Policy Evaluation Breakdown</CardTitle>
            </CardHeader>
            <CardContent>
              {govError ? (
                <ErrorMessage message="Reports endpoint not available. Policy evaluations will appear here once reporting is enabled." />
              ) : !hasGovData || pieData.length === 0 ? (
                <NoDataMessage
                  title="No Policy Evaluations Yet"
                  description="Policy evaluation data will appear here as agents interact with the governance system."
                />
              ) : (
                <>
                  <div className="flex justify-around py-4 text-center border-b mb-4">
                    <div>
                      <p className="text-2xl font-bold text-primary">{(govReport.policy_evaluations?.total || 0).toLocaleString()}</p>
                      <p className="text-xs text-muted-foreground">Total</p>
                    </div>
                    <div>
                      <p className="text-2xl font-bold text-success">{allowedPct}%</p>
                      <p className="text-xs text-muted-foreground">Allowed</p>
                    </div>
                    <div>
                      <p className="text-2xl font-bold text-destructive">{deniedPct}%</p>
                      <p className="text-xs text-muted-foreground">Denied</p>
                    </div>
                    <div>
                      <p className="text-2xl font-bold text-warning">{escalatedPct}%</p>
                      <p className="text-xs text-muted-foreground">Escalated</p>
                    </div>
                  </div>
                  <div className="h-64">
                    <ResponsiveContainer width="100%" height="100%">
                      <PieChart>
                        <Pie
                          data={pieData}
                          cx="50%"
                          cy="50%"
                          innerRadius={60}
                          outerRadius={100}
                          paddingAngle={2}
                          dataKey="value"
                        >
                          {pieData.map((entry, index) => (
                            <Cell key={`cell-${index}`} fill={entry.color} />
                          ))}
                        </Pie>
                        <Tooltip />
                      </PieChart>
                    </ResponsiveContainer>
                  </div>
                </>
              )}
            </CardContent>
          </Card>

          {/* Policies by Denial Rate */}
          <Card>
            <CardHeader>
              <CardTitle>Top Policies by Denial Rate</CardTitle>
            </CardHeader>
            <CardContent>
              {govError ? (
                <ErrorMessage message="Reports endpoint not available." />
              ) : !hasGovData || !govReport.policies_by_denial_rate?.length ? (
                <NoDataMessage
                  title="No Denial Data Yet"
                  description="Policy denial rate data will appear here as policies are evaluated."
                />
              ) : (
                <div className="h-64">
                  <ResponsiveContainer width="100%" height="100%">
                    <BarChart
                      data={govReport.policies_by_denial_rate.slice(0, 10)}
                      layout="vertical"
                      margin={{ left: 100 }}
                    >
                      <CartesianGrid strokeDasharray="3 3" />
                      <XAxis type="number" domain={[0, 100]} unit="%" />
                      <YAxis
                        type="category"
                        dataKey="policy_name"
                        width={90}
                        tick={{ fontSize: 11 }}
                      />
                      <Tooltip />
                      <Bar dataKey="denial_rate" fill="#ef4444" radius={[0, 4, 4, 0]} />
                    </BarChart>
                  </ResponsiveContainer>
                </div>
              )}
            </CardContent>
          </Card>

          {/* False Positive Trend */}
          <Card className="lg:col-span-2">
            <CardHeader>
              <CardTitle>False Positive Rate Trend</CardTitle>
            </CardHeader>
            <CardContent>
              {govError ? (
                <ErrorMessage message="Reports endpoint not available." />
              ) : !hasGovData || !govReport.false_positive_trend?.length ? (
                <NoDataMessage
                  title="No Trend Data Yet"
                  description="False positive trend data will appear here over time as escalations are resolved."
                />
              ) : (
                <>
                  <div className="h-64">
                    <ResponsiveContainer width="100%" height="100%">
                      <LineChart data={govReport.false_positive_trend}>
                        <CartesianGrid strokeDasharray="3 3" />
                        <XAxis
                          dataKey="date"
                          tickFormatter={(v) => new Date(v).toLocaleDateString()}
                          tick={{ fontSize: 10 }}
                        />
                        <YAxis domain={[0, 100]} unit="%" />
                        <Tooltip labelFormatter={(v) => new Date(v).toLocaleDateString()} />
                        <Line
                          type="monotone"
                          dataKey="rate"
                          stroke="#f59e0b"
                          strokeWidth={2}
                          dot={false}
                        />
                      </LineChart>
                    </ResponsiveContainer>
                  </div>
                  <p className="text-xs text-muted-foreground mt-2">
                    False positive rate = % of denials later approved on escalation.
                  </p>
                </>
              )}
            </CardContent>
          </Card>
        </div>
      )}

      {tab === 'escalations' && (
        <div className="grid gap-6 lg:grid-cols-2">
          {escError ? (
            <Card className="lg:col-span-2">
              <CardContent className="py-12">
                <ErrorMessage message="Escalations report endpoint not available. Escalation analytics will appear here once reporting is enabled." />
              </CardContent>
            </Card>
          ) : !hasEscData ? (
            <Card className="lg:col-span-2">
              <CardContent className="py-12">
                <NoDataMessage
                  title="No Escalation Data Yet"
                  description="Escalation analytics will appear here as approval requests are created and resolved."
                />
              </CardContent>
            </Card>
          ) : (
            <>
              {/* Key Metrics */}
              <div className="grid gap-4 grid-cols-2">
                <Card>
                  <CardContent className="p-6 text-center">
                    <p className="text-3xl font-bold text-primary">{escReport.total_escalations || 0}</p>
                    <p className="text-sm text-muted-foreground">Total Escalations</p>
                  </CardContent>
                </Card>
                <Card>
                  <CardContent className="p-6 text-center">
                    <p className="text-3xl font-bold text-success">{(escReport.approval_rate || 0).toFixed(1)}%</p>
                    <p className="text-sm text-muted-foreground">Approval Rate</p>
                  </CardContent>
                </Card>
                <Card>
                  <CardContent className="p-6 text-center">
                    <p className="text-3xl font-bold text-destructive">{(escReport.rejection_rate || 0).toFixed(1)}%</p>
                    <p className="text-sm text-muted-foreground">Rejection Rate</p>
                  </CardContent>
                </Card>
                <Card>
                  <CardContent className="p-6 text-center">
                    <p className="text-3xl font-bold text-primary">
                      {(escReport.average_resolution_time_hours || 0) < 1
                        ? `${Math.round((escReport.average_resolution_time_hours || 0) * 60)}m`
                        : `${(escReport.average_resolution_time_hours || 0).toFixed(1)}h`}
                    </p>
                    <p className="text-sm text-muted-foreground">Avg Resolution Time</p>
                  </CardContent>
                </Card>
              </div>

              {/* Escalations by Policy */}
              <Card>
                <CardHeader>
                  <CardTitle>Escalations by Policy</CardTitle>
                </CardHeader>
                <CardContent>
                  {!escReport.escalations_by_policy?.length ? (
                    <NoDataMessage
                      title="No Policy Escalation Data"
                      description="Policy escalation data will appear here as escalations occur."
                    />
                  ) : (
                    <div className="h-64">
                      <ResponsiveContainer width="100%" height="100%">
                        <BarChart
                          data={escReport.escalations_by_policy.slice(0, 10)}
                          layout="vertical"
                          margin={{ left: 100 }}
                        >
                          <CartesianGrid strokeDasharray="3 3" />
                          <XAxis type="number" />
                          <YAxis
                            type="category"
                            dataKey="policy_name"
                            width={90}
                            tick={{ fontSize: 11 }}
                          />
                          <Tooltip />
                          <Bar dataKey="count" fill="#f59e0b" radius={[0, 4, 4, 0]} />
                        </BarChart>
                      </ResponsiveContainer>
                    </div>
                  )}
                </CardContent>
              </Card>
            </>
          )}
        </div>
      )}
    </div>
  );
}
