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
	"webmail_engine/internal/store"
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

// executeSync performs synchronization for an account using UID tracking
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

		// Sync folder with UID tracking (no need to explicitly select, GetFolderStatus uses EXAMINE)
		count, err := m.syncFolder(accountID, client, folder.Name)
		if err != nil {
			log.Printf("Failed to sync folder %s: %v", folder.Name, err)
			continue
		}
		totalSynced += count
	}

	return totalSynced, nil
}

// syncFolder synchronizes a single folder using UID tracking with fallback strategies
func (m *SyncManager) syncFolder(accountID string, client *pool.IMAPAdapter, folderName string) (int, error) {
	ctx := context.Background()

	// Get cached sync state
	cachedState, err := m.accountService.GetFolderSyncState(ctx, accountID, folderName)
	if err != nil && !store.IsNotFound(err) {
		log.Printf("Failed to get folder sync state for %s/%s: %v", accountID, folderName, err)
		// Continue without cached state
		cachedState = nil
	}

	// Get current folder status (includes UID validity and message count)
	status, err := client.GetFolderStatus(folderName)
	if err != nil {
		return 0, fmt.Errorf("failed to get folder status: %w", err)
	}

	// Check UID validity - if changed, mailbox was reset and full sync needed
	if cachedState != nil && status.UIDValidity != cachedState.UIDValidity {
		log.Printf("UID validity changed for %s/%s (was %d, now %d), performing full sync",
			accountID, folderName, cachedState.UIDValidity, status.UIDValidity)
		return m.fullSyncFolder(accountID, client, folderName, status)
	}

	// Determine sync strategy based on cached state
	if cachedState != nil && cachedState.IsInitialized && cachedState.LastSyncedUID > 0 {
		// Incremental sync: fetch messages from lastSyncedUID+1 to current max UID
		if status.UIDNext > cachedState.LastSyncedUID+1 {
			return m.incrementalSync(accountID, client, folderName, cachedState, status)
		}
		// No new messages
		return 0, nil
	}

	// Initial sync or no cached state - use date-based fallback
	return m.initialSyncFolder(accountID, client, folderName, status)
}

// fullSyncFolder performs a full synchronization of a folder (after UID validity change)
func (m *SyncManager) fullSyncFolder(accountID string, client *pool.IMAPAdapter, folderName string, status *pool.FolderStatus) (int, error) {
	log.Printf("Performing full sync for %s/%s (UID range: 1:%d)", accountID, folderName, status.UIDNext-1)

	// Search for ALL messages by UID range
	if status.UIDNext <= 1 {
		// Empty folder
		return m.updateFolderSyncState(accountID, folderName, status, 0)
	}

	// Fetch all messages in batches
	return m.syncUIDRange(accountID, client, folderName, 1, status.UIDNext-1, status)
}

// incrementalSync fetches only new messages since last sync
func (m *SyncManager) incrementalSync(accountID string, client *pool.IMAPAdapter, folderName string, cachedState *models.FolderSyncState, status *pool.FolderStatus) (int, error) {
	fromUID := cachedState.LastSyncedUID + 1
	toUID := status.UIDNext - 1

	if fromUID > toUID {
		return 0, nil
	}

	log.Printf("Incremental sync for %s/%s (UID range: %d:%d)", accountID, folderName, fromUID, toUID)
	return m.syncUIDRange(accountID, client, folderName, fromUID, toUID, status)
}

// initialSyncFolder performs initial synchronization using date-based search
func (m *SyncManager) initialSyncFolder(accountID string, client *pool.IMAPAdapter, folderName string, status *pool.FolderStatus) (int, error) {
	log.Printf("Initial sync for %s/%s", accountID, folderName)

	// Use historical scope from sync settings
	account, err := m.accountService.GetAccount(context.Background(), accountID)
	if err != nil {
		return 0, err
	}

	historicalDays := account.SyncSettings.HistoricalScope
	if historicalDays <= 0 {
		historicalDays = 30 // Default to 30 days
	}

	sinceDate := time.Now().AddDate(0, 0, -historicalDays)

	// Search by date as fallback
	uids, err := client.Search(fmt.Sprintf("SINCE %s", sinceDate.Format("02-Jan-2006")))
	if err != nil {
		log.Printf("Date-based search failed for %s/%s: %v, falling back to UNSEEN", accountID, folderName, err)
		// Fallback to UNSEEN search
		uids, err = client.Search("UNSEEN")
		if err != nil {
			return 0, fmt.Errorf("search failed: %w", err)
		}
	}

	if len(uids) == 0 {
		return m.updateFolderSyncState(accountID, folderName, status, 0)
	}

	// Fetch messages
	count := 0
	for _, uid := range uids {
		envelopes, err := client.FetchMessages([]uint32{uid}, false)
		if err != nil {
			log.Printf("Failed to fetch message %d in %s/%s: %v", uid, accountID, folderName, err)
			continue
		}

		if len(envelopes) > 0 {
			count++
			// Process message (update cache, trigger webhooks, etc.)
			// This would integrate with the message service
		}
	}

	return m.updateFolderSyncState(accountID, folderName, status, count)
}

// syncUIDRange fetches messages in a UID range and updates sync state
func (m *SyncManager) syncUIDRange(accountID string, client *pool.IMAPAdapter, folderName string, fromUID, toUID uint32, status *pool.FolderStatus) (int, error) {
	// Search for UIDs in range
	searchCriteria := fmt.Sprintf("UID %d:%d", fromUID, toUID)
	uids, err := client.Search(searchCriteria)
	if err != nil {
		return 0, fmt.Errorf("UID search failed: %w", err)
	}

	if len(uids) == 0 {
		return m.updateFolderSyncState(accountID, folderName, status, 0)
	}

	// Fetch messages in batches (to avoid memory issues with large ranges)
	batchSize := 50
	count := 0

	for i := 0; i < len(uids); i += batchSize {
		end := i + batchSize
		if end > len(uids) {
			end = len(uids)
		}

		batch := uids[i:end]
		envelopes, err := client.FetchMessages(batch, false)
		if err != nil {
			log.Printf("Failed to fetch batch %d-%d in %s/%s: %v", batch[0], batch[len(batch)-1], accountID, folderName, err)
			continue
		}

		count += len(envelopes)

		// Process each message
		for _, env := range envelopes {
			// Update cache
			// Trigger webhook for new message
			// This would integrate with the message service
			_ = env
		}
	}

	return m.updateFolderSyncState(accountID, folderName, status, count)
}

// updateFolderSyncState updates the folder sync state after successful sync
func (m *SyncManager) updateFolderSyncState(accountID, folderName string, status *pool.FolderStatus, messagesSynced int) (int, error) {
	state := &models.FolderSyncState{
		AccountID:     accountID,
		FolderName:    folderName,
		UIDValidity:   status.UIDValidity,
		LastSyncedUID: status.UIDNext - 1, // UIDNext is the next unused UID
		LastSyncTime:  time.Now(),
		MessageCount:  status.Messages,
		IsInitialized: true,
	}

	ctx := context.Background()
	if err := m.accountService.UpsertFolderSyncState(ctx, state); err != nil {
		log.Printf("Failed to update folder sync state for %s/%s: %v", accountID, folderName, err)
		// Don't fail the sync, just log the error
	}

	return messagesSynced, nil
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
	if interval < 60*time.Second {
		interval = 60 * time.Second // Minimum 1 minute
	}

	if err := m.StartSync(accountID, interval); err != nil {
		log.Printf("Failed to start sync for %s: %v", accountID, err)
	}
}
