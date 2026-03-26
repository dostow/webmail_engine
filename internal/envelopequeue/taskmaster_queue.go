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

// Enqueue dispatches the envelope to all configured tasks in the pipeline.
// Each envelope is dispatched to every enabled task sequentially.
// If any task dispatch fails, the error is returned but previous dispatches are not rolled back.
func (q *TaskmasterEnvelopeQueue) Enqueue(ctx context.Context, envelope *models.EnvelopeQueueItem, opts *EnqueueOptions) error {
	if q.closed {
		return ErrQueueUnavailable
	}

	if len(q.tasks) == 0 {
		return fmt.Errorf("no tasks configured in pipeline")
	}

	// Build all task dispatches
	dispatches := make([]taskmaster.TaskDispatch, 0, len(q.tasks))
	for _, taskRoute := range q.tasks {
		payload, err := q.buildPayload(taskRoute, envelope, opts)
		if err != nil {
			return fmt.Errorf("failed to build payload for task %s: %w", taskRoute.TaskID, err)
		}

		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload for task %s: %w", taskRoute.TaskID, err)
		}

		dispatches = append(dispatches, taskmaster.TaskDispatch{
			TaskID:  taskRoute.TaskID,
			Payload: payloadBytes,
		})
	}

	// Dispatch all tasks at once using the dispatcher's multi-dispatch method
	if err := q.dispatcher.DispatchMultiple(ctx, dispatches); err != nil {
		return fmt.Errorf("failed to dispatch envelope tasks: %w", err)
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
