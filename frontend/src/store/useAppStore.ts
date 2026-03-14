import { create } from 'zustand'
import { persist } from 'zustand/middleware'

export interface Account {
  id: string
  email: string
  status: string
  connectionLimit: number
  createdAt: string
  updatedAt: string
}

export interface Message {
  uid: string
  subject: string
  from: Array<{ name: string; address: string }>
  to: Array<{ name: string; address: string }>
  date: string
  flags: string[]
  size: number
}

interface AppState {
  // Account state
  accounts: Account[]
  selectedAccountId: string | null
  setSelectedAccountId: (id: string | null) => void
  setAccounts: (accounts: Account[]) => void
  addAccount: (account: Account) => void
  removeAccount: (id: string) => void

  // Message state
  selectedMessage: Message | null
  setSelectedMessage: (message: Message | null) => void
  currentFolder: string
  setCurrentFolder: (folder: string) => void

  // UI state
  sidebarCollapsed: boolean
  setSidebarCollapsed: (collapsed: boolean) => void
  theme: 'dark' | 'light'
  setTheme: (theme: 'dark' | 'light') => void

  // API configuration
  apiUrl: string
  setApiUrl: (url: string) => void
}

export const useAppStore = create<AppState>()(
  persist(
    (set) => ({
      // Account state
      accounts: [],
      selectedAccountId: null,
      setSelectedAccountId: (id) => set({ selectedAccountId: id }),
      setAccounts: (accounts) => set({ accounts }),
      addAccount: (account) =>
        set((state) => ({ accounts: [...state.accounts, account] })),
      removeAccount: (id) =>
        set((state) => ({ accounts: state.accounts.filter((a) => a.id !== id) })),

      // Message state
      selectedMessage: null,
      setSelectedMessage: (message) => set({ selectedMessage: message }),
      currentFolder: 'INBOX',
      setCurrentFolder: (folder) => set({ currentFolder: folder }),

      // UI state
      sidebarCollapsed: false,
      setSidebarCollapsed: (collapsed) => set({ sidebarCollapsed: collapsed }),
      theme: 'dark',
      setTheme: (theme) => set({ theme }),

      // API configuration
      apiUrl: 'http://localhost:8080',
      setApiUrl: (url) => set({ apiUrl: url }),
    }),
    {
      name: 'webmail-storage',
      partialize: (state) => ({
        apiUrl: state.apiUrl,
        theme: state.theme,
        sidebarCollapsed: state.sidebarCollapsed,
      }),
    }
  )
)
