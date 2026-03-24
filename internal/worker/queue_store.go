package worker

import (
	"fmt"
	"log"

	"webmail_engine/internal/config"
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
	case "sql", "":
		// For "sql" type, determine the actual driver from the SQL config
		if cfg.Store.SQL == nil {
			return nil, fmt.Errorf("SQL config is required for store type: sql")
		}
		log.Printf("Using SQL store (%s) at %s", cfg.Store.SQL.Driver, cfg.Store.SQL.DSN)
		return store.NewSQLStore(config.SQLConfig{
			Driver:         cfg.Store.SQL.Driver,
			DSN:            cfg.Store.SQL.DSN,
			MaxConnections: cfg.Store.SQL.MaxConnections,
			MinIdle:        cfg.Store.SQL.MinIdle,
			BusyTimeoutMs:  cfg.Store.SQL.BusyTimeoutMs,
		})
	case "sqlite", "postgres":
		// Direct driver specification (deprecated, but supported for backwards compatibility)
		if cfg.Store.SQL == nil {
			return nil, fmt.Errorf("SQL config is required for store type: %s", cfg.Store.Type)
		}
		log.Printf("Using SQL store (%s) at %s", cfg.Store.SQL.Driver, cfg.Store.SQL.DSN)
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
		return nil, fmt.Errorf("unknown store type: %s (supported: sql, sqlite, postgres, memory)", cfg.Store.Type)
	}
}
