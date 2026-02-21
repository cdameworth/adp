'use client';

import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api, Policy } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Shield, AlertTriangle, Clock, DollarSign, GitBranch, Activity, Plus, X, Settings, Trash2, Power, Server, Network, CheckCircle2, Info, Lock, Code, Wrench } from 'lucide-react';

const categoryIcons: Record<string, React.ElementType> = {
  security: Shield,
  governance: GitBranch,
  time_based: Clock,
  financial: DollarSign,
  performance: Activity,
  custom: Settings,
};

const categoryColors: Record<string, string> = {
  security: 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200',
  governance: 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200',
  time_based: 'bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-200',
  financial: 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200',
  performance: 'bg-orange-100 text-orange-800 dark:bg-orange-900 dark:text-orange-200',
  custom: 'bg-gray-100 text-gray-800 dark:bg-gray-900 dark:text-gray-200',
};

const categoryLabels: Record<string, string> = {
  security: 'Security',
  governance: 'Governance',
  time_based: 'Time-Based',
  financial: 'Financial',
  performance: 'Performance',
  custom: 'Custom',
};

// Built-in policy definitions with their metadata
const BUILTIN_POLICIES = [
  {
    name: 'deny_sensitive_files',
    description: 'Block access to sensitive files like .env, .pem, .key, credentials',
    category: 'security',
    defaultConfig: { patterns: ['.env', '.pem', '.key', '*.secret', 'credentials.*'] },
  },
  {
    name: 'blast_radius_limit',
    description: 'Limit the number of files that can be modified in a single action',
    category: 'governance',
    defaultConfig: { max_files: 10, trust_level_override: 4 },
  },
  {
    name: 'off_hours_production',
    description: 'Block production deployments during off-hours (10 PM - 6 AM)',
    category: 'time_based',
    defaultConfig: { start_hour: 22, end_hour: 6, min_trust_level: 5 },
  },
  {
    name: 'cost_limit',
    description: 'Enforce cost limits based on trust level',
    category: 'financial',
    defaultConfig: { limits_by_trust: { '1': 10, '2': 50, '3': 200, '4': 1000, '5': 10000 } },
  },
  {
    name: 'require_migration_approval',
    description: 'Require human approval for database migrations and schema changes',
    category: 'governance',
    defaultConfig: { action_types: ['migrate_database', 'alter_schema'] },
  },
  {
    name: 'rate_limit_api',
    description: 'Rate limit API calls per session',
    category: 'performance',
    defaultConfig: { requests_per_minute: 60 },
  },
];

// Example Rego templates
const REGO_TEMPLATES = [
  {
    name: 'Block Production Deploy',
    code: `package policy

deny[msg] {
  input.action.type == "deploy"
  input.context.environment == "production"
  input.session.trust_level < 4
  msg := "Production deployments require trust level 4+"
}`,
  },
  {
    name: 'Restrict Vendor Directory',
    code: `package policy

deny[msg] {
  input.action.type == "modify_file"
  some path in input.action.target.paths
  contains(path, "vendor/")
  msg := "Cannot modify files in vendor directory"
}`,
  },
  {
    name: 'Require Tests for Code Changes',
    code: `package policy

deny[msg] {
  input.action.type == "modify_file"
  some path in input.action.target.paths
  endswith(path, ".go")
  not has_test_file(input.action.target.paths)
  msg := "Code changes must include corresponding test files"
}

has_test_file(paths) {
  some path in paths
  endswith(path, "_test.go")
}`,
  },
];

interface PolicyDialogProps {
  policy?: Policy;
  onClose: () => void;
  onSubmit: (data: Partial<Policy>) => void;
  isLoading: boolean;
}

function PolicyDialog({ policy, onClose, onSubmit, isLoading }: PolicyDialogProps) {
  const [name, setName] = useState(policy?.name || '');
  const [description, setDescription] = useState(policy?.description || '');
  const [category, setCategory] = useState<string>(policy?.category || 'governance');
  const [policyType, setPolicyType] = useState<string>(policy?.policy_type || 'builtin');
  const [builtinName, setBuiltinName] = useState(policy?.builtin_name || '');
  const [regoCode, setRegoCode] = useState(policy?.rego_code || '');
  const [minTrustLevel, setMinTrustLevel] = useState(policy?.min_trust_level || 1);
  const [priority, setPriority] = useState(policy?.priority || 100);
  const [enabled, setEnabled] = useState(policy?.enabled ?? true);
  const [configJson, setConfigJson] = useState(
    policy?.config ? JSON.stringify(policy.config, null, 2) : '{}'
  );

  const handleBuiltinSelect = (selectedName: string) => {
    const builtin = BUILTIN_POLICIES.find(p => p.name === selectedName);
    if (builtin) {
      setBuiltinName(selectedName);
      setName(builtin.name);
      setDescription(builtin.description);
      setCategory(builtin.category);
      setConfigJson(JSON.stringify(builtin.defaultConfig, null, 2));
    }
  };

  const handleRegoTemplateSelect = (templateName: string) => {
    const template = REGO_TEMPLATES.find(t => t.name === templateName);
    if (template) {
      setRegoCode(template.code);
    }
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;

    let config: Record<string, unknown> | undefined;
    try {
      const parsed = JSON.parse(configJson);
      if (Object.keys(parsed).length > 0) {
        config = parsed;
      }
    } catch {
      // Invalid JSON, ignore config
    }

    onSubmit({
      name: name.trim(),
      description: description.trim() || undefined,
      category: category as Policy['category'],
      policy_type: policyType as Policy['policy_type'],
      builtin_name: policyType === 'builtin' ? builtinName.trim() || undefined : undefined,
      rego_code: policyType === 'rego' ? regoCode.trim() || undefined : undefined,
      config,
      min_trust_level: minTrustLevel,
      priority,
      enabled,
    });
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div className="bg-card rounded-lg shadow-lg max-w-2xl w-full mx-4 p-6 max-h-[90vh] overflow-y-auto">
        <div className="flex items-center justify-between mb-4">
          <div className="flex items-center gap-2">
            <h2 className="text-xl font-bold">{policy ? 'Edit Policy' : 'Add Policy'}</h2>
            <Badge variant="outline" className="text-xs">
              <Lock className="h-3 w-3 mr-1" />
              Admin Only
            </Badge>
          </div>
          <Button variant="ghost" size="icon" onClick={onClose} disabled={isLoading}>
            <X className="h-4 w-4" />
          </Button>
        </div>

        {/* RBAC Warning */}
        <div className="mb-4 p-3 bg-yellow-50 dark:bg-yellow-950 border border-yellow-200 dark:border-yellow-800 rounded-lg flex gap-2">
          <AlertTriangle className="h-4 w-4 text-yellow-500 flex-shrink-0 mt-0.5" />
          <p className="text-xs text-yellow-800 dark:text-yellow-200">
            Policy changes affect all agents in your organization. Only administrators should modify policies.
            Changes take effect within 5 minutes (cache refresh).
          </p>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4">
          {/* Policy Type Selection - Prominent at top */}
          <div>
            <label className="text-sm font-medium block mb-2">Policy Type *</label>
            <div className="grid grid-cols-3 gap-2">
              <button
                type="button"
                onClick={() => setPolicyType('builtin')}
                className={`p-3 rounded-lg border text-left transition-colors ${
                  policyType === 'builtin'
                    ? 'border-primary bg-primary/10'
                    : 'border-border hover:border-primary/50'
                }`}
              >
                <div className="flex items-center gap-2 mb-1">
                  <Wrench className="h-4 w-4" />
                  <span className="font-medium text-sm">Built-in</span>
                </div>
                <p className="text-xs text-muted-foreground">Pre-configured rules</p>
              </button>
              <button
                type="button"
                onClick={() => setPolicyType('rego')}
                className={`p-3 rounded-lg border text-left transition-colors ${
                  policyType === 'rego'
                    ? 'border-primary bg-primary/10'
                    : 'border-border hover:border-primary/50'
                }`}
              >
                <div className="flex items-center gap-2 mb-1">
                  <Code className="h-4 w-4" />
                  <span className="font-medium text-sm">Custom Rego</span>
                </div>
                <p className="text-xs text-muted-foreground">Write OPA rules</p>
              </button>
              <button
                type="button"
                onClick={() => setPolicyType('custom')}
                className={`p-3 rounded-lg border text-left transition-colors opacity-50 cursor-not-allowed`}
                disabled
              >
                <div className="flex items-center gap-2 mb-1">
                  <Settings className="h-4 w-4" />
                  <span className="font-medium text-sm">Webhook</span>
                </div>
                <p className="text-xs text-muted-foreground">Coming soon</p>
              </button>
            </div>
          </div>

          {/* Built-in Policy Selection */}
          {policyType === 'builtin' && (
            <div className="space-y-3 p-4 bg-muted/50 rounded-lg">
              <label className="text-sm font-medium block">Select Built-in Policy *</label>
              <div className="grid gap-2">
                {BUILTIN_POLICIES.map((bp) => (
                  <button
                    key={bp.name}
                    type="button"
                    onClick={() => handleBuiltinSelect(bp.name)}
                    className={`p-3 rounded-lg border text-left transition-colors ${
                      builtinName === bp.name
                        ? 'border-primary bg-primary/10'
                        : 'border-border hover:border-primary/50 bg-background'
                    }`}
                  >
                    <div className="flex items-center justify-between">
                      <span className="font-mono text-sm">{bp.name}</span>
                      <Badge variant="outline" className="text-xs">
                        {categoryLabels[bp.category] || bp.category}
                      </Badge>
                    </div>
                    <p className="text-xs text-muted-foreground mt-1">{bp.description}</p>
                  </button>
                ))}
              </div>
            </div>
          )}

          {/* Rego Code Editor */}
          {policyType === 'rego' && (
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <label className="text-sm font-medium">Rego Code *</label>
                <select
                  onChange={(e) => handleRegoTemplateSelect(e.target.value)}
                  className="text-xs rounded border bg-background px-2 py-1"
                  defaultValue=""
                >
                  <option value="" disabled>Load template...</option>
                  {REGO_TEMPLATES.map((t) => (
                    <option key={t.name} value={t.name}>{t.name}</option>
                  ))}
                </select>
              </div>
              <textarea
                value={regoCode}
                onChange={(e) => setRegoCode(e.target.value)}
                className="w-full rounded-md border bg-black text-green-400 px-3 py-2 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                rows={12}
                placeholder={`package policy

deny[msg] {
  # Your policy logic here
  input.action.type == "some_action"
  msg := "Action denied by custom policy"
}`}
                disabled={isLoading}
                spellCheck={false}
              />
              <p className="text-xs text-muted-foreground">
                Write Rego rules that return <code className="bg-muted px-1 rounded">deny[msg]</code> to block actions.
                Available input: <code className="bg-muted px-1 rounded">input.action</code>,
                <code className="bg-muted px-1 rounded">input.session</code>,
                <code className="bg-muted px-1 rounded">input.context</code>
              </p>
            </div>
          )}

          {/* Common Fields */}
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="text-sm font-medium block mb-1">Policy Name *</label>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                className="w-full rounded-md border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
                placeholder="my_custom_policy"
                required
                disabled={isLoading}
              />
            </div>
            <div>
              <label className="text-sm font-medium block mb-1">Category</label>
              <select
                value={category}
                onChange={(e) => setCategory(e.target.value)}
                className="w-full rounded-md border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
                disabled={isLoading}
              >
                <option value="security">Security</option>
                <option value="governance">Governance</option>
                <option value="time_based">Time-Based</option>
                <option value="financial">Financial</option>
                <option value="performance">Performance</option>
                <option value="custom">Custom</option>
              </select>
            </div>
          </div>

          <div>
            <label className="text-sm font-medium block mb-1">Description</label>
            <textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              className="w-full rounded-md border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
              rows={2}
              placeholder="Describe what this policy does and why"
              disabled={isLoading}
            />
          </div>

          {/* Configuration (for built-in policies) */}
          {policyType === 'builtin' && builtinName && (
            <div>
              <label className="text-sm font-medium block mb-1">Configuration (JSON)</label>
              <textarea
                value={configJson}
                onChange={(e) => setConfigJson(e.target.value)}
                className="w-full rounded-md border bg-background px-3 py-2 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                rows={4}
                disabled={isLoading}
                spellCheck={false}
              />
              <p className="text-xs text-muted-foreground mt-1">
                Customize the policy behavior. Invalid JSON will be ignored.
              </p>
            </div>
          )}

          <div className="grid grid-cols-3 gap-4">
            <div>
              <label className="text-sm font-medium block mb-1">Min Trust Level</label>
              <select
                value={minTrustLevel}
                onChange={(e) => setMinTrustLevel(Number(e.target.value))}
                className="w-full rounded-md border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
                disabled={isLoading}
              >
                <option value={1}>1 - Observer</option>
                <option value={2}>2 - Contributor</option>
                <option value={3}>3 - Developer</option>
                <option value={4}>4 - Maintainer</option>
                <option value={5}>5 - Admin</option>
              </select>
              <p className="text-xs text-muted-foreground mt-1">Policy applies to this level and above</p>
            </div>

            <div>
              <label className="text-sm font-medium block mb-1">Priority</label>
              <input
                type="number"
                min={1}
                max={1000}
                value={priority}
                onChange={(e) => setPriority(Number(e.target.value))}
                className="w-full rounded-md border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
                disabled={isLoading}
              />
              <p className="text-xs text-muted-foreground mt-1">Lower = evaluated first</p>
            </div>

            <div className="flex items-end pb-6">
              <label className="flex items-center gap-2 cursor-pointer">
                <input
                  type="checkbox"
                  checked={enabled}
                  onChange={(e) => setEnabled(e.target.checked)}
                  className="rounded border-gray-300"
                  disabled={isLoading}
                />
                <span className="text-sm font-medium">Enabled</span>
              </label>
            </div>
          </div>

          <div className="flex justify-end gap-2 pt-4 border-t">
            <Button type="button" variant="outline" onClick={onClose} disabled={isLoading}>
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={isLoading || !name.trim() || (policyType === 'builtin' && !builtinName) || (policyType === 'rego' && !regoCode.trim())}
            >
              {isLoading ? 'Saving...' : policy ? 'Update Policy' : 'Create Policy'}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
}

function PolicyCard({ policy, onEdit, onDelete, onToggle }: {
  policy: Policy;
  onEdit: (policy: Policy) => void;
  onDelete: (policy: Policy) => void;
  onToggle: (policy: Policy) => void;
}) {
  const Icon = categoryIcons[policy.category] || Settings;

  return (
    <Card className={policy.enabled ? '' : 'opacity-60'}>
      <CardHeader className="flex flex-row items-start justify-between space-y-0 pb-2">
        <div className="flex items-center gap-2">
          <div className="p-2 rounded-lg bg-muted">
            <Icon className="h-4 w-4" />
          </div>
          <div>
            <CardTitle className="text-base">{policy.name}</CardTitle>
            <span className={`text-xs px-2 py-0.5 rounded-full ${categoryColors[policy.category] || categoryColors.custom}`}>
              {categoryLabels[policy.category] || policy.category}
            </span>
          </div>
        </div>
        <div className="flex items-center gap-1">
          <Button
            variant="ghost"
            size="icon"
            onClick={() => onToggle(policy)}
            title={policy.enabled ? 'Disable' : 'Enable'}
          >
            <Power className={`h-4 w-4 ${policy.enabled ? 'text-green-500' : 'text-gray-400'}`} />
          </Button>
          <Button variant="ghost" size="icon" onClick={() => onEdit(policy)} title="Edit">
            <Settings className="h-4 w-4" />
          </Button>
          <Button variant="ghost" size="icon" onClick={() => onDelete(policy)} title="Delete">
            <Trash2 className="h-4 w-4 text-destructive" />
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        <p className="text-sm text-muted-foreground mb-4">{policy.description || 'No description'}</p>
        <div className="flex items-center justify-between text-xs text-muted-foreground">
          <span>Triggered: {policy.trigger_count || 0} times</span>
          <span>Last: {policy.last_triggered || 'Never'}</span>
        </div>
        <div className="flex items-center gap-2 mt-2">
          <Badge variant={policy.enabled ? 'success' : 'secondary'}>
            {policy.enabled ? 'Enabled' : 'Disabled'}
          </Badge>
          <Badge variant="outline" className="text-xs">
            Trust Level {policy.min_trust_level}+
          </Badge>
          <Badge variant="outline" className="text-xs">
            {policy.policy_type}
          </Badge>
        </div>
      </CardContent>
    </Card>
  );
}

export default function PoliciesPage() {
  const queryClient = useQueryClient();
  const [showDialog, setShowDialog] = useState(false);
  const [editingPolicy, setEditingPolicy] = useState<Policy | undefined>();
  const [deleteConfirm, setDeleteConfirm] = useState<Policy | null>(null);

  const { data, isLoading, error } = useQuery({
    queryKey: ['policies'],
    queryFn: () => api.getPolicies({ limit: 100 }),
  });

  const createMutation = useMutation({
    mutationFn: (policyData: Partial<Policy>) => api.createPolicy(policyData),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['policies'] });
      setShowDialog(false);
      setEditingPolicy(undefined);
    },
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: string; data: Partial<Policy> }) => api.updatePolicy(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['policies'] });
      setShowDialog(false);
      setEditingPolicy(undefined);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.deletePolicy(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['policies'] });
      setDeleteConfirm(null);
    },
  });

  const toggleMutation = useMutation({
    mutationFn: (id: string) => api.togglePolicyEnabled(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['policies'] });
    },
  });

  const handleEdit = (policy: Policy) => {
    setEditingPolicy(policy);
    setShowDialog(true);
  };

  const handleSubmit = (data: Partial<Policy>) => {
    if (editingPolicy) {
      updateMutation.mutate({ id: editingPolicy.id, data });
    } else {
      createMutation.mutate(data);
    }
  };

  const handleDelete = (policy: Policy) => {
    setDeleteConfirm(policy);
  };

  const confirmDelete = () => {
    if (deleteConfirm) {
      deleteMutation.mutate(deleteConfirm.id);
    }
  };

  const handleToggle = (policy: Policy) => {
    toggleMutation.mutate(policy.id);
  };

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
        Failed to load policies: {error instanceof Error ? error.message : 'Unknown error'}
      </div>
    );
  }

  const policies = data?.items || [];

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Policies</h1>
          <p className="text-muted-foreground">
            Manage governance policies for agent behavior
          </p>
        </div>
        <Button onClick={() => { setEditingPolicy(undefined); setShowDialog(true); }}>
          <Plus className="h-4 w-4 mr-2" />
          Add Policy
        </Button>
      </div>

      {policies.length > 0 ? (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {policies.map((policy) => (
            <PolicyCard
              key={policy.id}
              policy={policy}
              onEdit={handleEdit}
              onDelete={handleDelete}
              onToggle={handleToggle}
            />
          ))}
        </div>
      ) : (
        <Card>
          <CardContent className="p-8 text-center">
            <AlertTriangle className="h-12 w-12 mx-auto text-muted-foreground mb-4" />
            <p className="text-muted-foreground">No policies configured</p>
            <p className="text-sm text-muted-foreground mt-1">
              Add policies to govern agent behavior and enforce security rules.
            </p>
            <Button className="mt-4" onClick={() => setShowDialog(true)}>
              <Plus className="h-4 w-4 mr-2" />
              Add Your First Policy
            </Button>
          </CardContent>
        </Card>
      )}

      {/* Enforcement Models */}
      <Card>
        <CardHeader>
          <CardTitle>Enforcement Models</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="p-3 bg-blue-50 dark:bg-blue-950 border border-blue-200 dark:border-blue-800 rounded-lg flex gap-3">
            <Info className="h-5 w-5 text-blue-500 flex-shrink-0 mt-0.5" />
            <p className="text-sm text-blue-800 dark:text-blue-200">
              Policy enforcement is <strong>optional</strong> and depends on how agents are configured.
              Choose the enforcement model that matches your security requirements and agent capabilities.
            </p>
          </div>

          <div className="grid gap-4 md:grid-cols-3">
            {/* Advisory Model */}
            <div className="p-4 border rounded-lg space-y-3">
              <div className="flex items-center gap-2">
                <Shield className="h-5 w-5 text-blue-500" />
                <h4 className="font-semibold">Advisory (API)</h4>
              </div>
              <Badge variant="success" className="text-xs">
                <CheckCircle2 className="h-3 w-3 mr-1" />
                Active
              </Badge>
              <p className="text-sm text-muted-foreground">
                Agents voluntarily call the governance API before taking actions. Trust-based enforcement.
              </p>
              <div className="text-xs space-y-1 pt-2 border-t">
                <p className="font-medium">Endpoint:</p>
                <code className="block bg-muted px-2 py-1 rounded text-xs">POST /v1/governance/check</code>
              </div>
            </div>

            {/* MCP Model */}
            <div className="p-4 border rounded-lg space-y-3">
              <div className="flex items-center gap-2">
                <Server className="h-5 w-5 text-purple-500" />
                <h4 className="font-semibold">MCP Server</h4>
              </div>
              <Badge variant="success" className="text-xs">
                <CheckCircle2 className="h-3 w-3 mr-1" />
                Active
              </Badge>
              <p className="text-sm text-muted-foreground">
                Policy checks integrated into MCP tool calls. Requires MCP-compatible agents (Claude, etc.).
              </p>
              <div className="text-xs space-y-1 pt-2 border-t">
                <p className="font-medium">Tool:</p>
                <code className="block bg-muted px-2 py-1 rounded text-xs">adp_check_action</code>
              </div>
            </div>

            {/* Gateway Model */}
            <div className="p-4 border rounded-lg space-y-3 opacity-60">
              <div className="flex items-center gap-2">
                <Network className="h-5 w-5 text-gray-400" />
                <h4 className="font-semibold">Gateway (Proxy)</h4>
              </div>
              <Badge variant="secondary" className="text-xs">
                <Clock className="h-3 w-3 mr-1" />
                Planned
              </Badge>
              <p className="text-sm text-muted-foreground">
                All agent actions flow through ADP proxy. Zero-trust enforcement - no agent cooperation required.
              </p>
              <div className="text-xs space-y-1 pt-2 border-t">
                <p className="font-medium text-muted-foreground">Components:</p>
                <p className="text-muted-foreground">File proxy, Network proxy, Command sandbox</p>
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Policy Types */}
      <Card>
        <CardHeader>
          <CardTitle>Policy Types</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid gap-4 md:grid-cols-2">
            <div className="p-4 border rounded-lg">
              <h4 className="font-semibold mb-2">Built-in Policies</h4>
              <p className="text-sm text-muted-foreground mb-3">
                Pre-configured policy evaluators with sensible defaults. Configure via the dashboard.
              </p>
              <ul className="text-sm text-muted-foreground space-y-1">
                <li>• <strong>deny_sensitive_files</strong> - Block .env, .pem, .key files</li>
                <li>• <strong>blast_radius_limit</strong> - Limit files per action</li>
                <li>• <strong>off_hours_production</strong> - Block night deploys</li>
                <li>• <strong>cost_limit</strong> - Trust-based cost limits</li>
                <li>• <strong>require_migration_approval</strong> - DB change approval</li>
              </ul>
            </div>
            <div className="p-4 border rounded-lg">
              <h4 className="font-semibold mb-2">Custom Rego Policies</h4>
              <p className="text-sm text-muted-foreground mb-3">
                Write custom rules using Open Policy Agent&apos;s Rego language for full control.
              </p>
              <pre className="p-2 bg-muted rounded text-xs font-mono overflow-x-auto">
{`deny[msg] {
  input.action.type == "deploy"
  input.context.environment == "production"
  msg := "Production deploy blocked"
}`}
              </pre>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* How It Works */}
      <Card>
        <CardHeader>
          <CardTitle>How Enforcement Works</CardTitle>
        </CardHeader>
        <CardContent>
          <ol className="text-sm text-muted-foreground space-y-2 list-decimal list-inside">
            <li>Agent calls governance check (via API or MCP tool) with action details</li>
            <li>Engine loads all enabled policies from database (cached for 5 minutes)</li>
            <li>Each policy is evaluated based on trust level (1-5) and action context</li>
            <li>If any policy denies, response includes <code className="bg-muted px-1 rounded">requires_approval: true</code></li>
            <li>Agent can request human approval via <code className="bg-muted px-1 rounded">/v1/governance/approvals</code></li>
            <li>Approvers resolve requests through the dashboard or API</li>
          </ol>

          <div className="mt-4 p-3 bg-yellow-50 dark:bg-yellow-950 border border-yellow-200 dark:border-yellow-800 rounded-lg flex gap-3">
            <AlertTriangle className="h-5 w-5 text-yellow-500 flex-shrink-0 mt-0.5" />
            <p className="text-sm text-yellow-800 dark:text-yellow-200">
              <strong>Advisory mode requires agent cooperation.</strong> Agents can bypass policy checks if not properly configured.
              For mandatory enforcement, consider the Gateway model (coming soon) or use MCP with trusted agents.
            </p>
          </div>
        </CardContent>
      </Card>

      {showDialog && (
        <PolicyDialog
          policy={editingPolicy}
          onClose={() => { setShowDialog(false); setEditingPolicy(undefined); }}
          onSubmit={handleSubmit}
          isLoading={createMutation.isPending || updateMutation.isPending}
        />
      )}

      {deleteConfirm && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
          <div className="bg-card rounded-lg shadow-lg max-w-md w-full mx-4 p-6">
            <h2 className="text-xl font-bold mb-2">Delete Policy</h2>
            <p className="text-muted-foreground mb-4">
              Are you sure you want to delete &quot;{deleteConfirm.name}&quot;? This action cannot be undone.
            </p>
            <div className="flex justify-end gap-2">
              <Button variant="outline" onClick={() => setDeleteConfirm(null)} disabled={deleteMutation.isPending}>
                Cancel
              </Button>
              <Button variant="destructive" onClick={confirmDelete} disabled={deleteMutation.isPending}>
                {deleteMutation.isPending ? 'Deleting...' : 'Delete'}
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
