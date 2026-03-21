package models

import (
	"encoding/json"
	"time"
)

// AuditLog represents a logged security event
type AuditLog struct {
	ID        int64     `json:"id"`
	AccountID string    `json:"account_id"`
	Email     string    `json:"email"`
	Event     string    `json:"event"`
	Details   string    `json:"details"`
	Timestamp time.Time `json:"timestamp"`
	IP        string    `json:"ip"`
}

// AccountProcessorConfig defines a processor enabled for a specific sync account
type AccountProcessorConfig struct {
	Type     string          `json:"type"`     // e.g., "llm_processor", "link_tracker"
	Meta     json.RawMessage `json:"meta"`     // Type-specific configuration (JSON)
	Enabled  bool            `json:"enabled"`  // Whether processor is active
	Priority int             `json:"priority"` // Execution order (lower = earlier)
}

// AccountStatus represents the current state of an email account
type AccountStatus string

const (
	AccountStatusActive       AccountStatus = "active"
	AccountStatusInactive     AccountStatus = "inactive"
	AccountStatusError        AccountStatus = "error"
	AccountStatusAuthRequired AccountStatus = "auth_required"
	AccountStatusThrottled    AccountStatus = "throttled"
	AccountStatusDisabled     AccountStatus = "disabled"
)

// AuthType represents the authentication method for an email account
type AuthType string

const (
	AuthTypePassword AuthType = "password"
	AuthTypeOAuth2   AuthType = "oauth2"
	AuthTypeAppPass  AuthType = "app_password"
)

// ConnectionProtocol represents the protocol used for email connections
type ConnectionProtocol string

const (
	ProtocolIMAP ConnectionProtocol = "imap"
	ProtocolSMTP ConnectionProtocol = "smtp"
)

// EncryptionType represents the encryption method for connections
type EncryptionType string

const (
	EncryptionNone     EncryptionType = "none"
	EncryptionSSL      EncryptionType = "ssl"
	EncryptionTLS      EncryptionType = "tls"
	EncryptionStartTLS EncryptionType = "starttls"
)

// MessageFlag represents email message flags
type MessageFlag string

const (
	FlagSeen     MessageFlag = "seen"
	FlagAnswered MessageFlag = "answered"
	FlagFlagged  MessageFlag = "flagged"
	FlagDeleted  MessageFlag = "deleted"
	FlagDraft    MessageFlag = "draft"
	FlagRecent   MessageFlag = "recent"
)

// SortField represents fields that can be used for sorting messages
type SortField string

const (
	SortByDate           SortField = "date"
	SortByFrom           SortField = "from"
	SortBySubject        SortField = "subject"
	SortByTo             SortField = "to"
	SortBySize           SortField = "size"
	SortByHasAttachments SortField = "has_attachments"
)

// SortOrder represents the order of sorting
type SortOrder string

const (
	SortOrderAsc  SortOrder = "asc"
	SortOrderDesc SortOrder = "desc"
)

// CacheContext represents the pagination context for targeted cache invalidation
type CacheContext struct {
	Cursor    string    `json:"cursor"`
	Limit     int       `json:"limit"`
	SortBy    SortField `json:"sort_by"`
	SortOrder SortOrder `json:"sort_order"`
}

// ContentType represents MIME content types
type ContentType string

const (
	ContentTypeTextPlain   ContentType = "text/plain"
	ContentTypeTextHTML    ContentType = "text/html"
	ContentTypeMultipart   ContentType = "multipart"
	ContentTypeApplication ContentType = "application"
	ContentTypeImage       ContentType = "image"
)

// EventType represents webhook event types
type EventType string

const (
	EventNewMessage         EventType = "message.new"
	EventMessageDeleted     EventType = "message.deleted"
	EventMessageFlagged     EventType = "message.flagged"
	EventAuthError          EventType = "auth.error"
	EventConnectionLost     EventType = "connection.lost"
	EventConnectionRestored EventType = "connection.restored"
	EventQuotaWarning       EventType = "quota.warning"
)

// HealthStatus represents component health state
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
)

// Account represents an email account configuration
type Account struct {
	ID                 string                   `json:"id"`
	Email              string                   `json:"email"`
	AuthType           AuthType                 `json:"auth_type"`
	Status             AccountStatus            `json:"status"`
	CreatedAt          time.Time                `json:"created_at"`
	UpdatedAt          time.Time                `json:"updated_at"`
	LastSyncAt         *time.Time               `json:"last_sync_at,omitempty"`
	IMAPConfig         ServerConfig             `json:"imap_config"`
	SMTPConfig         ServerConfig             `json:"smtp_config"`
	ProxyConfig        *ProxySettings           `json:"proxy_config,omitempty"`
	FairUsePolicy      *FairUsePolicy           `json:"fair_use_policy,omitempty"`
	ConnectionLimit    int                      `json:"connection_limit"`
	SyncSettings       SyncSettings             `json:"sync_settings"`
	ServerCapabilities *ServerCapabilities      `json:"server_capabilities,omitempty"`
	ProcessorConfigs   []AccountProcessorConfig `json:"processor_configs,omitempty"`
}

// ServerCapabilities represents IMAP server capabilities
type ServerCapabilities struct {
	// Standard IMAP capabilities
	Capabilities []string `json:"capabilities"`

	// Extended capabilities (parsed from standard capabilities)
	SupportsQResync     bool `json:"supports_qresync"`
	SupportsCondStore   bool `json:"supports_condstore"`
	SupportsSort        bool `json:"supports_sort"`
	SupportsSearchRes   bool `json:"supports_search_res"`
	SupportsLiteralPlus bool `json:"supports_literal_plus"`
	SupportsUTF8Accept  bool `json:"supports_utf8_accept"`
	SupportsUTF8Only    bool `json:"supports_utf8_only"`
	SupportsMove        bool `json:"supports_move"`
	SupportsUIDPlus     bool `json:"supports_uid_plus"`
	SupportsUnselect    bool `json:"supports_unselect"`
	SupportsIdle        bool `json:"supports_idle"`
	SupportsStartTLS    bool `json:"supports_starttls"`
	SupportsAuthPlain   bool `json:"supports_auth_plain"`
	SupportsAuthLogin   bool `json:"supports_auth_login"`
	SupportsAuthOAuth2  bool `json:"supports_auth_oauth2"`

	// Server identification
	ServerName    string `json:"server_name,omitempty"`
	ServerVendor  string `json:"server_vendor,omitempty"`
	ServerVersion string `json:"server_version,omitempty"`

	// Last capability check timestamp
	LastChecked time.Time `json:"last_checked"`
}

// ServerConfig represents IMAP/SMTP server configuration
type ServerConfig struct {
	Host       string         `json:"host"`
	Port       int            `json:"port"`
	Encryption EncryptionType `json:"encryption"`
	Username   string         `json:"username"`
	// Password is stored encrypted and never returned in JSON
	Password     string `json:"-"`
	AccessToken  string `json:"-"`
	RefreshToken string `json:"-"`
}

// ProxySettings represents proxy configuration for an account
type ProxySettings struct {
	Enabled        bool   `json:"enabled"`
	Type           string `json:"type"` // socks5, http
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Username       string `json:"username,omitempty"`
	Password       string `json:"-"`
	Timeout        int    `json:"timeout"` // seconds
	FallbackDirect bool   `json:"fallback_direct"`
}

// SyncSettings represents data collection preferences
type SyncSettings struct {
	HistoricalScope            int            `json:"historical_scope"` // days
	AutoSync                   bool           `json:"auto_sync"`
	SyncInterval               int            `json:"sync_interval"` // seconds
	IncludeSpam                bool           `json:"include_spam"`
	IncludeTrash               bool           `json:"include_trash"`
	MaxMessageSize             int            `json:"max_message_size"`    // bytes
	AttachmentHandling         string         `json:"attachment_handling"` // inline, url, skip
	FetchBody                  bool           `json:"fetch_body"`          // whether to fetch full message bodies
	EnableLinkExtraction       bool           `json:"enable_link_extraction"`
	EnableAttachmentProcessing bool           `json:"enable_attachment_processing"`
	FairUsePolicy              *FairUsePolicy `json:"fair_use_policy,omitempty"`
}

// FolderSyncState tracks synchronization state for a single folder
type FolderSyncState struct {
	AccountID     string    `json:"account_id"`
	FolderName    string    `json:"folder_name"`
	UIDValidity   uint32    `json:"uid_validity"`    // IMAP UIDVALIDITY value
	LastSyncedUID uint32    `json:"last_synced_uid"` // Highest UID synced
	LastSyncTime  time.Time `json:"last_sync_time"`
	MessageCount  uint32    `json:"message_count"`  // Total messages in folder
	IsInitialized bool      `json:"is_initialized"` // Whether initial sync completed
}

// EnvelopeProcessingPriority represents the priority level for envelope processing
type EnvelopeProcessingPriority string

const (
	PriorityHigh   EnvelopeProcessingPriority = "high"   // UNSEEN, FLAGGED, from known contacts
	PriorityNormal EnvelopeProcessingPriority = "normal" // Regular INBOX messages
	PriorityLow    EnvelopeProcessingPriority = "low"    // Archive, old messages
)

// EnvelopeProcessingStatus represents the current state of envelope processing
type EnvelopeProcessingStatus string

const (
	EnvelopeStatusPending    EnvelopeProcessingStatus = "pending"
	EnvelopeStatusProcessing EnvelopeProcessingStatus = "processing"
	EnvelopeStatusCompleted  EnvelopeProcessingStatus = "completed"
	EnvelopeStatusFailed     EnvelopeProcessingStatus = "failed"
	EnvelopeStatusSkipped    EnvelopeProcessingStatus = "skipped"
)

// EnvelopeQueueItem represents an envelope waiting for processing
type EnvelopeQueueItem struct {
	ID                 string                     `json:"id"`
	AccountID          string                     `json:"account_id"`
	FolderName         string                     `json:"folder_name"`
	UID                uint32                     `json:"uid"`
	MessageID          string                     `json:"message_id"`
	From               []Contact                  `json:"from"`
	To                 []Contact                  `json:"to"`
	Subject            string                     `json:"subject"`
	Date               time.Time                  `json:"date"`
	Flags              []string                   `json:"flags"`
	Size               int64                      `json:"size"`
	Priority           EnvelopeProcessingPriority `json:"priority"`
	Status             EnvelopeProcessingStatus   `json:"status"`
	RetryCount         int                        `json:"retry_count"`
	MaxRetries         int                        `json:"max_retries"`
	LastError          string                     `json:"last_error,omitempty"`
	EnqueuedAt         time.Time                  `json:"enqueued_at"`
	ProcessingAt       *time.Time                 `json:"processing_at,omitempty"`
	CompletedAt        *time.Time                 `json:"completed_at,omitempty"`
	ProcessingMetadata *ProcessingMetadata        `json:"processing_metadata,omitempty"`
}

// FairUsePolicy represents rate limiting configuration
type FairUsePolicy struct {
	Enabled         bool           `json:"enabled"`
	TokenBucketSize int            `json:"token_bucket_size"`
	RefillRate      int            `json:"refill_rate"` // tokens per minute
	OperationCosts  map[string]int `json:"operation_costs"`
	PriorityLevels  map[string]int `json:"priority_levels"`
	ProviderLimits  ProviderLimits `json:"provider_limits"`
}

// ProviderLimits represents email provider specific limits
type ProviderLimits struct {
	MaxConnections      int `json:"max_connections"`
	MaxRequestsPerHour  int `json:"max_requests_per_hour"`
	MaxRecipientsPerDay int `json:"max_recipients_per_day"`
	MaxMessageSize      int `json:"max_message_size"`
}

// TokenBucket represents the current state of fair-use tokens
type TokenBucket struct {
	AccountID  string    `json:"account_id"`
	Tokens     int       `json:"tokens"`
	MaxTokens  int       `json:"max_tokens"`
	LastRefill time.Time `json:"last_refill"`
	RefillRate int       `json:"refill_rate"`
}

// Message represents an email message
type Message struct {
	UID                string              `json:"uid"`
	MessageID          string              `json:"message_id"`
	Folder             string              `json:"folder"`
	Subject            string              `json:"subject"`
	From               Contact             `json:"from"`
	To                 []Contact           `json:"to"`
	Cc                 []Contact           `json:"cc"`
	Bcc                []Contact           `json:"bcc"`
	ReplyTo            []Contact           `json:"reply_to"`
	Date               time.Time           `json:"date"`
	Flags              []MessageFlag       `json:"flags"`
	Headers            map[string]string   `json:"headers,omitempty"`
	Body               *MessageBody        `json:"body,omitempty"`
	Attachments        []Attachment        `json:"attachments,omitempty"`
	Links              []string            `json:"links,omitempty"`
	Size               int64               `json:"size"`
	ThreadID           string              `json:"thread_id"`
	InReplyTo          string              `json:"in_reply_to,omitempty"`
	References         []string            `json:"references,omitempty"`
	ContentType        ContentType         `json:"content_type"`
	RawHeaders         string              `json:"raw_headers,omitempty"`
	ProcessingMetadata *ProcessingMetadata `json:"processing_metadata,omitempty"`
}

// Contact represents an email contact
type Contact struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

// MessageBody represents the message content
type MessageBody struct {
	Text      string `json:"text,omitempty"`
	HTML      string `json:"html,omitempty"`
	PlainText string `json:"plain_text,omitempty"`
}

// Attachment represents an email attachment
type Attachment struct {
	ID          string     `json:"id"`
	PartID      string     `json:"part_id,omitempty"` // IMAP body part identifier
	Filename    string     `json:"filename"`
	ContentType string     `json:"content_type"`
	Size        int64      `json:"size"`
	Disposition string     `json:"disposition"` // inline, attachment
	ContentID   string     `json:"content_id,omitempty"`
	Checksum    string     `json:"checksum"`
	AccessURL   string     `json:"access_url,omitempty"`
	URLExpiry   *time.Time `json:"url_expiry,omitempty"`
}

// ProcessingMetadata represents message processing information
type ProcessingMetadata struct {
	CacheStatus    string    `json:"cache_status"`    // hit, miss, stale
	ProcessingTime int64     `json:"processing_time"` // milliseconds
	SizeOriginal   int64     `json:"size_original"`
	SizeProcessed  int64     `json:"size_processed"`
	ProcessedAt    time.Time `json:"processed_at"`
	TokenConsumed  int       `json:"token_consumed"`
}

// MessageList represents a paginated list of messages
type MessageList struct {
	Messages    []MessageSummary `json:"messages"`
	TotalCount  int              `json:"total_count"`
	PageSize    int              `json:"page_size"`
	CurrentPage int              `json:"current_page"`
	TotalPages  int              `json:"total_pages"`
	HasMore     bool             `json:"has_more"`
	NextCursor  string           `json:"next_cursor,omitempty"`
	Folder      string           `json:"folder"`
	DataSource  string           `json:"data_source"` // cache, live
	Freshness   time.Time        `json:"freshness"`
	UIDValidity uint32           `json:"uid_validity,omitempty"` // For cache validation
}

// MessageSummary represents basic message metadata for list views
type MessageSummary struct {
	UID            string        `json:"uid"`
	MessageID      string        `json:"message_id"`
	Subject        string        `json:"subject"`
	From           Contact       `json:"from"`
	To             []Contact     `json:"to"`
	Date           time.Time     `json:"date"`
	Flags          []MessageFlag `json:"flags"`
	HasAttachments bool          `json:"has_attachments"`
	Size           int64         `json:"size"`
	ThreadID       string        `json:"thread_id"`
	Folder         string        `json:"folder"`
}

// FolderInfo represents IMAP folder information
type FolderInfo struct {
	Name        string    `json:"name"`
	Delimiter   string    `json:"delimiter"`
	Attributes  []string  `json:"attributes"`
	Messages    int       `json:"messages"`
	Recent      int       `json:"recent"`
	Unseen      int       `json:"unseen"`
	UIDNext     uint32    `json:"uid_next"`
	UIDValidity uint32    `json:"uid_validity"`
	LastSync    time.Time `json:"last_sync"`
}

// FolderTreeNode represents a node in the folder hierarchy
type FolderTreeNode struct {
	Folder   *FolderInfo       `json:"folder"`
	Children []*FolderTreeNode `json:"children,omitempty"`
	Path     string            `json:"path"`
	Depth    int               `json:"depth"`
}

// SearchQuery represents search criteria for messages
type SearchQuery struct {
	AccountID       string        `json:"account_id"`
	Folder          string        `json:"folder,omitempty"`
	Keywords        []string      `json:"keywords,omitempty"`
	From            string        `json:"from,omitempty"`
	To              string        `json:"to,omitempty"`
	Subject         string        `json:"subject,omitempty"`
	Body            string        `json:"body,omitempty"`
	Since           *time.Time    `json:"since,omitempty"`
	Before          *time.Time    `json:"before,omitempty"`
	Flags           []MessageFlag `json:"flags,omitempty"`
	HasFlags        []MessageFlag `json:"has_flags,omitempty"`
	NotFlags        []MessageFlag `json:"not_flags,omitempty"`
	WithAttachments *bool         `json:"with_attachments,omitempty"`
	Limit           int           `json:"limit,omitempty"`
	Offset          int           `json:"offset,omitempty"`
	Cursor          string        `json:"cursor,omitempty"`
	SortBy          SortField     `json:"sort_by,omitempty"`
	SortOrder       SortOrder     `json:"sort_order,omitempty"`
	UseCache        bool          `json:"use_cache"`
}

// SearchResult represents search results
type SearchResult struct {
	Messages     []MessageSummary `json:"messages"`
	TotalMatches int              `json:"total_matches"`
	SearchTime   int64            `json:"search_time"` // milliseconds
	CacheUsed    bool             `json:"cache_used"`
	NextOffset   int              `json:"next_offset,omitempty"`
}

// SendEmailRequest represents a request to send an email
type SendEmailRequest struct {
	AccountID       string            `json:"account_id"`
	To              []Contact         `json:"to"`
	Cc              []Contact         `json:"cc,omitempty"`
	Bcc             []Contact         `json:"bcc,omitempty"`
	ReplyTo         []Contact         `json:"reply_to,omitempty"`
	Subject         string            `json:"subject"`
	TextBody        string            `json:"text_body,omitempty"`
	HTMLBody        string            `json:"html_body,omitempty"`
	AttachmentIDs   []string          `json:"attachment_ids,omitempty"`
	TemplateID      string            `json:"template_id,omitempty"`
	TemplateVars    map[string]string `json:"template_vars,omitempty"`
	ScheduleAt      *time.Time        `json:"schedule_at,omitempty"`
	Priority        string            `json:"priority,omitempty"` // high, normal, low
	TrackingEnabled bool              `json:"tracking_enabled"`
	Headers         map[string]string `json:"headers,omitempty"`
}

// SendEmailResponse represents the response after sending an email
type SendEmailResponse struct {
	Status          string     `json:"status"` // queued, sent, scheduled
	MessageID       string     `json:"message_id"`
	TrackingID      string     `json:"tracking_id,omitempty"`
	SentAt          *time.Time `json:"sent_at,omitempty"`
	ScheduledAt     *time.Time `json:"scheduled_at,omitempty"`
	QueueID         string     `json:"queue_id,omitempty"`
	EstimatedSendAt *time.Time `json:"estimated_send_at,omitempty"`
	ResourceUsage   int        `json:"resource_usage"`
}

// WebhookEvent represents an incoming webhook event
type WebhookEvent struct {
	EventID    string           `json:"event_id"`
	EventType  EventType        `json:"event_type"`
	Timestamp  time.Time        `json:"timestamp"`
	AccountID  string           `json:"account_id"`
	Version    string           `json:"version"`
	Data       WebhookEventData `json:"data"`
	Signature  string           `json:"signature"`
	RetryCount int              `json:"retry_count,omitempty"`
}

// WebhookEventData represents event-specific data
type WebhookEventData struct {
	MessageID    string     `json:"message_id,omitempty"`
	Subject      string     `json:"subject,omitempty"`
	From         Contact    `json:"from,omitempty"`
	Preview      string     `json:"preview,omitempty"`
	DeletionTime *time.Time `json:"deletion_time,omitempty"`
	AuthType     string     `json:"auth_type,omitempty"`
	ErrorDetails string     `json:"error_details,omitempty"`
	RecoveryInfo string     `json:"recovery_info,omitempty"`
}

// WebhookResponse represents the webhook processing response
type WebhookResponse struct {
	Status      string     `json:"status"` // accepted, processed, rejected
	ReceiptID   string     `json:"receipt_id"`
	ProcessedAt *time.Time `json:"processed_at,omitempty"`
	Error       string     `json:"error,omitempty"`
	RetryAfter  *int       `json:"retry_after,omitempty"` // seconds
}

// AccountStatusResponse represents account status information
type AccountStatusResponse struct {
	AccountID       string             `json:"account_id"`
	ConnectionState ConnectionState    `json:"connection_state"`
	Performance     PerformanceMetrics `json:"performance"`
	Resources       ResourceStatus     `json:"resources"`
	Health          HealthIndicators   `json:"health"`
	Issues          []AccountIssue     `json:"issues,omitempty"`
	LastSuccessful  *time.Time         `json:"last_successful,omitempty"`
}

// ConnectionState represents the current connection state
type ConnectionState struct {
	Status        AccountStatus `json:"status"`
	IMAPConnected bool          `json:"imap_connected"`
	SMTPConnected bool          `json:"smtp_connected"`
	LastConnected *time.Time    `json:"last_connected,omitempty"`
	LastError     *string       `json:"last_error,omitempty"`
	ErrorCount    int           `json:"error_count"`
}

// PerformanceMetrics represents recent performance data
type PerformanceMetrics struct {
	AvgLatency       int64      `json:"avg_latency"` // milliseconds
	RecentErrors     int        `json:"recent_errors"`
	OperationsPerMin int        `json:"operations_per_min"`
	LastOperation    *time.Time `json:"last_operation,omitempty"`
}

// ResourceStatus represents resource allocation
type ResourceStatus struct {
	CurrentConnections int          `json:"current_connections"`
	MaxConnections     int          `json:"max_connections"`
	TokenBucket        *TokenBucket `json:"token_bucket,omitempty"`
	QuotaUsed          int64        `json:"quota_used"`
	QuotaTotal         int64        `json:"quota_total"`
}

// HealthIndicators represents health assessment
type HealthIndicators struct {
	Score           int          `json:"score"` // 0-100
	Status          HealthStatus `json:"status"`
	Recommendations []string     `json:"recommendations,omitempty"`
}

// AccountIssue represents an issue with an account
type AccountIssue struct {
	Type        string    `json:"type"`
	Severity    string    `json:"severity"` // warning, critical
	Description string    `json:"description"`
	DetectedAt  time.Time `json:"detected_at"`
}

// SystemHealthResponse represents overall system health
type SystemHealthResponse struct {
	Status          HealthStatus               `json:"status"`
	Score           int                        `json:"score"`
	Components      map[string]ComponentHealth `json:"components"`
	Performance     SystemPerformance          `json:"performance"`
	Capacity        CapacityAnalysis           `json:"capacity"`
	Alerts          []SystemAlert              `json:"alerts,omitempty"`
	HistoricalTrend []HealthDataPoint          `json:"historical_trend,omitempty"`
}

// ComponentHealth represents individual component health
type ComponentHealth struct {
	Status    HealthStatus `json:"status"`
	Message   string       `json:"message,omitempty"`
	Latency   int64        `json:"latency,omitempty"`
	ErrorRate float64      `json:"error_rate,omitempty"`
	Details   any          `json:"details,omitempty"`
}

// SystemPerformance represents system-wide performance metrics
type SystemPerformance struct {
	RequestLatencyP50 int64   `json:"request_latency_p50"`
	RequestLatencyP95 int64   `json:"request_latency_p95"`
	RequestLatencyP99 int64   `json:"request_latency_p99"`
	ErrorRate         float64 `json:"error_rate"`
	RequestsPerSecond int     `json:"requests_per_second"`
	ActiveConnections int     `json:"active_connections"`
	QueueDepth        int     `json:"queue_depth"`
}

// CapacityAnalysis represents capacity utilization
type CapacityAnalysis struct {
	CPUUsage            float64  `json:"cpu_usage"`
	MemoryUsage         float64  `json:"memory_usage"`
	ConnectionUsage     float64  `json:"connection_usage"`
	CacheUsage          float64  `json:"cache_usage"`
	ProjectedExhaustion *string  `json:"projected_exhaustion,omitempty"`
	Bottlenecks         []string `json:"bottlenecks,omitempty"`
}

// SystemAlert represents an active system alert
type SystemAlert struct {
	ID           string    `json:"id"`
	Severity     string    `json:"severity"` // warning, critical
	Component    string    `json:"component"`
	Message      string    `json:"message"`
	DetectedAt   time.Time `json:"detected_at"`
	Acknowledged bool      `json:"acknowledged"`
}

// HealthDataPoint represents a historical health data point
type HealthDataPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Score     int       `json:"score"`
	Status    string    `json:"status"`
}

// AttachmentAccessResponse represents secure attachment access information
type AttachmentAccessResponse struct {
	AttachmentID     string    `json:"attachment_id"`
	AccessURL        string    `json:"access_url"`
	URLExpiry        time.Time `json:"url_expiry"`
	Filename         string    `json:"filename"`
	ContentType      string    `json:"content_type"`
	Size             int64     `json:"size"`
	Checksum         string    `json:"checksum"`
	AccessMethod     string    `json:"access_method"` // stream, download
	MaxDownloads     int       `json:"max_downloads"`
	CurrentDownloads int       `json:"current_downloads"`
}

// AddAccountRequest represents a request to add an email account
type AddAccountRequest struct {
	Email           string         `json:"email"`
	AuthType        AuthType       `json:"auth_type"`
	Password        string         `json:"password,omitempty"`
	AccessToken     string         `json:"access_token,omitempty"`
	RefreshToken    string         `json:"refresh_token,omitempty"`
	IMAPHost        string         `json:"imap_host"`
	IMAPPort        int            `json:"imap_port"`
	IMAPEncryption  EncryptionType `json:"imap_encryption"`
	SMTPHost        string         `json:"smtp_host"`
	SMTPPort        int            `json:"smtp_port"`
	SMTPEncryption  EncryptionType `json:"smtp_encryption"`
	ConnectionLimit int            `json:"connection_limit"`
	SyncSettings    SyncSettings   `json:"sync_settings"`
	ProxyConfig     *ProxySettings `json:"proxy_config,omitempty"`
}

// AddAccountResponse represents the response after adding an account
type AddAccountResponse struct {
	AccountID             string         `json:"account_id"`
	Status                AccountStatus  `json:"status"`
	ConnectionEstablished bool           `json:"connection_established"`
	InitialSyncStatus     string         `json:"initial_sync_status"`
	InitialSyncProgress   int            `json:"initial_sync_progress"`
	MessageCount          int            `json:"message_count"`
	ResourceAllocation    ResourceStatus `json:"resource_allocation"`
}
