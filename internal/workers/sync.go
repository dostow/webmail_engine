package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"webmail_engine/internal/taskmaster"
)

// SyncTask performs email synchronization for an account.
// This migrates the existing sync_worker logic to the taskmaster pattern.
type SyncTask struct {
	// SyncService handles the actual synchronization logic
	SyncService SyncService
}

// SyncService defines the interface for synchronization operations.
type SyncService interface {
	SyncAccount(ctx context.Context, accountID string, opts SyncOptions) (*SyncResult, error)
	SyncFolder(ctx context.Context, accountID, folderName string, opts SyncOptions) (*SyncResult, error)
	GetSyncState(ctx context.Context, accountID, folderName string) (*FolderSyncState, error)
}

// SyncOptions configures synchronization behavior.
type SyncOptions struct {
	// FullSync forces a complete re-sync of all messages
	FullSync bool `json:"full_sync"`

	// Folder limits sync to a specific folder (empty = all folders)
	Folder string `json:"folder,omitempty"`

	// HistoricalScope limits how far back to sync (days)
	HistoricalScope int `json:"historical_scope"`

	// IncludeSpam includes spam/junk folder
	IncludeSpam bool `json:"include_spam"`

	// IncludeTrash includes trash folder
	IncludeTrash bool `json:"include_trash"`

	// FetchBody determines whether to fetch message bodies
	FetchBody bool `json:"fetch_body"`

	// EnableLinkExtraction extracts links from message bodies
	EnableLinkExtraction bool `json:"enable_link_extraction"`

	// EnableAttachmentProcessing processes attachments
	EnableAttachmentProcessing bool `json:"enable_attachment_processing"`
}

// SyncResult holds the result of a synchronization operation.
type SyncResult struct {
	// AccountID is the synchronized account
	AccountID string `json:"account_id"`

	// MessagesSynced is the number of new messages found
	MessagesSynced int `json:"messages_synced"`

	// FoldersSynced is the number of folders processed
	FoldersSynced int `json:"folders_synced"`

	// EnvelopesEnqueued is the number of messages enqueued for processing
	EnvelopesEnqueued int `json:"envelopes_enqueued"`

	// Duration is how long the sync took
	Duration time.Duration `json:"duration"`

	// Errors contains any non-fatal errors encountered
	Errors []string `json:"errors,omitempty"`
}

// FolderSyncState holds the sync state for a folder.
type FolderSyncState struct {
	AccountID     string    `json:"account_id"`
	FolderName    string    `json:"folder_name"`
	UIDValidity   uint32    `json:"uid_validity"`
	LastSyncedUID uint32    `json:"last_synced_uid"`
	LastSyncTime  time.Time `json:"last_sync_time"`
	MessageCount  int       `json:"message_count"`
	IsInitialized bool      `json:"is_initialized"`
}

// SyncPayload is the payload for sync tasks.
type SyncPayload struct {
	// AccountID is the account to synchronize
	AccountID string `json:"account_id"`

	// Options configures the sync behavior
	Options SyncOptions `json:"options,omitempty"`

	// Priority sets the sync priority (high, normal, low)
	Priority string `json:"priority,omitempty"`
}

// ID returns the unique task identifier.
func (t *SyncTask) ID() string {
	return "sync"
}

// Execute performs email synchronization for an account.
func (t *SyncTask) Execute(ctx context.Context, payload []byte) error {
	startTime := time.Now()

	// Parse payload
	var req SyncPayload
	if err := json.Unmarshal(payload, &req); err != nil {
		return taskmaster.NewNonRetryableTaskError(t.ID(), "invalid payload format", err)
	}

	// Validate required fields
	if req.AccountID == "" {
		return taskmaster.NewNonRetryableTaskError(t.ID(), "account_id is required", nil)
	}

	// Check if service is available
	if t.SyncService == nil {
		return taskmaster.NewSystemTaskError(t.ID(), "sync service not configured", nil)
	}

	// Apply default options
	opts := req.Options
	if opts.HistoricalScope <= 0 {
		opts.HistoricalScope = 30 // Default: 30 days
	}

	// Log sync start
	fmt.Printf("Starting sync for account %s (full_sync=%v, folder=%s)\n",
		req.AccountID, opts.FullSync, opts.Folder)

	// Perform sync
	var result *SyncResult
	var err error

	if opts.Folder != "" {
		// Sync specific folder
		result, err = t.SyncService.SyncFolder(ctx, req.AccountID, opts.Folder, opts)
	} else {
		// Sync all folders
		result, err = t.SyncService.SyncAccount(ctx, req.AccountID, opts)
	}

	if err != nil {
		return taskmaster.WrapError(t.ID(), "sync failed", err)
	}

	// Log result
	fmt.Printf("Sync completed for account %s: %d messages, %d folders, %d envelopes enqueued (took %v)\n",
		req.AccountID, result.MessagesSynced, result.FoldersSynced, result.EnvelopesEnqueued, time.Since(startTime))

	// Log any errors
	for _, errMsg := range result.Errors {
		fmt.Printf("Sync warning for account %s: %s\n", req.AccountID, errMsg)
	}

	return nil
}

// Ensure interface compliance
var _ taskmaster.Task = (*SyncTask)(nil)
