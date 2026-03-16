import { useState, useEffect } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { StatusBadge } from '@/components/ui/StatusBadge';
import { ServerCapabilitiesDisplay } from './accounts/ServerCapabilitiesDisplay';
import { SyncSettingsDialog } from './accounts/SyncSettingsDialog';
import type { Account } from '@/types';
import * as api from '@/services/api';

export function AccountDetailView() {
  const { accountId } = useParams<{ accountId: string }>();
  const navigate = useNavigate();
  const [account, setAccount] = useState<Account | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [syncSettingsOpen, setSyncSettingsOpen] = useState(false);

  useEffect(() => {
    if (!accountId) {
      navigate('/accounts');
      return;
    }

    const fetchAccount = async () => {
      try {
        setLoading(true);
        const data = await api.getAccount(accountId);
        setAccount(data);
        setError(null);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load account');
      } finally {
        setLoading(false);
      }
    };

    fetchAccount();
  }, [accountId, navigate]);

  const handleRefresh = async () => {
    if (!accountId) return;
    try {
      setLoading(true);
      const data = await api.getAccount(accountId);
      setAccount(data);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to refresh account');
    } finally {
      setLoading(false);
    }
  };

  const handleDelete = async () => {
    if (!accountId || !confirm('Are you sure you want to delete this account?')) return;

    try {
      await api.deleteAccount(accountId);
      navigate('/accounts');
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

  const formatDate = (dateString: string) => {
    return new Date(dateString).toLocaleDateString('en-US', {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  };

  const formatSyncInterval = (seconds: number): string => {
    if (seconds < 60) return `${seconds} seconds`;
    if (seconds < 3600) return `${Math.round(seconds / 60)} minutes`;
    if (seconds < 86400) return `${Math.round(seconds / 3600)} hours`;
    return `${Math.round(seconds / 86400)} days`;
  };

  if (loading) {
    return (
      <div className="flex flex-col gap-6">
        <Card>
          <div className="p-12 text-center text-muted-foreground">Loading account...</div>
        </Card>
      </div>
    );
  }

  if (error || !account) {
    return (
      <div className="flex flex-col gap-6">
        <Card>
          <div className="p-12 text-center">
            <div className="text-5xl mb-4">❌</div>
            <p className="text-muted-foreground">{error || 'Account not found'}</p>
            <Button variant="outline" className="mt-4" onClick={() => navigate('/accounts')}>
              Back to Accounts
            </Button>
          </div>
        </Card>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6">
      {/* Header Card */}
      <Card>
        <div className="flex items-center justify-between border-b px-6 py-4">
          <div className="flex items-center gap-3">
            <Button variant="ghost" size="sm" onClick={() => navigate('/accounts')}>
              <svg className="h-4 w-4 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M15 19l-7-7 7-7" />
              </svg>
              Back
            </Button>
            <h3 className="text-lg font-semibold">Account Details</h3>
          </div>
          <div className="flex items-center gap-2">
            {getStatusBadge(account.status)}
            <Button variant="outline" size="sm" onClick={() => window.location.reload()}>
              Refresh
            </Button>
          </div>
        </div>

        <div className="p-6">
          <div className="grid grid-cols-2 gap-6">
            <div>
              <h4 className="text-sm font-semibold text-muted-foreground mb-3">Account Information</h4>
              <dl className="space-y-2 text-sm">
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">Email:</dt>
                  <dd className="font-medium">{account.email}</dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">Status:</dt>
                  <dd>{getStatusBadge(account.status)}</dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">Auth Type:</dt>
                  <dd className="capitalize">{account.auth_type || 'password'}</dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">Created:</dt>
                  <dd>{formatDate(account.created_at)}</dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">Last Updated:</dt>
                  <dd>{formatDate(account.updated_at)}</dd>
                </div>
                {account.last_sync_at && (
                  <div className="flex justify-between">
                    <dt className="text-muted-foreground">Last Sync:</dt>
                    <dd>{formatDate(account.last_sync_at)}</dd>
                  </div>
                )}
              </dl>
            </div>

            <div>
              <div className="flex items-center justify-between mb-3">
                <h4 className="text-sm font-semibold text-muted-foreground">Sync Settings</h4>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setSyncSettingsOpen(true)}
                  className="h-7 text-xs"
                >
                  <svg className="h-3.5 w-3.5 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z" />
                  </svg>
                  Edit
                </Button>
              </div>
              <dl className="space-y-2 text-sm">
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">Auto Sync:</dt>
                  <dd>
                    <span className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium ${
                      account.sync_settings.auto_sync
                        ? 'bg-green-100 text-green-800'
                        : 'bg-gray-100 text-gray-800'
                    }`}>
                      {account.sync_settings.auto_sync ? 'Enabled' : 'Disabled'}
                    </span>
                  </dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">Sync Interval:</dt>
                  <dd>{formatSyncInterval(account.sync_settings.sync_interval)}</dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">Historical Scope:</dt>
                  <dd>{account.sync_settings.historical_scope > 0 ? `${account.sync_settings.historical_scope} days` : 'All messages'}</dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-muted-foreground">Folders:</dt>
                  <dd className="text-xs">
                    <div className="flex gap-1 flex-wrap">
                      <span className="px-1.5 py-0.5 rounded bg-muted">INBOX</span>
                      {account.sync_settings.include_spam && (
                        <span className="px-1.5 py-0.5 rounded bg-muted">Spam</span>
                      )}
                      {account.sync_settings.include_trash && (
                        <span className="px-1.5 py-0.5 rounded bg-muted">Trash</span>
                      )}
                    </div>
                  </dd>
                </div>
                {account.last_sync_at && (
                  <div className="flex justify-between">
                    <dt className="text-muted-foreground">Last Sync:</dt>
                    <dd>{formatDate(account.last_sync_at)}</dd>
                  </div>
                )}
              </dl>
            </div>
          </div>

          <div className="mt-6 pt-6 border-t">
            <h4 className="text-sm font-semibold mb-3">Server Configuration</h4>
            <div className="grid grid-cols-2 gap-6">
              <div>
                <h5 className="text-xs font-semibold text-muted-foreground mb-2">IMAP Server</h5>
                <div className="text-sm bg-muted/50 rounded-lg p-3">
                  <div className="flex justify-between mb-1">
                    <span className="text-muted-foreground">Host:</span>
                    <span className="font-mono">{account.imap_config.host}</span>
                  </div>
                  <div className="flex justify-between mb-1">
                    <span className="text-muted-foreground">Port:</span>
                    <span className="font-mono">{account.imap_config.port}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Encryption:</span>
                    <span className="uppercase">{account.imap_config.encryption}</span>
                  </div>
                </div>
              </div>

              <div>
                <h5 className="text-xs font-semibold text-muted-foreground mb-2">SMTP Server</h5>
                <div className="text-sm bg-muted/50 rounded-lg p-3">
                  <div className="flex justify-between mb-1">
                    <span className="text-muted-foreground">Host:</span>
                    <span className="font-mono">{account.smtp_config.host}</span>
                  </div>
                  <div className="flex justify-between mb-1">
                    <span className="text-muted-foreground">Port:</span>
                    <span className="font-mono">{account.smtp_config.port}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Encryption:</span>
                    <span className="uppercase">{account.smtp_config.encryption}</span>
                  </div>
                </div>
              </div>
            </div>
          </div>

          <div className="mt-6 pt-6 border-t flex justify-end gap-2">
            <Button variant="destructive" size="sm" onClick={handleDelete}>
              Delete Account
            </Button>
          </div>
        </div>
      </Card>

      {/* Server Capabilities Card */}
      <Card>
        <div className="p-6">
          <ServerCapabilitiesDisplay accountId={account.id} initialCapabilities={account.server_capabilities} />
        </div>
      </Card>

      {/* Sync Settings Dialog */}
      <SyncSettingsDialog
        account={account}
        open={syncSettingsOpen}
        onOpenChange={setSyncSettingsOpen}
        onSuccess={handleRefresh}
      />
    </div>
  );
}
