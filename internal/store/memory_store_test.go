package store

import (
	"context"
	"testing"
	"time"

	"webmail_engine/internal/models"
)

func TestMemoryStore_CreateAndGet(t *testing.T) {
	store := NewMemoryStore()
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
	err := store.Create(ctx, account)
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

func TestMemoryStore_DuplicateEmail(t *testing.T) {
	store := NewMemoryStore()
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
	err := store.Create(ctx, account2)
	if err != ErrAlreadyExists {
		t.Errorf("Expected ErrAlreadyExists, got %v", err)
	}
}

func TestMemoryStore_Update(t *testing.T) {
	store := NewMemoryStore()
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

func TestMemoryStore_Delete(t *testing.T) {
	store := NewMemoryStore()
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
	_, err := store.GetByID(ctx, "acc_1")
	if err != ErrNotFound {
		t.Errorf("Expected ErrNotFound, got %v", err)
	}
}

func TestMemoryStore_List(t *testing.T) {
	store := NewMemoryStore()
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

func TestMemoryStore_NotFound(t *testing.T) {
	store := NewMemoryStore()
	defer store.Close()

	ctx := context.Background()

	// Get non-existent account
	_, err := store.GetByID(ctx, "nonexistent")
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
}

func TestMemoryStore_Health(t *testing.T) {
	store := NewMemoryStore()
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

	// Close and check health
	store.Close()
	health = store.Health(ctx)
	if health.Status != "unhealthy" {
		t.Errorf("Expected unhealthy status after close, got %s", health.Status)
	}
}

func TestMemoryStore_Close(t *testing.T) {
	store := NewMemoryStore()

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

	// Create account
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	// Close store
	if err := store.Close(); err != nil {
		t.Fatalf("Failed to close store: %v", err)
	}

	// Try to operate on closed store
	_, err := store.GetByID(ctx, "acc_1")
	if err != ErrStoreUnavailable {
		t.Errorf("Expected ErrStoreUnavailable, got %v", err)
	}
}
