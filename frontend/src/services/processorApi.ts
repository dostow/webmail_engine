import type {
  AccountProcessorConfig,
  AccountProcessorsResponse,
  CreateProcessorRequest,
  ProcessorTypeInfoResponse,
} from '@/types/processor';
import { getApiBaseUrl } from './api';

// Processor Type APIs
export async function listProcessorTypes(): Promise<string[]> {
  const response = await fetch(`${getApiBaseUrl()}/v1/processors/types`);
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error || 'Failed to list processor types');
  }
  const data = await response.json();
  return data.types;
}

export async function getProcessorTypeInfo(type: string): Promise<ProcessorTypeInfoResponse> {
  const response = await fetch(`${getApiBaseUrl()}/v1/processors/types/${type}`);
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error || 'Failed to get processor type info');
  }
  return response.json();
}

// Account Processor APIs
export async function getAccountProcessors(accountId: string): Promise<AccountProcessorConfig[]> {
  const response = await fetch(`${getApiBaseUrl()}/v1/processors/accounts/${accountId}`);
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error || 'Failed to get account processors');
  }
  const data: AccountProcessorsResponse = await response.json();
  return data.configs;
}

export async function createAccountProcessor(
  accountId: string,
  request: CreateProcessorRequest
): Promise<void> {
  const response = await fetch(`${getApiBaseUrl()}/v1/processors/accounts/${accountId}/processors`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
  });
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error || 'Failed to create processor');
  }
}

export async function updateAccountProcessor(
  accountId: string,
  processorType: string,
  enabled: boolean
): Promise<void> {
  const response = await fetch(
    `${getApiBaseUrl()}/v1/processors/accounts/${accountId}/${processorType}`,
    {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ enabled }),
    }
  );
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error || 'Failed to update processor');
  }
}

export async function deleteAccountProcessor(
  accountId: string,
  processorType: string
): Promise<void> {
  const response = await fetch(
    `${getApiBaseUrl()}/v1/processors/accounts/${accountId}/processors/${processorType}`,
    {
      method: 'DELETE',
    }
  );
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error || 'Failed to delete processor');
  }
}
