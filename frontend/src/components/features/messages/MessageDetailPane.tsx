import { useState, useEffect, useCallback, useRef } from 'react';
import { Button } from '@/components/ui/Button';
import { Avatar } from '@/components/ui/avatar';
import { Badge } from '@/components/ui/badge';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { useTriageStore } from './useTriageStore';
import { useMessageList } from './useMessageList';
import { useEmailToast } from '@/hooks/useToast';
import { formatFullDate } from '@/utils/format';
import * as api from '@/services/api';
import type { Message, MessageDetail } from '@/types';

interface MessageDetailPaneProps {
  accountId: string;
  messageUid: string;
}

function getInitials(name: string) {
  return name
    .split(' ')
    .map((n) => n[0])
    .join('')
    .toUpperCase()
    .slice(0, 2);
}

function getSenderDisplayName(from?: Message['from']): string {
  if (!from) return 'Unknown';
  return from.name || from.address || 'Unknown';
}

function getRecipientDisplayName(to?: MessageDetail['to']): string {
  if (!to?.[0]) return 'Me';
  return to[0].name || to[0].address || 'Me';
}

/** Displays email headers in a modal dialog. */
function EmailHeadersDialog({
  headers,
  open,
  onOpenChange,
}: {
  headers: Record<string, string> | undefined;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const headerEntries = headers ? Object.entries(headers) : [];

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-4xl lg:min-w-[40vw] max-h-[80vh] overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle>Email Headers</DialogTitle>
        </DialogHeader>
        <div className="flex-1 overflow-y-auto mt-2 p-2 no-scrollbar">
          {headerEntries.length > 0 ? (
            <div className="flex flex-col gap-1.5">
              {headerEntries.map(([key, value]) => (
                <div
                  key={key}
                  className="flex gap-3 py-2 border-b last:border-0"
                >
                  <span className="font-semibold text-muted-foreground shrink-0 sm:w-20 w-30">
                    {key}
                  </span>
                  <span className="break-all whitespace-pre-wrap">
                    {value}
                  </span>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-sm text-muted-foreground text-center py-8">
              No headers available
            </p>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

/** Renders HTML email body in a sandboxed iframe that fills the available space. */
function EmailIframe({ html }: { html: string }) {
  const iframeRef = useRef<HTMLIFrameElement>(null);

  // Apply base styles inside the iframe to match our theme
  const wrappedHtml = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<style>
  * { box-sizing: border-box; }
  body {
    margin: 0;
    padding: 16px;
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, sans-serif;
    font-size: 14px;
    line-height: 1.6;
    color: #1a1a1a;
    word-break: break-word;
    overflow-wrap: break-word;
  }
  img { max-width: 100%; height: auto; }
  a { color: #2563eb; }
  pre { white-space: pre-wrap; word-break: break-word; }
  table { max-width: 100% !important; }
</style>
</head>
<body>${html}</body>
</html>`;

  useEffect(() => {
    const iframe = iframeRef.current;
    if (!iframe) return;

    const handleLoad = () => {
      iframe.contentWindow?.addEventListener('click', (e: MouseEvent) => {
        const target = e.target as HTMLElement;
        const link = target.closest('a');
        if (link) {
          e.preventDefault();
          e.stopPropagation();
          const href = link.getAttribute('href');
          if (href) {
            window.open(href, '_blank', 'noopener,noreferrer');
          }
        }
      });
    };

    iframe.addEventListener('load', handleLoad);
    return () => iframe.removeEventListener('load', handleLoad);
  }, []);

  return (
    <iframe
      ref={iframeRef}
      srcDoc={wrappedHtml}
      sandbox="allow-same-origin"
      title="Email body"
      className="w-full h-full border-0"
    />
  );
}

export function MessageDetailPane({ accountId, messageUid }: MessageDetailPaneProps) {
  const [detail, setDetail] = useState<MessageDetail | null>(null);
  const [bodyLoading, setBodyLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [headersOpen, setHeadersOpen] = useState(false);
  const [markedAsRead, setMarkedAsRead] = useState(false);

  // Get folder and pagination info from message list store
  const { folder, currentCursor, pageSize } = useMessageList();

  // Pull the list-row data from the store for instant header rendering
  const { selectedMessage: initialMessage, openCompose, clearAll } = useTriageStore();
  const { showError, showSuccess } = useEmailToast();

  const fetchDetail = useCallback(async () => {
    setBodyLoading(true);
    setError(null);
    try {
      const data = await api.getMessage(accountId, messageUid, folder);
      setDetail(data);
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to load message';
      setError(msg);
      showError(msg);
    } finally {
      setBodyLoading(false);
    }
  }, [accountId, messageUid, folder, showError]);

  useEffect(() => {
    // Reset previous detail when switching messages
    setDetail(null);
    setBodyLoading(true);
    setError(null);
    setMarkedAsRead(false);
    fetchDetail();
  }, [fetchDetail]);

  // Mark message as read when it's loaded and viewed
  useEffect(() => {
    if (detail && !markedAsRead) {
      const isUnread = detail.flags && !detail.flags.some(f => f.toLowerCase() === 'seen');
      if (isUnread) {
        const cacheContext = currentCursor ? {
          cursor: currentCursor,
          limit: pageSize || 50,
          sort_by: 'date',
          sort_order: 'desc'
        } : undefined;

        api.markMessageRead(accountId, messageUid, folder, cacheContext).catch(() => {
          // Silently fail - marking as read is not critical
        });
        setMarkedAsRead(true);
      }
    }
  }, [detail, accountId, messageUid, folder, markedAsRead]);

  // --- Header data: use detail if loaded, else fall back to the list-row snapshot ---
  const headerFrom = detail?.from ?? initialMessage?.from;
  const headerTo = detail?.to ?? undefined;
  const headerSubject = detail?.subject ?? initialMessage?.subject;
  const headerDate = detail?.date ?? initialMessage?.date;
  const senderName = getSenderDisplayName(headerFrom);

  const handleReply = () => {
    const replyTo = headerFrom?.address || '';
    const subject = headerSubject?.startsWith('Re:') ? headerSubject : `Re: ${headerSubject}`;
    openCompose({ to: replyTo, subject, isReply: true });
  };

  const handleForward = () => {
    const subject = headerSubject?.startsWith('Fwd:') ? headerSubject : `Fwd: ${headerSubject}`;
    openCompose({ subject });
  };

  const handleDelete = async () => {
    if (!confirm('Are you sure you want to delete this message?')) return;
    try {
      // TODO: implement delete API
      showSuccess('Message deleted');
      clearAll();
    } catch {
      showError('Failed to delete message');
    }
  };

  // If we have no initial list-row data AND body is still loading, show full skeleton
  const noHeaderYet = !initialMessage && bodyLoading;

  if (noHeaderYet) {
    return (
      <div className="h-full flex flex-col min-h-0 p-4 gap-4 bg-card rounded-lg border">
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-6 w-3/4" />
        <Skeleton className="h-4 w-1/2" />
        <Skeleton className="flex-1" />
      </div>
    );
  }

  return (
    <div className="message-detail-pane h-full flex flex-col min-h-0 bg-card rounded-lg border">
      {/* ── Sticky header (instantly visible from list-row data) ── */}
      <div className="shrink-0 px-5 pt-5 pb-3 border-b">
        {/* Sender row */}
        <div className="flex items-start justify-between gap-3 mb-3">
          <div className="flex items-center gap-3 min-w-0">
            <Avatar className="h-9 w-9 shrink-0">
              <div className="h-full w-full bg-primary text-primary-foreground flex items-center justify-center text-xs font-semibold">
                {getInitials(senderName)}
              </div>
            </Avatar>
            <div className="min-w-0">
              <div className="font-semibold text-sm truncate">{senderName}</div>
              <div className="text-xs text-muted-foreground truncate">
                {headerFrom?.address}
                {headerTo?.[0] && <> → {getRecipientDisplayName(headerTo)}</>}
              </div>
            </div>
          </div>
          <div className="flex items-center gap-2 shrink-0">
            <Badge variant="secondary" className="text-xs">{folder || 'INBOX'}</Badge>
            <span className="text-xs text-muted-foreground whitespace-nowrap">
              {headerDate ? formatFullDate(headerDate) : '—'}
            </span>
          </div>
        </div>

        {/* Subject */}
        <h2 className="text-base font-bold leading-tight mb-3">
          {headerSubject || '(No subject)'}
        </h2>

        {/* Actions */}
        <div className="flex items-center gap-1.5">
          <Button onClick={handleReply} variant="outline" size="sm" className="h-7 text-xs gap-1">
            <svg className="h-3 w-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M3 10h10a8 8 0 018 8v2M3 10l6 6m-6-6l6-6" />
            </svg>
            Reply
          </Button>
          <Button onClick={handleForward} variant="outline" size="sm" className="h-7 text-xs gap-1">
            <svg className="h-3 w-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M13 5l7 7-7 7M5 5l7 7-7 7" />
            </svg>
            Forward
          </Button>
          <Button
            onClick={() => setHeadersOpen(true)}
            variant="outline"
            size="sm"
            className="h-7 text-xs gap-1"
            disabled={!detail?.headers}
          >
            <svg className="h-3 w-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            Headers
          </Button>
          <Button
            onClick={handleDelete}
            variant="ghost"
            size="sm"
            className="h-7 text-xs text-destructive hover:text-destructive gap-1 ml-auto"
          >
            <svg className="h-3 w-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
            </svg>
            Delete
          </Button>
          <Button onClick={clearAll} variant="ghost" size="sm" className="h-7 w-7 p-0 text-muted-foreground">
            <svg className="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </Button>
        </div>
      </div>

      {/* ── Body area ── */}
      <div className="flex-1 min-h-0 overflow-y-auto bg-white/90 flex flex-col">
        {bodyLoading ? (
          // Body still loading — show skeleton below the already-visible header
          <div className="p-5 flex flex-col gap-3">
            <Skeleton className="h-4 w-full" />
            <Skeleton className="h-4 w-5/6" />
            <Skeleton className="h-4 w-3/4" />
            <Skeleton className="h-4 w-full" />
            <Skeleton className="h-4 w-4/5" />
          </div>
        ) : error ? (
          <div className="flex flex-col items-center justify-center gap-2 p-8 text-muted-foreground">
            <div className="text-3xl">⚠️</div>
            <p className="text-sm">{error}</p>
            <Button variant="outline" size="sm" onClick={fetchDetail}>Retry</Button>
          </div>
        ) : detail?.html_body ? (
          // HTML body rendered in sandboxed iframe — fills available space
          <div className="flex-1 min-h-0">
            <EmailIframe html={detail.html_body} />
          </div>
        ) : (
          <pre className="flex-1 min-h-0 bg-background p-5 whitespace-pre-wrap font-sans text-sm leading-relaxed text-foreground overflow-y-auto">
            {detail?.text_body || '(No content)'}
          </pre>
        )}
      </div>

      {/* ── Attachments ── */}
      {detail?.attachments && detail.attachments.length > 0 && (
        <div className="shrink-0 border-t p-4">
          <p className="text-xs font-medium text-muted-foreground mb-2">
            {detail.attachments.length} Attachment{detail.attachments.length > 1 ? 's' : ''}
          </p>
          <div className="flex flex-wrap gap-2">
            {detail.attachments.map((att, i) => (
              <button
                key={i}
                onClick={async (e) => {
                  e.preventDefault();
                  try {
                    await api.downloadAttachment(accountId, messageUid, att.id, att.filename);
                  } catch (err) {
                    showError('Failed to download attachment');
                  }
                }}
                className="flex items-center gap-1.5 px-2.5 py-1.5 rounded-md border bg-muted/40 text-xs cursor-pointer hover:bg-muted hover:border-primary transition-colors"
              >
                <svg className="h-3.5 w-3.5 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M15.172 7l-6.586 6.586a2 2 0 102.828 2.828l6.414-6.586a4 4 0 00-5.656-5.656l-6.415 6.585a6 6 0 108.486 8.486L20.5 13" />
                </svg>
                <span className="font-medium truncate max-w-35">{att.filename}</span>
                <span className="text-muted-foreground shrink-0">{(att.size / 1024).toFixed(1)} KB</span>
              </button>
            ))}
          </div>
        </div>
      )}

      {/* Email Headers Modal */}
      <EmailHeadersDialog
        headers={detail?.headers}
        open={headersOpen}
        onOpenChange={setHeadersOpen}
      />
    </div>
  );
}
