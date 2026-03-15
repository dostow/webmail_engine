import { useState } from 'react';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Label } from '@/components/ui/label';
import { Separator } from '@/components/ui/separator';
import { useTriageStore } from './useTriageStore';
import { useEmailToast } from '@/hooks/useToast';
import * as api from '@/services/api';
import type { Account } from '@/types';

interface ComposePaneProps {
  accounts: Account[];
}

export function ComposePane({ accounts }: ComposePaneProps) {
  const { composeOptions, selectedAccountId, clearPane } = useTriageStore();
  const { showSuccess, showError } = useEmailToast();

  const [sending, setSending] = useState(false);
  const [useHtml, setUseHtml] = useState(false);
  const [form, setForm] = useState({
    accountId: selectedAccountId || accounts[0]?.id || '',
    to: composeOptions?.to || '',
    cc: '',
    bcc: '',
    subject: composeOptions?.subject || '',
    textBody: composeOptions?.body || '',
    htmlBody: '',
  });

  const handleSend = async () => {
    if (!form.accountId || !form.to) {
      showError('Please fill in the required fields (From and To).');
      return;
    }
    setSending(true);
    try {
      await api.sendEmail(form.accountId, {
        to: form.to.split(',').map((e) => ({ name: '', address: e.trim() })),
        cc: form.cc ? form.cc.split(',').map((e) => ({ name: '', address: e.trim() })) : undefined,
        bcc: form.bcc ? form.bcc.split(',').map((e) => ({ name: '', address: e.trim() })) : undefined,
        subject: form.subject,
        text_body: useHtml ? '' : form.textBody,
        html_body: useHtml ? form.htmlBody : undefined,
      });
      showSuccess('Email sent!');
      clearPane();
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to send email';
      showError(msg);
    } finally {
      setSending(false);
    }
  };

  const isReply = composeOptions?.isReply;

  return (
    <div className="h-full flex flex-col min-h-0 bg-card rounded-lg border">
      {/* Header */}
      <div className="shrink-0 flex items-center justify-between px-5 py-3.5 border-b">
        <h3 className="font-semibold text-sm">{isReply ? 'Reply' : 'New Message'}</h3>
        <Button variant="ghost" size="sm" className="h-7 w-7 p-0 text-muted-foreground" onClick={clearPane}>
          <svg className="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M6 18L18 6M6 6l12 12" />
          </svg>
        </Button>
      </div>

      {/* Form fields */}
      <div className="flex-1 min-h-0 overflow-y-auto px-5 py-4 flex flex-col gap-3">
        {/* From */}
        <div className="flex flex-col gap-1">
          <Label className="text-xs text-muted-foreground">From</Label>
          <select
            value={form.accountId}
            onChange={(e) => setForm({ ...form, accountId: e.target.value })}
            className="flex h-8 w-full rounded-md border border-input bg-transparent px-2.5 py-1 text-sm outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/50"
          >
            {accounts.map((a) => (
              <option key={a.id} value={a.id}>{a.email}</option>
            ))}
          </select>
        </div>

        {/* To */}
        <div className="flex flex-col gap-1">
          <Label className="text-xs text-muted-foreground">To</Label>
          <Input
            type="text"
            placeholder="recipient@example.com"
            value={form.to}
            onChange={(e) => setForm({ ...form, to: e.target.value })}
          />
        </div>

        {/* Cc / Bcc */}
        <div className="grid grid-cols-2 gap-2">
          <div className="flex flex-col gap-1">
            <Label className="text-xs text-muted-foreground">Cc</Label>
            <Input
              type="text"
              placeholder="cc@example.com"
              value={form.cc}
              onChange={(e) => setForm({ ...form, cc: e.target.value })}
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label className="text-xs text-muted-foreground">Bcc</Label>
            <Input
              type="text"
              placeholder="bcc@example.com"
              value={form.bcc}
              onChange={(e) => setForm({ ...form, bcc: e.target.value })}
            />
          </div>
        </div>

        {/* Subject */}
        <div className="flex flex-col gap-1">
          <Label className="text-xs text-muted-foreground">Subject</Label>
          <Input
            type="text"
            placeholder="Email subject"
            value={form.subject}
            onChange={(e) => setForm({ ...form, subject: e.target.value })}
          />
        </div>

        <Separator />

        {/* Body toggle */}
        <div className="flex items-center justify-between">
          <Label className="text-xs text-muted-foreground">Body</Label>
          <div className="flex gap-1">
            <button
              type="button"
              className={`px-2.5 py-1 text-[11px] rounded-md border transition-colors ${!useHtml ? 'bg-primary text-primary-foreground border-primary' : 'bg-muted text-muted-foreground border-border hover:bg-muted/80'}`}
              onClick={() => setUseHtml(false)}
            >
              Plain
            </button>
            <button
              type="button"
              className={`px-2.5 py-1 text-[11px] rounded-md border transition-colors ${useHtml ? 'bg-primary text-primary-foreground border-primary' : 'bg-muted text-muted-foreground border-border hover:bg-muted/80'}`}
              onClick={() => setUseHtml(true)}
            >
              HTML
            </button>
          </div>
        </div>

        {/* Body textarea */}
        {useHtml ? (
          <textarea
            className="flex-1 min-h-[200px] w-full rounded-md border border-input bg-transparent px-3 py-2 text-sm font-mono outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/50 resize-none"
            placeholder="Enter HTML content..."
            value={form.htmlBody}
            onChange={(e) => setForm({ ...form, htmlBody: e.target.value })}
            rows={12}
          />
        ) : (
          <textarea
            className="flex-1 min-h-[200px] w-full rounded-md border border-input bg-transparent px-3 py-2 text-sm outline-none focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/50 resize-none"
            placeholder="Enter your message..."
            value={form.textBody}
            onChange={(e) => setForm({ ...form, textBody: e.target.value })}
            rows={12}
          />
        )}
      </div>

      {/* Footer */}
      <div className="shrink-0 border-t px-5 py-3 flex items-center gap-2">
        <Button onClick={handleSend} disabled={sending} size="sm" className="gap-1.5">
          <svg className="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 19l9 2-9-18-9 18 9-2zm0 0v-8" />
          </svg>
          {sending ? 'Sending…' : 'Send'}
        </Button>
        <Button onClick={clearPane} variant="ghost" size="sm" disabled={sending}>
          Discard
        </Button>
      </div>
    </div>
  );
}
