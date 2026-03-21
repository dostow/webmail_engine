import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Avatar } from '@/components/ui/avatar';
import { ScrollArea, ScrollBar } from '@/components/ui/scroll-area';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { cn } from '@/lib/utils';
import { formatMessageDate } from '@/utils/format';
import { useMessages } from '@/hooks/useMessages';
import { useTriageStore } from './useTriageStore';
import type { Account, Message } from '@/types';

interface MessageListProps {
  accounts: Account[];
  messages: Message[];
  total: number;
  page: number;
  totalPages: number;
  loading: boolean;
  folder?: string;
  onAccountChange: (accountId: string) => void;
  onPageChange: (page: number) => void;
  onRefresh: () => void;
}

const MESSAGES_PER_PAGE = 50;

function getInitials(name: string) {
  return name.split(' ').map((n) => n[0]).join('').toUpperCase().slice(0, 2);
}

function getSenderName(message: Message): string {
  if (message.from?.name) return message.from.name;
  if (message.from?.address) return message.from.address;
  return 'Unknown';
}

function isUnread(message: Message) {
  return message.flags && !message.flags.some(f => f.toLowerCase() === 'seen');
}

export function MessageList({
  accounts,
  messages,
  total,
  page,
  totalPages,
  loading,
  folder,
  onAccountChange,
  onPageChange,
  onRefresh,
}: MessageListProps) {
  const { selectedAccountId, selectedMessageUid, selectMessage, openCompose } = useTriageStore();
  const { filterMessages, filters, updateFilters, clearFilters } = useMessages();

  const filteredMessages = filterMessages(messages, filters);
  const hasActiveFilters = Object.values(filters).some((v) => v !== undefined && v !== false);

  const handleRowClick = (message: Message) => {
    if (selectedAccountId) {
      selectMessage(selectedAccountId, message.uid, message);
    }
  };

  return (
    <div className="h-full flex flex-col min-h-0">
      {/* Account selector + actions */}
      <div className="shrink-0 flex items-center justify-between gap-3 px-3 py-2.5 border-b bg-muted/20">
        <div className="flex items-center gap-3 flex-1">
          <Select value={selectedAccountId || ''} onValueChange={onAccountChange}>
            <SelectTrigger className="h-8 text-sm flex-1 max-w-65">
              <SelectValue placeholder="Select account…" />
            </SelectTrigger>
            <SelectContent>
              {accounts.map((a) => (
                <SelectItem key={a.id} value={a.id}>{a.email}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          {folder && (
            <span className="text-xs text-muted-foreground font-medium">
              📁 {folder}
            </span>
          )}
        </div>
        <div className="flex items-center gap-1">
          <Button
            variant="ghost"
            size="icon"
            className="h-8 w-8"
            title="Compose"
            onClick={() => openCompose()}
          >
            <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
            </svg>
          </Button>
          <Button
            variant="ghost"
            size="icon"
            className="h-8 w-8"
            title="Refresh"
            onClick={onRefresh}
            disabled={loading}
          >
            <svg className={cn("h-4 w-4", loading && "animate-spin")} fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
          </Button>
        </div>
      </div>

      {/* Filters */}
      <div className="shrink-0 flex items-center gap-2 px-3 py-2 border-b bg-background">
        <Input
          placeholder="Search…"
          value={filters.search || ''}
          onChange={(e) => updateFilters({ search: e.target.value })}
          className="h-7 text-xs flex-1"
        />
        <Button
          variant={filters.unreadOnly ? 'default' : 'outline'}
          size="sm"
          className="h-7 text-xs px-2 shrink-0"
          onClick={() => updateFilters({ unreadOnly: !filters.unreadOnly })}
        >
          Unread
        </Button>
        <Button
          variant={filters.hasAttachments ? 'default' : 'outline'}
          size="sm"
          className="h-7 text-xs px-2 shrink-0"
          onClick={() => updateFilters({ hasAttachments: !filters.hasAttachments })}
        >
          📎
        </Button>
        {hasActiveFilters && (
          <Button
            variant="ghost"
            size="sm"
            className="h-7 text-xs px-2 shrink-0 text-muted-foreground"
            onClick={clearFilters}
          >
            ✕
          </Button>
        )}
      </div>

      {/* Filter summary */}
      {hasActiveFilters && (
        <div className="shrink-0 px-3 py-1 text-[11px] text-muted-foreground bg-muted/10 border-b">
          {filteredMessages.length} of {messages.length} messages
        </div>
      )}

      {/* Message table/states */}
      {!selectedAccountId ? (
        <div className="flex-1 flex flex-col items-center justify-center gap-2 text-muted-foreground">
          <div className="text-4xl">📬</div>
          <p className="text-sm">Select an account to view messages</p>
        </div>
      ) : loading && messages.length === 0 ? (
        <div className="flex-1 flex items-center justify-center text-muted-foreground text-sm">
          Loading…
        </div>
      ) : filteredMessages.length === 0 ? (
        <div className="flex-1 flex flex-col items-center justify-center gap-2 text-muted-foreground">
          <div className="text-4xl">📭</div>
          {hasActiveFilters ? (
            <div className="text-center">
              <p className="text-sm">No messages match your filters</p>
              <Button variant="link" size="sm" onClick={clearFilters}>Clear filters</Button>
            </div>
          ) : (
            <p className="text-sm">No messages in this folder</p>
          )}
        </div>
      ) : (
        <ScrollArea className="flex-1 min-h-0">
          <Table>
            <TableHeader className="sticky top-0 bg-background z-10">
              <TableRow>
                <TableHead className="w-[30px] px-2"></TableHead>
                <TableHead className="w-[36px] px-1"></TableHead>
                <TableHead className="w-[140px]">From</TableHead>
                <TableHead>Subject</TableHead>
                <TableHead className="w-[100px] text-right">
                  <span className="flex items-center justify-end gap-1">
                    Date
                    <svg className="h-3 w-3 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M15.172 7l-6.586 6.586a2 2 0 102.828 2.828l6.414-6.586a4 4 0 00-5.656-5.656l-6.415 6.585a6 6 0 108.486 8.486L20.5 13" />
                    </svg>
                  </span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filteredMessages.map((message) => {
                const isSelected = message.uid === selectedMessageUid;
                const unread = isUnread(message);
                return (
                  <TableRow
                    key={message.uid}
                    className={cn(
                      'cursor-pointer transition-colors',
                      isSelected
                        ? 'bg-primary/10 hover:bg-primary/15'
                        : unread
                          ? 'bg-accent/30 hover:bg-accent/50 font-medium'
                          : 'hover:bg-muted/40'
                    )}
                    onClick={() => handleRowClick(message)}
                  >
                    <TableCell className="px-2 py-2">
                      {unread && (
                        <div className="h-2 w-2 rounded-full bg-primary mx-auto" />
                      )}
                    </TableCell>
                    <TableCell className="px-1 py-2">
                      <Avatar className="h-7 w-7">
                        <div className="h-full w-full bg-secondary text-secondary-foreground flex items-center justify-center text-[10px] font-semibold">
                          {getInitials(getSenderName(message))}
                        </div>
                      </Avatar>
                    </TableCell>
                    <TableCell className="py-2 max-w-[140px]">
                      <span className={cn("block truncate text-xs", unread && "font-semibold")}>
                        {getSenderName(message)}
                      </span>
                    </TableCell>
                    <TableCell className="py-2 max-w-0">
                      <div className="flex flex-col truncate">
                        <span className={cn("truncate text-xs", unread && "font-semibold")}>
                          {message.subject || '(No subject)'}
                        </span>
                        {message.has_attachments && (
                          <div className="flex items-center gap-1 mt-0.5">
                            <svg className="h-3 w-3 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M15.172 7l-6.586 6.586a2 2 0 102.828 2.828l6.414-6.586a4 4 0 00-5.656-5.656l-6.415 6.585a6 6 0 108.486 8.486L20.5 13" />
                            </svg>
                            <span className="text-[10px] text-muted-foreground">
                              Has attachments
                            </span>
                          </div>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="py-2 text-right text-[10px] text-muted-foreground whitespace-nowrap">
                      {formatMessageDate(message.date)}
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
          <ScrollBar orientation="vertical" />
        </ScrollArea>
      )}

      {/* Pagination footer */}
      <div className="shrink-0 flex items-center justify-between border-t px-3 py-1.5 bg-muted/10 text-[11px] text-muted-foreground">
        <span>{total > 0 ? `${Math.min(page * MESSAGES_PER_PAGE, total)} / ${total}` : '—'}</span>
        <div className="flex items-center gap-0.5">
          <Button variant="ghost" size="icon" className="h-6 w-6" onClick={() => onPageChange(page - 1)} disabled={page <= 1}>
            <svg className="h-2.5 w-2.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M15 19l-7-7 7-7" />
            </svg>
          </Button>
          <span className="px-1">{page} / {totalPages}</span>
          <Button variant="ghost" size="icon" className="h-6 w-6" onClick={() => onPageChange(page + 1)} disabled={page >= totalPages}>
            <svg className="h-2.5 w-2.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M9 5l7 7-7 7" />
            </svg>
          </Button>
        </div>
      </div>
    </div>
  );
}
