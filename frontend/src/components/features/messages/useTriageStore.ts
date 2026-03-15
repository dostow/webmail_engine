import { create } from 'zustand';
import type { Message } from '@/types';

export type PaneMode = 'detail' | 'compose' | null;

export interface ComposeOptions {
  to?: string;
  subject?: string;
  body?: string;
  isReply?: boolean;
}

interface TriageState {
  selectedAccountId: string | null;
  selectedMessageUid: string | null;
  selectedMessage: Message | null;  // basic list row data for instant header render
  paneMode: PaneMode;
  composeOptions: ComposeOptions | null;

  // Actions
  setAccount: (accountId: string) => void;
  selectMessage: (accountId: string, uid: string, message?: Message) => void;
  openCompose: (opts?: ComposeOptions) => void;
  clearPane: () => void;
  clearAll: () => void;
}

export const useTriageStore = create<TriageState>((set) => ({
  selectedAccountId: null,
  selectedMessageUid: null,
  selectedMessage: null,
  paneMode: null,
  composeOptions: null,

  setAccount: (accountId) =>
    set({ selectedAccountId: accountId, selectedMessageUid: null, selectedMessage: null, paneMode: null, composeOptions: null }),

  selectMessage: (accountId, uid, message) =>
    set({ selectedAccountId: accountId, selectedMessageUid: uid, selectedMessage: message ?? null, paneMode: 'detail', composeOptions: null }),

  openCompose: (opts) =>
    set({ paneMode: 'compose', composeOptions: opts ?? null }),

  clearPane: () =>
    set((state) => ({
      paneMode: state.selectedMessageUid ? 'detail' : null,
      composeOptions: null,
    })),

  clearAll: () =>
    set({ selectedMessageUid: null, selectedMessage: null, paneMode: null, composeOptions: null }),
}));
