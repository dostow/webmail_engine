package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"webmail_engine/internal/service"
	"webmail_engine/internal/taskmaster"
)

// EnvelopeProcessorTask processes email envelopes.
type EnvelopeProcessorTask struct {
	ProcessorService ProcessorService
}

// ProcessorService defines the interface for envelope processing.
type ProcessorService interface {
	ProcessEnvelope(ctx context.Context, envelope *service.EnvelopeQueueItem) error
	GetProcessorStats(ctx context.Context) (*service.ProcessorStats, error)
}

// EnvelopeProcessorPayload is the payload for envelope processor tasks.
type EnvelopeProcessorPayload struct {
	EnvelopeID string         `json:"envelope_id"`
	AccountID  string         `json:"account_id"`
	FolderName string         `json:"folder_name"`
	UID        uint32         `json:"uid"`
	Options    ProcessOptions `json:"options,omitempty"`
}

// ProcessOptions configures envelope processing behavior.
type ProcessOptions struct {
	FetchBody          bool `json:"fetch_body"`
	ExtractLinks       bool `json:"extract_links"`
	ProcessAttachments bool `json:"process_attachments"`
	UpdateSearchIndex  bool `json:"update_search_index"`
	TriggerWebhooks    bool `json:"trigger_webhooks"`
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
	envelope := &service.EnvelopeQueueItem{
		ID:         req.EnvelopeID,
		AccountID:  req.AccountID,
		FolderName: req.FolderName,
		UID:        req.UID,
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
