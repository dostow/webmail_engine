package taskmaster

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// TaskScheduler handles periodic task creation and scheduling.
// It is used by the sync worker to create recurring sync tasks for accounts.
type TaskScheduler struct {
	mu         sync.RWMutex
	dispatcher Dispatcher
	schedules  map[string]*schedule
	running    atomic.Bool
	stopChan   chan struct{}
	wg         sync.WaitGroup
	logger     Logger
}

// schedule represents a scheduled recurring task.
type schedule struct {
	mu             sync.RWMutex
	id             string
	taskID         string
	payload        []byte
	interval       time.Duration
	opts           *ScheduleTaskOptions
	nextRun        time.Time
	lastRun        *time.Time
	executionCount int
	isActive       bool
	createdAt      time.Time
	stopChan       chan struct{}
}

// NewTaskScheduler creates a new task scheduler.
func NewTaskScheduler(dispatcher Dispatcher, logger Logger) *TaskScheduler {
	return &TaskScheduler{
		dispatcher: dispatcher,
		schedules:  make(map[string]*schedule),
		stopChan:   make(chan struct{}),
		logger:     logger,
	}
}

// Start begins the scheduler's background goroutines.
func (s *TaskScheduler) Start(ctx context.Context) error {
	if s.running.Load() {
		return ErrDispatcherAlreadyStarted
	}

	s.logger.Info("Starting task scheduler")
	s.running.Store(true)

	// Start schedule monitor goroutine
	s.wg.Add(1)
	go s.monitorSchedules(ctx)

	return nil
}

// Stop gracefully shuts down the scheduler.
func (s *TaskScheduler) Stop(ctx context.Context) error {
	if !s.running.Load() {
		return nil
	}

	s.logger.Info("Stopping task scheduler...")

	// Signal stop
	close(s.stopChan)

	// Wait for monitor to finish
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info("Task scheduler stopped")
		s.running.Store(false)
		return nil
	case <-ctx.Done():
		s.logger.Error("Task scheduler shutdown timed out")
		s.running.Store(false)
		return ctx.Err()
	}
}

// CreateTask creates a new task for immediate or delayed execution.
func (s *TaskScheduler) CreateTask(ctx context.Context, taskID string, payload []byte, opts *CreateTaskOptions) (string, error) {
	if !s.dispatcher.IsRunning() {
		return "", ErrDispatcherNotStarted
	}

	// Apply defaults
	if opts == nil {
		opts = &CreateTaskOptions{}
	}

	taskUUID := uuid.New().String()

	// Handle delay
	if opts.Delay > 0 {
		go func() {
			select {
			case <-time.After(opts.Delay):
				if err := s.dispatcher.Dispatch(ctx, taskID, payload); err != nil {
					s.logger.Error("Delayed task execution failed",
						"task_id", taskUUID,
						"type", taskID,
						"error", err)
				}
			case <-ctx.Done():
				return
			}
		}()
		s.logger.Debug("Scheduled delayed task", "task_id", taskUUID, "delay", opts.Delay)
	} else {
		// Immediate execution
		if err := s.dispatcher.Dispatch(ctx, taskID, payload); err != nil {
			return "", err
		}
	}

	s.logger.Debug("Created task", "task_id", taskUUID, "type", taskID)
	return taskUUID, nil
}

// ScheduleTask creates a recurring task that runs at the specified interval.
func (s *TaskScheduler) ScheduleTask(ctx context.Context, taskID string, payload []byte, interval time.Duration, opts *ScheduleTaskOptions) (string, error) {
	if !s.dispatcher.IsRunning() {
		return "", ErrDispatcherNotStarted
	}

	if interval <= 0 {
		return "", fmt.Errorf("interval must be positive")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Apply defaults
	if opts == nil {
		opts = &ScheduleTaskOptions{}
	}

	scheduleID := uuid.New().String()
	if opts.Name != "" {
		scheduleID = opts.Name
	}

	sched := &schedule{
		id:        scheduleID,
		taskID:    taskID,
		payload:   payload,
		interval:  interval,
		opts:      opts,
		nextRun:   time.Now(),
		isActive:  true,
		createdAt: time.Now(),
		stopChan:  make(chan struct{}),
	}

	if !opts.StartAt.IsZero() {
		sched.nextRun = opts.StartAt
	}

	s.schedules[scheduleID] = sched

	// Start goroutine for this schedule
	s.wg.Add(1)
	go s.runSchedule(ctx, sched)

	s.logger.Info("Created scheduled task",
		"schedule_id", scheduleID,
		"task_id", taskID,
		"interval", interval)

	return scheduleID, nil
}

// CancelSchedule stops a scheduled task from running.
func (s *TaskScheduler) CancelSchedule(ctx context.Context, scheduleID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sched, exists := s.schedules[scheduleID]
	if !exists {
		return fmt.Errorf("schedule not found: %s", scheduleID)
	}

	sched.mu.Lock()
	sched.isActive = false
	sched.mu.Unlock()

	close(sched.stopChan)
	delete(s.schedules, scheduleID)

	s.logger.Info("Cancelled scheduled task", "schedule_id", scheduleID)
	return nil
}

// GetSchedule returns information about a scheduled task.
func (s *TaskScheduler) GetSchedule(ctx context.Context, scheduleID string) (*ScheduleInfo, error) {
	s.mu.RLock()
	sched, exists := s.schedules[scheduleID]
	s.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("schedule not found: %s", scheduleID)
	}

	sched.mu.RLock()
	defer sched.mu.RUnlock()

	info := &ScheduleInfo{
		ID:             sched.id,
		TaskID:         sched.taskID,
		Name:           sched.opts.Name,
		Interval:       sched.interval,
		NextRun:        sched.nextRun,
		ExecutionCount: sched.executionCount,
		MaxExecutions:  sched.opts.MaxExecutions,
		IsActive:       sched.isActive,
		CreatedAt:      sched.createdAt,
	}

	if sched.lastRun != nil {
		info.LastRun = sched.lastRun
	}

	return info, nil
}

// runSchedule runs the main loop for a single schedule.
func (s *TaskScheduler) runSchedule(ctx context.Context, sched *schedule) {
	defer s.wg.Done()

	s.logger.Debug("Starting schedule runner",
		"schedule_id", sched.id,
		"task_id", sched.taskID,
		"interval", sched.interval)

	ticker := time.NewTicker(sched.interval)
	defer ticker.Stop()

	// Handle initial delay if start is in the future
	if sched.nextRun.After(time.Now()) {
		delay := time.Until(sched.nextRun)
		select {
		case <-time.After(delay):
			// Continue to ticker
		case <-sched.stopChan:
			return
		case <-ctx.Done():
			return
		}
	}

	for {
		select {
		case <-sched.stopChan:
			s.logger.Debug("Schedule stopped", "schedule_id", sched.id)
			return
		case <-ctx.Done():
			s.logger.Debug("Schedule context cancelled", "schedule_id", sched.id)
			return
		case <-ticker.C:
			sched.mu.Lock()
			if !sched.isActive {
				sched.mu.Unlock()
				return
			}

			// Check max executions
			if sched.opts.MaxExecutions > 0 && sched.executionCount >= sched.opts.MaxExecutions {
				s.logger.Info("Schedule reached max executions", "schedule_id", sched.id)
				sched.isActive = false
				sched.mu.Unlock()
				return
			}

			// Check end time
			if !sched.opts.EndAt.IsZero() && time.Now().After(sched.opts.EndAt) {
				s.logger.Info("Schedule reached end time", "schedule_id", sched.id)
				sched.isActive = false
				sched.mu.Unlock()
				return
			}

			sched.executionCount++
			now := time.Now()
			sched.lastRun = &now
			sched.nextRun = now.Add(sched.interval)
			sched.mu.Unlock()

			// Execute task
			if err := s.dispatcher.Dispatch(ctx, sched.taskID, sched.payload); err != nil {
				s.logger.Error("Scheduled task execution failed",
					"schedule_id", sched.id,
					"task_id", sched.taskID,
					"error", err)
			} else {
				s.logger.Debug("Scheduled task executed",
					"schedule_id", sched.id,
					"task_id", sched.taskID,
					"execution", sched.executionCount)
			}
		}
	}
}

// monitorSchedules periodically checks and logs schedule status.
func (s *TaskScheduler) monitorSchedules(ctx context.Context) {
	defer s.wg.Done()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.RLock()
			activeCount := 0
			for _, sched := range s.schedules {
				sched.mu.RLock()
				if sched.isActive {
					activeCount++
				}
				sched.mu.RUnlock()
			}
			s.mu.RUnlock()

			if activeCount > 0 {
				s.logger.Debug("Scheduler status", "active_schedules", activeCount)
			}
		}
	}
}

// GetActiveSchedules returns the number of active schedules.
func (s *TaskScheduler) GetActiveSchedules() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, sched := range s.schedules {
		sched.mu.RLock()
		if sched.isActive {
			count++
		}
		sched.mu.RUnlock()
	}
	return count
}

// GetAllSchedules returns information about all schedules.
func (s *TaskScheduler) GetAllSchedules(ctx context.Context) []*ScheduleInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	infos := make([]*ScheduleInfo, 0, len(s.schedules))
	for _, sched := range s.schedules {
		sched.mu.RLock()
		info := &ScheduleInfo{
			ID:             sched.id,
			TaskID:         sched.taskID,
			Name:           sched.opts.Name,
			Interval:       sched.interval,
			NextRun:        sched.nextRun,
			ExecutionCount: sched.executionCount,
			MaxExecutions:  sched.opts.MaxExecutions,
			IsActive:       sched.isActive,
			CreatedAt:      sched.createdAt,
		}
		if sched.lastRun != nil {
			info.LastRun = sched.lastRun
		}
		infos = append(infos, info)
		sched.mu.RUnlock()
	}

	return infos
}
