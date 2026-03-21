import { ActionFunctionArgs, LoaderFunctionArgs, redirect } from 'react-router-dom';
import * as api from '@/services/api';
import type { Message, AccountStats } from '@/types';

export async function accountsLoader() {
  return api.listAccounts();
}

export async function messagesLoader({ params, request }: LoaderFunctionArgs) {
  const url = new URL(request.url);
  const accountId = params.accountId || url.searchParams.get('accountId');
  const folder = url.searchParams.get('folder') || 'INBOX';
  const page = parseInt(url.searchParams.get('page') || '1', 10);

  const accounts = await api.listAccounts();
  const selectedId = accountId || (accounts.length > 0 ? accounts[0].id : null);

  let messages: Message[] = [];
  let total = 0;

  if (selectedId) {
    try {
      // Use proper cursor format matching backend's CursorData structure
      const cursor = page > 1 ? btoa(JSON.stringify({ page: page - 1, sort_by: 'date', sort_order: 'desc' })) : '';
      const response = await api.getMessages(selectedId, folder, 50, cursor, 'date', 'desc');
      messages = response.messages;
      total = response.total_count;
    } catch (err: any) {
      // Handle authentication errors - redirect to account detail page
      // AUTH_ERROR indicates the account is disabled or needs re-authentication
      if (err?.code === 'AUTH_ERROR') {
        // Find the account and redirect to its detail page
        const account = accounts.find((a) => a.id === selectedId);
        if (account) {
          return redirect(`/accounts/${selectedId}?error=account_disabled`);
        }
      }
      // Re-throw other errors
      throw err;
    }
  }

  return { accounts, messages, total, selectedAccountId: selectedId };
}

export async function healthLoader() {
  const [health, accounts] = await Promise.all([
    api.getSystemHealth(),
    api.listAccounts(),
  ]);

  const stats = await Promise.all(
    accounts.map((a) => api.getAccountStats(a.id).catch(() => null))
  );

  return {
    health,
    accountStats: stats.filter(Boolean) as AccountStats[],
  };
}

// Kept for the standalone /compose route
export async function composeLoader() {
  const accounts = await api.listAccounts();
  return { accounts };
}

export async function createAccountAction({ request }: ActionFunctionArgs) {
  const formData = await request.formData();
  const data = Object.fromEntries(formData);

  await api.createAccount({
    email: data.email as string,
    password: data.password as string,
    imap_host: (data.imap_host as string) || undefined,
    imap_port: data.imap_port ? parseInt(data.imap_port as string, 10) : undefined,
    imap_encryption: (data.imap_encryption as string) || undefined,
    smtp_host: (data.smtp_host as string) || undefined,
    smtp_port: data.smtp_port ? parseInt(data.smtp_port as string, 10) : undefined,
    smtp_encryption: (data.smtp_encryption as string) || undefined,
  });

  return redirect('/accounts');
}

// Kept for the standalone /compose route
export async function sendEmailAction({ request }: ActionFunctionArgs) {
  const formData = await request.formData();
  const accountId = formData.get('accountId') as string;
  const to = (formData.get('to') as string).split(',').map((email) => ({ name: '', address: email.trim() }));
  const cc = formData.get('cc')
    ? (formData.get('cc') as string).split(',').map((email) => ({ name: '', address: email.trim() }))
    : undefined;
  const bcc = formData.get('bcc')
    ? (formData.get('bcc') as string).split(',').map((email) => ({ name: '', address: email.trim() }))
    : undefined;

  await api.sendEmail(accountId, {
    to,
    cc,
    bcc,
    subject: formData.get('subject') as string,
    text_body: formData.get('textBody') as string,
    html_body: (formData.get('htmlBody') as string) || undefined,
  });

  return redirect('/messages');
}
