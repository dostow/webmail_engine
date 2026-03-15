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

export interface MessageFilters {
  search?: string;
  from?: string;
  subject?: string;
  unreadOnly?: boolean;
  hasAttachments?: boolean;
}

export function useMessages() {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [filters, setFilters] = useState<MessageFilters>({});
  const { showError } = useEmailToast();

  const fetchMessages = useCallback(async (
    accountId: string,
    folder: string = 'INBOX',
    limit: number = 50,
    cursor: string = '',
    sortBy?: string,
    sortOrder?: string
  ): Promise<MessageListResponse | null> => {
    try {
      setLoading(true);
      const data = await api.getMessages(accountId, folder, limit, cursor, sortBy, sortOrder);
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

  // Client-side filtering of messages
  const filterMessages = useCallback((messages: Message[], filters: MessageFilters): Message[] => {
    if (!filters || Object.keys(filters).length === 0) {
      return messages;
    }

    return messages.filter((message) => {
      // Search filter (matches subject or sender)
      if (filters.search) {
        const searchLower = filters.search.toLowerCase();
        const subjectMatch = message.subject?.toLowerCase().includes(searchLower);
        const fromMatch = message.from?.name?.toLowerCase().includes(searchLower) ||
                         message.from?.address?.toLowerCase().includes(searchLower);
        if (!subjectMatch && !fromMatch) {
          return false;
        }
      }

      // From filter
      if (filters.from) {
        const fromLower = filters.from.toLowerCase();
        const fromMatch = message.from?.name?.toLowerCase().includes(fromLower) ||
                         message.from?.address?.toLowerCase().includes(fromLower);
        if (!fromMatch) {
          return false;
        }
      }

      // Subject filter
      if (filters.subject) {
        const subjectLower = filters.subject.toLowerCase();
        if (!message.subject?.toLowerCase().includes(subjectLower)) {
          return false;
        }
      }

      // Unread filter
      if (filters.unreadOnly) {
        const isUnread = message.flags && !message.flags.includes('\\Seen');
        if (!isUnread) {
          return false;
        }
      }

      // Has attachments filter
      if (filters.hasAttachments) {
        if (!message.has_attachments) {
          return false;
        }
      }

      return true;
    });
  }, []);

  const updateFilters = useCallback((newFilters: Partial<MessageFilters>) => {
    setFilters((prev) => ({ ...prev, ...newFilters }));
  }, []);

  const clearFilters = useCallback(() => {
    setFilters({});
  }, []);

  return {
    loading,
    error,
    filters,
    fetchMessages,
    fetchMessage,
    filterMessages,
    updateFilters,
    clearFilters,
  };
}
