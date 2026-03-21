import type {
  Account,
  AddAccountRequest,
  MessageDetail,
  MessageListResponse,
  SearchQuery,
  SearchResponse,
  SendEmailRequest,
  SendEmailResponse,
  SystemHealthResponse,
  AccountStats,
  APIError,
  Message,
  PoolStats,
} from '../types';

export interface CacheContext {
  cursor: string;
  limit: number;
  sort_by: string;
  sort_order: string;
}

import { useAppStore } from '../store/useAppStore';

const getApiBaseUrl = () => useAppStore.getState().apiUrl || import.meta.env.VITE_API_BASE_URL || 'http://localhost:8080';
export { getApiBaseUrl };

class ApiError extends Error {
  code: string;
  details?: Record<string, string>;

  constructor(message: string, code: string, details?: Record<string, string>) {
    super(message);
    this.name = 'ApiError';
    this.code = code;
    this.details = details;
  }
}

async function handleResponse<T>(response: Response): Promise<T> {
  if (!response.ok) {
    const errorData = await response.json().catch(() => ({}));
    const apiError = errorData.error as APIError | undefined;
    throw new ApiError(
      apiError?.message || 'An error occurred',
      apiError?.code || 'UNKNOWN_ERROR',
      apiError?.details
    );
  }
  return response.json();
}

// Transform message from API format to frontend format
function transformMessage(message: Message): MessageDetail {
  return {
    ...message,
    text_body: message.body?.text || message.body?.plain_text || '',
    html_body: message.body?.html || '',
  };
}

// Account APIs
export async function listAccounts(): Promise<Account[]> {
  const response = await fetch(`${getApiBaseUrl()}/v1/accounts`);
  const data = await handleResponse<{ accounts: Account[]; total: number }>(response);
  return data.accounts;
}

export async function createAccount(request: AddAccountRequest): Promise<Account> {
  const response = await fetch(`${getApiBaseUrl()}/v1/accounts`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
  });
  return handleResponse<Account>(response);
}

export async function getAccount(id: string): Promise<Account> {
  const response = await fetch(`${getApiBaseUrl()}/v1/accounts/${id}`);
  return handleResponse<Account>(response);
}

export async function updateAccount(
  id: string,
  updates: Record<string, unknown>
): Promise<Account> {
  const response = await fetch(`${getApiBaseUrl()}/v1/accounts/${id}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(updates),
  });
  return handleResponse<Account>(response);
}

export async function deleteAccount(id: string): Promise<void> {
  const response = await fetch(`${getApiBaseUrl()}/v1/accounts/${id}`, {
    method: 'DELETE',
  });
  if (!response.ok) {
    const errorData = await response.json().catch(() => ({}));
    const apiError = errorData.error as APIError | undefined;
    throw new ApiError(
      apiError?.message || 'Failed to delete account',
      apiError?.code || 'UNKNOWN_ERROR'
    );
  }
}

// Message APIs
export async function getMessages(
  accountId: string,
  folder?: string,
  limit?: number,
  cursor?: string,
  sortBy?: string,
  sortOrder?: string
): Promise<MessageListResponse> {
  const params = new URLSearchParams();
  if (folder) params.set('folder', folder);
  if (limit) params.set('limit', limit.toString());
  if (cursor) params.set('cursor', cursor);
  if (sortBy) params.set('sort_by', sortBy);
  if (sortOrder) params.set('sort_order', sortOrder);

  const response = await fetch(
    `${getApiBaseUrl()}/v1/accounts/${accountId}/messages?${params}`
  );
  return handleResponse<MessageListResponse>(response);
}

export async function getMessage(
  accountId: string,
  uid: string,
  folder?: string
): Promise<MessageDetail> {
  const params = new URLSearchParams();
  if (folder) params.set('folder', folder);

  const response = await fetch(
    `${getApiBaseUrl()}/v1/accounts/${accountId}/messages/${uid}?${params}`
  );
  const message = await handleResponse<Message>(response);
  return transformMessage(message);
}

export async function searchMessages(
  query: SearchQuery
): Promise<SearchResponse> {
  const response = await fetch(`${getApiBaseUrl()}/v1/accounts/${query.account_id}/search`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(query),
  });
  return handleResponse<SearchResponse>(response);
}

// Send APIs
export async function sendEmail(
  accountId: string,
  request: SendEmailRequest
): Promise<SendEmailResponse> {
  const response = await fetch(`${getApiBaseUrl()}/v1/accounts/${accountId}/send`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
  });
  return handleResponse<SendEmailResponse>(response);
}

// Attachment APIs
export async function getAttachmentDownloadUrl(
  accountId: string,
  messageUid: string,
  attachmentId: string
): Promise<{ download_url: string; expires_at: string; filename: string; content_type: string; size: number }> {
  const response = await fetch(
    `${getApiBaseUrl()}/v1/accounts/${accountId}/messages/${messageUid}/attachments/${attachmentId}`
  );
  return handleResponse(response);
}

export async function downloadAttachment(
  accountId: string,
  messageUid: string,
  attachmentId: string,
  filename: string
) {
  const response = await fetch(
    `${getApiBaseUrl()}/v1/accounts/${accountId}/messages/${messageUid}/attachments/${attachmentId}`,
    {
      headers: {
        'Accept': 'application/octet-stream',
      },
    }
  );

  if (!response.ok) {
    throw new Error('Failed to download attachment');
  }

  const blob = await response.blob();
  const url = window.URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  window.URL.revokeObjectURL(url);
  document.body.removeChild(a);
}

// Health APIs
export async function getSystemHealth(): Promise<SystemHealthResponse> {
  const response = await fetch(`${getApiBaseUrl()}/v1/health`);
  return handleResponse<SystemHealthResponse>(response);
}

export async function getPoolStats(): Promise<PoolStats> {
  const response = await fetch(`${getApiBaseUrl()}/v1/debug/pool-stats`);
  return handleResponse<PoolStats>(response);
}

export async function getAccountStatus(accountId: string): Promise<{
  account_id: string;
  status: string;
  last_sync?: string;
  error?: string;
}> {
  const response = await fetch(`${getApiBaseUrl()}/v1/health/accounts/${accountId}`);
  return handleResponse(response);
}

export async function getAccountStats(accountId: string): Promise<AccountStats> {
  const response = await fetch(`${getApiBaseUrl()}/v1/accounts/${accountId}/stats`);
  return handleResponse<AccountStats>(response);
}

// Server Capabilities APIs
export async function getServerCapabilities(accountId: string): Promise<import('@/types').ServerCapabilities> {
  const response = await fetch(`${getApiBaseUrl()}/v1/accounts/${accountId}/capabilities`);
  return handleResponse<import('@/types').ServerCapabilities>(response);
}

export async function refreshServerCapabilities(accountId: string): Promise<{ capabilities: import('@/types').ServerCapabilities; refreshed: boolean }> {
  const response = await fetch(`${getApiBaseUrl()}/v1/accounts/${accountId}/capabilities/refresh`, {
    method: 'POST',
  });
  return handleResponse<{ capabilities: import('@/types').ServerCapabilities; refreshed: boolean }>(response);
}

export async function updateSyncSettings(
  accountId: string,
  syncSettings: import('@/types').SyncSettings
): Promise<Account> {
  const response = await fetch(`${getApiBaseUrl()}/v1/accounts/${accountId}/sync-settings`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(syncSettings),
  });
  return handleResponse<Account>(response);
}

export async function getAccountFolders(accountId: string): Promise<{ folders: any[]; total: number }> {
  const response = await fetch(`${getApiBaseUrl()}/v1/accounts/${accountId}/folders`);
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error || 'Failed to load folders');
  }
  return response.json();
}

export async function getLiveFolders(accountId: string): Promise<{ folders: any[]; total: number }> {
  const response = await fetch(`${getApiBaseUrl()}/v1/accounts/${accountId}/folders/live`);
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error || 'Failed to load folders');
  }
  return response.json();
}

export async function getFolderTree(accountId: string): Promise<{ folders: any[]; total: number }> {
  const response = await fetch(`${getApiBaseUrl()}/v1/accounts/${accountId}/folders/tree`);
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error || 'Failed to load folder tree');
  }
  return response.json();
}

export async function markMessageRead(
  accountId: string,
  uid: string,
  folder?: string,
  cacheContext?: CacheContext
): Promise<void> {
  const params = new URLSearchParams();
  if (folder) params.set('folder', folder);

  const response = await fetch(
    `${getApiBaseUrl()}/v1/accounts/${accountId}/messages/${uid}/mark-read?${params}`,
    { 
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ cache_context: cacheContext })
    }
  );
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error?.message || 'Failed to mark message as read');
  }
}

export async function markMessagesRead(
  accountId: string,
  uids: string[],
  folder?: string,
  cacheContext?: CacheContext
): Promise<void> {
  const response = await fetch(`${getApiBaseUrl()}/v1/accounts/${accountId}/messages/mark-read`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ uids, folder: folder || 'INBOX', cache_context: cacheContext }),
  });
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error?.message || 'Failed to mark messages as read');
  }
}
