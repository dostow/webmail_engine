import { useNavigate, useParams, useLoaderData } from 'react-router-dom';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Badge } from '@/components/ui/badge';
import { Avatar } from '@/components/ui/avatar';
import { ScrollArea } from '@/components/ui/scroll-area';
import { Separator } from '@/components/ui/separator';
import { formatFullDate } from '@/utils/format';
import { useEmailToast } from '@/hooks/useToast';
import type { MessageDetail as MessageDetailType } from '@/types';

export function MessageDetail() {
  const navigate = useNavigate();
  const { accountId, messageUid } = useParams<{ accountId: string; messageUid: string }>();
  const message = useLoaderData() as MessageDetailType;
  const { showError, showSuccess } = useEmailToast();

  const handleDelete = async () => {
    if (!accountId || !messageUid) return;

    if (!confirm('Are you sure you want to delete this message?')) return;

    try {
      // TODO: Implement delete API endpoint
      showSuccess('Message deleted successfully');
      navigate('/messages');
    } catch (err) {
      showError('Failed to delete message');
    }
  };

  const handleReply = () => {
    if (!message) return;
    const replyTo = message.from?.address || '';
    const subject = message.subject?.startsWith('Re:') ? message.subject : `Re: ${message.subject}`;
    navigate(`/compose?to=${encodeURIComponent(replyTo)}&subject=${encodeURIComponent(subject)}`);
  };

  const handleForward = () => {
    if (!message) return;
    const subject = message.subject?.startsWith('Fwd:') ? message.subject : `Fwd: ${message.subject}`;
    navigate(`/compose?subject=${encodeURIComponent(subject || '')}`);
  };

  const getInitials = (name: string) => {
    return name
      .split(' ')
      .map((n) => n[0])
      .join('')
      .toUpperCase()
      .slice(0, 2);
  };

  const getSenderName = (): string => {
    if (!message) return 'Unknown';
    if (message.from?.name) return message.from.name;
    if (message.from?.address) return message.from.address;
    return 'Unknown';
  };

  const getRecipientName = (): string => {
    if (!message) return 'Me';
    if (message.to?.[0]?.name) return message.to[0].name;
    if (message.to?.[0]?.address) return message.to[0].address;
    return 'Me';
  };

  if (!message) {
    return (
      <div className="message-detail h-full flex flex-col min-h-0">
        <Card className="p-6 flex-1 flex flex-col items-center justify-center">
          <div className="text-center py-8">
            <p className="text-muted-foreground mb-4">Message not found</p>
            <Button onClick={() => navigate('/messages')}>Back to Messages</Button>
          </div>
        </Card>
      </div>
    );
  }

  return (
    <div className="message-detail h-full flex flex-col min-h-0">
      <Card className="p-6 flex-1 min-h-0 flex flex-col overflow-hidden">
        {/* Header */}
        <div className="flex items-start justify-between mb-4 shrink-0">
          <div className="flex items-center gap-3">
            <Avatar className="h-10 w-10">
              <div className="h-full w-full bg-primary text-primary-foreground flex items-center justify-center text-sm font-medium">
                {getInitials(getSenderName())}
              </div>
            </Avatar>
            <div>
              <div className="font-medium">
                {getSenderName()}
              </div>
              <div className="text-sm text-muted-foreground">
                to {getRecipientName()}
              </div>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <Badge variant="secondary">INBOX</Badge>
          </div>
        </div>

        {/* Subject */}
        <div className="mb-4 shrink-0">
          <h2 className="text-xl font-semibold">{message.subject || '(No subject)'}</h2>
          <div className="text-sm text-muted-foreground mt-1">
            {formatFullDate(message.date)}
          </div>
        </div>

        <Separator className="my-4" />

        {/* Action Buttons */}
        <div className="flex gap-2 mb-4 shrink-0">
          <Button onClick={handleReply} variant="outline" size="sm">
            <svg className="mr-2 h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M3 10h10a8 8 0 018 8v2M3 10l6 6m-6-6l6-6" />
            </svg>
            Reply
          </Button>
          <Button onClick={handleForward} variant="outline" size="sm">
            <svg className="mr-2 h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M13 5l7 7-7 7M5 5l7 7-7 7" />
            </svg>
            Forward
          </Button>
          <Button onClick={handleDelete} variant="destructive" size="sm">
            <svg className="mr-2 h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
            </svg>
            Delete
          </Button>
          <Button onClick={() => navigate('/messages')} variant="ghost" size="sm">
            Back
          </Button>
        </div>

        {/* Message Body */}
        <ScrollArea className="flex-1 min-h-0 w-full rounded-md border p-4">
          {message.html_body ? (
            <div dangerouslySetInnerHTML={{ __html: message.html_body }} className="prose dark:prose-invert max-w-none" />
          ) : (
            <pre className="whitespace-pre-wrap font-sans text-sm">{message.text_body}</pre>
          )}
        </ScrollArea>

        {/* Attachments */}
        {message.attachments && message.attachments.length > 0 && (
          <div className="shrink-0 mt-4">
            <Separator className="mb-4" />
            <div>
              <h3 className="text-sm font-medium mb-2">Attachments ({message.attachments.length})</h3>
              <div className="flex flex-wrap gap-2">
                {message.attachments.map((attachment, index) => (
                  <Card key={index} className="p-3 flex items-center gap-2 cursor-pointer hover:bg-accent">
                    <svg className="h-5 w-5 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M15.172 7l-6.586 6.586a2 2 0 102.828 2.828l6.414-6.586a4 4 0 00-5.656-5.656l-6.415 6.585a6 6 0 108.486 8.486L20.5 13" />
                    </svg>
                    <div className="text-sm">
                      <div className="font-medium">{attachment.filename}</div>
                      <div className="text-muted-foreground">{(attachment.size / 1024).toFixed(1)} KB</div>
                    </div>
                  </Card>
                ))}
              </div>
            </div>
          </div>
        )}
      </Card>
    </div>
  );
}
