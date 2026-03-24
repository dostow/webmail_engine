package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"webmail_engine/internal/taskmaster"
)

// SpamCheckTask performs spam detection on emails.
type SpamCheckTask struct {
	// SpamService is the spam detection service (injected dependency)
	SpamService SpamDetectionService
}

// SpamDetectionService defines the interface for spam detection operations.
type SpamDetectionService interface {
	CheckSpam(ctx context.Context, email *SpamCheckEmail) (*SpamCheckResult, error)
	UpdateSpamRules(ctx context.Context, rules []SpamRule) error
	GetSpamStats(ctx context.Context, accountID string) (*SpamStats, error)
}

// SpamCheckEmail represents an email to check for spam.
type SpamCheckEmail struct {
	AccountID   string            `json:"account_id"`
	MessageID   string            `json:"message_id"`
	From        string            `json:"from"`
	Subject     string            `json:"subject"`
	Body        string            `json:"body"`
	Headers     map[string]string `json:"headers,omitempty"`
	Attachments []AttachmentInfo  `json:"attachments,omitempty"`
}

// AttachmentInfo holds attachment metadata for spam checking.
type AttachmentInfo struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	Hash        string `json:"hash,omitempty"`
}

// SpamCheckResult holds the result of a spam check.
type SpamCheckResult struct {
	IsSpam      bool      `json:"is_spam"`
	Confidence  float64   `json:"confidence"`
	Score       float64   `json:"score"`
	Reasons     []string  `json:"reasons"`
	Action      string    `json:"action"` // "allow", "quarantine", "reject"
	CheckedAt   time.Time `json:"checked_at"`
}

// SpamRule represents a spam filtering rule.
type SpamRule struct {
	ID         string `json:"id"`
	Type       string `json:"type"` // "sender", "subject", "content", "attachment"
	Pattern    string `json:"pattern"`
	Action     string `json:"action"`
	Priority   int    `json:"priority"`
	Enabled    bool   `json:"enabled"`
}

// SpamStats holds spam detection statistics.
type SpamStats struct {
	TotalChecked   int `json:"total_checked"`
	SpamDetected   int `json:"spam_detected"`
	FalsePositives int `json:"false_positives"`
	LastUpdated    time.Time `json:"last_updated"`
}

// SpamCheckPayload is the payload for spam check tasks.
type SpamCheckPayload struct {
	AccountID string          `json:"account_id"`
	MessageID string          `json:"message_id"`
	From      string          `json:"from"`
	Subject   string          `json:"subject"`
	Body      string          `json:"body,omitempty"`
	Headers   json.RawMessage `json:"headers,omitempty"`
	Options   SpamCheckOptions `json:"options,omitempty"`
}

// SpamCheckOptions configures the spam check behavior.
type SpamCheckOptions struct {
	CheckHeaders    bool `json:"check_headers"`
	CheckBody       bool `json:"check_body"`
	CheckAttachments bool `json:"check_attachments"`
	QuarantineIfSpam bool `json:"quarantine_if_spam"`
}

// ID returns the unique task identifier.
func (t *SpamCheckTask) ID() string {
	return "spam_check"
}

// Execute performs spam detection on an email.
func (t *SpamCheckTask) Execute(ctx context.Context, payload []byte) error {
	// Parse payload
	var req SpamCheckPayload
	if err := json.Unmarshal(payload, &req); err != nil {
		return taskmaster.NewNonRetryableTaskError(t.ID(), "invalid payload format", err)
	}

	// Validate required fields
	if req.AccountID == "" {
		return taskmaster.NewNonRetryableTaskError(t.ID(), "account_id is required", nil)
	}
	if req.MessageID == "" {
		return taskmaster.NewNonRetryableTaskError(t.ID(), "message_id is required", nil)
	}
	if req.From == "" {
		return taskmaster.NewNonRetryableTaskError(t.ID(), "from address is required", nil)
	}

	// Check if service is available
	if t.SpamService == nil {
		// If no spam service configured, skip silently (not an error)
		fmt.Printf("Spam check skipped for message %s: service not configured\n", req.MessageID)
		return nil
	}

	// Build spam check email
	email := &SpamCheckEmail{
		AccountID: req.AccountID,
		MessageID: req.MessageID,
		From:      req.From,
		Subject:   req.Subject,
		Body:      req.Body,
	}

	// Parse headers if provided
	if len(req.Headers) > 0 {
		if err := json.Unmarshal(req.Headers, &email.Headers); err != nil {
			return taskmaster.NewNonRetryableTaskError(t.ID(), "invalid headers format", err)
		}
	}

	// Apply default options
	opts := req.Options
	if opts.CheckHeaders = true; true { // Always check headers
		opts.CheckHeaders = true
	}

	// Perform spam check
	result, err := t.SpamService.CheckSpam(ctx, email)
	if err != nil {
		return taskmaster.WrapError(t.ID(), "failed to check spam", err)
	}

	// Log result
	action := "allowed"
	if result.IsSpam {
		action = result.Action
		if action == "" {
			action = "quarantined"
		}
	}
	fmt.Printf("Spam check completed for message %s: is_spam=%v, confidence=%.2f, action=%s\n",
		req.MessageID, result.IsSpam, result.Confidence, action)

	return nil
}

// Ensure interface compliance
var _ taskmaster.Task = (*SpamCheckTask)(nil)
