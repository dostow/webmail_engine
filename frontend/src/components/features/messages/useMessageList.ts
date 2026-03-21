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
  currentCursor?: string; // The exact cursor string used to fetch the current page
  nextCursor?: string;
  lastUid?: number; // Last UID from current page for stable pagination

  // UI State
  loading: boolean;
  error: string | null;
  accountId: string | null;

  // Actions
  setAccount: (accountId: string) => void;
  setFolder: (folder: string) => void;
  setPage: (page: number) => Promise<void>;
  nextPage: () => Promise<void>;
  previousPage: () => Promise<void>;
  refresh: () => Promise<void>;
  clear: () => void;
}

const MESSAGES_PER_PAGE = 50;

/**
 * Encodes cursor data to match backend's CursorData structure.
 * Uses base64-encoded JSON with page, last_uid, sort_by, sort_order, and timestamp.
 * Including last_uid enables stable pagination even when new emails arrive.
 */
function encodeCursor(page: number, lastUid?: number, sortBy: string = 'date', sortOrder: string = 'desc'): string {
  if (page <= 1 && !lastUid) {
    return '';
  }
  const cursorData: Record<string, any> = {
    page: page - 1, // Backend uses 0-based page index
    sort_by: sortBy,
    sort_order: sortOrder
  };
  if (lastUid) {
    cursorData.last_uid = lastUid;
  }
  return btoa(JSON.stringify(cursorData));
}

/**
 * Extracts the last UID from a message list for stable pagination.
 */
function getLastUidFromMessages(messages: Message[]): number | undefined {
  if (messages.length === 0) return undefined;
  const lastMessage = messages[messages.length - 1];
  const uid = parseInt(lastMessage.uid, 10);
  return isNaN(uid) ? undefined : uid;
}

export const useMessageList = create<MessageListState>((set, get) => ({
  // Initial state
  messages: [],
  total: 0,
  folder: 'INBOX',
  currentPage: 1,
  pageSize: MESSAGES_PER_PAGE,
  totalPages: 1,
  nextCursor: undefined,
  lastUid: undefined,
  loading: false,
  error: null,
  accountId: null,

  setAccount: async (accountId) => {
    set({ accountId, messages: [], total: 0, loading: true, error: null, currentPage: 1, lastUid: undefined });
    await get().refresh();
  },

  setFolder: async (folder) => {
    set({ folder, messages: [], total: 0, loading: true, error: null, currentPage: 1, lastUid: undefined });
    await get().refresh();
  },

  setPage: async (page) => {
    const state = get();
    if (page < 1 || page > state.totalPages || page === state.currentPage) {
      return;
    }

    set({ loading: true, error: null, currentPage: page });

    try {
      // Encode cursor with lastUid for stable pagination
      // ONLY use lastUid if we are advancing exactly one page.
      const isNextPage = page === state.currentPage + 1;
      const anchorUid = isNextPage ? state.lastUid : undefined;
      const cursor = encodeCursor(page, anchorUid, 'date', 'desc');

      const response = await api.getMessages(
        state.accountId!,
        state.folder,
        MESSAGES_PER_PAGE,
        cursor,
        'date',
        'desc'
      );

      // Extract last UID for next pagination
      const newLastUid = getLastUidFromMessages(response.messages);

      // Use server's pagination values as source of truth
      set({
        messages: response.messages,
        total: response.total_count,
        pageSize: response.page_size,
        currentPage: response.current_page,
        totalPages: response.total_pages,
        currentCursor: cursor,
        nextCursor: response.next_cursor,
        lastUid: newLastUid,
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
      // When going back, we need to recalculate - clear lastUid to use page-based
      set({ lastUid: undefined });
      await get().setPage(state.currentPage - 1);
    }
  },

  refresh: async () => {
    const state = get();
    if (!state.accountId) return;

    set({ loading: true, error: null });

    try {
      const page = state.currentPage;
      // On refresh, omit lastUid so we fetch the current page without 
      // advancing the cursor to the next page
      const cursor = encodeCursor(page, undefined, 'date', 'desc');

      const response = await api.getMessages(
        state.accountId,
        state.folder,
        MESSAGES_PER_PAGE,
        cursor,
        'date',
        'desc'
      );

      // Extract last UID for next pagination
      const newLastUid = getLastUidFromMessages(response.messages);

      // Use server's pagination values as source of truth
      set({
        messages: response.messages,
        total: response.total_count,
        pageSize: response.page_size,
        currentPage: response.current_page,
        totalPages: response.total_pages,
        currentCursor: cursor,
        nextCursor: response.next_cursor,
        lastUid: newLastUid,
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
      currentCursor: undefined,
      nextCursor: undefined,
      lastUid: undefined,
      accountId: null,
      loading: false,
      error: null,
    });
  },
}));
