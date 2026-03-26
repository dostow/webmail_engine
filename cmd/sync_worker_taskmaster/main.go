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

	// Create dispatcher with mode
	execMode := toTaskmasterMode(mode)
	log.Printf("Initializing taskmaster dispatcher (mode=%s)...", execMode.String())

	dispatcher := createDispatcher(execMode, cfg, mode)

	// Create envelope queue that uses taskmaster dispatcher
	queue := createQueue(dispatcher)

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

// createDispatcher creates a taskmaster dispatcher with mode-specific configuration
func createDispatcher(mode taskmaster.ExecutionMode, cfg *workerconfig.WorkerConfig, _ string) taskmaster.FullDispatcher {
	switch mode {
	case taskmaster.RESTMode:
		return taskmaster.NewDispatcher(
			taskmaster.WithMode(mode),
			taskmaster.WithRESTConfig(&taskmaster.RESTConfig{
				Addr:                ":8080",
				BasePath:            "/api/v1/tasks",
				EnableSyncExecution: true,
			}),
			taskmaster.WithLogger(&standardLogger{}),
		)
	case taskmaster.MachineryMode:
		return taskmaster.NewDispatcher(
			taskmaster.WithMode(mode),
			taskmaster.WithMachineryConfig(&taskmaster.MachineryConfig{
				BrokerURL:         cfg.Queue.RedisURL,
				ResultBackend:     cfg.Queue.RedisURL,
				DefaultQueue:      "webmail_sync_tasks",
				DefaultRetryCount: 3,
			}),
			taskmaster.WithLogger(&standardLogger{}),
		)
	default: // ManagedMode (scheduled_managed)
		return taskmaster.NewDispatcher(
			taskmaster.WithMode(mode),
			taskmaster.WithWorkerCount(2),
			taskmaster.WithTaskTimeout(5*time.Minute),
			taskmaster.WithQueueSize(100),
			taskmaster.WithLogger(&standardLogger{}),
		)
	}
}

// toTaskmasterMode converts string mode to taskmaster.ExecutionMode
func toTaskmasterMode(mode string) taskmaster.ExecutionMode {
	switch mode {
	case "rest":
		return taskmaster.RESTMode
	case "machinery":
		return taskmaster.MachineryMode
	default:
		return taskmaster.ManagedMode // scheduled_managed
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

func createQueue(dispatcher taskmaster.TaskDispatcher) envelopequeue.EnvelopeQueue {
	// Configure envelope processing pipeline with all workers EXCEPT sync.
	// Sync task fetches envelopes and enqueues them for processing.
	// The envelope queue then dispatches to downstream workers.
	//
	// Available workers (configure based on your needs):
	// - envelope_processor: Processes envelope metadata, fetches message body
	// - link_extractor: Extracts links from message bodies
	// - attachment_processor: Processes attachments
	// - webhook_notifier: Sends webhooks for new messages
	// - spam_classifier: Classifies spam (if enabled)
	//
	// Note: "sync" task is NOT included to avoid infinite loop
	// (sync -> enqueue -> sync -> enqueue -> ...)
	config := &envelopequeue.TaskmasterQueueConfig{
		Tasks: []envelopequeue.TaskRoute{
			{
				TaskID:  "envelope_processor",
				Enabled: true,
				Config: map[string]interface{}{
					"fetch_body":          true,
					"extract_links":       false, // Can be enabled per account
					"process_attachments": false, // Can be enabled per account
				},
			},
			// Add more workers as needed:
			// {
			// 	TaskID:  "link_extractor",
			// 	Enabled: true,
			// 	Config: map[string]interface{}{
			// 		"extract_images": true,
			// 	},
			// },
			// {
			// 	TaskID:  "webhook_notifier",
			// 	Enabled: true,
			// 	Config: map[string]interface{}{
			// 		"url": "https://your-webhook-url.com/notify",
			// 	},
			// },
		},
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

// standardLogger adapts the standard log package to the taskmaster.Logger interface.
type standardLogger struct{}

func (l *standardLogger) Info(msg string, keysAndValues ...interface{}) {
	log.Printf("[INFO] %s %v", msg, keysAndValues)
}

func (l *standardLogger) Error(msg string, keysAndValues ...interface{}) {
	log.Printf("[ERROR] %s %v", msg, keysAndValues)
}

func (l *standardLogger) Debug(msg string, keysAndValues ...interface{}) {
	log.Printf("[DEBUG] %s %v", msg, keysAndValues)
}

func (l *standardLogger) Warn(msg string, keysAndValues ...interface{}) {
	log.Printf("[WARN] %s %v", msg, keysAndValues)
}

func main() {
	// Parse command line flags
	configPath := flag.String("config", "", "Path to configuration file")
	mode := flag.String("mode", "scheduled_managed", "Operational mode: scheduled_managed, rest, machinery")
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
	operationalMode := resolveOperationalMode(*mode, cfg.OperationalMode)
	cfg.OperationalMode = operationalMode

	// Log startup info
	log.Printf("=== Sync Worker Taskmaster ===")
	log.Printf("Worker ID:       %s", cfg.WorkerID)
	log.Printf("Operational Mode: %s", operationalMode)
	log.Printf("Mode Source:     %s", getModeSource(*mode, cfg.OperationalMode))
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

// resolveOperationalMode implements precedence rules: CLI > Config > Default
func resolveOperationalMode(cliMode, configMode string) string {
	validModes := map[string]bool{
		"scheduled_managed": true,
		"rest":              true,
		"machinery":         true,
	}

	// CLI argument takes precedence
	if cliMode != "" && cliMode != "scheduled_managed" {
		if !validModes[cliMode] {
			log.Fatalf("Invalid mode '%s'. Valid modes: scheduled_managed, rest, machinery", cliMode)
		}
		return cliMode
	}

	// Config file value
	if configMode != "" {
		if !validModes[configMode] {
			log.Fatalf("Invalid config mode '%s'. Valid modes: scheduled_managed, rest, machinery", configMode)
		}
		return configMode
	}

	// Default
	log.Println("No mode specified, defaulting to 'scheduled_managed'")
	return "scheduled_managed"
}

// getModeSource returns where the mode came from
func getModeSource(cliMode, configMode string) string {
	if cliMode != "" && cliMode != "scheduled_managed" {
		return "CLI"
	}
	if configMode != "" {
		return "Config"
	}
	return "Default"
}
