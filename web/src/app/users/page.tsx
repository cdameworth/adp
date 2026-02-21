'use client';

import { useState } from 'react';
import { useSession } from 'next-auth/react';
import { useRouter } from 'next/navigation';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { api, UserProfile } from '@/lib/api';
import { Plus, Pencil, UserX, X, CheckCircle2, AlertCircle } from 'lucide-react';

type DialogMode = 'create' | 'edit' | null;

export default function UsersPage() {
  const { data: session, status } = useSession();
  const router = useRouter();
  const queryClient = useQueryClient();

  const [dialogMode, setDialogMode] = useState<DialogMode>(null);
  const [editUser, setEditUser] = useState<UserProfile | null>(null);
  const [formEmail, setFormEmail] = useState('');
  const [formName, setFormName] = useState('');
  const [formPassword, setFormPassword] = useState('');
  const [formRole, setFormRole] = useState('user');
  const [formStatus, setFormStatus] = useState('active');
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null);
  const [confirmDisable, setConfirmDisable] = useState<string | null>(null);

  const isAdmin = session?.user?.role === 'admin';

  // Redirect non-admins
  if (status === 'authenticated' && !isAdmin) {
    router.push('/');
    return null;
  }

  const { data, isLoading } = useQuery({
    queryKey: ['admin-users'],
    queryFn: () => api.getUsers({ limit: 100 }),
    enabled: isAdmin,
  });

  const createMutation = useMutation({
    mutationFn: (data: { email: string; name: string; password: string; role: string }) =>
      api.createUser(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin-users'] });
      setDialogMode(null);
      resetForm();
      setMessage({ type: 'success', text: 'User created successfully' });
    },
    onError: (err: any) => {
      const msg = err?.message?.includes('already exists')
        ? 'A user with this email already exists'
        : 'Failed to create user';
      setMessage({ type: 'error', text: msg });
    },
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: string; data: { name?: string; role?: string; status?: string } }) =>
      api.updateUser(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin-users'] });
      setDialogMode(null);
      setEditUser(null);
      resetForm();
      setMessage({ type: 'success', text: 'User updated successfully' });
    },
    onError: () => {
      setMessage({ type: 'error', text: 'Failed to update user' });
    },
  });

  const disableMutation = useMutation({
    mutationFn: (id: string) => api.disableUser(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin-users'] });
      setConfirmDisable(null);
      setMessage({ type: 'success', text: 'User disabled successfully' });
    },
    onError: () => {
      setMessage({ type: 'error', text: 'Failed to disable user' });
    },
  });

  function resetForm() {
    setFormEmail('');
    setFormName('');
    setFormPassword('');
    setFormRole('user');
    setFormStatus('active');
  }

  function openCreate() {
    resetForm();
    setMessage(null);
    setDialogMode('create');
  }

  function openEdit(user: UserProfile) {
    setEditUser(user);
    setFormName(user.name);
    setFormRole(user.role);
    setFormStatus(user.status);
    setMessage(null);
    setDialogMode('edit');
  }

  function handleSubmitCreate(e: React.FormEvent) {
    e.preventDefault();
    createMutation.mutate({ email: formEmail, name: formName, password: formPassword, role: formRole });
  }

  function handleSubmitEdit(e: React.FormEvent) {
    e.preventDefault();
    if (!editUser) return;
    updateMutation.mutate({
      id: editUser.id,
      data: { name: formName, role: formRole, status: formStatus },
    });
  }

  const users = data?.items ?? [];

  if (status === 'loading' || isLoading) {
    return (
      <div className="space-y-6">
        <div>
          <h1 className="text-3xl font-bold">User Management</h1>
          <p className="text-muted-foreground">Loading...</p>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">User Management</h1>
          <p className="text-muted-foreground">
            Manage user accounts and roles
          </p>
        </div>
        <Button onClick={openCreate}>
          <Plus className="h-4 w-4 mr-2" />
          Add User
        </Button>
      </div>

      {message && (
        <div className={`flex items-center gap-2 px-4 py-3 rounded-lg text-sm ${
          message.type === 'success'
            ? 'bg-green-100 dark:bg-green-900 text-green-800 dark:text-green-200'
            : 'bg-destructive/10 text-destructive'
        }`}>
          {message.type === 'success' ? <CheckCircle2 className="h-4 w-4" /> : <AlertCircle className="h-4 w-4" />}
          {message.text}
        </div>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Users ({data?.total ?? 0})</CardTitle>
          <CardDescription>All registered user accounts</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b text-left">
                  <th className="pb-3 font-medium">Name</th>
                  <th className="pb-3 font-medium">Email</th>
                  <th className="pb-3 font-medium">Role</th>
                  <th className="pb-3 font-medium">Status</th>
                  <th className="pb-3 font-medium">Created</th>
                  <th className="pb-3 font-medium text-right">Actions</th>
                </tr>
              </thead>
              <tbody>
                {users.map((u) => (
                  <tr key={u.id} className="border-b last:border-0">
                    <td className="py-3 font-medium">{u.name || '—'}</td>
                    <td className="py-3 text-muted-foreground">{u.email}</td>
                    <td className="py-3">
                      <Badge variant={u.role === 'admin' ? 'default' : 'secondary'}>
                        {u.role}
                      </Badge>
                    </td>
                    <td className="py-3">
                      <Badge variant={u.status === 'active' ? 'success' : 'destructive'}>
                        {u.status}
                      </Badge>
                    </td>
                    <td className="py-3 text-muted-foreground">
                      {new Date(u.created_at).toLocaleDateString()}
                    </td>
                    <td className="py-3 text-right">
                      <div className="flex items-center justify-end gap-1">
                        <Button variant="ghost" size="icon" onClick={() => openEdit(u)} title="Edit user">
                          <Pencil className="h-4 w-4" />
                        </Button>
                        {u.id !== session?.user?.id && u.status === 'active' && (
                          <Button
                            variant="ghost"
                            size="icon"
                            onClick={() => setConfirmDisable(u.id)}
                            title="Disable user"
                          >
                            <UserX className="h-4 w-4 text-destructive" />
                          </Button>
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
                {users.length === 0 && (
                  <tr>
                    <td colSpan={6} className="py-8 text-center text-muted-foreground">
                      No users found
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>

      {/* Create / Edit Dialog */}
      {dialogMode && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
          <div className="bg-card rounded-lg shadow-lg max-w-md w-full mx-4 p-6">
            <div className="flex items-center justify-between mb-4">
              <h2 className="text-xl font-bold">
                {dialogMode === 'create' ? 'Add User' : 'Edit User'}
              </h2>
              <Button variant="ghost" size="icon" onClick={() => { setDialogMode(null); setEditUser(null); }}>
                <X className="h-4 w-4" />
              </Button>
            </div>

            <form onSubmit={dialogMode === 'create' ? handleSubmitCreate : handleSubmitEdit} className="space-y-4">
              {dialogMode === 'create' && (
                <>
                  <div>
                    <label className="text-sm font-medium block mb-1">Email</label>
                    <input
                      type="email"
                      value={formEmail}
                      onChange={(e) => setFormEmail(e.target.value)}
                      required
                      className="w-full rounded-md border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
                    />
                  </div>
                  <div>
                    <label className="text-sm font-medium block mb-1">Password</label>
                    <input
                      type="password"
                      value={formPassword}
                      onChange={(e) => setFormPassword(e.target.value)}
                      required
                      minLength={8}
                      className="w-full rounded-md border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
                    />
                    <p className="text-xs text-muted-foreground mt-1">Minimum 8 characters</p>
                  </div>
                </>
              )}

              <div>
                <label className="text-sm font-medium block mb-1">Name</label>
                <input
                  type="text"
                  value={formName}
                  onChange={(e) => setFormName(e.target.value)}
                  className="w-full rounded-md border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </div>

              <div>
                <label className="text-sm font-medium block mb-1">Role</label>
                <select
                  value={formRole}
                  onChange={(e) => setFormRole(e.target.value)}
                  className="w-full rounded-md border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
                >
                  <option value="user">User</option>
                  <option value="admin">Admin</option>
                </select>
              </div>

              {dialogMode === 'edit' && (
                <div>
                  <label className="text-sm font-medium block mb-1">Status</label>
                  <select
                    value={formStatus}
                    onChange={(e) => setFormStatus(e.target.value)}
                    className="w-full rounded-md border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
                  >
                    <option value="active">Active</option>
                    <option value="disabled">Disabled</option>
                  </select>
                </div>
              )}

              <div className="flex justify-end gap-2 pt-2">
                <Button type="button" variant="outline" onClick={() => { setDialogMode(null); setEditUser(null); }}>
                  Cancel
                </Button>
                <Button type="submit" disabled={createMutation.isPending || updateMutation.isPending}>
                  {(createMutation.isPending || updateMutation.isPending) ? 'Saving...' : 'Save'}
                </Button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Confirm Disable Dialog */}
      {confirmDisable && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
          <div className="bg-card rounded-lg shadow-lg max-w-sm w-full mx-4 p-6">
            <h2 className="text-lg font-bold mb-2">Disable User</h2>
            <p className="text-sm text-muted-foreground mb-4">
              Are you sure you want to disable this user? They will no longer be able to log in.
            </p>
            <div className="flex justify-end gap-2">
              <Button variant="outline" onClick={() => setConfirmDisable(null)}>Cancel</Button>
              <Button
                variant="destructive"
                onClick={() => disableMutation.mutate(confirmDisable)}
                disabled={disableMutation.isPending}
              >
                {disableMutation.isPending ? 'Disabling...' : 'Disable'}
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
