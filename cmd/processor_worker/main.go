package main

import (
	"flag"
	"log"
	"time"

	"webmail_engine/internal/config"
	"webmail_engine/internal/service"
	"webmail_engine/internal/worker"
	"webmail_engine/internal/workerconfig"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "", "Path to configuration file")
	queueType := flag.String("queue", "memory", "Queue type: memory, redis")
	redisURL := flag.String("redis", "redis://localhost:6379", "Redis URL (if queue=redis)")
	storeType := flag.String("store", "sql", "Store type: sql, memory")
	storeDriver := flag.String("store-driver", "sqlite", "SQL driver: sqlite, postgres")
	storeDSN := flag.String("store-dsn", "./data/webmail.db", "SQL DSN (e.g., ./data/webmail.db for sqlite or postgres://...)")
	attachmentPath := flag.String("attachments", "data/attachments", "Attachment storage path")
	concurrency := flag.Int("concurrency", 4, "Number of processor workers")
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
			TempStoragePath: *attachmentPath,
		}
		// Expand environment variables in config values
		config.ExpandEnvVars(cfg)
	}

	// Create and run processor worker
	processorWorker, err := worker.NewProcessorWorker(cfg, nil)
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
