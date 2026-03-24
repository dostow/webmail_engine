package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"webmail_engine/internal/taskmaster"
)

// EnvelopeProcessorTask processes email envelopes from the queue.
// This migrates the existing processor_worker logic to the taskmaster pattern.
type EnvelopeProcessorTask struct {
	// ProcessorService handles the actual envelope processing
	ProcessorService EnvelopeProcessorService
}

// EnvelopeProcessorService defines the interface for envelope processing operations.
type EnvelopeProcessorService interface {
	ProcessEnvelope(ctx context.Context, envelope *EnvelopeQueueItem) error
	GetProcessorStats(ctx context.Context) (*ProcessorStats, error)
}

// EnvelopeQueueItem represents an envelope waiting to be processed.
type EnvelopeQueueItem struct {
	// ID is the unique envelope identifier
	ID string `json:"id"`

	// AccountID is the account this envelope belongs to
	AccountID string `json:"account_id"`

	// FolderName is the IMAP folder name
	FolderName string `json:"folder_name"`

	// UID is the IMAP UID
	UID uint32 `json:"uid"`

	// MessageID is the email Message-ID header
	MessageID string `json:"message_id"`

	// From is the sender address
	From string `json:"from"`

	// Subject is the email subject
	Subject string `json:"subject"`

	// Date is the email date
	Date time.Time `json:"date"`

	// Size is the message size in bytes
	Size int64 `json:"size"`

	// Priority is the processing priority
	Priority string `json:"priority"`

	// RetryCount is the number of processing attempts
	RetryCount int `json:"retry_count"`

	// MaxRetries is the maximum retry attempts
	MaxRetries int `json:"max_retries"`
}

// ProcessorStats holds envelope processor statistics.
type ProcessorStats struct {
	ProcessedCount    int64         `json:"processed_count"`
	FailedCount       int64         `json:"failed_count"`
	SkippedCount      int64         `json:"skipped_count"`
	LastProcessedAt   time.Time     `json:"last_processed_at"`
	AvgProcessingTime time.Duration `json:"avg_processing_time"`
	CurrentQueueSize  int64         `json:"current_queue_size"`
}

// EnvelopeProcessorPayload is the payload for envelope processor tasks.
type EnvelopeProcessorPayload struct {
	// EnvelopeID identifies the envelope to process
	EnvelopeID string `json:"envelope_id"`

	// AccountID is the account this envelope belongs to
	AccountID string `json:"account_id"`

	// FolderName is the IMAP folder name
	FolderName string `json:"folder_name"`

	// UID is the IMAP UID
	UID uint32 `json:"uid"`

	// Options configures processing behavior
	Options ProcessOptions `json:"options,omitempty"`
}

// ProcessOptions configures envelope processing behavior.
type ProcessOptions struct {
	// FetchBody determines whether to fetch the full message body
	FetchBody bool `json:"fetch_body"`

	// ExtractLinks enables link extraction from the body
	ExtractLinks bool `json:"extract_links"`

	// ProcessAttachments enables attachment processing
	ProcessAttachments bool `json:"process_attachments"`

	// UpdateSearchIndex enables search index updates
	UpdateSearchIndex bool `json:"update_search_index"`

	// TriggerWebhooks enables webhook notifications
	TriggerWebhooks bool `json:"trigger_webhooks"`
}

// ID returns the unique task identifier.
func (t *EnvelopeProcessorTask) ID() string {
	return "envelope_processor"
}

// Execute processes an email envelope.
func (t *EnvelopeProcessorTask) Execute(ctx context.Context, payload []byte) error {
	startTime := time.Now()

	// Parse payload
	var req EnvelopeProcessorPayload
	if err := json.Unmarshal(payload, &req); err != nil {
		return taskmaster.NewNonRetryableTaskError(t.ID(), "invalid payload format", err)
	}

	// Validate required fields
	if req.EnvelopeID == "" {
		return taskmaster.NewNonRetryableTaskError(t.ID(), "envelope_id is required", nil)
	}
	if req.AccountID == "" {
		return taskmaster.NewNonRetryableTaskError(t.ID(), "account_id is required", nil)
	}

	// Check if service is available
	if t.ProcessorService == nil {
		return taskmaster.NewSystemTaskError(t.ID(), "envelope processor service not configured", nil)
	}

	// Build envelope item
	envelope := &EnvelopeQueueItem{
		ID:         req.EnvelopeID,
		AccountID:  req.AccountID,
		FolderName: req.FolderName,
		UID:        req.UID,
	}

	// Apply default options
	opts := req.Options
	if opts.FetchBody = true; true { // Default to fetching body
		opts.FetchBody = true
	}
	if opts.TriggerWebhooks = true; true { // Default to triggering webhooks
		opts.TriggerWebhooks = true
	}

	// Log processing start
	fmt.Printf("Processing envelope %s (account=%s, folder=%s, uid=%d)\n",
		req.EnvelopeID, req.AccountID, req.FolderName, req.UID)

	// Process envelope
	if err := t.ProcessorService.ProcessEnvelope(ctx, envelope); err != nil {
		return taskmaster.WrapError(t.ID(), "envelope processing failed", err)
	}

	// Log success
	fmt.Printf("Envelope %s processed successfully in %v\n", req.EnvelopeID, time.Since(startTime))

	return nil
}

// Ensure interface compliance
var _ taskmaster.Task = (*EnvelopeProcessorTask)(nil)
