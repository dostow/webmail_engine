package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"webmail_engine/internal/taskmaster"
	"webmail_engine/internal/workers"
)

// This is an example worker using the new taskmaster system.
// It demonstrates how to initialize and use the Dispatcher with different execution modes.
//
// Usage:
//   go run cmd/taskmaster_worker/main.go -mode managed -workers 4
//   go run cmd/taskmaster_worker/main.go -mode rest -addr :8080
//   go run cmd/taskmaster_worker/main.go -mode machinery -broker redis://localhost:6379

func main() {
	// Parse command line flags
	mode := flag.String("mode", "managed", "Execution mode: managed, rest, machinery")
	workerCount := flag.Int("workers", 4, "Number of worker goroutines (for managed mode)")
	taskTimeout := flag.Duration("timeout", 30*time.Second, "Default task timeout")
	queueSize := flag.Int("queue-size", 100, "Task queue size (for managed mode)")
	addr := flag.String("addr", ":8080", "HTTP server address (for rest mode)")
	brokerURL := flag.String("broker", "redis://localhost:6379", "Broker URL (for machinery mode)")
	flag.Parse()

	// Create logger (using standard log for simplicity)
	logger := &standardLogger{}

	// Determine execution mode
	var execMode taskmaster.ExecutionMode
	switch *mode {
	case "managed":
		execMode = taskmaster.ManagedMode
	case "rest":
		execMode = taskmaster.RESTMode
	case "machinery":
		execMode = taskmaster.MachineryMode
	default:
		log.Fatalf("Unknown execution mode: %s", *mode)
	}

	// Create dispatcher with functional options
	log.Printf("Initializing taskmaster dispatcher (mode=%s)...", execMode.String())

	dispatcher := taskmaster.NewDispatcher(
		taskmaster.WithMode(execMode),
		taskmaster.WithWorkerCount(*workerCount),
		taskmaster.WithTaskTimeout(*taskTimeout),
		taskmaster.WithQueueSize(*queueSize),
		taskmaster.WithLogger(logger),
	)

	// Configure mode-specific options
	switch execMode {
	case taskmaster.RESTMode:
		dispatcher = taskmaster.NewDispatcher(
			taskmaster.WithMode(execMode),
			taskmaster.WithRESTConfig(&taskmaster.RESTConfig{
				Addr:                *addr,
				BasePath:            "/api/v1/tasks",
				EnableSyncExecution: true,
			}),
			taskmaster.WithLogger(logger),
		)
	case taskmaster.MachineryMode:
		dispatcher = taskmaster.NewDispatcher(
			taskmaster.WithMode(execMode),
			taskmaster.WithMachineryConfig(&taskmaster.MachineryConfig{
				BrokerURL:         *brokerURL,
				ResultBackend:     *brokerURL,
				DefaultQueue:      "webmail_tasks",
				DefaultRetryCount: 3,
			}),
			taskmaster.WithLogger(logger),
		)
	}

	// Register task implementations
	// Note: In a real application, you would inject actual service implementations
	dispatcher.Register(&workers.AIAnalysisTask{})
	dispatcher.Register(&workers.SpamCheckTask{})
	dispatcher.Register(&workers.SyncTask{})
	dispatcher.Register(&workers.EnvelopeProcessorTask{})

	log.Printf("Registered tasks: %v", dispatcher.GetRegisteredTasks())

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the dispatcher
	if err := dispatcher.Start(ctx); err != nil {
		log.Fatalf("Failed to start dispatcher: %v", err)
	}

	log.Printf("Taskmaster dispatcher started successfully (mode=%s)", execMode.String())

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	<-sigChan

	log.Println("Shutting down taskmaster dispatcher...")

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := dispatcher.Stop(shutdownCtx); err != nil {
		log.Printf("Error during shutdown: %v", err)
	}

	log.Println("Taskmaster dispatcher stopped")
}

// standardLogger adapts the standard log package to the taskmaster.Logger interface.
type standardLogger struct{}

func (l *standardLogger) Info(msg string, keysAndValues ...interface{}) {
	log.Printf("[INFO] %s %v", msg, keysAndValues)
}

func (l *standardLogger) Error(msg string, keysAndValues ...interface{}) {
	log.Printf("[ERROR] %s %v", msg, keysAndValues)
}

func (l *standardLogger) Debug(msg string, keysAndValues ...interface{}) {
	log.Printf("[DEBUG] %s %v", msg, keysAndValues)
}

func (l *standardLogger) Warn(msg string, keysAndValues ...interface{}) {
	log.Printf("[WARN] %s %v", msg, keysAndValues)
}
