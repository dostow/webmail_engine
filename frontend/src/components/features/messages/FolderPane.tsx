import { cn } from '@/lib/utils';
import { useFolders, type FolderInfo, type FolderTreeNode } from '@/hooks/useFolders';
import { useTriageStore } from './useTriageStore';
import { ScrollArea } from '@/components/ui/scroll-area';
import { Button } from '@/components/ui/Button';
import { Skeleton } from '@/components/ui/skeleton';
import { useState } from 'react';

interface FolderPaneProps {
  accountId: string;
  selectedFolder: string;
  onSelectFolder: (folder: string) => void;
}

function getFolderIcon(folderName: string) {
  const name = folderName.toUpperCase();
  switch (name) {
    case 'INBOX':
      return (
        <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M20 13V6a2 2 0 00-2-2H6a2 2 0 00-2 2v7m16 0v5a2 2 0 01-2 2H6a2 2 0 01-2-2v-5m16 0h-2.586a1 1 0 00-.707.293l-2.414 2.414a1 1 0 01-.707.293h-3.172a1 1 0 01-.707-.293l-2.414-2.414A1 1 0 006.586 13H4" />
        </svg>
      );
    case 'SENT':
      return (
        <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 19l9 2-9-18-9 18 9-2zm0 0v-8" />
        </svg>
      );
    case 'DRAFTS':
      return (
        <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
        </svg>
      );
    case 'TRASH':
      return (
        <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
        </svg>
      );
    case 'JUNK':
    case 'SPAM':
      return (
        <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
        </svg>
      );
    case 'ARCHIVE':
      return (
        <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4" />
        </svg>
      );
    default:
      return (
        <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z" />
        </svg>
      );
  }
}

function getFolderDisplayName(folderName: string): string {
  // Always strip the full parent path and show only the last segment
  const parts = folderName.split('.');
  const displayName = parts[parts.length - 1];

  const name = displayName.toUpperCase();
  switch (name) {
    case 'INBOX':
      return 'Inbox';
    case 'DRAFTS':
      return 'Drafts';
    case 'SENT':
      return 'Sent';
    case 'TRASH':
      return 'Trash';
    case 'JUNK':
    case 'SPAM':
      return 'Junk';
    case 'ARCHIVE':
      return 'Archive';
    default:
      return displayName;
  }
}

function FolderItem({
  folder,
  isSelected,
  onClick,
  depth = 0,
  hasChildren = false,
  isExpanded = false,
  onToggleExpand,
}: {
  folder: FolderInfo;
  isSelected: boolean;
  onClick: () => void;
  depth?: number;
  hasChildren?: boolean;
  isExpanded?: boolean;
  onToggleExpand?: (e: React.MouseEvent) => void;
}) {
  const hasUnseen = folder.unseen > 0;

  return (
    <div
      className="w-full flex items-center gap-2 px-3 py-1.5 rounded-md text-sm transition-colors hover:bg-muted"
      style={{ paddingLeft: `${12 + (depth * 16)}px` }}
    >
      {/* Expand/collapse chevron for folders with children */}
      {hasChildren ? (
        <button
          onClick={onToggleExpand}
          className="p-0.5 hover:bg-muted-foreground/20 rounded"
        >
          <svg
            className={`h-3 w-3 transition-transform ${isExpanded ? 'rotate-90' : ''}`}
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M9 5l7 7-7 7" />
          </svg>
        </button>
      ) : (
        <div className="w-4" />
      )}

      <button
        onClick={onClick}
        className={cn(
          'flex-1 flex items-center gap-2 rounded-md transition-colors',
          isSelected
            ? 'bg-primary text-primary-foreground'
            : 'text-muted-foreground hover:text-foreground'
        )}
      >
        <span className={cn(isSelected ? 'text-primary-foreground' : 'text-muted-foreground')}>
          {getFolderIcon(folder.name)}
        </span>
        <span className="flex-1 text-left truncate">
          {getFolderDisplayName(folder.name)}
        </span>
        {folder.messages > 0 && (
          <span className="text-[10px] text-muted-foreground/70 mr-1">
            {folder.messages.toLocaleString()}
          </span>
        )}
        {hasUnseen && (
          <span
            className={cn(
              'text-xs font-medium px-1.5 py-0.5 rounded-full min-w-[1.25rem] text-center',
              isSelected
                ? 'bg-primary-foreground text-primary'
                : 'bg-primary text-primary-foreground'
            )}
          >
            {folder.unseen}
          </span>
        )}
      </button>
    </div>
  );
}

function FolderTreeNodeItem({
  node,
  selectedFolder,
  onSelectFolder,
  expandedFolders,
  onToggleExpand,
}: {
  node: FolderTreeNode;
  selectedFolder: string;
  onSelectFolder: (folder: string) => void;
  expandedFolders: Set<string>;
  onToggleExpand: (path: string) => void;
}) {
  const isExpanded = expandedFolders.has(node.path);
  const hasChildren = node.children && node.children.length > 0;

  return (
    <div>
      <FolderItem
        folder={node.folder}
        isSelected={selectedFolder === node.folder.name}
        onClick={() => onSelectFolder(node.folder.name)}
        depth={node.depth}
        hasChildren={hasChildren}
        isExpanded={isExpanded}
        onToggleExpand={(e) => {
          e.stopPropagation();
          onToggleExpand(node.path);
        }}
      />
      {hasChildren && isExpanded && (
        <div>
          {node.children.map((child) => (
            <FolderTreeNodeItem
              key={child.path}
              node={child}
              selectedFolder={selectedFolder}
              onSelectFolder={onSelectFolder}
              expandedFolders={expandedFolders}
              onToggleExpand={onToggleExpand}
            />
          ))}
        </div>
      )}
    </div>
  );
}

export function FolderPane({ accountId, selectedFolder, onSelectFolder }: FolderPaneProps) {
  const { folderTree, loading, error } = useFolders(accountId, true);
  const { setAccount } = useTriageStore();
  const [expandedFolders, setExpandedFolders] = useState<Set<string>>(new Set(['INBOX']));

  const handleFolderClick = (folderName: string) => {
    setAccount(accountId);
    onSelectFolder(folderName);
  };

  const toggleExpand = (path: string) => {
    setExpandedFolders((prev) => {
      const next = new Set(prev);
      if (next.has(path)) {
        next.delete(path);
      } else {
        next.add(path);
      }
      return next;
    });
  };

  if (loading) {
    return (
      <div className="p-3 space-y-2">
        <Skeleton className="h-8 w-full" />
        <Skeleton className="h-8 w-full" />
        <Skeleton className="h-8 w-full" />
        <Skeleton className="h-8 w-full" />
        <Skeleton className="h-8 w-full" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="p-4 text-sm text-destructive">
        <p className="font-medium mb-2">Failed to load folders</p>
        <p className="text-muted-foreground">{error}</p>
      </div>
    );
  }

  if (folderTree.length === 0) {
    return (
      <div className="p-4 text-sm text-muted-foreground text-center">
        <p>No folders found</p>
      </div>
    );
  }

  return (
    <div className="h-full flex flex-col">
      {/* Folder tree */}
      <ScrollArea className="flex-1">
        <div className="p-2 space-y-0.5">
          {folderTree.map((node) => (
            <FolderTreeNodeItem
              key={node.path}
              node={node}
              selectedFolder={selectedFolder}
              onSelectFolder={handleFolderClick}
              expandedFolders={expandedFolders}
              onToggleExpand={toggleExpand}
            />
          ))}
        </div>
      </ScrollArea>

      {/* Refresh button */}
      <div className="p-2 border-t mt-auto">
        <Button variant="ghost" size="sm" className="w-full text-xs" onClick={() => window.location.reload()}>
          <svg className="h-3 w-3 mr-1.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          Refresh
        </Button>
      </div>
    </div>
  );
}
