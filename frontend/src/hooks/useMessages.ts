import { useCallback, useState } from 'react';
import * as api from '@/services/api';
import { useEmailToast } from '@/hooks/useToast';
import type { Message } from '@/types';

export interface MessageListResponse {
  messages: Message[];
  folder: string;
  total: number;
  has_more: boolean;
  next_cursor?: string;
}

export function useMessages() {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const { showError } = useEmailToast();

  const fetchMessages = useCallback(async (
    accountId: string,
    folder: string = 'INBOX',
    limit: number = 50,
    cursor: string = ''
  ): Promise<MessageListResponse | null> => {
    try {
      setLoading(true);
      const data = await api.getMessages(accountId, folder, limit, cursor);
      setError(null);
      return data;
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load messages';
      setError(message);
      showError(message);
      return null;
    } finally {
      setLoading(false);
    }
  }, [showError]);

  const fetchMessage = useCallback(async (
    accountId: string,
    uid: string,
    folder: string = 'INBOX'
  ): Promise<any | null> => {
    try {
      setLoading(true);
      const data = await api.getMessage(accountId, uid, folder);
      setError(null);
      return data;
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load message';
      setError(message);
      showError(message);
      return null;
    } finally {
      setLoading(false);
    }
  }, [showError]);

  return {
    loading,
    error,
    fetchMessages,
    fetchMessage,
  };
}
