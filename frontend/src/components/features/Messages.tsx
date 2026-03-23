import React from 'react';
import { useLoaderData, useNavigation, useSearchParams, useNavigate } from 'react-router-dom';
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from '@/components/ui/resizable';
import { useTriageStore } from './messages/useTriageStore';
import { MessageList } from './messages/MessageList';
import { FolderPane } from './messages/FolderPane';
import { MessageDetailPane } from './messages/MessageDetailPane';
import { ComposePane } from './messages/ComposePane';
import type { Account, Message } from '@/types';

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const MESSAGES_PER_PAGE = 50;
const DEFAULT_FOLDER = 'INBOX';
const DEFAULT_PAGE = 1;

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface LoaderData {
  accounts: Account[];
  messages: Message[];
  total: number;
  selectedAccountId: string | null;
}

export type PaneMode = 'detail' | 'compose' | 'empty';

// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

interface FolderPanelContentProps {
  accountId: string | null;
  selectedFolder: string;
  onSelectFolder: (folder: string) => void;
}

function FolderPanelContent({ accountId, selectedFolder, onSelectFolder }: FolderPanelContentProps) {
  if (!accountId) {
    return (
      <p className="p-4 text-sm text-muted-foreground text-center">
        Select an account to view folders
      </p>
    );
  }
  return (
    <FolderPane accountId={accountId} selectedFolder={selectedFolder} onSelectFolder={onSelectFolder} />
  );
}

interface DetailPanelContentProps {
  paneMode: PaneMode;
  accountId: string | null;
  messageUid: string | null;
  accounts: Account[];
}

function DetailPanelContent({ paneMode, accountId, messageUid, accounts }: DetailPanelContentProps) {
  if (paneMode === 'detail' && accountId && messageUid) {
    return <MessageDetailPane accountId={accountId} messageUid={messageUid} />;
  }
  if (paneMode === 'compose') {
    return <ComposePane accounts={accounts} />;
  }
  return (
    <div className="h-full flex items-center justify-center text-muted-foreground text-sm">
      <div className="text-center">
        <div className="text-4xl mb-2">📬</div>
        <p>Select a message to read</p>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// MessagesView
//
// Data flow:
//   User action (account / folder / page change)
//     → navigate() updates the URL
//       → React Router re-runs the loader
//         → useLoaderData() returns fresh data
//           → component re-renders with new messages
//
// The loader is the ONLY mechanism that triggers a fetch. No useEffect syncing,
// no store.refresh() calls — navigation drives everything.
// ---------------------------------------------------------------------------

export function MessagesView() {
  const { accounts, messages, total, selectedAccountId: loaderAccountId } = useLoaderData() as LoaderData;

  const navigation = useNavigation();
  const isLoading = navigation.state === 'loading';

  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();

  const { selectedMessageUid, paneMode, setAccount, selectedAccountId } = useTriageStore();

  // Derived URL state
  const selectedFolder = searchParams.get('folder') ?? DEFAULT_FOLDER;
  const page = parseInt(searchParams.get('page') ?? String(DEFAULT_PAGE), 10);
  const totalPages = Math.max(1, Math.ceil(total / MESSAGES_PER_PAGE));

  // Priority: in-memory triage store > URL param > loader fallback
  const effectiveAccountId = selectedAccountId ?? searchParams.get('accountId') ?? loaderAccountId;

  // -------------------------------------------------------------------------
  // Handlers — every handler navigates, which re-runs the loader
  // -------------------------------------------------------------------------

  const handleAccountChange = React.useCallback(
    (accountId: string) => {
      setAccount(accountId);
      navigate(`/messages?accountId=${accountId}&folder=${DEFAULT_FOLDER}&page=${DEFAULT_PAGE}`);
    },
    [setAccount, navigate],
  );

  const handlePageChange = React.useCallback(
    (newPage: number) => {
      const params = new URLSearchParams(searchParams);
      params.set('page', String(newPage));
      if (effectiveAccountId) params.set('accountId', effectiveAccountId);
      if (selectedFolder) params.set('folder', selectedFolder);
      navigate(`/messages?${params.toString()}`);
    },
    [navigate, searchParams, effectiveAccountId, selectedFolder],
  );

  const handleFolderChange = React.useCallback(
    (folder: string) => {
      const params = new URLSearchParams(searchParams);
      if (effectiveAccountId) params.set('accountId', effectiveAccountId);
      params.set('folder', folder);
      params.set('page', String(DEFAULT_PAGE));
      setSearchParams(params);
    },
    [searchParams, setSearchParams, effectiveAccountId],
  );

  const handleRefresh = React.useCallback(() => {
    const params = new URLSearchParams(searchParams);
    params.set('page', String(page));
    if (effectiveAccountId) params.set('accountId', effectiveAccountId);
    if (selectedFolder) params.set('folder', selectedFolder);
    navigate(`/messages?${params.toString()}`);
  }, [navigate, searchParams, page, effectiveAccountId, selectedFolder]);

  // -------------------------------------------------------------------------
  // Render
  // -------------------------------------------------------------------------

  return (
    <div className="w-full h-full flex flex-col min-h-0">
      <ResizablePanelGroup orientation="horizontal" className="flex-1 min-h-0 w-full">

        {/* Folder pane */}
        <ResizablePanel id="folder-list" defaultSize="15%" minSize={30} maxSize={200} className="no-scrollbar">
          <div className="h-full border-r bg-muted/10">
            <FolderPanelContent
              accountId={effectiveAccountId}
              selectedFolder={selectedFolder}
              onSelectFolder={handleFolderChange}
            />
          </div>
        </ResizablePanel>

        <ResizableHandle withHandle />

        {/* Message list pane */}
        <ResizablePanel id="message-list" defaultSize="25%" minSize="30%" maxSize="50%" className="no-scrollbar">
          <MessageList
            accounts={accounts}
            messages={messages}
            total={total}
            page={page}
            totalPages={totalPages}
            loading={isLoading}
            folder={selectedFolder}
            onAccountChange={handleAccountChange}
            onPageChange={handlePageChange}
            onRefresh={handleRefresh}
          />
        </ResizablePanel>

        <ResizableHandle withHandle />

        {/* Detail / compose pane */}
        <ResizablePanel id="detail-pane" defaultSize="50%" minSize="30%">
          <div className="h-full px-2 min-h-0">
            <DetailPanelContent
              paneMode={paneMode || 'empty'}
              accountId={effectiveAccountId}
              messageUid={selectedMessageUid}
              accounts={accounts}
            />
          </div>
        </ResizablePanel>

      </ResizablePanelGroup>
    </div>
  );
}