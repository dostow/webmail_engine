package envelopequeue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"webmail_engine/internal/models"
	"webmail_engine/internal/taskmaster"
)

// TaskmasterEnvelopeQueue is an envelope queue implementation that dispatches
// sync tasks to the taskmaster dispatcher instead of storing envelopes directly.
// This allows the taskmaster system to handle task scheduling and execution.
type TaskmasterEnvelopeQueue struct {
	dispatcher taskmaster.TaskDispatcher
	taskID     string
	closed     bool
}

// TaskmasterQueueConfig holds configuration for the taskmaster envelope queue
type TaskmasterQueueConfig struct {
	// TaskID is the task identifier to dispatch (default: "sync")
	TaskID string
}

// DefaultTaskmasterQueueConfig returns default configuration
func DefaultTaskmasterQueueConfig() *TaskmasterQueueConfig {
	return &TaskmasterQueueConfig{
		TaskID: "sync",
	}
}

// NewTaskmasterEnvelopeQueue creates a new envelope queue that uses taskmaster dispatch
func NewTaskmasterEnvelopeQueue(dispatcher taskmaster.TaskDispatcher, cfg *TaskmasterQueueConfig) *TaskmasterEnvelopeQueue {
	if cfg == nil {
		cfg = DefaultTaskmasterQueueConfig()
	}
	return &TaskmasterEnvelopeQueue{
		dispatcher: dispatcher,
		taskID:     cfg.TaskID,
	}
}

// Enqueue dispatches a sync task to the taskmaster dispatcher.
// Instead of storing the envelope, it creates a task that will process the envelope.
func (q *TaskmasterEnvelopeQueue) Enqueue(ctx context.Context, envelope *models.EnvelopeQueueItem, opts *EnqueueOptions) error {
	if q.closed {
		return ErrQueueUnavailable
	}

	// Create sync task payload with the already-fetched envelope data
	payload := SyncTaskPayload{
		AccountID:  envelope.AccountID,
		FolderName: envelope.FolderName,
		UIDs:       []uint32{envelope.UID},
		Options: SyncOptions{
			FetchBody: false, // Envelope already fetched by sync service
		},
		// Include envelope metadata that was already fetched
		Envelopes: []EnvelopeMetadata{
			{
				UID:       envelope.UID,
				MessageID: envelope.MessageID,
				From:      envelope.From,
				To:        envelope.To,
				Subject:   envelope.Subject,
				Date:      envelope.Date,
				Flags:     envelope.Flags,
				Size:      envelope.Size,
				Priority:  envelope.Priority,
			},
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal sync task payload: %w", err)
	}

	// Dispatch task to taskmaster
	if err := q.dispatcher.Dispatch(ctx, q.taskID, payloadBytes); err != nil {
		return fmt.Errorf("failed to dispatch sync task: %w", err)
	}

	return nil
}

// Close marks the queue as closed.
func (q *TaskmasterEnvelopeQueue) Close() error {
	q.closed = true
	return nil
}

// SyncTaskPayload represents the payload for a sync task
type SyncTaskPayload struct {
	AccountID  string             `json:"account_id"`
	FolderName string             `json:"folder_name"`
	UIDs       []uint32           `json:"uids"`
	Options    SyncOptions        `json:"options"`
	Envelopes  []EnvelopeMetadata `json:"envelopes,omitempty"` // Already-fetched envelope data
}

// EnvelopeMetadata contains envelope data that was already fetched by the sync service
type EnvelopeMetadata struct {
	UID       uint32                            `json:"uid"`
	MessageID string                            `json:"message_id"`
	From      []models.Contact                  `json:"from"`
	To        []models.Contact                  `json:"to"`
	Subject   string                            `json:"subject"`
	Date      time.Time                         `json:"date"`
	Flags     []string                          `json:"flags"`
	Size      int64                             `json:"size"`
	Priority  models.EnvelopeProcessingPriority `json:"priority"`
}

// SyncOptions represents sync options for the task payload
type SyncOptions struct {
	FetchBody bool `json:"fetch_body"`
}

// ensure interface compliance
var _ EnvelopeQueue = (*TaskmasterEnvelopeQueue)(nil)
