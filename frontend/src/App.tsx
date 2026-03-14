import { useState } from 'react';
import { Routes, Route, Navigate, useNavigate, useLocation } from 'react-router-dom';
import { TooltipProvider } from '@/components/ui/tooltip';
import { Toaster } from '@/components/ui/sonner';
import { Sidebar, Header } from '@/components/layout';
import { AccountsView, MessagesView, ComposeView, HealthView, SettingsView, MessageDetail } from '@/components/features';
import './App.css';

const navSections = [
  {
    title: 'Main',
    items: [
      {
        id: 'accounts',
        label: 'Accounts',
        icon: (
          <svg width="20" height="20" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 4.354a4 4 0 110 5.292M15 21H3v-1a6 6 0 0112 0v1zm0 0h6v-1a6 6 0 00-9-5.197M13 7a4 4 0 11-8 0 4 4 0 018 0z" />
          </svg>
        ),
      },
      {
        id: 'messages',
        label: 'Messages',
        icon: (
          <svg width="20" height="20" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M20 13V6a2 2 0 00-2-2H6a2 2 0 00-2 2v7m16 0v5a2 2 0 01-2 2H6a2 2 0 01-2-2v-5m16 0h-2.586a1 1 0 00-.707.293l-2.414 2.414a1 1 0 01-.707.293h-3.172a1 1 0 01-.707-.293l-2.414-2.414A1 1 0 006.586 13H4" />
          </svg>
        ),
      },
      {
        id: 'compose',
        label: 'Compose',
        icon: (
          <svg width="20" height="20" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
          </svg>
        ),
      },
    ],
  },
  {
    title: 'System',
    items: [
      {
        id: 'health',
        label: 'Health',
        icon: (
          <svg width="20" height="20" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
        ),
      },
      {
        id: 'settings',
        label: 'Settings',
        icon: (
          <svg width="20" height="20" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
          </svg>
        ),
      },
    ],
  },
];

// Map route paths to nav item IDs
const routeToNavMap: Record<string, string> = {
  '/accounts': 'accounts',
  '/messages': 'messages',
  '/compose': 'compose',
  '/health': 'health',
  '/settings': 'settings',
};

// Map nav item IDs to routes
const navToRouteMap: Record<string, string> = {
  accounts: '/accounts',
  messages: '/messages',
  compose: '/compose',
  health: '/health',
  settings: '/settings',
};

// Map nav item IDs to page titles
const navToTitleMap: Record<string, string> = {
  accounts: 'Accounts',
  messages: 'Messages',
  compose: 'Compose',
  health: 'System Health',
  settings: 'Settings',
};

function AppContent() {
  const navigate = useNavigate();
  const location = useLocation();
  const [apiUrl, setApiUrl] = useState(() => {
    return localStorage.getItem('apiUrl') || import.meta.env.VITE_API_BASE_URL || 'http://localhost:8080';
  });

  // Determine active nav item from current route
  const getActiveNavFromPath = (): string => {
    const path = location.pathname;

    // Handle message detail route - show Messages as active
    if (path.startsWith('/messages/')) {
      return 'messages';
    }

    // Direct route match
    if (routeToNavMap[path]) {
      return routeToNavMap[path];
    }

    // Default to accounts
    return 'accounts';
  };

  const activeNav = getActiveNavFromPath();

  const handleNavChange = (navId: string) => {
    const route = navToRouteMap[navId];
    if (route) {
      navigate(route);
    }
  };

  const getPageTitle = (): string => {
    // For message detail, show a different title
    if (location.pathname.startsWith('/messages/')) {
      const parts = location.pathname.split('/');
      if (parts.length >= 4) {
        return 'Message Detail';
      }
      return 'Messages';
    }
    return navToTitleMap[activeNav] || 'Webmail';
  };

  return (
    <div className="app">
      <Sidebar
        sections={navSections}
        activeView={activeNav}
        onViewChange={handleNavChange}
      />
      <main className="main-content">
        <Header
          title={getPageTitle()}
          apiUrl={apiUrl}
          onApiUrlChange={setApiUrl}
        />
        <Routes>
          <Route path="/accounts" element={<AccountsView />} />
          <Route path="/messages" element={<MessagesView />} />
          <Route path="/messages/:accountId/:messageUid" element={<MessageDetail />} />
          <Route path="/compose" element={<ComposeView />} />
          <Route path="/health" element={<HealthView />} />
          <Route path="/settings" element={<SettingsView />} />
          <Route path="/" element={<Navigate to="/accounts" replace />} />
          <Route path="*" element={<Navigate to="/accounts" replace />} />
        </Routes>
      </main>
      <Toaster />
    </div>
  );
}

function App() {
  return (
    <TooltipProvider>
      <AppContent />
    </TooltipProvider>
  );
}

export default App;
