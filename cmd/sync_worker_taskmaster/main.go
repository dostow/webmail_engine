package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"webmail_engine/internal/config"
	"webmail_engine/internal/envelopequeue"
	"webmail_engine/internal/pool"
	"webmail_engine/internal/service"
	"webmail_engine/internal/store"
	"webmail_engine/internal/taskmaster"
	"webmail_engine/internal/workerconfig"

	"webmail_engine/internal/workers"
)

// SyncWorkerWithTaskmaster is a sync worker that uses taskmaster for task scheduling.
// It loads accounts and creates scheduled sync tasks for each account.
type SyncWorkerWithTaskmaster struct {
	Config         *workerconfig.WorkerConfig
	Store          store.AccountStore
	Dispatcher     taskmaster.FullDispatcher
	SyncService    *service.SyncService
	SessionPool    *pool.IMAPSessionPool
	Queue          envelopequeue.EnvelopeQueue
	AccountService *service.AccountService
}

// NewSyncWorkerWithTaskmaster creates a new sync worker using taskmaster.
func NewSyncWorkerWithTaskmaster(cfg *workerconfig.WorkerConfig) (*SyncWorkerWithTaskmaster, error) {
	// Initialize store
	accountStore, err := createStore(cfg)
	if err != nil {
		return nil, err
	}

	// Initialize envelope queue
	queue, err := createQueue(cfg)
	if err != nil {
		accountStore.Close()
		return nil, err
	}

	// Initialize IMAP session pool
	sessionPool := pool.NewIMAPSessionPool(pool.DefaultSessionPoolConfig(), nil)

	// Create account service
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
		accountStore.Close()
		queue.Close()
		return nil, err
	}

	// Set account service in session pool
	sessionPool.SetAccountService(accountService)

	// Create sync service
	syncService := service.NewSyncService(accountService, sessionPool, queue)

	// Create dispatcher in managed mode (for local task execution)
	dispatcher := taskmaster.NewDispatcher(
		taskmaster.WithMode(taskmaster.ManagedMode),
		taskmaster.WithWorkerCount(2),             // Sync tasks are I/O bound, few workers needed
		taskmaster.WithTaskTimeout(5*time.Minute), // Sync can take time
		taskmaster.WithQueueSize(100),
	)

	// Register the sync task with the sync service
	syncTask := &workers.SyncTask{
		SyncService: syncService,
	}
	if err := dispatcher.Register(syncTask); err != nil {
		accountStore.Close()
		queue.Close()
		return nil, err
	}

	return &SyncWorkerWithTaskmaster{
		Config:         cfg,
		Store:          accountStore,
		Dispatcher:     dispatcher,
		SyncService:    syncService,
		SessionPool:    sessionPool,
		Queue:          queue,
		AccountService: accountService,
	}, nil
}

// Start loads accounts and schedules sync tasks.
func (w *SyncWorkerWithTaskmaster) Start() error {
	log.Printf("Starting sync worker with taskmaster (ID: %s)", w.Config.WorkerID)

	ctx := context.Background()

	// Start the dispatcher
	if err := w.Dispatcher.Start(ctx); err != nil {
		return err
	}

	// Load all active accounts
	accounts, _, err := w.Store.List(ctx, 0, 0)
	if err != nil {
		log.Printf("Warning: Failed to load accounts: %v", err)
		return nil
	}

	log.Printf("Loaded %d accounts", len(accounts))

	// Create scheduled sync tasks for each active account
	scheduledCount := 0
	for _, acc := range accounts {
		if acc.Status != "active" || !acc.SyncSettings.AutoSync {
			continue
		}

		interval := time.Duration(acc.SyncSettings.SyncInterval) * time.Second
		if interval < 60*time.Second {
			interval = 60 * time.Second // Minimum 1 minute
		}

		// Create sync task payload
		payload := workers.SyncPayload{
			AccountID: acc.ID,
			Options: service.SyncOptions{
				HistoricalScope:            30,
				IncludeSpam:                acc.SyncSettings.IncludeSpam,
				IncludeTrash:               acc.SyncSettings.IncludeTrash,
				FetchBody:                  acc.SyncSettings.FetchBody,
				EnableLinkExtraction:       acc.SyncSettings.EnableLinkExtraction,
				EnableAttachmentProcessing: acc.SyncSettings.EnableAttachmentProcessing,
			},
		}

		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			log.Printf("Failed to marshal sync payload for account %s: %v", acc.ID, err)
			continue
		}

		// Schedule recurring sync task
		scheduleName := "sync_" + acc.ID
		scheduleID, err := w.Dispatcher.ScheduleTask(ctx, "sync", payloadBytes, interval, &taskmaster.ScheduleTaskOptions{
			Name: scheduleName,
		})
		if err != nil {
			log.Printf("Failed to schedule sync for account %s: %v", acc.ID, err)
			continue
		}

		scheduledCount++
		log.Printf("Scheduled sync for account %s (interval: %v, schedule_id: %s)", acc.ID, interval, scheduleID)
	}

	log.Printf("Sync worker started with %d scheduled accounts", scheduledCount)
	return nil
}

// Run blocks until shutdown signal.
func (w *SyncWorkerWithTaskmaster) Run() error {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	return w.Stop()
}

// Stop gracefully shuts down the worker.
func (w *SyncWorkerWithTaskmaster) Stop() error {
	log.Println("Shutting down sync worker with taskmaster...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stop dispatcher (which also stops the scheduler)
	if err := w.Dispatcher.Stop(ctx); err != nil {
		log.Printf("Error stopping dispatcher: %v", err)
	}

	// Close resources
	if w.Queue != nil {
		w.Queue.Close()
	}
	if w.Store != nil {
		w.Store.Close()
	}

	log.Println("Sync worker with taskmaster stopped")
	return nil
}

// Helper functions

func createQueue(cfg *workerconfig.WorkerConfig) (envelopequeue.EnvelopeQueue, error) {
	switch cfg.Queue.Type {
	case "memory":
		return envelopequeue.NewMemoryEnvelopeQueue(envelopequeue.DefaultMemoryEnvelopeQueueConfig()), nil
	case "redis":
		config := cfg.ToEnvelopeQueueConfig()
		return envelopequeue.NewMachineryEnvelopeQueue(config)
	default:
		return envelopequeue.NewMemoryEnvelopeQueue(envelopequeue.DefaultMemoryEnvelopeQueueConfig()), nil
	}
}

func createStore(cfg *workerconfig.WorkerConfig) (store.AccountStore, error) {
	var accountStore store.AccountStore
	var err error

	switch cfg.Store.Type {
	case "sql":
		if cfg.Store.SQL == nil {
			cfg.Store.SQL = &config.SQLConfig{Driver: "sqlite", DSN: "./data/webmail.db", MaxConnections: 10}
		}
		accountStore, err = store.NewSQLStore(*cfg.Store.SQL)
	case "memory":
		accountStore = store.NewMemoryStore()
	default:
		return nil, err
	}

	return accountStore, err
}

func main() {
	// Parse command line flags
	configPath := flag.String("config", "", "Path to configuration file")
	storeType := flag.String("store", "sql", "Store type: sql, memory")
	storeDriver := flag.String("store-driver", "sqlite", "SQL driver: sqlite, postgres")
	storeDSN := flag.String("store-dsn", "./data/webmail.db", "SQL DSN")
	flag.Parse()

	// Load or create configuration
	var cfg *workerconfig.WorkerConfig
	var err error

	if *configPath != "" {
		cfg, err = workerconfig.LoadWorkerConfig(*configPath)
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}
	} else {
		cfg = workerconfig.DefaultWorkerConfig("sync")
		cfg.Store.Type = *storeType
		cfg.Store.SQL = &config.SQLConfig{
			Driver: *storeDriver,
			DSN:    *storeDSN,
		}
		config.ExpandEnvVars(cfg)
	}

	// Create and run sync worker with taskmaster
	syncWorker, err := NewSyncWorkerWithTaskmaster(cfg)
	if err != nil {
		log.Fatalf("Failed to create sync worker: %v", err)
	}

	if err := syncWorker.Start(); err != nil {
		log.Fatalf("Failed to start sync worker: %v", err)
	}

	if err := syncWorker.Run(); err != nil {
		log.Fatalf("Sync worker error: %v", err)
	}
}
