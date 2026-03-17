package envelopequeue

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"webmail_engine/internal/models"
)

// MemoryEnvelopeQueue is a channel-based in-memory queue
// Designed for single-process testing where sync and processor run together
// Uses channels for efficient producer-consumer communication
type MemoryEnvelopeQueue struct {
	mu           sync.RWMutex
	highChan     chan *models.EnvelopeQueueItem
	normalChan   chan *models.EnvelopeQueueItem
	lowChan      chan *models.EnvelopeQueueItem
	itemStore    map[string]*models.EnvelopeQueueItem // For status tracking
	closed       bool
	maxQueueSize int
	stats        MemoryQueueStats
}

// MemoryQueueStats holds statistics for channel-based queue
type MemoryQueueStats struct {
	mu               sync.RWMutex
	EnqueuedCount    int64 `json:"enqueued_count"`
	DequeuedCount    int64 `json:"dequeued_count"`
	ProcessedCount   int64 `json:"processed_count"`
	FailedCount      int64 `json:"failed_count"`
	CurrentQueueSize int   `json:"current_queue_size"`
}

// MemoryEnvelopeQueueConfig configures the channel-based queue
type MemoryEnvelopeQueueConfig struct {
	MaxQueueSize int `json:"max_queue_size"` // Max items per priority channel
}

// DefaultMemoryEnvelopeQueueConfig returns default channel queue config
func DefaultMemoryEnvelopeQueueConfig() *MemoryEnvelopeQueueConfig {
	return &MemoryEnvelopeQueueConfig{
		MaxQueueSize: 1000, // 1000 items per priority level
	}
}

// NewMemoryEnvelopeQueue creates a new channel-based envelope queue
// This queue is designed for single-process testing
func NewMemoryEnvelopeQueue(config *MemoryEnvelopeQueueConfig) *MemoryEnvelopeQueue {
	if config == nil {
		config = DefaultMemoryEnvelopeQueueConfig()
	}

	return &MemoryEnvelopeQueue{
		highChan:     make(chan *models.EnvelopeQueueItem, config.MaxQueueSize),
		normalChan:   make(chan *models.EnvelopeQueueItem, config.MaxQueueSize),
		lowChan:      make(chan *models.EnvelopeQueueItem, config.MaxQueueSize),
		itemStore:    make(map[string]*models.EnvelopeQueueItem),
		maxQueueSize: config.MaxQueueSize,
	}
}

// Enqueue adds an envelope to the appropriate priority channel
func (q *MemoryEnvelopeQueue) Enqueue(ctx context.Context, envelope *models.EnvelopeQueueItem, opts *EnqueueOptions) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return ErrQueueUnavailable
	}

	if opts == nil {
		opts = DefaultEnqueueOptions()
	}

	// Initialize envelope
	envelope.Priority = opts.Priority
	envelope.MaxRetries = opts.MaxRetries
	envelope.Status = models.EnvelopeStatusPending
	envelope.EnqueuedAt = time.Now()

	// Store for status tracking
	q.itemStore[envelope.ID] = envelope

	// Select channel based on priority
	var targetChan chan *models.EnvelopeQueueItem
	switch opts.Priority {
	case models.PriorityHigh:
		targetChan = q.highChan
	case models.PriorityNormal:
		targetChan = q.normalChan
	case models.PriorityLow:
		targetChan = q.lowChan
	default:
		targetChan = q.normalChan
	}

	// Try to enqueue with context support
	select {
	case targetChan <- envelope:
		q.stats.mu.Lock()
		q.stats.EnqueuedCount++
		q.stats.CurrentQueueSize = len(q.highChan) + len(q.normalChan) + len(q.lowChan)
		q.stats.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Channel full
		return fmt.Errorf("queue full for priority %s (max: %d)", opts.Priority, q.maxQueueSize)
	}
}

// DequeuePending retrieves pending envelopes - for channel queue, this returns from store
// For actual processing, use DequeueChannel which blocks on channels
func (q *MemoryEnvelopeQueue) DequeuePending(ctx context.Context, accountID string, limit int) ([]*models.EnvelopeQueueItem, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if q.closed {
		return nil, ErrQueueUnavailable
	}

	var pending []*models.EnvelopeQueueItem
	for _, item := range q.itemStore {
		if item.Status == models.EnvelopeStatusPending && item.AccountID == accountID {
			pending = append(pending, item)
		}
		if len(pending) >= limit {
			break
		}
	}

	sortEnvelopesByPriority(pending)
	return pending, nil
}

// DequeueChannel blocks until an envelope is available or context is cancelled
// Returns envelope from highest priority channel available
func (q *MemoryEnvelopeQueue) DequeueChannel(ctx context.Context) (*models.EnvelopeQueueItem, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case env := <-q.highChan:
			q.stats.mu.Lock()
			q.stats.DequeuedCount++
			q.stats.CurrentQueueSize = len(q.highChan) + len(q.normalChan) + len(q.lowChan)
			q.stats.mu.Unlock()
			return env, nil
		case env := <-q.normalChan:
			q.stats.mu.Lock()
			q.stats.DequeuedCount++
			q.stats.CurrentQueueSize = len(q.highChan) + len(q.normalChan) + len(q.lowChan)
			q.stats.mu.Unlock()
			return env, nil
		case env := <-q.lowChan:
			q.stats.mu.Lock()
			q.stats.DequeuedCount++
			q.stats.CurrentQueueSize = len(q.highChan) + len(q.normalChan) + len(q.lowChan)
			q.stats.mu.Unlock()
			return env, nil
		}
	}
}

// DequeueChannelWithPriority dequeues from a specific priority channel
func (q *MemoryEnvelopeQueue) DequeueChannelWithPriority(ctx context.Context, priority models.EnvelopeProcessingPriority) (*models.EnvelopeQueueItem, error) {
	var targetChan chan *models.EnvelopeQueueItem
	switch priority {
	case models.PriorityHigh:
		targetChan = q.highChan
	case models.PriorityLow:
		targetChan = q.lowChan
	default:
		targetChan = q.normalChan
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case env := <-targetChan:
		q.stats.mu.Lock()
		q.stats.DequeuedCount++
		q.stats.CurrentQueueSize = len(q.highChan) + len(q.normalChan) + len(q.lowChan)
		q.stats.mu.Unlock()
		return env, nil
	}
}

// UpdateStatus updates the processing status of an envelope
func (q *MemoryEnvelopeQueue) UpdateStatus(ctx context.Context, id string, status models.EnvelopeProcessingStatus, lastError string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return ErrQueueUnavailable
	}

	envelope, exists := q.itemStore[id]
	if !exists {
		return ErrNotFound
	}

	envelope.Status = status
	envelope.LastError = lastError

	now := time.Now()
	switch status {
	case models.EnvelopeStatusProcessing:
		envelope.ProcessingAt = &now
	case models.EnvelopeStatusCompleted:
		envelope.CompletedAt = &now
		q.stats.mu.Lock()
		q.stats.ProcessedCount++
		q.stats.mu.Unlock()
	case models.EnvelopeStatusFailed, models.EnvelopeStatusSkipped:
		envelope.CompletedAt = &now
		q.stats.mu.Lock()
		q.stats.FailedCount++
		q.stats.mu.Unlock()
	}

	return nil
}

// GetStats returns queue statistics
func (q *MemoryEnvelopeQueue) GetStats(ctx context.Context, accountID string) (*EnvelopeQueueStats, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	stats := &EnvelopeQueueStats{}
	for _, env := range q.itemStore {
		if accountID != "" && env.AccountID != accountID {
			continue
		}
		stats.Total++
		switch env.Status {
		case models.EnvelopeStatusPending:
			stats.Pending++
		case models.EnvelopeStatusProcessing:
			stats.Processing++
		case models.EnvelopeStatusCompleted:
			stats.Completed++
		case models.EnvelopeStatusFailed, models.EnvelopeStatusSkipped:
			stats.Failed++
		}
	}

	return stats, nil
}

// GetChannelStats returns channel-specific statistics
func (q *MemoryEnvelopeQueue) GetChannelStats() *MemoryQueueStats {
	q.stats.mu.RLock()
	defer q.stats.mu.RUnlock()

	statsCopy := q.stats
	return &statsCopy
}

// CleanupOld removes old completed/failed envelopes
func (q *MemoryEnvelopeQueue) CleanupOld(ctx context.Context, olderThan time.Duration) (int64, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return 0, ErrQueueUnavailable
	}

	cutoff := time.Now().Add(-olderThan)
	var removed int64

	for id, env := range q.itemStore {
		if env.CompletedAt != nil && env.CompletedAt.Before(cutoff) {
			delete(q.itemStore, id)
			removed++
		}
	}

	return removed, nil
}

// GetPendingByPriority returns pending envelopes grouped by priority
func (q *MemoryEnvelopeQueue) GetPendingByPriority(ctx context.Context, accountID string) (map[models.EnvelopeProcessingPriority][]*models.EnvelopeQueueItem, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	result := map[models.EnvelopeProcessingPriority][]*models.EnvelopeQueueItem{
		models.PriorityHigh:   {},
		models.PriorityNormal: {},
		models.PriorityLow:    {},
	}

	for _, env := range q.itemStore {
		if accountID != "" && env.AccountID != accountID {
			continue
		}
		if env.Status == models.EnvelopeStatusPending {
			result[env.Priority] = append(result[env.Priority], env)
		}
	}

	return result, nil
}

// MarkForRetry marks a failed envelope for retry
func (q *MemoryEnvelopeQueue) MarkForRetry(ctx context.Context, id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return ErrQueueUnavailable
	}

	envelope, exists := q.itemStore[id]
	if !exists {
		return ErrNotFound
	}

	if envelope.RetryCount >= envelope.MaxRetries {
		return ErrMaxRetriesExceeded
	}

	envelope.Status = models.EnvelopeStatusPending
	envelope.LastError = ""
	envelope.RetryCount++
	envelope.ProcessingAt = nil

	// Re-enqueue to appropriate channel
	var targetChan chan *models.EnvelopeQueueItem
	switch envelope.Priority {
	case models.PriorityHigh:
		targetChan = q.highChan
	case models.PriorityLow:
		targetChan = q.lowChan
	default:
		targetChan = q.normalChan
	}

	select {
	case targetChan <- envelope:
		return nil
	default:
		// Channel full, keep in store for manual retry
		log.Printf("Warning: Could not re-envelope %s for retry - queue full", id)
		return nil
	}
}

// Close releases resources
func (q *MemoryEnvelopeQueue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return nil
	}

	q.closed = true
	close(q.highChan)
	close(q.normalChan)
	close(q.lowChan)
	q.itemStore = nil

	return nil
}

// IsClosed returns whether the queue is closed
func (q *MemoryEnvelopeQueue) IsClosed() bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.closed
}

// Len returns total items across all channels
func (q *MemoryEnvelopeQueue) Len() int {
	return len(q.highChan) + len(q.normalChan) + len(q.lowChan)
}

// LenByPriority returns items in a specific priority channel
func (q *MemoryEnvelopeQueue) LenByPriority(priority models.EnvelopeProcessingPriority) int {
	q.mu.RLock()
	defer q.mu.RUnlock()

	switch priority {
	case models.PriorityHigh:
		return len(q.highChan)
	case models.PriorityLow:
		return len(q.lowChan)
	default:
		return len(q.normalChan)
	}
}
