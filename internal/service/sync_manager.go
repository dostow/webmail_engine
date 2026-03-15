package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"webmail_engine/internal/models"
	"webmail_engine/internal/pool"
)

// SyncManager manages background synchronization for all accounts
type SyncManager struct {
	mu             sync.RWMutex
	messageService *MessageService
	accountService *AccountService
	sessions       *pool.IMAPSessionPool
	syncTasks      map[string]*SyncTask
	globalCtx      context.Context
	globalCancel   context.CancelFunc
}

// SyncTask represents a background sync task for an account
type SyncTask struct {
	AccountID      string
	Interval       time.Duration
	LastSync       time.Time
	NextSync       time.Time
	IsRunning      bool
	StopChan       chan struct{}
	Status         string
	LastError      error
	MessagesSynced int
}

// NewSyncManager creates a new sync manager
func NewSyncManager(
	msgService *MessageService,
	accService *AccountService,
	sessions *pool.IMAPSessionPool,
) *SyncManager {
	ctx, cancel := context.WithCancel(context.Background())

	return &SyncManager{
		messageService: msgService,
		accountService: accService,
		sessions:       sessions,
		syncTasks:      make(map[string]*SyncTask),
		globalCtx:      ctx,
		globalCancel:   cancel,
	}
}

// StartSync starts background sync for an account
func (m *SyncManager) StartSync(accountID string, interval time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already running
	if _, exists := m.syncTasks[accountID]; exists {
		log.Printf("Sync already running for account %s", accountID)
		return nil
	}

	// Create sync task
	task := &SyncTask{
		AccountID: accountID,
		Interval:  interval,
		NextSync:  time.Now(),
		StopChan:  make(chan struct{}),
		Status:    "starting",
	}

	m.syncTasks[accountID] = task

	// Start goroutine
	go m.runSyncLoop(task)

	log.Printf("Started background sync for account %s (interval: %v)", accountID, interval)
	return nil
}

// StopSync stops background sync for an account
func (m *SyncManager) StopSync(accountID string) {
	m.mu.Lock()
	task, exists := m.syncTasks[accountID]
	if !exists {
		m.mu.Unlock()
		return
	}

	// Remove from map
	delete(m.syncTasks, accountID)
	m.mu.Unlock()

	// Signal stop
	close(task.StopChan)
	log.Printf("Stopped background sync for account %s", accountID)
}

// StopAll stops all background sync tasks
func (m *SyncManager) StopAll() {
	m.globalCancel()

	m.mu.Lock()
	for _, task := range m.syncTasks {
		close(task.StopChan)
	}
	m.syncTasks = make(map[string]*SyncTask)
	m.mu.Unlock()

	log.Println("Stopped all background sync tasks")
}

// GetSyncStatus returns sync status for an account
func (m *SyncManager) GetSyncStatus(accountID string) *SyncTask {
	m.mu.RLock()
	defer m.mu.RUnlock()

	task, exists := m.syncTasks[accountID]
	if !exists {
		return nil
	}

	return task
}

// runSyncLoop runs the sync loop for an account
func (m *SyncManager) runSyncLoop(task *SyncTask) {
	ticker := time.NewTicker(task.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-task.StopChan:
			return
		case <-m.globalCtx.Done():
			return
		case <-ticker.C:
			// Check if it's time to sync
			if time.Now().Before(task.NextSync) {
				continue
			}

			// Skip if already running
			if task.IsRunning {
				log.Printf("Sync already running for %s, skipping", task.AccountID)
				continue
			}

			// Run sync
			task.IsRunning = true
			task.Status = "syncing"
			task.LastSync = time.Now()

			count, err := m.executeSync(task.AccountID)

			task.MessagesSynced = count
			task.LastError = err
			task.IsRunning = false

			if err != nil {
				log.Printf("Sync error for %s: %v", task.AccountID, err)
				task.Status = "error"
				// Backoff on error - wait longer before next sync
				task.NextSync = time.Now().Add(task.Interval * 2)
			} else {
				log.Printf("Sync completed for %s: %d messages", task.AccountID, count)
				task.Status = "idle"
				task.NextSync = time.Now().Add(task.Interval)
			}
		}
	}
}

func (m *SyncManager) executeSync(accountID string) (int, error) {
	// Get account with credentials
	account, err := m.accountService.GetAccountWithCredentials(context.Background(), accountID)
	if err != nil {
		return 0, err
	}

	// Connect using session pool
	imapConfig := pool.IMAPConfig{
		Host:       account.IMAPConfig.Host,
		Port:       account.IMAPConfig.Port,
		Username:   account.IMAPConfig.Username,
		Password:   account.IMAPConfig.Password,
		Encryption: account.IMAPConfig.Encryption,
	}

	client, release, err := m.sessions.Acquire(context.Background(), accountID, imapConfig)
	if err != nil {
		// Check for authentication errors
		if errors.Is(err, models.ErrMailServerAuthFailed) {
			log.Printf("[Sync] Auth failed for %s, stopping sync task", accountID)
			m.accountService.LogAuditEntry(context.Background(), accountID, account.Email, "auth_failure", "Sync failed: invalid credentials", "127.0.0.1")
			go m.StopSync(accountID)
			return 0, fmt.Errorf("authentication failed: %w", err)
		}
		return 0, err
	}
	defer release()

	// List folders
	folders, err := client.ListFolders()
	if err != nil {
		return 0, err
	}

	totalSynced := 0

	// Sync each folder
	for _, folder := range folders {
		// Skip spam/trash if configured
		if !account.SyncSettings.IncludeSpam && containsString(folder.Attributes, "\\Junk") {
			continue
		}
		if !account.SyncSettings.IncludeTrash && containsString(folder.Attributes, "\\Trash") {
			continue
		}

		// Select folder
		_, err := client.SelectFolder(folder.Name)
		if err != nil {
			log.Printf("Failed to select folder %s: %v", folder.Name, err)
			continue
		}

		// Get cached UID validity
		// Compare with server UID validity
		// If different, full sync needed
		// If same, incremental sync from last UID

		// Search for unseen messages
		uids, err := client.Search("UNSEEN")
		if err != nil {
			log.Printf("Failed to search folder %s: %v", folder.Name, err)
			continue
		}

		// Fetch new messages
		for _, uid := range uids {
			// Fetch message
			envelopes, err := client.FetchMessages([]uint32{uid}, false)
			if err != nil {
				log.Printf("Failed to fetch message %d: %v", uid, err)
				continue
			}

			if len(envelopes) > 0 {
				totalSynced++
				// Update cache
				// Trigger webhook for new message
			}
		}

		// Update folder info in cache
	}

	// Update account last sync time
	// This would need a method to update the account

	return totalSynced, nil
}

// containsString checks if a string is in a slice
func containsString(slice []string, target string) bool {
	for _, s := range slice {
		if s == target {
			return true
		}
	}
	return false
}

// StartSyncForNewAccount starts sync when a new account is added
func (m *SyncManager) StartSyncForNewAccount(accountID string, syncSettings models.SyncSettings) {
	if !syncSettings.AutoSync {
		log.Printf("Auto-sync disabled for account %s", accountID)
		return
	}

	interval := time.Duration(syncSettings.SyncInterval) * time.Second
	if interval < 30*time.Second {
		interval = 300 * time.Second // Minimum 5 minutes
	}

	if err := m.StartSync(accountID, interval); err != nil {
		log.Printf("Failed to start sync for %s: %v", accountID, err)
	}
}
