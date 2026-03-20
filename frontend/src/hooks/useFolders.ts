import { useState, useEffect, useCallback } from 'react';
import * as api from '@/services/api';

export interface FolderInfo {
  name: string;
  delimiter?: string;
  attributes?: string[];
  messages: number;
  unseen: number;
  recent?: number;
  uid_next?: number;
  uid_validity?: number;
  last_sync?: string;
}

export interface FolderTreeNode {
  folder: FolderInfo;
  children: FolderTreeNode[];
  path: string;
  depth: number;
}

export interface FolderTreeResponse {
  account_id?: string;
  folders: FolderTreeNode[];
  total: number;
}

export function useFolders(accountId: string | null, useTree = true) {
  const [folders, setFolders] = useState<FolderInfo[]>([]);
  const [folderTree, setFolderTree] = useState<FolderTreeNode[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchFolders = useCallback(async () => {
    if (!accountId) {
      setFolders([]);
      setFolderTree([]);
      return;
    }

    setLoading(true);
    setError(null);
    try {
      // Use folder tree endpoint if requested
      if (useTree) {
        const data: FolderTreeResponse = await api.getFolderTree(accountId);
        setFolderTree(data.folders || []);
        // Also flatten for backward compatibility
        const flatFolders: FolderInfo[] = [];
        const flattenTree = (nodes: FolderTreeNode[]) => {
          nodes.forEach((node) => {
            flatFolders.push(node.folder);
            if (node.children && node.children.length > 0) {
              flattenTree(node.children);
            }
          });
        };
        flattenTree(data.folders || []);
        setFolders(flatFolders);
      } else {
        const data = await api.getAccountFolders(accountId);
        // Transform to FolderInfo format
        const folderInfos: FolderInfo[] = data.folders.map((f: any) => ({
          name: f.name,
          messages: f.messages || 0,
          unseen: f.unseen || 0,
          last_sync: f.last_sync,
        }));
        setFolders(folderInfos);
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to load folders';
      setError(msg);
    } finally {
      setLoading(false);
    }
  }, [accountId, useTree]);

  useEffect(() => {
    fetchFolders();
  }, [fetchFolders]);

  return { folders, folderTree, loading, error, refetch: fetchFolders };
}
