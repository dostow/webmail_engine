import { create } from 'zustand';
import * as api from '@/services/api';
import type { Message } from '@/types';

export interface MessageListState {
  // Data
  messages: Message[];
  total: number;
  folder: string;
  
  // Pagination
  currentPage: number;
  pageSize: number;
  totalPages: number;
  nextCursor?: string;
  
  // UI State
  loading: boolean;
  error: string | null;
  accountId: string | null;
  
  // Actions
  setAccount: (accountId: string) => void;
  setPage: (page: number) => Promise<void>;
  nextPage: () => Promise<void>;
  previousPage: () => Promise<void>;
  refresh: () => Promise<void>;
  clear: () => void;
}

const MESSAGES_PER_PAGE = 50;

export const useMessageList = create<MessageListState>((set, get) => ({
  // Initial state
  messages: [],
  total: 0,
  folder: 'INBOX',
  currentPage: 1,
  pageSize: MESSAGES_PER_PAGE,
  totalPages: 1,
  nextCursor: undefined,
  loading: false,
  error: null,
  accountId: null,

  setAccount: async (accountId) => {
    set({ accountId, loading: true, error: null, currentPage: 1 });
    await get().refresh();
  },

  setPage: async (page) => {
    const state = get();
    if (page < 1 || page > state.totalPages || page === state.currentPage) {
      return;
    }
    
    set({ loading: true, error: null, currentPage: page });
    
    try {
      // Calculate cursor based on page
      const offset = (page - 1) * MESSAGES_PER_PAGE;
      const cursor = offset > 0 ? btoa(JSON.stringify({ offset, page })) : '';
      
      const response = await api.getMessages(
        state.accountId!,
        state.folder,
        MESSAGES_PER_PAGE,
        cursor,
        'date',
        'desc'
      );
      
      set({
        messages: response.messages,
        total: response.total,
        nextCursor: response.next_cursor,
        totalPages: Math.max(1, Math.ceil(response.total / MESSAGES_PER_PAGE)),
        loading: false,
      });
    } catch (err) {
      set({
        error: err instanceof Error ? err.message : 'Failed to load messages',
        loading: false,
      });
    }
  },

  nextPage: async () => {
    const state = get();
    if (state.currentPage < state.totalPages) {
      await get().setPage(state.currentPage + 1);
    }
  },

  previousPage: async () => {
    const state = get();
    if (state.currentPage > 1) {
      await get().setPage(state.currentPage - 1);
    }
  },

  refresh: async () => {
    const state = get();
    if (!state.accountId) return;
    
    set({ loading: true, error: null });
    
    try {
      const page = state.currentPage;
      const offset = (page - 1) * MESSAGES_PER_PAGE;
      const cursor = offset > 0 ? btoa(JSON.stringify({ offset, page })) : '';
      
      const response = await api.getMessages(
        state.accountId,
        state.folder,
        MESSAGES_PER_PAGE,
        cursor,
        'date',
        'desc'
      );
      
      set({
        messages: response.messages,
        total: response.total,
        nextCursor: response.next_cursor,
        totalPages: Math.max(1, Math.ceil(response.total / MESSAGES_PER_PAGE)),
        loading: false,
      });
    } catch (err) {
      set({
        error: err instanceof Error ? err.message : 'Failed to load messages',
        loading: false,
      });
    }
  },

  clear: () => {
    set({
      messages: [],
      total: 0,
      currentPage: 1,
      totalPages: 1,
      nextCursor: undefined,
      accountId: null,
      loading: false,
      error: null,
    });
  },
}));
