# Processor Management UI/UX Implementation Plan

## Overview

This plan outlines the implementation of a management UI/UX flow on the frontend for managing email processors. The system will allow users to view available processor types, link processors to accounts, and configure them through a wizard-based form.

---

## Phase 1: Backend API Enhancements

### 1.1 Add Processor Link Creation Endpoint

**File**: `internal/api/processor_handler.go`

Add a new endpoint to create/update processor configurations for an account:

```go
// CreateAccountProcessorRequest represents a request to link a processor to an account
type CreateAccountProcessorRequest struct {
    Type     string                 `json:"type" binding:"required"`
    Meta     map[string]interface{} `json:"meta"`
    Enabled  bool                   `json:"enabled"`
    Priority int                    `json:"priority"`
}

// createAccountProcessor links a processor to an account
func (h *ProcessorHandler) createAccountProcessor(c *gin.Context) {
    accountID := c.Param("account_id")

    var req CreateAccountProcessorRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    // Validate processor type
    registry := processor.GlobalRegistry()
    if !registry.IsRegistered(req.Type) {
        c.JSON(http.StatusBadRequest, gin.H{
            "error": fmt.Sprintf("unknown processor type: %s", req.Type),
        })
        return
    }

    // Get existing configs
    existingConfigs, err := h.store.GetAccountProcessorConfigs(c.Request.Context(), accountID)
    if err != nil && !store.IsNotFound(err) {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }

    // Check if processor already exists
    exists := false
    for i, cfg := range existingConfigs {
        if cfg.Type == req.Type {
            // Update existing
            metaJSON, _ := json.Marshal(req.Meta)
            existingConfigs[i].Meta = processor.ProcessorMeta(metaJSON)
            existingConfigs[i].Enabled = req.Enabled
            existingConfigs[i].Priority = req.Priority
            exists = true
            break
        }
    }

    if !exists {
        // Add new
        metaJSON, _ := json.Marshal(req.Meta)
        existingConfigs = append(existingConfigs, models.AccountProcessorConfig{
            Type:     req.Type,
            Meta:     processor.ProcessorMeta(metaJSON),
            Enabled:  req.Enabled,
            Priority: req.Priority,
        })
    }

    // Save updated configs
    err = h.store.UpdateAccountProcessorConfigs(c.Request.Context(), accountID, existingConfigs)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }

    c.JSON(http.StatusOK, gin.H{"status": "created", "type": req.Type})
}

// DeleteAccountProcessor removes a processor from an account
func (h *ProcessorHandler) deleteAccountProcessor(c *gin.Context) {
    accountID := c.Param("account_id")
    processorType := c.Param("processor_type")

    configs, err := h.store.GetAccountProcessorConfigs(c.Request.Context(), accountID)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }

    // Filter out the processor to delete
    filtered := make([]models.AccountProcessorConfig, 0, len(configs))
    for _, cfg := range configs {
        if cfg.Type != processorType {
            filtered = append(filtered, cfg)
        }
    }

    err = h.store.UpdateAccountProcessorConfigs(c.Request.Context(), accountID, filtered)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }

    c.JSON(http.StatusOK, gin.H{"status": "deleted", "type": processorType})
}
```

**Routes to add**:
```go
processorGroup.POST("/accounts/:account_id/processors", h.createAccountProcessor)
processorGroup.DELETE("/accounts/:account_id/processors/:processor_type", h.deleteAccountProcessor)
```

### 1.2 Update Processor Type Info Endpoint

Enhance the existing endpoint to include example configurations:

```go
// ProcessorTypeInfo represents information about a processor type
type ProcessorTypeInfo struct {
    Type             string                 `json:"type"`
    Description      string                 `json:"description"`
    MetaSchema       map[string]interface{} `json:"meta_schema"`
    DefaultConfig    map[string]interface{} `json:"default_config"`
    RequiresAPIKey   bool                   `json:"requires_api_key"`
    SupportedActions []string               `json:"supported_actions,omitempty"`
}
```

---

## Phase 2: Frontend TypeScript Types

### 2.1 Create Processor Types

**File**: `frontend/src/types/processor.ts`

```typescript
// Processor type definitions
export interface ProcessorType {
  type: string;
  description: string;
  meta_schema: ProcessorMetaSchema;
  default_config?: Record<string, unknown>;
  requires_api_key?: boolean;
  supported_actions?: string[];
}

export interface ProcessorMetaSchema {
  type: string;
  required?: string[];
  properties: Record<string, SchemaProperty>;
}

export interface SchemaProperty {
  type: 'string' | 'number' | 'boolean' | 'array' | 'object';
  description?: string;
  default?: unknown;
  enum?: string[];
  items?: SchemaProperty;
  properties?: Record<string, SchemaProperty>;
}

export interface AccountProcessorConfig {
  type: string;
  meta: Record<string, unknown>;
  enabled: boolean;
  priority: number;
}

export interface AccountProcessorsResponse {
  account_id: string;
  configs: AccountProcessorConfig[];
}

export interface CreateProcessorRequest {
  type: string;
  meta: Record<string, unknown>;
  enabled?: boolean;
  priority?: number;
}
```

---

## Phase 3: Frontend API Service

### 3.1 Create Processor API Service

**File**: `frontend/src/services/processorApi.ts`

```typescript
import {
  ProcessorType,
  AccountProcessorConfig,
  AccountProcessorsResponse,
  CreateProcessorRequest,
} from '@/types/processor';
import { getApiBaseUrl } from './api';

// Processor Type APIs
export async function listProcessorTypes(): Promise<ProcessorType[]> {
  const response = await fetch(`${getApiBaseUrl()}/v1/processors/types`);
  const data = await response.json();
  return data.types.map((type: string) => ({
    type,
    description: '',
    meta_schema: { type: 'object', properties: {} },
  }));
}

export async function getProcessorTypeInfo(type: string): Promise<ProcessorType> {
  const response = await fetch(`${getApiBaseUrl()}/v1/processors/types/${type}`);
  return response.json();
}

// Account Processor APIs
export async function getAccountProcessors(accountId: string): Promise<AccountProcessorConfig[]> {
  const response = await fetch(`${getApiBaseUrl()}/v1/processors/accounts/${accountId}`);
  const data: AccountProcessorsResponse = await response.json();
  return data.configs;
}

export async function createAccountProcessor(
  accountId: string,
  request: CreateProcessorRequest
): Promise<void> {
  const response = await fetch(`${getApiBaseUrl()}/v1/processors/accounts/${accountId}/processors`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
  });
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error || 'Failed to create processor');
  }
}

export async function updateAccountProcessor(
  accountId: string,
  processorType: string,
  enabled: boolean
): Promise<void> {
  const response = await fetch(
    `${getApiBaseUrl()}/v1/processors/accounts/${accountId}/${processorType}`,
    {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ enabled }),
    }
  );
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error || 'Failed to update processor');
  }
}

export async function deleteAccountProcessor(
  accountId: string,
  processorType: string
): Promise<void> {
  const response = await fetch(
    `${getApiBaseUrl()}/v1/processors/accounts/${accountId}/processors/${processorType}`,
    {
      method: 'DELETE',
    }
  );
  if (!response.ok) {
    const error = await response.json();
    throw new Error(error.error || 'Failed to delete processor');
  }
}
```

---

## Phase 4: Frontend Components

### 4.1 Processor Management View

**File**: `frontend/src/components/features/processors/ProcessorManagement.tsx`

```typescript
import { useState, useEffect } from 'react';
import { useParams } from 'react-router-dom';
import { Card } from '@/components/ui/Card';
import { Button } from '@/components/ui/Button';
import {
  getAccountProcessors,
  listProcessorTypes,
  deleteAccountProcessor,
  updateAccountProcessor,
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
        types.map(t => getProcessorTypeInfo(t.type).catch(() => t))
      );
      setAvailableTypes(typeInfos);
    } catch (error) {
      toast.error('Failed to load available processors');
    }
  }

  async function handleToggle(processorType: string, enabled: boolean) {
    if (!accountId) return;
    try {
      await updateAccountProcessor(accountId, processorType, enabled);
      setProcessors(prev =>
        prev.map(p => (p.type === processorType ? { ...p, enabled } : p))
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
      setProcessors(prev => prev.filter(p => p.type !== processorType));
      toast.success('Processor removed');
    } catch (error) {
      toast.error('Failed to remove processor');
    }
  }

  function handleWizardComplete() {
    setShowWizard(false);
    loadProcessors();
  }

  const existingTypes = new Set(processors.map(p => p.type));
  const availableToAdd = availableTypes.filter(t => !existingTypes.has(t.type));

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-2xl font-bold">Email Processors</h2>
          <p className="text-muted-foreground">
            Manage automated email processing for this account
          </p>
        </div>
        <Button onClick={() => setShowWizard(true)} disabled={availableToAdd.length === 0}>
          Add Processor
        </Button>
      </div>

      {loading ? (
        <div className="text-center py-8">Loading processors...</div>
      ) : processors.length === 0 ? (
        <Card>
          <div className="p-8 text-center text-muted-foreground">
            No processors configured. Click "Add Processor" to get started.
          </div>
        </Card>
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          {processors.map(processor => (
            <ProcessorCard
              key={processor.type}
              processor={processor}
              typeInfo={availableTypes.find(t => t.type === processor.type)}
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
```

### 4.2 Processor Card Component

**File**: `frontend/src/components/features/processors/ProcessorCard.tsx`

```typescript
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
            <h3 className="font-semibold text-lg">{processor.type}</h3>
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
              <pre className="overflow-x-auto">
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
```

### 4.3 Processor Wizard Component

**File**: `frontend/src/components/features/processors/ProcessorWizard.tsx`

```typescript
import { useState } from 'react';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
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
      meta: type.default_config || {},
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
      toast.error('Failed to add processor');
    }
  }

  return (
    <Dialog open onOpenChange={() => onCancel()}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Add Email Processor</DialogTitle>
        </DialogHeader>

        {/* Step Indicator */}
        <div className="flex items-center gap-2 mb-6">
          <div className={`flex items-center gap-2 ${step === 'select' ? 'text-primary' : 'text-muted-foreground'}`}>
            <div className={`w-8 h-8 rounded-full flex items-center justify-center ${
              step === 'select' ? 'bg-primary text-primary-foreground' : 'bg-muted'
            }`}>1</div>
            <span>Select</span>
          </div>
          <div className="flex-1 h-px bg-muted" />
          <div className={`flex items-center gap-2 ${step === 'configure' ? 'text-primary' : 'text-muted-foreground'}`}>
            <div className={`w-8 h-8 rounded-full flex items-center justify-center ${
              step === 'configure' ? 'bg-primary text-primary-foreground' : 'bg-muted'
            }`}>2</div>
            <span>Configure</span>
          </div>
          <div className="flex-1 h-px bg-muted" />
          <div className={`flex items-center gap-2 ${step === 'review' ? 'text-primary' : 'text-muted-foreground'}`}>
            <div className={`w-8 h-8 rounded-full flex items-center justify-center ${
              step === 'review' ? 'bg-primary text-primary-foreground' : 'bg-muted'
            }`}>3</div>
            <span>Review</span>
          </div>
        </div>

        {/* Step Content */}
        {step === 'select' && (
          <div className="grid gap-4">
            <h3 className="text-lg font-medium">Choose a Processor</h3>
            <div className="grid gap-3">
              {availableTypes.map(type => (
                <Card
                  key={type.type}
                  className="p-4 cursor-pointer hover:bg-accent transition-colors"
                  onClick={() => handleSelectType(type)}
                >
                  <div className="flex items-start justify-between">
                    <div>
                      <h4 className="font-semibold">{type.type}</h4>
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
                  <p className="font-medium">{config.type}</p>
                </div>
                <div>
                  <span className="text-sm text-muted-foreground">Status</span>
                  <p className="font-medium">{config.enabled ? 'Enabled' : 'Disabled'}</p>
                </div>
                <div>
                  <span className="text-sm text-muted-foreground">Configuration</span>
                  <pre className="bg-muted p-3 rounded-md mt-1 text-sm">
                    {JSON.stringify(config.meta, null, 2)}
                  </pre>
                </div>
              </div>
            </Card>
            <div className="flex justify-end gap-3">
              <Button variant="outline" onClick={() => setStep('configure')}>
                Back
              </Button>
              <Button onClick={handleSubmit}>
                Add Processor
              </Button>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
```

### 4.4 Dynamic Config Form Component

**File**: `frontend/src/components/features/processors/ProcessorConfigForm.tsx`

```typescript
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
              className="w-full px-3 py-2 border rounded-md"
            >
              <option value="">Select...</option>
              {prop.enum.map(val => (
                <option key={val} value={val}>{val}</option>
              ))}
            </select>
          );
        }
        return (
          <Input
            type={key.includes('key') || key.includes('password') ? 'password' : 'text'}
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
            onChange={(e) => updateMeta(key, parseFloat(e.target.value))}
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
        return (
          <Input
            value={((config.meta[key] as string[]) || []).join(', ')}
            onChange={(e) => updateMeta(key, e.target.value.split(',').map(s => s.trim()))}
            placeholder="Comma-separated values"
          />
        );

      default:
        return <div className="text-sm text-muted-foreground">Unsupported field type</div>;
    }
  }

  const requiredFields = schema.required || [];

  return (
    <div className="space-y-4">
      <h3 className="text-lg font-medium">Configure {config.type}</h3>
      <div className="grid gap-4">
        {Object.entries(schema.properties).map(([key, prop]) => (
          <div key={key} className="grid gap-2">
            <Label htmlFor={key} className="flex items-center gap-2">
              {key.replace(/_/g, ' ').replace(/\b\w/g, l => l.toUpperCase())}
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
        <Button onClick={onNext}>
          Review
        </Button>
      </div>
    </div>
  );
}
```

---

## Phase 5: Routing Integration

### 5.1 Add Processor Route

**File**: `frontend/src/router.tsx`

```typescript
{
  path: 'accounts/:accountId/processors',
  element: <ProcessorManagementView />,
}
```

### 5.2 Add Navigation Link

Add a link to the processor management page in the account detail view:

```typescript
<Link to={`/accounts/${accountId}/processors`}>
  <Button variant="outline">Manage Processors</Button>
</Link>
```

---

## Phase 6: Testing & Polish

### 6.1 Test Scenarios

1. **Wizard Flow**:
   - Select processor type → Configure → Review → Submit
   - Cancel at each step
   - Validation errors

2. **Processor Management**:
   - Toggle processor on/off
   - Delete processor
   - View configuration

3. **Form Validation**:
   - Required fields
   - Type validation (numbers, strings)
   - API key format

### 6.2 UI Polish

- Loading states
- Error boundaries
- Empty states
- Success/error toasts
- Responsive design

---

## Implementation Checklist

### Backend
- [ ] Add `POST /v1/processors/accounts/:account_id/processors` endpoint
- [ ] Add `DELETE /v1/processors/accounts/:account_id/processors/:type` endpoint
- [ ] Enhance processor type info with `default_config` and `requires_api_key`
- [ ] Test endpoints with curl/Postman

### Frontend
- [ ] Create TypeScript types (`types/processor.ts`)
- [ ] Create API service (`services/processorApi.ts`)
- [ ] Create `ProcessorManagementView` component
- [ ] Create `ProcessorCard` component
- [ ] Create `ProcessorWizard` component
- [ ] Create `ProcessorConfigForm` component
- [ ] Add route to router
- [ ] Add navigation link in account detail
- [ ] Test wizard flow
- [ ] Add loading/error states
- [ ] Responsive design testing

### Documentation
- [ ] Update API documentation
- [ ] Add user guide for processor management
- [ ] Document available processor types and configurations
