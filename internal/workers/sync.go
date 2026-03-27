package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"webmail_engine/internal/service"
	"webmail_engine/internal/taskmaster"
)

// SyncTask performs email synchronization for an account.
// This migrates the existing sync_worker logic to the taskmaster pattern.
type SyncTask struct {
	// SyncService handles the actual synchronization logic
	SyncService SyncService
}

// SyncService defines the interface for synchronization operations.
// This matches the service.SyncService interface.
type SyncService interface {
	SyncAccount(ctx context.Context, accountID string, opts service.SyncOptions) (*service.SyncResult, error)
	SyncFolder(ctx context.Context, accountID, folderName string, opts service.SyncOptions) (*service.SyncResult, error)
	GetSyncState(ctx context.Context, accountID, folderName string) (*service.FolderSyncState, error)
}

// SyncPayload is the payload for sync tasks.
type SyncPayload struct {
	// AccountID is the account to synchronize
	AccountID string `json:"account_id"`

	// Options configures the sync behavior
	Options service.SyncOptions `json:"options,omitempty"`

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

	// Check context before starting
	select {
	case <-ctx.Done():
		fmt.Printf("Sync task cancelled before starting for payload: %s\n", string(payload))
		return ctx.Err()
	default:
	}

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
	var result *service.SyncResult
	var err error

	if opts.Folder != "" {
		// Sync specific folder
		result, err = t.SyncService.SyncFolder(ctx, req.AccountID, opts.Folder, opts)
	} else {
		// Sync all folders
		result, err = t.SyncService.SyncAccount(ctx, req.AccountID, opts)
	}

	// Check if error is due to context cancellation
	if err != nil {
		if ctx.Err() != nil {
			fmt.Printf("Sync interrupted by shutdown for account %s (duration: %v)\n",
				req.AccountID, time.Since(startTime))
			return fmt.Errorf("sync cancelled: %w", ctx.Err())
		}
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
