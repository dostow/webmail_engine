import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import { Badge } from '@/components/ui/badge';
import { Switch } from '@/components/ui/switch';
import type { AccountProcessorConfig, ProcessorType } from '@/types/processor';

interface ProcessorCardProps {
  processor: AccountProcessorConfig;
  typeInfo?: ProcessorType;
  onToggle: (type: string, enabled: boolean) => void;
  onDelete: (type: string) => void;
}

export function ProcessorCard({
  processor,
  typeInfo,
  onToggle,
  onDelete,
}: ProcessorCardProps) {
  return (
    <Card className="p-6">
      <div className="flex items-start justify-between gap-4">
        <div className="flex-1">
          <div className="flex items-center gap-2 mb-2">
            <h3 className="font-semibold text-lg capitalize">
              {processor.type.replace(/_/g, ' ')}
            </h3>
            <Badge variant={processor.enabled ? 'default' : 'secondary'}>
              {processor.enabled ? 'Active' : 'Inactive'}
            </Badge>
          </div>
          <p className="text-sm text-muted-foreground mb-4">
            {typeInfo?.description || 'No description available'}
          </p>
          {Object.keys(processor.meta).length > 0 && (
            <div className="text-xs bg-muted p-3 rounded-md">
              <h4 className="font-medium mb-2">Configuration:</h4>
              <pre className="overflow-x-auto text-xs">
                {JSON.stringify(processor.meta, null, 2)}
              </pre>
            </div>
          )}
        </div>
        <div className="flex flex-col gap-2">
          <Switch
            checked={processor.enabled}
            onCheckedChange={(checked) => onToggle(processor.type, checked)}
          />
          <Button
            variant="destructive"
            size="sm"
            onClick={() => onDelete(processor.type)}
          >
            Remove
          </Button>
        </div>
      </div>
    </Card>
  );
}
