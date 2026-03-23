import { useState } from 'react';
import { useLoaderData, useNavigation, useNavigate } from 'react-router-dom';
import {
  Plus,
  RefreshCw,
  Trash2,
  AlertTriangle,
  Mail,
  Server,
  Settings2
} from 'lucide-react';

import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { StatusBadge } from '@/components/ui/StatusBadge';
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/Alert';
import { ScrollableContent } from '@/components/ui/scrollable-content';
import { AccountWizard } from './AccountWizard';

import type { Account } from '@/types';
import * as api from '@/services/api';

export function AccountsView() {
  const accounts = useLoaderData() as Account[];
  const navigation = useNavigation();
  const navigate = useNavigate();
  const [showWizard, setShowWizard] = useState(false);

  const handleDelete = async (id: string) => {
    if (!confirm('Are you sure you want to delete this account? This cannot be undone.')) return;
    try {
      await api.deleteAccount(id);
      window.location.reload();
    } catch (err) {
      console.error('Failed to delete account', err);
    }
  };

  const getStatusBadge = (status: string) => {
    const statusMap: Record<string, { variant: any; label: string }> = {
      active: { variant: 'success', label: 'Active' },
      error: { variant: 'error', label: 'Error' },
      syncing: { variant: 'warning', label: 'Syncing' },
      disabled: { variant: 'error', label: 'Disabled' },
      auth_required: { variant: 'warning', label: 'Auth Required' },
      throttled: { variant: 'warning', label: 'Throttled' },
    };
    const config = statusMap[status] || { variant: 'info', label: status };
    return <StatusBadge status={config.variant} label={config.label} />;
  };

  const accountsNeedingAttention = accounts.filter((acc) =>
    ['disabled', 'auth_required', 'error', 'throttled'].includes(acc.status)
  );

  return (
    <div className="flex flex-col h-full max-w-7xl mx-auto w-full gap-6 p-4 lg:p-8">

      {/* Page Header */}
      <header className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-foreground">Email Accounts</h1>
          <p className="text-sm text-muted-foreground">Manage your connected mailboxes and server configurations.</p>
        </div>

        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => window.location.reload()}
            disabled={navigation.state === 'loading'}
          >
            <RefreshCw className={`mr-2 h-4 w-4 ${navigation.state === 'loading' ? 'animate-spin' : ''}`} />
            Refresh
          </Button>

          <Button size="sm" className="shadow-sm" onClick={() => setShowWizard(true)}>
            <Plus className="mr-2 h-4 w-4" />
            Add Account
          </Button>
        </div>
      </header>

      {/* Account Alerts */}
      {accountsNeedingAttention.length > 0 && (
        <Alert variant="warning" className="border-warning/20 bg-warning/5 animate-in fade-in slide-in-from-top-2">
          <AlertTriangle className="h-4 w-4" />
          <div className="ml-2">
            <AlertTitle className="text-sm font-semibold">Account Connection Issues</AlertTitle>
            <AlertDescription className="text-xs opacity-90">
              The following accounts require re-authentication or server check: {accountsNeedingAttention.map(a => a.email).join(', ')}
            </AlertDescription>
          </div>
        </Alert>
      )}

      {/* Accounts Table/List */}
      <Card className="flex-1 flex flex-col min-h-0 overflow-hidden shadow-sm border-border/50">
        <ScrollableContent heightStrategy="flex" className="p-0">
          {accounts.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-20 text-center">
              <div className="w-16 h-16 bg-muted rounded-full flex items-center justify-center mb-4">
                <Mail className="h-8 w-8 text-muted-foreground opacity-30" />
              </div>
              <h3 className="text-base font-semibold">No accounts found</h3>
              <p className="text-sm text-muted-foreground max-w-xs mx-auto mb-6">
                You haven't added any email accounts yet. Connect your first account to start syncing.
              </p>
              <Button variant="outline" size="sm" onClick={() => setShowWizard(true)}>
                Add Account
              </Button>
            </div>
          ) : (
            <div className="divide-y divide-border">
              {/* Table Header */}
              <div className="hidden md:grid grid-cols-12 px-6 py-3 bg-muted/40 text-[10px] font-bold uppercase tracking-widest text-muted-foreground">
                <div className="col-span-5">Account Information</div>
                <div className="col-span-3">Configuration</div>
                <div className="col-span-2 text-center">Status</div>
                <div className="col-span-2 text-right">Actions</div>
              </div>

              {accounts.map((account) => (
                <div
                  key={account.id}
                  className="grid grid-cols-1 md:grid-cols-12 items-center px-6 py-4 hover:bg-muted/20 transition-colors"
                >
                  {/* Info */}
                  <div className="col-span-1 md:col-span-5 flex items-center gap-3">
                    <div className="h-8 w-8 rounded-full bg-primary/10 flex items-center justify-center text-primary shrink-0">
                      <Mail className="h-4 w-4" />
                    </div>
                    <div className="min-w-0">
                      <div className="font-medium text-sm truncate">{account.email}</div>
                      <div className="text-[10px] text-muted-foreground font-mono truncate uppercase opacity-60">
                        {account.id.split('-')[0]}...
                      </div>
                    </div>
                  </div>

                  {/* Server Details */}
                  <div className="col-span-1 md:col-span-3 py-2 md:py-0">
                    <div className="flex items-center gap-2 text-xs text-muted-foreground">
                      <Server className="h-3 w-3" />
                      <span className="truncate">{account.imap_config.host}</span>
                      <span className="px-1 py-0.5 rounded bg-muted text-[10px] font-mono">
                        {account.imap_config.port}
                      </span>
                    </div>
                  </div>

                  {/* Status */}
                  <div className="col-span-1 md:col-span-2 flex md:justify-center">
                    {getStatusBadge(account.status)}
                  </div>

                  {/* Actions */}
                  <div className="col-span-1 md:col-span-2 flex justify-end gap-1 mt-2 md:mt-0">
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-8 w-8 text-muted-foreground"
                      onClick={() => window.location.href = `/accounts/${account.id}`}
                      title="Settings"
                    >
                      <Settings2 className="h-4 w-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-8 w-8 text-destructive hover:bg-destructive/10"
                      onClick={() => handleDelete(account.id)}
                      title="Delete"
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </ScrollableContent>
      </Card>

      {showWizard && (
        <AccountWizard
          onComplete={() => {
            setShowWizard(false);
            navigate(0); // Re-run loader to refresh account list
          }}
          onCancel={() => setShowWizard(false)}
        />
      )}
    </div>
  );
}