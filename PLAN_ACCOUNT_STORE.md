# Plan: Pluggable Account Store Interface

## Executive Summary

Add a repository/store interface layer to support multiple persistence backends for email accounts in the webmail_engine. This enables accounts to persist across restarts while maintaining the REST-to-IMAP gateway's performance and security requirements.

---

## Requirements Analysis

### Core Requirements

| Requirement | Rationale | Priority |
|-------------|-----------|----------|
| **Persistence across restarts** | Accounts shouldn't be lost on server restart | Critical |
| **Pluggable backends** | Support different deployment scenarios (dev, prod, embedded) | Critical |
| **Credential encryption at rest** | Passwords must remain encrypted in storage | Critical |
| **Fast account lookup by ID** | Every API call needs account credentials | Critical |
| **Lookup by email** | Prevent duplicate account registration | High |
| **List all accounts efficiently** | Admin/management operations | High |
| **Atomic updates** | Prevent partial writes during config changes | High |
| **Connection pooling** | Support high concurrent request volumes | High |
| **Minimal latency impact** | Target <5ms additional latency per operation | High |

### REST-to-IMAP Specific Considerations

1. **Read-Heavy Workload**: Accounts are read frequently (every IMAP operation), written rarely (only on add/update)
2. **Credential Access Pattern**: Full account with credentials needed only for connection pool, stripped for API responses
3. **Account State**: Status, last_sync_at, health scores change frequently and should be persisted
4. **Multi-Instance Readiness**: Future deployments may run multiple instances sharing same account store
5. **Graceful Degradation**: Service should continue operating if store becomes temporarily unavailable (use cached accounts)

---

## Design Principles

1. **Interface-First**: Define Go interface, multiple implementations
2. **Zero Downtime**: Existing in-memory behavior as fallback
3. **Encryption Separation**: Store layer handles encrypted bytes, doesn't manage keys
4. **Context-Aware**: All operations support context for timeouts/cancellation
5. **Error Transparency**: Clear error types for different failure modes

---

## Proposed Architecture

### Layer Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                    AccountService                           │
│  (Business logic, encryption, connection verification)      │
├─────────────────────────────────────────────────────────────┤
│                    AccountStore (interface)                 │
│  - GetByID(ctx, id)                                         │
│  - GetByEmail(ctx, email)                                   │
│  - List(ctx)                                                │
│  - Create(ctx, account)                                     │
│  - Update(ctx, account)                                     │
│  - Delete(ctx, id)                                          │
│  - Close()                                                  │
├─────────────────────────────────────────────────────────────┤
│                  Store Implementations                      │
│  ┌──────────────┐  ┌──────────────┐  ┌─────────────────┐   │
│  │   Memory     │  │   SQLite     │  │   PostgreSQL    │   │
│  │   (dev/test) │  │  (embedded)  │  │   (production)  │   │
│  └──────────────┘  └──────────────┘  └─────────────────┘   │
│  ┌──────────────┐  ┌──────────────┐                         │
│  │    Redis     │  │   DynamoDB   │                         │
│  │   (cache)    │  │   (cloud)    │                         │
│  └──────────────┘  └──────────────┘                         │
└─────────────────────────────────────────────────────────────┘
```

---

## Interface Definition

### AccountStore Interface

```go
// File: internal/store/account_store.go

package store

import (
    "context"
    "webmail_engine/internal/models"
)

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
}

// HealthStatus represents store health information
type HealthStatus struct {
    Status      string  // "healthy", "degraded", "unhealthy"
    LatencyMs   int64   // Average operation latency
    Connected   bool    // Connection status
    Message     string  // Additional context
}

// Standard errors
var (
    ErrNotFound      = errors.New("account not found")
    ErrAlreadyExists = errors.New("account already exists")
    ErrStoreUnavailable = errors.New("store unavailable")
)
```

---

## Implementation Plan

### Phase 1: Interface + Memory Store (Week 1)

**Deliverables:**
1. `internal/store/account_store.go` - Interface definition
2. `internal/store/memory_store.go` - In-memory implementation
3. `internal/store/errors.go` - Standard error types
4. Update `AccountService` to use store interface

**Memory Store Features:**
- Thread-safe map with RWMutex
- Email index for duplicate detection
- Used as default/fallback when no persistent store configured

**Changes to AccountService:**
```go
type AccountService struct {
    store    store.AccountStore  // New: replaces accounts map
    // ... existing fields
}

// Constructor change
func NewAccountService(
    store store.AccountStore,  // New parameter
    pool *pool.ConnectionPool,
    cache *cache.Cache,
    // ...
) (*AccountService, error)
```

### Phase 2: SQLite Store (Week 2)

**Deliverables:**
1. `internal/store/sqlite_store.go` - SQLite implementation
2. `internal/store/sqlite_migrations.go` - Schema migrations
3. Database configuration in `config.Config`

**Schema Design:**
```sql
CREATE TABLE accounts (
    id              TEXT PRIMARY KEY,
    email           TEXT UNIQUE NOT NULL,
    auth_type       TEXT NOT NULL,
    status          TEXT NOT NULL,
    
    -- IMAP config (encrypted fields stored as base64)
    imap_host       TEXT NOT NULL,
    imap_port       INTEGER NOT NULL,
    imap_encryption TEXT NOT NULL,
    imap_username   TEXT NOT NULL,
    imap_password   TEXT NOT NULL,  -- Encrypted
    
    -- SMTP config
    smtp_host       TEXT NOT NULL,
    smtp_port       INTEGER NOT NULL,
    smtp_encryption TEXT NOT NULL,
    smtp_username   TEXT NOT NULL,
    smtp_password   TEXT NOT NULL,  -- Encrypted
    
    -- Settings stored as JSON
    connection_limit INTEGER NOT NULL DEFAULT 5,
    sync_settings    TEXT NOT NULL DEFAULT '{}',
    proxy_config     TEXT DEFAULT NULL,
    fair_use_policy  TEXT DEFAULT NULL,
    
    -- Timestamps
    created_at       DATETIME NOT NULL,
    updated_at       DATETIME NOT NULL,
    last_sync_at     DATETIME DEFAULT NULL
);

CREATE INDEX idx_accounts_email ON accounts(email);
CREATE INDEX idx_accounts_status ON accounts(status);
```

**SQLite Configuration:**
```json
{
  "store": {
    "type": "sqlite",
    "sqlite": {
      "path": "./data/accounts.db",
      "max_connections": 10,
      "busy_timeout_ms": 5000
    }
  }
}
```

### Phase 3: PostgreSQL Store (Week 3)

**Deliverables:**
1. `internal/store/postgres_store.go` - PostgreSQL implementation
2. Connection pooling configuration
3. Migration scripts

**PostgreSQL Configuration:**
```json
{
  "store": {
    "type": "postgres",
    "postgres": {
      "host": "localhost",
      "port": 5432,
      "database": "webmail_engine",
      "user": "webmail",
      "password": "${DB_PASSWORD}",
      "ssl_mode": "require",
      "max_connections": 25,
      "min_idle": 5,
      "conn_timeout_ms": 10000
    }
  }
}
```

### Phase 4: Redis Store (Optional, Week 4)

**Deliverables:**
1. `internal/store/redis_store.go` - Redis implementation
2. Hash-based account storage

**Use Case:** Multi-instance deployments with shared Redis

**Redis Configuration:**
```json
{
  "store": {
    "type": "redis",
    "redis": {
      "host": "localhost",
      "port": 6379,
      "password": "${REDIS_PASSWORD}",
      "db": 1,
      "key_prefix": "webmail:account:"
    }
  }
}
```

---

## Configuration Changes

### New Config Structure

```go
// internal/config/config.go

type StoreConfig struct {
    Type       string            `json:"type"` // "memory", "sqlite", "postgres", "redis"
    SQLite     *SQLiteConfig     `json:"sqlite,omitempty"`
    Postgres   *PostgresConfig   `json:"postgres,omitempty"`
    Redis      *RedisStoreConfig `json:"redis,omitempty"`
}

type SQLiteConfig struct {
    Path           string `json:"path"`
    MaxConnections int    `json:"max_connections"`
    BusyTimeoutMs  int    `json:"busy_timeout_ms"`
}

type PostgresConfig struct {
    Host         string `json:"host"`
    Port         int    `json:"port"`
    Database     string `json:"database"`
    User         string `json:"user"`
    Password     string `json:"password"`
    SSLMode      string `json:"ssl_mode"`
    MaxConnections int  `json:"max_connections"`
    MinIdle      int    `json:"min_idle"`
    ConnTimeoutMs int   `json:"conn_timeout_ms"`
}

type RedisStoreConfig struct {
    Host      string `json:"host"`
    Port      int    `json:"port"`
    Password  string `json:"password"`
    DB        int    `json:"db"`
    KeyPrefix string `json:"key_prefix"`
}

type Config struct {
    // ... existing fields
    Store StoreConfig `json:"store"`
}
```

### Example Configuration File

```json
{
  "server": { /* ... */ },
  "redis": { /* ... */ },
  "store": {
    "type": "sqlite",
    "sqlite": {
      "path": "./data/accounts.db",
      "max_connections": 10,
      "busy_timeout_ms": 5000
    }
  },
  "security": { /* ... */ }
}
```

---

## AccountService Integration

### Updated Constructor

```go
func NewAccountService(
    store store.AccountStore,
    pool *pool.ConnectionPool,
    cache *cache.Cache,
    scheduler *scheduler.FairUseScheduler,
    syncMgr *SyncManager,
    config AccountServiceConfig,
) (*AccountService, error) {
    // ... existing code
}
```

### Updated Methods

```go
// AddAccount
func (s *AccountService) AddAccount(ctx context.Context, req models.AddAccountRequest) (*models.AddAccountResponse, error) {
    // Check for existing account by email
    existing, err := s.store.GetByEmail(ctx, req.Email)
    if err == nil {
        // Account exists, handle update logic
        return s.updateExistingAccount(ctx, existing, req)
    } else if err != store.ErrNotFound {
        return nil, err
    }
    
    // Create new account
    account := buildAccountFromRequest(req)
    
    // Encrypt passwords
    // Verify connection
    // ...
    
    // Store in persistent store
    if err := s.store.Create(ctx, account); err != nil {
        return nil, fmt.Errorf("failed to store account: %w", err)
    }
    
    // Cache for fast access
    s.cache.SetAccount(ctx, account)
    
    // ... rest of existing logic
}

// GetAccount
func (s *AccountService) GetAccount(ctx context.Context, accountID string) (*models.Account, error) {
    // Try cache first
    cached, _ := s.cache.GetAccount(ctx, accountID)
    if cached != nil {
        return stripSensitiveData(cached), nil
    }
    
    // Fall back to store
    account, err := s.store.GetByID(ctx, accountID)
    if err != nil {
        return nil, err
    }
    
    // Update cache
    s.cache.SetAccount(ctx, account)
    
    return stripSensitiveData(account), nil
}

// GetAccountWithCredentials (internal use)
func (s *AccountService) GetAccountWithCredentials(ctx context.Context, accountID string) (*models.Account, error) {
    // Try cache first
    cached, _ := s.cache.GetAccount(ctx, accountID)
    if cached != nil {
        return cached, nil
    }
    
    // Fall back to store
    account, err := s.store.GetByID(ctx, accountID)
    if err != nil {
        return nil, err
    }
    
    // Update cache
    s.cache.SetAccount(ctx, account)
    
    return account, nil
}

// ListAccounts
func (s *AccountService) ListAccounts(ctx context.Context) ([]*models.Account, error) {
    accounts, _, err := s.store.List(ctx, 0, 1000)
    if err != nil {
        return nil, err
    }
    
    // Strip sensitive data
    result := make([]*models.Account, len(accounts))
    for i, acc := range accounts {
        result[i] = stripSensitiveData(acc)
    }
    
    return result, nil
}

// UpdateAccount
func (s *AccountService) UpdateAccount(ctx context.Context, accountID string, updates map[string]interface{}) (*models.Account, error) {
    // Get existing account
    account, err := s.store.GetByID(ctx, accountID)
    if err != nil {
        return nil, err
    }
    
    // Apply updates
    // ...
    
    // Persist
    if err := s.store.Update(ctx, account); err != nil {
        return nil, err
    }
    
    // Update cache
    s.cache.SetAccount(ctx, account)
    
    return stripSensitiveData(account), nil
}

// DeleteAccount
func (s *AccountService) DeleteAccount(ctx context.Context, accountID string) error {
    // Delete from store
    if err := s.store.Delete(ctx, accountID); err != nil {
        return err
    }
    
    // Invalidate cache
    s.cache.InvalidateAccount(ctx, accountID)
    
    // Close connections, remove from scheduler, etc.
    // ...
    
    return nil
}
```

---

## Startup Integration

### Loading Accounts on Startup

```go
// cmd/main.go

// After creating accountService
log.Println("Loading accounts from store...")
accounts, total, err := accountService.LoadAllAccounts(context.Background())
if err != nil {
    log.Printf("Warning: Failed to load accounts from store: %v", err)
} else {
    log.Printf("Loaded %d accounts from store", total)
    
    // Restore active connections for each account
    for _, acc := range accounts {
        if acc.Status == models.AccountStatusActive {
            // Re-establish connections
            // Re-initialize fair-use scheduler
            // Restart sync if enabled
        }
    }
}
```

---

## Error Handling Strategy

### Error Types

```go
// internal/store/errors.go

package store

import "errors"

var (
    ErrNotFound         = errors.New("account not found")
    ErrAlreadyExists    = errors.New("account already exists")
    ErrStoreUnavailable = errors.New("store unavailable")
    ErrConnectionFailed = errors.New("failed to connect to store")
    ErrMigrationFailed  = errors.New("database migration failed")
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
```

### Fallback Behavior

| Scenario | Behavior |
|----------|----------|
| Store unavailable at startup | Log warning, use in-memory fallback |
| Store unavailable during operation | Return error, don't silently fail |
| Cache miss + store unavailable | Return error to client |
| Store write fails | Rollback, return error, keep old account |

---

## Migration Path

### Existing Deployments (No Persistence)

1. **Current State**: Accounts in memory only
2. **Upgrade Process**:
   - Deploy with `store.type = "memory"` (no behavior change)
   - Export accounts via API: `GET /v1/accounts`
   - Re-deploy with persistent store config
   - Re-import accounts via `POST /v1/accounts`

### Future: Import Tool

```bash
# Export accounts from running instance
curl http://localhost:8080/v1/accounts | jq '.accounts' > accounts.json

# Import to new instance with persistent store
./webmail_engine import --config config.json --input accounts.json
```

---

## Testing Strategy

### Unit Tests

```go
// internal/store/memory_store_test.go
func TestMemoryStore_CreateAndGet(t *testing.T) {
    store := NewMemoryStore()
    defer store.Close()
    
    account := &models.Account{ID: "acc_1", Email: "test@example.com"}
    
    err := store.Create(ctx, account)
    require.NoError(t, err)
    
    retrieved, err := store.GetByID(ctx, "acc_1")
    require.NoError(t, err)
    assert.Equal(t, account.Email, retrieved.Email)
}

func TestMemoryStore_DuplicateEmail(t *testing.T) {
    store := NewMemoryStore()
    
    account1 := &models.Account{ID: "acc_1", Email: "test@example.com"}
    account2 := &models.Account{ID: "acc_2", Email: "test@example.com"}
    
    require.NoError(t, store.Create(ctx, account1))
    assert.ErrorIs(t, store.Create(ctx, account2), store.ErrAlreadyExists)
}
```

### Integration Tests

```go
// internal/store/sqlite_store_integration_test.go
func TestSQLiteStore_Persistence(t *testing.T) {
    tmpDir := t.TempDir()
    dbPath := filepath.Join(tmpDir, "test.db")
    
    store, err := NewSQLiteStore(SQLiteConfig{Path: dbPath})
    require.NoError(t, err)
    defer store.Close()
    
    // Create account
    account := &models.Account{ID: "acc_1", Email: "test@example.com"}
    require.NoError(t, store.Create(ctx, account))
    
    // Close and reopen
    store.Close()
    
    store2, err := NewSQLiteStore(SQLiteConfig{Path: dbPath})
    require.NoError(t, err)
    defer store2.Close()
    
    // Verify account persisted
    retrieved, err := store2.GetByID(ctx, "acc_1")
    require.NoError(t, err)
    assert.Equal(t, "test@example.com", retrieved.Email)
}
```

### Benchmark Tests

```go
func BenchmarkMemoryStore_GetByID(b *testing.B) {
    store := NewMemoryStore()
    // Setup with 1000 accounts
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        store.GetByID(ctx, "acc_500")
    }
}

func BenchmarkSQLiteStore_GetByID(b *testing.B) {
    // Similar benchmark for SQLite
}
```

---

## Performance Targets

| Operation | Target Latency | Notes |
|-----------|---------------|-------|
| GetByID (cached) | <1ms | Redis/memory cache hit |
| GetByID (store) | <5ms | SQLite/Postgres direct |
| Create | <50ms | Includes encryption |
| Update | <20ms | Partial update |
| List (100 accounts) | <100ms | With pagination |
| Delete | <10ms | Includes cache invalidation |

---

## Security Considerations

### Encryption at Rest

1. **Passwords**: Already encrypted by `AccountService` before store receives them
2. **Store Layer**: Receives and stores already-encrypted strings
3. **Key Management**: Encryption keys remain in `AccountService`, never passed to store

### Access Control

1. **File Permissions**: SQLite database file should be `0600`
2. **Database Users**: PostgreSQL user with minimal required privileges
3. **Network Security**: PostgreSQL connections over TLS

### Audit Logging

```go
// Optional: Add audit logging wrapper
type AuditingStore struct {
    store  AccountStore
    logger *log.Logger
}

func (s *AuditingStore) Create(ctx context.Context, account *models.Account) error {
    s.logger.Printf("CREATE account=%s email=%s", account.ID, account.Email)
    return s.store.Create(ctx, account)
}
```

---

## Rollback Plan

If issues arise after deployment:

1. **Revert to Memory Store**: Change config `store.type = "memory"`
2. **Data Preservation**: SQLite/Postgres data remains intact
3. **No Data Loss**: Accounts can be re-imported later

---

## Success Criteria

### Functional
- [ ] Accounts persist across server restarts
- [ ] All existing API endpoints work unchanged
- [ ] Duplicate email detection works correctly
- [ ] Account updates are atomic

### Performance
- [ ] GetByID latency <5ms (p95)
- [ ] No regression in account creation time
- [ ] Memory usage stable under load

### Quality
- [ ] Unit test coverage >80%
- [ ] Integration tests pass for all store types
- [ ] Documentation updated

---

## Implementation Checklist

### Phase 1: Interface + Memory
- [ ] Create `internal/store/` package
- [ ] Define `AccountStore` interface
- [ ] Implement `MemoryStore`
- [ ] Define standard errors
- [ ] Update `AccountService` to use store
- [ ] Add unit tests

### Phase 2: SQLite
- [ ] Add `modernc.org/sqlite` dependency
- [ ] Implement `SQLiteStore`
- [ ] Create migration system
- [ ] Add connection pooling
- [ ] Integration tests
- [ ] Benchmark tests

### Phase 3: PostgreSQL
- [ ] Add `pgx` dependency
- [ ] Implement `PostgresStore`
- [ ] Connection pool configuration
- [ ] SSL/TLS support
- [ ] Integration tests (requires running Postgres)

### Phase 4: Integration
- [ ] Update `cmd/main.go` for store initialization
- [ ] Add startup account loading
- [ ] Update configuration documentation
- [ ] Create migration guide
- [ ] End-to-end testing

### Phase 5: Optional Enhancements
- [ ] Redis store implementation
- [ ] Import/export CLI tool
- [ ] Audit logging wrapper
- [ ] Metrics/monitoring integration

---

## Dependencies

| Package | Version | Purpose |
|---------|---------|---------|
| `modernc.org/sqlite` | v1.x | SQLite driver (pure Go) |
| `github.com/jackc/pgx/v5` | v5.x | PostgreSQL driver |
| `github.com/redis/go-redis/v9` | v9.x | Redis client (already used for cache) |

---

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Store latency affects API response | High | Cache layer, connection pooling |
| Migration failures corrupt data | High | Transactional migrations, backups |
| Encryption key loss | Critical | Key management documentation, backup procedures |
| Store becomes bottleneck | Medium | Connection pooling, read replicas (future) |
| Breaking changes to account schema | Medium | Versioned migrations, rollback support |

---

## Future Enhancements

1. **Read Replicas**: For high-scale deployments with many instances
2. **Sharding**: By account ID for horizontal scaling
3. **Event Sourcing**: Track account configuration changes over time
4. **Multi-Tenancy**: Namespace accounts by organization
5. **Account Import/Export**: CLI tool for migration between instances

---

## Appendix: File Structure

```
internal/store/
├── account_store.go         # Interface definition
├── errors.go                # Standard errors
├── memory_store.go          # In-memory implementation
├── memory_store_test.go     # Memory store tests
├── sqlite_store.go          # SQLite implementation
├── sqlite_store_test.go     # SQLite unit tests
├── sqlite_migrations.go     # Schema migrations
├── postgres_store.go        # PostgreSQL implementation
├── postgres_store_test.go   # PostgreSQL unit tests
├── redis_store.go           # Redis implementation (optional)
└── test_helpers.go          # Test utilities
```

---

## Timeline Summary

| Phase | Duration | Deliverables |
|-------|----------|--------------|
| 1. Interface + Memory | Week 1 | Store interface, memory implementation, AccountService integration |
| 2. SQLite | Week 2 | SQLite store, migrations, integration tests |
| 3. PostgreSQL | Week 3 | PostgreSQL store, connection pooling, SSL support |
| 4. Integration | Week 4 | Startup loading, config updates, documentation |
| 5. Optional | Week 5+ | Redis store, CLI tools, enhancements |

**Total Estimated Duration**: 4-5 weeks
