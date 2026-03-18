import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import type { ProcessorMetaSchema, AccountProcessorConfig } from '@/types/processor';

interface ProcessorConfigFormProps {
  schema: ProcessorMetaSchema;
  config: AccountProcessorConfig;
  onChange: (config: AccountProcessorConfig) => void;
  onNext: () => void;
  onBack: () => void;
}

export function ProcessorConfigForm({
  schema,
  config,
  onChange,
  onNext,
  onBack,
}: ProcessorConfigFormProps) {
  function updateMeta(key: string, value: unknown) {
    onChange({
      ...config,
      meta: { ...config.meta, [key]: value },
    });
  }

  function renderField(key: string, prop: typeof schema.properties[string]) {
    switch (prop.type) {
      case 'string':
        if (prop.enum) {
          return (
            <select
              value={(config.meta[key] as string) || ''}
              onChange={(e) => updateMeta(key, e.target.value)}
              className="w-full px-3 py-2 border rounded-md bg-background"
            >
              <option value="">Select...</option>
              {prop.enum.map((val) => (
                <option key={val} value={val}>
                  {val}
                </option>
              ))}
            </select>
          );
        }
        return (
          <Input
            type={key.includes('key') || key.includes('password') || key.includes('secret') ? 'password' : 'text'}
            value={(config.meta[key] as string) || ''}
            onChange={(e) => updateMeta(key, e.target.value)}
            placeholder={prop.description}
          />
        );

      case 'number':
        return (
          <Input
            type="number"
            value={(config.meta[key] as number)?.toString() || ''}
            onChange={(e) => updateMeta(key, parseFloat(e.target.value) || 0)}
            placeholder={prop.description}
          />
        );

      case 'boolean':
        return (
          <Switch
            checked={(config.meta[key] as boolean) ?? (prop.default as boolean) ?? false}
            onCheckedChange={(checked) => updateMeta(key, checked)}
          />
        );

      case 'array':
        // Simple string array input
        const arrayValue = (config.meta[key] as string[]) || [];
        return (
          <Input
            value={arrayValue.join(', ')}
            onChange={(e) =>
              updateMeta(
                key,
                e.target.value.split(',').map((s) => s.trim()).filter(Boolean)
              )
            }
            placeholder="Comma-separated values"
          />
        );

      default:
        return (
          <div className="text-sm text-muted-foreground">
            Unsupported field type
          </div>
        );
    }
  }

  const requiredFields = schema.required || [];

  return (
    <div className="space-y-4">
      <h3 className="text-lg font-medium">Configure {config.type.replace(/_/g, ' ')}</h3>
      <div className="grid gap-4">
        {Object.entries(schema.properties).map(([key, prop]) => (
          <div key={key} className="grid gap-2">
            <Label htmlFor={key} className="flex items-center gap-2">
              {key.replace(/_/g, ' ').replace(/\b\w/g, (l) => l.toUpperCase())}
              {requiredFields.includes(key) && (
                <span className="text-destructive">*</span>
              )}
            </Label>
            {renderField(key, prop)}
            {prop.description && (
              <p className="text-xs text-muted-foreground">{prop.description}</p>
            )}
          </div>
        ))}
      </div>
      <div className="flex justify-between pt-4">
        <Button variant="outline" onClick={onBack}>
          Back
        </Button>
        <Button onClick={onNext}>Review</Button>
      </div>
    </div>
  );
}
