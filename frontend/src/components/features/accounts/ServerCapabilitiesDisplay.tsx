import { useState, useCallback } from 'react';
import { Button } from '@/components/ui/Button';
import { getServerCapabilities, refreshServerCapabilities } from '@/services/api';
import type { ServerCapabilities } from '@/types';

interface ServerCapabilitiesProps {
  accountId: string;
  initialCapabilities?: ServerCapabilities;
}

const CAPABILITY_ITEMS: Array<{ key: keyof ServerCapabilities; label: string; description: string }> = [
  { key: 'supports_qresync', label: 'QRESYNC', description: 'Fast resynchronization with VANISHED support (RFC 7162)' },
  { key: 'supports_condstore', label: 'CONDSTORE', description: 'Conditional STORE for efficient flag synchronization (RFC 7162)' },
  { key: 'supports_sort', label: 'SORT', description: 'Server-side message sorting (RFC 5256)' },
  { key: 'supports_search_res', label: 'SEARCHRES', description: 'Search result resynchronization (RFC 7162)' },
  { key: 'supports_literal_plus', label: 'LITERAL+', description: 'Non-synchronizing literals for better performance (RFC 7888)' },
  { key: 'supports_utf8_accept', label: 'UTF8=ACCEPT', description: 'UTF-8 support in protocol (RFC 6855)' },
  { key: 'supports_utf8_only', label: 'UTF8=ONLY', description: 'UTF-8 only mode (RFC 6855)' },
  { key: 'supports_move', label: 'MOVE', description: 'Atomic message move between folders (RFC 6851)' },
  { key: 'supports_uid_plus', label: 'UIDPLUS', description: 'Extended UID operations (RFC 4315)' },
  { key: 'supports_unselect', label: 'UNSELECT', description: 'Close folder without selecting another (RFC 3691)' },
  { key: 'supports_idle', label: 'IDLE', description: 'Real-time push notifications (RFC 2177)' },
  { key: 'supports_starttls', label: 'STARTTLS', description: 'Upgrade plain connection to TLS (RFC 2595)' },
  { key: 'supports_auth_plain', label: 'AUTH=PLAIN', description: 'PLAIN authentication mechanism' },
  { key: 'supports_auth_login', label: 'AUTH=LOGIN', description: 'LOGIN authentication mechanism' },
  { key: 'supports_auth_oauth2', label: 'AUTH=OAUTH2', description: 'OAuth2 authentication (RFC 7628)' },
];

export function ServerCapabilitiesDisplay({ accountId, initialCapabilities }: ServerCapabilitiesProps) {
  const [capabilities, setCapabilities] = useState<ServerCapabilities | undefined>(initialCapabilities);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [expanded, setExpanded] = useState(false);

  const fetchCapabilities = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await getServerCapabilities(accountId);
      setCapabilities(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load server capabilities');
    } finally {
      setLoading(false);
    }
  }, [accountId]);

  const handleRefresh = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await refreshServerCapabilities(accountId);
      setCapabilities(data.capabilities);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to refresh server capabilities');
    } finally {
      setLoading(false);
    }
  }, [accountId]);

  const formatLastChecked = (dateString: string) => {
    const date = new Date(dateString);
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffMins = Math.floor(diffMs / 60000);
    const diffHours = Math.floor(diffMins / 60);
    const diffDays = Math.floor(diffHours / 24);

    if (diffMins < 1) return 'Just now';
    if (diffMins < 60) return `${diffMins}m ago`;
    if (diffHours < 24) return `${diffHours}h ago`;
    return `${diffDays}d ago`;
  };

  const supportedCount = capabilities
    ? CAPABILITY_ITEMS.filter(item => capabilities[item.key] === true).length
    : 0;

  return (
    <div className="border rounded-lg p-4 bg-muted/20">
      <div className="flex items-center justify-between mb-3">
        <div>
          <h3 className="text-sm font-semibold">IMAP Server Capabilities</h3>
          {capabilities && (
            <p className="text-xs text-muted-foreground mt-0.5">
              {capabilities.server_vendor || capabilities.server_name || 'Unknown server'}
              {capabilities.server_version && ` v${capabilities.server_version}`}
              {' • '}Last checked: {formatLastChecked(capabilities.last_checked)}
            </p>
          )}
        </div>
        <div className="flex items-center gap-2">
          <span className="text-xs text-muted-foreground">
            {supportedCount}/{CAPABILITY_ITEMS.length} supported
          </span>
          <Button
            variant="outline"
            size="sm"
            onClick={handleRefresh}
            disabled={loading}
            className="h-7 text-xs"
          >
            {loading ? (
              <svg className="h-3 w-3 animate-spin" fill="none" viewBox="0 0 24 24">
                <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
              </svg>
            ) : (
              <svg className="h-3 w-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
              </svg>
            )}
            Refresh
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setExpanded(!expanded)}
            className="h-7 text-xs"
          >
            {expanded ? 'Collapse' : 'Expand'}
          </Button>
        </div>
      </div>

      {error && (
        <div className="mb-3 p-2 bg-destructive/10 border border-destructive/20 rounded text-xs text-destructive">
          {error}
        </div>
      )}

      {!capabilities && !loading && (
        <div className="text-center py-4 text-muted-foreground text-sm">
          <Button variant="outline" size="sm" onClick={fetchCapabilities}>
            Load Capabilities
          </Button>
        </div>
      )}

      {loading && !capabilities && (
        <div className="text-center py-4 text-muted-foreground text-sm">
          Loading server capabilities...
        </div>
      )}

      {capabilities && (
        <>
          {/* Quick summary */}
          <div className="flex flex-wrap gap-2 mb-3">
            {capabilities.supports_qresync && (
              <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-green-100 text-green-800">
                ✓ QRESYNC
              </span>
            )}
            {capabilities.supports_condstore && !capabilities.supports_qresync && (
              <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-green-100 text-green-800">
                ✓ CONDSTORE
              </span>
            )}
            {capabilities.supports_idle && (
              <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-blue-100 text-blue-800">
                ✓ IDLE
              </span>
            )}
            {!capabilities.supports_qresync && !capabilities.supports_condstore && (
              <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-yellow-100 text-yellow-800">
                ⚠ Basic IMAP
              </span>
            )}
          </div>

          {/* Expanded view */}
          {expanded && (
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-2 mt-3 pt-3 border-t">
              {CAPABILITY_ITEMS.map((item) => {
                const value = capabilities[item.key];
                return (
                  <div
                    key={item.key}
                    className={`p-2 rounded text-xs ${
                      value === true
                        ? 'bg-green-50 border border-green-200'
                        : 'bg-gray-50 border border-gray-200 opacity-60'
                    }`}
                  >
                    <div className="flex items-center gap-1.5 font-medium">
                      {value === true ? (
                        <svg className="h-3.5 w-3.5 text-green-600" fill="currentColor" viewBox="0 0 20 20">
                          <path fillRule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z" clipRule="evenodd" />
                        </svg>
                      ) : (
                        <svg className="h-3.5 w-3.5 text-gray-400" fill="currentColor" viewBox="0 0 20 20">
                          <path fillRule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z" clipRule="evenodd" />
                        </svg>
                      )}
                      <span className={value === true ? 'text-green-800' : 'text-gray-600'}>
                        {item.label}
                      </span>
                    </div>
                    <p className="mt-1 text-gray-500">{item.description}</p>
                  </div>
                );
              })}
            </div>
          )}

          {/* Raw capabilities */}
          {expanded && capabilities.capabilities && capabilities.capabilities.length > 0 && (
            <div className="mt-3 pt-3 border-t">
              <p className="text-xs font-medium text-muted-foreground mb-1">Raw capabilities:</p>
              <code className="text-xs bg-gray-100 px-2 py-1 rounded block overflow-x-auto">
                {capabilities.capabilities.join(' ')}
              </code>
            </div>
          )}
        </>
      )}
    </div>
  );
}
