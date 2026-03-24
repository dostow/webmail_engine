package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"webmail_engine/internal/cache"
	"webmail_engine/internal/config"
	"webmail_engine/internal/configutil"
	"webmail_engine/internal/envelopequeue"
	"webmail_engine/internal/pool"
	"webmail_engine/internal/service"
	"webmail_engine/internal/storage"
	"webmail_engine/internal/store"
	"webmail_engine/internal/worker"
	"webmail_engine/internal/workerconfig"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "", "Path to configuration file")
	storeType := flag.String("store", "sql", "Store type: sql, memory")
	storeDriver := flag.String("store-driver", "sqlite", "SQL driver: sqlite, postgres")
	storeDSN := flag.String("store-dsn", "./data/accounts.db", "SQL DSN (e.g., ./data/accounts.db for sqlite)")
	concurrency := flag.Int("concurrency", 4, "Number of processor workers")
	syncInterval := flag.Int("sync-interval", 60, "Sync interval in seconds")
	attachmentPath := flag.String("attachments", "data/attachments", "Attachment storage path")
	encryptionKey := flag.String("encryption-key", "", "Encryption key (32 bytes base64, required)")
	flag.Parse()

	// Load or create configuration
	var cfg *workerconfig.WorkerConfig
	var err error

	if *configPath != "" {
		cfg, err = workerconfig.LoadWorkerConfig(*configPath)
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}
		// Set default processor config if not provided in file
		if cfg.ProcessorConfig == nil {
			cfg.ProcessorConfig = service.DefaultEnvelopeProcessorConfig()
		}
		// Command line encryption key overrides config file
		if *encryptionKey != "" {
			cfg.Security.EncryptionKey = *encryptionKey
		}
	} else {
		if *encryptionKey == "" {
			log.Fatal("Encryption key is required. Generate one with: openssl rand -base64 32")
		}
		cfg = workerconfig.DefaultWorkerConfig("memory-worker")
		cfg.Store.Type = *storeType
		cfg.Store.SQL = &config.SQLConfig{
			Driver: *storeDriver,
			DSN:    *storeDSN,
		}
		cfg.Queue.Type = "memory"
		cfg.Security.EncryptionKey = *encryptionKey
		cfg.ProcessorConfig = &service.EnvelopeProcessorConfig{
			Concurrency:     *concurrency,
			BatchSize:       20,
			PollInterval:    5 * time.Second,
			CleanupInterval: 1 * time.Hour,
			CleanupAge:      24 * time.Hour,
			TempStoragePath: *attachmentPath,
		}
		// Expand environment variables in config values
		config.ExpandEnvVars(cfg)
	}

	// Validate configuration
	if err := configutil.ValidateWorkerConfig(cfg); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	// Override config with command line flags
	cfg.ProcessorConfig.Concurrency = *concurrency
	cfg.ProcessorConfig.TempStoragePath = *attachmentPath

	log.Printf("Starting memory worker (sync + processor in single process)")
	log.Printf("Database: %s", cfg.Store.SQL.DSN)
	log.Printf("Processor concurrency: %d", cfg.ProcessorConfig.Concurrency)
	log.Printf("Sync interval: %d seconds", *syncInterval)

	// Create shared memory queue (channel-based)
	queue := envelopequeue.NewMemoryEnvelopeQueue(envelopequeue.DefaultMemoryEnvelopeQueueConfig())
	log.Println("Created shared memory queue (channel-based)")

	// Create shared store
	accountStore, err := createStore(cfg)
	if err != nil {
		log.Fatalf("Failed to create store: %v", err)
	}
	defer accountStore.Close()

	// Create shared session pool (nil account service - workers don't disable accounts)
	sessionPool := pool.NewIMAPSessionPool(pool.DefaultSessionPoolConfig(), nil)

	// Create shared cache
	memCache := cache.NewCache(nil) // nil = memory-only, no Redis
	defer memCache.Close()

	// Create shared attachment storage
	attachmentStorage := storage.NewFileAttachmentStorage(*attachmentPath)
	defer attachmentStorage.Shutdown()

	// Create sync worker with shared resources
	syncWorker, err := worker.NewSyncWorker(cfg, &worker.SyncWorkerOptions{
		Queue:       queue,
		Store:       accountStore,
		SessionPool: sessionPool,
	})
	if err != nil {
		log.Fatalf("Failed to create sync worker: %v", err)
	}

	// Create processor worker with shared resources
	processorWorker, err := worker.NewProcessorWorker(cfg, &worker.ProcessorWorkerOptions{
		Queue:             queue,
		Store:             accountStore,
		SessionPool:       sessionPool,
		Cache:             memCache,
		AttachmentStorage: attachmentStorage,
	})
	if err != nil {
		log.Fatalf("Failed to create processor worker: %v", err)
	}

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start both workers
	var wg sync.WaitGroup
	wg.Add(2)

	// Start sync worker
	go func() {
		defer wg.Done()
		if err := syncWorker.Start(); err != nil {
			log.Printf("Sync worker error: %v", err)
		}
	}()

	// Start processor worker
	go func() {
		defer wg.Done()
		if err := processorWorker.Start(); err != nil {
			log.Printf("Processor worker error: %v", err)
		}
	}()

	// Wait for shutdown signal
	<-sigChan

	log.Println("Shutting down memory worker...")

	// Stop workers
	processorWorker.Stop()
	syncWorker.Stop()

	// Wait for goroutines to finish
	wg.Wait()

	log.Println("Memory worker stopped")
}

// createStore creates the account store based on configuration
func createStore(cfg *workerconfig.WorkerConfig) (store.AccountStore, error) {
	switch cfg.Store.Type {
	case "sql":
		if cfg.Store.SQL == nil {
			cfg.Store.SQL = &config.SQLConfig{Driver: "sqlite", DSN: "./data/accounts.db", MaxConnections: 10}
		}
		log.Printf("Using SQL store with driver=%s, dsn=%s", cfg.Store.SQL.Driver, cfg.Store.SQL.DSN)
		return store.NewSQLStore(config.SQLConfig{
			Driver:         cfg.Store.SQL.Driver,
			DSN:            cfg.Store.SQL.DSN,
			MaxConnections: cfg.Store.SQL.MaxConnections,
			MinIdle:        cfg.Store.SQL.MinIdle,
			BusyTimeoutMs:  cfg.Store.SQL.BusyTimeoutMs,
		})
	case "memory":
		log.Println("Using in-memory store (data will not persist)")
		return store.NewMemoryStore(), nil
	default:
		return nil, fmt.Errorf("unknown store type: %s", cfg.Store.Type)
	}
}
