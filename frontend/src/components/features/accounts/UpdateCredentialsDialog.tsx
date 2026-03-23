import { useState, useEffect } from 'react';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Label } from '@/components/ui/label';
import type { Account } from '@/types';
import * as api from '@/services/api';

interface UpdateCredentialsDialogProps {
  account: Account | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSuccess: () => void;
}

export function UpdateCredentialsDialog({
  account,
  open,
  onOpenChange,
  onSuccess,
}: UpdateCredentialsDialogProps) {
  const [loading, setLoading] = useState(false);
  const [formData, setFormData] = useState({
    email: '',
    password: '',
    imap_host: '',
    imap_port: '',
    imap_encryption: 'ssl',
    smtp_host: '',
    smtp_port: '',
    smtp_encryption: 'ssl',
  });

  useEffect(() => {
    if (account) {
      setFormData({
        email: account.email,
        password: '',
        imap_host: account.imap_config.host,
        imap_port: account.imap_config.port.toString(),
        imap_encryption: account.imap_config.encryption,
        smtp_host: account.smtp_config.host,
        smtp_port: account.smtp_config.port.toString(),
        smtp_encryption: account.smtp_config.encryption,
      });
    }
  }, [account]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!account) return;
    setLoading(true);

    try {
      const updates: Record<string, unknown> = {
        email: formData.email,
        imap_host: formData.imap_host,
        imap_port: parseInt(formData.imap_port, 10),
        imap_encryption: formData.imap_encryption,
        smtp_host: formData.smtp_host,
        smtp_port: parseInt(formData.smtp_port, 10),
        smtp_encryption: formData.smtp_encryption,
      };

      // Only include password if it's provided
      if (formData.password) {
        updates.password = formData.password;
      }

      await api.updateAccount(account.id, updates);

      onSuccess();
      onOpenChange(false);
    } catch (err: any) {
      // Error is handled by the caller
      throw err;
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Update Email Credentials</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit}>
          <div className="space-y-6 py-4">
            {/* Account Information */}
            <div className="space-y-4">
              <h4 className="text-sm font-semibold text-muted-foreground">Account Information</h4>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <Label htmlFor="email">Email Address</Label>
                  <Input
                    id="email"
                    type="email"
                    required
                    value={formData.email}
                    onChange={(e) => setFormData({ ...formData, email: e.target.value })}
                    placeholder="you@example.com"
                  />
                </div>
                <div>
                  <Label htmlFor="password">
                    Password {account?.status === 'disabled' && '(Required to re-enable)'}
                  </Label>
                  <Input
                    id="password"
                    type="password"
                    required={account?.status === 'disabled'}
                    placeholder={account?.status === 'disabled' ? 'Enter password to re-enable' : '••••••••'}
                    value={formData.password}
                    onChange={(e) => setFormData({ ...formData, password: e.target.value })}
                  />
                </div>
              </div>
            </div>

            {/* IMAP Settings */}
            <div className="space-y-4 border-t pt-4">
              <h4 className="text-sm font-semibold text-muted-foreground">IMAP Settings</h4>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <Label htmlFor="imap_host">IMAP Host</Label>
                  <Input
                    id="imap_host"
                    type="text"
                    value={formData.imap_host}
                    onChange={(e) => setFormData({ ...formData, imap_host: e.target.value })}
                    placeholder="imap.example.com"
                  />
                </div>
                <div>
                  <Label htmlFor="imap_port">IMAP Port</Label>
                  <Input
                    id="imap_port"
                    type="number"
                    value={formData.imap_port}
                    onChange={(e) => setFormData({ ...formData, imap_port: e.target.value })}
                    placeholder="993"
                  />
                </div>
              </div>
              <div>
                <Label htmlFor="imap_encryption">IMAP Encryption</Label>
                <select
                  id="imap_encryption"
                  value={formData.imap_encryption}
                  onChange={(e) => setFormData({ ...formData, imap_encryption: e.target.value as 'ssl' | 'starttls' | 'tls' | 'none' })}
                  className="flex h-9 w-full rounded-lg border border-input bg-transparent px-3 py-1 text-sm shadow-sm transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                >
                  <option value="ssl">SSL/TLS</option>
                  <option value="starttls">STARTTLS</option>
                  <option value="tls">TLS</option>
                  <option value="none">None</option>
                </select>
              </div>
            </div>

            {/* SMTP Settings */}
            <div className="space-y-4 border-t pt-4">
              <h4 className="text-sm font-semibold text-muted-foreground">SMTP Settings</h4>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <Label htmlFor="smtp_host">SMTP Host</Label>
                  <Input
                    id="smtp_host"
                    type="text"
                    value={formData.smtp_host}
                    onChange={(e) => setFormData({ ...formData, smtp_host: e.target.value })}
                    placeholder="smtp.example.com"
                  />
                </div>
                <div>
                  <Label htmlFor="smtp_port">SMTP Port</Label>
                  <Input
                    id="smtp_port"
                    type="number"
                    value={formData.smtp_port}
                    onChange={(e) => setFormData({ ...formData, smtp_port: e.target.value })}
                    placeholder="587"
                  />
                </div>
              </div>
              <div>
                <Label htmlFor="smtp_encryption">SMTP Encryption</Label>
                <select
                  id="smtp_encryption"
                  value={formData.smtp_encryption}
                  onChange={(e) => setFormData({ ...formData, smtp_encryption: e.target.value as 'ssl' | 'starttls' | 'tls' | 'none' })}
                  className="flex h-9 w-full rounded-lg border border-input bg-transparent px-3 py-1 text-sm shadow-sm transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                >
                  <option value="starttls">STARTTLS</option>
                  <option value="ssl">SSL/TLS</option>
                  <option value="tls">TLS</option>
                  <option value="none">None</option>
                </select>
              </div>
            </div>
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>
              Cancel
            </Button>
            <Button type="submit" disabled={loading}>
              {loading ? 'Updating...' : 'Update Credentials'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
