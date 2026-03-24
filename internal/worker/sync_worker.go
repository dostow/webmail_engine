package worker

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"webmail_engine/internal/envelopequeue"
	"webmail_engine/internal/pool"
	"webmail_engine/internal/service"
	"webmail_engine/internal/store"
	"webmail_engine/internal/workerconfig"
)

// SyncWorker represents a running sync worker using SyncService.
// This worker loads accounts and performs immediate synchronization.
// For scheduled sync, use sync_worker_taskmaster instead.
type SyncWorker struct {
	Config      *workerconfig.WorkerConfig
	Queue       envelopequeue.EnvelopeQueue
	Store       store.AccountStore
	SessionPool *pool.IMAPSessionPool
	SyncService *service.SyncService
}

// SyncWorkerOptions holds optional configuration for sync worker
type SyncWorkerOptions struct {
	Queue       envelopequeue.EnvelopeQueue
	Store       store.AccountStore
	SessionPool *pool.IMAPSessionPool
}

// NewSyncWorker creates a new sync worker instance using SyncService.
func NewSyncWorker(cfg *workerconfig.WorkerConfig, opts *SyncWorkerOptions) (*SyncWorker, error) {
	if opts == nil {
		opts = &SyncWorkerOptions{}
	}

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

	// Create account service with encryption key
	accountService, err := service.NewAccountService(
		accountStore,
		nil, // connPool - not needed for sync worker
		sessionPool,
		nil, // cache - not needed for sync worker
		nil, // scheduler - not needed for sync worker
		service.AccountServiceConfig{
			EncryptionKey: cfg.Security.EncryptionKey,
		},
	)
	if err != nil {
		queue.Close()
		return nil, fmt.Errorf("failed to create account service: %w", err)
	}

	// Set the account service in the session pool (for auth failure handling)
	if opts.SessionPool == nil {
		sessionPool.SetAccountService(accountService)
	}

	// Create sync service
	syncService := service.NewSyncService(accountService, sessionPool, queue)

	return &SyncWorker{
		Config:      cfg,
		Queue:       queue,
		Store:       accountStore,
		SessionPool: sessionPool,
		SyncService: syncService,
	}, nil
}

// Start starts the sync worker and performs one-time sync for all active accounts.
func (w *SyncWorker) Start() error {
	log.Printf("Starting sync worker (ID: %s, Queue: %s)", w.Config.WorkerID, w.Config.Queue.Type)

	// Load and sync all active accounts
	ctx := context.Background()
	accounts, _, err := w.Store.List(ctx, 0, 0)
	if err != nil {
		log.Printf("Warning: Failed to load accounts: %v", err)
	} else {
		log.Printf("Loaded %d accounts", len(accounts))

		syncCount := 0
		for _, acc := range accounts {
			if acc.Status != "active" || !acc.SyncSettings.AutoSync {
				continue
			}

			// Perform one-time sync for this account
			log.Printf("Starting sync for account %s...", acc.ID)

			syncOpts := service.SyncOptions{
				HistoricalScope:            30,
				IncludeSpam:                acc.SyncSettings.IncludeSpam,
				IncludeTrash:               acc.SyncSettings.IncludeTrash,
				FetchBody:                  acc.SyncSettings.FetchBody,
				EnableLinkExtraction:       acc.SyncSettings.EnableLinkExtraction,
				EnableAttachmentProcessing: acc.SyncSettings.EnableAttachmentProcessing,
			}

			result, err := w.SyncService.SyncAccount(ctx, acc.ID, syncOpts)
			if err != nil {
				log.Printf("Sync failed for account %s: %v", acc.ID, err)
			} else {
				syncCount++
				log.Printf("Sync completed for account %s: %d messages, %d folders, %d envelopes enqueued",
					acc.ID, result.MessagesSynced, result.FoldersSynced, result.EnvelopesEnqueued)
			}
		}

		log.Printf("Sync completed for %d/%d accounts", syncCount, len(accounts))
	}

	log.Println("Sync worker is running. Press Ctrl+C to stop.")
	return nil
}

// Run starts the worker and blocks until shutdown signal.
// Note: This worker performs one-time sync on Start().
// For continuous scheduled sync, use sync_worker_taskmaster instead.
func (w *SyncWorker) Run() error {
	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	<-sigChan

	return w.Stop()
}

// Stop gracefully stops the sync worker.
func (w *SyncWorker) Stop() error {
	log.Println("Shutting down sync worker...")

	if w.Queue != nil {
		w.Queue.Close()
	}

	log.Println("Sync worker stopped")
	return nil
}
