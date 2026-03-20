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

export function useFolders(accountId: string | null) {
  const [folders, setFolders] = useState<FolderInfo[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchFolders = useCallback(async () => {
    if (!accountId) {
      setFolders([]);
      return;
    }

    setLoading(true);
    setError(null);
    try {
      const data = await api.getAccountFolders(accountId);
      // Transform to FolderInfo format
      const folderInfos: FolderInfo[] = data.folders.map((f: any) => ({
        name: f.name,
        messages: f.messages || 0,
        unseen: f.unseen || 0,
        last_sync: f.last_sync,
      }));
      setFolders(folderInfos);
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to load folders';
      setError(msg);
    } finally {
      setLoading(false);
    }
  }, [accountId]);

  useEffect(() => {
    fetchFolders();
  }, [fetchFolders]);

  return { folders, loading, error, refetch: fetchFolders };
}
