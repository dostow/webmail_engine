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
} from '../types';

import { useAppStore } from '../store/useAppStore';

const getApiBaseUrl = () => useAppStore.getState().apiUrl || import.meta.env.VITE_API_BASE_URL || 'http://localhost:8080';

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
  cursor?: string
): Promise<MessageListResponse> {
  const params = new URLSearchParams();
  if (folder) params.set('folder', folder);
  if (limit) params.set('limit', limit.toString());
  if (cursor) params.set('cursor', cursor);

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

// Health APIs
export async function getSystemHealth(): Promise<SystemHealthResponse> {
  const response = await fetch(`${getApiBaseUrl()}/v1/health`);
  return handleResponse<SystemHealthResponse>(response);
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
