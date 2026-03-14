import { useCallback } from 'react';
import { toast } from 'sonner';

export function useEmailToast() {
  const showSuccess = useCallback((message: string) => {
    toast.success(message);
  }, []);

  const showError = useCallback((message: string) => {
    toast.error(message);
  }, []);

  const showInfo = useCallback((message: string) => {
    toast.info(message);
  }, []);

  return { showSuccess, showError, showInfo };
}
