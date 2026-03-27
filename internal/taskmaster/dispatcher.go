package taskmaster

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Logger defines the interface for structured logging.
// This allows dependency injection of custom loggers (e.g., zap, logrus).
type Logger interface {
	Info(msg string, keysAndValues ...interface{})
	Error(msg string, keysAndValues ...interface{})
	Debug(msg string, keysAndValues ...interface{})
	Warn(msg string, keysAndValues ...interface{})
}

// defaultLogger is a simple logger that wraps the standard log package.
type defaultLogger struct{}

func (l *defaultLogger) Info(msg string, keysAndValues ...interface{}) {
	l.log("INFO", msg, keysAndValues...)
}

func (l *defaultLogger) Error(msg string, keysAndValues ...interface{}) {
	l.log("ERROR", msg, keysAndValues...)
}

func (l *defaultLogger) Debug(msg string, keysAndValues ...interface{}) {
	l.log("DEBUG", msg, keysAndValues...)
}

func (l *defaultLogger) Warn(msg string, keysAndValues ...interface{}) {
	l.log("WARN", msg, keysAndValues...)
}

func (l *defaultLogger) log(level, msg string, keysAndValues ...interface{}) {
	prefix := fmt.Sprintf("[%s] %s: %s", level, time.Now().Format(time.RFC3339), msg)

	if len(keysAndValues) == 0 {
		log.Print(prefix)
		return
	}

	// Format key-value pairs properly
	kvStr := formatKeyValuePairs(keysAndValues)
	log.Printf("%s %s", prefix, kvStr)
}

// formatKeyValuePairs formats key-value pairs in a readable "key=value" format.
// If the number of keysAndValues is odd, the last value is printed as-is.
func formatKeyValuePairs(keysAndValues []interface{}) string {
	if len(keysAndValues) == 0 {
		return ""
	}

	result := make([]string, 0, len(keysAndValues)/2+1)

	for i := 0; i < len(keysAndValues); i += 2 {
		if i+1 < len(keysAndValues) {
			// We have both key and value
			result = append(result, fmt.Sprintf("%v=%v", keysAndValues[i], keysAndValues[i+1]))
		} else {
			// Odd number of arguments, print the last one as-is
			result = append(result, fmt.Sprintf("%v", keysAndValues[i]))
		}
	}

	return fmt.Sprintf("[%s]", joinStrings(result, " "))
}

// joinStrings joins a slice of strings with the given separator.
func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for _, s := range strs[1:] {
		result += sep + s
	}
	return result
}

// DispatcherImpl is the core dispatcher implementation.
// It manages task registration, routing, and execution mode switching.
type DispatcherImpl struct {
	mu       sync.RWMutex
	config   *DispatcherConfig
	tasks    map[string]Task
	logger   Logger
	mode     ExecutionMode
	running  bool
	started  chan struct{}
	stopOnce sync.Once

	// Mode-specific components
	managedDispatcher   *ManagedDispatcher
	restDispatcher      *RESTDispatcher
	machineryDispatcher *MachineryDispatcher
	scheduler           *TaskScheduler
}

// Option defines a functional option for configuring the dispatcher.
type Option func(*DispatcherImpl)

// WithMode sets the execution mode for the dispatcher.
func WithMode(mode ExecutionMode) Option {
	return func(d *DispatcherImpl) {
		d.config.Mode = mode
	}
}

// WithWorkerCount sets the number of worker goroutines for ManagedMode.
func WithWorkerCount(count int) Option {
	return func(d *DispatcherImpl) {
		d.config.WorkerCount = count
	}
}

// WithTaskTimeout sets the default timeout for task execution.
func WithTaskTimeout(timeout time.Duration) Option {
	return func(d *DispatcherImpl) {
		d.config.TaskTimeout = timeout
	}
}

// WithQueueSize sets the task queue buffer size for ManagedMode.
func WithQueueSize(size int) Option {
	return func(d *DispatcherImpl) {
		d.config.QueueSize = size
	}
}

// WithRESTConfig sets the configuration for RESTMode.
func WithRESTConfig(cfg *RESTConfig) Option {
	return func(d *DispatcherImpl) {
		d.config.RESTConfig = cfg
	}
}

// WithMachineryConfig sets the configuration for MachineryMode.
func WithMachineryConfig(cfg *MachineryConfig) Option {
	return func(d *DispatcherImpl) {
		d.config.MachineryConfig = cfg
	}
}

// WithLogger sets a custom logger for the dispatcher.
func WithLogger(logger Logger) Option {
	return func(d *DispatcherImpl) {
		d.logger = logger
	}
}

// WithDispatchOnly sets the dispatcher to dispatch-only mode for Machinery mode.
// In this mode, the dispatcher can send tasks to the Machinery queue but won't
// consume tasks locally (no worker started). Useful for push-only scenarios.
func WithDispatchOnly(dispatchOnly bool) Option {
	return func(d *DispatcherImpl) {
		d.config.DispatchOnly = dispatchOnly
	}
}

// NewDispatcher creates a new Dispatcher with the given options.
// The dispatcher must be started with Start() before tasks can be dispatched.
//
// Example:
//
//	dispatcher := taskmaster.NewDispatcher(
//	    taskmaster.WithMode(taskmaster.ManagedMode),
//	    taskmaster.WithWorkerCount(10),
//	    taskmaster.WithTaskTimeout(30*time.Second),
//	    taskmaster.WithLogger(zapLogger),
//	)
func NewDispatcher(opts ...Option) *DispatcherImpl {
	// Start with default config
	d := &DispatcherImpl{
		config:  DefaultDispatcherConfig(),
		tasks:   make(map[string]Task),
		logger:  &defaultLogger{},
		started: make(chan struct{}),
	}

	// Apply options
	for _, opt := range opts {
		opt(d)
	}

	d.mode = d.config.Mode

	return d
}

// Register associates a Task implementation with its ID.
// Returns an error if a task with the same ID is already registered.
// Registration should happen before Start() is called.
func (d *DispatcherImpl) Register(task Task) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if task == nil {
		return NewNonRetryableTaskError("", "cannot register nil task", nil)
	}

	taskID := task.ID()
	if taskID == "" {
		return NewNonRetryableTaskError("", "task ID cannot be empty", nil)
	}

	if _, exists := d.tasks[taskID]; exists {
		return fmt.Errorf("%w: %s", ErrTaskAlreadyRegistered, taskID)
	}

	d.tasks[taskID] = task
	d.logger.Debug("Registered task", "task_id", taskID)

	return nil
}

// Start initializes the dispatcher and any underlying systems.
// This includes starting worker pools, setting up queues, or initializing HTTP servers.
// Must be called before Dispatch().
func (d *DispatcherImpl) Start(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.running {
		return ErrDispatcherAlreadyStarted
	}

	d.logger.Info("Starting dispatcher", "mode", d.mode.String())

	// Initialize mode-specific dispatcher
	var err error
	switch d.mode {
	case ManagedMode:
		err = d.startManagedMode(ctx)
	case RESTMode:
		err = d.startRESTMode(ctx)
	case MachineryMode:
		err = d.startMachineryMode(ctx)
	default:
		return NewSystemTaskError("", fmt.Sprintf("unknown execution mode: %d", d.mode), nil)
	}

	if err != nil {
		return err
	}

	d.running = true
	close(d.started)
	d.logger.Info("Dispatcher started successfully", "mode", d.mode.String())

	return nil
}

// Stop gracefully shuts down the dispatcher and cleans up resources.
// It stops accepting new tasks and waits for in-progress tasks to complete.
// The context timeout controls how long to wait for graceful shutdown.
func (d *DispatcherImpl) Stop(ctx context.Context) error {
	d.mu.RLock()
	if !d.running {
		d.mu.RUnlock()
		return nil // Already stopped
	}
	d.mu.RUnlock()

	var stopErr error
	d.stopOnce.Do(func() {
		d.logger.Info("Stopping dispatcher...")

		// Stop mode-specific dispatcher
		switch d.mode {
		case ManagedMode:
			stopErr = d.stopManagedMode(ctx)
		case RESTMode:
			stopErr = d.stopRESTMode(ctx)
		case MachineryMode:
			stopErr = d.stopMachineryMode(ctx)
		}

		d.mu.Lock()
		d.running = false
		d.mu.Unlock()

		if stopErr != nil {
			d.logger.Error("Dispatcher stopped with errors", "error", stopErr)
		} else {
			d.logger.Info("Dispatcher stopped successfully")
		}
	})

	return stopErr
}

// Dispatch sends a task for execution with the given payload.
// The taskID must match a registered task implementation.
// Returns ErrTaskNotFound if the task is not registered.
// Returns ErrDispatcherNotStarted if Start has not been called.
func (d *DispatcherImpl) Dispatch(ctx context.Context, taskID string, payload []byte) error {
	d.mu.RLock()
	running := d.running
	d.mu.RUnlock()

	if !running {
		// Check if we're in the process of starting
		select {
		case <-d.started:
			// Started successfully
		default:
			return ErrDispatcherNotStarted
		}
	}

	// Validate task is registered
	d.mu.RLock()

	_, exists := d.tasks[taskID]
	d.mu.RUnlock()

	if !exists {
		return fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}

	// Dispatch to mode-specific handler
	switch d.mode {
	case ManagedMode:
		return d.dispatchManaged(ctx, taskID, payload)
	case RESTMode:
		return d.dispatchREST(ctx, taskID, payload)
	case MachineryMode:
		return d.dispatchMachinery(ctx, taskID, payload)
	default:
		return NewSystemTaskError(taskID, "unknown execution mode", nil)
	}
}

// DispatchMultiple sends tasks to multiple task handlers in sequence.
// Each task is dispatched with its own payload.
// Returns an error if any dispatch fails (previous dispatches are not rolled back).
func (d *DispatcherImpl) DispatchMultiple(ctx context.Context, tasks []TaskDispatch) error {
	if len(tasks) == 0 {
		return nil
	}

	var errs []error
	for _, task := range tasks {
		if err := d.Dispatch(ctx, task.TaskID, task.Payload); err != nil {
			errs = append(errs, fmt.Errorf("task %s: %w", task.TaskID, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("dispatch multiple failed: %v", errs)
	}
	return nil
}

// IsRunning returns true if the dispatcher is currently running.
func (d *DispatcherImpl) IsRunning() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.running
}

// Mode returns the current execution mode of the dispatcher.
func (d *DispatcherImpl) Mode() ExecutionMode {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.mode
}

// GetTask returns a registered task by ID (for testing and inspection).
func (d *DispatcherImpl) GetTask(taskID string) (Task, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	task, exists := d.tasks[taskID]
	return task, exists
}

// GetRegisteredTasks returns a list of all registered task IDs.
func (d *DispatcherImpl) GetRegisteredTasks() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	ids := make([]string, 0, len(d.tasks))
	for id := range d.tasks {
		ids = append(ids, id)
	}
	return ids
}

// CreateTask creates a new task for immediate or scheduled execution.
// This implements the TaskCreator interface.
func (d *DispatcherImpl) CreateTask(ctx context.Context, taskID string, payload []byte, opts *CreateTaskOptions) (string, error) {
	if d.scheduler != nil {
		return d.scheduler.CreateTask(ctx, taskID, payload, opts)
	}

	// Fallback: dispatch immediately
	if err := d.Dispatch(ctx, taskID, payload); err != nil {
		return "", err
	}
	return uuid.New().String(), nil
}

// ScheduleTask creates a recurring task that runs at the specified interval.
// This implements the TaskCreator interface.
func (d *DispatcherImpl) ScheduleTask(ctx context.Context, taskID string, payload []byte, interval time.Duration, opts *ScheduleTaskOptions) (string, error) {
	if d.scheduler == nil {
		return "", NewSystemTaskError("", "scheduler not initialized", nil)
	}
	return d.scheduler.ScheduleTask(ctx, taskID, payload, interval, opts)
}

// CancelSchedule stops a scheduled task from running.
// This implements the TaskCreator interface.
func (d *DispatcherImpl) CancelSchedule(ctx context.Context, scheduleID string) error {
	if d.scheduler == nil {
		return NewSystemTaskError("", "scheduler not initialized", nil)
	}
	return d.scheduler.CancelSchedule(ctx, scheduleID)
}

// GetSchedule returns information about a scheduled task.
// This implements the TaskCreator interface.
func (d *DispatcherImpl) GetSchedule(ctx context.Context, scheduleID string) (*ScheduleInfo, error) {
	if d.scheduler == nil {
		return nil, NewSystemTaskError("", "scheduler not initialized", nil)
	}
	return d.scheduler.GetSchedule(ctx, scheduleID)
}

// CreateTaskMultiple creates multiple tasks for immediate or delayed execution.
// Returns task IDs for tracking in the same order as input.
func (d *DispatcherImpl) CreateTaskMultiple(ctx context.Context, tasks []TaskCreation) ([]string, error) {
	if len(tasks) == 0 {
		return []string{}, nil
	}

	taskIDs := make([]string, 0, len(tasks))
	var errs []error

	for _, task := range tasks {
		taskID, err := d.CreateTask(ctx, task.TaskID, task.Payload, task.Options)
		if err != nil {
			errs = append(errs, fmt.Errorf("task %s: %w", task.TaskID, err))
		} else {
			taskIDs = append(taskIDs, taskID)
		}
	}

	if len(errs) > 0 {
		return taskIDs, fmt.Errorf("create multiple tasks failed: %v", errs)
	}
	return taskIDs, nil
}

// ScheduleTaskMultiple schedules multiple recurring tasks.
// Returns schedule IDs for tracking in the same order as input.
func (d *DispatcherImpl) ScheduleTaskMultiple(ctx context.Context, tasks []ScheduledTask) ([]string, error) {
	if len(tasks) == 0 {
		return []string{}, nil
	}

	scheduleIDs := make([]string, 0, len(tasks))
	var errs []error

	for _, task := range tasks {
		scheduleID, err := d.ScheduleTask(ctx, task.TaskID, task.Payload, task.Interval, task.Options)
		if err != nil {
			errs = append(errs, fmt.Errorf("task %s: %w", task.TaskID, err))
		} else {
			scheduleIDs = append(scheduleIDs, scheduleID)
		}
	}

	if len(errs) > 0 {
		return scheduleIDs, fmt.Errorf("schedule multiple tasks failed: %v", errs)
	}
	return scheduleIDs, nil
}

// startManagedMode initializes the managed worker pool.
func (d *DispatcherImpl) startManagedMode(ctx context.Context) error {
	d.managedDispatcher = NewManagedDispatcher(d.tasks, d.config, d.logger)
	if err := d.managedDispatcher.Start(ctx); err != nil {
		return err
	}

	// Initialize scheduler for task creation capabilities
	d.scheduler = NewTaskScheduler(d.managedDispatcher, d.logger)
	return d.scheduler.Start(ctx)
}

// stopManagedMode gracefully shuts down the managed worker pool.
func (d *DispatcherImpl) stopManagedMode(ctx context.Context) error {
	var errs []error

	// Stop scheduler first
	if d.scheduler != nil {
		if err := d.scheduler.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("scheduler stop error: %w", err))
		}
	}

	// Stop managed dispatcher
	if d.managedDispatcher != nil {
		if err := d.managedDispatcher.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("managed dispatcher stop error: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors during shutdown: %v", errs)
	}
	return nil
}

// dispatchManaged sends a task to the managed worker pool.
func (d *DispatcherImpl) dispatchManaged(ctx context.Context, taskID string, payload []byte) error {
	if d.managedDispatcher == nil {
		return NewSystemTaskError(taskID, "managed dispatcher not initialized", nil)
	}
	return d.managedDispatcher.Dispatch(ctx, taskID, payload)
}

// startRESTMode initializes the REST API server.
func (d *DispatcherImpl) startRESTMode(ctx context.Context) error {
	d.restDispatcher = NewRESTDispatcher(d.tasks, d.config, d.logger)
	return d.restDispatcher.Start(ctx)
}

// stopRESTMode gracefully shuts down the REST API server.
func (d *DispatcherImpl) stopRESTMode(ctx context.Context) error {
	if d.restDispatcher == nil {
		return nil
	}
	return d.restDispatcher.Stop(ctx)
}

// dispatchREST sends a task via the REST API.
func (d *DispatcherImpl) dispatchREST(ctx context.Context, taskID string, payload []byte) error {
	if d.restDispatcher == nil {
		return NewSystemTaskError(taskID, "rest dispatcher not initialized", nil)
	}
	return d.restDispatcher.Dispatch(ctx, taskID, payload)
}

// startMachineryMode initializes the Machinery v2 integration.
func (d *DispatcherImpl) startMachineryMode(ctx context.Context) error {
	d.machineryDispatcher = NewMachineryDispatcher(d.tasks, d.config, d.logger)
	d.machineryDispatcher.dispatchOnly = d.config.DispatchOnly
	return d.machineryDispatcher.Start(ctx)
}

// stopMachineryMode gracefully shuts down the Machinery integration.
func (d *DispatcherImpl) stopMachineryMode(ctx context.Context) error {
	if d.machineryDispatcher == nil {
		return nil
	}
	return d.machineryDispatcher.Stop(ctx)
}

// dispatchMachinery sends a task via Machinery v2.
func (d *DispatcherImpl) dispatchMachinery(ctx context.Context, taskID string, payload []byte) error {
	if d.machineryDispatcher == nil {
		return NewSystemTaskError(taskID, "machinery dispatcher not initialized", nil)
	}
	return d.machineryDispatcher.Dispatch(ctx, taskID, payload)
}

// TaskDispatcher is a subset interface of Dispatcher for dispatching tasks.
// This interface is used by components that only need to dispatch tasks
// without managing the full dispatcher lifecycle.
type TaskDispatcher interface {
	// Dispatch sends a task for execution with the given payload.
	// The taskID must match a registered task implementation.
	Dispatch(ctx context.Context, taskID string, payload []byte) error

	// DispatchMultiple sends tasks to multiple task handlers in sequence.
	// Each task is dispatched with its own payload.
	// Returns an error if any dispatch fails (previous dispatches are not rolled back).
	DispatchMultiple(ctx context.Context, tasks []TaskDispatch) error

	// CreateTask creates a new task for immediate or scheduled execution.
	CreateTask(ctx context.Context, taskID string, payload []byte, opts *CreateTaskOptions) (string, error)

	// CreateTaskMultiple creates multiple tasks for immediate or delayed execution.
	// Returns task IDs for tracking in the same order as input.
	// Returns aggregated errors if any fail (successful creations are not rolled back).
	CreateTaskMultiple(ctx context.Context, tasks []TaskCreation) ([]string, error)

	// ScheduleTask creates a recurring task that runs at the specified interval.
	ScheduleTask(ctx context.Context, taskID string, payload []byte, interval time.Duration, opts *ScheduleTaskOptions) (string, error)
}

// ensure interface compliance
var _ Dispatcher = (*DispatcherImpl)(nil)
var _ TaskDispatcher = (*DispatcherImpl)(nil)
