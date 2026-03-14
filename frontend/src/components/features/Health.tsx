import { useState, useEffect } from 'react';
import { Card, StatusBadge } from '../ui';
import type { SystemHealthResponse, AccountStats } from '../../types';
import * as api from '../../services/api';
import './Health.css';

export function HealthView() {
  const [health, setHealth] = useState<SystemHealthResponse | null>(null);
  const [accountStats, setAccountStats] = useState<AccountStats[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    loadHealth();
  }, []);

  const loadHealth = async () => {
    try {
      setLoading(true);
      const [healthData, accounts] = await Promise.all([
        api.getSystemHealth(),
        api.listAccounts(),
      ]);
      setHealth(healthData);

      const stats = await Promise.all(
        accounts.map((a) => api.getAccountStats(a.id).catch(() => null))
      );
      setAccountStats(stats.filter(Boolean) as AccountStats[]);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load health data');
    } finally {
      setLoading(false);
    }
  };

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

  if (loading) {
    return (
      <div className="health-view">
        <div className="empty-state">Loading system health...</div>
      </div>
    );
  }

  return (
    <div className="health-view">
      {error && (
        <Card>
          <div className="form-error">{error}</div>
        </Card>
      )}

      {health && (
        <Card title="System Health">
          <div className="health-grid">
            <div className="health-card main">
              <div className="health-value">{health.score}</div>
              <div className="health-label">Overall Score</div>
              <div className="health-status">
                <StatusBadge
                  status={getHealthStatus(health.status) as 'success' | 'warning' | 'error'}
                  label={health.status.charAt(0).toUpperCase() + health.status.slice(1)}
                />
              </div>
            </div>

            {Object.entries(health.components).map(([name, component]) => (
              <div key={name} className="health-card">
                <div className="health-label">{name.charAt(0).toUpperCase() + name.slice(1)}</div>
                <div className="health-status-small">
                  <StatusBadge
                    status={getHealthStatus(component.status) as 'success' | 'warning' | 'error'}
                    label={component.status}
                    showDot={false}
                  />
                </div>
              </div>
            ))}
          </div>
        </Card>
      )}

      <Card title="Account Status">
        {accountStats.length === 0 ? (
          <div className="empty-state">
            <p>No accounts configured</p>
          </div>
        ) : (
          <div className="account-stats-list">
            {accountStats.map((stats) => (
              <div key={stats.account_id} className="account-stat-item">
                <div className="stat-info">
                  <div className="stat-email">{stats.email}</div>
                  <div className="stat-meta">
                    IMAP: {stats.imap_host} | SMTP: {stats.smtp_host}
                  </div>
                </div>
                <div className="stat-status">
                  <StatusBadge
                    status={stats.connection_status === 'connected' ? 'success' : 'error'}
                    label={stats.connection_status}
                  />
                </div>
              </div>
            ))}
          </div>
        )}
      </Card>

      <Card>
        <div className="health-actions">
          <button className="btn btn-secondary" onClick={loadHealth}>
            <svg width="16" height="16" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
            Refresh
          </button>
        </div>
      </Card>
    </div>
  );
}
