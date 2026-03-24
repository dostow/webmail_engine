package taskmaster

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// MockTask is a test implementation of the Task interface.
type MockTask struct {
	id       string
	execFunc func(ctx context.Context, payload []byte) error
}

func (t *MockTask) ID() string {
	return t.id
}

func (t *MockTask) Execute(ctx context.Context, payload []byte) error {
	if t.execFunc != nil {
		return t.execFunc(ctx, payload)
	}
	return nil
}

// MockLogger is a test implementation of the Logger interface.
type MockLogger struct {
	mu       sync.Mutex
	messages []string
}

func (l *MockLogger) Info(msg string, keysAndValues ...interface{}) {
	l.log("INFO", msg)
}

func (l *MockLogger) Error(msg string, keysAndValues ...interface{}) {
	l.log("ERROR", msg)
}

func (l *MockLogger) Debug(msg string, keysAndValues ...interface{}) {
	l.log("DEBUG", msg)
}

func (l *MockLogger) Warn(msg string, keysAndValues ...interface{}) {
	l.log("WARN", msg)
}

func (l *MockLogger) log(level, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, "["+level+"] "+msg)
}

func (l *MockLogger) GetMessages() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	result := make([]string, len(l.messages))
	copy(result, l.messages)
	return result
}

// TestDispatcherRegistration tests task registration.
func TestDispatcherRegistration(t *testing.T) {
	tests := []struct {
		name        string
		task        Task
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid task registration",
			task:        &MockTask{id: "test_task"},
			expectError: false,
		},
		{
			name:        "nil task",
			task:        nil,
			expectError: true,
			errorMsg:    "cannot register nil task",
		},
		{
			name:        "empty task ID",
			task:        &MockTask{id: ""},
			expectError: true,
			errorMsg:    "task ID cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dispatcher := NewDispatcher()
			err := dispatcher.Register(tt.task)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				if tt.errorMsg != "" && !containsSubstring(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// containsSubstring checks if a string contains a substring.
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestDispatcherDuplicateRegistration tests that duplicate task IDs are rejected.
func TestDispatcherDuplicateRegistration(t *testing.T) {
	dispatcher := NewDispatcher()
	task := &MockTask{id: "duplicate_task"}

	// First registration should succeed
	if err := dispatcher.Register(task); err != nil {
		t.Fatalf("first registration failed: %v", err)
	}

	// Second registration should fail
	if err := dispatcher.Register(task); err == nil {
		t.Error("expected error for duplicate registration, got nil")
	} else if !errors.Is(err, ErrTaskAlreadyRegistered) && err.Error() != "task already registered: duplicate_task" {
		t.Errorf("expected ErrTaskAlreadyRegistered, got %v", err)
	}
}

// TestDispatcherStartStop tests the dispatcher lifecycle.
func TestDispatcherStartStop(t *testing.T) {
	dispatcher := NewDispatcher(WithMode(ManagedMode))

	ctx := context.Background()

	// Start should succeed
	if err := dispatcher.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !dispatcher.IsRunning() {
		t.Error("dispatcher should be running after Start")
	}

	// Second start should fail
	if err := dispatcher.Start(ctx); err == nil {
		t.Error("expected error for second Start, got nil")
	}

	// Stop should succeed
	if err := dispatcher.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if dispatcher.IsRunning() {
		t.Error("dispatcher should not be running after Stop")
	}
}

// TestDispatcherDispatchBeforeStart tests that dispatch fails before Start.
func TestDispatcherDispatchBeforeStart(t *testing.T) {
	dispatcher := NewDispatcher(WithMode(ManagedMode))
	dispatcher.Register(&MockTask{id: "test"})

	ctx := context.Background()

	// Dispatch before Start should fail
	err := dispatcher.Dispatch(ctx, "test", []byte{})
	if err == nil {
		t.Error("expected error for dispatch before start, got nil")
	}
	if err != ErrDispatcherNotStarted {
		t.Errorf("expected ErrDispatcherNotStarted, got %v", err)
	}
}

// TestDispatcherDispatchUnknownTask tests dispatching an unknown task.
func TestDispatcherDispatchUnknownTask(t *testing.T) {
	dispatcher := NewDispatcher(WithMode(ManagedMode))

	ctx := context.Background()
	if err := dispatcher.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer dispatcher.Stop(ctx)

	err := dispatcher.Dispatch(ctx, "unknown_task", []byte{})
	if err == nil {
		t.Error("expected error for unknown task, got nil")
	}
	if !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}
}

// TestDispatcherManagedModeExecution tests task execution in managed mode.
func TestDispatcherManagedModeExecution(t *testing.T) {
	var executed bool
	var mu sync.Mutex

	dispatcher := NewDispatcher(
		WithMode(ManagedMode),
		WithWorkerCount(2),
		WithLogger(&MockLogger{}),
	)

	task := &MockTask{
		id: "test_exec",
		execFunc: func(ctx context.Context, payload []byte) error {
			mu.Lock()
			defer mu.Unlock()
			executed = true
			return nil
		},
	}

	if err := dispatcher.Register(task); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	ctx := context.Background()
	if err := dispatcher.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer dispatcher.Stop(ctx)

	// Give workers time to start
	time.Sleep(100 * time.Millisecond)

	// Dispatch task
	if err := dispatcher.Dispatch(ctx, "test_exec", []byte("test payload")); err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}

	// Wait for execution
	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	if !executed {
		t.Error("task was not executed")
	}
	mu.Unlock()
}

// TestDispatcherFunctionalOptions tests the functional options pattern.
func TestDispatcherFunctionalOptions(t *testing.T) {
	logger := &MockLogger{}

	dispatcher := NewDispatcher(
		WithMode(ManagedMode),
		WithWorkerCount(8),
		WithTaskTimeout(60*time.Second),
		WithQueueSize(200),
		WithLogger(logger),
	)

	if dispatcher.Mode() != ManagedMode {
		t.Errorf("expected mode ManagedMode, got %v", dispatcher.Mode())
	}

	if dispatcher.config.WorkerCount != 8 {
		t.Errorf("expected worker count 8, got %d", dispatcher.config.WorkerCount)
	}

	if dispatcher.config.TaskTimeout != 60*time.Second {
		t.Errorf("expected task timeout 60s, got %v", dispatcher.config.TaskTimeout)
	}

	if dispatcher.config.QueueSize != 200 {
		t.Errorf("expected queue size 200, got %d", dispatcher.config.QueueSize)
	}
}

// TestTaskError tests the TaskError type.
func TestTaskError(t *testing.T) {
	baseErr := errors.New("base error")

	tests := []struct {
		name            string
		taskErr         *TaskError
		expectRetryable bool
	}{
		{
			name:            "retryable error",
			taskErr:         NewRetryableTaskError("test", "retryable error", baseErr),
			expectRetryable: true,
		},
		{
			name:            "non-retryable error",
			taskErr:         NewNonRetryableTaskError("test", "non-retryable error", baseErr),
			expectRetryable: false,
		},
		{
			name:            "system error",
			taskErr:         NewSystemTaskError("test", "system error", baseErr),
			expectRetryable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.taskErr.IsRetryable() != tt.expectRetryable {
				t.Errorf("expected IsRetryable=%v, got %v", tt.expectRetryable, tt.taskErr.IsRetryable())
			}

			if tt.taskErr.Error() == "" {
				t.Error("Error() should not return empty string")
			}

			if !errors.Is(tt.taskErr, tt.taskErr.Err) && tt.taskErr.Err != nil {
				// Unwrap should work
				unwrapped := errors.Unwrap(tt.taskErr)
				if unwrapped != tt.taskErr.Err {
					t.Errorf("expected unwrapped error to be %v, got %v", tt.taskErr.Err, unwrapped)
				}
			}
		})
	}
}

// TestIsRetryable tests the IsRetryable helper function.
func TestIsRetryable(t *testing.T) {
	baseErr := errors.New("base error")

	tests := []struct {
		name            string
		err             error
		expectRetryable bool
	}{
		{
			name:            "retryable task error",
			err:             NewRetryableTaskError("test", "retryable", baseErr),
			expectRetryable: true,
		},
		{
			name:            "non-retryable task error",
			err:             NewNonRetryableTaskError("test", "non-retryable", baseErr),
			expectRetryable: false,
		},
		{
			name:            "regular error",
			err:             baseErr,
			expectRetryable: false,
		},
		{
			name:            "nil error",
			err:             nil,
			expectRetryable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if IsRetryable(tt.err) != tt.expectRetryable {
				t.Errorf("expected IsRetryable=%v for %v", tt.expectRetryable, tt.err)
			}
		})
	}
}

// TestManagedDispatcherStats tests the managed dispatcher statistics.
func TestManagedDispatcherStats(t *testing.T) {
	logger := &MockLogger{}
	tasks := map[string]Task{
		"test": &MockTask{id: "test"},
	}

	config := &DispatcherConfig{
		Mode:        ManagedMode,
		WorkerCount: 2,
		TaskTimeout: 5 * time.Second,
		QueueSize:   10,
	}

	dispatcher := NewManagedDispatcher(tasks, config, logger)

	ctx := context.Background()
	if err := dispatcher.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer dispatcher.Stop(ctx)

	// Dispatch some tasks
	for i := 0; i < 5; i++ {
		dispatcher.Dispatch(ctx, "test", []byte{})
	}

	// Wait for processing
	time.Sleep(500 * time.Millisecond)

	stats := dispatcher.GetStats()
	if stats.TasksReceived != 5 {
		t.Errorf("expected 5 tasks received, got %d", stats.TasksReceived)
	}
}

// TestExecutionModeString tests the ExecutionMode String method.
func TestExecutionModeString(t *testing.T) {
	tests := []struct {
		mode     ExecutionMode
		expected string
	}{
		{ManagedMode, "managed"},
		{RESTMode, "rest"},
		{MachineryMode, "machinery"},
		{999, "unknown"},
	}

	for _, tt := range tests {
		if tt.mode.String() != tt.expected {
			t.Errorf("mode %d: expected %q, got %q", tt.mode, tt.expected, tt.mode.String())
		}
	}
}

// TestDefaultDispatcherConfig tests the default configuration.
func TestDefaultDispatcherConfig(t *testing.T) {
	config := DefaultDispatcherConfig()

	if config.Mode != ManagedMode {
		t.Errorf("expected default mode ManagedMode, got %v", config.Mode)
	}

	if config.WorkerCount != 4 {
		t.Errorf("expected default worker count 4, got %d", config.WorkerCount)
	}

	if config.TaskTimeout != 30*time.Second {
		t.Errorf("expected default task timeout 30s, got %v", config.TaskTimeout)
	}

	if config.QueueSize != 100 {
		t.Errorf("expected default queue size 100, got %d", config.QueueSize)
	}

	if config.RESTConfig == nil {
		t.Error("expected RESTConfig to be set")
	}

	if config.MachineryConfig == nil {
		t.Error("expected MachineryConfig to be set")
	}
}

// BenchmarkDispatcherDispatch benchmarks task dispatching.
func BenchmarkDispatcherDispatch(b *testing.B) {
	dispatcher := NewDispatcher(
		WithMode(ManagedMode),
		WithWorkerCount(4),
		WithLogger(&MockLogger{}),
	)

	dispatcher.Register(&MockTask{id: "bench"})

	ctx := context.Background()
	dispatcher.Start(ctx)
	defer dispatcher.Stop(ctx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dispatcher.Dispatch(ctx, "bench", []byte("payload"))
	}
}
