package workerconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"webmail_engine/internal/config"
	"webmail_engine/internal/envelopequeue"
)

// WorkerConfig holds configuration for standalone workers (sync/processor)
// Supports separate dispatch (intake), execution (processing), and sub-task dispatch configuration.
type WorkerConfig struct {
	WorkerType      string                `json:"worker_type"`       // "sync", "processor", "taskmaster"
	WorkerID        string                `json:"worker_id"`         // Unique worker identifier
	Dispatch        DispatchConfig        `json:"dispatch"`          // How tasks are received
	Execution       ExecutionConfig       `json:"execution"`         // How tasks are processed
	SubTaskDispatch SubTaskDispatchConfig `json:"sub_task_dispatch"` // How sub-tasks (envelope processing) are dispatched
	Queue           QueueConfig           `json:"queue"`             // Legacy: for backward compatibility
	Store           config.StoreConfig    `json:"store"`
	Logging         config.LoggingConfig  `json:"logging"`
	ShutdownTimeout time.Duration         `json:"shutdown_timeout"`
	Security        config.SecurityConfig `json:"security"`
	IMAP            config.IMAPConfig     `json:"imap,omitempty"`
	Webhook         WebhookConfig         `json:"webhook,omitempty"`
	Scheduler       SchedulerConfig       `json:"scheduler,omitempty"`
}

// DispatchConfig configures how tasks are received/intake.
// Supports: managed (local), rest (HTTP), machinery (Redis/RabbitMQ)
type DispatchConfig struct {
	Type      string `json:"type"`                 // "managed", "rest", "machinery"
	Addr      string `json:"addr,omitempty"`       // HTTP address for rest mode
	RedisURL  string `json:"redis_url,omitempty"`  // Redis URL for machinery mode
	QueueName string `json:"queue_name,omitempty"` // Queue name for machinery mode
}

// ExecutionConfig configures how tasks are executed/processed.
// Supports: managed (local worker pool), machinery (remote workers)
type ExecutionConfig struct {
	Mode          string        `json:"mode"`                     // "managed", "machinery"
	WorkerCount   int           `json:"worker_count"`             // For managed mode
	TaskTimeout   time.Duration `json:"task_timeout"`             // Default task timeout
	QueueSize     int           `json:"queue_size"`               // Queue buffer size
	RedisURL      string        `json:"redis_url,omitempty"`      // For machinery execution
	ResultBackend string        `json:"result_backend,omitempty"` // Machinery result backend
}

// SubTaskDispatchConfig configures how sub-tasks (e.g., envelope processing) are dispatched.
// This is separate from main task dispatch to allow different scaling for envelope processing.
// Supports: managed (direct call), machinery (Redis/RabbitMQ queue)
type SubTaskDispatchConfig struct {
	Type           string `json:"type"`            // "managed" (direct), "machinery" (queue)
	RedisURL       string `json:"redis_url"`       // Redis URL for machinery mode
	QueueName      string `json:"queue_name"`      // Queue name for machinery mode
	HighPriority   string `json:"high_priority"`   // High priority queue name
	NormalPriority string `json:"normal_priority"` // Normal priority queue name
	LowPriority    string `json:"low_priority"`    // Low priority queue name
}

// SchedulerConfig configures task scheduling behavior.
type SchedulerConfig struct {
	Enabled          bool `json:"enabled"`           // Enable task scheduling
	ScheduleAccounts bool `json:"schedule_accounts"` // Auto-schedule accounts (sync_worker)
}

// WebhookConfig holds webhook notification configuration
type WebhookConfig struct {
	URL       string `json:"url"`        // Webhook endpoint URL
	SecretKey string `json:"secret_key"` // HMAC signing key
	Enabled   bool   `json:"enabled"`    // Enable webhook notifications
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

// DefaultWorkerConfig returns default worker configuration.
// Default: managed dispatch + managed execution (single-instance, local processing).
func DefaultWorkerConfig(workerType string) *WorkerConfig {
	return &WorkerConfig{
		WorkerType: workerType,
		WorkerID:   fmt.Sprintf("%s-%d", workerType, time.Now().UnixNano()),
		Dispatch: DispatchConfig{
			Type: "managed", // Default: local task creation
		},
		Execution: ExecutionConfig{
			Mode:        "managed", // Default: local worker pool
			WorkerCount: 4,
			TaskTimeout: 30 * time.Second,
			QueueSize:   100,
		},
		SubTaskDispatch: SubTaskDispatchConfig{
			Type:           "managed", // Default: direct dispatch (same process)
			RedisURL:       "redis://localhost:6379",
			QueueName:      "envelope_tasks",
			HighPriority:   "envelope_high",
			NormalPriority: "envelope_normal",
			LowPriority:    "envelope_low",
		},
		Scheduler: SchedulerConfig{
			Enabled:          true,
			ScheduleAccounts: workerType == "sync", // Auto-schedule for sync workers
		},
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

// GetExecutionMode returns the taskmaster ExecutionMode based on Dispatch and Execution config.
// Maps the new separated config to the existing ExecutionMode enum.
func (w *WorkerConfig) GetExecutionMode() string {
	// Execution mode determines how tasks are processed
	switch w.Execution.Mode {
	case "machinery":
		return "machinery"
	case "managed":
		// For managed execution, dispatch type determines intake method
		switch w.Dispatch.Type {
		case "rest":
			return "rest"
		case "machinery":
			// Machinery dispatch with managed execution = receive from Redis, execute locally
			return "machinery"
		default:
			return "managed"
		}
	default:
		return "managed"
	}
}

// GetDispatchConfig returns dispatch-specific configuration for the dispatcher.
func (w *WorkerConfig) GetDispatchConfig() (addr string, redisURL string, queueName string) {
	switch w.Dispatch.Type {
	case "rest":
		if w.Dispatch.Addr != "" {
			addr = w.Dispatch.Addr
		} else {
			addr = ":8080"
		}
	case "machinery":
		redisURL = w.Dispatch.RedisURL
		if redisURL == "" {
			redisURL = w.Queue.RedisURL
		}
		queueName = w.Dispatch.QueueName
		if queueName == "" {
			queueName = "webmail_tasks"
		}
	}
	return addr, redisURL, queueName
}

// GetSubTaskDispatchConfig returns sub-task dispatch configuration.
func (w *WorkerConfig) GetSubTaskDispatchConfig() (redisURL string, queueName string, highPriority string, normalPriority string, lowPriority string) {
	redisURL = w.SubTaskDispatch.RedisURL
	if redisURL == "" {
		redisURL = w.Queue.RedisURL
	}
	queueName = w.SubTaskDispatch.QueueName
	if queueName == "" {
		queueName = "envelope_tasks"
	}
	highPriority = w.SubTaskDispatch.HighPriority
	if highPriority == "" {
		highPriority = w.Queue.HighPriority
	}
	normalPriority = w.SubTaskDispatch.NormalPriority
	if normalPriority == "" {
		normalPriority = w.Queue.NormalPriority
	}
	lowPriority = w.SubTaskDispatch.LowPriority
	if lowPriority == "" {
		lowPriority = w.Queue.LowPriority
	}
	return redisURL, queueName, highPriority, normalPriority, lowPriority
}
