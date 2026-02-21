'use client';

import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api, Service } from '@/lib/api';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { formatRelativeTime } from '@/lib/utils';
import { ExternalLink, Settings, Plus, X } from 'lucide-react';
import Link from 'next/link';

interface AddServiceDialogProps {
  onClose: () => void;
  onSubmit: (data: Partial<Service>) => void;
  isLoading: boolean;
}

function AddServiceDialog({ onClose, onSubmit, isLoading }: AddServiceDialogProps) {
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [repositoryUrl, setRepositoryUrl] = useState('');
  const [ownerTeam, setOwnerTeam] = useState('');

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;

    onSubmit({
      name: name.trim(),
      description: description.trim() || undefined,
      repository_url: repositoryUrl.trim() || undefined,
      owner_team: ownerTeam.trim() || undefined,
    });
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div className="bg-card rounded-lg shadow-lg max-w-md w-full mx-4 p-6">
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-xl font-bold">Add Service</h2>
          <Button variant="ghost" size="icon" onClick={onClose} disabled={isLoading}>
            <X className="h-4 w-4" />
          </Button>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="text-sm font-medium block mb-1">Name *</label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="w-full rounded-md border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
              placeholder="my-service"
              required
              disabled={isLoading}
            />
          </div>

          <div>
            <label className="text-sm font-medium block mb-1">Description</label>
            <textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              className="w-full rounded-md border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
              rows={2}
              placeholder="A brief description of the service"
              disabled={isLoading}
            />
          </div>

          <div>
            <label className="text-sm font-medium block mb-1">Repository URL</label>
            <input
              type="url"
              value={repositoryUrl}
              onChange={(e) => setRepositoryUrl(e.target.value)}
              className="w-full rounded-md border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
              placeholder="https://github.com/org/repo"
              disabled={isLoading}
            />
          </div>

          <div>
            <label className="text-sm font-medium block mb-1">Owner Team</label>
            <input
              type="text"
              value={ownerTeam}
              onChange={(e) => setOwnerTeam(e.target.value)}
              className="w-full rounded-md border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
              placeholder="platform-team"
              disabled={isLoading}
            />
          </div>

          <div className="flex justify-end gap-2 pt-4">
            <Button type="button" variant="outline" onClick={onClose} disabled={isLoading}>
              Cancel
            </Button>
            <Button type="submit" disabled={isLoading || !name.trim()}>
              {isLoading ? 'Creating...' : 'Create Service'}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
}

function ServiceCard({ service }: { service: Service }) {
  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between space-y-0">
        <div>
          <CardTitle className="text-lg">{service.name}</CardTitle>
          {service.description && (
            <p className="text-sm text-muted-foreground mt-1">{service.description}</p>
          )}
        </div>
        <Link href={`/services/${service.id}`}>
          <Button variant="ghost" size="icon">
            <Settings className="h-4 w-4" />
          </Button>
        </Link>
      </CardHeader>
      <CardContent>
        <div className="space-y-3">
          {/* Owner */}
          <div className="flex items-center justify-between text-sm">
            <span className="text-muted-foreground">Owner</span>
            <span>{service.owner_team || service.owner_user || '-'}</span>
          </div>

          {/* Repository */}
          {service.repository_url && (
            <div className="flex items-center justify-between text-sm">
              <span className="text-muted-foreground">Repository</span>
              <a
                href={service.repository_url}
                target="_blank"
                rel="noopener noreferrer"
                className="flex items-center gap-1 text-primary hover:underline"
              >
                <span className="max-w-32 truncate">{service.repository_url.split('/').slice(-2).join('/')}</span>
                <ExternalLink className="h-3 w-3" />
              </a>
            </div>
          )}

          {/* Context Config */}
          {service.context_config && (
            <div className="flex flex-wrap gap-1 pt-2 border-t">
              {service.context_config.token_budget && (
                <Badge variant="outline" className="text-xs">
                  Token Budget: {
                    (service.context_config.token_budget.essential || 0) +
                    (service.context_config.token_budget.task_relevant || 0) +
                    (service.context_config.token_budget.supporting || 0)
                  }
                </Badge>
              )}
              {service.context_config.essential_paths && service.context_config.essential_paths.length > 0 && (
                <Badge variant="outline" className="text-xs">
                  {service.context_config.essential_paths.length} essential paths
                </Badge>
              )}
            </div>
          )}

          {/* Timestamps */}
          <div className="text-xs text-muted-foreground pt-2 border-t">
            Created {formatRelativeTime(service.created_at)}
            {service.updated_at && ` • Updated ${formatRelativeTime(service.updated_at)}`}
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

export default function ServicesPage() {
  const queryClient = useQueryClient();
  const [showAddDialog, setShowAddDialog] = useState(false);

  const { data, isLoading, error } = useQuery({
    queryKey: ['services'],
    queryFn: () => api.getServices({ limit: 50 }),
  });

  const createMutation = useMutation({
    mutationFn: (serviceData: Partial<Service>) => api.createService(serviceData),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['services'] });
      setShowAddDialog(false);
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
        Failed to load services: {error instanceof Error ? error.message : 'Unknown error'}
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Services</h1>
          <p className="text-muted-foreground">
            Manage service configurations for agent governance
          </p>
        </div>
        <Button onClick={() => setShowAddDialog(true)}>
          <Plus className="h-4 w-4 mr-2" />
          Add Service
        </Button>
      </div>

      {data?.items && data.items.length > 0 ? (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {data.items.map((service) => (
            <ServiceCard key={service.id} service={service} />
          ))}
        </div>
      ) : (
        <Card>
          <CardContent className="p-8 text-center">
            <p className="text-muted-foreground">No services configured</p>
            <p className="text-sm text-muted-foreground mt-1">
              Add a service to start tracking agent activity.
            </p>
            <Button className="mt-4" onClick={() => setShowAddDialog(true)}>
              <Plus className="h-4 w-4 mr-2" />
              Add Your First Service
            </Button>
          </CardContent>
        </Card>
      )}

      {showAddDialog && (
        <AddServiceDialog
          onClose={() => setShowAddDialog(false)}
          onSubmit={(data) => createMutation.mutate(data)}
          isLoading={createMutation.isPending}
        />
      )}
    </div>
  );
}
