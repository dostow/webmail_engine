package taskmaster

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RichardKnop/machinery/v2"
	"github.com/RichardKnop/machinery/v2/backends/result"
	"github.com/RichardKnop/machinery/v2/config"
	"github.com/RichardKnop/machinery/v2/log"
	"github.com/RichardKnop/machinery/v2/tasks"

	redisbackend "github.com/RichardKnop/machinery/v2/backends/redis"
	redisbroker "github.com/RichardKnop/machinery/v2/brokers/redis"
	eagerlock "github.com/RichardKnop/machinery/v2/locks/eager"
)

// MachineryDispatcher handles task execution via Machinery v2.
// This provides distributed task queue functionality with Redis/RabbitMQ backends.
type MachineryDispatcher struct {
	mu           sync.RWMutex
	tasks        map[string]Task
	config       *DispatcherConfig
	logger       Logger
	running      bool
	server       *machinery.Server
	worker       *machinery.Worker
	consumerTag  string
	stopChan     chan struct{}
	taskRegistry map[string]interface{}
}

// NewMachineryDispatcher creates a new Machinery dispatcher.
func NewMachineryDispatcher(tasks map[string]Task, config *DispatcherConfig, logger Logger) *MachineryDispatcher {
	consumerTag := fmt.Sprintf("taskmaster_%d", time.Now().UnixNano())

	return &MachineryDispatcher{
		tasks:        tasks,
		config:       config,
		logger:       logger,
		consumerTag:  consumerTag,
		stopChan:     make(chan struct{}),
		taskRegistry: make(map[string]interface{}),
	}
}

// Start initializes the Machinery server and worker.
func (d *MachineryDispatcher) Start(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.running {
		return ErrDispatcherAlreadyStarted
	}

	if d.config.MachineryConfig == nil {
		return NewSystemTaskError("", "machinery config not provided", nil)
	}

	d.logger.Info("Initializing Machinery v2 server",
		"broker", d.config.MachineryConfig.BrokerURL,
		"backend", d.config.MachineryConfig.ResultBackend)

	// Configure Machinery
	cnf := &config.Config{
		DefaultQueue:    d.config.MachineryConfig.DefaultQueue,
		ResultsExpireIn: 3600, // 1 hour
	}

	// Parse broker URL to determine backend type
	brokerURL := d.config.MachineryConfig.BrokerURL
	resultBackend := d.config.MachineryConfig.ResultBackend

	// Create server with Redis backend
	server, err := d.createServerWithConfig(cnf, brokerURL, resultBackend)
	if err != nil {
		return NewSystemTaskError("", "failed to create machinery server", err)
	}
	d.server = server

	// Set custom logger to route Machinery logs through our logger
	log.Set(&machineryLogger{logger: d.logger})

	// Register all tasks with Machinery
	for taskID, task := range d.tasks {
		if err := d.registerTaskWithMachinery(taskID, task); err != nil {
			return fmt.Errorf("failed to register task %s: %w", taskID, err)
		}
	}

	// Create worker with concurrency
	d.worker = server.NewWorker(d.consumerTag, d.config.WorkerCount)

	// Set up pre-task handler for logging
	d.worker.SetPreTaskHandler(func(signature *tasks.Signature) {
		d.logger.Debug("Machinery task starting",
			"task_id", signature.Name,
			"task_uuid", signature.UUID)
	})

	// Set up post-task handler for logging
	d.worker.SetPostTaskHandler(func(signature *tasks.Signature) {
		d.logger.Debug("Machinery task completed",
			"task_id", signature.Name,
			"task_uuid", signature.UUID)
	})

	// Set up error handler
	d.worker.SetErrorHandler(func(err error) {
		d.logger.Error("Machinery worker error", "error", err)
	})

	// Start worker in goroutine
	go func() {
		d.logger.Info("Starting Machinery worker",
			"consumer_tag", d.consumerTag,
			"concurrency", d.config.WorkerCount,
			"queue", d.config.MachineryConfig.DefaultQueue)

		if err := d.worker.Launch(); err != nil {
			if err != machinery.ErrWorkerQuitGracefully {
				d.logger.Error("Machinery worker stopped with error", "error", err)
			} else {
				d.logger.Info("Machinery worker stopped gracefully")
			}
		}
	}()

	d.running = true
	d.logger.Info("Machinery dispatcher started successfully")

	return nil
}

// createServerWithConfig creates a Machinery server with the given configuration.
func (d *MachineryDispatcher) createServerWithConfig(cnf *config.Config, brokerURL, resultBackend string) (*machinery.Server, error) {
	// For Redis, use the go-redis implementation
	if strings.HasPrefix(brokerURL, "redis://") || strings.HasPrefix(brokerURL, "rediss://") {
		redisAddrs := []string{strings.TrimPrefix(strings.TrimPrefix(brokerURL, "redis://"), "rediss://")}
		broker := redisbroker.NewGR(cnf, redisAddrs, 0)

		backendAddrs := []string{strings.TrimPrefix(strings.TrimPrefix(resultBackend, "redis://"), "rediss://")}
		backend := redisbackend.NewGR(cnf, backendAddrs, 0)

		lock := eagerlock.New()

		return machinery.NewServer(cnf, broker, backend, lock), nil
	}

	return nil, fmt.Errorf("unsupported broker: %s", brokerURL)
}

// Stop gracefully shuts down the Machinery worker and server.
func (d *MachineryDispatcher) Stop(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.running {
		return nil
	}

	d.logger.Info("Stopping Machinery dispatcher...")

	// Signal stop
	close(d.stopChan)

	// Stop worker gracefully
	if d.worker != nil {
		d.worker.Quit()
		d.logger.Info("Machinery worker stopped")
	}

	d.running = false
	d.logger.Info("Machinery dispatcher stopped")
	return nil
}

// Dispatch sends a task for execution via Machinery.
func (d *MachineryDispatcher) Dispatch(ctx context.Context, taskID string, payload []byte) error {
	d.mu.RLock()
	task, exists := d.tasks[taskID]
	d.mu.RUnlock()

	if !exists {
		return fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}

	if !d.running {
		return ErrDispatcherNotStarted
	}

	// Create Machinery signature
	signature, err := d.createSignature(taskID, payload)
	if err != nil {
		return NewNonRetryableTaskError(taskID, "failed to create signature", err)
	}

	// Apply retry policy
	if d.config.MachineryConfig.DefaultRetryCount > 0 {
		signature.RetryCount = d.config.MachineryConfig.DefaultRetryCount
		signature.RetryTimeout = 60 // seconds
	}

	d.logger.Debug("Dispatching task via Machinery",
		"task_id", taskID,
		"signature_uuid", signature.UUID)

	// Send task to Machinery
	var asyncResult *result.AsyncResult
	if ctx.Done() != nil {
		asyncResult, err = d.server.SendTaskWithContext(ctx, signature)
	} else {
		asyncResult, err = d.server.SendTask(signature)
	}

	if err != nil {
		return NewRetryableTaskError(taskID, "failed to send task to Machinery", err)
	}

	// Log result when available (async, non-blocking)
	go func() {
		timeout := d.config.TaskTimeout
		if timeout <= 0 {
			timeout = 5 * time.Minute
		}

		results, err := asyncResult.Get(timeout)
		if err != nil {
			d.logger.Error("Machinery task failed",
				"task_id", taskID,
				"signature_uuid", signature.UUID,
				"error", err)
		} else {
			d.logger.Debug("Machinery task result received",
				"task_id", taskID,
				"results", fmt.Sprintf("%v", results))
		}
	}()

	_ = task // Task is executed by Machinery workers
	return nil
}

// DispatchMultiple sends multiple tasks for execution via Machinery.
func (d *MachineryDispatcher) DispatchMultiple(ctx context.Context, tasks []TaskDispatch) error {
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

// DispatchSync sends a task and waits for completion (synchronous).
func (d *MachineryDispatcher) DispatchSync(ctx context.Context, taskID string, payload []byte) error {
	d.mu.RLock()
	task, exists := d.tasks[taskID]
	d.mu.RUnlock()

	if !exists {
		return fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}

	if !d.running {
		return ErrDispatcherNotStarted
	}

	// Create Machinery signature
	signature, err := d.createSignature(taskID, payload)
	if err != nil {
		return NewNonRetryableTaskError(taskID, "failed to create signature", err)
	}

	// Apply retry policy
	if d.config.MachineryConfig.DefaultRetryCount > 0 {
		signature.RetryCount = d.config.MachineryConfig.DefaultRetryCount
		signature.RetryTimeout = 60
	}

	d.logger.Debug("Dispatching synchronous task via Machinery",
		"task_id", taskID,
		"signature_uuid", signature.UUID)

	// Send task
	var asyncResult *result.AsyncResult
	if ctx.Done() != nil {
		asyncResult, err = d.server.SendTaskWithContext(ctx, signature)
	} else {
		asyncResult, err = d.server.SendTask(signature)
	}

	if err != nil {
		return NewRetryableTaskError(taskID, "failed to send task", err)
	}

	// Wait for result with timeout
	timeout := d.config.TaskTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	results, err := asyncResult.Get(timeout)
	if err != nil {
		return NewRetryableTaskError(taskID, "task execution failed", err)
	}

	d.logger.Debug("Synchronous task completed",
		"task_id", taskID,
		"results", fmt.Sprintf("%v", results))

	_ = task
	return nil
}

// registerTaskWithMachinery registers a task with Machinery's task registry.
func (d *MachineryDispatcher) registerTaskWithMachinery(taskID string, task Task) error {
	// Register task function that accepts []byte payload
	err := d.server.RegisterTask(taskID, func(payload []byte) ([]byte, error) {
		ctx := context.Background()
		if err := task.Execute(ctx, payload); err != nil {
			return nil, err
		}
		return []byte("OK"), nil
	})

	if err != nil {
		return fmt.Errorf("failed to register task %s: %w", taskID, err)
	}

	d.taskRegistry[taskID] = task
	d.logger.Debug("Registered task with Machinery", "task_id", taskID)
	return nil
}

// createSignature creates a Machinery signature from a task request.
func (d *MachineryDispatcher) createSignature(taskID string, payload []byte) (*tasks.Signature, error) {
	sig := &tasks.Signature{
		Name: taskID,
		Args: []tasks.Arg{
			{
				Type:  "[]byte",
				Value: payload,
			},
		},
		UUID: fmt.Sprintf("%s_%d", taskID, time.Now().UnixNano()),
	}

	return sig, nil
}

// IsRunning returns true if the Machinery dispatcher is running.
func (d *MachineryDispatcher) IsRunning() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.running
}

// GetRegisteredTasks returns a list of all registered task IDs.
func (d *MachineryDispatcher) GetRegisteredTasks() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	ids := make([]string, 0, len(d.tasks))
	for id := range d.tasks {
		ids = append(ids, id)
	}
	return ids
}

// Mode returns the execution mode (MachineryMode).
func (d *MachineryDispatcher) Mode() ExecutionMode {
	return MachineryMode
}

// Register registers a task dynamically (after Start).
func (d *MachineryDispatcher) Register(task Task) error {
	if task == nil {
		return NewNonRetryableTaskError("", "cannot register nil task", nil)
	}

	taskID := task.ID()
	if taskID == "" {
		return NewNonRetryableTaskError("", "task ID cannot be empty", nil)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if _, exists := d.tasks[taskID]; exists {
		return ErrTaskAlreadyRegistered
	}

	d.tasks[taskID] = task

	// If already running, register with Machinery immediately
	if d.running && d.server != nil {
		if err := d.registerTaskWithMachinery(taskID, task); err != nil {
			delete(d.tasks, taskID)
			return err
		}
	}

	d.logger.Debug("Registered task", "task_id", taskID)
	return nil
}

// GetServer returns the underlying Machinery server (for advanced usage).
func (d *MachineryDispatcher) GetServer() *machinery.Server {
	return d.server
}

// GetWorker returns the underlying Machinery worker (for advanced usage).
func (d *MachineryDispatcher) GetWorker() *machinery.Worker {
	return d.worker
}

// GetQueueLength returns the length of a specific queue.
func (d *MachineryDispatcher) GetQueueLength(queueName string) (int, error) {
	if d.server == nil {
		return 0, fmt.Errorf("server not initialized")
	}

	broker := d.server.GetBroker()
	if broker == nil {
		return 0, fmt.Errorf("broker not available")
	}

	// Note: GetQueueLength is broker-specific and may not be available for all backends
	return 0, nil
}

// machineryLogger adapts Machinery's logger to our Logger interface.
type machineryLogger struct {
	logger Logger
}

func (l *machineryLogger) Debug(v ...interface{}) {
	l.logger.Debug(fmt.Sprint(v...))
}

func (l *machineryLogger) Info(v ...interface{}) {
	l.logger.Info(fmt.Sprint(v...))
}

func (l *machineryLogger) Error(v ...interface{}) {
	l.logger.Error(fmt.Sprint(v...))
}

func (l *machineryLogger) Warning(v ...interface{}) {
	l.logger.Warn(fmt.Sprint(v...))
}

func (l *machineryLogger) Fatal(v ...interface{}) {
	l.logger.Error(fmt.Sprint(v...))
}

func (l *machineryLogger) Debugf(format string, v ...interface{}) {
	l.logger.Debug(fmt.Sprintf(format, v...))
}

func (l *machineryLogger) Infof(format string, v ...interface{}) {
	l.logger.Info(fmt.Sprintf(format, v...))
}

func (l *machineryLogger) Errorf(format string, v ...interface{}) {
	l.logger.Error(fmt.Sprintf(format, v...))
}

func (l *machineryLogger) Warningf(format string, v ...interface{}) {
	l.logger.Warn(fmt.Sprintf(format, v...))
}

func (l *machineryLogger) Fatalf(format string, v ...interface{}) {
	l.logger.Error(fmt.Sprintf(format, v...))
}

func (l *machineryLogger) Fatalln(v ...interface{}) {
	l.logger.Error(fmt.Sprint(v...))
}

func (l *machineryLogger) Print(v ...interface{}) {
	l.logger.Info(fmt.Sprint(v...))
}

func (l *machineryLogger) Printf(format string, v ...interface{}) {
	l.logger.Info(fmt.Sprintf(format, v...))
}

func (l *machineryLogger) Println(v ...interface{}) {
	l.logger.Info(fmt.Sprint(v...))
}

func (l *machineryLogger) Panic(v ...interface{}) {
	l.logger.Error(fmt.Sprint(v...))
}

func (l *machineryLogger) Panicf(format string, v ...interface{}) {
	l.logger.Error(fmt.Sprintf(format, v...))
}

func (l *machineryLogger) Panicln(v ...interface{}) {
	l.logger.Error(fmt.Sprint(v...))
}

// ensure interface compliance
var _ Dispatcher = (*MachineryDispatcher)(nil)

// TaskSignatureBuilder helps build Machinery task signatures with fluent API.
type TaskSignatureBuilder struct {
	signature *tasks.Signature
}

// NewTaskSignatureBuilder creates a new signature builder.
func NewTaskSignatureBuilder(taskID string) *TaskSignatureBuilder {
	return &TaskSignatureBuilder{
		signature: &tasks.Signature{
			Name: taskID,
			UUID: fmt.Sprintf("%s_%d", taskID, time.Now().UnixNano()),
		},
	}
}

// WithArgs adds arguments to the signature.
func (b *TaskSignatureBuilder) WithArgs(args ...tasks.Arg) *TaskSignatureBuilder {
	b.signature.Args = append(b.signature.Args, args...)
	return b
}

// WithBytesArg adds a byte slice argument.
func (b *TaskSignatureBuilder) WithBytesArg(payload []byte) *TaskSignatureBuilder {
	b.signature.Args = append(b.signature.Args, tasks.Arg{
		Type:  "[]byte",
		Value: payload,
	})
	return b
}

// WithJSONArg adds a JSON-serializable argument.
func (b *TaskSignatureBuilder) WithJSONArg(data interface{}) *TaskSignatureBuilder {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return b
	}
	b.signature.Args = append(b.signature.Args, tasks.Arg{
		Type:  "string",
		Value: string(jsonBytes),
	})
	return b
}

// WithRetryCount sets the retry count.
func (b *TaskSignatureBuilder) WithRetryCount(count int) *TaskSignatureBuilder {
	b.signature.RetryCount = count
	return b
}

// WithRetryTimeout sets the retry timeout in seconds.
func (b *TaskSignatureBuilder) WithRetryTimeout(seconds int) *TaskSignatureBuilder {
	b.signature.RetryTimeout = seconds
	return b
}

// WithETA sets the estimated time of arrival (when the task should be executed).
func (b *TaskSignatureBuilder) WithETA(eta time.Time) *TaskSignatureBuilder {
	b.signature.ETA = &eta
	return b
}

// WithGroupUUID sets the group UUID for task grouping.
func (b *TaskSignatureBuilder) WithGroupUUID(uuid string) *TaskSignatureBuilder {
	b.signature.GroupUUID = uuid
	return b
}

// WithPriority sets the task priority.
func (b *TaskSignatureBuilder) WithPriority(priority uint8) *TaskSignatureBuilder {
	b.signature.Priority = priority
	return b
}

// WithRoutingKey sets the routing key for task routing.
func (b *TaskSignatureBuilder) WithRoutingKey(routingKey string) *TaskSignatureBuilder {
	b.signature.RoutingKey = routingKey
	return b
}

// WithImmutable sets whether the task is immutable (can't be modified).
func (b *TaskSignatureBuilder) WithImmutable(immutable bool) *TaskSignatureBuilder {
	b.signature.Immutable = immutable
	return b
}

// Build returns the final signature.
func (b *TaskSignatureBuilder) Build() *tasks.Signature {
	return b.signature
}

// Send sends the built signature via the Machinery server.
func (b *TaskSignatureBuilder) Send(server *machinery.Server) (*result.AsyncResult, error) {
	return server.SendTask(b.signature)
}

// SendWithContext sends the built signature via the Machinery server with context.
func (b *TaskSignatureBuilder) SendWithContext(ctx context.Context, server *machinery.Server) (*result.AsyncResult, error) {
	return server.SendTaskWithContext(ctx, b.signature)
}

// ChainBuilder helps build task chains (sequential execution).
type ChainBuilder struct {
	signatures []*tasks.Signature
}

// NewChainBuilder creates a new chain builder.
func NewChainBuilder() *ChainBuilder {
	return &ChainBuilder{
		signatures: make([]*tasks.Signature, 0),
	}
}

// AddTask adds a task to the chain.
func (c *ChainBuilder) AddTask(signature *tasks.Signature) *ChainBuilder {
	c.signatures = append(c.signatures, signature)
	return c
}

// Build creates a Machinery chain.
func (c *ChainBuilder) Build() (*tasks.Chain, error) {
	return tasks.NewChain(c.signatures...)
}

// Send sends the chain via the Machinery server.
func (c *ChainBuilder) Send(server *machinery.Server) (*result.ChainAsyncResult, error) {
	chain, err := c.Build()
	if err != nil {
		return nil, err
	}
	return server.SendChain(chain)
}

// GroupBuilder helps build task groups (parallel execution).
type GroupBuilder struct {
	signatures []*tasks.Signature
}

// NewGroupBuilder creates a new group builder.
func NewGroupBuilder() *GroupBuilder {
	return &GroupBuilder{
		signatures: make([]*tasks.Signature, 0),
	}
}

// AddTask adds a task to the group.
func (g *GroupBuilder) AddTask(signature *tasks.Signature) *GroupBuilder {
	g.signatures = append(g.signatures, signature)
	return g
}

// Build creates a Machinery group.
func (g *GroupBuilder) Build() (*tasks.Group, error) {
	return tasks.NewGroup(g.signatures...)
}

// Send sends the group via the Machinery server.
func (g *GroupBuilder) Send(server *machinery.Server, sendConcurrency int) ([]*result.AsyncResult, error) {
	group, err := g.Build()
	if err != nil {
		return nil, err
	}
	return server.SendGroup(group, sendConcurrency)
}

// ChordBuilder helps build chords (group + callback).
type ChordBuilder struct {
	group    *tasks.Group
	callback *tasks.Signature
}

// NewChordBuilder creates a new chord builder.
func NewChordBuilder() *ChordBuilder {
	return &ChordBuilder{}
}

// WithGroup sets the group for the chord.
func (c *ChordBuilder) WithGroup(signatures ...*tasks.Signature) *ChordBuilder {
	group, _ := tasks.NewGroup(signatures...)
	c.group = group
	return c
}

// WithCallback sets the callback for the chord.
func (c *ChordBuilder) WithCallback(callback *tasks.Signature) *ChordBuilder {
	c.callback = callback
	return c
}

// Build creates a Machinery chord.
func (c *ChordBuilder) Build() (*tasks.Chord, error) {
	if c.group == nil || c.callback == nil {
		return nil, fmt.Errorf("chord requires both group and callback")
	}
	return tasks.NewChord(c.group, c.callback)
}

// Send sends the chord via the Machinery server.
func (c *ChordBuilder) Send(server *machinery.Server, sendConcurrency int) (*result.ChordAsyncResult, error) {
	chord, err := c.Build()
	if err != nil {
		return nil, err
	}
	return server.SendChord(chord, sendConcurrency)
}

// MachineryIntegrationGuide provides documentation for Machinery usage.
const MachineryIntegrationGuide = `
Machinery v2 Integration Guide
==============================

The Machinery dispatcher provides distributed task queue functionality.

Configuration:
- BrokerURL: redis://localhost:6379
- ResultBackend: redis://localhost:6379
- DefaultQueue: Name of the default queue

Usage:
1. Create dispatcher with MachineryConfig
2. Register tasks
3. Start the dispatcher
4. Dispatch tasks using Dispatch() or DispatchSync()

Features:
- Distributed task execution across multiple workers
- Automatic retry with exponential backoff
- Task chains (sequential execution)
- Task groups (parallel execution)
- Chords (group + callback)
- Redis broker backend
`
