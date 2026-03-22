package configutil

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"webmail_engine/internal/config"
	"webmail_engine/internal/workerconfig"
)

// ValidationError represents a configuration validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error for field '%s': %s", e.Field, e.Message)
}

// Validator validates configuration
type Validator struct {
	errors []ValidationError
}

// NewValidator creates a new validator
func NewValidator() *Validator {
	return &Validator{
		errors: make([]ValidationError, 0),
	}
}

// Validate validates a Config and returns an error if validation fails
func Validate(cfg *config.Config) error {
	v := NewValidator()

	// Validate server config
	v.validateServerConfig(&cfg.Server)

	// Validate security config (critical!)
	v.validateSecurityConfig(&cfg.Security)

	// Validate store config
	v.validateStoreConfig(&cfg.Store)

	// Validate storage config
	v.validateStorageConfig(&cfg.Storage)

	if len(v.errors) > 0 {
		return &MultiError{Errors: v.errors}
	}

	return nil
}

// ValidateWorkerConfig validates a WorkerConfig
func ValidateWorkerConfig(cfg *workerconfig.WorkerConfig) error {
	v := NewValidator()

	// worker_type is optional - useful for standalone workers but not required
	// for combined workers like memory_worker
	if cfg.WorkerType != "" {
		// If specified, validate it
		switch cfg.WorkerType {
		case "sync", "processor", "memory-worker":
			// Valid types
		default:
			v.errors = append(v.errors, ValidationError{
				Field:   "worker_type",
				Message: fmt.Sprintf("unknown worker type: %s (supported: sync, processor, memory-worker)", cfg.WorkerType),
			})
		}
	}

	// Validate queue config
	v.validateQueueConfig(&cfg.Queue)

	// Validate store config
	v.validateWorkerStoreConfig(&cfg.Store)

	// Validate encryption key (required for AccountService)
	v.validateSecurityConfig(&cfg.Security)
	// Validate key length (must be 32 bytes for XChaCha20-Poly1305)

	if len(v.errors) > 0 {
		return &MultiError{Errors: v.errors}
	}

	return nil
}

func (v *Validator) validateServerConfig(cfg *config.ServerConfig) {
	if cfg.Host == "" {
		v.errors = append(v.errors, ValidationError{
			Field:   "server.host",
			Message: "host is required",
		})
	}

	if cfg.Port <= 0 || cfg.Port > 65535 {
		v.errors = append(v.errors, ValidationError{
			Field:   "server.port",
			Message: "port must be between 1 and 65535",
		})
	}

	if cfg.TLSEnabled {
		if cfg.TLSCertFile == "" {
			v.errors = append(v.errors, ValidationError{
				Field:   "server.tls_cert_file",
				Message: "TLS cert file is required when TLS is enabled",
			})
		}
		if cfg.TLSKeyFile == "" {
			v.errors = append(v.errors, ValidationError{
				Field:   "server.tls_key_file",
				Message: "TLS key file is required when TLS is enabled",
			})
		}
	}
}

func (v *Validator) validateSecurityConfig(cfg *config.SecurityConfig) {
	// CRITICAL: Encryption key must be set
	if strings.TrimSpace(cfg.EncryptionKey) == "" {
		v.errors = append(v.errors, ValidationError{
			Field:   "security.encryption_key",
			Message: "encryption key is required - generate one with: openssl rand -base64 32",
		})
	}

	// Validate key length (must be 32 bytes for XChaCha20-Poly1305)
	if cfg.EncryptionKey != "" {
		var err error
		key := cfg.EncryptionKey
		if len(key) == 64 {
			var keyBytes []byte
			keyBytes, err = hex.DecodeString(key)
			if err == nil && len(keyBytes) != 32 {
				v.errors = append(v.errors, ValidationError{
					Field:   "security.encryption_key",
					Message: fmt.Sprintf("encryption key must be 32 bytes (got %d)", keyBytes),
				})
			}
		} else {
			keyBytes, err := parseKeyLength(cfg.EncryptionKey)
			if err != nil {
				v.errors = append(v.errors, ValidationError{
					Field:   "security.encryption_key",
					Message: fmt.Sprintf("encryption key must be 32 bytes (got %d)", keyBytes),
				})
			}
		}
	}

	if strings.TrimSpace(cfg.WebhookSecret) == "" {
		v.errors = append(v.errors, ValidationError{
			Field:   "security.webhook_secret",
			Message: "webhook secret is required",
		})
	}
}

func (v *Validator) validateStoreConfig(cfg *config.StoreConfig) {
	switch cfg.Type {
	case "sqlite", "postgres", "":
		if cfg.SQL == nil {
			v.errors = append(v.errors, ValidationError{
				Field:   "store.sqlite",
				Message: "SQLite config is required when store type is sqlite",
			})
		} else if cfg.SQL.DSN == "" {
			v.errors = append(v.errors, ValidationError{
				Field:   "store.sqlite.path",
				Message: "SQLite path is required",
			})
		}
	case "memory":
		// Memory store is valid, no additional checks needed
	default:
		v.errors = append(v.errors, ValidationError{
			Field:   "store.type",
			Message: fmt.Sprintf("unknown store type: %s (supported: sqlite, postgres, memory)", cfg.Type),
		})
	}
}

func (v *Validator) validateWorkerStoreConfig(cfg *config.StoreConfig) {
	switch cfg.Type {
	case "sqlite", "postgres", "":
		if cfg.SQL.DSN == "" {
			v.errors = append(v.errors, ValidationError{
				Field:   "store.sql.dsn",
				Message: "SQL DSN is required",
			})
		}
	case "memory":
		// Valid
	default:
		v.errors = append(v.errors, ValidationError{
			Field:   "store.type",
			Message: fmt.Sprintf("unknown store type: %s (supported: sqlite, postgres, memory)", cfg.Type),
		})
	}
}

func (v *Validator) validateStorageConfig(cfg *config.StorageConfig) {
	if cfg.AttachmentPath == "" {
		v.errors = append(v.errors, ValidationError{
			Field:   "storage.attachment_path",
			Message: "attachment path is required",
		})
	}
}

func (v *Validator) validateQueueConfig(cfg *workerconfig.QueueConfig) {
	switch cfg.Type {
	case "memory", "redis":
		// Valid types
		if cfg.Type == "redis" && cfg.RedisURL == "" {
			v.errors = append(v.errors, ValidationError{
				Field:   "queue.redis_url",
				Message: "Redis URL is required when queue type is redis",
			})
		}
	default:
		v.errors = append(v.errors, ValidationError{
			Field:   "queue.type",
			Message: fmt.Sprintf("unknown queue type: %s (supported: memory, redis)", cfg.Type),
		})
	}
}

// MultiError represents multiple validation errors
type MultiError struct {
	Errors []ValidationError
}

func (e *MultiError) Error() string {
	var sb strings.Builder
	sb.WriteString("configuration validation failed:\n")
	for _, err := range e.Errors {
		sb.WriteString(fmt.Sprintf("  - %s: %s\n", err.Field, err.Message))
	}
	return sb.String()
}

// parseKeyLength returns the decoded key length in bytes
func parseKeyLength(key string) (int, error) {
	// Try to decode as base64 first (common format)
	decoded, err := base64Decode(key)
	if err == nil {
		return len(decoded), nil
	}
	// Otherwise use raw string length
	return len(key), nil
}

// base64Decode attempts to decode a base64 string
func base64Decode(s string) ([]byte, error) {
	// Try standard base64
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err == nil {
		return decoded, nil
	}
	// Try URL-safe base64
	decoded, err = base64.URLEncoding.DecodeString(s)
	if err == nil {
		return decoded, nil
	}
	return nil, err
}
