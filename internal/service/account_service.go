package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"webmail_engine/internal/cache"
	"webmail_engine/internal/models"
	"webmail_engine/internal/pool"
	"webmail_engine/internal/scheduler"
	"webmail_engine/internal/store"
)

// AccountService manages email accounts
type AccountService struct {
	mu         sync.RWMutex
	store      store.AccountStore
	pool       *pool.ConnectionPool
	cache      *cache.Cache
	scheduler  *scheduler.FairUseScheduler
	syncMgr    *SyncManager
	encryptKey []byte
	nextID     int
}

// AccountServiceConfig holds service configuration
type AccountServiceConfig struct {
	EncryptionKey string
	PoolConfig    pool.PoolConfig
}

// NewAccountService creates a new account service
func NewAccountService(
	str store.AccountStore,
	pool *pool.ConnectionPool,
	cache *cache.Cache,
	scheduler *scheduler.FairUseScheduler,
	syncMgr *SyncManager,
	config AccountServiceConfig,
) (*AccountService, error) {
	encryptKey, err := parseEncryptionKey(config.EncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("invalid encryption key: %w", err)
	}

	return &AccountService{
		store:      str,
		pool:       pool,
		cache:      cache,
		scheduler:  scheduler,
		syncMgr:    syncMgr,
		encryptKey: encryptKey,
		nextID:     1,
	}, nil
}

// SetSyncManager sets the sync manager (for circular dependency)
func (s *AccountService) SetSyncManager(syncMgr *SyncManager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncMgr = syncMgr
}

// AddAccount adds a new email account (or updates if credentials differ)
func (s *AccountService) AddAccount(ctx context.Context, req models.AddAccountRequest) (*models.AddAccountResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for existing account with same email in store
	existing, err := s.store.GetByEmail(ctx, req.Email)
	if err == nil {
		// Account exists, check if config is the same
		if s.isAccountConfigSame(existing, req) {
			// Same config, return existing account
			log.Printf("Account %s already exists with same configuration", req.Email)
			return &models.AddAccountResponse{
				AccountID:             existing.ID,
				Status:                existing.Status,
				ConnectionEstablished: true,
				InitialSyncStatus:     "already_running",
				InitialSyncProgress:   100,
				MessageCount:          0,
				ResourceAllocation: models.ResourceStatus{
					CurrentConnections: s.pool.GetAccountConnectionCount(existing.ID),
					MaxConnections:     existing.ConnectionLimit,
				},
			}, nil
		}

		// Different config, update the account
		log.Printf("Account %s exists with different configuration, updating...", req.Email)
		return s.updateAccountConfig(ctx, existing, req)
	} else if !store.IsNotFound(err) {
		return nil, fmt.Errorf("failed to check for existing account: %w", err)
	}

	// Generate account ID
	accountID := fmt.Sprintf("acc_%d", s.nextID)
	s.nextID++

	// Create account
	account := &models.Account{
		ID:        accountID,
		Email:     req.Email,
		AuthType:  req.AuthType,
		Status:    models.AccountStatusInactive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		IMAPConfig: models.ServerConfig{
			Host:       req.IMAPHost,
			Port:       req.IMAPPort,
			Encryption: req.IMAPEncryption,
			Username:   req.Email,
			Password:   req.Password,
		},
		SMTPConfig: models.ServerConfig{
			Host:       req.SMTPHost,
			Port:       req.SMTPPort,
			Encryption: req.SMTPEncryption,
			Username:   req.Email,
			Password:   req.Password,
		},
		ConnectionLimit: req.ConnectionLimit,
		SyncSettings:    req.SyncSettings,
		ProxyConfig:     req.ProxyConfig,
	}

	// Encrypt password
	encryptedPassword, err := s.encrypt(req.Password)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt password: %w", err)
	}

	// Verify connection FIRST with raw password (before encrypting)
	if req.IMAPHost != "" {
		log.Printf("Verifying IMAP connection to %s:%d...", req.IMAPHost, req.IMAPPort)
		// Use timeout context for connection verification
		timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		if err := s.verifyConnectionWithRawPassword(timeoutCtx, req); err != nil {
			log.Printf("Connection verification failed: %v", err)
			// Check if it's a timeout
			if timeoutCtx.Err() == context.DeadlineExceeded {
				return nil, models.NewTimeoutError("Connection verification", 30)
			}
			return nil, models.NewServiceUnavailableError("Email server", err.Error())
		} else {
			log.Printf("Connection verification successful")
		}
	}

	// Now encrypt and store
	account.IMAPConfig.Password = encryptedPassword
	account.SMTPConfig.Password = encryptedPassword

	// Update status to active before storing
	account.Status = models.AccountStatusActive
	now := time.Now()
	account.LastSyncAt = &now

	// Initialize fair-use scheduling
	s.scheduler.InitializeAccount(accountID, req.SyncSettings.FairUsePolicy)

	// Store account in persistent store
	if err := s.store.Create(ctx, account); err != nil {
		if store.IsAlreadyExists(err) {
			return nil, models.ErrAccountExists
		}
		return nil, fmt.Errorf("failed to store account: %w", err)
	}

	// Start background sync if enabled
	if s.syncMgr != nil {
		s.syncMgr.StartSyncForNewAccount(accountID, req.SyncSettings)
	}

	// Don't cache the full account with encrypted password
	// The cache is only for stripped accounts used in API responses
	// The first API call to GetAccount will populate the cache

	return &models.AddAccountResponse{
		AccountID:             accountID,
		Status:                account.Status,
		ConnectionEstablished: true,
		InitialSyncStatus:     "started",
		InitialSyncProgress:   0,
		MessageCount:          0,
		ResourceAllocation: models.ResourceStatus{
			CurrentConnections: 1,
			MaxConnections:     account.ConnectionLimit,
		},
	}, nil
}

// isAccountConfigSame checks if two account configurations are identical
func (s *AccountService) isAccountConfigSame(acc *models.Account, req models.AddAccountRequest) bool {
	return acc.AuthType == req.AuthType &&
		acc.IMAPConfig.Host == req.IMAPHost &&
		acc.IMAPConfig.Port == req.IMAPPort &&
		acc.IMAPConfig.Encryption == req.IMAPEncryption &&
		acc.SMTPConfig.Host == req.SMTPHost &&
		acc.SMTPConfig.Port == req.SMTPPort &&
		acc.SMTPConfig.Encryption == req.SMTPEncryption &&
		acc.ConnectionLimit == req.ConnectionLimit
}

// updateAccountConfig updates an existing account with new configuration
func (s *AccountService) updateAccountConfig(ctx context.Context, acc *models.Account, req models.AddAccountRequest) (*models.AddAccountResponse, error) {
	// Verify new connection first
	if req.IMAPHost != "" {
		log.Printf("Verifying new IMAP connection to %s:%d...", req.IMAPHost, req.IMAPPort)
		timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		if err := s.verifyConnectionWithRawPassword(timeoutCtx, req); err != nil {
			log.Printf("New connection verification failed: %v", err)
			if timeoutCtx.Err() == context.DeadlineExceeded {
				return nil, models.NewTimeoutError("Connection verification", 30)
			}
			return nil, models.NewServiceUnavailableError("Email server", err.Error())
		}
		log.Printf("New connection verification successful")
	}

	// Encrypt new password
	encryptedPassword, err := s.encrypt(req.Password)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt password: %w", err)
	}

	// Update account configuration
	acc.AuthType = req.AuthType
	acc.IMAPConfig.Host = req.IMAPHost
	acc.IMAPConfig.Port = req.IMAPPort
	acc.IMAPConfig.Encryption = req.IMAPEncryption
	acc.IMAPConfig.Password = encryptedPassword
	acc.SMTPConfig.Host = req.SMTPHost
	acc.SMTPConfig.Port = req.SMTPPort
	acc.SMTPConfig.Encryption = req.SMTPEncryption
	acc.SMTPConfig.Password = encryptedPassword
	acc.ConnectionLimit = req.ConnectionLimit
	acc.SyncSettings = req.SyncSettings
	acc.ProxyConfig = req.ProxyConfig
	acc.UpdatedAt = time.Now()

	// Update in persistent store
	if err := s.store.Update(ctx, acc); err != nil {
		return nil, fmt.Errorf("failed to update account in store: %w", err)
	}

	// Close old connections to force reconnect with new credentials
	s.pool.CloseAccount(acc.ID)

	// Reinitialize fair-use scheduling with new policy
	s.scheduler.InitializeAccount(acc.ID, req.SyncSettings.FairUsePolicy)

	// Restart sync if enabled
	if s.syncMgr != nil {
		s.syncMgr.StopSync(acc.ID)
		s.syncMgr.StartSyncForNewAccount(acc.ID, req.SyncSettings)
	}

	// Don't cache the full account with encrypted password
	// The cache is only for stripped accounts used in API responses

	acc.Status = models.AccountStatusActive
	now := time.Now()
	acc.LastSyncAt = &now

	return &models.AddAccountResponse{
		AccountID:             acc.ID,
		Status:                acc.Status,
		ConnectionEstablished: true,
		InitialSyncStatus:     "reconfigured",
		InitialSyncProgress:   100,
		MessageCount:          0,
		ResourceAllocation: models.ResourceStatus{
			CurrentConnections: 1,
			MaxConnections:     acc.ConnectionLimit,
		},
	}, nil
}

// GetAccountWithCredentials retrieves an account with decrypted credentials for internal use
func (s *AccountService) GetAccountWithCredentials(ctx context.Context, accountID string) (*models.Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Always fetch from store to get encrypted password
	// Note: We don't cache full accounts with passwords to avoid security issues
	// The store (SQLite) is fast enough for this use case
	account, err := s.store.GetByID(ctx, accountID)
	if err != nil {
		if store.IsNotFound(err) {
			return nil, models.ErrAccountNotFound
		}
		return nil, fmt.Errorf("failed to get account from store: %w", err)
	}

	log.Printf("[DEBUG] Account %s loaded from store", accountID)

	// Decrypt passwords
	accountCopy := *account
	if account.IMAPConfig.Password != "" {
		password, err := s.decrypt(account.IMAPConfig.Password)
		if err == nil {
			accountCopy.IMAPConfig.Password = password
			log.Printf("[DEBUG] IMAP password decrypted successfully for %s (length: %d)", accountID, len(password))
		} else {
			log.Printf("Warning: Failed to decrypt IMAP password for account %s: %v", accountID, err)
		}
	} else {
		log.Printf("[DEBUG] IMAP password is empty for account %s", accountID)
	}
	if account.SMTPConfig.Password != "" {
		password, err := s.decrypt(account.SMTPConfig.Password)
		if err == nil {
			accountCopy.SMTPConfig.Password = password
			log.Printf("[DEBUG] SMTP password decrypted successfully for %s (length: %d)", accountID, len(password))
		} else {
			log.Printf("Warning: Failed to decrypt SMTP password for account %s: %v", accountID, err)
		}
	} else {
		log.Printf("[DEBUG] SMTP password is empty for account %s", accountID)
	}

	return &accountCopy, nil
}

// GetAccount retrieves an account by ID (without sensitive data)
func (s *AccountService) GetAccount(ctx context.Context, accountID string) (*models.Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Try cache first (cache stores stripped accounts for API responses)
	cached, _ := s.cache.GetAccount(ctx, accountID)
	if cached != nil {
		// Cache already has stripped data
		return cached, nil
	}

	// Fall back to store
	account, err := s.store.GetByID(ctx, accountID)
	if err != nil {
		if store.IsNotFound(err) {
			return nil, models.ErrAccountNotFound
		}
		return nil, fmt.Errorf("failed to get account from store: %w", err)
	}

	// Cache the stripped version (not the full account)
	stripped := stripSensitiveData(account)
	s.cache.SetAccount(ctx, stripped)

	return stripped, nil
}

// ListAccounts lists all accounts
func (s *AccountService) ListAccounts(ctx context.Context) ([]*models.Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	accounts, total, err := s.store.List(ctx, 0, 1000)
	if err != nil {
		return nil, fmt.Errorf("failed to list accounts from store: %w", err)
	}

	if total == 0 {
		return []*models.Account{}, nil
	}

	// Strip sensitive data from all accounts
	result := make([]*models.Account, len(accounts))
	for i, acc := range accounts {
		result[i] = stripSensitiveData(acc)
	}

	return result, nil
}

// UpdateAccount updates an account
func (s *AccountService) UpdateAccount(ctx context.Context, accountID string, updates map[string]interface{}) (*models.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get account from store
	account, err := s.store.GetByID(ctx, accountID)
	if err != nil {
		if store.IsNotFound(err) {
			return nil, models.ErrAccountNotFound
		}
		return nil, fmt.Errorf("failed to get account: %w", err)
	}

	// Apply updates
	for key, value := range updates {
		switch key {
		case "connection_limit":
			if limit, ok := value.(int); ok {
				account.ConnectionLimit = limit
			}
		case "sync_settings":
			if settings, ok := value.(models.SyncSettings); ok {
				account.SyncSettings = settings
			}
		case "status":
			if status, ok := value.(models.AccountStatus); ok {
				account.Status = status
			}
		}
	}

	account.UpdatedAt = time.Now()

	// Update in store
	if err := s.store.Update(ctx, account); err != nil {
		return nil, fmt.Errorf("failed to update account in store: %w", err)
	}

	// Invalidate cache so next GetAccount fetches fresh data
	s.cache.InvalidateAccount(ctx, accountID)

	return stripSensitiveData(account), nil
}

// DeleteAccount removes an account
func (s *AccountService) DeleteAccount(ctx context.Context, accountID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Verify account exists
	_, err := s.store.GetByID(ctx, accountID)
	if err != nil {
		if store.IsNotFound(err) {
			return models.ErrAccountNotFound
		}
		return fmt.Errorf("failed to get account: %w", err)
	}

	// Close connections
	s.pool.CloseAccount(accountID)

	// Remove from scheduler
	s.scheduler.RemoveAccount(accountID)

	// Invalidate cache
	s.cache.InvalidateAccount(ctx, accountID)

	// Delete from store
	if err := s.store.Delete(ctx, accountID); err != nil {
		return fmt.Errorf("failed to delete account from store: %w", err)
	}

	return nil
}

// GetAccountStatus returns the current status of an account
func (s *AccountService) GetAccountStatus(ctx context.Context, accountID string) (*models.AccountStatusResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Get account from store
	account, err := s.store.GetByID(ctx, accountID)
	if err != nil {
		if store.IsNotFound(err) {
			return nil, models.ErrAccountNotFound
		}
		return nil, fmt.Errorf("failed to get account: %w", err)
	}

	// Get connection info
	connCount := s.pool.GetAccountConnectionCount(accountID)

	// Get token bucket
	bucket := s.scheduler.GetTokenBucket(accountID)

	// Build status response
	status := &models.AccountStatusResponse{
		AccountID: accountID,
		ConnectionState: models.ConnectionState{
			Status:        account.Status,
			IMAPConnected: connCount > 0,
			SMTPConnected: connCount > 0,
			LastConnected: account.LastSyncAt,
			ErrorCount:    0,
		},
		Performance: models.PerformanceMetrics{
			AvgLatency:       0,
			RecentErrors:     0,
			OperationsPerMin: 0,
			LastOperation:    account.LastSyncAt,
		},
		Resources: models.ResourceStatus{
			CurrentConnections: connCount,
			MaxConnections:     account.ConnectionLimit,
			TokenBucket:        bucket,
		},
		Health: models.HealthIndicators{
			Score:  s.calculateHealthScore(account),
			Status: s.calculateHealthStatus(account),
		},
	}

	if account.LastSyncAt != nil {
		status.LastSuccessful = account.LastSyncAt
	}

	return status, nil
}

// verifyConnectionWithRawPassword tests the IMAP/SMTP connection with raw password
func (s *AccountService) verifyConnectionWithRawPassword(ctx context.Context, req models.AddAccountRequest) error {
	// Test IMAP connection
	imapConfig := pool.IMAPConfig{
		Host:       req.IMAPHost,
		Port:       req.IMAPPort,
		Username:   req.Email,
		Password:   req.Password, // Use raw password
		Encryption: req.IMAPEncryption,
	}

	log.Printf("Attempting IMAP login to %s:%d as %s", req.IMAPHost, req.IMAPPort, req.Email)
	imapClient, err := pool.ConnectIMAP(ctx, imapConfig)
	if err != nil {
		return fmt.Errorf("IMAP connection failed: %w", err)
	}
	defer imapClient.Close()
	log.Printf("IMAP connection successful")

	// Test SMTP connection (optional, don't fail if SMTP is unavailable)
	if req.SMTPHost != "" {
		smtpConfig := pool.SMTPConfig{
			Host:       req.SMTPHost,
			Port:       req.SMTPPort,
			Username:   req.Email,
			Password:   req.Password, // Use raw password
			Encryption: req.SMTPEncryption,
		}

		log.Printf("Attempting SMTP login to %s:%d as %s", req.SMTPHost, req.SMTPPort, req.Email)
		smtpClient, err := pool.ConnectSMTPv2(ctx, smtpConfig)
		if err == nil {
			defer smtpClient.Close()
			log.Printf("SMTP connection successful")
		} else {
			log.Printf("SMTP connection failed (non-fatal): %v", err)
		}
	}

	return nil
}

// encrypt encrypts data using AES-GCM
func (s *AccountService) encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(s.encryptKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decrypt decrypts data using AES-GCM
func (s *AccountService) decrypt(ciphertextStr string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertextStr)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(s.encryptKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce := data[:nonceSize]
	ciphertext := data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// calculateHealthScore calculates a health score for an account
func (s *AccountService) calculateHealthScore(account *models.Account) int {
	score := 100

	// Reduce score for error status
	switch account.Status {
	case models.AccountStatusError:
		score -= 50
	case models.AccountStatusThrottled:
		score -= 30
	case models.AccountStatusAuthRequired:
		score -= 40
	}

	// Reduce score if no recent sync
	if account.LastSyncAt != nil {
		timeSinceSync := time.Since(*account.LastSyncAt)
		if timeSinceSync > 24*time.Hour {
			score -= 20
		} else if timeSinceSync > 1*time.Hour {
			score -= 10
		}
	}

	if score < 0 {
		score = 0
	}

	return score
}

// calculateHealthStatus calculates health status for an account
func (s *AccountService) calculateHealthStatus(account *models.Account) models.HealthStatus {
	if account.Status == models.AccountStatusError {
		return models.HealthStatusUnhealthy
	}

	if account.Status == models.AccountStatusThrottled || account.Status == models.AccountStatusAuthRequired {
		return models.HealthStatusDegraded
	}

	return models.HealthStatusHealthy
}

// parseEncryptionKey parses the encryption key
func parseEncryptionKey(key string) ([]byte, error) {
	if key == "" {
		// Use default key (in production, this should be configured)
		key = "default-encryption-key-32-bytes!"
	}

	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes")
	}

	return []byte(key), nil
}

// stripSensitiveData returns a copy of the account without sensitive fields
func stripSensitiveData(acc *models.Account) *models.Account {
	if acc == nil {
		return nil
	}

	copy := *acc
	copy.IMAPConfig.Password = ""
	copy.SMTPConfig.Password = ""
	copy.SMTPConfig.AccessToken = ""
	copy.SMTPConfig.RefreshToken = ""
	copy.IMAPConfig.AccessToken = ""
	copy.IMAPConfig.RefreshToken = ""
	if copy.ProxyConfig != nil {
		proxyCopy := *copy.ProxyConfig
		proxyCopy.Password = ""
		copy.ProxyConfig = &proxyCopy
	}
	return &copy
}
