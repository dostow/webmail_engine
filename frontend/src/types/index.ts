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
  flags: string[] | null;
  headers: Record<string, string>;
  body?: MessageBody;  // Nested body object
  size: number;
  thread_id: string;
  content_type: string;
  has_attachments?: boolean;
  preview?: string;
}

// Nested body structure
export interface MessageBody {
  text: string;
  html: string;
  plain_text?: string;
}

// Message detail extends Message with parsed body fields for convenience
export interface MessageDetail extends Message {
  text_body: string;  // Alias for body.text
  html_body: string;  // Alias for body.html
  attachments?: Attachment[];
}

export interface Attachment {
  filename: string;
  content_type: string;
  size: number;
  cid?: string;
  download_url?: string;
}

export interface MessageListResponse {
  messages: Message[];
  folder: string;
  total_count: number;
  page_size: number;
  current_page: number;
  total_pages: number;
  has_more: boolean;
  next_cursor?: string;
  data_source?: string;
  freshness?: string;
}

export interface SearchQuery {
  account_id: string;
  keywords?: string[];
  from?: string;
  to?: string;
  subject?: string;
  folder?: string;
  limit?: number;
  offset?: number;
}

export interface SearchResponse {
  messages: Message[];
  total: number;
}

export interface SendEmailRequest {
  to: EmailAddress[];
  cc?: EmailAddress[];
  bcc?: EmailAddress[];
  subject: string;
  html_body?: string;
  text_body?: string;
  attachments?: AttachmentRequest[];
  in_reply_to?: string;
  references?: string[];
}

export interface AttachmentRequest {
  filename: string;
  content_type: string;
  content: string; // base64 encoded
}

export interface SendEmailResponse {
  id: string;
  status: 'queued' | 'sent' | 'failed';
  message: string;
}

export interface SystemHealthResponse {
  status: 'healthy' | 'degraded' | 'unhealthy';
  score: number;
  components: Record<string, ComponentHealth>;
}

export interface ComponentHealth {
  status: 'healthy' | 'degraded' | 'unhealthy';
  message?: string;
  details?: any;
}

export interface PoolStats {
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
