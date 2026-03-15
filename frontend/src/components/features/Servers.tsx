import { useState, useEffect, useCallback, useMemo } from 'react';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { ScrollArea } from '@/components/ui/scroll-area';
import type { Account, ServerCapabilities } from '@/types';
import * as api from '@/services/api';

interface ServerGroup {
  serverKey: string;
  serverName: string;
  serverVendor?: string;
  serverVersion?: string;
  accounts: Account[];
  capabilities?: ServerCapabilities;
  accountCount: number;
  qresyncCount: number;
  condstoreCount: number;
}

export function ServersView() {
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [expandedServer, setExpandedServer] = useState<string | null>(null);

  const fetchAccounts = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await api.listAccounts();
      setAccounts(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load accounts');
    } finally {
      setLoading(false);
    }
  }, []);

  const refreshServerCapabilities = useCallback(async (_serverKey: string, accountIds: string[]) => {
    try {
      const promises = accountIds.map(id => api.refreshServerCapabilities(id));
      await Promise.allSettled(promises);
      await fetchAccounts();
    } catch (err) {
      console.error('Failed to refresh capabilities:', err);
    }
  }, [fetchAccounts]);

  useEffect(() => {
    fetchAccounts();
  }, [fetchAccounts]);

  // Group accounts by server
  const serverGroups = useMemo<ServerGroup[]>(() => {
    const groups = new Map<string, ServerGroup>();

    accounts.forEach(account => {
      const host = account.imap_config.host.toLowerCase();
      const port = account.imap_config.port;
      const serverKey = `${host}:${port}`;

      if (!groups.has(serverKey)) {
        const caps = account.server_capabilities;
        groups.set(serverKey, {
          serverKey,
          serverName: caps?.server_name || host,
          serverVendor: caps?.server_vendor,
          serverVersion: caps?.server_version,
          accounts: [],
          capabilities: caps || undefined,
          accountCount: 0,
          qresyncCount: 0,
          condstoreCount: 0,
        });
      }

      const group = groups.get(serverKey)!;
      group.accounts.push(account);
      group.accountCount++;

      if (account.server_capabilities) {
        if (account.server_capabilities.supports_qresync) {
          group.qresyncCount++;
        }
        if (account.server_capabilities.supports_condstore) {
          group.condstoreCount++;
        }
        // Use the most complete capabilities info
        if (!group.capabilities || 
            (account.server_capabilities.capabilities?.length || 0) > (group.capabilities.capabilities?.length || 0)) {
          group.capabilities = account.server_capabilities;
        }
      }
    });

    // Convert to array and sort by account count
    return Array.from(groups.values()).sort((a, b) => b.accountCount - a.accountCount);
  }, [accounts]);

  const totalServers = serverGroups.length;
  const totalQResync = serverGroups.reduce((sum, g) => sum + (g.capabilities?.supports_qresync ? 1 : 0), 0);
  const totalCondStore = serverGroups.reduce((sum, g) => sum + (g.capabilities?.supports_condstore ? 1 : 0), 0);

  if (loading) {
    return (
      <div className="flex flex-col gap-6">
        <Card>
          <div className="p-12 text-center text-muted-foreground">Loading servers...</div>
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
            <Button variant="outline" className="mt-4" onClick={fetchAccounts}>
              Retry
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
          <div>
            <h3 className="text-lg font-semibold">IMAP Servers</h3>
            <p className="text-sm text-muted-foreground">
              {totalServers} unique server{totalServers !== 1 ? 's' : ''} detected across {accounts.length} account{accounts.length !== 1 ? 's' : ''}
            </p>
          </div>
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" onClick={fetchAccounts}>
              Refresh
            </Button>
          </div>
        </div>

        <div className="p-6">
          {/* Summary Stats */}
          <div className="grid grid-cols-4 gap-4">
            <div className="bg-muted/50 rounded-lg p-4 text-center">
              <div className="text-3xl font-bold text-primary">{totalServers}</div>
              <div className="text-sm text-muted-foreground">Unique Servers</div>
            </div>
            <div className="bg-green-50 rounded-lg p-4 text-center border border-green-200">
              <div className="text-3xl font-bold text-green-700">{totalQResync}</div>
              <div className="text-sm text-green-600">QRESYNC Servers</div>
            </div>
            <div className="bg-blue-50 rounded-lg p-4 text-center border border-blue-200">
              <div className="text-3xl font-bold text-blue-700">{totalCondStore}</div>
              <div className="text-sm text-blue-600">CONDSTORE Servers</div>
            </div>
            <div className="bg-purple-50 rounded-lg p-4 text-center border border-purple-200">
              <div className="text-3xl font-bold text-purple-700">{accounts.length}</div>
              <div className="text-sm text-purple-600">Total Accounts</div>
            </div>
          </div>
        </div>
      </Card>

      {/* Server List */}
      <Card>
        <div className="border-b px-6 py-4">
          <h4 className="font-semibold">Detected Servers</h4>
          <p className="text-sm text-muted-foreground mt-1">
            Click on a server to view detailed capabilities
          </p>
        </div>

        <div className="p-0">
          {serverGroups.length === 0 ? (
            <div className="p-12 text-center text-muted-foreground">
              No servers detected. Add email accounts to see server information.
            </div>
          ) : (
            <ScrollArea className="h-[600px]">
              <div className="divide-y">
                {serverGroups.map((server) => {
                  const isExpanded = expandedServer === server.serverKey;
                  const hasCapabilities = !!server.capabilities;

                  return (
                    <div key={server.serverKey} className="p-4 hover:bg-muted/30 transition-colors">
                      <div className="flex items-center justify-between">
                        <div 
                          className="flex items-center gap-4 flex-1 cursor-pointer"
                          onClick={() => setExpandedServer(isExpanded ? null : server.serverKey)}
                        >
                          {/* Server Icon */}
                          <div className={`h-12 w-12 rounded-lg flex items-center justify-center ${
                            server.capabilities?.supports_qresync 
                              ? 'bg-green-100 text-green-700' 
                              : 'bg-muted text-muted-foreground'
                          }`}>
                            <svg className="h-6 w-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2m-2-4h.01M17 16h.01" />
                            </svg>
                          </div>

                          {/* Server Info */}
                          <div className="flex-1">
                            <div className="flex items-center gap-2">
                              <h5 className="font-semibold text-base">
                                {server.serverVendor || server.serverName}
                                {server.serverVersion && <span className="text-muted-foreground font-normal ml-1">v{server.serverVersion}</span>}
                              </h5>
                              {server.capabilities?.supports_qresync && (
                                <span className="px-2 py-0.5 bg-green-100 text-green-800 rounded text-xs font-medium">
                                  QRESYNC
                                </span>
                              )}
                              {server.capabilities?.supports_condstore && !server.capabilities?.supports_qresync && (
                                <span className="px-2 py-0.5 bg-blue-100 text-blue-800 rounded text-xs font-medium">
                                  CONDSTORE
                                </span>
                              )}
                            </div>
                            <div className="flex items-center gap-4 text-sm text-muted-foreground mt-1">
                              <span className="font-mono">{server.serverKey}</span>
                              <span>{server.accountCount} account{server.accountCount !== 1 ? 's' : ''}</span>
                              {server.qresyncCount > 0 && (
                                <span className="text-green-600">{server.qresyncCount} with QRESYNC</span>
                              )}
                            </div>
                          </div>
                        </div>

                        {/* Actions */}
                        <div className="flex items-center gap-2">
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => refreshServerCapabilities(server.serverKey, server.accounts.map(a => a.id))}
                            disabled={!hasCapabilities}
                          >
                            <svg className={`h-4 w-4 mr-1 ${!hasCapabilities ? 'animate-spin' : ''}`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
                            </svg>
                            Refresh
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => setExpandedServer(isExpanded ? null : server.serverKey)}
                          >
                            <svg 
                              className={`h-4 w-4 transition-transform ${isExpanded ? 'rotate-180' : ''}`} 
                              fill="none" 
                              stroke="currentColor" 
                              viewBox="0 0 24 24"
                            >
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M19 9l-7 7-7-7" />
                            </svg>
                          </Button>
                        </div>
                      </div>

                      {/* Expanded Details */}
                      {isExpanded && (
                        <div className="mt-4 pt-4 border-t ml-16">
                          <div className="grid grid-cols-2 gap-4 mb-4">
                            <div>
                              <h6 className="text-xs font-semibold text-muted-foreground mb-2">Server Information</h6>
                              <dl className="space-y-1 text-sm">
                                <div className="flex justify-between">
                                  <dt className="text-muted-foreground">Hostname:</dt>
                                  <dd className="font-mono">{server.serverKey.split(':')[0]}</dd>
                                </div>
                                <div className="flex justify-between">
                                  <dt className="text-muted-foreground">Port:</dt>
                                  <dd className="font-mono">{server.serverKey.split(':')[1]}</dd>
                                </div>
                                {server.serverVendor && (
                                  <div className="flex justify-between">
                                    <dt className="text-muted-foreground">Vendor:</dt>
                                    <dd>{server.serverVendor}</dd>
                                  </div>
                                )}
                                {server.serverVersion && (
                                  <div className="flex justify-between">
                                    <dt className="text-muted-foreground">Version:</dt>
                                    <dd>{server.serverVersion}</dd>
                                  </div>
                                )}
                              </dl>
                            </div>

                            <div>
                              <h6 className="text-xs font-semibold text-muted-foreground mb-2">Accounts on this Server</h6>
                              <div className="space-y-1">
                                {server.accounts.map(account => (
                                  <div key={account.id} className="flex items-center justify-between text-sm p-2 bg-muted/30 rounded">
                                    <span className="truncate max-w-[200px]">{account.email}</span>
                                    <Button
                                      variant="ghost"
                                      size="sm"
                                      className="h-6 text-xs"
                                      onClick={() => window.location.href = `/accounts/${account.id}`}
                                    >
                                      View
                                    </Button>
                                  </div>
                                ))}
                              </div>
                            </div>
                          </div>

                          {/* Capabilities Summary */}
                          {server.capabilities && (
                            <div>
                              <h6 className="text-xs font-semibold text-muted-foreground mb-2">Key Capabilities</h6>
                              <div className="flex flex-wrap gap-2">
                                {server.capabilities.supports_qresync && (
                                  <span className="px-2 py-1 bg-green-100 text-green-800 rounded text-xs">✓ QRESYNC</span>
                                )}
                                {server.capabilities.supports_condstore && (
                                  <span className="px-2 py-1 bg-blue-100 text-blue-800 rounded text-xs">✓ CONDSTORE</span>
                                )}
                                {server.capabilities.supports_idle && (
                                  <span className="px-2 py-1 bg-purple-100 text-purple-800 rounded text-xs">✓ IDLE</span>
                                )}
                                {server.capabilities.supports_sort && (
                                  <span className="px-2 py-1 bg-gray-100 text-gray-800 rounded text-xs">✓ SORT</span>
                                )}
                                {server.capabilities.supports_uid_plus && (
                                  <span className="px-2 py-1 bg-gray-100 text-gray-800 rounded text-xs">✓ UIDPLUS</span>
                                )}
                                {server.capabilities.supports_move && (
                                  <span className="px-2 py-1 bg-gray-100 text-gray-800 rounded text-xs">✓ MOVE</span>
                                )}
                                {!server.capabilities.supports_qresync && !server.capabilities.supports_condstore && (
                                  <span className="px-2 py-1 bg-yellow-100 text-yellow-800 rounded text-xs">⚠ Basic IMAP</span>
                                )}
                              </div>
                            </div>
                          )}

                          <div className="mt-4 flex justify-end">
                            <Button
                              variant="default"
                              size="sm"
                              onClick={() => window.location.href = `/servers/${encodeURIComponent(server.serverKey)}`}
                            >
                              View Full Capabilities
                              <svg className="h-4 w-4 ml-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M9 5l7 7-7 7" />
                              </svg>
                            </Button>
                          </div>
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
              <div className="h-4" /> {/* Bottom padding for scroll area */}
            </ScrollArea>
          )}
        </div>
      </Card>
    </div>
  );
}
