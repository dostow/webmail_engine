package taskmaster

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestManagedDispatcherStartStop tests the managed dispatcher lifecycle.
func TestManagedDispatcherStartStop(t *testing.T) {
	logger := &MockLogger{}
	tasks := map[string]Task{
		"test": &MockTask{id: "test"},
	}

	config := &DispatcherConfig{
		Mode:        ManagedMode,
		WorkerCount: 2,
		QueueSize:   10,
	}

	dispatcher := NewManagedDispatcher(tasks, config, logger)

	ctx := context.Background()

	// Start should succeed
	if err := dispatcher.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !dispatcher.IsRunning() {
		t.Error("dispatcher should be running after Start")
	}

	// Stop should succeed
	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := dispatcher.Stop(stopCtx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if dispatcher.IsRunning() {
		t.Error("dispatcher should not be running after Stop")
	}
}

// TestManagedDispatcherDispatch tests task dispatching.
func TestManagedDispatcherDispatch(t *testing.T) {
	var executed int32

	logger := &MockLogger{}
	tasks := map[string]Task{
		"test": &MockTask{
			id: "test",
			execFunc: func(ctx context.Context, payload []byte) error {
				atomic.AddInt32(&executed, 1)
				return nil
			},
		},
	}

	config := &DispatcherConfig{
		Mode:        ManagedMode,
		WorkerCount: 2,
		QueueSize:   10,
	}

	dispatcher := NewManagedDispatcher(tasks, config, logger)

	ctx := context.Background()
	if err := dispatcher.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer dispatcher.Stop(ctx)

	// Dispatch multiple tasks
	for i := 0; i < 5; i++ {
		if err := dispatcher.Dispatch(ctx, "test", []byte("payload")); err != nil {
			t.Errorf("Dispatch failed: %v", err)
		}
	}

	// Wait for execution
	time.Sleep(500 * time.Millisecond)

	if atomic.LoadInt32(&executed) != 5 {
		t.Errorf("expected 5 tasks executed, got %d", atomic.LoadInt32(&executed))
	}
}

// TestManagedDispatcherDispatchSync tests synchronous task execution.
func TestManagedDispatcherDispatchSync(t *testing.T) {
	var executed int32

	logger := &MockLogger{}
	tasks := map[string]Task{
		"test": &MockTask{
			id: "test",
			execFunc: func(ctx context.Context, payload []byte) error {
				atomic.AddInt32(&executed, 1)
				time.Sleep(10 * time.Millisecond) // Simulate work
				return nil
			},
		},
	}

	config := &DispatcherConfig{
		Mode:        ManagedMode,
		WorkerCount: 2,
		QueueSize:   10,
	}

	dispatcher := NewManagedDispatcher(tasks, config, logger)

	ctx := context.Background()
	if err := dispatcher.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer dispatcher.Stop(ctx)

	// Dispatch synchronously
	if err := dispatcher.DispatchSync(ctx, "test", []byte("payload")); err != nil {
		t.Fatalf("DispatchSync failed: %v", err)
	}

	if atomic.LoadInt32(&executed) != 1 {
		t.Errorf("expected 1 task executed, got %d", atomic.LoadInt32(&executed))
	}
}

// TestManagedDispatcherUnknownTask tests dispatching an unknown task.
func TestManagedDispatcherUnknownTask(t *testing.T) {
	logger := &MockLogger{}
	tasks := map[string]Task{} // No tasks registered

	config := &DispatcherConfig{
		Mode:        ManagedMode,
		WorkerCount: 2,
		QueueSize:   10,
	}

	dispatcher := NewManagedDispatcher(tasks, config, logger)

	ctx := context.Background()
	if err := dispatcher.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer dispatcher.Stop(ctx)

	err := dispatcher.Dispatch(ctx, "unknown", []byte{})
	if err == nil {
		t.Error("expected error for unknown task, got nil")
	}
}

// TestManagedDispatcherQueueFull tests behavior when queue is full.
func TestManagedDispatcherQueueFull(t *testing.T) {
	blocking := make(chan struct{})
	var blocked int32

	logger := &MockLogger{}
	tasks := map[string]Task{
		"blocking": &MockTask{
			id: "blocking",
			execFunc: func(ctx context.Context, payload []byte) error {
				atomic.AddInt32(&blocked, 1)
				<-blocking // Block forever
				return nil
			},
		},
	}

	config := &DispatcherConfig{
		Mode:        ManagedMode,
		WorkerCount: 1,
		QueueSize:   2, // Small queue
	}

	dispatcher := NewManagedDispatcher(tasks, config, logger)

	ctx := context.Background()
	if err := dispatcher.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer dispatcher.Stop(ctx)

	// Fill the worker
	dispatcher.Dispatch(ctx, "blocking", []byte{})
	time.Sleep(50 * time.Millisecond)

	// Fill the queue
	dispatcher.Dispatch(ctx, "blocking", []byte{})
	dispatcher.Dispatch(ctx, "blocking", []byte{})
	time.Sleep(50 * time.Millisecond)

	// Next dispatch should fail with queue full
	err := dispatcher.Dispatch(ctx, "blocking", []byte{})
	if err == nil {
		t.Error("expected error when queue is full, got nil")
	}

	close(blocking) // Unblock
}

// TestManagedDispatcherGracefulShutdown tests graceful shutdown with in-progress tasks.
func TestManagedDispatcherGracefulShutdown(t *testing.T) {
	var started int32
	var completed int32

	logger := &MockLogger{}
	tasks := map[string]Task{
		"slow": &MockTask{
			id: "slow",
			execFunc: func(ctx context.Context, payload []byte) error {
				atomic.AddInt32(&started, 1)
				time.Sleep(100 * time.Millisecond)
				atomic.AddInt32(&completed, 1)
				return nil
			},
		},
	}

	config := &DispatcherConfig{
		Mode:        ManagedMode,
		WorkerCount: 2,
		QueueSize:   10,
	}

	dispatcher := NewManagedDispatcher(tasks, config, logger)

	ctx := context.Background()
	if err := dispatcher.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Dispatch tasks
	for i := 0; i < 3; i++ {
		dispatcher.Dispatch(ctx, "slow", []byte{})
	}

	// Give tasks time to start
	time.Sleep(50 * time.Millisecond)

	// Stop with timeout
	stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if err := dispatcher.Stop(stopCtx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// All started tasks should complete
	if atomic.LoadInt32(&started) != atomic.LoadInt32(&completed) {
		t.Errorf("not all started tasks completed: started=%d, completed=%d",
			atomic.LoadInt32(&started), atomic.LoadInt32(&completed))
	}
}

// TestManagedDispatcherWorkerCount tests different worker counts.
func TestManagedDispatcherWorkerCount(t *testing.T) {
	tests := []struct {
		name        string
		workerCount int
		taskCount   int
	}{
		{"single worker", 1, 5},
		{"multiple workers", 4, 10},
		{"many workers", 8, 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var executed int32

			logger := &MockLogger{}
			tasks := map[string]Task{
				"test": &MockTask{
					id: "test",
					execFunc: func(ctx context.Context, payload []byte) error {
						atomic.AddInt32(&executed, 1)
						return nil
					},
				},
			}

			config := &DispatcherConfig{
				Mode:        ManagedMode,
				WorkerCount: tt.workerCount,
				QueueSize:   tt.taskCount,
			}

			dispatcher := NewManagedDispatcher(tasks, config, logger)

			ctx := context.Background()
			if err := dispatcher.Start(ctx); err != nil {
				t.Fatalf("Start failed: %v", err)
			}
			defer dispatcher.Stop(ctx)

			// Dispatch tasks
			for i := 0; i < tt.taskCount; i++ {
				dispatcher.Dispatch(ctx, "test", []byte{})
			}

			// Wait for execution
			time.Sleep(500 * time.Millisecond)

			if atomic.LoadInt32(&executed) != int32(tt.taskCount) {
				t.Errorf("expected %d tasks executed, got %d", tt.taskCount, atomic.LoadInt32(&executed))
			}
		})
	}
}

// TestManagedDispatcherTaskTimeout tests task timeout handling.
func TestManagedDispatcherTaskTimeout(t *testing.T) {
	var timedOut int32

	logger := &MockLogger{}
	tasks := map[string]Task{
		"slow": &MockTask{
			id: "slow",
			execFunc: func(ctx context.Context, payload []byte) error {
				select {
				case <-time.After(500 * time.Millisecond):
					return nil
				case <-ctx.Done():
					atomic.AddInt32(&timedOut, 1)
					return ctx.Err()
				}
			},
		},
	}

	config := &DispatcherConfig{
		Mode:        ManagedMode,
		WorkerCount: 1,
		TaskTimeout: 100 * time.Millisecond, // Short timeout
		QueueSize:   10,
	}

	dispatcher := NewManagedDispatcher(tasks, config, logger)

	ctx := context.Background()
	if err := dispatcher.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer dispatcher.Stop(ctx)

	dispatcher.Dispatch(ctx, "slow", []byte{})

	// Wait for timeout
	time.Sleep(300 * time.Millisecond)

	if atomic.LoadInt32(&timedOut) != 1 {
		t.Error("expected task to timeout")
	}
}

// TestManagedDispatcherStatsTracking tests statistics tracking.
func TestManagedDispatcherStatsTracking(t *testing.T) {
	logger := &MockLogger{}
	tasks := map[string]Task{
		"success": &MockTask{id: "success"},
		"failure": &MockTask{
			id: "failure",
			execFunc: func(ctx context.Context, payload []byte) error {
				return context.Canceled
			},
		},
	}

	config := &DispatcherConfig{
		Mode:        ManagedMode,
		WorkerCount: 2,
		QueueSize:   10,
	}

	dispatcher := NewManagedDispatcher(tasks, config, logger)

	ctx := context.Background()
	if err := dispatcher.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer dispatcher.Stop(ctx)

	// Dispatch mix of successful and failed tasks
	for i := 0; i < 3; i++ {
		dispatcher.Dispatch(ctx, "success", []byte{})
	}
	for i := 0; i < 2; i++ {
		dispatcher.Dispatch(ctx, "failure", []byte{})
	}

	// Wait for processing
	time.Sleep(500 * time.Millisecond)

	stats := dispatcher.GetStats()

	if stats.TasksReceived != 5 {
		t.Errorf("expected 5 tasks received, got %d", stats.TasksReceived)
	}

	if stats.TasksCompleted != 3 {
		t.Errorf("expected 3 tasks completed, got %d", stats.TasksCompleted)
	}

	if stats.TasksFailed != 2 {
		t.Errorf("expected 2 tasks failed, got %d", stats.TasksFailed)
	}
}

// TestManagedDispatcherConcurrentDispatch tests concurrent task dispatching.
func TestManagedDispatcherConcurrentDispatch(t *testing.T) {
	var executed int32

	logger := &MockLogger{}
	tasks := map[string]Task{
		"test": &MockTask{
			id: "test",
			execFunc: func(ctx context.Context, payload []byte) error {
				atomic.AddInt32(&executed, 1)
				return nil
			},
		},
	}

	config := &DispatcherConfig{
		Mode:        ManagedMode,
		WorkerCount: 4,
		QueueSize:   100,
	}

	dispatcher := NewManagedDispatcher(tasks, config, logger)

	ctx := context.Background()
	if err := dispatcher.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer dispatcher.Stop(ctx)

	// Dispatch from multiple goroutines
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				dispatcher.Dispatch(ctx, "test", []byte{})
			}
		}()
	}

	wg.Wait()

	// Wait for execution
	time.Sleep(500 * time.Millisecond)

	if atomic.LoadInt32(&executed) != 100 {
		t.Errorf("expected 100 tasks executed, got %d", atomic.LoadInt32(&executed))
	}
}

// BenchmarkManagedDispatcherDispatch benchmarks managed dispatcher performance.
func BenchmarkManagedDispatcherDispatch(b *testing.B) {
	logger := &MockLogger{}
	tasks := map[string]Task{
		"bench": &MockTask{id: "bench"},
	}

	config := &DispatcherConfig{
		Mode:        ManagedMode,
		WorkerCount: 4,
		QueueSize:   1000,
	}

	dispatcher := NewManagedDispatcher(tasks, config, logger)

	ctx := context.Background()
	dispatcher.Start(ctx)
	defer dispatcher.Stop(ctx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dispatcher.Dispatch(ctx, "bench", []byte("payload"))
	}
}
