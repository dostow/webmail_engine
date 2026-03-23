package store

import (
	"context"
	"database/sql/driver"
	"encoding/json"
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

// TestJSONBlob_ScanString tests scanning a string value into JSONBlob
func TestJSONBlob_ScanString(t *testing.T) {
	var jb JSONBlob
	jsonStr := `{"key": "value", "number": 42}`

	err := jb.Scan(jsonStr)
	if err != nil {
		t.Fatalf("Failed to scan string into JSONBlob: %v", err)
	}

	if string(jb) != jsonStr {
		t.Errorf("Expected %s, got %s", jsonStr, string(jb))
	}
}

// TestJSONBlob_ScanBytes tests scanning a []byte value into JSONBlob
func TestJSONBlob_ScanBytes(t *testing.T) {
	var jb JSONBlob
	jsonBytes := []byte(`{"key": "value", "number": 42}`)

	err := jb.Scan(jsonBytes)
	if err != nil {
		t.Fatalf("Failed to scan []byte into JSONBlob: %v", err)
	}

	if string(jb) != string(jsonBytes) {
		t.Errorf("Expected %s, got %s", string(jsonBytes), string(jb))
	}
}

// TestJSONBlob_ScanNil tests scanning nil into JSONBlob
func TestJSONBlob_ScanNil(t *testing.T) {
	var jb JSONBlob

	err := jb.Scan(nil)
	if err != nil {
		t.Fatalf("Failed to scan nil into JSONBlob: %v", err)
	}

	if jb != nil {
		t.Errorf("Expected nil JSONBlob, got %v", jb)
	}
}

// TestJSONBlob_ScanUnsupportedType tests scanning an unsupported type
func TestJSONBlob_ScanUnsupportedType(t *testing.T) {
	var jb JSONBlob

	err := jb.Scan(12345)
	if err == nil {
		t.Fatal("Expected error when scanning int, got nil")
	}

	expectedErr := "unsupported type"
	if !contains(err.Error(), expectedErr) {
		t.Errorf("Expected error to contain %q, got %q", expectedErr, err.Error())
	}
}

// TestJSONBlob_Value tests the Value() method
func TestJSONBlob_Value(t *testing.T) {
	jsonStr := `{"key": "value"}`
	jb := JSONBlob(jsonStr)

	val, err := jb.Value()
	if err != nil {
		t.Fatalf("Failed to get value from JSONBlob: %v", err)
	}

	strVal, ok := val.(string)
	if !ok {
		t.Fatalf("Expected string value, got %T", val)
	}

	if strVal != jsonStr {
		t.Errorf("Expected %s, got %s", jsonStr, strVal)
	}
}

// TestJSONBlob_ValueNil tests Value() with nil JSONBlob
func TestJSONBlob_ValueNil(t *testing.T) {
	var jb JSONBlob

	val, err := jb.Value()
	if err != nil {
		t.Fatalf("Failed to get value from nil JSONBlob: %v", err)
	}

	if val != nil {
		t.Errorf("Expected nil value, got %v", val)
	}
}

// TestJSONBlob_MarshalJSON tests JSON marshaling
func TestJSONBlob_MarshalJSON(t *testing.T) {
	jsonStr := `{"key": "value"}`
	jb := JSONBlob(jsonStr)

	data, err := json.Marshal(jb)
	if err != nil {
		t.Fatalf("Failed to marshal JSONBlob: %v", err)
	}

	// Compare parsed JSON to handle whitespace differences
	var marshaled, original interface{}
	if err := json.Unmarshal(data, &marshaled); err != nil {
		t.Fatalf("Failed to unmarshal marshaled data: %v", err)
	}
	if err := json.Unmarshal([]byte(jsonStr), &original); err != nil {
		t.Fatalf("Failed to unmarshal original data: %v", err)
	}

	if !jsonEqual(marshaled, original) {
		t.Errorf("Expected %s, got %s", jsonStr, string(data))
	}
}

// TestJSONBlob_MarshalJSONNil tests JSON marshaling with nil
func TestJSONBlob_MarshalJSONNil(t *testing.T) {
	var jb JSONBlob

	data, err := json.Marshal(jb)
	if err != nil {
		t.Fatalf("Failed to marshal nil JSONBlob: %v", err)
	}

	if string(data) != "null" {
		t.Errorf("Expected null, got %s", string(data))
	}
}

// TestJSONBlob_UnmarshalJSON tests JSON unmarshaling
func TestJSONBlob_UnmarshalJSON(t *testing.T) {
	jsonStr := `{"key": "value"}`
	var jb JSONBlob

	err := json.Unmarshal([]byte(jsonStr), &jb)
	if err != nil {
		t.Fatalf("Failed to unmarshal JSONBlob: %v", err)
	}

	if string(jb) != jsonStr {
		t.Errorf("Expected %s, got %s", jsonStr, string(jb))
	}
}

// TestJSONBlob_UnmarshalJSONNil tests JSON unmarshaling with JSON null
func TestJSONBlob_UnmarshalJSONNil(t *testing.T) {
	var jb JSONBlob = []byte(`{"existing": "data"}`)

	// Unmarshal JSON null (not Go nil, but JSON "null")
	err := json.Unmarshal([]byte("null"), &jb)
	if err != nil {
		t.Fatalf("Failed to unmarshal JSON null into JSONBlob: %v", err)
	}

	if jb != nil {
		t.Errorf("Expected nil JSONBlob, got %v", jb)
	}
}

// TestJSONBlob_ImplementsInterfaces verifies JSONBlob implements required interfaces
func TestJSONBlob_ImplementsInterfaces(t *testing.T) {
	var jb JSONBlob

	// Check sql.Scanner interface
	var _ interface {
		Scan(src interface{}) error
	} = &jb

	// Check driver.Valuer interface
	var _ interface {
		Value() (driver.Value, error)
	} = jb

	// Check json.Marshaler interface
	var _ interface {
		MarshalJSON() ([]byte, error)
	} = jb

	// Check json.Unmarshaler interface
	var _ interface {
		UnmarshalJSON([]byte) error
	} = &jb
}

// TestSQLStore_AllJSONFields tests all JSON fields in account persistence
func TestSQLStore_AllJSONFields(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLStore(config.SQLConfig{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Failed to create SQLite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create account with all JSON fields populated
	account := &models.Account{
		ID:        "acc_json_test",
		Email:     "jsontest@example.com",
		AuthType:  models.AuthTypePassword,
		Status:    models.AccountStatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		IMAPConfig: models.ServerConfig{
			Host:       "imap.example.com",
			Port:       993,
			Encryption: models.EncryptionSSL,
			Username:   "jsontest",
			Password:   "pass",
		},
		SMTPConfig: models.ServerConfig{
			Host:       "smtp.example.com",
			Port:       587,
			Encryption: models.EncryptionStartTLS,
			Username:   "jsontest",
			Password:   "pass",
		},
		ConnectionLimit: 5,
		SyncSettings: models.SyncSettings{
			HistoricalScope:            30,
			AutoSync:                   true,
			SyncInterval:               300,
			IncludeSpam:                false,
			IncludeTrash:               true,
			MaxMessageSize:             10485760,
			AttachmentHandling:         "inline",
			FetchBody:                  true,
			EnableLinkExtraction:       true,
			EnableAttachmentProcessing: true,
		},
		ProxyConfig: &models.ProxySettings{
			Enabled:        true,
			Type:           "socks5",
			Host:           "proxy.example.com",
			Port:           1080,
			Username:       "proxyuser",
			Timeout:        30,
			FallbackDirect: true,
		},
		FairUsePolicy: &models.FairUsePolicy{
			Enabled:         true,
			TokenBucketSize: 100,
			RefillRate:      10,
			OperationCosts: map[string]int{
				"FETCH":  1,
				"SEARCH": 5,
				"SEND":   10,
			},
			PriorityLevels: map[string]int{
				"high":   50,
				"normal": 30,
				"low":    10,
			},
			ProviderLimits: models.ProviderLimits{
				MaxConnections:      5,
				MaxRequestsPerHour:  100,
				MaxRecipientsPerDay: 500,
				MaxMessageSize:      25000000,
			},
		},
		ProcessorConfigs: []models.AccountProcessorConfig{
			{
				Type:     "llm_processor",
				Enabled:  true,
				Priority: 1,
				Meta:     json.RawMessage(`{"model": "gpt-4", "temperature": 0.7}`),
			},
			{
				Type:     "link_tracker",
				Enabled:  false,
				Priority: 2,
				Meta:     json.RawMessage(`{"track_clicks": true, "domain": "track.example.com"}`),
			},
		},
	}

	// Create account
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Failed to create account with JSON fields: %v", err)
	}

	// Retrieve and verify all JSON fields
	retrieved, err := store.GetByID(ctx, "acc_json_test")
	if err != nil {
		t.Fatalf("Failed to get account: %v", err)
	}

	// Verify SyncSettings
	if retrieved.SyncSettings.HistoricalScope != 30 {
		t.Errorf("Expected SyncSettings.HistoricalScope 30, got %d", retrieved.SyncSettings.HistoricalScope)
	}
	if !retrieved.SyncSettings.AutoSync {
		t.Error("Expected SyncSettings.AutoSync to be true")
	}
	if retrieved.SyncSettings.MaxMessageSize != 10485760 {
		t.Errorf("Expected SyncSettings.MaxMessageSize 10485760, got %d", retrieved.SyncSettings.MaxMessageSize)
	}

	// Verify ProxyConfig
	if retrieved.ProxyConfig == nil {
		t.Fatal("Expected ProxyConfig to be set")
	}
	if retrieved.ProxyConfig.Type != "socks5" {
		t.Errorf("Expected ProxyConfig.Type 'socks5', got %s", retrieved.ProxyConfig.Type)
	}
	if retrieved.ProxyConfig.Port != 1080 {
		t.Errorf("Expected ProxyConfig.Port 1080, got %d", retrieved.ProxyConfig.Port)
	}

	// Verify FairUsePolicy
	if retrieved.FairUsePolicy == nil {
		t.Fatal("Expected FairUsePolicy to be set")
	}
	if !retrieved.FairUsePolicy.Enabled {
		t.Error("Expected FairUsePolicy.Enabled to be true")
	}
	if retrieved.FairUsePolicy.TokenBucketSize != 100 {
		t.Errorf("Expected FairUsePolicy.TokenBucketSize 100, got %d", retrieved.FairUsePolicy.TokenBucketSize)
	}
	if retrieved.FairUsePolicy.OperationCosts["FETCH"] != 1 {
		t.Errorf("Expected FairUsePolicy.OperationCosts[FETCH] 1, got %d", retrieved.FairUsePolicy.OperationCosts["FETCH"])
	}
	if retrieved.FairUsePolicy.ProviderLimits.MaxConnections != 5 {
		t.Errorf("Expected ProviderLimits.MaxConnections 5, got %d", retrieved.FairUsePolicy.ProviderLimits.MaxConnections)
	}

	// Verify ProcessorConfigs
	if len(retrieved.ProcessorConfigs) != 2 {
		t.Fatalf("Expected 2 ProcessorConfigs, got %d", len(retrieved.ProcessorConfigs))
	}
	if retrieved.ProcessorConfigs[0].Type != "llm_processor" {
		t.Errorf("Expected first processor type 'llm_processor', got %s", retrieved.ProcessorConfigs[0].Type)
	}
	if !retrieved.ProcessorConfigs[0].Enabled {
		t.Error("Expected first processor to be enabled")
	}
	if retrieved.ProcessorConfigs[1].Type != "link_tracker" {
		t.Errorf("Expected second processor type 'link_tracker', got %s", retrieved.ProcessorConfigs[1].Type)
	}
	if retrieved.ProcessorConfigs[1].Enabled {
		t.Error("Expected second processor to be disabled")
	}
}

// TestSQLStore_ListWithJSONFields tests listing accounts with JSON fields
func TestSQLStore_ListWithJSONFields(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLStore(config.SQLConfig{Driver: "sqlite", DSN: dbPath})
	if err != nil {
		t.Fatalf("Failed to create SQLite store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create accounts with JSON fields
	for i := 1; i <= 3; i++ {
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
			SyncSettings: models.SyncSettings{
				HistoricalScope: i * 10,
				AutoSync:        true,
				SyncInterval:    300,
			},
		}
		if err := store.Create(ctx, account); err != nil {
			t.Fatalf("Failed to create account %d: %v", i, err)
		}
	}

	// List all accounts
	accounts, total, err := store.List(ctx, 0, 100)
	if err != nil {
		t.Fatalf("Failed to list accounts: %v", err)
	}

	if total != 3 {
		t.Errorf("Expected total 3, got %d", total)
	}

	// Verify JSON fields are properly loaded
	for i, acc := range accounts {
		if acc.SyncSettings.HistoricalScope == 0 {
			t.Errorf("Account %d: Expected non-zero HistoricalScope", i)
		}
	}
}

// jsonEqual compares two JSON values for equality
func jsonEqual(a, b interface{}) bool {
	return deepEqualJSON(a, b)
}

func deepEqualJSON(a, b interface{}) bool {
	switch aVal := a.(type) {
	case map[string]interface{}:
		bVal, ok := b.(map[string]interface{})
		if !ok || len(aVal) != len(bVal) {
			return false
		}
		for k, v := range aVal {
			if bv, exists := bVal[k]; !exists || !deepEqualJSON(v, bv) {
				return false
			}
		}
		return true
	case []interface{}:
		bVal, ok := b.([]interface{})
		if !ok || len(aVal) != len(bVal) {
			return false
		}
		for i := range aVal {
			if !deepEqualJSON(aVal[i], bVal[i]) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}
