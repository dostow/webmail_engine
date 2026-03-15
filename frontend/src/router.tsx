import { createBrowserRouter, Navigate } from 'react-router-dom';
import { RootLayout } from '@/components/layout';
import {
  AccountsView,
  MessagesView,
  ComposeView,
  HealthView,
  SettingsView,
} from '@/components/features';
import {
  accountsLoader,
  messagesLoader,
  healthLoader,
  composeLoader,
  createAccountAction,
  sendEmailAction,
} from '@/lib/loaders';

export const router = createBrowserRouter([
  {
    path: '/',
    element: <RootLayout />,
    children: [
      {
        index: true,
        element: <Navigate to="/accounts" replace />,
      },
      {
        path: 'accounts',
        element: <AccountsView />,
        loader: accountsLoader,
        action: createAccountAction,
      },
      {
        // Single flat route — no children. Message detail is state-driven
        path: 'messages',
        element: <MessagesView />,
        loader: messagesLoader,
      },
      {
        // Standalone compose still available from the nav
        path: 'compose',
        element: <ComposeView />,
        loader: composeLoader,
        action: sendEmailAction,
      },
      {
        path: 'health',
        element: <HealthView />,
        loader: healthLoader,
      },
      {
        path: 'settings',
        element: <SettingsView />,
      },
      {
        path: '*',
        element: <Navigate to="/accounts" replace />,
      },
    ],
  },
]);
