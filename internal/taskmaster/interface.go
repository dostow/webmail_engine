// Package taskmaster provides a centralized task management system for the webmail engine.
// It cleanly separates task execution from dispatch mechanisms, supporting multiple
// execution modes (REST API, Machinery v2, and managed worker pools).
//
// The package follows modern Go idioms with clean architecture principles:
// - Domain logic lives in internal/workers
// - Infrastructure and orchestration live in taskmaster
// - All operations support context propagation for cancellation
//
// Taskmaster supports two primary use cases:
// 1. Task Execution (processor_worker): Execute tasks from a queue or HTTP requests
// 2. Task Creation (sync_worker): Create and schedule periodic tasks for accounts
package taskmaster

import (
	"context"
	"time"
)

// Task defines the core domain logic contract for executable units.
// Implementations should contain business logic and be placed in internal/workers.
type Task interface {
	// ID returns a unique identifier for this task type.
	// This ID is used for routing and registration.
	ID() string

	// Execute performs the domain-specific work with the provided payload.
	// The payload format is task-specific and should be documented by each implementation.
	// Context propagation allows for cancellation and timeout handling.
	Execute(ctx context.Context, payload []byte) error
}

// Dispatcher defines the lifecycle management and task distribution contract.
// It supports multiple execution modes and provides a unified interface for task dispatching.
type Dispatcher interface {
	// Register associates a Task implementation with its ID.
	// Returns an error if a task with the same ID is already registered.
	Register(task Task) error

	// Start initializes the dispatcher and any underlying systems.
	// This includes starting worker pools, setting up queues, or initializing HTTP servers.
	// Must be called before Dispatch.
	Start(ctx context.Context) error

	// Stop gracefully shuts down the dispatcher and cleans up resources.
	// It stops accepting new tasks and waits for in-progress tasks to complete.
	// The context timeout controls how long to wait for graceful shutdown.
	Stop(ctx context.Context) error

	// Dispatch sends a task for execution with the given payload.
	// The taskID must match a registered task implementation.
	// Returns ErrTaskNotFound if the task is not registered.
	// Returns ErrDispatcherNotStarted if Start has not been called.
	Dispatch(ctx context.Context, taskID string, payload []byte) error

	// DispatchMultiple sends tasks to multiple task handlers in sequence.
	// Each task is dispatched with its own payload.
	// Returns an error if any dispatch fails (previous dispatches are not rolled back).
	DispatchMultiple(ctx context.Context, tasks []TaskDispatch) error

	// IsRunning returns true if the dispatcher is currently running.
	IsRunning() bool

	// Mode returns the current execution mode of the dispatcher.
	Mode() ExecutionMode
}

// TaskDispatch represents a single task dispatch request.
type TaskDispatch struct {
	TaskID  string `json:"task_id"`
	Payload []byte `json:"payload"`
}

// TaskCreator defines the contract for creating and scheduling tasks.
// This is used by the sync worker to create periodic sync tasks.
type TaskCreator interface {
	// CreateTask creates a new task for immediate or scheduled execution.
	// Returns the task ID for tracking.
	CreateTask(ctx context.Context, taskID string, payload []byte, opts *CreateTaskOptions) (string, error)

	// ScheduleTask creates a recurring task that runs at the specified interval.
	// Returns a schedule ID that can be used to cancel the schedule.
	ScheduleTask(ctx context.Context, taskID string, payload []byte, interval time.Duration, opts *ScheduleTaskOptions) (string, error)

	// CancelSchedule stops a scheduled task from running.
	CancelSchedule(ctx context.Context, scheduleID string) error

	// GetSchedule returns information about a scheduled task.
	GetSchedule(ctx context.Context, scheduleID string) (*ScheduleInfo, error)
}

// CreateTaskOptions holds options for task creation.
type CreateTaskOptions struct {
	// Priority sets the task priority (high, normal, low).
	Priority string

	// Delay specifies a delay before the task should execute.
	Delay time.Duration

	// MaxRetries overrides the default retry count.
	MaxRetries int

	// Metadata holds arbitrary task metadata.
	Metadata map[string]string
}

// ScheduleTaskOptions holds options for task scheduling.
type ScheduleTaskOptions struct {
	// Name is a human-readable name for the schedule.
	Name string

	// StartAt specifies when the schedule should start (default: immediately).
	StartAt time.Time

	// EndAt specifies when the schedule should stop (optional).
	EndAt time.Time

	// MaxExecutions limits the number of times the task runs (0 = unlimited).
	MaxExecutions int

	// Metadata holds arbitrary schedule metadata.
	Metadata map[string]string
}

// ScheduleInfo holds information about a scheduled task.
type ScheduleInfo struct {
	// ID is the unique schedule identifier.
	ID string `json:"id"`

	// TaskID is the type of task being scheduled.
	TaskID string `json:"task_id"`

	// Name is the human-readable schedule name.
	Name string `json:"name"`

	// Interval is the time between executions.
	Interval time.Duration `json:"interval"`

	// NextRun is when the task will next execute.
	NextRun time.Time `json:"next_run"`

	// LastRun is when the task last executed (if applicable).
	LastRun *time.Time `json:"last_run,omitempty"`

	// ExecutionCount is how many times the task has run.
	ExecutionCount int `json:"execution_count"`

	// MaxExecutions is the maximum number of executions (0 = unlimited).
	MaxExecutions int `json:"max_executions"`

	// IsActive indicates if the schedule is currently active.
	IsActive bool `json:"is_active"`

	// CreatedAt is when the schedule was created.
	CreatedAt time.Time `json:"created_at"`
}

// FullDispatcher combines Dispatcher and TaskCreator capabilities.
// Implementations may support both task execution and task creation.
type FullDispatcher interface {
	Dispatcher
	TaskCreator
}

// ExecutionMode defines the available task execution modes.
type ExecutionMode int

const (
	// ManagedMode uses an internal worker pool with goroutines and channels.
	// Best for standalone services with predictable workloads.
	ManagedMode ExecutionMode = iota

	// RESTMode exposes tasks via HTTP endpoints.
	// Best for microservices architecture or external integrations.
	RESTMode

	// MachineryMode integrates with Machinery v2 for distributed task queues.
	// Best for scalable, distributed task processing with Redis/RabbitMQ backends.
	MachineryMode
)

// String returns a human-readable representation of the execution mode.
func (m ExecutionMode) String() string {
	switch m {
	case ManagedMode:
		return "managed"
	case RESTMode:
		return "rest"
	case MachineryMode:
		return "machinery"
	default:
		return "unknown"
	}
}

// TaskRequest represents a task execution request within the managed worker pool.
type TaskRequest struct {
	// Ctx carries the request context for cancellation and timeouts.
	Ctx context.Context

	// TaskID identifies the type of task to execute.
	TaskID string

	// Payload contains the task-specific data.
	Payload []byte

	// ResultChan receives the execution result (optional, for synchronous execution).
	ResultChan chan<- TaskResult
}

// TaskResult represents the result of a task execution.
type TaskResult struct {
	// TaskID identifies the task that was executed.
	TaskID string

	// Err contains any error that occurred during execution.
	// nil indicates successful completion.
	Err error
}

// DispatcherConfig holds configuration for the dispatcher.
type DispatcherConfig struct {
	// Mode specifies the execution mode (default: ManagedMode).
	Mode ExecutionMode

	// WorkerCount is the number of worker goroutines for ManagedMode.
	// Ignored in other modes.
	WorkerCount int

	// TaskTimeout is the default timeout for task execution.
	// Individual tasks can override this with their own timeout settings.
	TaskTimeout time.Duration

	// QueueSize is the size of the task queue buffer for ManagedMode.
	// A value of 0 uses a default size.
	QueueSize int

	// RESTConfig holds configuration for RESTMode.
	RESTConfig *RESTConfig

	// MachineryConfig holds configuration for MachineryMode.
	MachineryConfig *MachineryConfig
}

// RESTConfig holds configuration for REST mode.
type RESTConfig struct {
	// Addr is the HTTP server address (e.g., ":8080").
	Addr string

	// BasePath is the URL prefix for task endpoints (e.g., "/api/v1/tasks").
	BasePath string

	// EnableSyncExecution enables synchronous execution (wait for completion).
	// When false, tasks are executed asynchronously and return immediately.
	EnableSyncExecution bool
}

// MachineryConfig holds configuration for Machinery mode.
type MachineryConfig struct {
	// BrokerURL is the message broker URL (e.g., "redis://localhost:6379").
	BrokerURL string

	// ResultBackend is the result backend URL.
	ResultBackend string

	// DefaultQueue is the default queue name for tasks.
	DefaultQueue string

	// DefaultRetryCount is the default number of retries for failed tasks.
	DefaultRetryCount int
}

// DefaultDispatcherConfig returns a DispatcherConfig with sensible defaults.
func DefaultDispatcherConfig() *DispatcherConfig {
	return &DispatcherConfig{
		Mode:        ManagedMode,
		WorkerCount: 4,
		TaskTimeout: 30 * time.Second,
		QueueSize:   100,
		RESTConfig: &RESTConfig{
			Addr:                ":8080",
			BasePath:            "/api/v1/tasks",
			EnableSyncExecution: false,
		},
		MachineryConfig: &MachineryConfig{
			BrokerURL:         "redis://localhost:6379",
			ResultBackend:     "redis://localhost:6379",
			DefaultQueue:      "tasks",
			DefaultRetryCount: 3,
		},
	}
}
