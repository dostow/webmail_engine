import { useLoaderData, useNavigation } from 'react-router-dom';
import { Card, StatusBadge, Button } from '../ui';
import type { SystemHealthResponse, AccountStats } from '../../types';

interface HealthData {
  health: SystemHealthResponse;
  accountStats: AccountStats[];
}

export function HealthView() {
  const { health, accountStats } = useLoaderData() as HealthData;
  const navigation = useNavigation();

  const loading = navigation.state === 'loading';

  const getHealthStatus = (status: string) => {
    switch (status) {
      case 'healthy':
        return 'success';
      case 'degraded':
        return 'warning';
      default:
        return 'error';
    }
  };

  return (
    <div className="flex flex-col gap-6">
      {health && (
        <Card>
          <div className="flex items-center justify-between border-b px-6 py-4">
            <h3 className="text-lg font-semibold">System Health</h3>
          </div>
          <div className="p-6">
            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
              <div className="rounded-lg bg-gradient-to-br from-primary to-primary/80 p-6 text-center text-primary-foreground">
                <div className="text-4xl font-bold">{health.score}</div>
                <div className="mt-1 text-sm opacity-80">Overall Score</div>
                <div className="mt-2">
                  <StatusBadge
                    status={getHealthStatus(health.status) as 'success' | 'warning' | 'error'}
                    label={health.status.charAt(0).toUpperCase() + health.status.slice(1)}
                  />
                </div>
              </div>

              {Object.entries(health.components).map(([name, component]) => (
                <div key={name} className="rounded-lg border bg-muted/50 p-6 text-center">
                  <div className="text-sm font-medium text-muted-foreground">
                    {name.charAt(0).toUpperCase() + name.slice(1)}
                  </div>
                  <div className="mt-2">
                    <StatusBadge
                      status={getHealthStatus(component.status) as 'success' | 'warning' | 'error'}
                      label={component.status}
                      showDot={false}
                    />
                  </div>
                </div>
              ))}
            </div>
          </div>
        </Card>
      )}

      <Card>
        <div className="flex items-center justify-between border-b px-6 py-4">
          <h3 className="text-lg font-semibold">Account Status</h3>
        </div>
        <div className="p-6">
          {accountStats.length === 0 ? (
            <div className="py-12 text-center text-muted-foreground">
              <p>No accounts configured</p>
            </div>
          ) : (
            <div className="flex flex-col gap-3">
              {accountStats.map((stats) => (
                <div
                  key={stats.account_id}
                  className="flex items-center justify-between rounded-lg border bg-muted/50 px-4 py-4"
                >
                  <div className="flex-1">
                    <div className="font-semibold">{stats.email}</div>
                    <div className="text-sm text-muted-foreground">
                      IMAP: {stats.imap_host} | SMTP: {stats.smtp_host}
                    </div>
                  </div>
                  <div>
                    <StatusBadge
                      status={stats.connection_status === 'connected' ? 'success' : 'error'}
                      label={stats.connection_status}
                    />
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </Card>

      <Card>
        <div className="flex items-center justify-end border-b px-6 py-4">
          <Button variant="outline" onClick={() => window.location.reload()} disabled={loading}>
            <svg className="mr-2 size-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
            Refresh
          </Button>
        </div>
      </Card>
    </div>
  );
}
