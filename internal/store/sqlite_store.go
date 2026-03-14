package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"webmail_engine/internal/models"
)

// accountDB is the GORM model for account persistence
type accountDB struct {
	ID              string          `gorm:"primaryKey;type:text;not null"`
	Email           string          `gorm:"uniqueIndex;type:text;not null"`
	AuthType        string          `gorm:"type:text;not null"`
	Status          string          `gorm:"type:text;not null"`
	IMAPHost        string          `gorm:"column:imap_host;type:text;not null"`
	IMAPPort        int             `gorm:"column:imap_port;not null"`
	IMAPEncryption  string          `gorm:"column:imap_encryption;type:text;not null"`
	IMAPUsername    string          `gorm:"column:imap_username;type:text;not null"`
	IMAPPassword    string          `gorm:"column:imap_password;type:text;not null"`
	SMTPHost        string          `gorm:"column:smtp_host;type:text;not null"`
	SMTPPort        int             `gorm:"column:smtp_port;not null"`
	SMTPEncryption  string          `gorm:"column:smtp_encryption;type:text;not null"`
	SMTPUsername    string          `gorm:"column:smtp_username;type:text;not null"`
	SMTPPassword    string          `gorm:"column:smtp_password;type:text;not null"`
	ConnectionLimit int             `gorm:"column:connection_limit;not null"`
	SyncSettings    json.RawMessage `gorm:"column:sync_settings;type:text;not null"`
	ProxyConfig     json.RawMessage `gorm:"column:proxy_config;type:text"`
	FairUsePolicy   json.RawMessage `gorm:"column:fair_use_policy;type:text"`
	CreatedAt       time.Time       `gorm:"column:created_at;not null"`
	UpdatedAt       time.Time       `gorm:"column:updated_at;not null"`
	LastSyncAt      *time.Time      `gorm:"column:last_sync_at"`
}

// TableName specifies the table name
func (accountDB) TableName() string {
	return "accounts"
}

// toAccount converts accountDB to models.Account
func (a *accountDB) toAccount() (*models.Account, error) {
	acc := &models.Account{
		ID:       a.ID,
		Email:    a.Email,
		AuthType: models.AuthType(a.AuthType),
		Status:   models.AccountStatus(a.Status),
		IMAPConfig: models.ServerConfig{
			Host:       a.IMAPHost,
			Port:       a.IMAPPort,
			Encryption: models.EncryptionType(a.IMAPEncryption),
			Username:   a.IMAPUsername,
			Password:   a.IMAPPassword,
		},
		SMTPConfig: models.ServerConfig{
			Host:       a.SMTPHost,
			Port:       a.SMTPPort,
			Encryption: models.EncryptionType(a.SMTPEncryption),
			Username:   a.SMTPUsername,
			Password:   a.SMTPPassword,
		},
		ConnectionLimit: a.ConnectionLimit,
		CreatedAt:       a.CreatedAt,
		UpdatedAt:       a.UpdatedAt,
		LastSyncAt:      a.LastSyncAt,
	}

	// Parse SyncSettings
	if len(a.SyncSettings) > 0 {
		if err := json.Unmarshal(a.SyncSettings, &acc.SyncSettings); err != nil {
			return nil, fmt.Errorf("failed to unmarshal sync settings: %w", err)
		}
	}

	// Parse ProxyConfig
	if len(a.ProxyConfig) > 0 {
		var proxyConfig models.ProxySettings
		if err := json.Unmarshal(a.ProxyConfig, &proxyConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal proxy config: %w", err)
		}
		acc.ProxyConfig = &proxyConfig
	}

	// Parse FairUsePolicy
	if len(a.FairUsePolicy) > 0 {
		var fairUsePolicy models.FairUsePolicy
		if err := json.Unmarshal(a.FairUsePolicy, &fairUsePolicy); err != nil {
			return nil, fmt.Errorf("failed to unmarshal fair use policy: %w", err)
		}
		acc.FairUsePolicy = &fairUsePolicy
	}

	return acc, nil
}

// fromAccount converts models.Account to accountDB
func (a *accountDB) fromAccount(acc *models.Account) error {
	a.ID = acc.ID
	a.Email = acc.Email
	a.AuthType = string(acc.AuthType)
	a.Status = string(acc.Status)
	a.IMAPHost = acc.IMAPConfig.Host
	a.IMAPPort = acc.IMAPConfig.Port
	a.IMAPEncryption = string(acc.IMAPConfig.Encryption)
	a.IMAPUsername = acc.IMAPConfig.Username
	a.IMAPPassword = acc.IMAPConfig.Password
	a.SMTPHost = acc.SMTPConfig.Host
	a.SMTPPort = acc.SMTPConfig.Port
	a.SMTPEncryption = string(acc.SMTPConfig.Encryption)
	a.SMTPUsername = acc.SMTPConfig.Username
	a.SMTPPassword = acc.SMTPConfig.Password
	a.ConnectionLimit = acc.ConnectionLimit
	a.CreatedAt = acc.CreatedAt
	a.UpdatedAt = acc.UpdatedAt
	a.LastSyncAt = acc.LastSyncAt

	// Marshal SyncSettings
	syncSettingsJSON, err := json.Marshal(acc.SyncSettings)
	if err != nil {
		return fmt.Errorf("failed to marshal sync settings: %w", err)
	}
	a.SyncSettings = syncSettingsJSON

	// Marshal ProxyConfig
	if acc.ProxyConfig != nil {
		proxyConfigJSON, err := json.Marshal(acc.ProxyConfig)
		if err != nil {
			return fmt.Errorf("failed to marshal proxy config: %w", err)
		}
		a.ProxyConfig = proxyConfigJSON
	} else {
		a.ProxyConfig = nil
	}

	// Marshal FairUsePolicy
	if acc.FairUsePolicy != nil {
		fairUsePolicyJSON, err := json.Marshal(acc.FairUsePolicy)
		if err != nil {
			return fmt.Errorf("failed to marshal fair use policy: %w", err)
		}
		a.FairUsePolicy = fairUsePolicyJSON
	} else {
		a.FairUsePolicy = nil
	}

	return nil
}

// SQLiteStore implements AccountStore using SQLite database with GORM
type SQLiteStore struct {
	db     *gorm.DB
	mu     sync.RWMutex
	closed bool

	// Statistics
	stats SQLiteStoreStats
}

// SQLiteStoreStats tracks store statistics
type SQLiteStoreStats struct {
	Creates int64 `json:"creates"`
	Updates int64 `json:"updates"`
	Deletes int64 `json:"deletes"`
	Gets    int64 `json:"gets"`
	Lists   int64 `json:"lists"`
	mu      sync.RWMutex
}

// SQLiteConfig holds SQLite configuration
type SQLiteConfig struct {
	Path           string `json:"path"`
	MaxConnections int    `json:"max_connections"`
	BusyTimeoutMs  int    `json:"busy_timeout_ms"`
}

// DefaultSQLiteConfig returns default SQLite configuration
func DefaultSQLiteConfig() SQLiteConfig {
	return SQLiteConfig{
		Path:           "./data/accounts.db",
		MaxConnections: 10,
		BusyTimeoutMs:  5000,
	}
}

// NewSQLiteStore creates a new SQLite store using GORM
func NewSQLiteStore(config SQLiteConfig) (*SQLiteStore, error) {
	if config.Path == "" {
		config = DefaultSQLiteConfig()
	}

	// Ensure directory exists
	dir := filepath.Dir(config.Path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Open database connection
	db, err := gorm.Open(sqlite.Open(config.Path), &gorm.Config{
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Get underlying SQL DB for configuration
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying DB: %w", err)
	}

	// Configure connection pool
	sqlDB.SetMaxOpenConns(config.MaxConnections)
	sqlDB.SetMaxIdleConns(config.MaxConnections)

	// Set busy timeout for SQLite
	if _, err := sqlDB.Exec(fmt.Sprintf("PRAGMA busy_timeout = %d", config.BusyTimeoutMs)); err != nil {
		return nil, fmt.Errorf("failed to set busy timeout: %w", err)
	}

	// Enable WAL mode for better concurrency
	if _, err := sqlDB.Exec("PRAGMA journal_mode = WAL"); err != nil {
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	store := &SQLiteStore{db: db}

	// Run automatic migrations
	if err := store.runMigrations(); err != nil {
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	return store, nil
}

// runMigrations runs GORM auto migrations
func (s *SQLiteStore) runMigrations() error {
	// GORM AutoMigrate handles schema creation and updates
	if err := s.db.AutoMigrate(&accountDB{}); err != nil {
		return fmt.Errorf("failed to auto migrate: %w", err)
	}

	// Create additional indexes for better query performance
	indexes := []struct {
		name  string
		field string
	}{
		{"idx_accounts_email", "email"},
		{"idx_accounts_status", "status"},
		{"idx_accounts_created_at", "created_at"},
	}

	for _, idx := range indexes {
		if !s.db.Migrator().HasIndex(&accountDB{}, idx.name) {
			s.db.Migrator().CreateIndex(&accountDB{}, idx.name)
		}
	}

	return nil
}

// GetByID retrieves an account by its ID
func (s *SQLiteStore) GetByID(ctx context.Context, id string) (*models.Account, error) {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return nil, ErrStoreUnavailable
	}
	s.mu.RUnlock()

	s.stats.mu.Lock()
	s.stats.Gets++
	s.stats.mu.Unlock()

	var accDB accountDB
	result := s.db.WithContext(ctx).First(&accDB, "id = ?", id)

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get account: %w", result.Error)
	}

	return accDB.toAccount()
}

// GetByEmail retrieves an account by email address
func (s *SQLiteStore) GetByEmail(ctx context.Context, email string) (*models.Account, error) {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return nil, ErrStoreUnavailable
	}
	s.mu.RUnlock()

	s.stats.mu.Lock()
	s.stats.Gets++
	s.stats.mu.Unlock()

	var accDB accountDB
	result := s.db.WithContext(ctx).Where("email = ?", email).First(&accDB)

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get account by email: %w", result.Error)
	}

	return accDB.toAccount()
}

// List retrieves all accounts with optional pagination
func (s *SQLiteStore) List(ctx context.Context, offset, limit int) ([]*models.Account, int, error) {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return nil, 0, ErrStoreUnavailable
	}
	s.mu.RUnlock()

	s.stats.mu.Lock()
	s.stats.Lists++
	s.stats.mu.Unlock()

	// Get total count
	var total int64
	if err := s.db.Model(&accountDB{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count accounts: %w", err)
	}

	if total == 0 {
		return []*models.Account{}, 0, nil
	}

	// Handle pagination
	if offset < 0 {
		offset = 0
	}
	if offset >= int(total) {
		return []*models.Account{}, int(total), nil
	}

	// Apply limit
	if limit <= 0 {
		limit = int(total)
	}

	var accDBs []accountDB
	result := s.db.WithContext(ctx).
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&accDBs)

	if result.Error != nil {
		return nil, int(total), fmt.Errorf("failed to list accounts: %w", result.Error)
	}

	accounts := make([]*models.Account, len(accDBs))
	for i, accDB := range accDBs {
		acc, err := accDB.toAccount()
		if err != nil {
			return nil, int(total), err
		}
		accounts[i] = acc
	}

	return accounts, int(total), nil
}

// Create stores a new account
func (s *SQLiteStore) Create(ctx context.Context, account *models.Account) error {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return ErrStoreUnavailable
	}
	s.mu.RUnlock()

	if account == nil {
		return ErrInvalidConfig
	}

	s.stats.mu.Lock()
	s.stats.Creates++
	s.stats.mu.Unlock()

	var accDB accountDB
	if err := accDB.fromAccount(account); err != nil {
		return err
	}

	result := s.db.WithContext(ctx).Create(&accDB)
	if result.Error != nil {
		if isUniqueConstraintError(result.Error) {
			return ErrAlreadyExists
		}
		return fmt.Errorf("failed to create account: %w", result.Error)
	}

	return nil
}

// Update modifies an existing account
func (s *SQLiteStore) Update(ctx context.Context, account *models.Account) error {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return ErrStoreUnavailable
	}
	s.mu.RUnlock()

	if account == nil {
		return ErrInvalidConfig
	}

	s.stats.mu.Lock()
	s.stats.Updates++
	s.stats.mu.Unlock()

	// First check if account exists
	var count int64
	if err := s.db.WithContext(ctx).Model(&accountDB{}).Where("id = ?", account.ID).Count(&count).Error; err != nil {
		return fmt.Errorf("failed to check account existence: %w", err)
	}

	if count == 0 {
		return ErrNotFound
	}

	var accDB accountDB
	if err := accDB.fromAccount(account); err != nil {
		return err
	}

	result := s.db.WithContext(ctx).Save(&accDB)
	if result.Error != nil {
		if isUniqueConstraintError(result.Error) {
			return ErrAlreadyExists
		}
		return fmt.Errorf("failed to update account: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

// Delete removes an account by ID
func (s *SQLiteStore) Delete(ctx context.Context, id string) error {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return ErrStoreUnavailable
	}
	s.mu.RUnlock()

	s.stats.mu.Lock()
	s.stats.Deletes++
	s.stats.mu.Unlock()

	result := s.db.WithContext(ctx).Delete(&accountDB{}, "id = ?", id)
	if result.Error != nil {
		return fmt.Errorf("failed to delete account: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

// Close releases resources
func (s *SQLiteStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	s.closed = true

	// Get underlying SQL DB and close
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}

	return sqlDB.Close()
}

// Health checks if the store is operational
func (s *SQLiteStore) Health(ctx context.Context) *HealthStatus {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return &HealthStatus{
			Status:    "unhealthy",
			Connected: false,
			Message:   "store is closed",
		}
	}
	s.mu.RUnlock()

	// Test database connection
	start := time.Now()
	var result int64
	err := s.db.WithContext(ctx).Raw("SELECT 1").Scan(&result).Error
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return &HealthStatus{
			Status:    "unhealthy",
			Connected: false,
			Message:   err.Error(),
			LatencyMs: latency,
		}
	}

	return &HealthStatus{
		Status:    "healthy",
		Connected: true,
		LatencyMs: latency,
	}
}

// GetStats returns store statistics
func (s *SQLiteStore) GetStats() SQLiteStoreStats {
	s.stats.mu.RLock()
	defer s.stats.mu.RUnlock()

	return SQLiteStoreStats{
		Creates: s.stats.Creates,
		Updates: s.stats.Updates,
		Deletes: s.stats.Deletes,
		Gets:    s.stats.Gets,
		Lists:   s.stats.Lists,
	}
}

// isUniqueConstraintError checks if error is a unique constraint violation
func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	// GORM wraps SQLite errors, check for unique constraint in error message
	errStr := err.Error()
	return contains(errStr, "UNIQUE constraint failed") || contains(errStr, "duplicate key")
}

// contains checks if string contains substring
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Ensure SQLiteStore implements AccountStore interface
var _ AccountStore = (*SQLiteStore)(nil)
