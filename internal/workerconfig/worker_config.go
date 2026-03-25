package workerconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"webmail_engine/internal/config"
	"webmail_engine/internal/envelopequeue"
	"webmail_engine/internal/service"
)

// WorkerConfig holds configuration for standalone workers (sync/processor)
type WorkerConfig struct {
	WorkerType      string                           `json:"worker_type"`                // "sync", "processor", "memory-worker"
	WorkerID        string                           `json:"worker_id"`                  // Unique worker identifier
	OperationalMode string                           `json:"operational_mode,omitempty"` // "scheduled_managed", "rest", "machinery"
	Queue           QueueConfig                      `json:"queue"`
	Store           config.StoreConfig               `json:"store"`
	Logging         config.LoggingConfig             `json:"logging"`
	ShutdownTimeout time.Duration                    `json:"shutdown_timeout"`
	Security        config.SecurityConfig            `json:"security"`
	IMAP            config.IMAPConfig                `json:"imap"`
	ProcessorConfig *service.EnvelopeProcessorConfig `json:"processor_config,omitempty"`
}

// QueueConfig holds message queue configuration
type QueueConfig struct {
	Type           string `json:"type"` // "memory", "redis", "rabbitmq"
	RedisURL       string `json:"redis_url"`
	RabbitMQURL    string `json:"rabbitmq_url"`
	HighPriority   string `json:"high_priority"`
	NormalPriority string `json:"normal_priority"`
	LowPriority    string `json:"low_priority"`
}

// DefaultWorkerConfig returns default worker configuration
func DefaultWorkerConfig(workerType string) *WorkerConfig {
	return &WorkerConfig{
		WorkerType:      workerType, // Can be empty for combined workers
		WorkerID:        fmt.Sprintf("%s-%d", workerType, time.Now().UnixNano()),
		OperationalMode: "scheduled_managed", // Default mode
		ShutdownTimeout: 30 * time.Second,
		Queue: QueueConfig{
			Type:           "memory",
			RedisURL:       "redis://localhost:6379",
			HighPriority:   "envelope_high",
			NormalPriority: "envelope_normal",
			LowPriority:    "envelope_low",
		},
		Store: config.StoreConfig{
			Type: "sql",
			SQL: &config.SQLConfig{
				Driver:         "sqlite",
				DSN:            "./data/webmail.db",
				MaxConnections: 10,
				MinIdle:        2,
				BusyTimeoutMs:  5000,
			},
		},
		Logging: config.LoggingConfig{
			Level:  "info",
			Format: "text",
		},
	}
}

// LoadWorkerConfig loads worker configuration from a JSON file
func LoadWorkerConfig(path string) (*WorkerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	cfg := DefaultWorkerConfig("")
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Expand environment variables in config values
	config.ExpandEnvVars(cfg)

	return cfg, nil
}

// ToEnvelopeQueueConfig converts WorkerConfig to envelopequeue.MachineryQueueConfig
func (w *WorkerConfig) ToEnvelopeQueueConfig() *envelopequeue.MachineryQueueConfig {
	return &envelopequeue.MachineryQueueConfig{
		BrokerURL:           w.Queue.RedisURL,
		ResultBackend:       w.Queue.RedisURL,
		HighPriorityQueue:   w.Queue.HighPriority,
		NormalPriorityQueue: w.Queue.NormalPriority,
		LowPriorityQueue:    w.Queue.LowPriority,
		EnqueueTimeout:      30 * time.Second,
		CleanupInterval:     24 * time.Hour,
	}
}

// ToInternalConfig converts WorkerConfig to internal config.Config for service initialization
func (w *WorkerConfig) ToInternalConfig() *config.Config {
	cfg := &config.Config{
		Store: config.StoreConfig{
			Type: w.Store.Type,
		},
		Redis: config.RedisConfig{
			Host: "localhost",
			Port: 6379,
		},
		Pool: config.PoolConfig{
			MaxConnections:  10,
			IdleTimeout:     5 * time.Minute,
			DialTimeout:     30 * time.Second,
			CleanupInterval: 1 * time.Minute,
		},
		Scheduler: config.SchedulerConfig{
			Enabled:           true,
			DefaultBucketSize: 100,
			DefaultRefillRate: 10,
		},
		Storage: config.StorageConfig{
			TempPath:         "/tmp/webmail",
			AttachmentPath:   "data/attachments",
			MaxTempSize:      100 * 1024 * 1024, // 100MB
			CleanupInterval:  1 * time.Hour,
			AttachmentExpiry: 24 * time.Hour,
		},
	}

	if w.Store.SQL != nil {
		cfg.Store.SQL = &config.SQLConfig{
			Driver:         w.Store.SQL.Driver,
			DSN:            w.Store.SQL.DSN,
			MaxConnections: w.Store.SQL.MaxConnections,
			MinIdle:        w.Store.SQL.MinIdle,
			BusyTimeoutMs:  w.Store.SQL.BusyTimeoutMs,
		}
	}

	return cfg
}
