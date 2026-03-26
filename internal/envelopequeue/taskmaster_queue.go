package envelopequeue

import (
	"context"
	"encoding/json"
	"fmt"

	"webmail_engine/internal/models"
	"webmail_engine/internal/taskmaster"
)

// TaskmasterEnvelopeQueue is an envelope queue implementation that dispatches
// tasks to the taskmaster dispatcher instead of storing envelopes directly.
// It supports a configurable pipeline of tasks that each envelope must go through.
type TaskmasterEnvelopeQueue struct {
	dispatcher taskmaster.TaskDispatcher
	tasks      []TaskRoute
	closed     bool
}

// TaskRoute defines a task in the processing pipeline
type TaskRoute struct {
	TaskID  string                 `json:"task_id"`
	Enabled bool                   `json:"enabled"`
	Config  map[string]interface{} `json:"config,omitempty"`
}

// TaskmasterQueueConfig holds configuration for the taskmaster envelope queue
type TaskmasterQueueConfig struct {
	// Tasks is the pipeline of tasks to dispatch for each envelope
	// Each envelope will be dispatched to all enabled tasks in order
	Tasks []TaskRoute `json:"tasks"`
}

// DefaultTaskmasterQueueConfig returns default configuration with a single envelope_processor task
func DefaultTaskmasterQueueConfig() *TaskmasterQueueConfig {
	return &TaskmasterQueueConfig{
		Tasks: []TaskRoute{
			{
				TaskID:  "envelope_processor",
				Enabled: true,
			},
		},
	}
}

// NewTaskmasterEnvelopeQueue creates a new envelope queue that uses taskmaster dispatch
func NewTaskmasterEnvelopeQueue(dispatcher taskmaster.TaskDispatcher, cfg *TaskmasterQueueConfig) *TaskmasterEnvelopeQueue {
	if cfg == nil {
		cfg = DefaultTaskmasterQueueConfig()
	}

	// Filter to only enabled tasks
	var enabledTasks []TaskRoute
	for _, task := range cfg.Tasks {
		if task.Enabled {
			enabledTasks = append(enabledTasks, task)
		}
	}

	return &TaskmasterEnvelopeQueue{
		dispatcher: dispatcher,
		tasks:      enabledTasks,
	}
}

// Enqueue creates tasks for all configured workers in the pipeline.
// Each envelope is dispatched to every enabled task using CreateTaskMultiple for tracking.
// Returns error if task creation fails for any worker.
func (q *TaskmasterEnvelopeQueue) Enqueue(ctx context.Context, envelope *models.EnvelopeQueueItem, opts *EnqueueOptions) error {
	if q.closed {
		return ErrQueueUnavailable
	}

	if len(q.tasks) == 0 {
		return fmt.Errorf("no tasks configured in pipeline")
	}

	// Build all task creations
	taskCreations := make([]taskmaster.TaskCreation, 0, len(q.tasks))
	for _, taskRoute := range q.tasks {
		payload, err := q.buildPayload(taskRoute, envelope, opts)
		if err != nil {
			return fmt.Errorf("failed to build payload for task %s: %w", taskRoute.TaskID, err)
		}

		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload for task %s: %w", taskRoute.TaskID, err)
		}

		// Build CreateTaskOptions from EnqueueOptions
		createOpts := &taskmaster.CreateTaskOptions{
			Metadata: map[string]string{
				"envelope_id": envelope.ID,
				"account_id":  envelope.AccountID,
				"folder_name": envelope.FolderName,
				"priority":    string(envelope.Priority),
			},
		}

		if opts != nil {
			if opts.Delay > 0 {
				createOpts.Delay = opts.Delay
			}
			if opts.MaxRetries > 0 {
				createOpts.MaxRetries = opts.MaxRetries
			}
		}

		taskCreations = append(taskCreations, taskmaster.TaskCreation{
			TaskID:  taskRoute.TaskID,
			Payload: payloadBytes,
			Options: createOpts,
		})
	}

	// Create all tasks at once using the dispatcher's batch create method
	taskIDs, err := q.dispatcher.CreateTaskMultiple(ctx, taskCreations)
	if err != nil {
		return fmt.Errorf("failed to create envelope tasks: %w", err)
	}

	// Log task IDs for tracking (optional, can be removed in production)
	if len(taskIDs) > 0 {
		// Tasks created successfully - taskIDs can be stored for tracking if needed
		_ = taskIDs
	}

	return nil
}

// EnvelopePayload is the standard payload structure for envelope-related tasks
type EnvelopePayload struct {
	EnvelopeID string                 `json:"envelope_id"`
	AccountID  string                 `json:"account_id"`
	FolderName string                 `json:"folder_name"`
	UID        uint32                 `json:"uid"`
	MessageID  string                 `json:"message_id,omitempty"`
	Options    map[string]interface{} `json:"options,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// buildPayload creates a task-specific payload based on the route configuration
func (q *TaskmasterEnvelopeQueue) buildPayload(route TaskRoute, envelope *models.EnvelopeQueueItem, opts *EnqueueOptions) (map[string]interface{}, error) {
	// Base payload with envelope information
	basePayload := EnvelopePayload{
		EnvelopeID: envelope.ID,
		AccountID:  envelope.AccountID,
		FolderName: envelope.FolderName,
		UID:        envelope.UID,
		MessageID:  envelope.MessageID,
		Options:    make(map[string]interface{}),
		Metadata:   make(map[string]interface{}),
	}

	// Apply route-specific config
	if route.Config != nil {
		// Merge config into options
		for k, v := range route.Config {
			basePayload.Options[k] = v
		}
	}

	// Add enqueue options if provided
	if opts != nil {
		basePayload.Metadata["priority"] = string(opts.Priority)
		basePayload.Metadata["max_retries"] = opts.MaxRetries
		if opts.Delay > 0 {
			basePayload.Options["delay"] = opts.Delay.String()
		}
	}

	// Convert to generic map for JSON marshaling
	payloadBytes, err := json.Marshal(basePayload)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(payloadBytes, &result); err != nil {
		return nil, err
	}

	return result, nil
}

// Close marks the queue as closed.
func (q *TaskmasterEnvelopeQueue) Close() error {
	q.closed = true
	return nil
}

// ensure interface compliance
var _ EnvelopeQueue = (*TaskmasterEnvelopeQueue)(nil)
