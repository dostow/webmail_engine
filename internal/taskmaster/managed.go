package taskmaster

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ManagedDispatcher handles task execution using an internal worker pool.
// It uses buffered channels for task distribution and implements graceful shutdown.
type ManagedDispatcher struct {
	mu          sync.RWMutex
	tasks       map[string]Task
	config      *DispatcherConfig
	logger      Logger
	taskChan    chan *TaskRequest
	workerCount int
	wg          sync.WaitGroup
	running     atomic.Bool
	stopChan    chan struct{}
	stats       *ManagedStats
	inFlight    atomic.Int64 // Track number of tasks currently executing
}

// ManagedStats holds statistics for the managed worker pool.
type ManagedStats struct {
	mu               sync.RWMutex
	TasksReceived    int64 `json:"tasks_received"`
	TasksCompleted   int64 `json:"tasks_completed"`
	TasksFailed      int64 `json:"tasks_failed"`
	TasksInFlight    int64 `json:"tasks_in_flight"`
	QueueDepth       int64 `json:"queue_depth"`
	LastTaskAt       time.Time
	AvgProcessTimeMs int64 `json:"avg_process_time_ms"`
}

// NewManagedDispatcher creates a new managed dispatcher.
func NewManagedDispatcher(tasks map[string]Task, config *DispatcherConfig, logger Logger) *ManagedDispatcher {
	workerCount := config.WorkerCount
	if workerCount <= 0 {
		workerCount = 4
	}

	queueSize := config.QueueSize
	if queueSize <= 0 {
		queueSize = 100
	}

	return &ManagedDispatcher{
		tasks:       tasks,
		config:      config,
		logger:      logger,
		taskChan:    make(chan *TaskRequest, queueSize),
		workerCount: workerCount,
		stopChan:    make(chan struct{}),
		stats: &ManagedStats{
			TasksReceived:    0,
			TasksCompleted:   0,
			TasksFailed:      0,
			TasksInFlight:    0,
			QueueDepth:       0,
			AvgProcessTimeMs: 0,
		},
	}
}

// Start initializes the worker pool and begins processing tasks.
func (d *ManagedDispatcher) Start(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.running.Load() {
		return ErrDispatcherAlreadyStarted
	}

	d.logger.Info("Starting managed worker pool", "worker_count", d.workerCount)

	// Start worker goroutines
	for i := 0; i < d.workerCount; i++ {
		d.wg.Add(1)
		go d.worker(ctx, i)
	}

	// Start stats monitoring goroutine
	d.wg.Add(1)
	go d.statsMonitor(ctx)

	d.running.Store(true)
	d.logger.Info("Managed worker pool started")

	return nil
}

// Stop gracefully shuts down the worker pool.
// It stops accepting new tasks and waits for in-progress tasks to complete.
func (d *ManagedDispatcher) Stop(ctx context.Context) error {
	if !d.running.Load() {
		return nil
	}

	inFlightCount := d.inFlight.Load()
	queueDepth := len(d.taskChan)
	d.logger.Info("Stopping managed worker pool...",
		"in_flight_tasks", inFlightCount,
		"queued_tasks", queueDepth)

	// Signal workers to stop
	close(d.stopChan)

	// Wait for workers to finish with timeout
	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		d.logger.Info("Managed worker pool stopped gracefully",
			"final_in_flight", d.inFlight.Load())
		d.running.Store(false)
		return nil
	case <-ctx.Done():
		remainingInFlight := d.inFlight.Load()
		d.logger.Error("Managed worker pool shutdown timed out",
			"error", ctx.Err(),
			"remaining_in_flight", remainingInFlight,
			"remaining_queue_depth", len(d.taskChan))
		d.running.Store(false)
		return fmt.Errorf("%w: %v", ErrGracefulShutdownTimeout, ctx.Err())
	}
}

// Dispatch sends a task to the worker pool for execution.
func (d *ManagedDispatcher) Dispatch(ctx context.Context, taskID string, payload []byte) error {
	if !d.running.Load() {
		return ErrDispatcherNotStarted
	}

	// Validate task exists
	d.mu.RLock()
	_, exists := d.tasks[taskID]
	d.mu.RUnlock()

	if !exists {
		return fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}

	// Update stats
	d.stats.mu.Lock()
	d.stats.TasksReceived++
	d.stats.mu.Unlock()

	// Create task request
	request := &TaskRequest{
		Ctx:     ctx,
		TaskID:  taskID,
		Payload: payload,
	}

	// Try to send with context awareness
	select {
	case d.taskChan <- request:
		d.stats.mu.Lock()
		d.stats.TasksInFlight++
		d.stats.QueueDepth = int64(len(d.taskChan))
		d.stats.mu.Unlock()
		d.logger.Debug("Task dispatched", "task_id", taskID, "queue_depth", len(d.taskChan))
		return nil
	case <-ctx.Done():
		return fmt.Errorf("failed to dispatch task: %w", ctx.Err())
	default:
		// Queue is full
		d.stats.mu.Lock()
		d.stats.TasksFailed++
		d.stats.mu.Unlock()
		return fmt.Errorf("%w: queue depth is %d", ErrQueueFull, len(d.taskChan))
	}
}

// DispatchMultiple sends multiple tasks to the worker pool in sequence.
func (d *ManagedDispatcher) DispatchMultiple(ctx context.Context, tasks []TaskDispatch) error {
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

// DispatchSync sends a task and waits for completion (synchronous execution).
// This is useful for testing or when immediate results are needed.
func (d *ManagedDispatcher) DispatchSync(ctx context.Context, taskID string, payload []byte) error {
	if !d.running.Load() {
		return ErrDispatcherNotStarted
	}

	// Validate task exists
	d.mu.RLock()
	_, exists := d.tasks[taskID]
	d.mu.RUnlock()

	if !exists {
		return fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}

	// Create result channel
	resultChan := make(chan TaskResult, 1)

	// Create task request with result channel
	request := &TaskRequest{
		Ctx:        ctx,
		TaskID:     taskID,
		Payload:    payload,
		ResultChan: resultChan,
	}

	// Try to send
	select {
	case d.taskChan <- request:
		d.logger.Debug("Task dispatched (sync)", "task_id", taskID)
	case <-ctx.Done():
		return fmt.Errorf("failed to dispatch task: %w", ctx.Err())
	default:
		return fmt.Errorf("%w", ErrQueueFull)
	}

	// Wait for result
	select {
	case result := <-resultChan:
		return result.Err
	case <-ctx.Done():
		return fmt.Errorf("task execution interrupted: %w", ctx.Err())
	}
}

// worker is the main processing loop for each worker goroutine.
func (d *ManagedDispatcher) worker(ctx context.Context, id int) {
	defer d.wg.Done()

	d.logger.Debug("Worker started", "worker_id", id)

	for {
		select {
		case <-d.stopChan:
			d.logger.Debug("Worker stopping", "worker_id", id)
			return
		case <-ctx.Done():
			d.logger.Debug("Worker context cancelled", "worker_id", id)
			return
		case request, ok := <-d.taskChan:
			if !ok {
				d.logger.Debug("Task channel closed", "worker_id", id)
				return
			}

			d.processTask(id, request)
		}
	}
}

// processTask executes a single task request.
func (d *ManagedDispatcher) processTask(workerID int, request *TaskRequest) {
	startTime := time.Now()
	taskID := request.TaskID

	// Track in-flight task
	d.inFlight.Add(1)
	defer d.inFlight.Add(-1)

	d.logger.Debug("Processing task", "worker_id", workerID, "task_id", taskID)

	// Get task implementation
	d.mu.RLock()
	task, exists := d.tasks[taskID]
	d.mu.RUnlock()

	if !exists {
		d.logger.Error("Task not found", "task_id", taskID)
		d.updateStats(false, startTime)
		if request.ResultChan != nil {
			request.ResultChan <- TaskResult{TaskID: taskID, Err: ErrTaskNotFound}
		}
		return
	}

	// Apply timeout if configured
	execCtx := request.Ctx
	if d.config.TaskTimeout > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(request.Ctx, d.config.TaskTimeout)
		defer cancel()
	}

	// Execute the task
	err := task.Execute(execCtx, request.Payload)

	// Update stats
	d.updateStats(err == nil, startTime)

	// Log result with shutdown awareness
	if err != nil {
		if request.Ctx.Err() != nil {
			d.logger.Info("Task interrupted by shutdown or timeout",
				"worker_id", workerID,
				"task_id", taskID,
				"error", err,
				"duration_ms", time.Since(startTime).Milliseconds())
		} else {
			d.logger.Error("Task execution failed",
				"worker_id", workerID,
				"task_id", taskID,
				"error", err,
				"duration_ms", time.Since(startTime).Milliseconds())
		}
	} else {
		d.logger.Debug("Task completed",
			"worker_id", workerID,
			"task_id", taskID,
			"duration_ms", time.Since(startTime).Milliseconds())
	}

	// Send result if synchronous execution
	if request.ResultChan != nil {
		request.ResultChan <- TaskResult{TaskID: taskID, Err: err}
	}
}

// updateStats updates the dispatcher statistics after task completion.
func (d *ManagedDispatcher) updateStats(success bool, startTime time.Time) {
	d.stats.mu.Lock()
	defer d.stats.mu.Unlock()

	d.stats.TasksInFlight--
	d.stats.QueueDepth = int64(len(d.taskChan))
	d.stats.LastTaskAt = time.Now()

	if success {
		d.stats.TasksCompleted++
	} else {
		d.stats.TasksFailed++
	}

	// Update average processing time (exponential moving average)
	processingTime := time.Since(startTime).Milliseconds()
	if d.stats.AvgProcessTimeMs == 0 {
		d.stats.AvgProcessTimeMs = processingTime
	} else {
		// EMA with alpha = 0.3
		d.stats.AvgProcessTimeMs = int64(float64(d.stats.AvgProcessTimeMs)*0.7 + float64(processingTime)*0.3)
	}
}

// statsMonitor periodically logs worker pool statistics.
func (d *ManagedDispatcher) statsMonitor(ctx context.Context) {
	defer d.wg.Done()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopChan:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats := d.GetStats()
			d.logger.Info("Worker pool stats",
				"tasks_received", stats.TasksReceived,
				"tasks_completed", stats.TasksCompleted,
				"tasks_failed", stats.TasksFailed,
				"queue_depth", stats.QueueDepth,
				"avg_process_time_ms", stats.AvgProcessTimeMs)
		}
	}
}

// GetStats returns current worker pool statistics.
func (d *ManagedDispatcher) GetStats() *ManagedStats {
	d.stats.mu.RLock()
	defer d.stats.mu.RUnlock()

	// Return a copy
	return &ManagedStats{
		TasksReceived:    d.stats.TasksReceived,
		TasksCompleted:   d.stats.TasksCompleted,
		TasksFailed:      d.stats.TasksFailed,
		TasksInFlight:    d.stats.TasksInFlight,
		QueueDepth:       d.stats.QueueDepth,
		LastTaskAt:       d.stats.LastTaskAt,
		AvgProcessTimeMs: d.stats.AvgProcessTimeMs,
	}
}

// IsRunning returns true if the worker pool is running.
func (d *ManagedDispatcher) IsRunning() bool {
	return d.running.Load()
}

// GetQueueDepth returns the current number of tasks waiting in the queue.
func (d *ManagedDispatcher) GetQueueDepth() int {
	return len(d.taskChan)
}

// GetWorkerCount returns the number of workers in the pool.
func (d *ManagedDispatcher) GetWorkerCount() int {
	return d.workerCount
}

// Mode returns the execution mode (ManagedMode).
func (d *ManagedDispatcher) Mode() ExecutionMode {
	return ManagedMode
}

// Register registers a task (no-op for ManagedDispatcher, tasks are passed in constructor).
func (d *ManagedDispatcher) Register(task Task) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if task == nil {
		return NewNonRetryableTaskError("", "cannot register nil task", nil)
	}
	taskID := task.ID()
	if _, exists := d.tasks[taskID]; exists {
		return ErrTaskAlreadyRegistered
	}
	d.tasks[taskID] = task
	return nil
}

// GetRegisteredTasks returns a list of all registered task IDs.
func (d *ManagedDispatcher) GetRegisteredTasks() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	ids := make([]string, 0, len(d.tasks))
	for id := range d.tasks {
		ids = append(ids, id)
	}
	return ids
}

// ensure interface compliance for core Dispatcher methods
var _ Dispatcher = (*ManagedDispatcher)(nil)
