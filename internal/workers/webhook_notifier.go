package workers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"webmail_engine/internal/models"
	"webmail_engine/internal/taskmaster"
	"webmail_engine/internal/webhook"
)

// WebhookNotifierTask sends webhook notifications for envelope events.
type WebhookNotifierTask struct {
	WebhookHandler *webhook.WebhookHandler
	WebhookURL     string
	SecretKey      string
}

// WebhookNotifierPayload is the payload for webhook notifier tasks.
type WebhookNotifierPayload struct {
	EnvelopeID string `json:"envelope_id"`
	AccountID  string `json:"account_id"`
	FolderName string `json:"folder_name"`
	UID        uint32 `json:"uid"`
	MessageID  string `json:"message_id"`
	EventType  string `json:"event_type"` // "envelope.received", "envelope.processed"
	WebhookURL string `json:"webhook_url,omitempty"`
}

// ID returns the unique task identifier.
func (t *WebhookNotifierTask) ID() string {
	return "webhook_notifier"
}

// Execute sends a webhook notification for the envelope event.
func (t *WebhookNotifierTask) Execute(ctx context.Context, payload []byte) error {
	startTime := time.Now()

	// Parse payload
	var req WebhookNotifierPayload
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

	// Determine webhook URL (task-specific or default)
	webhookURL := req.WebhookURL
	if webhookURL == "" {
		webhookURL = t.WebhookURL
	}

	if webhookURL == "" {
		return taskmaster.NewNonRetryableTaskError(t.ID(), "webhook URL not configured", nil)
	}

	// Determine event type
	eventType := req.EventType
	if eventType == "" {
		eventType = "envelope.received"
	}

	// Create webhook event
	event := &models.WebhookEvent{
		EventID:   req.EnvelopeID,
		EventType: models.EventType(eventType),
		Timestamp: time.Now(),
		AccountID: req.AccountID,
		Version:   "v1",
		Data: models.WebhookEventData{
			MessageID: req.MessageID,
		},
	}

	// Send webhook
	if err := t.sendWebhook(ctx, webhookURL, event); err != nil {
		return taskmaster.WrapError(t.ID(), "failed to send webhook", err)
	}

	// Log success
	fmt.Printf("Webhook sent for envelope %s in %v\n", req.EnvelopeID, time.Since(startTime))

	return nil
}

// sendWebhook sends a webhook event to the specified URL.
func (t *WebhookNotifierTask) sendWebhook(ctx context.Context, url string, event *models.WebhookEvent) error {
	// Serialize event
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Event", string(event.EventType))
	req.Header.Set("X-Webhook-Timestamp", event.Timestamp.Format(time.RFC3339))

	// Add signature if secret key is configured
	if t.SecretKey != "" {
		signature := t.signPayload(body)
		req.Header.Set("X-Webhook-Signature", signature)
	}

	// Send request
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send webhook: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}

// signPayload creates HMAC signature of the payload.
func (t *WebhookNotifierTask) signPayload(body []byte) string {
	h := hmac.New(sha256.New, []byte(t.SecretKey))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// Ensure interface compliance
var _ taskmaster.Task = (*WebhookNotifierTask)(nil)
