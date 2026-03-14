import React, { useState } from 'react';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Label } from '@/components/ui/label';
import type { EmailAddress } from '@/types';
import * as api from '@/services/api';
import './Compose.css';

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
    <div className="compose-view">
      <Card>
        <div className="card-header">
          <div className="card-title-wrapper">
            <h3 className="card-title">Compose Email</h3>
          </div>
        </div>
        <div className="card-content">
          <form onSubmit={handleSubmit} className="compose-form">
            <div className="form-group">
              <Label htmlFor="fromAccount">From</Label>
              <select
                id="fromAccount"
                className="input"
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

            <div className="form-group">
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

            <div className="form-row">
              <div className="form-group">
                <Label htmlFor="cc">Cc</Label>
                <Input
                  id="cc"
                  type="email"
                  placeholder="cc@example.com"
                  value={form.cc}
                  onChange={(e) => setForm({ ...form, cc: e.target.value })}
                />
              </div>
              <div className="form-group">
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

            <div className="form-group">
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

            <div className="form-group">
              <div className="compose-toolbar">
                <Label>Body</Label>
                <div className="compose-mode-toggle">
                  <button
                    type="button"
                    className={`mode-btn ${!useHtml ? 'active' : ''}`}
                    onClick={() => setUseHtml(false)}
                  >
                    Plain Text
                  </button>
                  <button
                    type="button"
                    className={`mode-btn ${useHtml ? 'active' : ''}`}
                    onClick={() => setUseHtml(true)}
                  >
                    HTML
                  </button>
                </div>
              </div>
              {useHtml ? (
                <textarea
                  className="textarea html-editor"
                  placeholder="Enter HTML content..."
                  value={form.htmlBody}
                  onChange={(e) => setForm({ ...form, htmlBody: e.target.value })}
                  rows={15}
                />
              ) : (
                <textarea
                  className="textarea"
                  placeholder="Enter your message..."
                  value={form.textBody}
                  onChange={(e) => setForm({ ...form, textBody: e.target.value })}
                  rows={15}
                />
              )}
            </div>

            {error && <div className="form-error">{error}</div>}
            {success && <div className="form-success">{success}</div>}

            <div className="compose-actions">
              <Button type="submit" variant="default" disabled={sending}>
                <svg width="16" height="16" fill="none" stroke="currentColor" viewBox="0 0 24 24">
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
