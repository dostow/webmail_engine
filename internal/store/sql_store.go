package store

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"webmail_engine/internal/config"
	"webmail_engine/internal/models"
)

// JSONBlob is a custom type for handling JSON columns in the database.
// It properly implements the sql.Scanner and driver.Valuer interfaces
// to handle the conversion between database TEXT/JSON columns and Go byte slices.
type JSONBlob []byte

// Scan implements the sql.Scanner interface.
// It converts the database value (string or []byte) into JSONBlob.
func (j *JSONBlob) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}

	switch v := value.(type) {
	case []byte:
		*j = JSONBlob(v)
	case string:
		*j = JSONBlob(v)
	default:
		return fmt.Errorf("failed to scan JSONBlob: unsupported type %T", value)
	}
	return nil
}

// Value implements the driver.Valuer interface.
// It converts JSONBlob into a driver.Value for database operations.
func (j JSONBlob) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return string(j), nil
}

// MarshalJSON implements json.Marshaler for JSONBlob.
func (j JSONBlob) MarshalJSON() ([]byte, error) {
	if j == nil {
		return []byte("null"), nil
	}
	return j, nil
}

// UnmarshalJSON implements json.Unmarshaler for JSONBlob.
func (j *JSONBlob) UnmarshalJSON(data []byte) error {
	if data == nil {
		*j = nil
		return nil
	}
	// Handle JSON null explicitly
	if string(data) == "null" {
		*j = nil
		return nil
	}
	*j = JSONBlob(data)
	return nil
}

// accountDB is the GORM model for account persistence
type accountDB struct {
	ID               string     `gorm:"primaryKey;type:text;not null"`
	Email            string     `gorm:"uniqueIndex;type:text;not null"`
	AuthType         string     `gorm:"type:text;not null"`
	Status           string     `gorm:"type:text;not null"`
	IMAPHost         string     `gorm:"column:imap_host;type:text;not null"`
	IMAPPort         int        `gorm:"column:imap_port;not null"`
	IMAPEncryption   string     `gorm:"column:imap_encryption;type:text;not null"`
	IMAPUsername     string     `gorm:"column:imap_username;type:text;not null"`
	IMAPPassword     string     `gorm:"column:imap_password;type:text;not null"`
	SMTPHost         string     `gorm:"column:smtp_host;type:text;not null"`
	SMTPPort         int        `gorm:"column:smtp_port;not null"`
	SMTPEncryption   string     `gorm:"column:smtp_encryption;type:text;not null"`
	SMTPUsername     string     `gorm:"column:smtp_username;type:text;not null"`
	SMTPPassword     string     `gorm:"column:smtp_password;type:text;not null"`
	ConnectionLimit  int        `gorm:"column:connection_limit;not null"`
	SyncSettings     JSONBlob   `gorm:"column:sync_settings;type:text;not null"`
	ProxyConfig      JSONBlob   `gorm:"column:proxy_config;type:text"`
	FairUsePolicy    JSONBlob   `gorm:"column:fair_use_policy;type:text"`
	ProcessorConfigs JSONBlob   `gorm:"column:processor_configs;type:text"`
	CreatedAt        time.Time  `gorm:"column:created_at;not null"`
	UpdatedAt        time.Time  `gorm:"column:updated_at;not null"`
	LastSyncAt       *time.Time `gorm:"column:last_sync_at"`
}

// TableName specifies the table name
func (accountDB) TableName() string {
	return "accounts"
}

// auditLogDB is the GORM model for audit log persistence
type auditLogDB struct {
	ID        int64     `gorm:"primaryKey;autoIncrement"`
	AccountID string    `gorm:"index;type:text"`
	Email     string    `gorm:"index;type:text"`
	Event     string    `gorm:"type:text;not null"`
	Details   string    `gorm:"type:text"`
	Timestamp time.Time `gorm:"index;not null"`
	IP        string    `gorm:"type:text"`
}

func (auditLogDB) TableName() string {
	return "audit_logs"
}

// folderSyncStateDB is the GORM model for folder sync state persistence
type folderSyncStateDB struct {
	AccountID     string    `gorm:"primaryKey;type:text;not null"`
	FolderName    string    `gorm:"primaryKey;type:text;not null"`
	UIDValidity   uint32    `gorm:"column:uid_validity;not null"`
	LastSyncedUID uint32    `gorm:"column:last_synced_uid;not null"`
	LastSyncTime  time.Time `gorm:"column:last_sync_time;not null"`
	MessageCount  uint32    `gorm:"column:message_count;not null"`
	IsInitialized bool      `gorm:"column:is_initialized;not null"`
	UpdatedAt     time.Time `gorm:"column:updated_at;not null"`
}

func (folderSyncStateDB) TableName() string {
	return "folder_sync_states"
}

// toFolderSyncState converts folderSyncStateDB to models.FolderSyncState
func (f *folderSyncStateDB) toFolderSyncState() *models.FolderSyncState {
	return &models.FolderSyncState{
		AccountID:     f.AccountID,
		FolderName:    f.FolderName,
		UIDValidity:   f.UIDValidity,
		LastSyncedUID: f.LastSyncedUID,
		LastSyncTime:  f.LastSyncTime,
		MessageCount:  f.MessageCount,
		IsInitialized: f.IsInitialized,
	}
}

// fromFolderSyncState converts models.FolderSyncState to folderSyncStateDB
func (f *folderSyncStateDB) fromFolderSyncState(state *models.FolderSyncState) {
	f.AccountID = state.AccountID
	f.FolderName = state.FolderName
	f.UIDValidity = state.UIDValidity
	f.LastSyncedUID = state.LastSyncedUID
	f.LastSyncTime = state.LastSyncTime
	f.MessageCount = state.MessageCount
	f.IsInitialized = state.IsInitialized
	f.UpdatedAt = time.Now()
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

	// Parse ProcessorConfigs
	if len(a.ProcessorConfigs) > 0 {
		if err := json.Unmarshal(a.ProcessorConfigs, &acc.ProcessorConfigs); err != nil {
			return nil, fmt.Errorf("failed to unmarshal processor configs: %w", err)
		}
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

	// Marshal ProcessorConfigs
	if len(acc.ProcessorConfigs) > 0 {
		processorConfigsJSON, err := json.Marshal(acc.ProcessorConfigs)
		if err != nil {
			return fmt.Errorf("failed to marshal processor configs: %w", err)
		}
		a.ProcessorConfigs = processorConfigsJSON
	} else {
		a.ProcessorConfigs = nil
	}

	return nil
}

// SQLStore implements AccountStore using SQLite database with GORM
type SQLStore struct {
	db     *gorm.DB
	mu     sync.RWMutex
	closed bool

	// Statistics
	stats SQLStoreStats
}

// SQLStoreStats tracks store statistics
type SQLStoreStats struct {
	Creates int64 `json:"creates"`
	Updates int64 `json:"updates"`
	Deletes int64 `json:"deletes"`
	Gets    int64 `json:"gets"`
	Lists   int64 `json:"lists"`
	mu      sync.RWMutex
}

// NewSQLStore creates a new SQL store using GORM
func NewSQLStore(cfg config.SQLConfig) (*SQLStore, error) {
	var db *gorm.DB
	var err error

	switch cfg.Driver {
	case "sqlite":
		// Ensure directory exists for sqlite
		if cfg.DSN != "" && cfg.DSN != ":memory:" {
			dir := filepath.Dir(cfg.DSN)
			if err := os.MkdirAll(dir, 0700); err != nil {
				return nil, fmt.Errorf("failed to create database directory: %w", err)
			}
		}

		db, err = gorm.Open(sqlite.Open(cfg.DSN), &gorm.Config{
			SkipDefaultTransaction: true,
		})
	case "postgres":
		db, err = gorm.Open(postgres.Open(cfg.DSN), &gorm.Config{
			SkipDefaultTransaction: true,
		})
	default:
		return nil, fmt.Errorf("unsupported database driver: %s", cfg.Driver)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Get underlying SQL DB for configuration
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying DB: %w", err)
	}

	// Configure connection pool
	sqlDB.SetMaxOpenConns(cfg.MaxConnections)
	if cfg.MinIdle > 0 {
		sqlDB.SetMaxIdleConns(cfg.MinIdle)
	} else {
		sqlDB.SetMaxIdleConns(cfg.MaxConnections)
	}

	if cfg.Driver == "sqlite" {
		// Set busy timeout for SQLite
		if _, err := sqlDB.Exec(fmt.Sprintf("PRAGMA busy_timeout = %d", cfg.BusyTimeoutMs)); err != nil {
			return nil, fmt.Errorf("failed to set busy timeout: %w", err)
		}

		// Enable WAL mode for better concurrency
		if _, err := sqlDB.Exec("PRAGMA journal_mode = WAL"); err != nil {
			return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
		}
	}

	store := &SQLStore{db: db}

	// Run automatic migrations
	if err := store.runMigrations(); err != nil {
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	return store, nil
}

// runMigrations runs GORM auto migrations
func (s *SQLStore) runMigrations() error {
	// GORM AutoMigrate handles schema creation and updates
	if err := s.db.AutoMigrate(&accountDB{}, &auditLogDB{}, &folderSyncStateDB{}); err != nil {
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
func (s *SQLStore) GetByID(ctx context.Context, id string) (*models.Account, error) {
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
func (s *SQLStore) GetByEmail(ctx context.Context, email string) (*models.Account, error) {
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
func (s *SQLStore) List(ctx context.Context, offset, limit int) ([]*models.Account, int, error) {
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
func (s *SQLStore) Create(ctx context.Context, account *models.Account) error {
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
func (s *SQLStore) Update(ctx context.Context, account *models.Account) error {
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
func (s *SQLStore) Delete(ctx context.Context, id string) error {
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
func (s *SQLStore) Close() error {
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
func (s *SQLStore) Health(ctx context.Context) *HealthStatus {
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

// CreateAuditLog stores a new audit log entry
func (s *SQLStore) CreateAuditLog(ctx context.Context, log *models.AuditLog) error {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return ErrStoreUnavailable
	}
	s.mu.RUnlock()

	dbLog := auditLogDB{
		AccountID: log.AccountID,
		Email:     log.Email,
		Event:     log.Event,
		Details:   log.Details,
		Timestamp: log.Timestamp,
		IP:        log.IP,
	}

	if dbLog.Timestamp.IsZero() {
		dbLog.Timestamp = time.Now()
	}

	result := s.db.WithContext(ctx).Create(&dbLog)
	if result.Error != nil {
		return fmt.Errorf("failed to create audit log: %w", result.Error)
	}
	log.ID = dbLog.ID
	return nil
}

// ListAuditLogs retrieves audit logs with optional pagination
func (s *SQLStore) ListAuditLogs(ctx context.Context, offset, limit int) ([]*models.AuditLog, int, error) {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return nil, 0, ErrStoreUnavailable
	}
	s.mu.RUnlock()

	var total int64
	if err := s.db.Model(&auditLogDB{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count audit logs: %w", err)
	}

	var dbLogs []auditLogDB
	query := s.db.WithContext(ctx).Order("timestamp DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}

	if err := query.Find(&dbLogs).Error; err != nil {
		return nil, int(total), fmt.Errorf("failed to list audit logs: %w", err)
	}

	logs := make([]*models.AuditLog, len(dbLogs))
	for i, dl := range dbLogs {
		logs[i] = &models.AuditLog{
			ID:        dl.ID,
			AccountID: dl.AccountID,
			Email:     dl.Email,
			Event:     dl.Event,
			Details:   dl.Details,
			Timestamp: dl.Timestamp,
			IP:        dl.IP,
		}
	}

	return logs, int(total), nil
}

// GetFolderSyncState retrieves sync state for a folder
func (s *SQLStore) GetFolderSyncState(ctx context.Context, accountID, folderName string) (*models.FolderSyncState, error) {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return nil, ErrStoreUnavailable
	}
	s.mu.RUnlock()

	var dbState folderSyncStateDB
	if err := s.db.WithContext(ctx).First(&dbState, "account_id = ? AND folder_name = ?", accountID, folderName).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get folder sync state: %w", err)
	}

	return dbState.toFolderSyncState(), nil
}

// UpsertFolderSyncState creates or updates folder sync state
func (s *SQLStore) UpsertFolderSyncState(ctx context.Context, state *models.FolderSyncState) error {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return ErrStoreUnavailable
	}
	s.mu.RUnlock()

	if state == nil {
		return ErrInvalidConfig
	}

	var dbState folderSyncStateDB
	dbState.fromFolderSyncState(state)

	// Use explicit upsert with ON CONFLICT clause for proper composite primary key handling
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "account_id"}, {Name: "folder_name"}},
		DoUpdates: clause.AssignmentColumns([]string{"uid_validity", "last_synced_uid", "last_sync_time", "message_count", "is_initialized", "updated_at"}),
	}).Create(&dbState).Error; err != nil {
		return fmt.Errorf("failed to upsert folder sync state: %w", err)
	}

	return nil
}

// DeleteFolderSyncState removes folder sync state
func (s *SQLStore) DeleteFolderSyncState(ctx context.Context, accountID, folderName string) error {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return ErrStoreUnavailable
	}
	s.mu.RUnlock()

	if err := s.db.WithContext(ctx).Delete(&folderSyncStateDB{}, "account_id = ? AND folder_name = ?", accountID, folderName).Error; err != nil {
		return fmt.Errorf("failed to delete folder sync state: %w", err)
	}

	return nil
}

// ListFolderSyncStates lists all folder sync states for an account
func (s *SQLStore) ListFolderSyncStates(ctx context.Context, accountID string) ([]*models.FolderSyncState, error) {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return nil, ErrStoreUnavailable
	}
	s.mu.RUnlock()

	var dbStates []folderSyncStateDB
	if err := s.db.WithContext(ctx).Where("account_id = ?", accountID).Find(&dbStates).Error; err != nil {
		return nil, fmt.Errorf("failed to list folder sync states: %w", err)
	}

	states := make([]*models.FolderSyncState, len(dbStates))
	for i, ds := range dbStates {
		states[i] = ds.toFolderSyncState()
	}

	return states, nil
}

// GetAccountProcessorConfigs retrieves processor configs for an account
func (s *SQLStore) GetAccountProcessorConfigs(ctx context.Context, accountID string) ([]models.AccountProcessorConfig, error) {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return nil, ErrStoreUnavailable
	}
	s.mu.RUnlock()

	var acc accountDB
	if err := s.db.WithContext(ctx).Where("id = ?", accountID).First(&acc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get account processor configs: %w", err)
	}

	if len(acc.ProcessorConfigs) == 0 {
		return []models.AccountProcessorConfig{}, nil
	}

	var configs []models.AccountProcessorConfig
	if err := json.Unmarshal(acc.ProcessorConfigs, &configs); err != nil {
		return nil, fmt.Errorf("failed to unmarshal processor configs: %w", err)
	}

	return configs, nil
}

// UpdateAccountProcessorConfigs updates processor configs for an account
func (s *SQLStore) UpdateAccountProcessorConfigs(ctx context.Context, accountID string, configs []models.AccountProcessorConfig) error {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return ErrStoreUnavailable
	}
	s.mu.RUnlock()

	configsJSON, err := json.Marshal(configs)
	if err != nil {
		return fmt.Errorf("failed to marshal processor configs: %w", err)
	}

	result := s.db.WithContext(ctx).Model(&accountDB{}).Where("id = ?", accountID).Update("processor_configs", configsJSON)
	if result.Error != nil {
		return fmt.Errorf("failed to update processor configs: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

// EnableAccountProcessor enables/disables a specific processor type
func (s *SQLStore) EnableAccountProcessor(ctx context.Context, accountID, processorType string, enabled bool) error {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return ErrStoreUnavailable
	}
	s.mu.RUnlock()

	// Get current configs
	configs, err := s.GetAccountProcessorConfigs(ctx, accountID)
	if err != nil {
		return err
	}

	// Find and update the processor config
	found := false
	for i := range configs {
		if configs[i].Type == processorType {
			configs[i].Enabled = enabled
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("processor type %s not found for account %s", processorType, accountID)
	}

	// Save updated configs
	return s.UpdateAccountProcessorConfigs(ctx, accountID, configs)
}

// GetStats returns store statistics
func (s *SQLStore) GetStats() SQLStoreStats {
	s.stats.mu.RLock()
	defer s.stats.mu.RUnlock()

	return SQLStoreStats{
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

// Ensure SQLStore implements AccountStore interface
var _ AccountStore = (*SQLStore)(nil)
