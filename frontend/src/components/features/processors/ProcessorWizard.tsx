import { useState } from 'react';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { Badge } from '@/components/ui/badge';
import { ProcessorConfigForm } from './ProcessorConfigForm';
import type { ProcessorType, AccountProcessorConfig } from '@/types/processor';
import { createAccountProcessor } from '@/services/processorApi';
import { toast } from 'sonner';

interface ProcessorWizardProps {
  accountId: string;
  availableTypes: ProcessorType[];
  onComplete: () => void;
  onCancel: () => void;
}

type WizardStep = 'select' | 'configure' | 'review';

export function ProcessorWizard({
  accountId,
  availableTypes,
  onComplete,
  onCancel,
}: ProcessorWizardProps) {
  const [step, setStep] = useState<WizardStep>('select');
  const [selectedType, setSelectedType] = useState<ProcessorType | null>(null);
  const [config, setConfig] = useState<AccountProcessorConfig>({
    type: '',
    meta: {},
    enabled: true,
    priority: 0,
  });

  function handleSelectType(type: ProcessorType) {
    setSelectedType(type);
    setConfig({
      type: type.type,
      meta: type.default_config ? { ...type.default_config } : {},
      enabled: true,
      priority: 0,
    });
    setStep('configure');
  }

  async function handleSubmit() {
    try {
      await createAccountProcessor(accountId, {
        type: config.type,
        meta: config.meta,
        enabled: config.enabled,
        priority: config.priority,
      });
      toast.success('Processor added successfully');
      onComplete();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : 'Failed to add processor');
    }
  }

  return (
    <Dialog open onOpenChange={() => onCancel()}>
      <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Add Email Processor</DialogTitle>
        </DialogHeader>

        {/* Step Indicator */}
        <div className="flex items-center gap-2 mb-6">
          <div
            className={`flex items-center gap-2 ${
              step === 'select' ? 'text-primary' : 'text-muted-foreground'
            }`}
          >
            <div
              className={`w-8 h-8 rounded-full flex items-center justify-center ${
                step === 'select'
                  ? 'bg-primary text-primary-foreground'
                  : 'bg-muted'
              }`}
            >
              1
            </div>
            <span className="text-sm font-medium">Select</span>
          </div>
          <div className="flex-1 h-px bg-muted" />
          <div
            className={`flex items-center gap-2 ${
              step === 'configure' ? 'text-primary' : 'text-muted-foreground'
            }`}
          >
            <div
              className={`w-8 h-8 rounded-full flex items-center justify-center ${
                step === 'configure'
                  ? 'bg-primary text-primary-foreground'
                  : 'bg-muted'
              }`}
            >
              2
            </div>
            <span className="text-sm font-medium">Configure</span>
          </div>
          <div className="flex-1 h-px bg-muted" />
          <div
            className={`flex items-center gap-2 ${
              step === 'review' ? 'text-primary' : 'text-muted-foreground'
            }`}
          >
            <div
              className={`w-8 h-8 rounded-full flex items-center justify-center ${
                step === 'review'
                  ? 'bg-primary text-primary-foreground'
                  : 'bg-muted'
              }`}
            >
              3
            </div>
            <span className="text-sm font-medium">Review</span>
          </div>
        </div>

        {/* Step Content */}
        {step === 'select' && (
          <div className="space-y-4">
            <h3 className="text-lg font-medium">Choose a Processor</h3>
            <div className="grid gap-3">
              {availableTypes.map((type) => (
                <Card
                  key={type.type}
                  className="p-4 cursor-pointer hover:bg-accent transition-colors"
                  onClick={() => handleSelectType(type)}
                >
                  <div className="flex items-start justify-between">
                    <div>
                      <h4 className="font-semibold capitalize">
                        {type.type.replace(/_/g, ' ')}
                      </h4>
                      <p className="text-sm text-muted-foreground mt-1">
                        {type.description}
                      </p>
                    </div>
                    {type.requires_api_key && (
                      <Badge variant="outline">Requires API Key</Badge>
                    )}
                  </div>
                </Card>
              ))}
            </div>
          </div>
        )}

        {step === 'configure' && selectedType && (
          <ProcessorConfigForm
            schema={selectedType.meta_schema}
            config={config}
            onChange={setConfig}
            onNext={() => setStep('review')}
            onBack={() => setStep('select')}
          />
        )}

        {step === 'review' && (
          <div className="space-y-4">
            <h3 className="text-lg font-medium">Review Configuration</h3>
            <Card className="p-4">
              <div className="space-y-3">
                <div>
                  <span className="text-sm text-muted-foreground">Processor</span>
                  <p className="font-medium capitalize">
                    {config.type.replace(/_/g, ' ')}
                  </p>
                </div>
                <div>
                  <span className="text-sm text-muted-foreground">Status</span>
                  <p className="font-medium">
                    {config.enabled ? 'Enabled' : 'Disabled'}
                  </p>
                </div>
                <div>
                  <span className="text-sm text-muted-foreground">Configuration</span>
                  <pre className="bg-muted p-3 rounded-md mt-1 text-xs overflow-x-auto">
                    {JSON.stringify(config.meta, null, 2)}
                  </pre>
                </div>
              </div>
            </Card>
            <div className="flex justify-end gap-3">
              <Button variant="outline" onClick={() => setStep('configure')}>
                Back
              </Button>
              <Button onClick={handleSubmit}>Add Processor</Button>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
