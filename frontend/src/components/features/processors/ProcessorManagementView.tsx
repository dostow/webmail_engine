import { useState, useEffect } from 'react';
import { useParams, Link } from 'react-router-dom';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import {
  getAccountProcessors,
  listProcessorTypes,
  deleteAccountProcessor,
  updateAccountProcessor,
  getProcessorTypeInfo,
} from '@/services/processorApi';
import type { AccountProcessorConfig, ProcessorType } from '@/types/processor';
import { ProcessorWizard } from './ProcessorWizard';
import { ProcessorCard } from './ProcessorCard';
import { toast } from 'sonner';

export function ProcessorManagementView() {
  const { accountId } = useParams<{ accountId: string }>();
  const [processors, setProcessors] = useState<AccountProcessorConfig[]>([]);
  const [availableTypes, setAvailableTypes] = useState<ProcessorType[]>([]);
  const [showWizard, setShowWizard] = useState(false);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    loadProcessors();
    loadAvailableTypes();
  }, [accountId]);

  async function loadProcessors() {
    if (!accountId) return;
    try {
      const configs = await getAccountProcessors(accountId);
      setProcessors(configs);
    } catch (error) {
      toast.error('Failed to load processors');
    } finally {
      setLoading(false);
    }
  }

  async function loadAvailableTypes() {
    try {
      const types = await listProcessorTypes();
      const typeInfos = await Promise.all(
        types.map((t) => getProcessorTypeInfo(t).catch(() => null))
      );
      setAvailableTypes(typeInfos.filter((t): t is ProcessorType => t !== null));
    } catch (error) {
      toast.error('Failed to load available processors');
    }
  }

  async function handleToggle(processorType: string, enabled: boolean) {
    if (!accountId) return;
    try {
      await updateAccountProcessor(accountId, processorType, enabled);
      setProcessors((prev) =>
        prev.map((p) => (p.type === processorType ? { ...p, enabled } : p))
      );
      toast.success(`Processor ${enabled ? 'enabled' : 'disabled'}`);
    } catch (error) {
      toast.error('Failed to update processor');
    }
  }

  async function handleDelete(processorType: string) {
    if (!accountId) return;
    try {
      await deleteAccountProcessor(accountId, processorType);
      setProcessors((prev) => prev.filter((p) => p.type !== processorType));
      toast.success('Processor removed');
    } catch (error) {
      toast.error('Failed to remove processor');
    }
  }

  function handleWizardComplete() {
    setShowWizard(false);
    loadProcessors();
  }

  const existingTypes = new Set(processors.map((p) => p.type));
  const availableToAdd = availableTypes.filter((t) => !existingTypes.has(t.type));

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <div>
          <div className="flex items-center gap-2 mb-1">
            <Link to={`/accounts/${accountId}`}>
              <Button variant="ghost" size="sm">
                ← Back to Account
              </Button>
            </Link>
          </div>
          <h2 className="text-2xl font-bold">Email Processors</h2>
          <p className="text-muted-foreground">
            Manage automated email processing for this account
          </p>
        </div>
        <Button
          onClick={() => setShowWizard(true)}
          disabled={availableToAdd.length === 0}
        >
          Add Processor
        </Button>
      </div>

      {loading ? (
        <Card>
          <div className="p-8 text-center text-muted-foreground">
            Loading processors...
          </div>
        </Card>
      ) : processors.length === 0 ? (
        <Card>
          <div className="p-8 text-center">
            <div className="text-muted-foreground mb-4">
              No processors configured for this account.
            </div>
            <Button onClick={() => setShowWizard(true)} disabled={availableToAdd.length === 0}>
              Add Your First Processor
            </Button>
          </div>
        </Card>
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          {processors.map((processor) => (
            <ProcessorCard
              key={processor.type}
              processor={processor}
              typeInfo={availableTypes.find((t) => t.type === processor.type)}
              onToggle={handleToggle}
              onDelete={handleDelete}
            />
          ))}
        </div>
      )}

      {showWizard && (
        <ProcessorWizard
          accountId={accountId!}
          availableTypes={availableToAdd}
          onComplete={handleWizardComplete}
          onCancel={() => setShowWizard(false)}
        />
      )}
    </div>
  );
}
