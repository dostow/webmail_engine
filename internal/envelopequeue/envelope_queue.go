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

// EnvelopeQueue defines the interface for envelope queue operations.
// With taskmaster handling task execution, this interface is minimal:
// - Enqueue: Submit an envelope for processing (dispatches to taskmaster)
// - Close: Release resources
type EnvelopeQueue interface {
	// Enqueue submits an envelope for processing.
	// Implementations dispatch the envelope to taskmaster for execution.
	Enqueue(ctx context.Context, envelope *models.EnvelopeQueueItem, opts *EnqueueOptions) error

	// Close releases resources (connections, workers, etc.)
	Close() error
}
