// API Types matching the Go backend models

export interface Account {
  id: string;
  email: string;
  status: 'active' | 'inactive' | 'error' | 'syncing' | 'disabled' | 'auth_required' | 'throttled';
  auth_type?: 'password' | 'oauth2' | 'app_password';
  created_at: string;
  updated_at: string;
  last_sync_at?: string;
  imap_config: IMAPConfig;
  smtp_config: SMTPConfig;
  sync_settings: SyncSettings;
  connection_limit: number;
  server_capabilities?: ServerCapabilities;
  processor_configs?: AccountProcessorConfig[];
}

export interface ServerCapabilities {
  capabilities: string[];
  supports_qresync: boolean;
  supports_condstore: boolean;
  supports_sort: boolean;
  supports_search_res: boolean;
  supports_literal_plus: boolean;
  supports_utf8_accept: boolean;
  supports_utf8_only: boolean;
  supports_move: boolean;
  supports_uid_plus: boolean;
  supports_unselect: boolean;
  supports_idle: boolean;
  supports_starttls: boolean;
  supports_auth_plain: boolean;
  supports_auth_login: boolean;
  supports_auth_oauth2: boolean;
  server_name?: string;
  server_vendor?: string;
  server_version?: string;
  last_checked: string;
}

export interface IMAPConfig {
  host: string;
  port: number;
  encryption: 'ssl' | 'starttls' | 'tls' | 'none';
  username: string;
}

export interface SMTPConfig {
  host: string;
  port: number;
  encryption: 'ssl' | 'starttls' | 'tls' | 'none';
  username: string;
}

export interface SyncSettings {
  sync_enabled: boolean;
  sync_interval: number;
  historical_scope: number;
  auto_sync: boolean;
  include_spam: boolean;
  include_trash: boolean;
  max_message_size: number;
  attachment_handling: string;
  fair_use_policy: FairUsePolicy;
}

export interface FairUsePolicy {
  bucket_size: number;
  refill_rate: number;
}

export interface AddAccountRequest {
  email: string;
  password?: string;
  access_token?: string;
  imap_host?: string;
  imap_port?: number;
  imap_encryption?: string;
  smtp_host?: string;
  smtp_port?: number;
  smtp_encryption?: string;
  connection_limit?: number;
}

export interface EmailAddress {
  name: string;
  address: string;
}

// Message list response - from field is a single object
export interface Message {
  uid: string;
  message_id: string;
  folder: string;
  subject: string;
  from: EmailAddress;  // Single object, not array
  to: EmailAddress[];
  cc?: EmailAddress[];
  bcc?: EmailAddress[];
  reply_to?: EmailAddress[];
  date: string;
  flags: string[];
  size: number;
  has_attachments: boolean;
  thread_id: string;
  body?: MessageBody;
}

export interface MessageBody {
  text?: string;
  html?: string;
  plain_text?: string;
}

// Message detail view - expanded with full body
export interface MessageDetail extends Message {
  text_body: string;
  html_body?: string;
  attachments?: Attachment[];
  headers?: Record<string, string>;
}

export interface Attachment {
  id: string;
  filename: string;
  content_type: string;
  size: number;
  content_id?: string;
  url?: string;
}

// Message list response
export interface MessageListResponse {
  messages: Message[];
  total_count: number;
  page_size: number;
  current_page: number;
  total_pages: number;
  has_more: boolean;
  next_cursor?: string;
  folder: string;
  data_source: 'cache' | 'live';
  freshness: string;
}

// Search
export interface SearchQuery {
  account_id: string;
  folder?: string;
  keywords?: string[];
  from?: string;
  to?: string;
  subject?: string;
  body?: string;
  since?: string;
  before?: string;
  limit?: number;
  offset?: number;
  sort_by?: string;
  sort_order?: 'asc' | 'desc';
}

export interface SearchResponse {
  messages: Message[];
  total_matches: number;
  search_time: number;
  next_offset?: number;
}

// Send Email
export interface SendEmailRequest {
  to: EmailAddress[];
  cc?: EmailAddress[];
  bcc?: EmailAddress[];
  subject: string;
  text_body?: string;
  html_body?: string;
  attachment_ids?: string[];
}

export interface SendEmailResponse {
  status: 'queued' | 'sent' | 'scheduled';
  message_id: string;
  tracking_id?: string;
  sent_at?: string;
}

// Health & Status
export interface SystemHealthResponse {
  status: 'healthy' | 'degraded' | 'unhealthy';
  score: number;
  components: Record<string, ComponentHealth>;
  performance: SystemPerformance;
  capacity: CapacityAnalysis;
}

export interface ComponentHealth {
  status: 'healthy' | 'degraded' | 'unhealthy';
  message?: string;
  latency?: number;
  error_rate?: number;
}

export interface SystemPerformance {
  request_latency_p50: number;
  request_latency_p95: number;
  request_latency_p99: number;
  error_rate: number;
  requests_per_second: number;
  active_connections: number;
  queue_depth: number;
}

export interface CapacityAnalysis {
  cpu_usage: number;
  memory_usage: number;
  connection_usage: number;
  cache_usage: number;
}

export interface PoolStats {
  total_connections: number;
  active_connections: number;
  idle_connections: number;
  max_connections: number;
  wait_count: number;
  avg_wait_time_ms: number;
  active_sessions: number;
  reuse_count: number;
  total_connects: number;
}

export interface AccountStats {
  account_id: string;
  email: string;
  status: string;
  connection_status: string;
  imap_host: string;
  smtp_host: string;
}

export interface APIError {
  code: string;
  message: string;
  details?: Record<string, string>;
}

// Folder Sync Information
export interface FolderSyncInfo {
  name: string;
  messages: number;
  unseen: number;
  last_sync: string;
  is_initialized: boolean;
  uid_validity: number;
}

// Processor Configuration
export interface AccountProcessorConfig {
  type: string;
  meta: Record<string, unknown>;
  enabled: boolean;
  priority: number;
}

// Export processor types
export type {
  ProcessorType,
  ProcessorMetaSchema,
  SchemaProperty,
  AccountProcessorConfig as ProcessorConfig,
  AccountProcessorsResponse,
  CreateProcessorRequest,
  ProcessorTypeInfoResponse,
} from './processor';
