package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"webmail_engine/internal/config"
	"webmail_engine/internal/pool"
	"webmail_engine/internal/service"
	"webmail_engine/internal/store"
	"webmail_engine/internal/taskmaster"
	"webmail_engine/internal/workerconfig"

	"webmail_engine/internal/workers"
)

// ProcessorWorkerWithTaskmaster is a processor worker that uses taskmaster for task execution.
type ProcessorWorkerWithTaskmaster struct {
	Config           *workerconfig.WorkerConfig
	Store            store.AccountStore
	Dispatcher       taskmaster.FullDispatcher
	ProcessorService *service.EnvelopeProcessorService
	AccountService   *service.AccountService
	SessionPool      *pool.IMAPSessionPool
	Mode             string
}

// NewProcessorWorkerWithTaskmaster creates a new processor worker using taskmaster.
func NewProcessorWorkerWithTaskmaster(cfg *workerconfig.WorkerConfig, mode string) (*ProcessorWorkerWithTaskmaster, error) {
	accountStore, err := createStore(cfg)
	if err != nil {
		return nil, err
	}

	sessionPool := pool.NewIMAPSessionPool(pool.DefaultSessionPoolConfig(), nil)

	accountService, err := service.NewAccountService(
		accountStore,
		nil,
		sessionPool,
		nil,
		nil,
		service.AccountServiceConfig{
			EncryptionKey: cfg.Security.EncryptionKey,
		},
	)
	if err != nil {
		accountStore.Close()
		return nil, err
	}

	sessionPool.SetAccountService(accountService)
	processorService := service.NewEnvelopeProcessorService(accountService, sessionPool)

	execMode := toTaskmasterMode(mode)
	log.Printf("Initializing taskmaster dispatcher (mode=%s)...", execMode.String())

	dispatcher := createDispatcher(execMode, cfg, mode)

	processorTask := &workers.EnvelopeProcessorTask{
		ProcessorService: processorService,
	}
	if err := dispatcher.Register(processorTask); err != nil {
		accountStore.Close()
		return nil, err
	}

	return &ProcessorWorkerWithTaskmaster{
		Config:           cfg,
		Store:            accountStore,
		Dispatcher:       dispatcher,
		ProcessorService: processorService,
		AccountService:   accountService,
		SessionPool:      sessionPool,
		Mode:             mode,
	}, nil
}

func createDispatcher(mode taskmaster.ExecutionMode, cfg *workerconfig.WorkerConfig, _ string) taskmaster.FullDispatcher {
	switch mode {
	case taskmaster.RESTMode:
		return taskmaster.NewDispatcher(
			taskmaster.WithMode(mode),
			taskmaster.WithRESTConfig(&taskmaster.RESTConfig{
				Addr:                ":8081",
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
				DefaultQueue:      "webmail_processor_tasks",
				DefaultRetryCount: 3,
			}),
			taskmaster.WithLogger(&standardLogger{}),
		)
	default:
		return taskmaster.NewDispatcher(
			taskmaster.WithMode(mode),
			taskmaster.WithWorkerCount(cfg.ProcessorConfig.Concurrency),
			taskmaster.WithTaskTimeout(2*time.Minute),
			taskmaster.WithQueueSize(cfg.ProcessorConfig.BatchSize*2),
			taskmaster.WithLogger(&standardLogger{}),
		)
	}
}

func toTaskmasterMode(mode string) taskmaster.ExecutionMode {
	switch mode {
	case "rest":
		return taskmaster.RESTMode
	case "machinery":
		return taskmaster.MachineryMode
	default:
		return taskmaster.ManagedMode
	}
}

func (w *ProcessorWorkerWithTaskmaster) Start() error {
	log.Printf("Starting processor worker (ID: %s, Mode: %s, Concurrency: %d)",
		w.Config.WorkerID, w.Mode, w.Config.ProcessorConfig.Concurrency)

	ctx := context.Background()
	if err := w.Dispatcher.Start(ctx); err != nil {
		return err
	}

	switch w.Mode {
	case "managed":
		return w.startManagedMode(ctx)
	case "rest":
		return w.startRESTMode(ctx)
	case "machinery":
		return w.startMachineryMode(ctx)
	default:
		return w.startManagedMode(ctx)
	}
}

func (w *ProcessorWorkerWithTaskmaster) startManagedMode(_ context.Context) error {
	log.Println("Processor worker running - waiting for envelope_processor tasks...")
	return nil
}

func (w *ProcessorWorkerWithTaskmaster) startRESTMode(_ context.Context) error {
	log.Println("REST mode: Waiting for HTTP task submissions...")
	log.Println("Endpoints:")
	log.Println("  POST /api/v1/tasks/envelope_processor - Submit task")
	log.Println("  GET  /api/v1/tasks/health - Health check")
	return nil
}

func (w *ProcessorWorkerWithTaskmaster) startMachineryMode(_ context.Context) error {
	log.Println("Machinery mode: Listening for tasks from queue...")
	log.Printf("Broker: %s", w.Config.Queue.RedisURL)
	return nil
}

func (w *ProcessorWorkerWithTaskmaster) Run() error {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	return w.Stop()
}

func (w *ProcessorWorkerWithTaskmaster) Stop() error {
	log.Println("Shutting down processor worker...")

	ctx, cancel := context.WithTimeout(context.Background(), w.Config.ShutdownTimeout)
	defer cancel()

	if err := w.Dispatcher.Stop(ctx); err != nil {
		log.Printf("Error stopping dispatcher: %v", err)
	}

	if w.Store != nil {
		w.Store.Close()
	}

	log.Println("Processor worker stopped")
	return nil
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
	configPath := flag.String("config", "", "Path to configuration file")
	mode := flag.String("mode", "managed", "Operational mode: managed, rest, machinery")
	queueType := flag.String("queue", "memory", "Queue type: memory, redis")
	redisURL := flag.String("redis", "redis://localhost:6379", "Redis URL")
	storeType := flag.String("store", "sql", "Store type: sql, memory")
	storeDriver := flag.String("store-driver", "sqlite", "SQL driver: sqlite, postgres")
	storeDSN := flag.String("store-dsn", "./data/webmail.db", "SQL DSN")
	concurrency := flag.Int("concurrency", 4, "Number of processor workers")
	flag.Parse()

	var cfg *workerconfig.WorkerConfig
	var err error

	if *configPath != "" {
		cfg, err = workerconfig.LoadWorkerConfig(*configPath)
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}
	} else {
		cfg = workerconfig.DefaultWorkerConfig("processor")
		cfg.Queue.Type = *queueType
		cfg.Queue.RedisURL = *redisURL
		cfg.Store.Type = *storeType
		cfg.Store.SQL = &config.SQLConfig{
			Driver: *storeDriver,
			DSN:    *storeDSN,
		}
		cfg.ProcessorConfig = &service.EnvelopeProcessorConfig{
			Concurrency:     *concurrency,
			BatchSize:       20,
			PollInterval:    5 * time.Second,
			CleanupInterval: 1 * time.Hour,
			CleanupAge:      24 * time.Hour,
			TempStoragePath: "data/attachments",
		}
		config.ExpandEnvVars(cfg)
	}

	operationalMode := resolveOperationalMode(*mode, cfg.OperationalMode)

	log.Printf("=== Processor Worker Taskmaster ===")
	log.Printf("Worker ID:        %s", cfg.WorkerID)
	log.Printf("Operational Mode: %s", operationalMode)
	log.Printf("Concurrency:      %d", cfg.ProcessorConfig.Concurrency)
	log.Printf("====================================")

	processorWorker, err := NewProcessorWorkerWithTaskmaster(cfg, operationalMode)
	if err != nil {
		log.Fatalf("Failed to create processor worker: %v", err)
	}

	if err := processorWorker.Start(); err != nil {
		log.Fatalf("Failed to start processor worker: %v", err)
	}

	if err := processorWorker.Run(); err != nil {
		log.Fatalf("Processor worker error: %v", err)
	}
}

func resolveOperationalMode(cliMode, configMode string) string {
	validModes := map[string]bool{
		"managed":   true,
		"rest":      true,
		"machinery": true,
	}

	if cliMode != "" && cliMode != "managed" {
		if !validModes[cliMode] {
			log.Fatalf("Invalid mode '%s'. Valid modes: managed, rest, machinery", cliMode)
		}
		return cliMode
	}

	if configMode != "" {
		if !validModes[configMode] {
			log.Fatalf("Invalid config mode '%s'. Valid modes: managed, rest, machinery", configMode)
		}
		return configMode
	}

	return "managed"
}
