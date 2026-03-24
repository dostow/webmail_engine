package worker

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"webmail_engine/internal/envelopequeue"
	"webmail_engine/internal/pool"
	"webmail_engine/internal/service"
	"webmail_engine/internal/store"
	"webmail_engine/internal/workerconfig"
)

// SyncWorker represents a running sync worker
type SyncWorker struct {
	Config      *workerconfig.WorkerConfig
	Queue       envelopequeue.EnvelopeQueue
	Store       store.AccountStore
	SessionPool *pool.IMAPSessionPool
	SyncManager *service.SyncManager
}

// SyncWorkerOptions holds optional configuration for sync worker
type SyncWorkerOptions struct {
	Queue       envelopequeue.EnvelopeQueue
	Store       store.AccountStore
	SessionPool *pool.IMAPSessionPool
}

// NewSyncWorker creates a new sync worker instance
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

	// Create sync manager
	syncMgr := service.NewSyncManagerForWorker(nil, accountService, sessionPool, queue)

	return &SyncWorker{
		Config:      cfg,
		Queue:       queue,
		Store:       accountStore,
		SessionPool: sessionPool,
		SyncManager: syncMgr,
	}, nil
}

// Start starts the sync worker and loads accounts
func (w *SyncWorker) Start() error {
	log.Printf("Starting sync worker (ID: %s, Queue: %s)", w.Config.WorkerID, w.Config.Queue.Type)

	// Load and start sync for all active accounts
	ctx := context.Background()
	accounts, _, err := w.Store.List(ctx, 0, 0)
	if err != nil {
		log.Printf("Warning: Failed to load accounts: %v", err)
	} else {
		log.Printf("Loaded %d accounts", len(accounts))

		for _, acc := range accounts {
			if acc.Status == "active" && acc.SyncSettings.AutoSync {
				interval := time.Duration(acc.SyncSettings.SyncInterval) * time.Second
				if interval < 60*time.Second {
					interval = 60 * time.Second
				}

				if err := w.SyncManager.StartSync(acc.ID, interval); err != nil {
					log.Printf("Failed to start sync for account %s: %v", acc.ID, err)
				} else {
					log.Printf("Started sync for account %s (interval: %v)", acc.ID, interval)
				}
			}
		}
	}

	log.Println("Sync worker is running. Press Ctrl+C to stop.")
	return nil
}

// Run starts the worker and blocks until shutdown signal
func (w *SyncWorker) Run() error {
	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	<-sigChan

	return w.Stop()
}

// Stop gracefully stops the sync worker
func (w *SyncWorker) Stop() error {
	log.Println("Shutting down sync worker...")

	ctx, cancel := context.WithTimeout(context.Background(), w.Config.ShutdownTimeout)
	defer cancel()

	_ = ctx
	w.SyncManager.StopAll()

	if w.Queue != nil {
		w.Queue.Close()
	}

	log.Println("Sync worker stopped")
	return nil
}
