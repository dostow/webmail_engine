import React, { useState } from 'react';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Label } from '@/components/ui/label';
import type { EmailAddress } from '@/types';
import * as api from '@/services/api';

interface ComposeViewProps {
  accountId?: string;
  onSent?: () => void;
}

interface ComposeForm {
  to: string;
  cc: string;
  bcc: string;
  subject: string;
  textBody: string;
  htmlBody: string;
}

const defaultForm: ComposeForm = {
  to: '',
  cc: '',
  bcc: '',
  subject: '',
  textBody: '',
  htmlBody: '',
};

export function ComposeView({ accountId, onSent }: ComposeViewProps) {
  const [accounts, setAccounts] = useState<{ id: string; email: string }[]>([]);
  const [selectedAccountId, setSelectedAccountId] = useState<string>(accountId || '');
  const [form, setForm] = useState<ComposeForm>(defaultForm);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  const [useHtml, setUseHtml] = useState(false);

  const loadAccounts = async () => {
    try {
      const data = await api.listAccounts();
      setAccounts(data.map((a) => ({ id: a.id, email: a.email })));
      if (!accountId && data.length > 0) {
        setSelectedAccountId(data[0].id);
      }
    } catch (err) {
      console.error('Failed to load accounts:', err);
    }
  };

  React.useEffect(() => {
    loadAccounts();
  }, []);

  React.useEffect(() => {
    if (accountId) {
      setSelectedAccountId(accountId);
    }
  }, [accountId]);

  const parseEmails = (input: string): EmailAddress[] => {
    return input
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean)
      .map((email) => ({ name: '', address: email }));
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedAccountId) {
      setError('Please select an account');
      return;
    }

    setSending(true);
    setError(null);
    setSuccess(null);

    try {
      await api.sendEmail(selectedAccountId, {
        to: parseEmails(form.to),
        cc: form.cc ? parseEmails(form.cc) : undefined,
        bcc: form.bcc ? parseEmails(form.bcc) : undefined,
        subject: form.subject,
        text_body: form.textBody,
        html_body: useHtml ? form.htmlBody : undefined,
      });

      setSuccess('Email sent successfully!');
      setForm(defaultForm);
      onSent?.();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to send email');
    } finally {
      setSending(false);
    }
  };

  return (
    <div className="max-w-[800px]">
      <Card>
        <div className="flex items-center justify-between border-b px-6 py-4">
          <h3 className="text-lg font-semibold">Compose Email</h3>
        </div>
        <div className="p-6">
          <form onSubmit={handleSubmit} className="flex flex-col gap-4">
            <div>
              <Label htmlFor="fromAccount">From</Label>
              <select
                id="fromAccount"
                className="flex h-8 w-full min-w-0 rounded-lg border border-input bg-transparent px-2.5 py-1 text-base transition-colors outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 md:text-sm"
                value={selectedAccountId}
                onChange={(e) => setSelectedAccountId(e.target.value)}
              >
                {accounts.map((account) => (
                  <option key={account.id} value={account.id}>
                    {account.email}
                  </option>
                ))}
              </select>
            </div>

            <div>
              <Label htmlFor="to">To</Label>
              <Input
                id="to"
                type="email"
                placeholder="recipient@example.com"
                value={form.to}
                onChange={(e) => setForm({ ...form, to: e.target.value })}
                required
              />
            </div>

            <div className="grid grid-cols-2 gap-4">
              <div>
                <Label htmlFor="cc">Cc</Label>
                <Input
                  id="cc"
                  type="email"
                  placeholder="cc@example.com"
                  value={form.cc}
                  onChange={(e) => setForm({ ...form, cc: e.target.value })}
                />
              </div>
              <div>
                <Label htmlFor="bcc">Bcc</Label>
                <Input
                  id="bcc"
                  type="email"
                  placeholder="bcc@example.com"
                  value={form.bcc}
                  onChange={(e) => setForm({ ...form, bcc: e.target.value })}
                />
              </div>
            </div>

            <div>
              <Label htmlFor="subject">Subject</Label>
              <Input
                id="subject"
                type="text"
                placeholder="Email subject"
                value={form.subject}
                onChange={(e) => setForm({ ...form, subject: e.target.value })}
                required
              />
            </div>

            <div>
              <div className="flex items-center justify-between mb-2">
                <Label>Body</Label>
                <div className="flex gap-2">
                  <button
                    type="button"
                    className={`px-3 py-1.5 text-xs rounded-md border transition-all ${!useHtml
                        ? 'bg-primary text-primary-foreground border-primary'
                        : 'bg-muted text-muted-foreground border-border hover:bg-muted/80'
                      }`}
                    onClick={() => setUseHtml(false)}
                  >
                    Plain Text
                  </button>
                  <button
                    type="button"
                    className={`px-3 py-1.5 text-xs rounded-md border transition-all ${useHtml
                        ? 'bg-primary text-primary-foreground border-primary'
                        : 'bg-muted text-muted-foreground border-border hover:bg-muted/80'
                      }`}
                    onClick={() => setUseHtml(true)}
                  >
                    HTML
                  </button>
                </div>
              </div>
              {useHtml ? (
                <textarea
                  className="w-full min-h-[200px] rounded-lg border border-input bg-transparent px-3 py-2 text-sm font-mono outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
                  placeholder="Enter HTML content..."
                  value={form.htmlBody}
                  onChange={(e) => setForm({ ...form, htmlBody: e.target.value })}
                  rows={15}
                />
              ) : (
                <textarea
                  className="w-full min-h-[200px] rounded-lg border border-input bg-transparent px-3 py-2 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
                  placeholder="Enter your message..."
                  value={form.textBody}
                  onChange={(e) => setForm({ ...form, textBody: e.target.value })}
                  rows={15}
                />
              )}
            </div>

            {error && (
              <div className="rounded-lg border border-destructive bg-destructive/10 px-4 py-3 text-destructive">
                {error}
              </div>
            )}
            {success && (
              <div className="rounded-lg border border-green-600 bg-green-600/10 px-4 py-3 text-green-600">
                {success}
              </div>
            )}

            <div className="flex gap-4 mt-4">
              <Button type="submit" variant="default" disabled={sending}>
                <svg className="mr-2 size-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 19l9 2-9-18-9 18 9-2zm0 0v-8" />
                </svg>
                {sending ? 'Sending...' : 'Send Email'}
              </Button>
              <Button
                type="button"
                variant="outline"
                onClick={() => setForm(defaultForm)}
              >
                Clear
              </Button>
            </div>
          </form>
        </div>
      </Card>
    </div>
  );
}
