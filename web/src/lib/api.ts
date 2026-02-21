/**
 * ADP API client for the web dashboard
 */

const API_BASE = process.env.NEXT_PUBLIC_ADP_API_URL || '/api';

export interface Session {
  id: string;
  agent_tool: string;
  user_id: string;
  organization_id: string;
  service_id?: string;
  trust_level: number;
  status: 'active' | 'ended' | 'expired';
  started_at: string;
  ended_at?: string;
  last_heartbeat?: string;
  metadata?: Record<string, unknown>;
}

export interface Decision {
  id: string;
  session_id: string;
  action_type: string;
  target: string;
  result: 'allowed' | 'denied' | 'escalated';
  reasoning?: string;
  confidence_score?: number;
  policy_names?: string[];
  created_at: string;
  metadata?: Record<string, unknown>;
}

export interface Approval {
  id: string;
  session_id: string;
  action: string;
  reason: string;
  status: 'pending' | 'approved' | 'denied' | 'expired';
  requested_at: string;
  resolved_at?: string;
  approver_id?: string;
  comment?: string;
  metadata?: Record<string, unknown>;
}

export interface Service {
  id: string;
  name: string;
  description?: string;
  owner_team?: string;
  owner_user?: string;
  repository_url?: string;
  context_config?: {
    essential_paths?: string[];
    excluded_patterns?: string[];
    token_budget?: {
      essential?: number;
      task_relevant?: number;
      supporting?: number;
    };
  };
  escalation_config?: {
    default_approvers?: string[];
    approval_timeout_hours?: number;
  };
  metadata?: Record<string, unknown>;
  created_at: string;
  updated_at?: string;
}

export interface ReportSummary {
  active_sessions: number;
  decisions_today: number;
  decisions_average_7d: number;
  escalation_queue_depth: number;
  policy_health_score: number;
  adoption_trend_30d: number[];
}

export interface GovernanceReport {
  time_range: { start: string; end: string };
  policy_evaluations: {
    total: number;
    allowed: number;
    denied: number;
    escalated: number;
  };
  policies_by_denial_rate: Array<{
    policy_name: string;
    evaluations: number;
    denial_rate: number;
  }>;
  false_positive_trend: Array<{ date: string; rate: number }>;
}

export interface EscalationReport {
  time_range: { start: string; end: string };
  total_escalations: number;
  approval_rate: number;
  rejection_rate: number;
  average_resolution_time_hours: number;
  escalations_by_policy: Array<{
    policy_name: string;
    count: number;
  }>;
}

export interface LineageNode {
  id: string;
  type: 'decision' | 'session' | 'commit' | 'service' | 'policy';
  label: string;
  timestamp?: string;
  properties?: Record<string, unknown>;
}

export interface LineageEdge {
  source: string;
  target: string;
  relationship: string;
}

export interface LineageGraph {
  nodes: LineageNode[];
  edges: LineageEdge[];
}

export interface Policy {
  id: string;
  organization_id?: string;
  name: string;
  description?: string;
  category: 'security' | 'governance' | 'time_based' | 'financial' | 'performance' | 'custom';
  enabled: boolean;
  priority: number;
  policy_type: 'rego' | 'builtin' | 'custom';
  rego_code?: string;
  builtin_name?: string;
  config?: Record<string, unknown>;
  min_trust_level: number;
  tags?: string[];
  metadata?: Record<string, unknown>;
  created_at: string;
  updated_at: string;
  created_by?: string;
  updated_by?: string;
  // Stats
  trigger_count?: number;
  last_triggered?: string;
}

export interface UserProfile {
  id: string;
  email: string;
  name: string;
  role: string;
  status: string;
  created_at: string;
}

export interface ListResponse<T> {
  items: T[];
  total: number;
  limit: number;
  offset: number;
}

class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
    this.name = 'ApiError';
  }
}

async function fetchApi<T>(path: string, init?: RequestInit): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(init?.headers as Record<string, string>),
  };

  // Inject access token from NextAuth session if available (client-side only)
  if (typeof window !== 'undefined' && !headers['Authorization']) {
    try {
      const { getSession } = await import('next-auth/react');
      const session = await getSession();
      if (session?.accessToken) {
        headers['Authorization'] = `Bearer ${session.accessToken}`;
      }
    } catch {
      // NextAuth not available — skip token injection
    }
  }

  const response = await fetch(`${API_BASE}${path}`, {
    ...init,
    headers,
  });

  if (!response.ok) {
    const text = await response.text();
    throw new ApiError(response.status, text || response.statusText);
  }

  return response.json() as Promise<T>;
}

export const api = {
  // Sessions
  getSessions: (params?: {
    limit?: number;
    offset?: number;
    status?: string;
    service_id?: string;
  }) => {
    const query = new URLSearchParams();
    if (params?.limit) query.set('limit', String(params.limit));
    if (params?.offset) query.set('offset', String(params.offset));
    if (params?.status) query.set('status', params.status);
    if (params?.service_id) query.set('service_id', params.service_id);
    const qs = query.toString();
    return fetchApi<ListResponse<Session>>(`/v1/sessions${qs ? `?${qs}` : ''}`);
  },

  getSession: (id: string) =>
    fetchApi<Session>(`/v1/sessions/${id}`),

  // Approvals
  getApprovals: (params?: {
    limit?: number;
    offset?: number;
    status?: string;
  }) => {
    const query = new URLSearchParams();
    if (params?.limit) query.set('limit', String(params.limit));
    if (params?.offset) query.set('offset', String(params.offset));
    if (params?.status) query.set('status', params.status);
    const qs = query.toString();
    return fetchApi<ListResponse<Approval>>(`/v1/governance/approvals${qs ? `?${qs}` : ''}`);
  },

  getPendingApprovals: (params?: { limit?: number; offset?: number }) => {
    const query = new URLSearchParams();
    if (params?.limit) query.set('limit', String(params.limit));
    if (params?.offset) query.set('offset', String(params.offset));
    const qs = query.toString();
    return fetchApi<ListResponse<Approval>>(`/v1/governance/approvals/pending${qs ? `?${qs}` : ''}`);
  },

  resolveApproval: (id: string, data: { status: 'approved' | 'denied'; comment?: string }) =>
    fetchApi<Approval>(`/v1/governance/approvals/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(data),
    }),

  // Decisions/Audit
  getDecisions: (params?: {
    limit?: number;
    offset?: number;
    session_id?: string;
    result?: string;
    since?: string;
    until?: string;
  }) => {
    const query = new URLSearchParams();
    if (params?.limit) query.set('limit', String(params.limit));
    if (params?.offset) query.set('offset', String(params.offset));
    if (params?.session_id) query.set('session_id', params.session_id);
    if (params?.result) query.set('result', params.result);
    if (params?.since) query.set('since', params.since);
    if (params?.until) query.set('until', params.until);
    const qs = query.toString();
    return fetchApi<ListResponse<Decision>>(`/v1/audit/decisions${qs ? `?${qs}` : ''}`);
  },

  getDecision: (id: string) =>
    fetchApi<Decision>(`/v1/audit/decisions/${id}`),

  getDecisionLineage: (id: string, depth?: number) => {
    const query = depth ? `?depth=${depth}` : '';
    return fetchApi<LineageGraph>(`/v1/audit/decisions/${id}/lineage${query}`);
  },

  // Services
  getServices: (params?: { limit?: number; offset?: number }) => {
    const query = new URLSearchParams();
    if (params?.limit) query.set('limit', String(params.limit));
    if (params?.offset) query.set('offset', String(params.offset));
    const qs = query.toString();
    return fetchApi<ListResponse<Service>>(`/v1/services${qs ? `?${qs}` : ''}`);
  },

  getService: (id: string) =>
    fetchApi<Service>(`/v1/services/${id}`),

  createService: (data: Partial<Service>) =>
    fetchApi<Service>('/v1/services', {
      method: 'POST',
      body: JSON.stringify(data),
    }),

  updateService: (id: string, data: Partial<Service>) =>
    fetchApi<Service>(`/v1/services/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(data),
    }),

  deleteService: (id: string) =>
    fetchApi<void>(`/v1/services/${id}`, { method: 'DELETE' }),

  // Reports
  getReportSummary: () =>
    fetchApi<ReportSummary>('/v1/reports/summary'),

  getGovernanceReport: (params?: {
    start?: string;
    end?: string;
    granularity?: 'hour' | 'day' | 'week' | 'month';
  }) => {
    const query = new URLSearchParams();
    if (params?.start) query.set('start', params.start);
    if (params?.end) query.set('end', params.end);
    if (params?.granularity) query.set('granularity', params.granularity);
    const qs = query.toString();
    return fetchApi<GovernanceReport>(`/v1/reports/governance${qs ? `?${qs}` : ''}`);
  },

  getEscalationReport: (params?: { start?: string; end?: string }) => {
    const query = new URLSearchParams();
    if (params?.start) query.set('start', params.start);
    if (params?.end) query.set('end', params.end);
    const qs = query.toString();
    return fetchApi<EscalationReport>(`/v1/reports/escalations${qs ? `?${qs}` : ''}`);
  },

  exportComplianceReport: (params: {
    start: string;
    end: string;
    format: 'json' | 'csv' | 'prometheus';
  }) => {
    const query = new URLSearchParams();
    query.set('start', params.start);
    query.set('end', params.end);
    query.set('format', params.format);
    return fetchApi<unknown>(`/v1/reports/compliance?${query.toString()}`);
  },

  // Policies
  getPolicies: (params?: {
    limit?: number;
    offset?: number;
    category?: string;
    enabled?: boolean;
    policy_type?: string;
  }) => {
    const query = new URLSearchParams();
    if (params?.limit) query.set('limit', String(params.limit));
    if (params?.offset) query.set('offset', String(params.offset));
    if (params?.category) query.set('category', params.category);
    if (params?.enabled !== undefined) query.set('enabled', String(params.enabled));
    if (params?.policy_type) query.set('policy_type', params.policy_type);
    const qs = query.toString();
    return fetchApi<ListResponse<Policy>>(`/v1/policies${qs ? `?${qs}` : ''}`);
  },

  getPolicy: (id: string) =>
    fetchApi<Policy>(`/v1/policies/${id}`),

  createPolicy: (data: Partial<Policy>) =>
    fetchApi<Policy>('/v1/policies', {
      method: 'POST',
      body: JSON.stringify(data),
    }),

  updatePolicy: (id: string, data: Partial<Policy>) =>
    fetchApi<Policy>(`/v1/policies/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(data),
    }),

  deletePolicy: (id: string) =>
    fetchApi<void>(`/v1/policies/${id}`, { method: 'DELETE' }),

  togglePolicyEnabled: (id: string) =>
    fetchApi<Policy>(`/v1/policies/${id}/toggle`, { method: 'PATCH' }),

  // Auth / Profile
  getProfile: () =>
    fetchApi<{ data: UserProfile }>('/v1/auth/me'),

  updateProfile: (data: { name: string }) =>
    fetchApi<{ data: UserProfile }>('/v1/auth/me', {
      method: 'PATCH',
      body: JSON.stringify(data),
    }),

  changePassword: (data: { current_password: string; new_password: string }) =>
    fetchApi<{ data: { message: string } }>('/v1/auth/change-password', {
      method: 'POST',
      body: JSON.stringify(data),
    }),

  // Admin: Users
  getUsers: (params?: { limit?: number; offset?: number }) => {
    const query = new URLSearchParams();
    if (params?.limit) query.set('limit', String(params.limit));
    if (params?.offset) query.set('offset', String(params.offset));
    const qs = query.toString();
    return fetchApi<ListResponse<UserProfile>>(`/v1/admin/users${qs ? `?${qs}` : ''}`);
  },

  getUser: (id: string) =>
    fetchApi<{ data: UserProfile }>(`/v1/admin/users/${id}`),

  createUser: (data: { email: string; name: string; password: string; role: string }) =>
    fetchApi<{ data: UserProfile }>('/v1/admin/users', {
      method: 'POST',
      body: JSON.stringify(data),
    }),

  updateUser: (id: string, data: { name?: string; role?: string; status?: string }) =>
    fetchApi<{ data: UserProfile }>(`/v1/admin/users/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(data),
    }),

  disableUser: (id: string) =>
    fetchApi<void>(`/v1/admin/users/${id}`, { method: 'DELETE' }),
};
