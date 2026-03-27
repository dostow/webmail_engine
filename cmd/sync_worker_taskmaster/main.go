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
	"webmail_engine/internal/logger"
	"webmail_engine/internal/pool"
	"webmail_engine/internal/service"
	"webmail_engine/internal/store"
	"webmail_engine/internal/taskmaster"
	"webmail_engine/internal/workerconfig"

	"webmail_engine/internal/workers"
)

// SyncWorkerWithTaskmaster is a sync worker that uses taskmaster for task scheduling.
// It supports three operational modes:
// - scheduled_managed: Schedules sync tasks for all accounts (default)
// - rest: Exposes HTTP endpoints for task submission (no account fetching)
// - machinery: Listens to message queue for tasks (no account fetching)
type SyncWorkerWithTaskmaster struct {
	Config         *workerconfig.WorkerConfig
	Store          store.AccountStore
	Dispatcher     taskmaster.FullDispatcher
	SyncService    *service.SyncService
	SessionPool    *pool.IMAPSessionPool
	Queue          envelopequeue.EnvelopeQueue
	AccountService *service.AccountService
	Mode           string // scheduled_managed, rest, or machinery
}

// NewSyncWorkerWithTaskmaster creates a new sync worker using taskmaster.
func NewSyncWorkerWithTaskmaster(cfg *workerconfig.WorkerConfig, mode string) (*SyncWorkerWithTaskmaster, error) {
	// Initialize store
	accountStore, err := createStore(cfg)
	if err != nil {
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
		return nil, err
	}

	// Set account service in session pool
	sessionPool.SetAccountService(accountService)

	// Create main dispatcher for sync task intake/execution
	log.Printf("Initializing taskmaster dispatcher (dispatch=%s, execution=%s)...", cfg.Dispatch.Type, cfg.Execution.Mode)
	dispatcher := createDispatcher(cfg)

	// Check if webhook is configured for queue pipeline
	webhookConfigured := cfg.Webhook.URL != ""

	// Create sub-task dispatcher for envelope processing
	// Reuse main dispatcher if sub-task config matches dispatch config
	var subTaskDispatcher taskmaster.TaskDispatcher
	if shouldReuseDispatcher(cfg) {
		log.Println("Sub-task dispatch: reusing main dispatcher (same config)")
		subTaskDispatcher = dispatcher

		// When reusing main dispatcher with machinery, register sub-task handlers for dispatch
		if cfg.Dispatch.Type == "machinery" {
			if err := dispatcher.Register(&workers.EnvelopeProcessorTask{}); err != nil {
				log.Printf("Warning: Failed to register envelope_processor: %v", err)
			}
			if webhookConfigured {
				webhookNotifier := &workers.WebhookNotifierTask{
					WebhookURL: cfg.Webhook.URL,
					SecretKey:  cfg.Webhook.SecretKey,
				}
				if err := dispatcher.Register(webhookNotifier); err != nil {
					log.Printf("Warning: Failed to register webhook_notifier: %v", err)
				}
			}
			log.Println("Sub-task handlers registered for dispatch")
		}
	} else {
		log.Printf("Initializing sub-task dispatcher (type=%s)...", cfg.SubTaskDispatch.Type)
		subTaskDispatcher, _ = createSubTaskDispatcher(cfg, webhookConfigured)
	}

	// Create envelope queue that uses sub-task dispatcher
	queue := createQueue(subTaskDispatcher, webhookConfigured)

	// Create sync service
	syncService := service.NewSyncService(accountService, sessionPool, queue)

	// Register the sync task with the sync service
	syncTask := &workers.SyncTask{
		SyncService: syncService,
	}
	if err := dispatcher.Register(syncTask); err != nil {
		accountStore.Close()
		queue.Close()
		return nil, err
	}

	// Register webhook notifier task (optional - for sending notifications)
	webhookNotifier := &workers.WebhookNotifierTask{
		WebhookURL: cfg.Webhook.URL,
		SecretKey:  cfg.Webhook.SecretKey,
	}
	if err := dispatcher.Register(webhookNotifier); err != nil {
		log.Printf("Warning: Failed to register webhook notifier: %v", err)
		// Continue without webhook support
	}

	return &SyncWorkerWithTaskmaster{
		Config:         cfg,
		Store:          accountStore,
		Dispatcher:     dispatcher,
		SyncService:    syncService,
		SessionPool:    sessionPool,
		Queue:          queue,
		AccountService: accountService,
		Mode:           mode,
	}, nil
}

// createDispatcher creates a taskmaster dispatcher with mode-specific configuration.
// Dispatch mode is determined by cfg.Dispatch.Type (managed, rest, machinery).
// Execution config controls worker pool settings for managed mode.
func createDispatcher(cfg *workerconfig.WorkerConfig) taskmaster.FullDispatcher {
	// Get dispatch-specific configuration
	addr, redisURL, queueName := cfg.GetDispatchConfig()

	switch cfg.Dispatch.Type {
	case "rest":
		return taskmaster.NewDispatcher(
			taskmaster.WithMode(taskmaster.RESTMode),
			taskmaster.WithRESTConfig(&taskmaster.RESTConfig{
				Addr:                addr,
				BasePath:            "/api/v1/tasks",
				EnableSyncExecution: true,
			}),
			taskmaster.WithLogger(logger.NewStandardLogger()),
		)
	case "machinery":
		return taskmaster.NewDispatcher(
			taskmaster.WithMode(taskmaster.MachineryMode),
			taskmaster.WithMachineryConfig(&taskmaster.MachineryConfig{
				BrokerURL:         redisURL,
				ResultBackend:     cfg.Execution.ResultBackend,
				DefaultQueue:      queueName,
				DefaultRetryCount: 3,
			}),
			taskmaster.WithLogger(logger.NewStandardLogger()),
		)
	default: // managed
		workerCount := cfg.Execution.WorkerCount
		if workerCount <= 0 {
			workerCount = 2
		}
		taskTimeout := cfg.Execution.TaskTimeout
		if taskTimeout <= 0 {
			taskTimeout = 5 * time.Minute
		}
		queueSize := cfg.Execution.QueueSize
		if queueSize <= 0 {
			queueSize = 100
		}
		return taskmaster.NewDispatcher(
			taskmaster.WithMode(taskmaster.ManagedMode),
			taskmaster.WithWorkerCount(workerCount),
			taskmaster.WithTaskTimeout(taskTimeout),
			taskmaster.WithQueueSize(queueSize),
			taskmaster.WithLogger(logger.NewStandardLogger()),
		)
	}
}

// Start initializes the worker based on the operational mode.
func (w *SyncWorkerWithTaskmaster) Start() error {
	log.Printf("Starting sync worker with taskmaster (ID: %s, Mode: %s)", w.Config.WorkerID, w.Mode)

	ctx := context.Background()

	// Start the dispatcher
	if err := w.Dispatcher.Start(ctx); err != nil {
		return err
	}

	// Mode-specific initialization
	switch w.Mode {
	case "scheduled_managed":
		return w.startScheduledMode(ctx)
	case "rest":
		return w.startRESTMode(ctx)
	case "machinery":
		return w.startMachineryMode(ctx)
	default:
		return w.startScheduledMode(ctx)
	}
}

// startScheduledMode loads accounts and creates scheduled sync tasks.
func (w *SyncWorkerWithTaskmaster) startScheduledMode(ctx context.Context) error {
	// Load all active accounts
	accounts, _, err := w.Store.List(ctx, 0, 0)
	if err != nil {
		log.Printf("Warning: Failed to load accounts: %v", err)
		return nil
	}

	log.Printf("Loaded %d accounts", len(accounts))

	// Build batch of scheduled tasks
	var scheduledTasks []taskmaster.ScheduledTask
	var accountNames []string

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

		scheduledTasks = append(scheduledTasks, taskmaster.ScheduledTask{
			TaskID:   "sync",
			Payload:  payloadBytes,
			Interval: interval,
			Options: &taskmaster.ScheduleTaskOptions{
				Name: "sync_" + acc.ID,
			},
		})
		accountNames = append(accountNames, acc.ID)
	}

	// Schedule all tasks in one batch
	if len(scheduledTasks) > 0 {
		scheduleIDs, err := w.Dispatcher.ScheduleTaskMultiple(ctx, scheduledTasks)
		if err != nil {
			log.Printf("Warning: Some scheduled tasks failed: %v", err)
		}

		// Log successfully scheduled accounts
		for i, scheduleID := range scheduleIDs {
			if i < len(accountNames) {
				log.Printf("Scheduled sync for account %s (schedule_id: %s)",
					accountNames[i], scheduleID)
			}
		}
	}

	log.Printf("Sync worker started with %d scheduled accounts", len(scheduledTasks))
	return nil
}

// startRESTMode initializes REST-specific components (no account fetching).
func (w *SyncWorkerWithTaskmaster) startRESTMode(_ context.Context) error {
	log.Println("REST mode: Account fetching disabled. Waiting for HTTP requests...")
	log.Println("Endpoints:")
	log.Println("  POST /api/v1/tasks/sync - Submit sync task")
	log.Println("  GET  /api/v1/tasks/health - Health check")
	// No account loading - tasks come via HTTP
	return nil
}

// startMachineryMode initializes Machinery-specific components.
func (w *SyncWorkerWithTaskmaster) startMachineryMode(_ context.Context) error {
	log.Println("Machinery mode: Listening for incoming sync tasks from message queue...")
	log.Printf("Broker: %s", w.Config.Queue.RedisURL)
	// No account loading - tasks come from queue
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

// shouldReuseDispatcher returns true if sub-task dispatch can reuse the main dispatcher.
// This avoids creating duplicate Machinery instances when config is the same.
func shouldReuseDispatcher(cfg *workerconfig.WorkerConfig) bool {
	// Must be same type
	if cfg.SubTaskDispatch.Type != cfg.Dispatch.Type {
		return false
	}

	// For machinery, check if Redis URL and queue match
	if cfg.Dispatch.Type == "machinery" {
		_, dispatchRedisURL, dispatchQueue := cfg.GetDispatchConfig()
		subTaskRedisURL, subTaskQueue, _, _, _ := cfg.GetSubTaskDispatchConfig()

		// Reuse if Redis URL and queue name match
		return dispatchRedisURL == subTaskRedisURL && dispatchQueue == subTaskQueue
	}

	// For rest mode, always reuse (sub-tasks use same HTTP endpoints)
	if cfg.Dispatch.Type == "rest" {
		return true
	}

	// For managed mode, always reuse (direct function calls)
	return true
}

// createSubTaskDispatcher creates a dispatcher for envelope sub-tasks.
// Sub-tasks use a separate configuration to allow different scaling than main sync tasks.
// Returns the dispatcher and whether tasks were registered.
func createSubTaskDispatcher(cfg *workerconfig.WorkerConfig, webhookConfigured bool) (taskmaster.FullDispatcher, bool) {
	redisURL, queueName, _, _, _ := cfg.GetSubTaskDispatchConfig()
	tasksRegistered := false

	switch cfg.SubTaskDispatch.Type {
	case "machinery":
		log.Printf("Sub-task dispatch: machinery (redis=%s, queue=%s)", redisURL, queueName)
		dispatcher := taskmaster.NewDispatcher(
			taskmaster.WithMode(taskmaster.MachineryMode),
			taskmaster.WithMachineryConfig(&taskmaster.MachineryConfig{
				BrokerURL:         redisURL,
				ResultBackend:     redisURL, // Use same Redis for results
				DefaultQueue:      queueName,
				DefaultRetryCount: 3,
			}),
			taskmaster.WithDispatchOnly(true), // Dispatch-only mode (no local worker)
			taskmaster.WithLogger(logger.NewStandardLogger()),
		)
		// Start the dispatcher to initialize the Machinery server
		if err := dispatcher.Start(context.Background()); err != nil {
			log.Printf("Warning: Failed to start sub-task dispatcher: %v", err)
		}
		// Register tasks for dispatch-only (they execute on remote workers)
		if err := dispatcher.Register(&workers.EnvelopeProcessorTask{}); err != nil {
			log.Printf("Warning: Failed to register envelope_processor: %v", err)
		} else {
			tasksRegistered = true
		}
		if webhookConfigured {
			webhookNotifier := &workers.WebhookNotifierTask{
				WebhookURL: cfg.Webhook.URL,
				SecretKey:  cfg.Webhook.SecretKey,
			}
			if err := dispatcher.Register(webhookNotifier); err != nil {
				log.Printf("Warning: Failed to register webhook_notifier: %v", err)
			} else {
				tasksRegistered = true
			}
		}
		log.Println("Sub-task handlers registered for dispatch")
		return dispatcher, tasksRegistered
	default: // managed - direct dispatch
		log.Println("Sub-task dispatch: managed (direct function calls)")
		// For managed mode, we use a minimal dispatcher that executes tasks directly
		return taskmaster.NewDispatcher(
			taskmaster.WithMode(taskmaster.ManagedMode),
			taskmaster.WithWorkerCount(1), // Single worker for direct dispatch
			taskmaster.WithTaskTimeout(5*time.Minute),
			taskmaster.WithQueueSize(1000), // Larger queue for burst handling
			taskmaster.WithLogger(logger.NewStandardLogger()),
		), false
	}
}

func createQueue(dispatcher taskmaster.TaskDispatcher, webhookConfigured bool) envelopequeue.EnvelopeQueue {
	// Configure envelope processing pipeline with all workers EXCEPT sync.
	// Sync task fetches envelopes and enqueues them for processing.
	// The envelope queue then dispatches to downstream workers.
	//
	// Available workers (configure based on your needs):
	// - envelope_processor: Processes envelope metadata, fetches message body
	// - link_extractor: Extracts links from message bodies
	// - attachment_processor: Processes attachments
	// - webhook_notifier: Sends webhooks for new messages (only if URL configured)
	// - spam_classifier: Classifies spam (if enabled)
	//
	// Note: "sync" task is NOT included to avoid infinite loop
	// (sync -> enqueue -> sync -> enqueue -> ...)

	// Build task pipeline
	tasks := []envelopequeue.TaskRoute{
		{
			TaskID:  "envelope_processor",
			Enabled: true,
			Config: map[string]interface{}{
				"fetch_body":          true,
				"extract_links":       false, // Can be enabled per account
				"process_attachments": false, // Can be enabled per account
			},
		},
	}

	// Add webhook notifier only if webhook URL is configured
	// Signature is optional - only added if secret_key is provided
	if webhookConfigured {
		tasks = append(tasks, envelopequeue.TaskRoute{
			TaskID:  "webhook_notifier",
			Enabled: true,
			Config: map[string]interface{}{
				"event_type": "envelope.received",
			},
		})
		log.Println("Webhook notifier enabled in envelope processing pipeline")
	} else {
		log.Println("Webhook notifier disabled (no URL configured)")
	}

	config := &envelopequeue.TaskmasterQueueConfig{
		Tasks: tasks,
	}

	return envelopequeue.NewTaskmasterEnvelopeQueue(dispatcher, config)
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
	mode := flag.String("mode", "", "Operational mode: managed, rest, machinery (overrides config)")
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

	// Resolve mode: CLI > Config > Default
	// Use GetExecutionMode() which considers both Dispatch and Execution config
	operationalMode := resolveOperationalMode(*mode, cfg.GetExecutionMode())

	// Log startup info with dispatch/execution details
	log.Printf("=== Sync Worker Taskmaster ===")
	log.Printf("Worker ID:     %s", cfg.WorkerID)
	log.Printf("Dispatch:      %s", cfg.Dispatch.Type)
	log.Printf("Execution:     %s (%s mode)", cfg.Execution.Mode, operationalMode)
	log.Printf("Scheduler:     enabled=%v, accounts=%v", cfg.Scheduler.Enabled, cfg.Scheduler.ScheduleAccounts)
	log.Printf("================================")

	// Create and run sync worker with taskmaster
	syncWorker, err := NewSyncWorkerWithTaskmaster(cfg, operationalMode)
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

// resolveOperationalMode implements precedence rules: CLI > Config > Default.
// Supports both legacy mode names (scheduled_managed) and new names (managed).
func resolveOperationalMode(cliMode, configMode string) string {
	validModes := map[string]bool{
		"managed":           true,
		"scheduled_managed": true, // Legacy alias for managed
		"rest":              true,
		"machinery":         true,
	}

	// CLI argument takes precedence
	if cliMode != "" {
		if !validModes[cliMode] {
			log.Fatalf("Invalid mode '%s'. Valid modes: managed, rest, machinery", cliMode)
		}
		// Normalize legacy mode name
		if cliMode == "scheduled_managed" {
			return "managed"
		}
		return cliMode
	}

	// Config file value
	if configMode != "" {
		if !validModes[configMode] {
			log.Fatalf("Invalid config mode '%s'. Valid modes: managed, rest, machinery", configMode)
		}
		// Normalize legacy mode name
		if configMode == "scheduled_managed" {
			return "managed"
		}
		return configMode
	}

	// Default
	log.Println("No mode specified, defaulting to 'managed'")
	return "managed"
}
