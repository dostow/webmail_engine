package service

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"webmail_engine/internal/envelopequeue"
	"webmail_engine/internal/models"
	"webmail_engine/internal/pool"
)

// EnvelopeProcessor processes email envelopes from the queue
// It fetches full message content, extracts metadata, and updates the cache
type EnvelopeProcessor struct {
	mu             sync.RWMutex
	queue          envelopequeue.EnvelopeQueue
	messageService *MessageService
	accountService *AccountService
	sessions       *pool.IMAPSessionPool
	config         *EnvelopeProcessorConfig
	workerCtx      context.Context
	workerCancel   context.CancelFunc
	isRunning      bool
	stats          ProcessorStats
}

// EnvelopeProcessorConfig holds processor configuration
type EnvelopeProcessorConfig struct {
	// Concurrency controls how many envelopes are processed in parallel
	Concurrency int `json:"concurrency"`

	// BatchSize is the number of envelopes to dequeue at once
	BatchSize int `json:"batch_size"`

	// PollInterval is how often to check for new envelopes
	PollInterval time.Duration `json:"poll_interval"`

	// CleanupInterval is how often to clean up old processed envelopes
	CleanupInterval time.Duration `json:"cleanup_interval"`

	// CleanupAge is the age threshold for envelope cleanup
	CleanupAge time.Duration `json:"cleanup_age"`

	// PriorityWeights controls processing priority
	PriorityWeights map[models.EnvelopeProcessingPriority]int `json:"priority_weights"`

	// TempStoragePath is the path for temporary file storage
	TempStoragePath string `json:"temp_storage_path"`
}

// DefaultEnvelopeProcessorConfig returns default processor configuration
func DefaultEnvelopeProcessorConfig() *EnvelopeProcessorConfig {
	return &EnvelopeProcessorConfig{
		Concurrency:     4,
		BatchSize:       20,
		PollInterval:    5 * time.Second,
		CleanupInterval: 1 * time.Hour,
		CleanupAge:      24 * time.Hour,
		TempStoragePath: "/tmp/webmail",
		PriorityWeights: map[models.EnvelopeProcessingPriority]int{
			models.PriorityHigh:   10,
			models.PriorityNormal: 5,
			models.PriorityLow:    1,
		},
	}
}

// ProcessorStats holds runtime statistics
type ProcessorStats struct {
	mu                sync.RWMutex
	ProcessedCount    int64         `json:"processed_count"`
	FailedCount       int64         `json:"failed_count"`
	SkippedCount      int64         `json:"skipped_count"`
	LastProcessedAt   time.Time     `json:"last_processed_at"`
	AvgProcessingTime time.Duration `json:"avg_processing_time"`
	CurrentQueueSize  int64         `json:"current_queue_size"`
}

// NewEnvelopeProcessor creates a new envelope processor
func NewEnvelopeProcessor(
	queue envelopequeue.EnvelopeQueue,
	messageService *MessageService,
	accountService *AccountService,
	sessions *pool.IMAPSessionPool,
	config *EnvelopeProcessorConfig,
) (*EnvelopeProcessor, error) {
	if config == nil {
		config = DefaultEnvelopeProcessorConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	processor := &EnvelopeProcessor{
		queue:          queue,
		messageService: messageService,
		accountService: accountService,
		sessions:       sessions,
		config:         config,
		workerCtx:      ctx,
		workerCancel:   cancel,
		isRunning:      false,
	}

	return processor, nil
}

// Start begins the envelope processing workers
func (p *EnvelopeProcessor) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.isRunning {
		return fmt.Errorf("processor already running")
	}

	p.isRunning = true

	// Start worker goroutines
	for i := 0; i < p.config.Concurrency; i++ {
		go p.worker(i)
	}

	// Start cleanup goroutine
	go p.cleanupLoop()

	log.Printf("Envelope processor started with %d workers", p.config.Concurrency)
	return nil
}

// Stop gracefully stops the envelope processor
func (p *EnvelopeProcessor) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.isRunning {
		return nil
	}

	log.Println("Stopping envelope processor...")
	p.workerCancel()
	p.isRunning = false

	return nil
}

// worker is the main processing loop for each worker goroutine
func (p *EnvelopeProcessor) worker(id int) {
	log.Printf("Envelope worker %d started", id)

	// Check if queue supports channel-based dequeuing
	if channelQueue, ok := p.queue.(interface {
		DequeueChannel(context.Context) (*models.EnvelopeQueueItem, error)
	}); ok {
		// Use channel-based processing (more efficient, no polling)
		p.channelWorker(id, channelQueue)
	} else {
		// Use polling-based processing
		p.pollingWorker(id)
	}
}

// channelWorker processes envelopes using channel-based dequeuing
func (p *EnvelopeProcessor) channelWorker(id int, channelQueue interface {
	DequeueChannel(context.Context) (*models.EnvelopeQueueItem, error)
}) {
	log.Printf("Envelope channel worker %d started", id)

	for {
		select {
		case <-p.workerCtx.Done():
			log.Printf("Envelope channel worker %d stopped", id)
			return
		default:
			envelope, err := channelQueue.DequeueChannel(p.workerCtx)
			if err != nil {
				if p.workerCtx.Err() != nil {
					return // Context cancelled, shutting down
				}
				log.Printf("Worker %d: error dequeuing from channel: %v", id, err)
				continue
			}
			p.processEnvelope(id, envelope)
		}
	}
}

// pollingWorker processes envelopes using polling
func (p *EnvelopeProcessor) pollingWorker(id int) {
	log.Printf("Envelope polling worker %d started", id)

	ticker := time.NewTicker(p.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.workerCtx.Done():
			log.Printf("Envelope polling worker %d stopped", id)
			return
		case <-ticker.C:
			p.processBatch(id)
		}
	}
}

// processBatch fetches and processes a batch of envelopes
func (p *EnvelopeProcessor) processBatch(workerID int) {
	// Get pending envelopes by priority (high priority first)
	priorityQueues, err := p.queue.GetPendingByPriority(p.workerCtx, "")
	if err != nil {
		log.Printf("Worker %d: failed to get pending envelopes: %v", workerID, err)
		return
	}

	// Process by priority order
	for _, priority := range []models.EnvelopeProcessingPriority{
		models.PriorityHigh,
		models.PriorityNormal,
		models.PriorityLow,
	} {
		envelopes := priorityQueues[priority]
		if len(envelopes) == 0 {
			continue
		}

		// Process up to BatchSize envelopes
		batchSize := p.config.BatchSize
		if len(envelopes) < batchSize {
			batchSize = len(envelopes)
		}

		for i := 0; i < batchSize; i++ {
			select {
			case <-p.workerCtx.Done():
				return
			default:
				p.processEnvelope(workerID, envelopes[i])
			}
		}
	}
}

// processEnvelope processes a single envelope
func (p *EnvelopeProcessor) processEnvelope(workerID int, envelope *models.EnvelopeQueueItem) {
	startTime := time.Now()

	// Update status to processing
	err := p.queue.UpdateStatus(p.workerCtx, envelope.ID, models.EnvelopeStatusProcessing, "")
	if err != nil {
		log.Printf("Worker %d: failed to update status for envelope %s: %v", workerID, envelope.ID, err)
		return
	}

	// Process the envelope
	processErr := p.executeProcessing(envelope)

	// Update status based on result
	if processErr != nil {
		// Check if we should retry
		if envelope.RetryCount < envelope.MaxRetries {
			// Mark for retry
			retryErr := p.queue.MarkForRetry(p.workerCtx, envelope.ID)
			if retryErr != nil {
				log.Printf("Worker %d: failed to mark envelope %s for retry: %v", workerID, envelope.ID, retryErr)
			}
			log.Printf("Worker %d: envelope %s scheduled for retry (%d/%d)", workerID, envelope.ID, envelope.RetryCount+1, envelope.MaxRetries)
		} else {
			// Mark as failed
			err = p.queue.UpdateStatus(p.workerCtx, envelope.ID, models.EnvelopeStatusFailed, processErr.Error())
			if err != nil {
				log.Printf("Worker %d: failed to update status for failed envelope %s: %v", workerID, envelope.ID, err)
			}
			p.stats.mu.Lock()
			p.stats.FailedCount++
			p.stats.mu.Unlock()
			log.Printf("Worker %d: envelope %s marked as failed after %d retries: %v", workerID, envelope.ID, envelope.MaxRetries, processErr)
		}
	} else {
		// Mark as completed
		err = p.queue.UpdateStatus(p.workerCtx, envelope.ID, models.EnvelopeStatusCompleted, "")
		if err != nil {
			log.Printf("Worker %d: failed to update status for completed envelope %s: %v", workerID, envelope.ID, err)
		}
		p.stats.mu.Lock()
		p.stats.ProcessedCount++
		p.stats.LastProcessedAt = time.Now()
		p.stats.mu.Unlock()
		log.Printf("Worker %d: envelope %s processed successfully in %v", workerID, envelope.ID, time.Since(startTime))
	}

	// Update average processing time
	processingTime := time.Since(startTime)
	p.stats.mu.Lock()
	totalOps := p.stats.ProcessedCount + p.stats.FailedCount
	if totalOps > 0 {
		// Running average
		currentAvg := int64(p.stats.AvgProcessingTime)
		newAvg := (currentAvg*(int64(totalOps)-1) + int64(processingTime)) / int64(totalOps)
		p.stats.AvgProcessingTime = time.Duration(newAvg)
	}
	p.stats.mu.Unlock()
}

// executeProcessing performs the actual envelope processing
func (p *EnvelopeProcessor) executeProcessing(envelope *models.EnvelopeQueueItem) error {
	ctx := context.Background()

	// Get account with credentials
	account, err := p.accountService.GetAccountWithCredentials(ctx, envelope.AccountID)
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

	client, release, err := p.sessions.Acquire(ctx, envelope.AccountID, imapConfig)
	if err != nil {
		return fmt.Errorf("failed to acquire IMAP session: %w", err)
	}
	defer release()

	// Fetch full message with body based on per-account sync settings
	includeBody := account.SyncSettings.FetchBody
	envelopes, err := client.FetchMessages([]uint32{envelope.UID}, includeBody)
	if err != nil {
		return fmt.Errorf("failed to fetch message: %w", err)
	}

	if len(envelopes) == 0 {
		return fmt.Errorf("message not found")
	}

	fetchedEnvelope := envelopes[0]

	// Process the full message
	// This would integrate with message service to:
	// 1. Store message in cache
	// 2. Extract links if enabled
	// 3. Process attachments if enabled
	// 4. Update search index
	// 5. Trigger webhooks for new messages

	err = p.storeAndProcessMessage(ctx, envelope, account, &fetchedEnvelope, includeBody)
	if err != nil {
		return fmt.Errorf("failed to store and process message: %w", err)
	}

	return nil
}

// storeAndProcessMessage stores the message and performs post-processing
func (p *EnvelopeProcessor) storeAndProcessMessage(
	ctx context.Context,
	envelope *models.EnvelopeQueueItem,
	account *models.Account,
	fetchedEnvelope *pool.MessageEnvelope,
	includeBody bool,
) error {
	// Convert envelope to full message model
	message := p.convertToMessage(envelope, fetchedEnvelope, account.ID)

	// TODO: Integrate with message service cache
	// For now, log the processing
	log.Printf("Processing message %s from %s (body: %v)", message.MessageID, message.From.Address, includeBody)

	// Extract links if enabled in account sync settings
	if account.SyncSettings.EnableLinkExtraction && includeBody {
		// links := extractLinks(message.Body)
		// message.Links = links
		log.Printf("Link extraction enabled for message %s", message.MessageID)
	}

	// Process attachments if enabled in account sync settings
	if account.SyncSettings.EnableAttachmentProcessing && includeBody {
		// attachments := processAttachments(message.Attachments)
		// message.Attachments = attachments
		log.Printf("Attachment processing enabled for message %s", message.MessageID)
	}

	// Update processing metadata
	message.ProcessingMetadata = &models.ProcessingMetadata{
		CacheStatus:    "processed",
		ProcessingTime: time.Since(envelope.EnqueuedAt).Milliseconds(),
		SizeOriginal:   message.Size,
		SizeProcessed:  message.Size,
		ProcessedAt:    time.Now(),
	}

	// TODO: Store in message cache
	// err := p.messageService.StoreMessage(ctx, message)
	// if err != nil {
	//     return err
	// }

	// TODO: Trigger webhook for new message
	// p.triggerWebhook(ctx, account, message)

	return nil
}

// convertToMessage converts envelope and fetched data to full message model
func (p *EnvelopeProcessor) convertToMessage(
	envelope *models.EnvelopeQueueItem,
	fetched *pool.MessageEnvelope,
	accountID string,
) *models.Message {
	// Generate thread ID from message ID or references
	threadID := generateThreadID(fetched.MessageID, []string{})

	from := models.Contact{}
	if len(fetched.From) > 0 {
		from = fetched.From[0]
	}

	return &models.Message{
		UID:         fmt.Sprintf("%d", fetched.UID),
		MessageID:   fetched.MessageID,
		Folder:      envelope.FolderName,
		Subject:     fetched.Subject,
		From:        from,
		To:          convertContacts(fetched.To),
		Date:        fetched.Date,
		Flags:       convertFlags(fetched.Flags),
		Size:        fetched.Size,
		ThreadID:    threadID,
		ContentType: models.ContentTypeTextPlain, // Default, would be determined from body
	}
}

// cleanupLoop periodically cleans up old processed envelopes
func (p *EnvelopeProcessor) cleanupLoop() {
	ticker := time.NewTicker(p.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.workerCtx.Done():
			return
		case <-ticker.C:
			removed, err := p.queue.CleanupOld(p.workerCtx, p.config.CleanupAge)
			if err != nil {
				log.Printf("Cleanup failed: %v", err)
			} else if removed > 0 {
				log.Printf("Cleaned up %d old envelopes", removed)
			}
		}
	}
}

// GetStats returns processor statistics
func (p *EnvelopeProcessor) GetStats() *ProcessorStats {
	// Get current queue size first (outside lock)
	ctx := context.Background()
	var queueSize int64
	stats, err := p.queue.GetStats(ctx, "")
	if err == nil && stats != nil {
		queueSize = stats.Pending
	}

	p.stats.mu.RLock()
	defer p.stats.mu.RUnlock()

	// Return a copy with updated queue size
	statsCopy := p.stats
	statsCopy.CurrentQueueSize = queueSize
	return &statsCopy
}

// Helper functions

func generateThreadID(messageID string, references []string) string {
	// Simple thread ID generation based on message ID or references
	if len(references) > 0 {
		return references[0]
	}
	if messageID != "" {
		return messageID
	}
	// Generate a new thread ID
	return fmt.Sprintf("thread_%d", time.Now().UnixNano())
}

func convertContacts(contacts []models.Contact) []models.Contact {
	result := make([]models.Contact, len(contacts))
	copy(result, contacts)
	return result
}

func convertFlags(flags []string) []models.MessageFlag {
	result := make([]models.MessageFlag, len(flags))
	for i, flag := range flags {
		result[i] = models.MessageFlag(flag)
	}
	return result
}

// EnvelopeProcessingPipeline represents the complete envelope processing setup
type EnvelopeProcessingPipeline struct {
	Queue       envelopequeue.EnvelopeQueue
	Processor   *EnvelopeProcessor
	SyncManager *SyncManager
}

// EnvelopeProcessingPipelineConfig holds configuration for the complete pipeline
type EnvelopeProcessingPipelineConfig struct {
	QueueType       string `json:"queue_type"` // memory, machinery
	QueueConfig     *envelopequeue.MachineryQueueConfig
	ProcessorConfig *EnvelopeProcessorConfig
}

// DefaultPipelineConfig returns default pipeline configuration
func DefaultPipelineConfig() *EnvelopeProcessingPipelineConfig {
	return &EnvelopeProcessingPipelineConfig{
		QueueType:       "memory",
		QueueConfig:     nil,
		ProcessorConfig: DefaultEnvelopeProcessorConfig(),
	}
}

// NewEnvelopeProcessingPipeline creates a complete envelope processing pipeline
// This wires together the queue, processor, and sync manager
func NewEnvelopeProcessingPipeline(
	messageService *MessageService,
	accountService *AccountService,
	sessions *pool.IMAPSessionPool,
	config *EnvelopeProcessingPipelineConfig,
) (*EnvelopeProcessingPipeline, error) {
	if config == nil {
		config = DefaultPipelineConfig()
	}

	// Create envelope queue
	var queue envelopequeue.EnvelopeQueue
	var err error

	switch config.QueueType {
	case "machinery":
		queue, err = envelopequeue.NewMachineryEnvelopeQueue(config.QueueConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create machinery queue: %w", err)
		}
	case "memory", "":
		queue = envelopequeue.NewMemoryEnvelopeQueue(envelopequeue.DefaultMemoryEnvelopeQueueConfig())
	default:
		return nil, fmt.Errorf("unknown queue type: %s", config.QueueType)
	}

	// Create envelope processor
	processor, err := NewEnvelopeProcessor(
		queue,
		messageService,
		accountService,
		sessions,
		config.ProcessorConfig,
	)
	if err != nil {
		queue.Close()
		return nil, fmt.Errorf("failed to create envelope processor: %w", err)
	}

	// Create sync manager with queue integration
	syncMgr := NewSyncManager(messageService, accountService, sessions, queue)

	return &EnvelopeProcessingPipeline{
		Queue:       queue,
		Processor:   processor,
		SyncManager: syncMgr,
	}, nil
}

// Start begins the envelope processing pipeline
func (p *EnvelopeProcessingPipeline) Start() error {
	// Start the processor workers
	if err := p.Processor.Start(); err != nil {
		return fmt.Errorf("failed to start processor: %w", err)
	}
	return nil
}

// Stop gracefully shuts down the envelope processing pipeline
func (p *EnvelopeProcessingPipeline) Stop() error {
	var errs []error

	// Stop processor
	if err := p.Processor.Stop(); err != nil {
		errs = append(errs, fmt.Errorf("failed to stop processor: %w", err))
	}

	// Close queue
	if err := p.Queue.Close(); err != nil {
		errs = append(errs, fmt.Errorf("failed to close queue: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors during shutdown: %v", errs)
	}
	return nil
}

// GetPipelineStats returns statistics for the entire pipeline
func (p *EnvelopeProcessingPipeline) GetPipelineStats() (*PipelineStats, error) {
	processorStats := p.Processor.GetStats()

	queueStats, err := p.Queue.GetStats(context.Background(), "")
	if err != nil {
		return nil, err
	}

	return &PipelineStats{
		ProcessorStats: *processorStats,
		QueueStats:     *queueStats,
	}, nil
}

// PipelineStats holds statistics for the entire processing pipeline
type PipelineStats struct {
	ProcessorStats ProcessorStats                   `json:"processor_stats"`
	QueueStats     envelopequeue.EnvelopeQueueStats `json:"queue_stats"`
}
