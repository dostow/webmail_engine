package store

import (
	"context"
	"errors"

	"webmail_engine/internal/models"
)

// Standard errors for store operations
var (
	ErrNotFound         = errors.New("account not found")
	ErrAlreadyExists    = errors.New("account already exists")
	ErrStoreUnavailable = errors.New("store unavailable")
	ErrConnectionFailed = errors.New("failed to connect to store")
	ErrMigrationFailed  = errors.New("database migration failed")
	ErrInvalidConfig    = errors.New("invalid store configuration")
)

// IsNotFound checks if error is ErrNotFound
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// IsAlreadyExists checks if error is ErrAlreadyExists
func IsAlreadyExists(err error) bool {
	return errors.Is(err, ErrAlreadyExists)
}

// IsUnavailable checks if error indicates store unavailability
func IsUnavailable(err error) bool {
	return errors.Is(err, ErrStoreUnavailable) || errors.Is(err, ErrConnectionFailed)
}

// HealthStatus represents store health information
type HealthStatus struct {
	Status    string `json:"status"`
	LatencyMs int64  `json:"latency_ms"`
	Connected bool   `json:"connected"`
	Message   string `json:"message,omitempty"`
}

// AccountStore defines the interface for account persistence
type AccountStore interface {
	// GetByID retrieves an account by its ID
	// Returns ErrNotFound if account doesn't exist
	GetByID(ctx context.Context, id string) (*models.Account, error)

	// GetByEmail retrieves an account by email address
	// Returns ErrNotFound if account doesn't exist
	GetByEmail(ctx context.Context, email string) (*models.Account, error)

	// List retrieves all accounts with optional pagination
	// Returns empty slice (not nil) if no accounts exist
	// Returns total count of all accounts (ignoring pagination)
	List(ctx context.Context, offset, limit int) ([]*models.Account, int, error)

	// Create stores a new account
	// Returns ErrAlreadyExists if account with same email exists
	Create(ctx context.Context, account *models.Account) error

	// Update modifies an existing account
	// Returns ErrNotFound if account doesn't exist
	Update(ctx context.Context, account *models.Account) error

	// Delete removes an account by ID
	// Returns ErrNotFound if account doesn't exist
	Delete(ctx context.Context, id string) error

	// Close releases resources (connections, file handles, etc.)
	Close() error

	// Health checks if the store is operational
	Health(ctx context.Context) *HealthStatus
	
	// CreateAuditLog stores a new audit log entry
	CreateAuditLog(ctx context.Context, log *models.AuditLog) error
	
	// ListAuditLogs retrieves audit logs with optional pagination
	ListAuditLogs(ctx context.Context, offset, limit int) ([]*models.AuditLog, int, error)
}
