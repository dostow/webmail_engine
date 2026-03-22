package main

import (
	"flag"
	"log"

	"webmail_engine/internal/config"
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
		cfg.Queue.Type = *queueType
		cfg.Queue.RedisURL = *redisURL
		cfg.Store.Type = *storeType
		cfg.Store.SQL = &config.SQLConfig{
			Driver: *storeDriver,
			DSN:    *storeDSN,
		}
	}

	// Create and run sync worker
	syncWorker, err := worker.NewSyncWorker(cfg, nil)
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
