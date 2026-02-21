'use client';

import { useState } from 'react';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Shield, Server, Network, CheckCircle2, AlertCircle, Clock, X, ExternalLink, Copy } from 'lucide-react';

type DialogType = 'auth' | 'github' | 'backstage' | 'slack' | null;

function ConfigDialog({ type, onClose }: { type: DialogType; onClose: () => void }) {
  if (!type) return null;

  const configs: Record<string, { title: string; description: string; fields?: { label: string; placeholder: string; type?: string }[]; docUrl?: string }> = {
    auth: {
      title: 'Configure Authentication',
      description: 'Set up OIDC or SAML authentication for your organization. This requires configuring your identity provider and updating the server configuration.',
      fields: [
        { label: 'Provider Type', placeholder: 'OIDC or SAML' },
        { label: 'Issuer URL', placeholder: 'https://your-idp.com' },
        { label: 'Client ID', placeholder: 'your-client-id' },
        { label: 'Client Secret', placeholder: '••••••••', type: 'password' },
      ],
      docUrl: 'https://docs.adp.dev/auth',
    },
    github: {
      title: 'Configure GitHub App',
      description: 'Connect ADP to GitHub for commit verification and status checks. Create a GitHub App and configure the credentials below.',
      fields: [
        { label: 'App ID', placeholder: '123456' },
        { label: 'Private Key', placeholder: 'Paste private key...', type: 'password' },
        { label: 'Webhook Secret', placeholder: '••••••••', type: 'password' },
        { label: 'Installation ID', placeholder: '12345678' },
      ],
      docUrl: 'https://docs.adp.dev/integrations/github',
    },
    backstage: {
      title: 'Configure Backstage',
      description: 'Sync your service catalog from Backstage. ADP will import entities and keep them in sync.',
      fields: [
        { label: 'Backstage URL', placeholder: 'https://backstage.your-company.com' },
        { label: 'API Token', placeholder: '••••••••', type: 'password' },
        { label: 'Sync Interval (minutes)', placeholder: '15' },
      ],
      docUrl: 'https://docs.adp.dev/integrations/backstage',
    },
    slack: {
      title: 'Configure Slack',
      description: 'Send escalation notifications to Slack channels when agents require human approval.',
      fields: [
        { label: 'Webhook URL', placeholder: 'https://hooks.slack.com/services/...' },
        { label: 'Default Channel', placeholder: '#agent-approvals' },
        { label: 'Bot Token', placeholder: 'xoxb-...', type: 'password' },
      ],
      docUrl: 'https://docs.adp.dev/integrations/slack',
    },
  };

  const config = configs[type];

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div className="bg-card rounded-lg shadow-lg max-w-lg w-full mx-4 p-6 max-h-[90vh] overflow-y-auto">
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-xl font-bold">{config.title}</h2>
          <Button variant="ghost" size="icon" onClick={onClose}>
            <X className="h-4 w-4" />
          </Button>
        </div>

        <p className="text-muted-foreground mb-4">{config.description}</p>

        <div className="space-y-4">
          {config.fields?.map((field, i) => (
            <div key={i}>
              <label className="text-sm font-medium block mb-1">{field.label}</label>
              <input
                type={field.type || 'text'}
                placeholder={field.placeholder}
                className="w-full rounded-md border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
              />
            </div>
          ))}
        </div>

        <div className="flex items-center justify-between mt-6 pt-4 border-t">
          {config.docUrl && (
            <a
              href={config.docUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="text-sm text-muted-foreground hover:text-foreground flex items-center gap-1"
            >
              <ExternalLink className="h-3 w-3" />
              Documentation
            </a>
          )}
          <div className="flex gap-2">
            <Button variant="outline" onClick={onClose}>Cancel</Button>
            <Button onClick={() => {
              alert('Settings saved! (In a production environment, this would persist to the backend.)');
              onClose();
            }}>
              Save Configuration
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}

export default function SettingsPage() {
  const [dialogType, setDialogType] = useState<DialogType>(null);
  const [orgName, setOrgName] = useState('Acme Corp');
  const [trustLevel, setTrustLevel] = useState('3');
  const [saveMessage, setSaveMessage] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  const handleSaveOrg = () => {
    setSaveMessage('Organization settings saved successfully!');
    setTimeout(() => setSaveMessage(null), 3000);
  };

  const copyMcpConfig = () => {
    const config = JSON.stringify({
      mcpServers: {
        adp: {
          command: "/usr/local/bin/adp-mcp",
          args: [],
          env: {
            ADP_DATABASE_POSTGRES_HOST: "localhost",
            ADP_DATABASE_POSTGRES_PORT: "5432",
            ADP_DATABASE_POSTGRES_DATABASE: "adp",
            ADP_DATABASE_POSTGRES_USERNAME: "adp",
            ADP_DATABASE_POSTGRES_PASSWORD: "your-password"
          }
        }
      }
    }, null, 2);
    navigator.clipboard.writeText(config);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">Settings</h1>
        <p className="text-muted-foreground">
          Configure your ADP instance
        </p>
      </div>

      {saveMessage && (
        <div className="bg-green-100 dark:bg-green-900 text-green-800 dark:text-green-200 px-4 py-3 rounded-lg flex items-center gap-2">
          <CheckCircle2 className="h-4 w-4" />
          {saveMessage}
        </div>
      )}

      <div className="grid gap-6">
        {/* Enforcement Models */}
        <Card>
          <CardHeader>
            <CardTitle>Policy Enforcement Models</CardTitle>
            <CardDescription>
              Choose how policies are enforced for AI agents
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
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
                  Agents call governance API before actions. Trust-based - agents voluntarily comply.
                </p>
                <div className="text-xs space-y-1">
                  <div className="flex items-center gap-1 text-green-600">
                    <CheckCircle2 className="h-3 w-3" /> Policy engine connected
                  </div>
                  <div className="flex items-center gap-1 text-green-600">
                    <CheckCircle2 className="h-3 w-3" /> Database policies loaded
                  </div>
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
                  Policy checks integrated into MCP tool calls. Requires MCP-compatible agents.
                </p>
                <div className="text-xs space-y-1">
                  <div className="flex items-center gap-1 text-green-600">
                    <CheckCircle2 className="h-3 w-3" /> MCP server available
                  </div>
                  <div className="flex items-center gap-1 text-green-600">
                    <CheckCircle2 className="h-3 w-3" /> Policy engine connected
                  </div>
                </div>
                <Button
                  variant="outline"
                  size="sm"
                  className="w-full mt-2"
                  onClick={copyMcpConfig}
                >
                  <Copy className="h-3 w-3 mr-1" />
                  {copied ? 'Copied!' : 'Copy MCP Config'}
                </Button>
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
                  All agent actions flow through ADP proxy. Zero-trust enforcement.
                </p>
                <div className="text-xs space-y-1">
                  <div className="flex items-center gap-1 text-gray-400">
                    <Clock className="h-3 w-3" /> File proxy not implemented
                  </div>
                  <div className="flex items-center gap-1 text-gray-400">
                    <Clock className="h-3 w-3" /> Network proxy not implemented
                  </div>
                </div>
              </div>
            </div>

            <div className="p-3 bg-muted/50 rounded-lg">
              <p className="text-sm text-muted-foreground">
                <strong>Current Setup:</strong> Advisory mode and MCP Server are active. Agents should call
                <code className="mx-1 px-1 bg-muted rounded">POST /v1/governance/check</code>
                before taking actions. For MCP-compatible agents, configure the
                <code className="mx-1 px-1 bg-muted rounded">adp-mcp</code> server.
              </p>
            </div>
          </CardContent>
        </Card>

        {/* Authentication */}
        <Card>
          <CardHeader>
            <CardTitle>Authentication</CardTitle>
            <CardDescription>
              Configure identity providers for dashboard access
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="font-medium">OIDC / OAuth 2.0</p>
                <p className="text-sm text-muted-foreground">
                  Connect with your identity provider (Okta, Auth0, etc.)
                </p>
              </div>
              <Badge variant="outline">Not Configured</Badge>
            </div>
            <div className="flex items-center justify-between">
              <div>
                <p className="font-medium">SAML 2.0</p>
                <p className="text-sm text-muted-foreground">
                  Enterprise SSO via SAML
                </p>
              </div>
              <Badge variant="outline">Not Configured</Badge>
            </div>
            <Button variant="outline" onClick={() => setDialogType('auth')}>
              Configure Authentication
            </Button>
          </CardContent>
        </Card>

        {/* Database Connections */}
        <Card>
          <CardHeader>
            <CardTitle>Database Connections</CardTitle>
            <CardDescription>
              Status of backend database connections
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="font-medium">PostgreSQL</p>
                <p className="text-sm text-muted-foreground">
                  Session and decision storage
                </p>
              </div>
              <Badge variant="success">Connected</Badge>
            </div>
            <div className="flex items-center justify-between">
              <div>
                <p className="font-medium">Neo4j</p>
                <p className="text-sm text-muted-foreground">
                  Decision lineage graph
                </p>
              </div>
              <Badge variant="success">Connected</Badge>
            </div>
            <div className="flex items-center justify-between">
              <div>
                <p className="font-medium">Qdrant</p>
                <p className="text-sm text-muted-foreground">
                  Vector search for context retrieval
                </p>
              </div>
              <Badge variant="success">Connected</Badge>
            </div>
            <div className="flex items-center justify-between">
              <div>
                <p className="font-medium">ClickHouse</p>
                <p className="text-sm text-muted-foreground">
                  Time-series analytics
                </p>
              </div>
              <Badge variant="success">Connected</Badge>
            </div>
            <div className="flex items-center justify-between">
              <div>
                <p className="font-medium">Redis</p>
                <p className="text-sm text-muted-foreground">
                  Caching and rate limiting
                </p>
              </div>
              <Badge variant="success">Connected</Badge>
            </div>
          </CardContent>
        </Card>

        {/* Integrations */}
        <Card>
          <CardHeader>
            <CardTitle>Integrations</CardTitle>
            <CardDescription>
              Connect ADP with your development tools
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="font-medium">GitHub App</p>
                <p className="text-sm text-muted-foreground">
                  Commit verification and check runs
                </p>
              </div>
              <Button variant="outline" size="sm" onClick={() => setDialogType('github')}>
                Configure
              </Button>
            </div>
            <div className="flex items-center justify-between">
              <div>
                <p className="font-medium">Backstage</p>
                <p className="text-sm text-muted-foreground">
                  Catalog sync and dashboard plugin
                </p>
              </div>
              <Button variant="outline" size="sm" onClick={() => setDialogType('backstage')}>
                Configure
              </Button>
            </div>
            <div className="flex items-center justify-between">
              <div>
                <p className="font-medium">Slack</p>
                <p className="text-sm text-muted-foreground">
                  Escalation notifications
                </p>
              </div>
              <Button variant="outline" size="sm" onClick={() => setDialogType('slack')}>
                Configure
              </Button>
            </div>
          </CardContent>
        </Card>

        {/* Organization */}
        <Card>
          <CardHeader>
            <CardTitle>Organization</CardTitle>
            <CardDescription>
              General organization settings
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid gap-4 md:grid-cols-2">
              <div>
                <label className="text-sm font-medium">Organization Name</label>
                <input
                  type="text"
                  value={orgName}
                  onChange={(e) => setOrgName(e.target.value)}
                  className="mt-1 w-full rounded-md border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </div>
              <div>
                <label className="text-sm font-medium">Default Trust Level</label>
                <select
                  value={trustLevel}
                  onChange={(e) => setTrustLevel(e.target.value)}
                  className="mt-1 w-full rounded-md border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
                >
                  <option value="1">1 - Observer</option>
                  <option value="2">2 - Contributor</option>
                  <option value="3">3 - Developer</option>
                  <option value="4">4 - Maintainer</option>
                  <option value="5">5 - Admin</option>
                </select>
              </div>
            </div>
            <Button onClick={handleSaveOrg}>Save Changes</Button>
          </CardContent>
        </Card>
      </div>

      <ConfigDialog type={dialogType} onClose={() => setDialogType(null)} />
    </div>
  );
}
