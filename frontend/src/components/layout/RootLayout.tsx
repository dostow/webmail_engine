import { useState, useEffect } from 'react';
import { Outlet, useNavigate, useLocation } from 'react-router-dom';
import { Sidebar, Header } from '@/components/layout';
import { Toaster } from '@/components/ui/sonner';
import { TooltipProvider } from '@/components/ui/tooltip';
import { useAppStore } from '@/store/useAppStore';

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

const routeToNavMap: Record<string, string> = {
  '/accounts': 'accounts',
  '/messages': 'messages',
  '/compose': 'compose',
  '/health': 'health',
  '/settings': 'settings',
};

const navToRouteMap: Record<string, string> = {
  accounts: '/accounts',
  messages: '/messages',
  compose: '/compose',
  health: '/health',
  settings: '/settings',
};

const navToTitleMap: Record<string, string> = {
  accounts: 'Accounts',
  messages: 'Messages',
  compose: 'Compose',
  health: 'System Health',
  settings: 'Settings',
};

export function RootLayout() {
  const navigate = useNavigate();
  const location = useLocation();
  const theme = useAppStore((state) => state.theme);
  const [apiUrl, setApiUrl] = useState(() => {
    return localStorage.getItem('apiUrl') || import.meta.env.VITE_API_BASE_URL || 'http://localhost:8080';
  });

  // Apply theme class to document
  useEffect(() => {
    if (theme === 'dark') {
      document.documentElement.classList.add('dark');
    } else {
      document.documentElement.classList.remove('dark');
    }
  }, [theme]);

  const getActiveNavFromPath = (): string => {
    const path = location.pathname;
    if (path.startsWith('/messages/')) return 'messages';
    if (routeToNavMap[path]) return routeToNavMap[path];
    return 'accounts';
  };

  const activeNav = getActiveNavFromPath();

  const handleNavChange = (navId: string) => {
    const route = navToRouteMap[navId];
    if (route) navigate(route);
  };

  const getPageTitle = (): string => {
    if (location.pathname.startsWith('/messages/')) {
      const parts = location.pathname.split('/');
      if (parts.length >= 4) return 'Message Detail';
      return 'Messages';
    }
    return navToTitleMap[activeNav] || 'Webmail';
  };

  return (
    <TooltipProvider>
      <div className="flex h-screen overflow-hidden bg-background">
        <Sidebar
          sections={navSections}
          activeView={activeNav}
          onViewChange={handleNavChange}
        />
        <main className="flex-1 ml-[250px] p-6 flex flex-col h-full min-h-0">
          <div className="shrink-0 pb-6">
            <Header
              title={getPageTitle()}
              apiUrl={apiUrl}
              onApiUrlChange={setApiUrl}
             />
          </div>
          <div className="flex-1 min-h-0 flex flex-col">
            <Outlet />
          </div>
        </main>
        <Toaster />
      </div>
    </TooltipProvider>
  );
}
