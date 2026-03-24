package config

import (
	"os"
	"testing"
	"time"
)

func TestExpandEnvVars_StringFields(t *testing.T) {
	os.Setenv("TEST_STRING", "expanded_value")
	defer os.Unsetenv("TEST_STRING")

	cfg := &Config{
		Server: ServerConfig{
			Host: "${TEST_STRING}",
		},
	}

	ExpandEnvVars(cfg)

	if cfg.Server.Host != "expanded_value" {
		t.Errorf("expected 'expanded_value', got '%s'", cfg.Server.Host)
	}
}

func TestExpandEnvVars_WithDefault(t *testing.T) {
	os.Unsetenv("TEST_MISSING_VAR")

	cfg := &Config{
		Server: ServerConfig{
			Host: "${TEST_MISSING_VAR:-default_host}",
		},
	}

	ExpandEnvVars(cfg)

	if cfg.Server.Host != "default_host" {
		t.Errorf("expected 'default_host', got '%s'", cfg.Server.Host)
	}
}

func TestExpandEnvVars_WithDefault_VarExists(t *testing.T) {
	os.Setenv("TEST_EXISTING_VAR", "existing_value")
	defer os.Unsetenv("TEST_EXISTING_VAR")

	cfg := &Config{
		Server: ServerConfig{
			Host: "${TEST_EXISTING_VAR:-default_host}",
		},
	}

	ExpandEnvVars(cfg)

	if cfg.Server.Host != "existing_value" {
		t.Errorf("expected 'existing_value', got '%s'", cfg.Server.Host)
	}
}

func TestExpandEnvVars_NestedStruct(t *testing.T) {
	os.Setenv("TEST_DSN", "postgres://user:pass@localhost:5432/db")
	defer os.Unsetenv("TEST_DSN")

	cfg := &Config{
		Store: StoreConfig{
			Type: "sql",
			SQL: &SQLConfig{
				Driver: "postgres",
				DSN:    "${TEST_DSN}",
			},
		},
	}

	ExpandEnvVars(cfg)

	if cfg.Store.SQL.DSN != "postgres://user:pass@localhost:5432/db" {
		t.Errorf("expected postgres DSN, got '%s'", cfg.Store.SQL.DSN)
	}
}

func TestExpandEnvVars_Slice(t *testing.T) {
	os.Setenv("TEST_ORIGIN1", "https://example.com")
	os.Setenv("TEST_ORIGIN2", "https://api.example.com")
	defer os.Unsetenv("TEST_ORIGIN1")
	defer os.Unsetenv("TEST_ORIGIN2")

	cfg := &Config{
		Security: SecurityConfig{
			AllowedOrigins: []string{"${TEST_ORIGIN1}", "${TEST_ORIGIN2}"},
		},
	}

	ExpandEnvVars(cfg)

	if len(cfg.Security.AllowedOrigins) != 2 {
		t.Fatalf("expected 2 origins, got %d", len(cfg.Security.AllowedOrigins))
	}
	if cfg.Security.AllowedOrigins[0] != "https://example.com" {
		t.Errorf("expected first origin 'https://example.com', got '%s'", cfg.Security.AllowedOrigins[0])
	}
	if cfg.Security.AllowedOrigins[1] != "https://api.example.com" {
		t.Errorf("expected second origin 'https://api.example.com', got '%s'", cfg.Security.AllowedOrigins[1])
	}
}

func TestExpandEnvVars_Map(t *testing.T) {
	os.Setenv("TEST_COST", "10")
	defer os.Unsetenv("TEST_COST")

	// Note: maps with string keys/values work, but int values won't be expanded
	// This test verifies maps are traversed without errors
	cfg := &Config{
		Scheduler: SchedulerConfig{
			OperationCosts: map[string]int{
				"FETCH": 1,
				"TEST":  5,
			},
		},
	}

	ExpandEnvVars(cfg)

	// Map values are ints, so no expansion happens, but function should not panic
	if cfg.Scheduler.OperationCosts["FETCH"] != 1 {
		t.Errorf("expected FETCH cost to remain 1, got %d", cfg.Scheduler.OperationCosts["FETCH"])
	}
}

func TestExpandEnvVars_MultipleInString(t *testing.T) {
	os.Setenv("TEST_USER", "admin")
	os.Setenv("TEST_PASS", "secret")
	defer os.Unsetenv("TEST_USER")
	defer os.Unsetenv("TEST_PASS")

	cfg := &Config{
		Store: StoreConfig{
			SQL: &SQLConfig{
				DSN: "postgres://${TEST_USER}:${TEST_PASS}@localhost:5432/db",
			},
		},
	}

	ExpandEnvVars(cfg)

	expected := "postgres://admin:secret@localhost:5432/db"
	if cfg.Store.SQL.DSN != expected {
		t.Errorf("expected '%s', got '%s'", expected, cfg.Store.SQL.DSN)
	}
}

func TestExpandEnvVars_NoExpansion(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
	}

	ExpandEnvVars(cfg)

	if cfg.Server.Host != "localhost" {
		t.Errorf("expected 'localhost', got '%s'", cfg.Server.Host)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Server.Port)
	}
}

func TestExpandEnvVars_EmptyVar(t *testing.T) {
	os.Setenv("TEST_EMPTY", "")
	defer os.Unsetenv("TEST_EMPTY")

	cfg := &Config{
		Server: ServerConfig{
			Host: "${TEST_EMPTY}",
		},
	}

	ExpandEnvVars(cfg)

	if cfg.Server.Host != "" {
		t.Errorf("expected empty string, got '%s'", cfg.Server.Host)
	}
}

func TestExpandEnvVars_SecurityFields(t *testing.T) {
	os.Setenv("TEST_ENCRYPTION_KEY", "9b532b632f684efbd7b1a60ae4ca727a8fd2def25f704fdeb4364d6f944f9087")
	os.Setenv("TEST_WEBHOOK_SECRET", "webhook_secret_value")
	os.Setenv("TEST_SIGNED_URL_SECRET", "signed_url_secret_value")
	defer os.Unsetenv("TEST_ENCRYPTION_KEY")
	defer os.Unsetenv("TEST_WEBHOOK_SECRET")
	defer os.Unsetenv("TEST_SIGNED_URL_SECRET")

	cfg := &Config{
		Security: SecurityConfig{
			EncryptionKey:   "${TEST_ENCRYPTION_KEY}",
			WebhookSecret:   "${TEST_WEBHOOK_SECRET}",
			SignedURLSecret: "${TEST_SIGNED_URL_SECRET}",
		},
	}

	ExpandEnvVars(cfg)

	if cfg.Security.EncryptionKey != "9b532b632f684efbd7b1a60ae4ca727a8fd2def25f704fdeb4364d6f944f9087" {
		t.Errorf("encryption key not expanded")
	}
	if cfg.Security.WebhookSecret != "webhook_secret_value" {
		t.Errorf("webhook secret not expanded")
	}
	if cfg.Security.SignedURLSecret != "signed_url_secret_value" {
		t.Errorf("signed url secret not expanded")
	}
}

func TestExpandEnvVars_DurationFields(t *testing.T) {
	// Duration fields are time.Duration (int64), so they won't be expanded
	// This test ensures the function doesn't panic on duration fields
	cfg := &Config{
		Server: ServerConfig{
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
		},
	}

	ExpandEnvVars(cfg)

	if cfg.Server.ReadTimeout != 30*time.Second {
		t.Errorf("expected 30s read timeout, got %v", cfg.Server.ReadTimeout)
	}
}

func TestLoadFromFileWithEnvVars(t *testing.T) {
	// Create a temporary config file
	tmpFile := t.TempDir() + "/test_config.json"
	configContent := `{
		"store": {
			"type": "sql",
			"sql": {
				"driver": "postgres",
				"dsn": "${TEST_DB_DSN}",
				"max_connections": 10,
				"min_idle": 2
			}
		},
		"security": {
			"encryption_key": "${TEST_ENCRYPTION_KEY}"
		}
	}`

	err := os.WriteFile(tmpFile, []byte(configContent), 0600)
	if err != nil {
		t.Fatalf("failed to create temp config file: %v", err)
	}
	defer os.Remove(tmpFile)

	// Set environment variables
	os.Setenv("TEST_DB_DSN", "postgres://test:test@localhost:5432/testdb")
	os.Setenv("TEST_ENCRYPTION_KEY", "9b532b632f684efbd7b1a60ae4ca727a8fd2def25f704fdeb4364d6f944f9087")
	defer os.Unsetenv("TEST_DB_DSN")
	defer os.Unsetenv("TEST_ENCRYPTION_KEY")

	// Load config
	cfg, err := LoadFromFile(tmpFile)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	// Verify expansion
	if cfg.Store.SQL.DSN != "postgres://test:test@localhost:5432/testdb" {
		t.Errorf("DSN not expanded correctly, got: %s", cfg.Store.SQL.DSN)
	}
	if cfg.Security.EncryptionKey != "9b532b632f684efbd7b1a60ae4ca727a8fd2def25f704fdeb4364d6f944f9087" {
		t.Errorf("EncryptionKey not expanded correctly, got: %s", cfg.Security.EncryptionKey)
	}
}
