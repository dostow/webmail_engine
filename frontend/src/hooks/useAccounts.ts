import { useCallback, useState } from 'react';
import * as api from '@/services/api';
import { useEmailToast } from '@/hooks/useToast';
import type { Account } from '@/types';

export function useAccounts() {
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const { showSuccess, showError } = useEmailToast();

  const fetchAccounts = useCallback(async () => {
    try {
      setLoading(true);
      const data = await api.listAccounts();
      setAccounts(data);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load accounts');
      showError('Failed to load accounts');
    } finally {
      setLoading(false);
    }
  }, [showError]);

  const createAccount = useCallback(async (requestData: any) => {
    try {
      setLoading(true);
      const account = await api.createAccount(requestData);
      setAccounts((prev) => [...prev, account]);
      showSuccess('Account added successfully');
      return account;
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to add account';
      showError(message);
      throw err;
    } finally {
      setLoading(false);
    }
  }, [showSuccess, showError]);

  const deleteAccount = useCallback(async (id: string) => {
    try {
      setLoading(true);
      await api.deleteAccount(id);
      setAccounts((prev) => prev.filter((a) => a.id !== id));
      showSuccess('Account deleted successfully');
    } catch (err) {
      showError('Failed to delete account');
      throw err;
    } finally {
      setLoading(false);
    }
  }, [showSuccess, showError]);

  return {
    accounts,
    loading,
    error,
    fetchAccounts,
    createAccount,
    deleteAccount,
  };
}
