import { useState, useCallback, useEffect } from 'react';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Label } from '@/components/ui/label';
import { StatusBadge } from '@/components/ui/StatusBadge';
import type { Account } from '@/types';
import * as api from '@/services/api';

interface AccountForm {
  email: string;
  password: string;
  imap_host: string;
  imap_port: string;
  imap_encryption: string;
  smtp_host: string;
  smtp_port: string;
  smtp_encryption: string;
}

interface AccountsViewProps {
  onAccountAdded?: () => void;
}

const defaultForm: AccountForm = {
  email: '',
  password: '',
  imap_host: '',
  imap_port: '993',
  imap_encryption: 'ssl',
  smtp_host: '',
  smtp_port: '587',
  smtp_encryption: 'starttls',
};

export function AccountsView({ onAccountAdded }: AccountsViewProps) {
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [loading, setLoading] = useState(false);
  const [form, setForm] = useState<AccountForm>(defaultForm);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const loadAccounts = useCallback(async () => {
    try {
      setLoading(true);
      const data = await api.listAccounts();
      setAccounts(data);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load accounts');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadAccounts();
  }, [loadAccounts]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    setError(null);

    try {
      await api.createAccount({
        email: form.email,
        password: form.password,
        imap_host: form.imap_host || undefined,
        imap_port: form.imap_port ? parseInt(form.imap_port, 10) : undefined,
        imap_encryption: form.imap_encryption || undefined,
        smtp_host: form.smtp_host || undefined,
        smtp_port: form.smtp_port ? parseInt(form.smtp_port, 10) : undefined,
        smtp_encryption: form.smtp_encryption || undefined,
      });

      setForm(defaultForm);
      onAccountAdded?.();
      loadAccounts();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add account');
    } finally {
      setSubmitting(false);
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm('Are you sure you want to delete this account?')) return;

    try {
      await api.deleteAccount(id);
      loadAccounts();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete account');
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
      default:
        return <StatusBadge status="info" label="Inactive" />;
    }
  };

  return (
    <div className="flex flex-col gap-6">
      <Card>
        <div className="flex items-center justify-between border-b px-6 py-4">
          <h3 className="text-lg font-semibold">Add Email Account</h3>
        </div>
        <div className="p-6">
          <form onSubmit={handleSubmit} className="max-w-[800px]">
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

            {error && (
              <div className="mb-4 rounded-lg border border-destructive bg-destructive/10 px-4 py-3 text-destructive">
                {error}
              </div>
            )}

            <Button type="submit" variant="default" disabled={submitting}>
              {submitting ? 'Adding...' : 'Add Account'}
            </Button>
          </form>
        </div>
      </Card>

      <Card>
        <div className="flex items-center justify-between border-b px-6 py-4">
          <h3 className="text-lg font-semibold">Your Accounts</h3>
          <Button variant="outline" size="sm" onClick={loadAccounts}>
            Refresh
          </Button>
        </div>
        <div className="p-6">
          {loading ? (
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
                      onClick={() => {/* TODO: Edit account */ }}
                    >
                      Edit
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
