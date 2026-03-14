package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"webmail_engine/internal/models"
)

// WebhookHandler processes incoming webhook events
type WebhookHandler struct {
	mu            sync.RWMutex
	secretKey     string
	processedEvents map[string]time.Time // eventID -> processedAt
	eventHandlers map[models.EventType][]EventHandler
	stats         *WebhookStats
}

// EventHandler handles specific event types
type EventHandler func(ctx context.Context, event *models.WebhookEvent) error

// WebhookStats tracks webhook processing statistics
type WebhookStats struct {
	TotalReceived   int64
	TotalProcessed  int64
	TotalFailed     int64
	Duplicates      int64
	InvalidSig      int64
	LastUpdate      time.Time
}

// WebhookHandlerConfig holds webhook handler configuration
type WebhookHandlerConfig struct {
	SecretKey string
}

// NewWebhookHandler creates a new webhook handler
func NewWebhookHandler(config WebhookHandlerConfig) *WebhookHandler {
	return &WebhookHandler{
		secretKey:     config.SecretKey,
		processedEvents: make(map[string]time.Time),
		eventHandlers: make(map[models.EventType][]EventHandler),
		stats: &WebhookStats{
			LastUpdate: time.Now(),
		},
	}
}

// HandleWebhook processes an incoming webhook request
func (h *WebhookHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondWebhookError(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()
	
	// Get signature from header
	signature := r.Header.Get("X-Webhook-Signature")
	if signature == "" {
		h.stats.InvalidSig++
		respondWebhookError(w, "Missing signature", http.StatusUnauthorized)
		return
	}
	
	// Verify signature
	if !h.verifySignature(body, signature) {
		h.stats.InvalidSig++
		respondWebhookError(w, "Invalid signature", http.StatusUnauthorized)
		return
	}
	
	// Parse event
	var event models.WebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		respondWebhookError(w, "Invalid event format", http.StatusBadRequest)
		return
	}
	
	h.stats.TotalReceived++
	
	// Check for duplicate
	if h.isDuplicate(event.EventID) {
		h.stats.Duplicates++
		// Return success but indicate it's a duplicate
		respondWebhookResponse(w, &models.WebhookResponse{
			Status:    "duplicate",
			ReceiptID: generateReceiptID(),
		})
		return
	}
	
	// Process event asynchronously
	go h.processEvent(r.Context(), &event)
	
	// Return immediate acceptance
	respondWebhookResponse(w, &models.WebhookResponse{
		Status:    "accepted",
		ReceiptID: generateReceiptID(),
	})
}

// RegisterHandler registers an event handler for a specific event type
func (h *WebhookHandler) RegisterHandler(eventType models.EventType, handler EventHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	
	h.eventHandlers[eventType] = append(h.eventHandlers[eventType], handler)
}

// processEvent processes a webhook event
func (h *WebhookHandler) processEvent(ctx context.Context, event *models.WebhookEvent) {
	// Mark as processed
	h.markAsProcessed(event.EventID)
	
	// Get handlers for this event type
	h.mu.RLock()
	handlers := h.eventHandlers[event.EventType]
	h.mu.RUnlock()
	
	// Process with each handler
	var lastError error
	for _, handler := range handlers {
		if err := handler(ctx, event); err != nil {
			lastError = err
		}
	}
	
	// Update stats
	if lastError != nil {
		h.stats.TotalFailed++
	} else {
		h.stats.TotalProcessed++
	}
}

// verifySignature verifies the webhook signature
func (h *WebhookHandler) verifySignature(body []byte, signature string) bool {
	if h.secretKey == "" {
		// Skip verification if no secret key configured
		return true
	}
	
	// Calculate expected signature
	mac := hmac.New(sha256.New, []byte(h.secretKey))
	mac.Write(body)
	expectedSig := hex.EncodeToString(mac.Sum(nil))
	
	// Constant-time comparison
	return hmac.Equal([]byte(signature), []byte(expectedSig))
}

// isDuplicate checks if an event has been processed before
func (h *WebhookHandler) isDuplicate(eventID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	
	_, exists := h.processedEvents[eventID]
	return exists
}

// markAsProcessed marks an event as processed
func (h *WebhookHandler) markAsProcessed(eventID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	
	h.processedEvents[eventID] = time.Now()
}

// GetStats returns webhook statistics
func (h *WebhookHandler) GetStats() *WebhookStats {
	h.mu.RLock()
	defer h.mu.RUnlock()
	
	stats := *h.stats
	stats.LastUpdate = time.Now()
	return &stats
}

// CleanupOldEvents removes old processed events from memory
func (h *WebhookHandler) CleanupOldEvents(maxAge time.Duration) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	
	now := time.Now()
	removed := 0
	
	for eventID, processedAt := range h.processedEvents {
		if now.Sub(processedAt) > maxAge {
			delete(h.processedEvents, eventID)
			removed++
		}
	}
	
	return removed
}

// StartCleanup starts periodic cleanup of old events
func (h *WebhookHandler) StartCleanup(ctx context.Context, interval, maxAge time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.CleanupOldEvents(maxAge)
		}
	}
}

// GenerateSignature generates a signature for a webhook payload
func GenerateSignature(payload []byte, secretKey string) string {
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// Helper functions

func respondWebhookResponse(w http.ResponseWriter, response *models.WebhookResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func respondWebhookError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": message,
	})
}

func generateReceiptID() string {
	return fmt.Sprintf("rcpt_%d", time.Now().UnixNano())
}

// Default event handlers

// NewMessageHandler handles new message events
func NewMessageHandler() EventHandler {
	return func(ctx context.Context, event *models.WebhookEvent) error {
		// Process new message event
		// In production, this would:
		// 1. Fetch the message
		// 2. Parse and store it
		// 3. Notify interested parties
		return nil
	}
}

// MessageDeletedHandler handles message deletion events
func MessageDeletedHandler() EventHandler {
	return func(ctx context.Context, event *models.WebhookEvent) error {
		// Process message deletion
		// In production, this would:
		// 1. Remove from cache
		// 2. Update indexes
		return nil
	}
}

// AuthErrorHandler handles authentication error events
func AuthErrorHandler() EventHandler {
	return func(ctx context.Context, event *models.WebhookEvent) error {
		// Process auth error
		// In production, this would:
		// 1. Update account status
		// 2. Notify administrators
		// 3. Pause sync operations
		return nil
	}
}
