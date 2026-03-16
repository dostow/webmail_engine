import { useState } from 'react';
import { Button } from '@/components/ui/Button';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from '@/components/ui/dialog';
import { Label } from '@/components/ui/label';
import { Input } from '@/components/ui/Input';
import type { Account, SyncSettings } from '@/types';
import * as api from '@/services/api';
import { toast } from 'sonner';

interface SyncSettingsDialogProps {
  account: Account;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSuccess?: () => void;
}

const SYNC_INTERVAL_OPTIONS = [
  { value: 60, label: '1 minute' },
  { value: 300, label: '5 minutes' },
  { value: 600, label: '10 minutes' },
  { value: 900, label: '15 minutes' },
  { value: 1800, label: '30 minutes' },
  { value: 3600, label: '1 hour' },
  { value: 7200, label: '2 hours' },
  { value: 14400, label: '4 hours' },
  { value: 28800, label: '8 hours' },
  { value: 86400, label: '24 hours' },
];

const HISTORICAL_SCOPE_OPTIONS = [
  { value: 7, label: '7 days' },
  { value: 14, label: '14 days' },
  { value: 30, label: '30 days' },
  { value: 60, label: '60 days' },
  { value: 90, label: '90 days' },
  { value: 180, label: '6 months' },
  { value: 365, label: '1 year' },
  { value: 0, label: 'All messages' },
];

export function SyncSettingsDialog({ account, open, onOpenChange, onSuccess }: SyncSettingsDialogProps) {
  const [loading, setLoading] = useState(false);
  const [settings, setSettings] = useState<SyncSettings>({
    auto_sync: account.sync_settings.auto_sync,
    sync_interval: account.sync_settings.sync_interval || 300,
    historical_scope: account.sync_settings.historical_scope || 30,
    include_spam: account.sync_settings.include_spam || false,
    include_trash: account.sync_settings.include_trash || false,
    max_message_size: account.sync_settings.max_message_size || 10485760,
    attachment_handling: account.sync_settings.attachment_handling || 'inline',
    sync_enabled: account.sync_settings.sync_enabled ?? account.sync_settings.auto_sync,
    fair_use_policy: account.sync_settings.fair_use_policy,
  });

  const handleSave = async () => {
    setLoading(true);
    try {
      await api.updateSyncSettings(account.id, settings);
      toast.success('Sync settings updated');
      onSuccess?.();
      onOpenChange(false);
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to update sync settings';
      toast.error(message);
    } finally {
      setLoading(false);
    }
  };

  const handleReset = () => {
    setSettings({
      auto_sync: account.sync_settings.auto_sync,
      sync_interval: account.sync_settings.sync_interval || 300,
      historical_scope: account.sync_settings.historical_scope || 30,
      include_spam: account.sync_settings.include_spam || false,
      include_trash: account.sync_settings.include_trash || false,
      max_message_size: account.sync_settings.max_message_size || 10485760,
      attachment_handling: account.sync_settings.attachment_handling || 'inline',
      sync_enabled: account.sync_settings.sync_enabled ?? account.sync_settings.auto_sync,
      fair_use_policy: account.sync_settings.fair_use_policy,
    });
  };

  const formatBytes = (bytes: number) => {
    if (bytes === 0) return '0 Bytes';
    const k = 1024;
    const sizes = ['Bytes', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return Math.round(bytes / Math.pow(k, i) * 100) / 100 + ' ' + sizes[i];
  };

  const parseBytes = (value: string): number => {
    const num = parseFloat(value);
    if (isNaN(num)) return 0;
    if (value.includes('GB')) return num * 1024 * 1024 * 1024;
    if (value.includes('MB')) return num * 1024 * 1024;
    if (value.includes('KB')) return num * 1024;
    return num;
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Sync Settings</DialogTitle>
          <DialogDescription>
            Configure how {account.email} synchronizes with the email server
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-6 py-4">
          {/* Auto Sync Toggle */}
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label htmlFor="auto_sync" className="text-base font-semibold">
                Enable Background Sync
              </Label>
              <button
                id="auto_sync"
                type="button"
                role="switch"
                aria-checked={settings.auto_sync}
                onClick={() => setSettings(s => ({ ...s, auto_sync: !s.auto_sync, sync_enabled: !s.auto_sync }))}
                className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 ${
                  settings.auto_sync ? 'bg-primary' : 'bg-muted'
                }`}
              >
                <span
                  className={`pointer-events-none block h-5 w-5 rounded-full bg-background shadow-lg ring-0 transition-transform ${
                    settings.auto_sync ? 'translate-x-5' : 'translate-x-0'
                  }`}
                />
              </button>
            </div>
            <p className="text-sm text-muted-foreground">
              When enabled, new messages will be automatically fetched from the server at regular intervals
            </p>
          </div>

          {/* Sync Interval */}
          <div className="space-y-2">
            <Label htmlFor="sync_interval">Sync Interval</Label>
            <select
              id="sync_interval"
              value={settings.sync_interval}
              onChange={(e) => setSettings(s => ({ ...s, sync_interval: parseInt(e.target.value) }))}
              disabled={!settings.auto_sync}
              className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {SYNC_INTERVAL_OPTIONS.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
            <p className="text-sm text-muted-foreground">
              How often to check for new messages. Minimum interval is 5 minutes.
            </p>
          </div>

          {/* Historical Scope */}
          <div className="space-y-2">
            <Label htmlFor="historical_scope">Historical Sync Scope</Label>
            <select
              id="historical_scope"
              value={settings.historical_scope}
              onChange={(e) => setSettings(s => ({ ...s, historical_scope: parseInt(e.target.value) }))}
              className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
            >
              {HISTORICAL_SCOPE_OPTIONS.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
            <p className="text-sm text-muted-foreground">
              How far back to sync messages when the account is first added or reset.
            </p>
          </div>

          {/* Folder Options */}
          <div className="space-y-3">
            <Label>Folders to Sync</Label>
            <div className="space-y-2">
              <div className="flex items-center space-x-2">
                <button
                  type="button"
                  role="checkbox"
                  aria-checked={settings.include_spam}
                  onClick={() => setSettings(s => ({ ...s, include_spam: !s.include_spam }))}
                  className={`h-4 w-4 rounded border transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${
                    settings.include_spam ? 'bg-primary border-primary' : 'border-input'
                  }`}
                >
                  {settings.include_spam && (
                    <svg className="h-4 w-4 text-primary-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="3" d="M5 13l4 4L19 7" />
                    </svg>
                  )}
                </button>
                <Label htmlFor="include_spam" className="font-normal cursor-pointer">
                  Include Spam/Junk folder
                </Label>
              </div>
              <div className="flex items-center space-x-2">
                <button
                  type="button"
                  role="checkbox"
                  aria-checked={settings.include_trash}
                  onClick={() => setSettings(s => ({ ...s, include_trash: !s.include_trash }))}
                  className={`h-4 w-4 rounded border transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${
                    settings.include_trash ? 'bg-primary border-primary' : 'border-input'
                  }`}
                >
                  {settings.include_trash && (
                    <svg className="h-4 w-4 text-primary-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="3" d="M5 13l4 4L19 7" />
                    </svg>
                  )}
                </button>
                <Label htmlFor="include_trash" className="font-normal cursor-pointer">
                  Include Trash folder
                </Label>
              </div>
            </div>
          </div>

          {/* Max Message Size */}
          <div className="space-y-2">
            <Label htmlFor="max_message_size">Max Message Size</Label>
            <div className="flex items-center gap-2">
              <Input
                id="max_message_size"
                type="text"
                value={formatBytes(settings.max_message_size)}
                onChange={(e) => setSettings(s => ({ ...s, max_message_size: parseBytes(e.target.value) }))}
                className="flex-1"
              />
            </div>
            <p className="text-sm text-muted-foreground">
              Messages larger than this size will be skipped during sync.
            </p>
          </div>

          {/* Status Info */}
          <div className="rounded-lg bg-muted p-4 space-y-2">
            <div className="flex items-center gap-2">
              <svg className="h-5 w-5 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
              <span className="text-sm font-medium">Sync Status</span>
            </div>
            <div className="text-sm text-muted-foreground ml-7 space-y-1">
              <p>Last sync: {account.last_sync_at ? new Date(account.last_sync_at).toLocaleString() : 'Never'}</p>
              <p>Current status: <span className="font-medium capitalize">{account.status}</span></p>
            </div>
          </div>
        </div>

        <DialogFooter className="gap-2">
          <Button variant="outline" onClick={handleReset} disabled={loading}>
            Reset
          </Button>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>
            Cancel
          </Button>
          <Button onClick={handleSave} disabled={loading}>
            {loading ? 'Saving...' : 'Save Changes'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
