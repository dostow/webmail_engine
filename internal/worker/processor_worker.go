package worker

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"webmail_engine/internal/cache"
	"webmail_engine/internal/envelopequeue"
	"webmail_engine/internal/pool"
	"webmail_engine/internal/scheduler"
	"webmail_engine/internal/service"
	"webmail_engine/internal/storage"
	"webmail_engine/internal/store"
	"webmail_engine/internal/workerconfig"
)

// ProcessorWorker represents a running processor worker
type ProcessorWorker struct {
	Config            *workerconfig.WorkerConfig
	Queue             envelopequeue.EnvelopeQueue
	Store             store.AccountStore
	SessionPool       *pool.IMAPSessionPool
	Cache             *cache.Cache
	Scheduler         *scheduler.FairUseScheduler
	AttachmentStorage storage.AttachmentStorage
	Processor         *service.EnvelopeProcessor
	ownsResources     bool // Whether worker owns its resources (true = cleanup on Stop)
}

// ProcessorWorkerOptions holds optional configuration for processor worker
type ProcessorWorkerOptions struct {
	Queue             envelopequeue.EnvelopeQueue
	Store             store.AccountStore
	SessionPool       *pool.IMAPSessionPool
	Cache             *cache.Cache
	AttachmentStorage storage.AttachmentStorage
}

// NewProcessorWorker creates a new processor worker instance
func NewProcessorWorker(cfg *workerconfig.WorkerConfig, opts *ProcessorWorkerOptions) (*ProcessorWorker, error) {
	if opts == nil {
		opts = &ProcessorWorkerOptions{}
	}

	// Determine if we own resources (true if no options provided for that resource)
	ownsQueue := opts.Queue == nil
	ownsStore := opts.Store == nil
	ownsCache := opts.Cache == nil
	ownsAttachmentStorage := opts.AttachmentStorage == nil

	// Initialize queue
	var queue envelopequeue.EnvelopeQueue
	if opts.Queue != nil {
		queue = opts.Queue
	} else {
		var err error
		queue, err = createQueue(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create queue: %w", err)
		}
	}

	// Initialize store
	var accountStore store.AccountStore
	if opts.Store != nil {
		accountStore = opts.Store
	} else {
		var err error
		accountStore, err = createStore(cfg)
		if err != nil {
			queue.Close()
			return nil, fmt.Errorf("failed to create store: %w", err)
		}
	}

	// Initialize IMAP session pool
	var sessionPool *pool.IMAPSessionPool
	if opts.SessionPool != nil {
		sessionPool = opts.SessionPool
	} else {
		// Create with nil account service initially
		sessionPool = pool.NewIMAPSessionPool(pool.DefaultSessionPoolConfig(), nil)
	}

	// Initialize cache
	var memCache *cache.Cache
	if opts.Cache != nil {
		memCache = opts.Cache
	} else {
		memCache = cache.NewCache(nil) // nil = memory-only
	}

	// Initialize fair-use scheduler
	fairUseScheduler := scheduler.NewFairUseScheduler()

	// Initialize attachment storage
	var attachmentStorage storage.AttachmentStorage
	if opts.AttachmentStorage != nil {
		attachmentStorage = opts.AttachmentStorage
	} else {
		attachmentStorage = storage.NewFileAttachmentStorage(cfg.ProcessorConfig.TempStoragePath)
	}

	// Create account service with encryption key
	accountService, err := service.NewAccountService(
		accountStore,
		nil, // connPool - not needed for processor worker
		sessionPool,
		memCache,
		fairUseScheduler,
		nil, // syncMgr - not needed for processor worker
		service.AccountServiceConfig{
			EncryptionKey: cfg.Security.EncryptionKey,
		},
	)
	if err != nil {
		cleanupResources(queue, accountStore, sessionPool, memCache, attachmentStorage)
		return nil, fmt.Errorf("failed to create account service: %w", err)
	}

	// Set the account service in the session pool (for auth failure handling)
	if opts.SessionPool == nil {
		sessionPool.SetAccountService(accountService)
	}

	// Create message service
	messageService, err := service.NewMessageService(
		sessionPool,
		memCache,
		fairUseScheduler,
		accountService,
		service.MessageServiceConfig{
			TempStoragePath: cfg.ProcessorConfig.TempStoragePath,
			MaxInlineSize:   10 * 1024 * 1024, // 10MB
		},
	)
	if err != nil {
		cleanupResources(queue, accountStore, sessionPool, memCache, attachmentStorage)
		return nil, fmt.Errorf("failed to create message service: %w", err)
	}

	// Create envelope processor
	processor, err := service.NewEnvelopeProcessor(
		queue,
		messageService,
		accountService,
		sessionPool,
		cfg.ProcessorConfig,
	)
	if err != nil {
		cleanupResources(queue, accountStore, sessionPool, memCache, attachmentStorage)
		return nil, fmt.Errorf("failed to create processor: %w", err)
	}

	return &ProcessorWorker{
		Config:            cfg,
		Queue:             queue,
		Store:             accountStore,
		SessionPool:       sessionPool,
		Cache:             memCache,
		Scheduler:         fairUseScheduler,
		AttachmentStorage: attachmentStorage,
		Processor:         processor,
		ownsResources:     ownsQueue && ownsStore && ownsCache && ownsAttachmentStorage,
	}, nil
}

// Start starts the processor worker
func (w *ProcessorWorker) Start() error {
	log.Printf("Starting processor worker (ID: %s, Queue: %s, Concurrency: %d)",
		w.Config.WorkerID, w.Config.Queue.Type, w.Config.ProcessorConfig.Concurrency)

	if err := w.Processor.Start(); err != nil {
		return fmt.Errorf("failed to start processor: %w", err)
	}

	log.Println("Processor worker is running. Press Ctrl+C to stop.")
	return nil
}

// Run starts the worker and blocks until shutdown signal
func (w *ProcessorWorker) Run() error {
	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	<-sigChan

	return w.Stop()
}

// Stop gracefully stops the processor worker
func (w *ProcessorWorker) Stop() error {
	log.Println("Shutting down processor worker...")

	w.Processor.Stop()

	// Only cleanup resources that we own (not shared)
	// If resources were passed via options, they should be cleaned up by the caller
	if w.ownsResources {
		cleanupResources(
			w.Queue,
			w.Store,
			w.SessionPool,
			w.Cache,
			w.AttachmentStorage,
		)
	}

	log.Println("Processor worker stopped")
	return nil
}

// GetStats returns processor worker statistics
func (w *ProcessorWorker) GetStats() *service.ProcessorStats {
	return w.Processor.GetStats()
}

// cleanupResources closes all worker resources
func cleanupResources(
	queue envelopequeue.EnvelopeQueue,
	store store.AccountStore,
	sessionPool *pool.IMAPSessionPool,
	cache *cache.Cache,
	attachmentStorage storage.AttachmentStorage,
) {
	if queue != nil {
		queue.Close()
	}
	if store != nil {
		store.Close()
	}
	if sessionPool != nil {
		// IMAPSessionPool doesn't have Close method
	}
	if cache != nil {
		cache.Close()
	}
	if attachmentStorage != nil {
		attachmentStorage.Shutdown()
	}
}
