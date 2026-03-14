import { useNavigate, useLoaderData, useNavigation } from 'react-router-dom';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Avatar } from '@/components/ui/avatar';
import { ScrollArea } from '@/components/ui/scroll-area';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { formatMessageDate } from '@/utils/format';
import type { Account, Message } from '@/types';

interface LoaderData {
  accounts: Account[];
  messages: Message[];
  total: number;
  selectedAccountId: string | null;
}

export function MessagesView() {
  const navigate = useNavigate();
  const { accounts, messages, selectedAccountId } = useLoaderData() as LoaderData;
  const navigation = useNavigation();

  const loading = navigation.state === 'loading';

  const handleAccountChange = (value: string) => {
    navigate(`/messages?accountId=${value}`);
  };

  const handleMessageClick = (message: Message) => {
    navigate(`/messages/${selectedAccountId}/${message.uid}`);
  };

  const getInitials = (name: string) => {
    return name
      .split(' ')
      .map((n) => n[0])
      .join('')
      .toUpperCase()
      .slice(0, 2);
  };

  const isUnread = (message: Message) => {
    return message.flags && !message.flags.includes('\\Seen');
  };

  const getSenderName = (message: Message): string => {
    if (message.from?.name) return message.from.name;
    if (message.from?.address) return message.from.address;
    return 'Unknown';
  };

  return (
    <div className="messages-view">
      <Card className="mb-4">
        <div className="flex items-center justify-between p-4">
          <div className="flex items-center gap-4">
            <Select value={selectedAccountId || ''} onValueChange={handleAccountChange}>
              <SelectTrigger className="w-[300px]">
                <SelectValue placeholder="Select an account..." />
              </SelectTrigger>
              <SelectContent>
                {accounts.map((account) => (
                  <SelectItem key={account.id} value={account.id}>
                    {account.email}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <Button
            variant="outline"
            onClick={() => window.location.reload()}
            disabled={!selectedAccountId || loading}
          >
            <svg className="mr-2 h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
            Refresh
          </Button>
        </div>
      </Card>

      <Card>
        {loading ? (
          <div className="flex items-center justify-center p-8">
            <p className="text-muted-foreground">Loading messages...</p>
          </div>
        ) : !selectedAccountId ? (
          <div className="flex flex-col items-center justify-center p-8">
            <div className="text-4xl mb-4">📬</div>
            <p className="text-muted-foreground">Select an account to view messages.</p>
          </div>
        ) : messages.length === 0 ? (
          <div className="flex flex-col items-center justify-center p-8">
            <div className="text-4xl mb-4">📭</div>
            <p className="text-muted-foreground">No messages in this folder.</p>
          </div>
        ) : (
          <ScrollArea className="h-[calc(100vh-250px)]">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-[50px]"></TableHead>
                  <TableHead className="w-[50px]"></TableHead>
                  <TableHead className="w-[200px]">Sender</TableHead>
                  <TableHead>Subject</TableHead>
                  <TableHead className="w-[150px]">Date</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {messages.map((message) => (
                  <TableRow
                    key={message.uid}
                    className={`cursor-pointer hover:bg-accent ${isUnread(message) ? 'bg-accent/50 font-medium' : ''}`}
                    onClick={() => handleMessageClick(message)}
                  >
                    <TableCell>
                      {isUnread(message) && (
                        <div className="h-2 w-2 rounded-full bg-primary" />
                      )}
                    </TableCell>
                    <TableCell>
                      <Avatar className="h-8 w-8">
                        <div className="h-full w-full bg-secondary text-secondary-foreground flex items-center justify-center text-xs font-medium">
                          {getInitials(getSenderName(message))}
                        </div>
                      </Avatar>
                    </TableCell>
                    <TableCell className="max-w-[200px] truncate">
                      <div className="font-medium">
                        {getSenderName(message)}
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-col">
                        <span className="font-medium">
                          {message.subject || '(No subject)'}
                        </span>
                        {message.preview && (
                          <span className="text-sm text-muted-foreground truncate max-w-md">
                            {message.preview}
                          </span>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="text-right text-muted-foreground">
                      {formatMessageDate(message.date)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </ScrollArea>
        )}
      </Card>
    </div>
  );
}
