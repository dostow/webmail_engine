package worker

import (
	"fmt"
	"log"

	"webmail_engine/internal/envelopequeue"
	"webmail_engine/internal/store"
	"webmail_engine/internal/workerconfig"
)

// createQueue creates the envelope queue based on configuration
func createQueue(cfg *workerconfig.WorkerConfig) (envelopequeue.EnvelopeQueue, error) {
	switch cfg.Queue.Type {
	case "redis":
		queueCfg := cfg.ToEnvelopeQueueConfig()
		return envelopequeue.NewMachineryEnvelopeQueue(queueCfg)
	case "memory", "":
		log.Println("Using memory-based queue (channel-driven, single-process testing)")
		return envelopequeue.NewMemoryEnvelopeQueue(envelopequeue.DefaultMemoryEnvelopeQueueConfig()), nil
	default:
		return nil, fmt.Errorf("unknown queue type: %s (supported: memory, redis)", cfg.Queue.Type)
	}
}

// createStore creates the account store based on configuration
func createStore(cfg *workerconfig.WorkerConfig) (store.AccountStore, error) {
	switch cfg.Store.Type {
	case "sqlite", "":
		log.Printf("Using SQLite store at %s", cfg.Store.SQLite.Path)
		return store.NewSQLiteStore(store.SQLiteConfig{
			Path:           cfg.Store.SQLite.Path,
			MaxConnections: cfg.Store.SQLite.MaxConnections,
			BusyTimeoutMs:  cfg.Store.SQLite.BusyTimeoutMs,
		})
	case "postgres":
		log.Printf("Using PostgreSQL store at %s:%d/%s", cfg.Store.Postgres.Host, cfg.Store.Postgres.Port, cfg.Store.Postgres.Database)
		return nil, fmt.Errorf("PostgreSQL store not yet implemented - use sqlite or memory")
	case "memory":
		log.Println("Using in-memory store (data will not persist)")
		return store.NewMemoryStore(), nil
	default:
		return nil, fmt.Errorf("unknown store type: %s (supported: sqlite, postgres, memory)", cfg.Store.Type)
	}
}
