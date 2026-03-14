import { useState } from 'react';
import { useLoaderData, Form, useNavigation, useActionData } from 'react-router-dom';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Label } from '@/components/ui/label';
import type { Account } from '@/types';

interface LoaderData {
  accounts: Account[];
}

const defaultForm = {
  to: '',
  cc: '',
  bcc: '',
  subject: '',
  textBody: '',
  htmlBody: '',
};

export function ComposeView() {
  const { accounts } = useLoaderData() as LoaderData;
  const navigation = useNavigation();
  const actionData = useActionData() as { error?: string } | undefined;
  const [form, setForm] = useState(defaultForm);
  const [useHtml, setUseHtml] = useState(false);

  const sending = navigation.state === 'submitting';

  return (
    <div className="max-w-[800px]">
      <Card>
        <div className="flex items-center justify-between border-b px-6 py-4">
          <h3 className="text-lg font-semibold">Compose Email</h3>
        </div>
        <div className="p-6">
          <Form method="post" className="flex flex-col gap-4">
            <div>
              <Label htmlFor="accountId">From</Label>
              <select
                id="accountId"
                name="accountId"
                className="flex h-8 w-full min-w-0 rounded-lg border border-input bg-transparent px-2.5 py-1 text-base transition-colors outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 md:text-sm"
                defaultValue={accounts[0]?.id || ''}
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
                name="to"
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
                  name="cc"
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
                  name="bcc"
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
                name="subject"
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
                  id="htmlBody"
                  name="htmlBody"
                  className="w-full min-h-[200px] rounded-lg border border-input bg-transparent px-3 py-2 text-sm font-mono outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
                  placeholder="Enter HTML content..."
                  value={form.htmlBody}
                  onChange={(e) => setForm({ ...form, htmlBody: e.target.value })}
                  rows={15}
                />
              ) : (
                <textarea
                  id="textBody"
                  name="textBody"
                  className="w-full min-h-[200px] rounded-lg border border-input bg-transparent px-3 py-2 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
                  placeholder="Enter your message..."
                  value={form.textBody}
                  onChange={(e) => setForm({ ...form, textBody: e.target.value })}
                  rows={15}
                />
              )}
            </div>

            {actionData?.error && (
              <div className="rounded-lg border border-destructive bg-destructive/10 px-4 py-3 text-destructive">
                {actionData.error}
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
          </Form>
        </div>
      </Card>
    </div>
  );
}
