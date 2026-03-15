import { useState, useEffect, useCallback } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { ScrollArea } from '@/components/ui/scroll-area';
import { ServerCapabilitiesDisplay } from './accounts/ServerCapabilitiesDisplay';
import type { Account } from '@/types';
import * as api from '@/services/api';

export function ServerDetailView() {
  const { serverKey } = useParams<{ serverKey: string }>();
  const navigate = useNavigate();
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedAccount, setSelectedAccount] = useState<string | null>(null);

  const fetchAccounts = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await api.listAccounts();
      
      // Filter accounts by server key
      const filtered = data.filter(acc => {
        const host = acc.imap_config.host.toLowerCase();
        const port = acc.imap_config.port;
        return `${host}:${port}` === serverKey;
      });
      
      setAccounts(filtered);
      
      // Select first account by default
      if (filtered.length > 0 && !selectedAccount) {
        setSelectedAccount(filtered[0].id);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load accounts');
    } finally {
      setLoading(false);
    }
  }, [serverKey, selectedAccount]);

  const refreshAllCapabilities = useCallback(async () => {
    try {
      const promises = accounts.map(acc => api.refreshServerCapabilities(acc.id));
      await Promise.allSettled(promises);
      await fetchAccounts();
    } catch (err) {
      console.error('Failed to refresh capabilities:', err);
    }
  }, [accounts, fetchAccounts]);

  useEffect(() => {
    fetchAccounts();
  }, [fetchAccounts]);

  const selectedAccountData = accounts.find(a => a.id === selectedAccount);

  // Get server info from first account with capabilities
  const serverInfo = accounts.find(a => a.server_capabilities);
  const serverCapabilities = serverInfo?.server_capabilities;
  const serverName = serverCapabilities?.server_name || serverKey?.split(':')[0] || 'Unknown';
  const serverVendor = serverCapabilities?.server_vendor;
  const serverVersion = serverCapabilities?.server_version;

  if (loading) {
    return (
      <div className="flex flex-col gap-6">
        <Card>
          <div className="p-12 text-center text-muted-foreground">Loading server details...</div>
        </Card>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex flex-col gap-6">
        <Card>
          <div className="p-12 text-center">
            <div className="text-5xl mb-4">❌</div>
            <p className="text-muted-foreground">{error}</p>
            <Button variant="outline" className="mt-4" onClick={() => navigate('/servers')}>
              Back to Servers
            </Button>
          </div>
        </Card>
      </div>
    );
  }

  if (accounts.length === 0) {
    return (
      <div className="flex flex-col gap-6">
        <Card>
          <div className="p-12 text-center">
            <div className="text-5xl mb-4">📭</div>
            <p className="text-muted-foreground">No accounts found for this server</p>
            <Button variant="outline" className="mt-4" onClick={() => navigate('/servers')}>
              Back to Servers
            </Button>
          </div>
        </Card>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6">
      {/* Header */}
      <Card>
        <div className="flex items-center justify-between border-b px-6 py-4">
          <div className="flex items-center gap-3">
            <Button variant="ghost" size="sm" onClick={() => navigate('/servers')}>
              <svg className="h-4 w-4 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M15 19l-7-7 7-7" />
              </svg>
              Back
            </Button>
            <div>
              <h3 className="text-lg font-semibold">
                {serverVendor || serverName}
                {serverVersion && <span className="text-muted-foreground font-normal ml-1">v{serverVersion}</span>}
              </h3>
              <p className="text-sm text-muted-foreground font-mono">{serverKey}</p>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <span className="text-sm text-muted-foreground">
              {accounts.length} account{accounts.length !== 1 ? 's' : ''}
            </span>
            <Button variant="outline" size="sm" onClick={refreshAllCapabilities}>
              <svg className="h-4 w-4 mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
              </svg>
              Refresh All
            </Button>
          </div>
        </div>

        <div className="p-6">
          {/* Quick Stats */}
          <div className="grid grid-cols-4 gap-4 mb-6">
            <div className="bg-muted/50 rounded-lg p-4 text-center">
              <div className="text-3xl font-bold">{accounts.length}</div>
              <div className="text-sm text-muted-foreground">Accounts</div>
            </div>
            <div className="bg-green-50 rounded-lg p-4 text-center border border-green-200">
              <div className="text-3xl font-bold text-green-700">
                {accounts.filter(a => a.server_capabilities?.supports_qresync).length}
              </div>
              <div className="text-sm text-green-600">QRESYNC</div>
            </div>
            <div className="bg-blue-50 rounded-lg p-4 text-center border border-blue-200">
              <div className="text-3xl font-bold text-blue-700">
                {accounts.filter(a => a.server_capabilities?.supports_condstore).length}
              </div>
              <div className="text-sm text-blue-600">CONDSTORE</div>
            </div>
            <div className="bg-purple-50 rounded-lg p-4 text-center border border-purple-200">
              <div className="text-3xl font-bold text-purple-700">
                {accounts.filter(a => a.server_capabilities?.supports_idle).length}
              </div>
              <div className="text-sm text-purple-600">IDLE</div>
            </div>
          </div>

          {/* Capabilities Summary from all accounts */}
          {serverCapabilities && (
            <div>
              <h4 className="text-sm font-semibold mb-3">Detected Capabilities</h4>
              <div className="flex flex-wrap gap-2">
                {serverCapabilities.supports_qresync && (
                  <span className="px-3 py-1 bg-green-100 text-green-800 rounded-full text-sm font-medium">
                    ✓ QRESYNC (RFC 7162)
                  </span>
                )}
                {serverCapabilities.supports_condstore && (
                  <span className="px-3 py-1 bg-blue-100 text-blue-800 rounded-full text-sm font-medium">
                    ✓ CONDSTORE
                  </span>
                )}
                {serverCapabilities.supports_idle && (
                  <span className="px-3 py-1 bg-purple-100 text-purple-800 rounded-full text-sm font-medium">
                    ✓ IDLE
                  </span>
                )}
                {serverCapabilities.supports_sort && (
                  <span className="px-3 py-1 bg-gray-100 text-gray-800 rounded-full text-sm">
                    ✓ SORT
                  </span>
                )}
                {serverCapabilities.supports_uid_plus && (
                  <span className="px-3 py-1 bg-gray-100 text-gray-800 rounded-full text-sm">
                    ✓ UIDPLUS
                  </span>
                )}
                {serverCapabilities.supports_move && (
                  <span className="px-3 py-1 bg-gray-100 text-gray-800 rounded-full text-sm">
                    ✓ MOVE
                  </span>
                )}
                {serverCapabilities.supports_utf8_accept && (
                  <span className="px-3 py-1 bg-gray-100 text-gray-800 rounded-full text-sm">
                    ✓ UTF8
                  </span>
                )}
              </div>
            </div>
          )}
        </div>
      </Card>

      {/* Account List and Capabilities */}
      <div className="grid grid-cols-3 gap-6">
        {/* Account List */}
        <Card className="col-span-1">
          <div className="border-b px-4 py-3">
            <h4 className="font-semibold text-sm">Accounts</h4>
          </div>
          <ScrollArea className="h-[500px]">
            <div className="p-2 space-y-1">
              {accounts.map(account => (
                <button
                  key={account.id}
                  onClick={() => setSelectedAccount(account.id)}
                  className={`w-full text-left p-3 rounded-lg transition-colors ${
                    selectedAccount === account.id
                      ? 'bg-primary text-primary-foreground'
                      : 'hover:bg-muted'
                  }`}
                >
                  <div className="font-medium text-sm truncate">{account.email}</div>
                  <div className={`text-xs mt-1 ${
                    selectedAccount === account.id ? 'text-primary-foreground/70' : 'text-muted-foreground'
                  }`}>
                    {account.server_capabilities?.supports_qresync ? (
                      <span className="text-green-600">● QRESYNC</span>
                    ) : account.server_capabilities?.supports_condstore ? (
                      <span className="text-blue-600">● CONDSTORE</span>
                    ) : (
                      <span className="text-yellow-600">● Basic</span>
                    )}
                  </div>
                </button>
              ))}
            </div>
          </ScrollArea>
        </Card>

        {/* Capabilities Display */}
        <Card className="col-span-2">
          <div className="border-b px-4 py-3 flex items-center justify-between">
            <h4 className="font-semibold text-sm">
              {selectedAccountData?.email}'s Server Capabilities
            </h4>
            <Button
              variant="outline"
              size="sm"
              onClick={() => navigate(`/accounts/${selectedAccount}`)}
            >
              View Account Details
            </Button>
          </div>
          <div className="p-4">
            {selectedAccount && (
              <ServerCapabilitiesDisplay 
                accountId={selectedAccount} 
                initialCapabilities={selectedAccountData?.server_capabilities}
              />
            )}
          </div>
        </Card>
      </div>
    </div>
  );
}
