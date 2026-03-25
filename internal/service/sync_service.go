package service

import (
	"context"
	"fmt"
	"log"
	"slices"
	"sync"
	"time"

	"webmail_engine/internal/envelopequeue"
	"webmail_engine/internal/models"
	"webmail_engine/internal/pool"
	"webmail_engine/internal/store"
)

// Constants for sync service configuration

// foldersPerBatch is the number of folders to sync concurrently.
const foldersPerBatch = 5

// initialSyncBatchLimit is the maximum number of messages fetched during initial sync.
const initialSyncBatchLimit = 500

// envelopeBatchSize is the number of messages fetched per batch during sync.
const envelopeBatchSize = 50

// defaultMaxRetries is the default number of retries for envelope processing.
const defaultMaxRetries = 3

// messagesPerDayEstimate is the estimated number of messages per day for historical scope calculations.
const messagesPerDayEstimate = 30

// maxUIDsPerSync is the maximum number of UIDs to process in a single sync operation.
const maxUIDsPerSync = 10000

// SyncService handles email synchronization operations.
// It provides methods for syncing accounts and folders, and managing sync state.
type SyncService struct {
	accountService *AccountService
	sessionPool    *pool.IMAPSessionPool
	queue          envelopequeue.EnvelopeQueue
}

// NewSyncService creates a new SyncService.
func NewSyncService(
	accountService *AccountService,
	sessionPool *pool.IMAPSessionPool,
	queue envelopequeue.EnvelopeQueue,
) *SyncService {
	return &SyncService{
		accountService: accountService,
		sessionPool:    sessionPool,
		queue:          queue,
	}
}

// SyncAccount performs synchronization for an entire account.
func (s *SyncService) SyncAccount(ctx context.Context, accountID string, opts SyncOptions) (*SyncResult, error) {
	start := time.Now()
	result := &SyncResult{AccountID: accountID}

	// Apply timeout if configured
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	// Get account with credentials
	account, err := s.accountService.GetAccountWithCredentials(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to get account: %w", err)
	}

	// Connect using session pool
	client, release, err := s.acquireIMAPClient(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire IMAP client: %w", err)
	}
	defer release()

	// List folders
	folders, err := client.ListFolders()
	if err != nil {
		return nil, fmt.Errorf("failed to list folders: %w", err)
	}

	totalSynced := 0
	envelopesEnqueued := 0
	var errors []string
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Process folders in batches
	for i := 0; i < len(folders); i += foldersPerBatch {
		// Check for cancellation before starting batch
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		end := i + foldersPerBatch
		if end > len(folders) {
			end = len(folders)
		}
		batch := folders[i:end]

		// Launch goroutines for this batch
		for _, folder := range batch {
			wg.Add(1)
			go func(folder pool.FolderInfo) {
				defer wg.Done()

				// Check for cancellation
				select {
				case <-ctx.Done():
					return
				default:
				}

				// Skip spam/trash if configured
				if !opts.IncludeSpam && slices.Contains(folder.Attributes, "\\Junk") {
					return
				}
				if !opts.IncludeTrash && slices.Contains(folder.Attributes, "\\Trash") {
					return
				}

				// Sync folder
				count, err := s.syncFolder(ctx, accountID, client, folder.Name, opts)

				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					errors = append(errors, fmt.Sprintf("folder %s: %v", folder.Name, err))
				} else {
					totalSynced += count
					envelopesEnqueued += count
				}
			}(folder)
		}

		// Wait for batch to complete
		wg.Wait()
	}

	result.MessagesSynced = totalSynced
	result.FoldersSynced = len(folders)
	result.EnvelopesEnqueued = envelopesEnqueued
	result.Errors = errors
	result.Duration = time.Since(start)

	return result, nil
}

// SyncFolder performs synchronization for a specific folder.
func (s *SyncService) SyncFolder(ctx context.Context, accountID, folderName string, opts SyncOptions) (*SyncResult, error) {
	start := time.Now()

	// Apply timeout if configured
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	// Get account with credentials
	account, err := s.accountService.GetAccountWithCredentials(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to get account: %w", err)
	}

	// Connect using session pool
	client, release, err := s.acquireIMAPClient(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire IMAP client: %w", err)
	}
	defer release()

	// Sync folder
	count, err := s.syncFolder(ctx, accountID, client, folderName, opts)
	if err != nil {
		return nil, err
	}

	return &SyncResult{
		AccountID:         accountID,
		MessagesSynced:    count,
		FoldersSynced:     1,
		EnvelopesEnqueued: count,
		Duration:          time.Since(start),
	}, nil
}

// GetSyncState returns the sync state for a folder.
func (s *SyncService) GetSyncState(ctx context.Context, accountID, folderName string) (*FolderSyncState, error) {
	state, err := s.accountService.GetFolderSyncState(ctx, accountID, folderName)
	if err != nil {
		return nil, err
	}

	return &FolderSyncState{
		AccountID:     state.AccountID,
		FolderName:    state.FolderName,
		UIDValidity:   state.UIDValidity,
		LastSyncedUID: state.LastSyncedUID,
		LastSyncTime:  state.LastSyncTime,
		MessageCount:  state.MessageCount,
		IsInitialized: state.IsInitialized,
	}, nil
}

// syncFolder synchronizes a single folder and enqueues envelopes.
func (s *SyncService) syncFolder(ctx context.Context, accountID string, client *pool.IMAPAdapter, folderName string, opts SyncOptions) (int, error) {
	// Get cached sync state
	cachedState, err := s.accountService.GetFolderSyncState(ctx, accountID, folderName)
	if err != nil && !store.IsNotFound(err) {
		log.Printf("Failed to get folder sync state for %s/%s: %v", accountID, folderName, err)
		cachedState = nil
	}

	// Get current folder status
	status, err := client.GetFolderStatus(folderName)
	if err != nil {
		return 0, fmt.Errorf("failed to get folder status: %w", err)
	}

	// Check UID validity - if changed, mailbox was reset and full sync needed
	if cachedState != nil && status.UIDValidity != cachedState.UIDValidity {
		log.Printf("UID validity changed for %s/%s (was %d, now %d), performing full sync",
			accountID, folderName, cachedState.UIDValidity, status.UIDValidity)
		return s.fullSyncFolder(ctx, accountID, client, folderName, status, opts)
	}

	// Determine sync strategy based on cached state
	if cachedState != nil && cachedState.IsInitialized && cachedState.LastSyncedUID > 0 {
		// Incremental sync: fetch messages from lastSyncedUID+1 to current max UID
		if status.UIDNext > cachedState.LastSyncedUID+1 {
			return s.incrementalSync(ctx, accountID, client, folderName, cachedState, status, opts)
		}
		// No new messages
		return 0, nil
	}

	// Initial sync or no cached state
	return s.initialSyncFolder(ctx, accountID, client, folderName, status, opts)
}

// fullSyncFolder performs a full synchronization of a folder.
func (s *SyncService) fullSyncFolder(ctx context.Context, accountID string, client *pool.IMAPAdapter, folderName string, status *pool.FolderStatus, opts SyncOptions) (int, error) {
	log.Printf("Performing full sync for %s/%s (UID range: 1:%d)", accountID, folderName, status.UIDNext-1)

	if status.UIDNext <= 1 {
		// Empty folder
		return s.updateFolderSyncState(ctx, accountID, folderName, status, 0)
	}

	// Fetch all messages in batches
	return s.syncUIDRange(ctx, accountID, client, folderName, 1, status.UIDNext-1, status, opts)
}

// incrementalSync fetches only new messages since last sync.
func (s *SyncService) incrementalSync(ctx context.Context, accountID string, client *pool.IMAPAdapter, folderName string, cachedState *models.FolderSyncState, status *pool.FolderStatus, opts SyncOptions) (int, error) {
	fromUID := cachedState.LastSyncedUID + 1
	toUID := status.UIDNext - 1

	if fromUID > toUID {
		return 0, nil
	}

	log.Printf("Incremental sync for %s/%s (UID range: %d:%d)", accountID, folderName, fromUID, toUID)
	return s.syncUIDRange(ctx, accountID, client, folderName, fromUID, toUID, status, opts)
}

// initialSyncFolder performs initial synchronization for a folder.
func (s *SyncService) initialSyncFolder(ctx context.Context, accountID string, client *pool.IMAPAdapter, folderName string, status *pool.FolderStatus, opts SyncOptions) (int, error) {
	log.Printf("Initial sync for %s/%s (%d messages)", accountID, folderName, status.Messages)

	if status.Messages == 0 {
		return s.updateFolderSyncState(ctx, accountID, folderName, status, 0)
	}

	// Calculate limit based on historical scope
	// Uses messagesPerDayEstimate (~30 msgs/day) as a rough heuristic
	historicalDays := opts.HistoricalScope
	if historicalDays <= 0 {
		historicalDays = 30
	}
	limit := historicalDays * messagesPerDayEstimate
	if limit > initialSyncBatchLimit {
		limit = initialSyncBatchLimit
	}

	// Fetch newest UIDs by sequence number (efficient server-side operation)
	uids, err := client.FetchNewestUIDsBySequence(int(status.Messages), limit)
	if err != nil {
		log.Printf("FetchNewestUIDsBySequence failed for %s/%s: %v, falling back to UNSEEN search", accountID, folderName, err)
		// Graceful fallback: fetch only UNSEEN messages
		uids, err = client.Search("UNSEEN")
		if err != nil {
			return 0, fmt.Errorf("fallback UNSEEN search failed: %w", err)
		}
	}

	if len(uids) == 0 {
		return s.updateFolderSyncState(ctx, accountID, folderName, status, 0)
	}

	log.Printf("Initial sync fetching %d UIDs for %s/%s", len(uids), accountID, folderName)
	return s.enqueueEnvelopes(ctx, accountID, folderName, uids, client, opts, status)
}

// syncUIDRange fetches messages in a UID range and enqueues envelopes.
func (s *SyncService) syncUIDRange(ctx context.Context, accountID string, client *pool.IMAPAdapter, folderName string, fromUID, toUID uint32, status *pool.FolderStatus, opts SyncOptions) (int, error) {
	// Search for UIDs in range
	searchCriteria := fmt.Sprintf("UID %d:%d", fromUID, toUID)
	uids, err := client.Search(searchCriteria)
	if err != nil {
		return 0, fmt.Errorf("UID search failed: %w", err)
	}

	if len(uids) == 0 {
		return s.updateFolderSyncState(ctx, accountID, folderName, status, 0)
	}

	// Limit UIDs to prevent memory issues with very large folders
	if len(uids) > maxUIDsPerSync {
		log.Printf("Limiting UID batch from %d to %d for %s/%s", len(uids), maxUIDsPerSync, accountID, folderName)
		uids = uids[:maxUIDsPerSync]
	}

	return s.enqueueEnvelopes(ctx, accountID, folderName, uids, client, opts, status)
}

// enqueueEnvelopes fetches envelopes and enqueues them for processing.
func (s *SyncService) enqueueEnvelopes(ctx context.Context, accountID, folderName string, uids []uint32, client *pool.IMAPAdapter, opts SyncOptions, status *pool.FolderStatus) (int, error) {
	count := 0
	enqueued := 0

	for i := 0; i < len(uids); i += envelopeBatchSize {
		end := i + envelopeBatchSize
		if end > len(uids) {
			end = len(uids)
		}

		batch := uids[i:end]
		envelopes, err := client.FetchMessages(batch, opts.FetchBody)
		if err != nil {
			if len(batch) >= 2 {
				log.Printf("Failed to fetch batch %d-%d in %s/%s: %v", batch[0], batch[len(batch)-1], accountID, folderName, err)
			} else if len(batch) == 1 {
				log.Printf("Failed to fetch UID %d in %s/%s: %v", batch[0], accountID, folderName, err)
			}
			continue
		}

		count += len(envelopes)

		for _, env := range envelopes {
			if err := s.enqueueEnvelope(ctx, accountID, folderName, &env); err != nil {
				log.Printf("Failed to enqueue envelope %s in %s/%s: %v", env.MessageID, accountID, folderName, err)
				continue
			}
			enqueued++
		}
	}

	log.Printf("Enqueued %d/%d envelopes for processing from %s/%s",
		enqueued, count, accountID, folderName)

	return s.updateFolderSyncState(ctx, accountID, folderName, status, count)
}

// enqueueEnvelope creates a queue item from an envelope and adds it to the processing queue.
func (s *SyncService) enqueueEnvelope(ctx context.Context, accountID, folderName string, env *pool.MessageEnvelope) error {
	if s.queue == nil {
		// Queue not configured, skip enqueueing
		return nil
	}

	// Determine priority based on flags and folder
	priority := s.determineEnvelopePriority(folderName, env)

	// Create queue item
	queueItem := &models.EnvelopeQueueItem{
		ID:         fmt.Sprintf("%s:%s:%d", accountID, folderName, env.UID),
		AccountID:  accountID,
		FolderName: folderName,
		UID:        env.UID,
		MessageID:  env.MessageID,
		From:       env.From,
		To:         env.To,
		Subject:    env.Subject,
		Date:       env.Date,
		Flags:      env.Flags,
		Size:       env.Size,
		Priority:   priority,
		Status:     models.EnvelopeStatusPending,
		MaxRetries: defaultMaxRetries,
	}

	queueOpts := &envelopequeue.EnqueueOptions{
		Priority:   priority,
		MaxRetries: defaultMaxRetries,
	}

	return s.queue.Enqueue(ctx, queueItem, queueOpts)
}

// determineEnvelopePriority determines processing priority based on envelope characteristics.
func (s *SyncService) determineEnvelopePriority(folderName string, env *pool.MessageEnvelope) models.EnvelopeProcessingPriority {
	// High priority: UNSEEN messages in INBOX
	isInbox := folderName == "INBOX" || folderName == "\\Inbox"
	isUnseen := true // Assume unseen unless \Seen flag present
	isFlagged := false

	for _, flag := range env.Flags {
		if flag == "\\Seen" {
			isUnseen = false
			break // Early exit - no need to check remaining flags
		}
		if flag == "\\Flagged" {
			isFlagged = true
		}
	}

	// High priority conditions
	if isInbox && (isUnseen || isFlagged) {
		return models.PriorityHigh
	}

	// Normal priority: INBOX messages
	if isInbox {
		return models.PriorityNormal
	}

	// Low priority: Archive, Sent, or other folders
	return models.PriorityLow
}

// updateFolderSyncState updates the folder sync state after successful sync.
func (s *SyncService) updateFolderSyncState(ctx context.Context, accountID, folderName string, status *pool.FolderStatus, messagesSynced int) (int, error) {
	var state *models.FolderSyncState

	if status != nil {
		state = &models.FolderSyncState{
			AccountID:     accountID,
			FolderName:    folderName,
			UIDValidity:   status.UIDValidity,
			LastSyncedUID: status.UIDNext - 1, // UIDNext is the next unused UID
			LastSyncTime:  time.Now(),
			MessageCount:  status.Messages,
			IsInitialized: true,
		}
	} else {
		// Get existing state and update it
		existing, err := s.accountService.GetFolderSyncState(ctx, accountID, folderName)
		if err == nil && existing != nil {
			// Create a copy to avoid mutating the cached state
			state = &models.FolderSyncState{
				AccountID:     existing.AccountID,
				FolderName:    existing.FolderName,
				UIDValidity:   existing.UIDValidity,
				LastSyncedUID: existing.LastSyncedUID,
				LastSyncTime:  time.Now(),
				MessageCount:  existing.MessageCount,
				IsInitialized: existing.IsInitialized,
			}
		}
	}

	if state != nil {
		if err := s.accountService.UpsertFolderSyncState(ctx, state); err != nil {
			log.Printf("Failed to update folder sync state for %s/%s: %v", accountID, folderName, err)
			// Don't fail the sync, just log the error
		}
	}

	return messagesSynced, nil
}

// acquireIMAPClient acquires an IMAP client from the session pool.
func (s *SyncService) acquireIMAPClient(ctx context.Context, account *models.Account) (*pool.IMAPAdapter, func(), error) {
	imapConfig := pool.IMAPConfig{
		Host:       account.IMAPConfig.Host,
		Port:       account.IMAPConfig.Port,
		Username:   account.IMAPConfig.Username,
		Password:   account.IMAPConfig.Password,
		Encryption: account.IMAPConfig.Encryption,
	}

	return s.sessionPool.Acquire(ctx, account.ID, imapConfig)
}

// SyncOptions configures synchronization behavior.
type SyncOptions struct {
	// FullSync forces a complete re-sync of all messages
	FullSync bool

	// Folder limits sync to a specific folder (empty = all folders)
	Folder string

	// HistoricalScope limits how far back to sync (days).
	// During initial sync, this is used to estimate the number of messages to fetch
	// using a heuristic of ~30 messages per day. For example, a scope of 30 days
	// would fetch approximately 900 messages (capped at initialSyncBatchLimit).
	// Default is 30 days if not specified.
	HistoricalScope int

	// IncludeSpam includes spam/junk folder
	IncludeSpam bool

	// IncludeTrash includes trash folder
	IncludeTrash bool

	// FetchBody determines whether to fetch message bodies
	FetchBody bool

	// EnableLinkExtraction extracts links from message bodies
	EnableLinkExtraction bool

	// EnableAttachmentProcessing processes attachments
	EnableAttachmentProcessing bool

	// Timeout sets a maximum duration for the sync operation.
	// If zero, no timeout is applied.
	Timeout time.Duration
}

// SyncResult holds the result of a synchronization operation.
type SyncResult struct {
	// AccountID is the synchronized account
	AccountID string

	// MessagesSynced is the number of new messages found
	MessagesSynced int

	// FoldersSynced is the number of folders processed
	FoldersSynced int

	// EnvelopesEnqueued is the number of messages enqueued for processing
	EnvelopesEnqueued int

	// Errors contains any non-fatal errors encountered
	Errors []string

	Duration time.Duration
}

// FolderSyncState holds the sync state for a folder.
type FolderSyncState struct {
	AccountID     string
	FolderName    string
	UIDValidity   uint32
	LastSyncedUID uint32
	LastSyncTime  time.Time
	MessageCount  uint32
	IsInitialized bool
}
