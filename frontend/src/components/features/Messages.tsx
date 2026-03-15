import { useNavigate, useLoaderData, useNavigation, useSearchParams } from 'react-router-dom';
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from '@/components/ui/resizable';
import { useTriageStore } from './messages/useTriageStore';
import { MessageList } from './messages/MessageList';
import { MessageDetailPane } from './messages/MessageDetailPane';
import { ComposePane } from './messages/ComposePane';
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
  const { accounts, messages, total, selectedAccountId: loaderAccountId } = useLoaderData() as LoaderData;
  const navigation = useNavigation();
  const [searchParams] = useSearchParams();

  const loading = navigation.state === 'loading';

  const { selectedAccountId, selectedMessageUid, paneMode, setAccount } = useTriageStore();

  // Effective account ID: prefer triage store value (user interacted), fallback to URL/loader
  const effectiveAccountId = selectedAccountId || loaderAccountId;

  const page = parseInt(searchParams.get('page') || '1', 10);
  const totalPages = Math.max(1, Math.ceil(total / MESSAGES_PER_PAGE));

  const handleAccountChange = (accountId: string) => {
    setAccount(accountId);
    navigate(`/messages?accountId=${accountId}`);
  };

  const handlePageChange = (newPage: number) => {
    const params = new URLSearchParams(searchParams);
    params.set('page', newPage.toString());
    if (effectiveAccountId) params.set('accountId', effectiveAccountId);
    navigate(`/messages?${params.toString()}`);
  };

  const handleRefresh = () => {
    const params = new URLSearchParams(searchParams);
    if (effectiveAccountId) params.set('accountId', effectiveAccountId);
    navigate(`/messages?${params.toString()}`);
  };

  const showRightPane = paneMode !== null;

  return (
    <div className="h-full flex flex-col min-h-0">
      <ResizablePanelGroup
        orientation="horizontal"
        className="flex-1 min-h-0"
      >
        {/* Left: Message List */}
        <ResizablePanel
          defaultSize={showRightPane ? 35 : 100}
          minSize={25}
          className="min-h-0"
        >
          <MessageList
            accounts={accounts}
            messages={messages}
            total={total}
            page={page}
            totalPages={totalPages}
            loading={loading}
            onAccountChange={handleAccountChange}
            onPageChange={handlePageChange}
            onRefresh={handleRefresh}
          />
        </ResizablePanel>

        {/* Right: Detail or Compose pane */}
        {showRightPane && (
          <>
            <ResizableHandle withHandle />
            <ResizablePanel defaultSize={65} minSize={30} className="min-h-0">
              <div className="h-full p-2 min-h-0">
                {paneMode === 'detail' && effectiveAccountId && selectedMessageUid ? (
                  <MessageDetailPane
                    accountId={effectiveAccountId}
                    messageUid={selectedMessageUid}
                  />
                ) : paneMode === 'compose' ? (
                  <ComposePane accounts={accounts} />
                ) : null}
              </div>
            </ResizablePanel>
          </>
        )}
      </ResizablePanelGroup>
    </div>
  );
}
