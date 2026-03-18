// Processor type definitions

export interface ProcessorType {
  type: string;
  description: string;
  meta_schema: ProcessorMetaSchema;
  default_config?: Record<string, unknown>;
  requires_api_key?: boolean;
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

export interface ProcessorTypeInfoResponse {
  type: string;
  description: string;
  meta_schema: ProcessorMetaSchema;
  default_config?: Record<string, unknown>;
  requires_api_key?: boolean;
}
