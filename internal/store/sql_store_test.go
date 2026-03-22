package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"webmail_engine/internal/config"
	"webmail_engine/internal/models"
)

// TestSQLStore_CreateAndGet tests basic create and get operations
func TestSQLStore_CreateAndGet(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLStore(config.SQLConfig{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Failed to create SQLite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	account := &models.Account{
		ID:        "acc_1",
		Email:     "test@example.com",
		AuthType:  models.AuthTypePassword,
		Status:    models.AccountStatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		IMAPConfig: models.ServerConfig{
			Host:       "imap.example.com",
			Port:       993,
			Encryption: models.EncryptionSSL,
			Username:   "test@example.com",
			Password:   "encrypted_password",
		},
		SMTPConfig: models.ServerConfig{
			Host:       "smtp.example.com",
			Port:       587,
			Encryption: models.EncryptionStartTLS,
			Username:   "test@example.com",
			Password:   "encrypted_password",
		},
		ConnectionLimit: 5,
	}

	// Create
	err = store.Create(ctx, account)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	// Get by ID
	retrieved, err := store.GetByID(ctx, "acc_1")
	if err != nil {
		t.Fatalf("Failed to get account by ID: %v", err)
	}

	if retrieved.Email != account.Email {
		t.Errorf("Expected email %s, got %s", account.Email, retrieved.Email)
	}

	// Get by Email
	retrievedByEmail, err := store.GetByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("Failed to get account by email: %v", err)
	}

	if retrievedByEmail.ID != account.ID {
		t.Errorf("Expected ID %s, got %s", account.ID, retrievedByEmail.ID)
	}
}

// TestSQLStore_DuplicateEmail tests duplicate email detection
func TestSQLStore_DuplicateEmail(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLStore(config.SQLConfig{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Failed to create SQLite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	account1 := &models.Account{
		ID:        "acc_1",
		Email:     "test@example.com",
		AuthType:  models.AuthTypePassword,
		Status:    models.AccountStatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		IMAPConfig: models.ServerConfig{
			Host:       "imap.example.com",
			Port:       993,
			Encryption: models.EncryptionSSL,
			Username:   "test",
			Password:   "pass",
		},
		SMTPConfig: models.ServerConfig{
			Host:       "smtp.example.com",
			Port:       587,
			Encryption: models.EncryptionStartTLS,
			Username:   "test",
			Password:   "pass",
		},
		ConnectionLimit: 5,
	}
	account2 := &models.Account{
		ID:        "acc_2",
		Email:     "test@example.com", // Same email
		AuthType:  models.AuthTypePassword,
		Status:    models.AccountStatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		IMAPConfig: models.ServerConfig{
			Host:       "imap2.example.com",
			Port:       993,
			Encryption: models.EncryptionSSL,
			Username:   "test2",
			Password:   "pass2",
		},
		SMTPConfig: models.ServerConfig{
			Host:       "smtp2.example.com",
			Port:       587,
			Encryption: models.EncryptionStartTLS,
			Username:   "test2",
			Password:   "pass2",
		},
		ConnectionLimit: 5,
	}

	// Create first account
	if err := store.Create(ctx, account1); err != nil {
		t.Fatalf("Failed to create first account: %v", err)
	}

	// Try to create second account with same email
	err = store.Create(ctx, account2)
	if err != ErrAlreadyExists {
		t.Errorf("Expected ErrAlreadyExists, got %v", err)
	}
}

// TestSQLStore_Update tests account update
func TestSQLStore_Update(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLStore(config.SQLConfig{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Failed to create SQLite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	account := &models.Account{
		ID:        "acc_1",
		Email:     "test@example.com",
		AuthType:  models.AuthTypePassword,
		Status:    models.AccountStatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		IMAPConfig: models.ServerConfig{
			Host:       "imap.example.com",
			Port:       993,
			Encryption: models.EncryptionSSL,
			Username:   "test",
			Password:   "pass",
		},
		SMTPConfig: models.ServerConfig{
			Host:       "smtp.example.com",
			Port:       587,
			Encryption: models.EncryptionStartTLS,
			Username:   "test",
			Password:   "pass",
		},
		ConnectionLimit: 5,
	}

	// Create
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	// Update
	account.ConnectionLimit = 10
	account.UpdatedAt = time.Now()
	if err := store.Update(ctx, account); err != nil {
		t.Fatalf("Failed to update account: %v", err)
	}

	// Verify update
	retrieved, err := store.GetByID(ctx, "acc_1")
	if err != nil {
		t.Fatalf("Failed to get account: %v", err)
	}

	if retrieved.ConnectionLimit != 10 {
		t.Errorf("Expected connection limit 10, got %d", retrieved.ConnectionLimit)
	}
}

// TestSQLStore_Delete tests account deletion
func TestSQLStore_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLStore(config.SQLConfig{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Failed to create SQLite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	account := &models.Account{
		ID:        "acc_1",
		Email:     "test@example.com",
		AuthType:  models.AuthTypePassword,
		Status:    models.AccountStatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		IMAPConfig: models.ServerConfig{
			Host:       "imap.example.com",
			Port:       993,
			Encryption: models.EncryptionSSL,
			Username:   "test",
			Password:   "pass",
		},
		SMTPConfig: models.ServerConfig{
			Host:       "smtp.example.com",
			Port:       587,
			Encryption: models.EncryptionStartTLS,
			Username:   "test",
			Password:   "pass",
		},
		ConnectionLimit: 5,
	}

	// Create
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	// Delete
	if err := store.Delete(ctx, "acc_1"); err != nil {
		t.Fatalf("Failed to delete account: %v", err)
	}

	// Verify deletion
	_, err = store.GetByID(ctx, "acc_1")
	if err != ErrNotFound {
		t.Errorf("Expected ErrNotFound, got %v", err)
	}
}

// TestSQLStore_List tests listing with pagination
func TestSQLStore_List(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLStore(config.SQLConfig{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Failed to create SQLite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create multiple accounts
	for i := 1; i <= 5; i++ {
		account := &models.Account{
			ID:        string(rune('a' + i)),
			Email:     string(rune('a'+i)) + "@example.com",
			AuthType:  models.AuthTypePassword,
			Status:    models.AccountStatusActive,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			IMAPConfig: models.ServerConfig{
				Host:       "imap.example.com",
				Port:       993,
				Encryption: models.EncryptionSSL,
				Username:   "test",
				Password:   "pass",
			},
			SMTPConfig: models.ServerConfig{
				Host:       "smtp.example.com",
				Port:       587,
				Encryption: models.EncryptionStartTLS,
				Username:   "test",
				Password:   "pass",
			},
			ConnectionLimit: 5,
		}
		if err := store.Create(ctx, account); err != nil {
			t.Fatalf("Failed to create account %d: %v", i, err)
		}
	}

	// List all
	accounts, total, err := store.List(ctx, 0, 100)
	if err != nil {
		t.Fatalf("Failed to list accounts: %v", err)
	}

	if total != 5 {
		t.Errorf("Expected total 5, got %d", total)
	}

	if len(accounts) != 5 {
		t.Errorf("Expected 5 accounts, got %d", len(accounts))
	}

	// List with pagination
	accounts, total, err = store.List(ctx, 2, 2)
	if err != nil {
		t.Fatalf("Failed to list accounts with pagination: %v", err)
	}

	if total != 5 {
		t.Errorf("Expected total 5, got %d", total)
	}

	if len(accounts) != 2 {
		t.Errorf("Expected 2 accounts with pagination, got %d", len(accounts))
	}
}

// TestSQLStore_Persistence tests that data persists after reopening
func TestSQLStore_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	ctx := context.Background()
	account := &models.Account{
		ID:        "acc_1",
		Email:     "test@example.com",
		AuthType:  models.AuthTypePassword,
		Status:    models.AccountStatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		IMAPConfig: models.ServerConfig{
			Host:       "imap.example.com",
			Port:       993,
			Encryption: models.EncryptionSSL,
			Username:   "test",
			Password:   "pass",
		},
		SMTPConfig: models.ServerConfig{
			Host:       "smtp.example.com",
			Port:       587,
			Encryption: models.EncryptionStartTLS,
			Username:   "test",
			Password:   "pass",
		},
		ConnectionLimit: 5,
	}

	// Create store and account
	store1, err := NewSQLStore(config.SQLConfig{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	if err := store1.Create(ctx, account); err != nil {
		store1.Close()
		t.Fatalf("Failed to create account: %v", err)
	}

	// Close first store
	store1.Close()

	// Reopen store
	store2, err := NewSQLStore(config.SQLConfig{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Failed to reopen store: %v", err)
	}
	defer store2.Close()

	// Verify account persisted
	retrieved, err := store2.GetByID(ctx, "acc_1")
	if err != nil {
		t.Fatalf("Failed to get persisted account: %v", err)
	}

	if retrieved.Email != account.Email {
		t.Errorf("Expected email %s, got %s", account.Email, retrieved.Email)
	}
}

// TestSQLStore_Health tests health check
func TestSQLStore_Health(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLStore(config.SQLConfig{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Failed to create SQLite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Check health
	health := store.Health(ctx)
	if health.Status != "healthy" {
		t.Errorf("Expected healthy status, got %s", health.Status)
	}

	if !health.Connected {
		t.Error("Expected store to be connected")
	}

	if health.LatencyMs < 0 {
		t.Error("Expected non-negative latency")
	}
}

// TestSQLStore_NotFound tests not found errors
func TestSQLStore_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLStore(config.SQLConfig{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Failed to create SQLite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Get non-existent account
	_, err = store.GetByID(ctx, "nonexistent")
	if err != ErrNotFound {
		t.Errorf("Expected ErrNotFound, got %v", err)
	}

	// Get non-existent email
	_, err = store.GetByEmail(ctx, "nonexistent@example.com")
	if err != ErrNotFound {
		t.Errorf("Expected ErrNotFound, got %v", err)
	}

	// Delete non-existent account
	err = store.Delete(ctx, "nonexistent")
	if err != ErrNotFound {
		t.Errorf("Expected ErrNotFound, got %v", err)
	}

	// Update non-existent account
	nonExistent := &models.Account{
		ID:        "nonexistent",
		Email:     "nonexistent@example.com",
		AuthType:  models.AuthTypePassword,
		Status:    models.AccountStatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		IMAPConfig: models.ServerConfig{
			Host:       "imap.example.com",
			Port:       993,
			Encryption: models.EncryptionSSL,
			Username:   "test",
			Password:   "pass",
		},
		SMTPConfig: models.ServerConfig{
			Host:       "smtp.example.com",
			Port:       587,
			Encryption: models.EncryptionStartTLS,
			Username:   "test",
			Password:   "pass",
		},
		ConnectionLimit: 5,
	}
	err = store.Update(ctx, nonExistent)
	if err != ErrNotFound {
		t.Errorf("Expected ErrNotFound, got %v", err)
	}
}

// TestSQLStore_JSONFields tests JSON field serialization
func TestSQLStore_JSONFields(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLStore(config.SQLConfig{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Failed to create SQLite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	fairUsePolicy := &models.FairUsePolicy{
		Enabled:         true,
		TokenBucketSize: 100,
		RefillRate:      10,
		OperationCosts: map[string]int{
			"FETCH":  1,
			"SEARCH": 5,
		},
	}

	proxyConfig := &models.ProxySettings{
		Enabled:        true,
		Type:           "socks5",
		Host:           "proxy.example.com",
		Port:           1080,
		Timeout:        30,
		FallbackDirect: true,
	}

	account := &models.Account{
		ID:        "acc_1",
		Email:     "test@example.com",
		AuthType:  models.AuthTypePassword,
		Status:    models.AccountStatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		IMAPConfig: models.ServerConfig{
			Host:       "imap.example.com",
			Port:       993,
			Encryption: models.EncryptionSSL,
			Username:   "test",
			Password:   "pass",
		},
		SMTPConfig: models.ServerConfig{
			Host:       "smtp.example.com",
			Port:       587,
			Encryption: models.EncryptionStartTLS,
			Username:   "test",
			Password:   "pass",
		},
		ConnectionLimit: 5,
		FairUsePolicy:   fairUsePolicy,
		ProxyConfig:     proxyConfig,
	}

	// Create
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	// Retrieve and verify JSON fields
	retrieved, err := store.GetByID(ctx, "acc_1")
	if err != nil {
		t.Fatalf("Failed to get account: %v", err)
	}

	if retrieved.FairUsePolicy == nil {
		t.Fatal("Expected FairUsePolicy to be set")
	}

	if !retrieved.FairUsePolicy.Enabled {
		t.Error("Expected FairUsePolicy.Enabled to be true")
	}

	if retrieved.FairUsePolicy.TokenBucketSize != 100 {
		t.Errorf("Expected TokenBucketSize 100, got %d", retrieved.FairUsePolicy.TokenBucketSize)
	}

	if retrieved.ProxyConfig == nil {
		t.Fatal("Expected ProxyConfig to be set")
	}

	if retrieved.ProxyConfig.Type != "socks5" {
		t.Errorf("Expected ProxyConfig.Type 'socks5', got %s", retrieved.ProxyConfig.Type)
	}
}
