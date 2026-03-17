import { useState } from 'react';
import { useLoaderData, Form, useNavigation, useActionData } from 'react-router-dom';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Label } from '@/components/ui/label';
import { StatusBadge } from '@/components/ui/StatusBadge';
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/Alert';
import type { Account } from '@/types';
import * as api from '@/services/api';

const defaultForm = {
  email: '',
  password: '',
  imap_host: '',
  imap_port: '993',
  imap_encryption: 'ssl',
  smtp_host: '',
  smtp_port: '587',
  smtp_encryption: 'starttls',
};

export function AccountsView() {
  const accounts = useLoaderData() as Account[];
  const navigation = useNavigation();
  const actionData = useActionData() as { error?: string } | undefined;
  const [form, setForm] = useState(defaultForm);

  const submitting = navigation.state === 'submitting';

  const handleDelete = async (id: string) => {
    if (!confirm('Are you sure you want to delete this account?')) return;

    try {
      await api.deleteAccount(id);
      // Revalidation happens automatically if we used an action, but here we're using a direct API call.
      // Ideally we'd move this to an action too.
      window.location.reload();
    } catch (err) {
      console.error('Failed to delete account', err);
    }
  };

  const getStatusBadge = (status: string) => {
    switch (status) {
      case 'active':
        return <StatusBadge status="success" label="Active" />;
      case 'error':
        return <StatusBadge status="error" label="Error" />;
      case 'syncing':
        return <StatusBadge status="warning" label="Syncing" />;
      case 'disabled':
        return <StatusBadge status="error" label="Disabled" />;
      case 'auth_required':
        return <StatusBadge status="warning" label="Auth Required" />;
      case 'throttled':
        return <StatusBadge status="warning" label="Throttled" />;
      default:
        return <StatusBadge status="info" label="Inactive" />;
    }
  };

  // Get accounts needing attention
  const accountsNeedingAttention = accounts.filter(
    (acc) => ['disabled', 'auth_required', 'error', 'throttled'].includes(acc.status)
  );

  return (
    <div className="flex flex-col gap-6">
      {/* Accounts Needing Attention Alert */}
      {accountsNeedingAttention.length > 0 && (
        <Alert variant="warning" className="flex items-start gap-3">
          <svg className="h-4 w-4 mt-0.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
          </svg>
          <div>
            <AlertTitle>
              {accountsNeedingAttention.length} {accountsNeedingAttention.length === 1 ? 'account' : 'accounts'} need attention
            </AlertTitle>
            <AlertDescription>
              {accountsNeedingAttention.map((acc) => acc.email).join(', ')}
            </AlertDescription>
          </div>
        </Alert>
      )}

      <Card>
        <div className="flex items-center justify-between border-b px-6 py-4">
          <h3 className="text-lg font-semibold">Add Email Account</h3>
        </div>
        <div className="p-6">
          <Form method="post" className="max-w-[800px]">
            <div className="mb-6 pb-6 border-b">
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <Label htmlFor="email">Email Address</Label>
                  <Input
                    id="email"
                    type="email"
                    name="email"
                    required
                    placeholder="you@example.com"
                    value={form.email}
                    onChange={(e) => setForm({ ...form, email: e.target.value })}
                  />
                </div>
                <div>
                  <Label htmlFor="password">Password</Label>
                  <Input
                    id="password"
                    type="password"
                    name="password"
                    required
                    placeholder="••••••••"
                    value={form.password}
                    onChange={(e) => setForm({ ...form, password: e.target.value })}
                  />
                </div>
              </div>
            </div>

            <div className="mb-6 pb-6 border-b">
              <h3 className="mb-4 text-sm font-semibold text-muted-foreground">IMAP Settings</h3>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <Label htmlFor="imap_host">IMAP Host</Label>
                  <Input
                    id="imap_host"
                    type="text"
                    name="imap_host"
                    placeholder="imap.example.com"
                    value={form.imap_host}
                    onChange={(e) => setForm({ ...form, imap_host: e.target.value })}
                  />
                </div>
                <div>
                  <Label htmlFor="imap_port">IMAP Port</Label>
                  <Input
                    id="imap_port"
                    type="number"
                    name="imap_port"
                    value={form.imap_port}
                    onChange={(e) => setForm({ ...form, imap_port: e.target.value })}
                  />
                </div>
              </div>
              <div className="grid grid-cols-2 gap-4 mt-4">
                <div>
                  <Label htmlFor="imap_encryption">Encryption</Label>
                  <select
                    id="imap_encryption"
                    name="imap_encryption"
                    className="flex h-8 w-full min-w-0 rounded-lg border border-input bg-transparent px-2.5 py-1 text-base transition-colors outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 md:text-sm"
                    value={form.imap_encryption}
                    onChange={(e) => setForm({ ...form, imap_encryption: e.target.value })}
                  >
                    <option value="ssl">SSL/TLS</option>
                    <option value="starttls">STARTTLS</option>
                    <option value="tls">TLS</option>
                    <option value="none">None</option>
                  </select>
                </div>
              </div>
            </div>

            <div className="mb-6">
              <h3 className="mb-4 text-sm font-semibold text-muted-foreground">SMTP Settings</h3>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <Label htmlFor="smtp_host">SMTP Host</Label>
                  <Input
                    id="smtp_host"
                    type="text"
                    name="smtp_host"
                    placeholder="smtp.example.com"
                    value={form.smtp_host}
                    onChange={(e) => setForm({ ...form, smtp_host: e.target.value })}
                  />
                </div>
                <div>
                  <Label htmlFor="smtp_port">SMTP Port</Label>
                  <Input
                    id="smtp_port"
                    type="number"
                    name="smtp_port"
                    value={form.smtp_port}
                    onChange={(e) => setForm({ ...form, smtp_port: e.target.value })}
                  />
                </div>
              </div>
              <div className="grid grid-cols-2 gap-4 mt-4">
                <div>
                  <Label htmlFor="smtp_encryption">Encryption</Label>
                  <select
                    id="smtp_encryption"
                    name="smtp_encryption"
                    className="flex h-8 w-full min-w-0 rounded-lg border border-input bg-transparent px-2.5 py-1 text-base transition-colors outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 md:text-sm"
                    value={form.smtp_encryption}
                    onChange={(e) => setForm({ ...form, smtp_encryption: e.target.value })}
                  >
                    <option value="starttls">STARTTLS</option>
                    <option value="ssl">SSL/TLS</option>
                    <option value="tls">TLS</option>
                    <option value="none">None</option>
                  </select>
                </div>
              </div>
            </div>

            {actionData?.error && (
              <div className="mb-4 rounded-lg border border-destructive bg-destructive/10 px-4 py-3 text-destructive">
                {actionData.error}
              </div>
            )}

            <Button type="submit" variant="default" disabled={submitting}>
              {submitting ? 'Adding...' : 'Add Account'}
            </Button>
          </Form>
        </div>
      </Card>

      <Card>
        <div className="flex items-center justify-between border-b px-6 py-4">
          <h3 className="text-lg font-semibold">Your Accounts</h3>
          <Button variant="outline" size="sm" onClick={() => window.location.reload()}>
            Refresh
          </Button>
        </div>
        <div className="p-6">
          {navigation.state === 'loading' ? (
            <div className="py-12 text-center text-muted-foreground">Loading accounts...</div>
          ) : accounts.length === 0 ? (
            <div className="py-12 text-center text-muted-foreground">
              <div className="mb-4 text-5xl opacity-30">📭</div>
              <p>No accounts yet. Add your first email account above.</p>
            </div>
          ) : (
            <div className="flex flex-col gap-3">
              {accounts.map((account) => (
                <div
                  key={account.id}
                  className="flex items-center justify-between rounded-lg border bg-muted/50 px-4 py-4"
                >
                  <div className="flex-1">
                    <div className="font-semibold">{account.email}</div>
                    <div className="text-sm text-muted-foreground">
                      IMAP: {account.imap_config.host}:{account.imap_config.port}
                    </div>
                  </div>
                  <div className="mx-4">{getStatusBadge(account.status)}</div>
                  <div className="flex gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => window.location.href = `/accounts/${account.id}`}
                    >
                      View Details
                    </Button>
                    <Button
                      variant="destructive"
                      size="sm"
                      onClick={() => handleDelete(account.id)}
                    >
                      Delete
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </Card>
    </div>
  );
}
