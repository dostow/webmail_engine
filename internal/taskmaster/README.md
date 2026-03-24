# Taskmaster - Centralized Task Management System

A modern, idiomatic Go task management system that cleanly separates task execution from dispatch mechanisms. Taskmaster supports multiple execution modes while maintaining clean architecture and domain logic separation.

## Features

- **Multiple Execution Modes**: Switch between managed worker pools, REST API, or Machinery v2 distributed queues
- **Clean Architecture**: Domain logic in `internal/workers`, infrastructure in `internal/taskmaster`
- **Context Propagation**: Full context.Context support for cancellation and timeouts
- **Graceful Shutdown**: Proper cleanup of in-progress tasks with configurable timeouts
- **Functional Options**: Flexible configuration using the functional options pattern
- **Structured Error Handling**: Categorized errors for retry decisions
- **Built-in Monitoring**: Task statistics and health monitoring

## Directory Structure

```
internal/
├── taskmaster/
│   ├── dispatcher.go      # Core initialization, routing, and functional options
│   ├── interface.go       # Task and Dispatcher interface definitions
│   ├── machinery.go       # Machinery v2 worker/server integration layer
│   ├── managed.go         # Worker pool and goroutine management
│   ├── rest.go            # HTTP handlers for REST API requests
│   └── errors.go          # Error types and categorization
└── workers/
    ├── ai_analysis.go     # AI-powered email analysis task
    ├── spam_check.go      # Spam detection task
    ├── sync.go            # Email synchronization task
    └── envelope_processor.go # Envelope processing task
```

## Quick Start

### Basic Usage (Managed Mode)

```go
package main

import (
    "context"
    "log"
    "time"

    "webmail_engine/internal/taskmaster"
    "webmail_engine/internal/workers"
)

func main() {
    // Create dispatcher with functional options
    dispatcher := taskmaster.NewDispatcher(
        taskmaster.WithMode(taskmaster.ManagedMode),
        taskmaster.WithWorkerCount(10),
        taskmaster.WithTaskTimeout(30*time.Second),
    )

    // Register task implementations
    dispatcher.Register(&workers.SyncTask{})
    dispatcher.Register(&workers.EnvelopeProcessorTask{})
    dispatcher.Register(&workers.AIAnalysisTask{})
    dispatcher.Register(&workers.SpamCheckTask{})

    // Start the dispatcher
    ctx := context.Background()
    if err := dispatcher.Start(ctx); err != nil {
        log.Fatal("Failed to start:", err)
    }

    // Dispatch tasks
    payload := []byte(`{"account_id": "acc_123", "message_id": "msg_456"}`)
    if err := dispatcher.Dispatch(ctx, "sync", payload); err != nil {
        log.Printf("Dispatch error: %v", err)
    }

    // Graceful shutdown
    defer func() {
        shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
        defer cancel()
        dispatcher.Stop(shutdownCtx)
    }()
}
```

### REST API Mode

```go
dispatcher := taskmaster.NewDispatcher(
    taskmaster.WithMode(taskmaster.RESTMode),
    taskmaster.WithRESTConfig(&taskmaster.RESTConfig{
        Addr:                ":8080",
        BasePath:            "/api/v1/tasks",
        EnableSyncExecution: true,
    }),
)

dispatcher.Register(&workers.SyncTask{})
dispatcher.Start(ctx)

// Tasks are now available at:
// POST /api/v1/tasks/sync
// POST /api/v1/tasks/envelope_processor
// GET  /api/v1/tasks/health
```

### Machinery v2 Mode (Distributed)

```go
dispatcher := taskmaster.NewDispatcher(
    taskmaster.WithMode(taskmaster.MachineryMode),
    taskmaster.WithMachineryConfig(&taskmaster.MachineryConfig{
        BrokerURL:         "redis://localhost:6379",
        ResultBackend:     "redis://localhost:6379",
        DefaultQueue:      "webmail_tasks",
        DefaultRetryCount: 3,
    }),
)

dispatcher.Register(&workers.SyncTask{})
dispatcher.Start(ctx)
```

## Execution Modes

### Managed Mode

Uses an internal worker pool with goroutines and channels. Best for:
- Standalone services with predictable workloads
- Single-instance deployments
- Low-latency requirements

```go
dispatcher := taskmaster.NewDispatcher(
    taskmaster.WithMode(taskmaster.ManagedMode),
    taskmaster.WithWorkerCount(8),
    taskmaster.WithQueueSize(200),
    taskmaster.WithTaskTimeout(60*time.Second),
)
```

### REST Mode

Exposes tasks via HTTP endpoints. Best for:
- Microservices architecture
- External integrations
- Language-agnostic task submission

Endpoints:
- `POST /api/v1/tasks/{taskID}` - Dispatch a task
- `GET /api/v1/tasks/health` - Health check
- `GET /api/v1/tasks/types` - List registered task types

Request format:
```json
{
    "task_id": "sync",
    "payload": {"account_id": "acc_123"},
    "sync": false
}
```

### Machinery Mode

Integrates with Machinery v2 for distributed task queues. Best for:
- Horizontal scaling across multiple workers
- Redis/RabbitMQ backends
- Complex retry and routing requirements

> **Note**: Machinery v2 requires adding the dependency:
> ```bash
> go get github.com/RichardKnop/machinery/v2
> ```

## Creating Custom Tasks

Implement the `taskmaster.Task` interface:

```go
package workers

import (
    "context"
    "encoding/json"
    "webmail_engine/internal/taskmaster"
)

type MyCustomTask struct {
    // Inject dependencies here
    MyService MyService
}

type MyCustomPayload struct {
    AccountID string `json:"account_id"`
    Data      string `json:"data"`
}

func (t *MyCustomTask) ID() string {
    return "my_custom_task"
}

func (t *MyCustomTask) Execute(ctx context.Context, payload []byte) error {
    // Parse payload
    var req MyCustomPayload
    if err := json.Unmarshal(payload, &req); err != nil {
        return taskmaster.NewNonRetryableTaskError(t.ID(), "invalid payload", err)
    }

    // Validate
    if req.AccountID == "" {
        return taskmaster.NewNonRetryableTaskError(t.ID(), "account_id required", nil)
    }

    // Execute business logic
    return t.MyService.Process(ctx, req.AccountID, req.Data)
}
```

## Error Handling

Taskmaster provides structured error types for proper error handling:

```go
// Retryable error (transient, retry may succeed)
return taskmaster.NewRetryableTaskError("sync", "network timeout", err)

// Non-retryable error (permanent, retry won't help)
return taskmaster.NewNonRetryableTaskError("sync", "invalid account_id", nil)

// System error (requires attention)
return taskmaster.NewSystemTaskError("sync", "service not configured", nil)

// Wrap existing errors
return taskmaster.WrapError("sync", "processing failed", err)

// Check error types
if taskmaster.IsRetryable(err) {
    // Schedule retry
}
```

## Configuration Options

```go
taskmaster.NewDispatcher(
    // Core options
    taskmaster.WithMode(taskmaster.ManagedMode),
    taskmaster.WithWorkerCount(10),
    taskmaster.WithTaskTimeout(30*time.Second),
    taskmaster.WithQueueSize(100),
    
    // REST mode options
    taskmaster.WithRESTConfig(&taskmaster.RESTConfig{
        Addr:                ":8080",
        BasePath:            "/api/v1/tasks",
        EnableSyncExecution: true,
    }),
    
    // Machinery mode options
    taskmaster.WithMachineryConfig(&taskmaster.MachineryConfig{
        BrokerURL:         "redis://localhost:6379",
        ResultBackend:     "redis://localhost:6379",
        DefaultQueue:      "tasks",
        DefaultRetryCount: 3,
    }),
    
    // Logging
    taskmaster.WithLogger(customLogger),
)
```

## Monitoring and Statistics

```go
// Get registered tasks
tasks := dispatcher.GetRegisteredTasks()

// Check if running
if dispatcher.IsRunning() {
    // Get mode-specific stats
    if managed, ok := dispatcher.(*taskmaster.DispatcherImpl); ok {
        // Access internal stats
    }
}
```

## Graceful Shutdown

```go
// Create context with timeout for shutdown
shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
defer cancel()

// Stop accepts new tasks and waits for in-progress tasks
if err := dispatcher.Stop(shutdownCtx); err != nil {
    if errors.Is(err, taskmaster.ErrGracefulShutdownTimeout) {
        log.Println("Shutdown timed out, forcing stop")
    }
}
```

## Running the Example Worker

```bash
# Managed mode (default)
go run cmd/taskmaster_worker/main.go -mode managed -workers 4

# REST API mode
go run cmd/taskmaster_worker/main.go -mode rest -addr :8080

# Machinery mode
go run cmd/taskmaster_worker/main.go -mode machinery -broker redis://localhost:6379
```

## Testing

```bash
# Run all tests
go test ./internal/taskmaster/... -v

# Run with race detection
go test ./internal/taskmaster/... -race -v

# Run benchmarks
go test ./internal/taskmaster/... -bench=. -benchmem
```

## Migration Guide

### From Existing Worker Pattern

1. **Identify worker logic**: Find the core processing logic in your existing workers
2. **Create Task implementation**: Wrap the logic in a `taskmaster.Task` implementation
3. **Inject dependencies**: Move service dependencies to the Task struct
4. **Update initialization**: Replace worker startup with dispatcher initialization
5. **Configure execution mode**: Choose the appropriate mode for your use case

Example migration:

```go
// Before: Direct worker
worker := worker.NewSyncWorker(cfg, nil)
worker.Start()

// After: Taskmaster
dispatcher := taskmaster.NewDispatcher(
    taskmaster.WithMode(taskmaster.ManagedMode),
    taskmaster.WithWorkerCount(cfg.Concurrency),
)
dispatcher.Register(&workers.SyncTask{SyncService: syncService})
dispatcher.Start(ctx)
```

## Best Practices

1. **Keep tasks focused**: Each task should do one thing well
2. **Use context properly**: Always respect context cancellation
3. **Validate payloads**: Return `ErrInvalidPayload` for malformed input
4. **Categorize errors**: Use appropriate error categories for retry logic
5. **Inject dependencies**: Don't create services inside tasks
6. **Log appropriately**: Use the injected logger with structured fields
7. **Test thoroughly**: Include unit tests and integration tests

## License

Built in 2026 by @Fabric
