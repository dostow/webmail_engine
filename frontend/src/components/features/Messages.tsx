import { useNavigate, useLoaderData, useNavigation, useSearchParams } from 'react-router-dom';
import React from 'react';
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from '@/components/ui/resizable';
import { useTriageStore } from './messages/useTriageStore';
import { useMessageList } from './messages/useMessageList';
import { MessageList } from './messages/MessageList';
import { MessageDetailPane } from './messages/MessageDetailPane';
import { ComposePane } from './messages/ComposePane';
import { FolderPane } from './messages/FolderPane';
import type { Account, Message } from '@/types';

interface LoaderData {
  accounts: Account[];
  messages: Message[];
  total: number;
  selectedAccountId: string | null;
}

const MESSAGES_PER_PAGE = 50;

export function MessagesView() {
  const navigate = useNavigate();
  const { accounts, messages: loaderMessages, total: loaderTotal, selectedAccountId: loaderAccountId } = useLoaderData() as LoaderData;
  const navigation = useNavigation();
  const messageListStore = useMessageList();

  const loading = navigation.state === 'loading';

  const { selectedAccountId, selectedMessageUid, paneMode, setAccount } = useTriageStore();
  const [searchParams, setSearchParams] = useSearchParams();
  const selectedFolder = searchParams.get('folder') || 'INBOX';
  const page = parseInt(searchParams.get('page') || '1', 10);

  // Effective account ID: prefer triage store value (user interacted), fallback to URL/loader
  const effectiveAccountId = selectedAccountId || loaderAccountId;

  // Sync folder with message list store on mount
  React.useEffect(() => {
    if (selectedFolder && messageListStore.folder !== selectedFolder) {
      messageListStore.setFolder(selectedFolder);
    }
  }, [selectedFolder]);

  // Sync account with message list store
  React.useEffect(() => {
    if (effectiveAccountId && messageListStore.accountId !== effectiveAccountId) {
      messageListStore.setAccount(effectiveAccountId);
    }
  }, [effectiveAccountId]);

  // Sync page with message list store - trigger fetch when page changes
  React.useEffect(() => {
    if (messageListStore.accountId && messageListStore.currentPage !== page) {
      messageListStore.setPage(page);
    }
  }, [page, messageListStore.accountId, messageListStore.currentPage]);

  const totalPages = Math.max(1, Math.ceil((messageListStore.total || loaderTotal) / MESSAGES_PER_PAGE));

  const handleAccountChange = (accountId: string) => {
    setAccount(accountId);
    navigate(`/messages?accountId=${accountId}&folder=${selectedFolder}`);
  };

  const handlePageChange = (newPage: number) => {
    const params = new URLSearchParams(searchParams);
    params.set('page', newPage.toString());
    if (effectiveAccountId) params.set('accountId', effectiveAccountId);
    if (selectedFolder) params.set('folder', selectedFolder);
    navigate(`/messages?${params.toString()}`);
  };

  const handleFolderChange = (folder: string) => {
    // Update message list store (which will fetch new messages)
    messageListStore.setFolder(folder);
    // Update URL params
    const params = new URLSearchParams(searchParams);
    if (effectiveAccountId) params.set('accountId', effectiveAccountId);
    params.set('folder', folder);
    params.set('page', '1'); // Reset to first page when changing folders
    setSearchParams(params);
  };

  const handleRefresh = () => {
    messageListStore.refresh();
  };

  // Use messages from message list store when available, otherwise use loader data
  // The store is the single source of truth once the user interacts with the UI
  const displayMessages = messageListStore.messages.length > 0 ? messageListStore.messages : loaderMessages;
  const displayTotal = messageListStore.total > 0 ? messageListStore.total : loaderTotal;
  const displayLoading = messageListStore.loading || (messageListStore.accountId && loading);

  return (
    <div className="w-full h-full flex flex-col min-h-0">
      <ResizablePanelGroup
        orientation="horizontal"
        className="flex-1 min-h-0 w-full"
      >
        {/* Left: Folder Pane */}
        <ResizablePanel
          id="folder-list"
          defaultSize={"15%"}
          minSize={30}
          maxSize={200}
          className="no-scrollbar"
        >
          <div className="h-full border-r bg-muted/10">
            {effectiveAccountId ? (
              <FolderPane
                accountId={effectiveAccountId}
                selectedFolder={selectedFolder}
                onSelectFolder={handleFolderChange}
              />
            ) : (
              <div className="p-4 text-sm text-muted-foreground text-center">
                Select an account to view folders
              </div>
            )}
          </div>
        </ResizablePanel>
        <ResizableHandle withHandle />
        {/* Middle: Message List */}
        <ResizablePanel
          id="message-list"
          defaultSize={"30%"}
          minSize={"30%"}
          maxSize={"50%"}
          className='no-scrollbar'
        >
          <MessageList
            accounts={accounts}
            messages={displayMessages}
            total={displayTotal}
            page={page}
            totalPages={totalPages}
            loading={displayLoading}
            folder={selectedFolder}
            onAccountChange={handleAccountChange}
            onPageChange={handlePageChange}
            onRefresh={handleRefresh}
          />
        </ResizablePanel>
        <ResizableHandle withHandle />
        {/* Right: Detail or Compose pane (always rendered, content toggled) */}
        <ResizablePanel
          id="detail-pane"
          defaultSize={"50%"}
          minSize={"30%"}
        >
          <div className="h-full px-2 min-h-0">
            {paneMode === 'detail' && effectiveAccountId && selectedMessageUid ? (
              <MessageDetailPane
                accountId={effectiveAccountId}
                messageUid={selectedMessageUid}
              />
            ) : paneMode === 'compose' ? (
              <ComposePane accounts={accounts} />
            ) : (
              <div className="h-full flex items-center justify-center text-muted-foreground text-sm">
                <div className="text-center">
                  <div className="text-4xl mb-2">📬</div>
                  <p>Select a message to read</p>
                </div>
              </div>
            )}
          </div>
        </ResizablePanel>
      </ResizablePanelGroup>
    </div>
  );
}
