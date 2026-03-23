import { useState } from 'react';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '@/components/ui/dialog';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Label } from '@/components/ui/label';
import { toast } from 'sonner';
import { createAccount } from '@/services/api';
import type { AddAccountRequest, APIError } from '@/types';

interface AccountWizardProps {
  onComplete: () => void;
  onCancel: () => void;
}

const DEFAULT_FORM = {
  email: '',
  password: '',
  imap_host: '',
  imap_port: '993',
  imap_encryption: 'ssl',
  smtp_host: '',
  smtp_port: '587',
  smtp_encryption: 'starttls',
};

export function AccountWizard({ onComplete, onCancel }: AccountWizardProps) {
  const [form, setForm] = useState(DEFAULT_FORM);
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitting(true);

    try {
      const request: AddAccountRequest = {
        email: form.email,
        password: form.password,
        imap_host: form.imap_host,
        imap_port: parseInt(form.imap_port, 10),
        imap_encryption: form.imap_encryption,
        smtp_host: form.smtp_host,
        smtp_port: parseInt(form.smtp_port, 10),
        smtp_encryption: form.smtp_encryption,
      };

      await createAccount(request);

      toast.success('Account added successfully');
      onComplete();
    } catch (error) {
      console.error('Error adding account:', error);
      if (error instanceof Error) {
        // Check if it's an API error with specific structure
        if ('code' in error || 'details' in error) {
          // This is likely an ApiError from our service
          const apiError = error as APIError; // Type assertion to access custom properties
          toast.error(apiError.message || 'Failed to add account');
        } else {
          toast.error(error.message || 'Failed to add account');
        }
      } else {
        toast.error('Failed to add account');
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Dialog open={true} onOpenChange={(open) => !open && onCancel()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="text-lg">Connect New Account</DialogTitle>
          <DialogDescription>
            Enter your IMAP and SMTP credentials to sync your mailbox.
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-6 py-4">
          {/* Account Identity */}
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <div className="space-y-2">
              <Label htmlFor="email">Email Address</Label>
              <Input
                id="email"
                type="email"
                required
                placeholder="you@example.com"
                value={form.email}
                onChange={(e) => setForm({ ...form, email: e.target.value })}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="password">App Password</Label>
              <Input
                id="password"
                type="password"
                required
                placeholder="••••••••••••"
                value={form.password}
                onChange={(e) => setForm({ ...form, password: e.target.value })}
              />
            </div>
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-6">
            {/* IMAP Config */}
            <div className="space-y-4 rounded-lg border bg-muted/30 p-4">
              <div className="flex items-center gap-2 text-xs font-bold uppercase tracking-wider text-muted-foreground">
                <span>🌐</span> Incoming (IMAP)
              </div>
              <div className="space-y-3">
                <div className="space-y-1">
                  <Label htmlFor="imap_host" className="text-[10px] uppercase">Host</Label>
                  <Input
                    id="imap_host"
                    placeholder="imap.gmail.com"
                    value={form.imap_host}
                    onChange={(e) => setForm({ ...form, imap_host: e.target.value })}
                  />
                </div>
                <div className="grid grid-cols-2 gap-2">
                  <div className="space-y-1">
                    <Label htmlFor="imap_port" className="text-[10px] uppercase">Port</Label>
                    <Input
                      id="imap_port"
                      value={form.imap_port}
                      onChange={(e) => setForm({ ...form, imap_port: e.target.value })}
                    />
                  </div>
                  <div className="space-y-1">
                    <Label className="text-[10px] uppercase">Security</Label>
                    <select
                      className="w-full h-8 rounded-md border border-input bg-background px-2 text-xs"
                      value={form.imap_encryption}
                      onChange={(e) => setForm({ ...form, imap_encryption: e.target.value })}
                    >
                      <option value="ssl">SSL/TLS</option>
                      <option value="starttls">STARTTLS</option>
                    </select>
                  </div>
                </div>
              </div>
            </div>

            {/* SMTP Config */}
            <div className="space-y-4 rounded-lg border bg-muted/30 p-4">
              <div className="flex items-center gap-2 text-xs font-bold uppercase tracking-wider text-muted-foreground">
                <span>📤</span> Outgoing (SMTP)
              </div>
              <div className="space-y-3">
                <div className="space-y-1">
                  <Label htmlFor="smtp_host" className="text-[10px] uppercase">Host</Label>
                  <Input
                    id="smtp_host"
                    placeholder="smtp.gmail.com"
                    value={form.smtp_host}
                    onChange={(e) => setForm({ ...form, smtp_host: e.target.value })}
                  />
                </div>
                <div className="grid grid-cols-2 gap-2">
                  <div className="space-y-1">
                    <Label htmlFor="smtp_port" className="text-[10px] uppercase">Port</Label>
                    <Input
                      id="smtp_port"
                      value={form.smtp_port}
                      onChange={(e) => setForm({ ...form, smtp_port: e.target.value })}
                    />
                  </div>
                  <div className="space-y-1">
                    <Label className="text-[10px] uppercase">Security</Label>
                    <select
                      className="w-full h-8 rounded-md border border-input bg-background px-2 text-xs"
                      value={form.smtp_encryption}
                      onChange={(e) => setForm({ ...form, smtp_encryption: e.target.value })}
                    >
                      <option value="starttls">STARTTLS</option>
                      <option value="ssl">SSL/TLS</option>
                    </select>
                  </div>
                </div>
              </div>
            </div>
          </div>

          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={onCancel}>
              Cancel
            </Button>
            <Button type="submit" disabled={submitting}>
              {submitting ? 'Connecting...' : 'Connect Account'}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
