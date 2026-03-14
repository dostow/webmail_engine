package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the webmail engine
type Config struct {
	Server      ServerConfig      `json:"server"`
	Redis       RedisConfig       `json:"redis"`
	Pool        PoolConfig        `json:"pool"`
	Scheduler   SchedulerConfig   `json:"scheduler"`
	Security    SecurityConfig    `json:"security"`
	Storage     StorageConfig     `json:"storage"`
	Webhook     WebhookConfig     `json:"webhook"`
	Logging     LoggingConfig     `json:"logging"`
	Store       StoreConfig       `json:"store"`
}

// StoreConfig holds account persistence store configuration
type StoreConfig struct {
	Type     string              `json:"type"` // "memory", "sqlite", "postgres"
	SQLite   *SQLiteConfig       `json:"sqlite,omitempty"`
	Postgres *PostgresConfig     `json:"postgres,omitempty"`
}

// SQLiteConfig holds SQLite configuration
type SQLiteConfig struct {
	Path           string `json:"path"`
	MaxConnections int    `json:"max_connections"`
	BusyTimeoutMs  int    `json:"busy_timeout_ms"`
}

// PostgresConfig holds PostgreSQL configuration
type PostgresConfig struct {
	Host         string `json:"host"`
	Port         int    `json:"port"`
	Database     string `json:"database"`
	User         string `json:"user"`
	Password     string `json:"password"`
	SSLMode      string `json:"ssl_mode"` // "disable", "require", "verify-full"
	MaxConnections int  `json:"max_connections"`
	MinIdle      int    `json:"min_idle"`
	ConnTimeoutMs int   `json:"conn_timeout_ms"`
}

// ServerConfig holds HTTP server configuration
type ServerConfig struct {
	Host            string        `json:"host"`
	Port            int           `json:"port"`
	ReadTimeout     time.Duration `json:"read_timeout"`
	WriteTimeout    time.Duration `json:"write_timeout"`
	IdleTimeout     time.Duration `json:"idle_timeout"`
	ShutdownTimeout time.Duration `json:"shutdown_timeout"`
	TLSEnabled      bool          `json:"tls_enabled"`
	TLSCertFile     string        `json:"tls_cert_file"`
	TLSKeyFile      string        `json:"tls_key_file"`
}

// RedisConfig holds Redis configuration
type RedisConfig struct {
	Host            string        `json:"host"`
	Port            int           `json:"port"`
	Password        string        `json:"password"`
	DB              int           `json:"db"`
	PoolSize        int           `json:"pool_size"`
	MinIdleConns    int           `json:"min_idle_conns"`
	ConnTimeout     time.Duration `json:"conn_timeout"`
	ReadTimeout     time.Duration `json:"read_timeout"`
	WriteTimeout    time.Duration `json:"write_timeout"`
	MaxRetries      int           `json:"max_retries"`
}

// PoolConfig holds connection pool configuration
type PoolConfig struct {
	MaxConnections int           `json:"max_connections"`
	IdleTimeout    time.Duration `json:"idle_timeout"`
	DialTimeout    time.Duration `json:"dial_timeout"`
	CleanupInterval time.Duration `json:"cleanup_interval"`
}

// SchedulerConfig holds fair-use scheduler configuration
type SchedulerConfig struct {
	Enabled          bool              `json:"enabled"`
	DefaultBucketSize int              `json:"default_bucket_size"`
	DefaultRefillRate int              `json:"default_refill_rate"`
	OperationCosts   map[string]int    `json:"operation_costs"`
	QueueSize        int               `json:"queue_size"`
	MaxQueueWait     time.Duration     `json:"max_queue_wait"`
}

// SecurityConfig holds security configuration
type SecurityConfig struct {
	EncryptionKey      string        `json:"encryption_key"`
	WebhookSecret      string        `json:"webhook_secret"`
	SignedURLSecret    string        `json:"signed_url_secret"`
	SignedURLExpiry    time.Duration `json:"signed_url_expiry"`
	MaxAttachmentSize  int64         `json:"max_attachment_size"`
	AllowedOrigins     []string      `json:"allowed_origins"`
	RateLimitEnabled   bool          `json:"rate_limit_enabled"`
	RateLimitRequests  int           `json:"rate_limit_requests"`
	RateLimitWindow    time.Duration `json:"rate_limit_window"`
}

// StorageConfig holds storage configuration
type StorageConfig struct {
	TempPath         string        `json:"temp_path"`
	AttachmentPath   string        `json:"attachment_path"`
	MaxTempSize      int64         `json:"max_temp_size"`
	CleanupInterval  time.Duration `json:"cleanup_interval"`
	AttachmentExpiry time.Duration `json:"attachment_expiry"`
}

// WebhookConfig holds webhook configuration
type WebhookConfig struct {
	Enabled           bool          `json:"enabled"`
	MaxRetries        int           `json:"max_retries"`
	RetryBackoff      time.Duration `json:"retry_backoff"`
	Timeout           time.Duration `json:"timeout"`
	EventRetention    time.Duration `json:"event_retention"`
	CleanupInterval   time.Duration `json:"cleanup_interval"`
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level      string `json:"level"`
	Format     string `json:"format"`
	OutputPath string `json:"output_path"`
	EnableJSON bool   `json:"enable_json"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host:            "0.0.0.0",
			Port:            8080,
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			IdleTimeout:     120 * time.Second,
			ShutdownTimeout: 30 * time.Second,
			TLSEnabled:      false,
		},
		Redis: RedisConfig{
			Host:            "localhost",
			Port:            6379,
			Password:        "",
			DB:              0,
			PoolSize:        10,
			MinIdleConns:    5,
			ConnTimeout:     10 * time.Second,
			ReadTimeout:     3 * time.Second,
			WriteTimeout:    3 * time.Second,
			MaxRetries:      3,
		},
		Pool: PoolConfig{
			MaxConnections:  100,
			IdleTimeout:     5 * time.Minute,
			DialTimeout:     30 * time.Second,
			CleanupInterval: 1 * time.Minute,
		},
		Scheduler: SchedulerConfig{
			Enabled:           true,
			DefaultBucketSize: 100,
			DefaultRefillRate: 10,
			OperationCosts: map[string]int{
				"FETCH":      1,
				"SEARCH":     5,
				"SEND":       3,
				"LIST":       1,
				"RETRIEVE":   2,
				"ATTACHMENT": 3,
				"IDLE":       0,
				"SYNC":       2,
			},
			QueueSize:    1000,
			MaxQueueWait: 30 * time.Minute,
		},
		Security: SecurityConfig{
			EncryptionKey:     "", // Should be set via environment
			WebhookSecret:     "",
			SignedURLSecret:   "",
			SignedURLExpiry:   24 * time.Hour,
			MaxAttachmentSize: 50 * 1024 * 1024, // 50MB
			RateLimitEnabled:  true,
			RateLimitRequests: 100,
			RateLimitWindow:   time.Minute,
		},
		Storage: StorageConfig{
			TempPath:         "./temp",
			AttachmentPath:   "./temp/attachments",
			MaxTempSize:      1024 * 1024 * 1024, // 1GB
			CleanupInterval:  1 * time.Hour,
			AttachmentExpiry: 24 * time.Hour,
		},
		Webhook: WebhookConfig{
			Enabled:         true,
			MaxRetries:      3,
			RetryBackoff:    time.Minute,
			Timeout:         30 * time.Second,
			EventRetention:  24 * time.Hour,
			CleanupInterval: 1 * time.Hour,
		},
		Logging: LoggingConfig{
			Level:      "info",
			Format:     "text",
			OutputPath: "stdout",
			EnableJSON: false,
		},
		Store: StoreConfig{
			Type: "memory", // Default to in-memory store
			SQLite: &SQLiteConfig{
				Path:           "./data/accounts.db",
				MaxConnections: 10,
				BusyTimeoutMs:  5000,
			},
		},
	}
}

// LoadFromFile loads configuration from a JSON file
func LoadFromFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	config := DefaultConfig()
	if err := json.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return config, nil
}

// LoadFromEnv loads configuration from environment variables
func LoadFromEnv() *Config {
	config := DefaultConfig()

	// Server
	if host := os.Getenv("SERVER_HOST"); host != "" {
		config.Server.Host = host
	}
	if port := os.Getenv("SERVER_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			config.Server.Port = p
		}
	}

	// Redis
	if host := os.Getenv("REDIS_HOST"); host != "" {
		config.Redis.Host = host
	}
	if port := os.Getenv("REDIS_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			config.Redis.Port = p
		}
	}
	if password := os.Getenv("REDIS_PASSWORD"); password != "" {
		config.Redis.Password = password
	}

	// Security
	if key := os.Getenv("ENCRYPTION_KEY"); key != "" {
		config.Security.EncryptionKey = key
	}
	if secret := os.Getenv("WEBHOOK_SECRET"); secret != "" {
		config.Security.WebhookSecret = secret
	}
	if secret := os.Getenv("SIGNED_URL_SECRET"); secret != "" {
		config.Security.SignedURLSecret = secret
	}

	// Logging
	if level := os.Getenv("LOG_LEVEL"); level != "" {
		config.Logging.Level = level
	}
	if format := os.Getenv("LOG_FORMAT"); format != "" {
		config.Logging.Format = format
	}

	return config
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Validate encryption key
	if c.Security.EncryptionKey == "" {
		return fmt.Errorf("encryption key is required")
	}
	if len(c.Security.EncryptionKey) != 32 {
		return fmt.Errorf("encryption key must be 32 bytes")
	}

	// Validate server port
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server port")
	}

	// Validate Redis port
	if c.Redis.Port < 1 || c.Redis.Port > 65535 {
		return fmt.Errorf("invalid Redis port")
	}

	// Validate pool size
	if c.Pool.MaxConnections < 1 {
		return fmt.Errorf("max connections must be positive")
	}

	return nil
}

// SaveToFile saves configuration to a JSON file
func (c *Config) SaveToFile(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
