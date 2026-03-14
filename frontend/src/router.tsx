import { createBrowserRouter, Navigate } from 'react-router-dom';
import { RootLayout } from '@/components/layout';
import { 
  AccountsView, 
  MessagesView, 
  ComposeView, 
  HealthView, 
  SettingsView, 
  MessageDetail 
} from '@/components/features';
import { 
  messageDetailLoader, 
  accountsLoader, 
  messagesLoader, 
  healthLoader,
  composeLoader,
  createAccountAction,
  deleteMessageAction,
  sendEmailAction
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
        path: 'messages',
        element: <MessagesView />,
        loader: messagesLoader,
      },
      {
        path: 'messages/:accountId/:messageUid',
        element: <MessageDetail />,
        loader: messageDetailLoader,
        action: deleteMessageAction,
      },
      {
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
