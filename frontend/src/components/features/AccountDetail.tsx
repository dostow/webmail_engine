import { useState, useEffect } from 'react';
import { useParams, useNavigate, useSearchParams, Link } from 'react-router-dom';
import { toast } from 'sonner';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Badge } from '@/components/ui/badge';
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/Alert';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { ScrollArea } from '@/components/ui/scroll-area';
import { ServerCapabilitiesDisplay } from './accounts/ServerCapabilitiesDisplay';
import { SyncSettingsDialog } from './accounts/SyncSettingsDialog';
import { UpdateCredentialsDialog } from './accounts/UpdateCredentialsDialog';
import type { Account, FolderSyncInfo } from '@/types';
import * as api from '@/services/api';

export function AccountDetailView() {
  const { accountId } = useParams<{ accountId: string }>();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [account, setAccount] = useState<Account | null>(null);
  const [folders, setFolders] = useState<FolderSyncInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [syncSettingsOpen, setSyncSettingsOpen] = useState(false);
  const [credentialsOpen, setCredentialsOpen] = useState(false);
  const [showedDisabledToast, setShowedDisabledToast] = useState(false);

  useEffect(() => {
    if (!accountId) {
      navigate('/accounts');
      return;
    }

    const errorParam = searchParams.get('error');
    if (errorParam === 'account_disabled' && !showedDisabledToast) {
      setShowedDisabledToast(true);
      toast.error('Account disabled - Please update your email credentials to re-enable');
    }

    fetchAccountData();
  }, [accountId, navigate, searchParams, showedDisabledToast]);

  async function fetchAccountData() {
    if (!accountId) return;
    try {
      setLoading(true);
      const [accountData, folderData] = await Promise.all([
        api.getAccount(accountId).catch((err) => {
          setError(err instanceof Error ? err.message : 'Failed to load account');
          return null;
        }),
        api.getAccountFolders(accountId).catch(() => []),
      ]);

      if (accountData) {
        setAccount(accountData);
      }
      setFolders(folderData);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load data');
    } finally {
      setLoading(false);
    }
  }

  const handleRefresh = async () => {
    if (!accountId) return;
    try {
      setLoading(true);
      const [accountData, folderData] = await Promise.all([
        api.getAccount(accountId),
        api.getAccountFolders(accountId),
      ]);
      setAccount(accountData);
      setFolders(folderData);
      setError(null);
      toast.success('Data refreshed');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to refresh');
    } finally {
      setLoading(false);
    }
  };

  const handleDelete = async () => {
    if (!accountId || !confirm('Are you sure you want to delete this account?')) return;

    try {
      await api.deleteAccount(accountId);
      navigate('/accounts');
      toast.success('Account deleted');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete account');
    }
  };

  const handleCredentialsUpdateSuccess = () => {
    toast.success('Credentials updated successfully! Account re-enabled.');
    fetchAccountData();
    setCredentialsOpen(false);
  };

  if (!account && !loading) {
    return (
      <div className="flex items-center justify-center min-h-[400px]">
        <div className="text-center">
          <h2 className="text-2xl font-bold mb-2">Account not found</h2>
          <Button onClick={() => navigate('/accounts')}>Back to Accounts</Button>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="space-y-1">
          <div className="flex items-center gap-2">
            <Button variant="ghost" size="sm" onClick={() => navigate('/accounts')}>
              <svg className="h-4 w-4 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M15 19l-7-7 7-7" />
              </svg>
              Back
            </Button>
            {account && getStatusBadge(account.status)}
          </div>
          <h1 className="text-3xl font-bold">{account?.email}</h1>
          <p className="text-muted-foreground">Account ID: {account?.id}</p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" onClick={handleRefresh} disabled={loading}>
            <svg className="h-4 w-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
            Refresh
          </Button>
          {(account?.status === 'disabled' || account?.status === 'auth_required') && (
            <Button onClick={() => setCredentialsOpen(true)}>
              Update Credentials
            </Button>
          )}
          <Link to={`/accounts/${accountId}/processors`}>
            <Button variant="outline">
              <svg className="h-4 w-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
              </svg>
              Processors
            </Button>
          </Link>
          <Button variant="destructive" onClick={handleDelete}>
            Delete Account
          </Button>
        </div>
      </div>

      {/* Error Alert */}
      {error && (
        <Alert variant="destructive">
          <AlertTitle>Error</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {/* Account Status Alert */}
      {account?.status === 'error' && (
        <Alert>
          <AlertTitle>Connection Error</AlertTitle>
          <AlertDescription>
            This account is experiencing errors. Check your connection settings and try refreshing.
          </AlertDescription>
        </Alert>
      )}

      {/* Main Content Tabs */}
      <Tabs defaultValue="overview" className="space-y-4">
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="folders">Synced Folders ({folders.length})</TabsTrigger>
          <TabsTrigger value="server">Server Info</TabsTrigger>
          <TabsTrigger value="settings">Settings</TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="space-y-4">
          <ScrollArea className="h-[600px] pr-4">
            <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
              {/* Connection Info */}
              <Card>
                <CardHeader>
                  <CardTitle className="text-lg">IMAP Configuration</CardTitle>
                  <CardDescription>Incoming mail server settings</CardDescription>
                </CardHeader>
                <CardContent className="space-y-2 text-sm">
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Host</span>
                    <span className="font-medium">{account?.imap_config.host}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Port</span>
                    <span className="font-medium">{account?.imap_config.port}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Encryption</span>
                    <Badge variant="outline" className="capitalize">
                      {account?.imap_config.encryption}
                    </Badge>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Username</span>
                    <span className="font-medium">{account?.imap_config.username}</span>
                  </div>
                </CardContent>
              </Card>

              {/* SMTP Info */}
              <Card>
                <CardHeader>
                  <CardTitle className="text-lg">SMTP Configuration</CardTitle>
                  <CardDescription>Outgoing mail server settings</CardDescription>
                </CardHeader>
                <CardContent className="space-y-2 text-sm">
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Host</span>
                    <span className="font-medium">{account?.smtp_config.host}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Port</span>
                    <span className="font-medium">{account?.smtp_config.port}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Encryption</span>
                    <Badge variant="outline" className="capitalize">
                      {account?.smtp_config.encryption}
                    </Badge>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Username</span>
                    <span className="font-medium">{account?.smtp_config.username}</span>
                  </div>
                </CardContent>
              </Card>

              {/* Account Stats */}
              <Card>
                <CardHeader>
                  <CardTitle className="text-lg">Account Information</CardTitle>
                  <CardDescription>General account details</CardDescription>
                </CardHeader>
                <CardContent className="space-y-2 text-sm">
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Auth Type</span>
                    <Badge variant="outline" className="capitalize">
                      {account?.auth_type || 'password'}
                    </Badge>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Created</span>
                    <span className="font-medium">{formatDate(account?.created_at || '')}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Last Sync</span>
                    <span className="font-medium">
                      {account?.last_sync_at ? formatDate(account.last_sync_at) : 'Never'}
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Synced Folders</span>
                    <Badge>{folders.length}</Badge>
                  </div>
                </CardContent>
              </Card>
            </div>

            {/* Quick Stats */}
            {folders.length > 0 && (
              <Card className="mt-4">
                <CardHeader>
                  <CardTitle>Folder Overview</CardTitle>
                  <CardDescription>Summary of synchronized mailboxes</CardDescription>
                </CardHeader>
                <CardContent>
                  <div className="grid gap-3 md:grid-cols-2 lg:grid-cols-3">
                    {folders.slice(0, 6).map((folder) => (
                      <div
                        key={folder.name}
                        className="flex items-center justify-between p-3 rounded-lg border bg-card"
                      >
                        <div className="space-y-0.5">
                          <div className="font-medium text-sm">{folder.name}</div>
                          <div className="text-xs text-muted-foreground">
                            {folder.is_initialized ? 'Synced' : 'Pending'}
                          </div>
                        </div>
                        <div className="text-right">
                          <div className="text-sm font-medium">{folder.messages}</div>
                          <div className="text-xs text-muted-foreground">messages</div>
                        </div>
                      </div>
                    ))}
                  </div>
                </CardContent>
              </Card>
            )}
          </ScrollArea>
        </TabsContent>

        <TabsContent value="folders" className="space-y-0">
          <Card className="h-[calc(100vh-250px)] flex flex-col">
            <CardHeader className="flex-shrink-0 pb-3">
              <CardTitle>Synchronized Folders</CardTitle>
              <CardDescription>
                Mailboxes being synchronized for this account
              </CardDescription>
            </CardHeader>
            <CardContent className="flex-1 overflow-hidden p-0">
              <ScrollArea className="h-full w-full">
                <div className="p-6 pt-0">
                  {folders.length === 0 ? (
                    <div className="text-center py-8 text-muted-foreground">
                      No folders synchronized yet. Folders will appear here once sync begins.
                    </div>
                  ) : (
                    <div className="space-y-2">
                      {folders.map((folder) => (
                        <div
                          key={folder.name}
                          className="flex items-center justify-between p-4 rounded-lg border"
                        >
                          <div className="flex items-center gap-3">
                            <svg className="h-5 w-5 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z" />
                            </svg>
                            <div>
                              <div className="font-medium">{folder.name}</div>
                              <div className="text-xs text-muted-foreground">
                                Last sync: {formatRelativeTime(folder.last_sync)}
                              </div>
                            </div>
                          </div>
                          <div className="flex items-center gap-4">
                            <div className="text-right">
                              <div className="text-sm font-medium">{folder.messages.toLocaleString()}</div>
                              <div className="text-xs text-muted-foreground">messages</div>
                            </div>
                            {folder.unseen > 0 && (
                              <Badge variant="default">{folder.unseen} unread</Badge>
                            )}
                            <Badge variant={folder.is_initialized ? 'default' : 'secondary'}>
                              {folder.is_initialized ? 'Synced' : 'Pending'}
                            </Badge>
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              </ScrollArea>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="server" className="space-y-0">
          <Card className="h-[calc(100vh-250px)] flex flex-col">
            <CardHeader className="flex-shrink-0 pb-3">
              <CardTitle>Server Capabilities</CardTitle>
              <CardDescription>IMAP server features and extensions</CardDescription>
            </CardHeader>
            <CardContent className="flex-1 overflow-hidden p-0">
              <ScrollArea className="h-full w-full">
                <div className="p-6 pt-0">
                  {account?.server_capabilities ? (
                    <ServerCapabilitiesDisplay accountId={accountId!} initialCapabilities={account.server_capabilities} />
                  ) : (
                    <div className="text-center py-8 text-muted-foreground">
                      Server capabilities not available
                    </div>
                  )}
                </div>
              </ScrollArea>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="settings" className="space-y-0">
          <Card className="h-[calc(100vh-250px)] flex flex-col">
            <CardHeader className="flex-shrink-0 pb-3">
              <div className="flex items-center justify-between">
                <div>
                  <CardTitle>Sync Settings</CardTitle>
                  <CardDescription>Configure how this account synchronizes</CardDescription>
                </div>
                <Button onClick={() => setSyncSettingsOpen(true)} variant="outline" size="sm">
                  <svg className="h-4 w-4 mr-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z" />
                  </svg>
                  Edit
                </Button>
              </div>
            </CardHeader>
            <CardContent className="flex-1 overflow-hidden p-0">
              <ScrollArea className="h-full w-full">
                <div className="p-6 pt-0">
                  <div className="grid gap-4 md:grid-cols-2">
                    <div className="space-y-1">
                      <div className="text-sm font-medium">Auto Sync</div>
                      <Badge variant={account?.sync_settings.auto_sync ? 'default' : 'secondary'}>
                        {account?.sync_settings.auto_sync ? 'Enabled' : 'Disabled'}
                      </Badge>
                    </div>
                    <div className="space-y-1">
                      <div className="text-sm font-medium">Sync Interval</div>
                      <div className="text-sm text-muted-foreground">
                        {formatSyncInterval(account?.sync_settings.sync_interval || 0)}
                      </div>
                    </div>
                    <div className="space-y-1">
                      <div className="text-sm font-medium">Historical Scope</div>
                      <div className="text-sm text-muted-foreground">
                        {account && account.sync_settings.historical_scope && account.sync_settings.historical_scope > 0
                          ? `${account.sync_settings.historical_scope} days`
                          : 'All messages'}
                      </div>
                    </div>
                    <div className="space-y-1">
                      <div className="text-sm font-medium">Max Message Size</div>
                      <div className="text-sm text-muted-foreground">
                        {formatFileSize(account?.sync_settings.max_message_size || 0)}
                      </div>
                    </div>
                  </div>
                </div>
              </ScrollArea>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>

      {/* Dialogs */}
      <SyncSettingsDialog
        account={account!}
        open={syncSettingsOpen}
        onOpenChange={setSyncSettingsOpen}
        onSuccess={fetchAccountData}
      />
      <UpdateCredentialsDialog
        account={account!}
        open={credentialsOpen}
        onOpenChange={setCredentialsOpen}
        onSuccess={handleCredentialsUpdateSuccess}
      />
    </div>
  );
}

// Helper functions
function getStatusBadge(status: string) {
  const variants: Record<string, 'default' | 'secondary' | 'destructive' | 'outline'> = {
    active: 'default',
    inactive: 'secondary',
    error: 'destructive',
    syncing: 'default',
    disabled: 'destructive',
    auth_required: 'destructive',
    throttled: 'secondary',
  };

  return (
    <Badge variant={variants[status] || 'outline'} className="capitalize">
      {status}
    </Badge>
  );
}

function formatDate(dateString: string): string {
  return new Date(dateString).toLocaleDateString('en-US', {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

function formatRelativeTime(dateString: string): string {
  const date = new Date(dateString);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMins = Math.floor(diffMs / 60000);
  const diffHours = Math.floor(diffMins / 60);
  const diffDays = Math.floor(diffHours / 24);

  if (diffMins < 1) return 'Just now';
  if (diffMins < 60) return `${diffMins}m ago`;
  if (diffHours < 24) return `${diffHours}h ago`;
  if (diffDays < 7) return `${diffDays}d ago`;
  return formatDate(dateString);
}

function formatSyncInterval(seconds: number): string {
  if (seconds < 60) return `${seconds} seconds`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)} minutes`;
  return `${Math.floor(seconds / 3600)} hours`;
}

function formatFileSize(bytes: number): string {
  if (bytes === 0) return 'Unlimited';
  const mb = bytes / (1024 * 1024);
  if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`;
  return `${mb.toFixed(1)} MB`;
}
