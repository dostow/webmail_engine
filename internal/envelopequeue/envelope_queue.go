package envelopequeue

import (
	"context"
	"errors"
	"time"

	"webmail_engine/internal/models"
)

// Standard errors for envelope queue operations
var (
	ErrQueueUnavailable   = errors.New("envelope queue unavailable")
	ErrMaxRetriesExceeded = errors.New("max retries exceeded")
	ErrNotFound           = errors.New("envelope not found")
)

// EnvelopeQueueStats represents envelope queue statistics
type EnvelopeQueueStats struct {
	Pending    int64 `json:"pending"`
	Processing int64 `json:"processing"`
	Completed  int64 `json:"completed"`
	Failed     int64 `json:"failed"`
	Total      int64 `json:"total"`
}

// EnqueueOptions represents options for enqueuing an envelope
type EnqueueOptions struct {
	Priority   models.EnvelopeProcessingPriority
	MaxRetries int
	Delay      time.Duration // Delay before processing
}

// DefaultEnqueueOptions returns default enqueue options
func DefaultEnqueueOptions() *EnqueueOptions {
	return &EnqueueOptions{
		Priority:   models.PriorityNormal,
		MaxRetries: 3,
		Delay:      0,
	}
}

// EnvelopeQueue defines the interface for envelope queue operations
// This is a separate domain from account storage - focused on async message processing
type EnvelopeQueue interface {
	// Enqueue adds an envelope to the processing queue
	Enqueue(ctx context.Context, envelope *models.EnvelopeQueueItem, opts *EnqueueOptions) error

	// Dequeue retrieves pending envelopes for processing (ordered by priority)
	// Returns envelopes sorted by: priority (high->normal->low), then by date (newest first)
	DequeuePending(ctx context.Context, accountID string, limit int) ([]*models.EnvelopeQueueItem, error)

	// UpdateStatus updates the processing status of an envelope
	UpdateStatus(ctx context.Context, id string, status models.EnvelopeProcessingStatus, lastError string) error

	// GetStats returns queue statistics for monitoring
	GetStats(ctx context.Context, accountID string) (*EnvelopeQueueStats, error)

	// CleanupOld removes old completed/failed envelopes to prevent queue bloat
	CleanupOld(ctx context.Context, olderThan time.Duration) (int64, error)

	// GetPendingByPriority returns pending envelopes grouped by priority
	GetPendingByPriority(ctx context.Context, accountID string) (map[models.EnvelopeProcessingPriority][]*models.EnvelopeQueueItem, error)

	// MarkForRetry marks a failed envelope for retry
	MarkForRetry(ctx context.Context, id string) error

	// Close releases resources (connections, workers, etc.)
	Close() error
}

// sortEnvelopesByPriority sorts envelopes by priority (high->normal->low) then by date (newest first)
func sortEnvelopesByPriority(envelopes []*models.EnvelopeQueueItem) {
	// Simple bubble sort for in-memory queue
	n := len(envelopes)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			shouldSwap := false

			// Compare priority
			if priorityValue(envelopes[j].Priority) < priorityValue(envelopes[j+1].Priority) {
				shouldSwap = true
			} else if priorityValue(envelopes[j].Priority) == priorityValue(envelopes[j+1].Priority) {
				// Same priority, sort by date (newest first)
				if envelopes[j].Date.Before(envelopes[j+1].Date) {
					shouldSwap = true
				}
			}

			if shouldSwap {
				envelopes[j], envelopes[j+1] = envelopes[j+1], envelopes[j]
			}
		}
	}
}

// priorityValue returns numeric value for priority comparison (higher = more important)
func priorityValue(p models.EnvelopeProcessingPriority) int {
	switch p {
	case models.PriorityHigh:
		return 3
	case models.PriorityNormal:
		return 2
	case models.PriorityLow:
		return 1
	default:
		return 0
	}
}
