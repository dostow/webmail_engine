package store

import (
	"context"
	"sync"

	"webmail_engine/internal/models"
)

// MemoryStore implements AccountStore using in-memory storage
// Suitable for development, testing, or ephemeral deployments
type MemoryStore struct {
	mu              sync.RWMutex
	accounts        map[string]*models.Account
	emailIndex      map[string]string // email -> account ID
	closed          bool
	auditLogs       []*models.AuditLog
	folderSyncState map[string]*models.FolderSyncState // key: accountID:folderName

	// Statistics for monitoring
	stats MemoryStoreStats
}

// MemoryStoreStats tracks store statistics
type MemoryStoreStats struct {
	Creates int64 `json:"creates"`
	Updates int64 `json:"updates"`
	Deletes int64 `json:"deletes"`
	Gets    int64 `json:"gets"`
	Lists   int64 `json:"lists"`
	mu      sync.RWMutex
}

// NewMemoryStore creates a new in-memory store
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		accounts:        make(map[string]*models.Account),
		emailIndex:      make(map[string]string),
		folderSyncState: make(map[string]*models.FolderSyncState),
	}
}

// GetByID retrieves an account by its ID
func (s *MemoryStore) GetByID(ctx context.Context, id string) (*models.Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreUnavailable
	}

	account, exists := s.accounts[id]
	if !exists {
		return nil, ErrNotFound
	}

	s.stats.mu.Lock()
	s.stats.Gets++
	s.stats.mu.Unlock()

	// Return a copy to prevent external modification
	return copyAccount(account), nil
}

// GetByEmail retrieves an account by email address
func (s *MemoryStore) GetByEmail(ctx context.Context, email string) (*models.Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreUnavailable
	}

	id, exists := s.emailIndex[email]
	if !exists {
		return nil, ErrNotFound
	}

	account, exists := s.accounts[id]
	if !exists {
		return nil, ErrNotFound
	}

	s.stats.mu.Lock()
	s.stats.Gets++
	s.stats.mu.Unlock()

	// Return a copy
	return copyAccount(account), nil
}

// List retrieves all accounts with optional pagination
func (s *MemoryStore) List(ctx context.Context, offset, limit int) ([]*models.Account, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, 0, ErrStoreUnavailable
	}

	s.stats.mu.Lock()
	s.stats.Lists++
	s.stats.mu.Unlock()

	total := len(s.accounts)
	if total == 0 {
		return []*models.Account{}, 0, nil
	}

	// Handle pagination
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return []*models.Account{}, total, nil
	}

	// Convert map to slice
	allAccounts := make([]*models.Account, 0, total)
	for _, acc := range s.accounts {
		allAccounts = append(allAccounts, copyAccount(acc))
	}

	// Apply pagination
	end := offset + limit
	if limit <= 0 || end > total {
		end = total
	}

	return allAccounts[offset:end], total, nil
}

// Create stores a new account
func (s *MemoryStore) Create(ctx context.Context, account *models.Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStoreUnavailable
	}

	if account == nil {
		return ErrInvalidConfig
	}

	// Check for duplicate email
	if _, exists := s.emailIndex[account.Email]; exists {
		return ErrAlreadyExists
	}

	// Store account
	s.accounts[account.ID] = copyAccount(account)
	s.emailIndex[account.Email] = account.ID

	s.stats.mu.Lock()
	s.stats.Creates++
	s.stats.mu.Unlock()

	return nil
}

// Update modifies an existing account
func (s *MemoryStore) Update(ctx context.Context, account *models.Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStoreUnavailable
	}

	if account == nil {
		return ErrInvalidConfig
	}

	// Check if account exists
	existing, exists := s.accounts[account.ID]
	if !exists {
		return ErrNotFound
	}

	// Check if email is being changed and if new email already exists
	if existing.Email != account.Email {
		if _, exists := s.emailIndex[account.Email]; exists {
			return ErrAlreadyExists
		}
		// Remove old email index
		delete(s.emailIndex, existing.Email)
		// Add new email index
		s.emailIndex[account.Email] = account.ID
	}

	// Update account
	s.accounts[account.ID] = copyAccount(account)

	s.stats.mu.Lock()
	s.stats.Updates++
	s.stats.mu.Unlock()

	return nil
}

// Delete removes an account by ID
func (s *MemoryStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStoreUnavailable
	}

	account, exists := s.accounts[id]
	if !exists {
		return ErrNotFound
	}

	// Remove email index
	delete(s.emailIndex, account.Email)

	// Remove account
	delete(s.accounts, id)

	s.stats.mu.Lock()
	s.stats.Deletes++
	s.stats.mu.Unlock()

	return nil
}

// Close releases resources
func (s *MemoryStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closed = true
	// Clear all data
	s.accounts = nil
	s.emailIndex = nil

	return nil
}

// Health checks if the store is operational
func (s *MemoryStore) Health(ctx context.Context) *HealthStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return &HealthStatus{
			Status:    "unhealthy",
			Connected: false,
			Message:   "store is closed",
		}
	}

	return &HealthStatus{
		Status:    "healthy",
		Connected: true,
		LatencyMs: 0, // In-memory operations are near-instant
	}
}

// GetStats returns store statistics
func (s *MemoryStore) GetStats() MemoryStoreStats {
	s.stats.mu.RLock()
	defer s.stats.mu.RUnlock()

	return MemoryStoreStats{
		Creates: s.stats.Creates,
		Updates: s.stats.Updates,
		Deletes: s.stats.Deletes,
		Gets:    s.stats.Gets,
		Lists:   s.stats.Lists,
	}
}

// CreateAuditLog stores a new audit log entry
func (s *MemoryStore) CreateAuditLog(ctx context.Context, log *models.AuditLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStoreUnavailable
	}

	log.ID = int64(len(s.auditLogs) + 1)
	s.auditLogs = append(s.auditLogs, log)
	return nil
}

// ListAuditLogs retrieves audit logs with optional pagination
func (s *MemoryStore) ListAuditLogs(ctx context.Context, offset, limit int) ([]*models.AuditLog, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, 0, ErrStoreUnavailable
	}

	total := len(s.auditLogs)
	if total == 0 {
		return []*models.AuditLog{}, 0, nil
	}

	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return []*models.AuditLog{}, total, nil
	}

	end := offset + limit
	if limit <= 0 || end > total {
		end = total
	}

	return s.auditLogs[offset:end], total, nil
}

// GetFolderSyncState retrieves sync state for a folder
func (s *MemoryStore) GetFolderSyncState(ctx context.Context, accountID, folderName string) (*models.FolderSyncState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreUnavailable
	}

	key := accountID + ":" + folderName
	state, exists := s.folderSyncState[key]
	if !exists {
		return nil, ErrNotFound
	}

	// Return a copy
	stateCopy := *state
	return &stateCopy, nil
}

// UpsertFolderSyncState creates or updates folder sync state
func (s *MemoryStore) UpsertFolderSyncState(ctx context.Context, state *models.FolderSyncState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStoreUnavailable
	}

	if state == nil {
		return ErrInvalidConfig
	}

	key := state.AccountID + ":" + state.FolderName
	s.folderSyncState[key] = state
	return nil
}

// DeleteFolderSyncState removes folder sync state
func (s *MemoryStore) DeleteFolderSyncState(ctx context.Context, accountID, folderName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStoreUnavailable
	}

	key := accountID + ":" + folderName
	delete(s.folderSyncState, key)
	return nil
}

// ListFolderSyncStates lists all folder sync states for an account
func (s *MemoryStore) ListFolderSyncStates(ctx context.Context, accountID string) ([]*models.FolderSyncState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreUnavailable
	}

	result := make([]*models.FolderSyncState, 0)
	for key, state := range s.folderSyncState {
		if state.AccountID == accountID {
			// Return a copy
			stateCopy := *state
			result = append(result, &stateCopy)
		}
		_ = key // avoid unused variable warning
	}

	return result, nil
}

// copyAccount creates a deep copy of an account
func copyAccount(acc *models.Account) *models.Account {
	if acc == nil {
		return nil
	}

	copy := *acc

	// Copy nested structs
	copy.IMAPConfig = acc.IMAPConfig
	copy.SMTPConfig = acc.SMTPConfig
	copy.SyncSettings = acc.SyncSettings

	if acc.ProxyConfig != nil {
		proxyCopy := *acc.ProxyConfig
		copy.ProxyConfig = &proxyCopy
	}

	if acc.FairUsePolicy != nil {
		policyCopy := *acc.FairUsePolicy
		copy.FairUsePolicy = &policyCopy
		if policyCopy.OperationCosts != nil {
			copy.FairUsePolicy.OperationCosts = make(map[string]int)
			for k, v := range acc.FairUsePolicy.OperationCosts {
				copy.FairUsePolicy.OperationCosts[k] = v
			}
		}
		if policyCopy.PriorityLevels != nil {
			copy.FairUsePolicy.PriorityLevels = make(map[string]int)
			for k, v := range acc.FairUsePolicy.PriorityLevels {
				copy.FairUsePolicy.PriorityLevels[k] = v
			}
		}
	}

	return &copy
}

// MemoryMessageStore stores sent message records
type MemoryMessageStore struct {
	mu        sync.RWMutex
	messages  map[string]*models.Message
	sentIndex map[string][]string // accountID -> message IDs
}

// NewMemoryMessageStore creates a new message store
func NewMemoryMessageStore() *MemoryMessageStore {
	return &MemoryMessageStore{
		messages:  make(map[string]*models.Message),
		sentIndex: make(map[string][]string),
	}
}

// StoreSentMessage stores a sent message record
func (s *MemoryMessageStore) StoreSentMessage(msg *models.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages[msg.MessageID] = msg

	// Index by account
	accountID := msg.From.Address
	s.sentIndex[accountID] = append(s.sentIndex[accountID], msg.MessageID)

	return nil
}

// GetMessageByID retrieves a message by its ID
func (s *MemoryMessageStore) GetMessageByID(messageID string) (*models.Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	msg, exists := s.messages[messageID]
	if !exists {
		return nil, ErrNotFound
	}

	return msg, nil
}

// GetSentMessages retrieves sent messages for an account
func (s *MemoryMessageStore) GetSentMessages(accountID string, limit, offset int) ([]*models.Message, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	messageIDs := s.sentIndex[accountID]
	total := len(messageIDs)

	if total == 0 {
		return []*models.Message{}, 0, nil
	}

	// Handle pagination
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return []*models.Message{}, total, nil
	}

	end := offset + limit
	if limit <= 0 || end > total {
		end = total
	}

	messages := make([]*models.Message, 0, end-offset)
	for i := offset; i < end; i++ {
		if msg, exists := s.messages[messageIDs[i]]; exists {
			messages = append(messages, msg)
		}
	}

	return messages, total, nil
}

// GetAccountProcessorConfigs retrieves processor configs for an account
func (s *MemoryStore) GetAccountProcessorConfigs(ctx context.Context, accountID string) ([]models.AccountProcessorConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return nil, ErrStoreUnavailable
	}

	account, exists := s.accounts[accountID]
	if !exists {
		return nil, ErrNotFound
	}

	if account.ProcessorConfigs == nil {
		return []models.AccountProcessorConfig{}, nil
	}

	// Return a copy
	configs := make([]models.AccountProcessorConfig, len(account.ProcessorConfigs))
	copy(configs, account.ProcessorConfigs)
	return configs, nil
}

// UpdateAccountProcessorConfigs updates processor configs for an account
func (s *MemoryStore) UpdateAccountProcessorConfigs(ctx context.Context, accountID string, configs []models.AccountProcessorConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStoreUnavailable
	}

	account, exists := s.accounts[accountID]
	if !exists {
		return ErrNotFound
	}

	// Store a copy
	account.ProcessorConfigs = make([]models.AccountProcessorConfig, len(configs))
	copy(account.ProcessorConfigs, configs)

	return nil
}

// EnableAccountProcessor enables/disables a specific processor type
func (s *MemoryStore) EnableAccountProcessor(ctx context.Context, accountID, processorType string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStoreUnavailable
	}

	account, exists := s.accounts[accountID]
	if !exists {
		return ErrNotFound
	}

	found := false
	for i := range account.ProcessorConfigs {
		if account.ProcessorConfigs[i].Type == processorType {
			account.ProcessorConfigs[i].Enabled = enabled
			found = true
			break
		}
	}

	if !found {
		return ErrNotFound
	}

	return nil
}

// Ensure MemoryStore implements AccountStore interface
var _ AccountStore = (*MemoryStore)(nil)
