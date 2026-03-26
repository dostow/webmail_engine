package taskmaster

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RESTDispatcher handles task execution via HTTP endpoints using Gin.
// It exposes RESTful APIs for task dispatching and monitoring.
type RESTDispatcher struct {
	mu        sync.RWMutex
	tasks     map[string]Task
	config    *DispatcherConfig
	logger    Logger
	engine    *gin.Engine
	server    *http.Server
	running   bool
	basePath  string
	scheduler *TaskScheduler
}

// TaskDispatchRequest represents the JSON payload for dispatching a task.
type TaskDispatchRequest struct {
	// TaskID identifies the type of task to execute.
	TaskID string `json:"task_id" binding:"required"`

	// Payload contains the task-specific data.
	Payload json.RawMessage `json:"payload"`

	// Sync indicates whether to wait for task completion.
	// Default is false (fire-and-forget).
	Sync bool `json:"sync,omitempty"`
}

// TaskDispatchResponse represents the response for a task dispatch request.
type TaskDispatchResponse struct {
	// RequestID is a unique identifier for correlation.
	RequestID string `json:"request_id"`

	// TaskID identifies the task that was dispatched.
	TaskID string `json:"task_id"`

	// Status indicates the current state of the task.
	Status string `json:"status"`

	// Error contains any error message if the task failed.
	Error string `json:"error,omitempty"`

	// Result contains the task result for synchronous execution.
	Result json.RawMessage `json:"result,omitempty"`
}

// ScheduleTaskRequest represents the JSON payload for scheduling a task.
type ScheduleTaskRequest struct {
	// TaskID identifies the type of task to schedule.
	TaskID string `json:"task_id" binding:"required"`

	// Payload contains the task-specific data.
	Payload json.RawMessage `json:"payload"`

	// Interval is the time between executions (e.g., "5m", "1h").
	Interval string `json:"interval" binding:"required"`

	// Name is a human-readable name for the schedule.
	Name string `json:"name,omitempty"`

	// MaxExecutions limits the number of executions (0 = unlimited).
	MaxExecutions int `json:"max_executions,omitempty"`
}

// ScheduleTaskResponse represents the response for scheduling a task.
type ScheduleTaskResponse struct {
	// ScheduleID is the unique identifier for the schedule.
	ScheduleID string `json:"schedule_id"`

	// TaskID identifies the scheduled task type.
	TaskID string `json:"task_id"`

	// Name is the human-readable schedule name.
	Name string `json:"name"`

	// Interval is the time between executions.
	Interval string `json:"interval"`

	// NextRun is when the task will next execute.
	NextRun time.Time `json:"next_run"`
}

// HealthResponse represents the health check response.
type HealthResponse struct {
	// Status indicates the overall health status.
	Status string `json:"status"`

	// Mode is the current execution mode.
	Mode string `json:"mode"`

	// Timestamp is the current server time.
	Timestamp time.Time `json:"timestamp"`

	// RegisteredTasks lists all registered task types.
	RegisteredTasks []string `json:"registered_tasks"`

	// ActiveSchedules is the number of active scheduled tasks.
	ActiveSchedules int `json:"active_schedules"`
}

// NewRESTDispatcher creates a new REST dispatcher.
func NewRESTDispatcher(tasks map[string]Task, config *DispatcherConfig, logger Logger) *RESTDispatcher {
	basePath := "/api/v1/tasks"
	if config.RESTConfig != nil && config.RESTConfig.BasePath != "" {
		basePath = config.RESTConfig.BasePath
	}

	// Set Gin mode based on environment
	if logger != nil {
		// Use debug mode for development
		gin.SetMode(gin.DebugMode)
	} else {
		// Use release mode for production (less logging)
		gin.SetMode(gin.ReleaseMode)
	}

	return &RESTDispatcher{
		tasks:    tasks,
		config:   config,
		logger:   logger,
		basePath: basePath,
	}
}

// Start initializes and starts the HTTP server.
func (d *RESTDispatcher) Start(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.running {
		return ErrDispatcherAlreadyStarted
	}

	// Create Gin engine
	d.engine = gin.New()
	d.engine.Use(gin.Recovery())
	d.engine.Use(d.loggingMiddleware())

	// Register routes
	d.registerRoutes(d.engine)

	// Create TaskScheduler for scheduled tasks
	d.scheduler = NewTaskScheduler(&dispatcherWrapper{tasks: d.tasks, logger: d.logger}, d.logger)
	if err := d.scheduler.Start(ctx); err != nil {
		return fmt.Errorf("failed to start scheduler: %w", err)
	}

	addr := ":8080"
	if d.config.RESTConfig != nil && d.config.RESTConfig.Addr != "" {
		addr = d.config.RESTConfig.Addr
	}

	d.server = &http.Server{
		Addr:         addr,
		Handler:      d.engine,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in goroutine
	serverErr := make(chan error, 1)
	go func() {
		d.logger.Info("Starting REST API server", "address", addr)
		if err := d.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Check for immediate startup errors
	select {
	case err := <-serverErr:
		return fmt.Errorf("failed to start REST server: %w", err)
	default:
		d.running = true
		d.logger.Info("REST API server started")
		return nil
	}
}

// Stop gracefully shuts down the HTTP server.
func (d *RESTDispatcher) Stop(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.running {
		return nil
	}

	d.logger.Info("Stopping REST API server...")

	// Stop scheduler first
	if d.scheduler != nil {
		if err := d.scheduler.Stop(ctx); err != nil {
			d.logger.Error("Scheduler shutdown error", "error", err)
		}
	}

	// Stop HTTP server
	if err := d.server.Shutdown(ctx); err != nil {
		d.logger.Error("REST server shutdown error", "error", err)
		d.running = false
		return err
	}

	d.running = false
	d.logger.Info("REST API server stopped")
	return nil
}

// Dispatch sends a task for execution via the REST API.
// This is used for internal dispatching (not HTTP).
func (d *RESTDispatcher) Dispatch(ctx context.Context, taskID string, payload []byte) error {
	if !d.running {
		return ErrDispatcherNotStarted
	}

	// Validate task exists
	d.mu.RLock()
	task, exists := d.tasks[taskID]
	d.mu.RUnlock()

	if !exists {
		return fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}

	// Execute directly (internal dispatch)
	return task.Execute(ctx, payload)
}

// DispatchMultiple sends multiple tasks for execution via the REST API.
func (d *RESTDispatcher) DispatchMultiple(ctx context.Context, tasks []TaskDispatch) error {
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

// registerRoutes sets up Gin route handlers.
func (d *RESTDispatcher) registerRoutes(engine *gin.Engine) {
	// API v1 task routes
	api := engine.Group(d.basePath)
	{
		// POST /api/v1/tasks/{taskID} - Dispatch a task
		api.POST("/:taskID", d.handleDispatch)

		// POST /api/v1/tasks/schedule - Schedule a recurring task
		api.POST("/schedule", d.handleSchedule)

		// GET /api/v1/tasks/schedules - List all schedules
		api.GET("/schedules", d.handleListSchedules)

		// DELETE /api/v1/tasks/schedules/:scheduleID - Cancel a schedule
		api.DELETE("/schedules/:scheduleID", d.handleCancelSchedule)

		// GET /api/v1/tasks/health - Health check
		api.GET("/health", d.handleHealth)

		// GET /api/v1/tasks/types - List registered task types
		api.GET("/types", d.handleListTypes)
	}
}

// handleDispatch handles POST /api/v1/tasks/{taskID}
func (d *RESTDispatcher) handleDispatch(c *gin.Context) {
	// Extract taskID from URL path
	taskID := c.Param("taskID")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Task ID is required in URL path"})
		return
	}

	// Generate request ID for correlation
	requestID := uuid.New().String()
	c.Header("X-Request-Id", requestID)

	// Parse JSON body
	var req TaskDispatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}

	// Validate task ID matches URL
	if req.TaskID != "" && req.TaskID != taskID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Task ID in body must match URL path"})
		return
	}
	req.TaskID = taskID

	// Validate task is registered
	d.mu.RLock()
	task, exists := d.tasks[taskID]
	d.mu.RUnlock()

	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Task not found: %s", taskID)})
		return
	}

	// Execute task
	ctx := c.Request.Context()
	ctx = context.WithValue(ctx, "request_id", requestID)

	var execErr error
	if req.Sync && d.config.RESTConfig.EnableSyncExecution {
		// Synchronous execution
		execErr = task.Execute(ctx, req.Payload)
	} else {
		// Asynchronous execution - run in goroutine
		go func() {
			if err := task.Execute(ctx, req.Payload); err != nil {
				d.logger.Error("Async task execution failed",
					"request_id", requestID,
					"task_id", taskID,
					"error", err)
			}
		}()
		execErr = nil
	}

	// Build response
	response := TaskDispatchResponse{
		RequestID: requestID,
		TaskID:    taskID,
	}

	if execErr != nil {
		response.Status = "failed"
		response.Error = execErr.Error()
		c.JSON(http.StatusInternalServerError, response)
	} else {
		response.Status = "accepted"
		if req.Sync {
			response.Status = "completed"
			response.Result = req.Payload // Echo back for sync
		}
		statusCode := http.StatusOK
		if !req.Sync {
			statusCode = http.StatusAccepted
		}
		c.JSON(statusCode, response)
	}
}

// handleSchedule handles POST /api/v1/tasks/schedule
func (d *RESTDispatcher) handleSchedule(c *gin.Context) {
	var req ScheduleTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}

	// Validate task is registered
	d.mu.RLock()
	_, exists := d.tasks[req.TaskID]
	d.mu.RUnlock()

	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Task not found: %s", req.TaskID)})
		return
	}

	// Parse interval
	interval, err := time.ParseDuration(req.Interval)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid interval: %v", err)})
		return
	}

	// Create schedule
	scheduleID, err := d.scheduler.ScheduleTask(c.Request.Context(), req.TaskID, req.Payload, interval, &ScheduleTaskOptions{
		Name:          req.Name,
		MaxExecutions: req.MaxExecutions,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to create schedule: %v", err)})
		return
	}

	// Get schedule info for response
	info, err := d.scheduler.GetSchedule(c.Request.Context(), scheduleID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to get schedule: %v", err)})
		return
	}

	response := ScheduleTaskResponse{
		ScheduleID: scheduleID,
		TaskID:     req.TaskID,
		Name:       info.Name,
		Interval:   req.Interval,
		NextRun:    info.NextRun,
	}

	c.JSON(http.StatusCreated, response)
}

// handleListSchedules handles GET /api/v1/tasks/schedules
func (d *RESTDispatcher) handleListSchedules(c *gin.Context) {
	schedules := d.scheduler.GetAllSchedules(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{"schedules": schedules})
}

// handleCancelSchedule handles DELETE /api/v1/tasks/schedules/:scheduleID
func (d *RESTDispatcher) handleCancelSchedule(c *gin.Context) {
	scheduleID := c.Param("scheduleID")
	if scheduleID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Schedule ID is required"})
		return
	}

	if err := d.scheduler.CancelSchedule(c.Request.Context(), scheduleID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Failed to cancel schedule: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Schedule cancelled", "schedule_id": scheduleID})
}

// handleHealth handles GET /api/v1/tasks/health
func (d *RESTDispatcher) handleHealth(c *gin.Context) {
	d.mu.RLock()
	taskIDs := make([]string, 0, len(d.tasks))
	for id := range d.tasks {
		taskIDs = append(taskIDs, id)
	}
	d.mu.RUnlock()

	activeSchedules := 0
	if d.scheduler != nil {
		activeSchedules = d.scheduler.GetActiveSchedules()
	}

	response := HealthResponse{
		Status:          "healthy",
		Mode:            "rest",
		Timestamp:       time.Now(),
		RegisteredTasks: taskIDs,
		ActiveSchedules: activeSchedules,
	}

	c.JSON(http.StatusOK, response)
}

// handleListTypes handles GET /api/v1/tasks/types
func (d *RESTDispatcher) handleListTypes(c *gin.Context) {
	d.mu.RLock()
	taskIDs := make([]string, 0, len(d.tasks))
	for id := range d.tasks {
		taskIDs = append(taskIDs, id)
	}
	d.mu.RUnlock()

	c.JSON(http.StatusOK, gin.H{"task_types": taskIDs})
}

// loggingMiddleware logs HTTP requests using the taskmaster logger.
func (d *RESTDispatcher) loggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		// Process request
		c.Next()

		// Log after processing
		d.logger.Info("HTTP request",
			"method", c.Request.Method,
			"path", path,
			"status", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
			"remote_addr", c.ClientIP(),
			"request_id", c.Writer.Header().Get("X-Request-Id"))
	}
}

// IsRunning returns true if the REST server is running.
func (d *RESTDispatcher) IsRunning() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.running
}

// dispatcherWrapper wraps RESTDispatcher to implement Dispatcher interface for scheduler.
type dispatcherWrapper struct {
	tasks  map[string]Task
	logger Logger
}

func (w *dispatcherWrapper) Register(task Task) error {
	return nil // Not used in REST mode
}

func (w *dispatcherWrapper) Start(ctx context.Context) error {
	return nil // Already started
}

func (w *dispatcherWrapper) Stop(ctx context.Context) error {
	return nil // Handled by RESTDispatcher
}

func (w *dispatcherWrapper) Dispatch(ctx context.Context, taskID string, payload []byte) error {
	// Execute asynchronously for scheduled tasks
	go func() {
		task, exists := w.tasks[taskID]
		if !exists {
			w.logger.Error("Task not found", "task_id", taskID)
			return
		}
		if err := task.Execute(ctx, payload); err != nil {
			w.logger.Error("Scheduled task execution failed",
				"task_id", taskID,
				"error", err)
		}
	}()

	return nil
}

func (w *dispatcherWrapper) DispatchMultiple(ctx context.Context, tasks []TaskDispatch) error {
	// Execute all tasks asynchronously
	for _, task := range tasks {
		go func(taskID string, payload []byte) {
			t, exists := w.tasks[taskID]
			if !exists {
				w.logger.Error("Task not found", "task_id", taskID)
				return
			}
			if err := t.Execute(ctx, payload); err != nil {
				w.logger.Error("Scheduled task execution failed",
					"task_id", taskID,
					"error", err)
			}
		}(task.TaskID, task.Payload)
	}
	return nil
}

func (w *dispatcherWrapper) IsRunning() bool {
	return true
}

func (w *dispatcherWrapper) Mode() ExecutionMode {
	return RESTMode
}

var _ Dispatcher = (*dispatcherWrapper)(nil)
