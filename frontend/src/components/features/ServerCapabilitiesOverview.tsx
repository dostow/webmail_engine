import { useState, useEffect, useCallback } from 'react';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import type { Account, ServerCapabilities } from '@/types';
import * as api from '@/services/api';

interface CapabilityStats {
  name: string;
  description: string;
  supportedBy: string[]; // Account emails
  supportedCount: number;
  totalCount: number;
  percentage: number;
}

const CAPABILITY_INFO: Record<string, { description: string; rfc?: string }> = {
  supports_qresync: { description: 'Fast resynchronization with VANISHED support', rfc: 'RFC 7162' },
  supports_condstore: { description: 'Conditional STORE for efficient flag sync', rfc: 'RFC 7162' },
  supports_sort: { description: 'Server-side message sorting', rfc: 'RFC 5256' },
  supports_search_res: { description: 'Search result resynchronization', rfc: 'RFC 7162' },
  supports_literal_plus: { description: 'Non-synchronizing literals', rfc: 'RFC 7888' },
  supports_utf8_accept: { description: 'UTF-8 support in protocol', rfc: 'RFC 6855' },
  supports_utf8_only: { description: 'UTF-8 only mode', rfc: 'RFC 6855' },
  supports_move: { description: 'Atomic message move between folders', rfc: 'RFC 6851' },
  supports_uid_plus: { description: 'Extended UID operations', rfc: 'RFC 4315' },
  supports_unselect: { description: 'Close folder without selecting another', rfc: 'RFC 3691' },
  supports_idle: { description: 'Real-time push notifications', rfc: 'RFC 2177' },
  supports_starttls: { description: 'Upgrade plain connection to TLS', rfc: 'RFC 2595' },
  supports_auth_plain: { description: 'PLAIN authentication mechanism' },
  supports_auth_login: { description: 'LOGIN authentication mechanism' },
  supports_auth_oauth2: { description: 'OAuth2 authentication', rfc: 'RFC 7628' },
};

export function ServerCapabilitiesOverviewView() {
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [stats, setStats] = useState<CapabilityStats[]>([]);

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

  const refreshCapabilities = useCallback(async (accountId: string) => {
    try {
      await api.refreshServerCapabilities(accountId);
      await fetchAccounts(); // Reload accounts with updated capabilities
    } catch (err) {
      console.error('Failed to refresh capabilities:', err);
    }
  }, [fetchAccounts]);

  const refreshAllCapabilities = useCallback(async () => {
    const promises = accounts.map(acc => refreshCapabilities(acc.id));
    await Promise.allSettled(promises);
  }, [accounts, refreshCapabilities]);

  useEffect(() => {
    fetchAccounts();
  }, [fetchAccounts]);

  // Calculate capability statistics
  useEffect(() => {
    if (accounts.length === 0) {
      setStats([]);
      return;
    }

    const capabilityKeys = Object.keys(CAPABILITY_INFO).filter(k => k.startsWith('supports_')) as Array<keyof ServerCapabilities>;
    
    const newStats: CapabilityStats[] = capabilityKeys.map((key) => {
      const supportedBy = accounts
        .filter(acc => acc.server_capabilities?.[key] === true)
        .map(acc => acc.email);

      const supportedCount = supportedBy.length;
      const totalCount = accounts.length;
      const percentage = Math.round((supportedCount / totalCount) * 100);

      return {
        name: key.replace('supports_', '').toUpperCase().replace('_', '='),
        description: CAPABILITY_INFO[key]?.description || '',
        supportedBy,
        supportedCount,
        totalCount,
        percentage,
      };
    });

    // Sort by percentage (most supported first)
    newStats.sort((a, b) => b.percentage - a.percentage);
    setStats(newStats);
  }, [accounts]);

  // Get unique server vendors
  const serverVendors = Array.from(
    new Set(accounts.filter(a => a.server_capabilities?.server_vendor).map(a => a.server_capabilities!.server_vendor!))
  );

  const qresyncCount = accounts.filter(a => a.server_capabilities?.supports_qresync).length;
  const condstoreCount = accounts.filter(a => a.server_capabilities?.supports_condstore).length;
  const idleCount = accounts.filter(a => a.server_capabilities?.supports_idle).length;

  if (loading) {
    return (
      <div className="flex flex-col gap-6">
        <Card>
          <div className="p-12 text-center text-muted-foreground">Loading server capabilities...</div>
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
          <h3 className="text-lg font-semibold">Server Capabilities Overview</h3>
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" onClick={fetchAccounts}>
              Refresh
            </Button>
            <Button variant="outline" size="sm" onClick={refreshAllCapabilities} disabled={accounts.length === 0}>
              Refresh All Capabilities
            </Button>
          </div>
        </div>

        <div className="p-6">
          {/* Summary Stats */}
          <div className="grid grid-cols-4 gap-4 mb-6">
            <div className="bg-muted/50 rounded-lg p-4 text-center">
              <div className="text-3xl font-bold text-primary">{accounts.length}</div>
              <div className="text-sm text-muted-foreground">Total Accounts</div>
            </div>
            <div className="bg-green-50 rounded-lg p-4 text-center border border-green-200">
              <div className="text-3xl font-bold text-green-700">{qresyncCount}</div>
              <div className="text-sm text-green-600">QRESYNC Support</div>
            </div>
            <div className="bg-blue-50 rounded-lg p-4 text-center border border-blue-200">
              <div className="text-3xl font-bold text-blue-700">{condstoreCount}</div>
              <div className="text-sm text-blue-600">CONDSTORE Support</div>
            </div>
            <div className="bg-purple-50 rounded-lg p-4 text-center border border-purple-200">
              <div className="text-3xl font-bold text-purple-700">{idleCount}</div>
              <div className="text-sm text-purple-600">IDLE Support</div>
            </div>
          </div>

          {/* Server Vendors */}
          {serverVendors.length > 0 && (
            <div className="mb-6">
              <h4 className="text-sm font-semibold mb-2">Server Vendors Detected</h4>
              <div className="flex flex-wrap gap-2">
                {serverVendors.map(vendor => (
                  <span key={vendor} className="px-3 py-1 bg-muted rounded-full text-sm">
                    {vendor}
                  </span>
                ))}
              </div>
            </div>
          )}
        </div>
      </Card>

      {/* Capability Stats */}
      <Card>
        <div className="border-b px-6 py-4">
          <h4 className="font-semibold">Capability Statistics</h4>
          <p className="text-sm text-muted-foreground mt-1">
            Shows which IMAP extensions are supported across your email accounts
          </p>
        </div>

        <div className="p-6">
          {stats.length === 0 ? (
            <div className="text-center py-12 text-muted-foreground">
              No capability data available. Click "Refresh All Capabilities" to detect.
            </div>
          ) : (
            <div className="space-y-4">
              {stats.map((stat) => (
                <div key={stat.name} className="border rounded-lg p-4">
                  <div className="flex items-center justify-between mb-2">
                    <div>
                      <div className="font-semibold">{stat.name}</div>
                      <div className="text-sm text-muted-foreground">{stat.description}</div>
                    </div>
                    <div className="text-right">
                      <div className="text-2xl font-bold">{stat.percentage}%</div>
                      <div className="text-sm text-muted-foreground">
                        {stat.supportedCount}/{stat.totalCount} accounts
                      </div>
                    </div>
                  </div>

                  {/* Progress bar */}
                  <div className="h-2 bg-muted rounded-full overflow-hidden">
                    <div
                      className={`h-full ${stat.percentage >= 80 ? 'bg-green-500' : stat.percentage >= 50 ? 'bg-yellow-500' : 'bg-red-500'}`}
                      style={{ width: `${stat.percentage}%` }}
                    />
                  </div>

                  {/* Supported by */}
                  {stat.supportedBy.length > 0 && (
                    <div className="mt-3 pt-3 border-t">
                      <div className="text-xs text-muted-foreground mb-1">Supported by:</div>
                      <div className="flex flex-wrap gap-1">
                        {stat.supportedBy.map(email => (
                          <span key={email} className="px-2 py-0.5 bg-green-100 text-green-800 rounded text-xs">
                            {email}
                          </span>
                        ))}
                      </div>
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
      </Card>

      {/* Accounts without capabilities */}
      {accounts.some(a => !a.server_capabilities) && (
        <Card>
          <div className="border-b px-6 py-4">
            <h4 className="font-semibold">Accounts Pending Detection</h4>
          </div>
          <div className="p-6">
            <div className="flex flex-col gap-2">
              {accounts.filter(a => !a.server_capabilities).map(account => (
                <div key={account.id} className="flex items-center justify-between p-3 bg-muted/50 rounded-lg">
                  <div>
                    <div className="font-medium">{account.email}</div>
                    <div className="text-sm text-muted-foreground">Capabilities not yet detected</div>
                  </div>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => refreshCapabilities(account.id)}
                  >
                    Detect Now
                  </Button>
                </div>
              ))}
            </div>
          </div>
        </Card>
      )}
    </div>
  );
}
