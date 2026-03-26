package service

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"webmail_engine/internal/pool"
)

// ProcessorStats holds runtime statistics for envelope processing
type ProcessorStats struct {
	mu                sync.RWMutex
	ProcessedCount    int64         `json:"processed_count"`
	FailedCount       int64         `json:"failed_count"`
	SkippedCount      int64         `json:"skipped_count"`
	LastProcessedAt   time.Time     `json:"last_processed_at"`
	AvgProcessingTime time.Duration `json:"avg_processing_time"`
	CurrentQueueSize  int64         `json:"current_queue_size"`
}

// EnvelopeProcessorService handles envelope processing operations.
type EnvelopeProcessorService struct {
	accountService *AccountService
	sessionPool    *pool.IMAPSessionPool
	stats          *ProcessorStats
}

// NewEnvelopeProcessorService creates a new envelope processor service.
func NewEnvelopeProcessorService(
	accountService *AccountService,
	sessionPool *pool.IMAPSessionPool,
) *EnvelopeProcessorService {
	return &EnvelopeProcessorService{
		accountService: accountService,
		sessionPool:    sessionPool,
		stats: &ProcessorStats{
			ProcessedCount:    0,
			FailedCount:       0,
			LastProcessedAt:   time.Time{},
			AvgProcessingTime: 0,
		},
	}
}

// EnvelopeQueueItem represents an envelope to be processed.
type EnvelopeQueueItem struct {
	ID         string    `json:"id"`
	AccountID  string    `json:"account_id"`
	FolderName string    `json:"folder_name"`
	UID        uint32    `json:"uid"`
	MessageID  string    `json:"message_id"`
	From       string    `json:"from"`
	Subject    string    `json:"subject"`
	Date       time.Time `json:"date"`
	Size       int64     `json:"size"`
}

// ProcessEnvelope processes a single envelope by fetching the message and extracting data.
func (s *EnvelopeProcessorService) ProcessEnvelope(ctx context.Context, envelope *EnvelopeQueueItem) error {
	startTime := time.Now()

	// Get account with credentials
	account, err := s.accountService.GetAccountWithCredentials(ctx, envelope.AccountID)
	if err != nil {
		return fmt.Errorf("failed to get account: %w", err)
	}

	// Connect using session pool
	imapConfig := pool.IMAPConfig{
		Host:       account.IMAPConfig.Host,
		Port:       account.IMAPConfig.Port,
		Username:   account.IMAPConfig.Username,
		Password:   account.IMAPConfig.Password,
		Encryption: account.IMAPConfig.Encryption,
	}

	client, release, err := s.sessionPool.Acquire(ctx, envelope.AccountID, imapConfig)
	if err != nil {
		return fmt.Errorf("failed to acquire IMAP session: %w", err)
	}
	defer release()

	// Fetch full message with body
	envelopes, err := client.FetchMessages([]uint32{envelope.UID}, true)
	if err != nil {
		return fmt.Errorf("failed to fetch message: %w", err)
	}

	if len(envelopes) == 0 {
		return fmt.Errorf("message not found")
	}

	fetchedEnvelope := envelopes[0]

	// Process the message (extract data, update cache, etc.)
	// This would integrate with message service to:
	// 1. Store message in cache
	// 2. Extract links if enabled
	// 3. Process attachments if enabled
	// 4. Update search index
	// 5. Trigger webhooks for new messages

	log.Printf("Processed envelope %s from %s (subject: %s)",
		envelope.ID, fetchedEnvelope.MessageID, fetchedEnvelope.Subject)

	// Update stats
	s.stats.mu.Lock()
	s.stats.ProcessedCount++
	s.stats.LastProcessedAt = time.Now()
	processingTime := time.Since(startTime)
	s.stats.AvgProcessingTime = processingTime
	s.stats.mu.Unlock()

	return nil
}

// GetProcessorStats returns processor statistics.
func (s *EnvelopeProcessorService) GetProcessorStats(ctx context.Context) (*ProcessorStats, error) {
	s.stats.mu.RLock()
	defer s.stats.mu.RUnlock()

	// Return a copy to avoid race conditions
	return &ProcessorStats{
		ProcessedCount:    s.stats.ProcessedCount,
		FailedCount:       s.stats.FailedCount,
		SkippedCount:      s.stats.SkippedCount,
		LastProcessedAt:   s.stats.LastProcessedAt,
		AvgProcessingTime: s.stats.AvgProcessingTime,
		CurrentQueueSize:  s.stats.CurrentQueueSize,
	}, nil
}
