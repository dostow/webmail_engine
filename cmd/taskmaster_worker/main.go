package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"webmail_engine/internal/config"
	"webmail_engine/internal/taskmaster"
	"webmail_engine/internal/workerconfig"
	"webmail_engine/internal/workers"
)

// TaskmasterWorker is a generic taskmaster worker that supports multiple execution modes.
// It demonstrates how to use the taskmaster system with configurable tasks.
//
// Operational Modes:
// - managed: Internal worker pool with goroutines (default)
// - rest: HTTP API endpoints for task submission
// - machinery: Distributed task processing via Redis/RabbitMQ
type TaskmasterWorker struct {
	Config     *workerconfig.WorkerConfig
	Dispatcher taskmaster.FullDispatcher
	Mode       string
}

// NewTaskmasterWorker creates a new taskmaster worker with the given configuration.
func NewTaskmasterWorker(cfg *workerconfig.WorkerConfig, mode string) (*TaskmasterWorker, error) {
	// Create dispatcher with mode-specific configuration
	execMode := toTaskmasterMode(mode)
	log.Printf("Initializing taskmaster dispatcher (mode=%s)...", execMode.String())

	dispatcher := createDispatcher(execMode, cfg, mode)

	// Register task implementations
	// Note: In production, inject actual service instances instead of nil
	dispatcher.Register(&workers.AIAnalysisTask{})
	dispatcher.Register(&workers.SpamCheckTask{})
	dispatcher.Register(&workers.SyncTask{})
	dispatcher.Register(&workers.EnvelopeProcessorTask{})

	// Register webhook notifier if configured
	if cfg.Webhook.Enabled && cfg.Webhook.URL != "" {
		webhookNotifier := &workers.WebhookNotifierTask{
			WebhookURL: cfg.Webhook.URL,
			SecretKey:  cfg.Webhook.SecretKey,
		}
		if err := dispatcher.Register(webhookNotifier); err != nil {
			log.Printf("Warning: Failed to register webhook notifier: %v", err)
		} else {
			log.Println("Webhook notifier registered")
		}
	}

	log.Printf("Registered tasks: %v", dispatcher.GetRegisteredTasks())

	return &TaskmasterWorker{
		Config:     cfg,
		Dispatcher: dispatcher,
		Mode:       mode,
	}, nil
}

// Start initializes and starts the taskmaster dispatcher.
func (w *TaskmasterWorker) Start() error {
	log.Printf("Starting taskmaster worker (ID: %s, Mode: %s)", w.Config.WorkerID, w.Mode)

	ctx := context.Background()
	if err := w.Dispatcher.Start(ctx); err != nil {
		return fmt.Errorf("failed to start dispatcher: %w", err)
	}

	log.Printf("Taskmaster dispatcher started successfully (mode=%s)", w.Mode)
	return nil
}

// Run blocks until a shutdown signal is received.
func (w *TaskmasterWorker) Run() error {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	return w.Stop()
}

// Stop gracefully shuts down the taskmaster worker.
func (w *TaskmasterWorker) Stop() error {
	log.Println("Shutting down taskmaster worker...")

	ctx, cancel := context.WithTimeout(context.Background(), w.Config.ShutdownTimeout)
	defer cancel()

	if err := w.Dispatcher.Stop(ctx); err != nil {
		return fmt.Errorf("error during shutdown: %w", err)
	}

	log.Println("Taskmaster worker stopped")
	return nil
}

// createDispatcher creates a taskmaster dispatcher with mode-specific configuration.
// Uses the new separated Dispatch and Execution config structure.
func createDispatcher(mode taskmaster.ExecutionMode, cfg *workerconfig.WorkerConfig, _ string) taskmaster.FullDispatcher {
	// Get dispatch-specific configuration
	addr, redisURL, queueName := cfg.GetDispatchConfig()

	switch mode {
	case taskmaster.RESTMode:
		return taskmaster.NewDispatcher(
			taskmaster.WithMode(mode),
			taskmaster.WithRESTConfig(&taskmaster.RESTConfig{
				Addr:                addr,
				BasePath:            "/api/v1/tasks",
				EnableSyncExecution: true,
			}),
			taskmaster.WithLogger(&standardLogger{}),
		)
	case taskmaster.MachineryMode:
		return taskmaster.NewDispatcher(
			taskmaster.WithMode(mode),
			taskmaster.WithMachineryConfig(&taskmaster.MachineryConfig{
				BrokerURL:         redisURL,
				ResultBackend:     cfg.Execution.ResultBackend,
				DefaultQueue:      queueName,
				DefaultRetryCount: 3,
			}),
			taskmaster.WithLogger(&standardLogger{}),
		)
	default: // ManagedMode
		workerCount := cfg.Execution.WorkerCount
		if workerCount <= 0 {
			workerCount = 4
		}
		taskTimeout := cfg.Execution.TaskTimeout
		if taskTimeout <= 0 {
			taskTimeout = 30 * time.Second
		}
		queueSize := cfg.Execution.QueueSize
		if queueSize <= 0 {
			queueSize = 100
		}
		return taskmaster.NewDispatcher(
			taskmaster.WithMode(mode),
			taskmaster.WithWorkerCount(workerCount),
			taskmaster.WithTaskTimeout(taskTimeout),
			taskmaster.WithQueueSize(queueSize),
			taskmaster.WithLogger(&standardLogger{}),
		)
	}
}

// toTaskmasterMode converts string mode to taskmaster.ExecutionMode.
func toTaskmasterMode(mode string) taskmaster.ExecutionMode {
	switch mode {
	case "rest":
		return taskmaster.RESTMode
	case "machinery":
		return taskmaster.MachineryMode
	default:
		return taskmaster.ManagedMode
	}
}

// resolveOperationalMode implements precedence rules: CLI > Config > Default.
// Supports both legacy mode names (scheduled_managed) and new names (managed).
func resolveOperationalMode(cliMode, configMode string) string {
	validModes := map[string]bool{
		"managed":           true,
		"scheduled_managed": true, // Legacy alias for managed
		"rest":              true,
		"machinery":         true,
	}

	// CLI argument takes precedence
	if cliMode != "" {
		if !validModes[cliMode] {
			log.Fatalf("Invalid mode '%s'. Valid modes: managed, rest, machinery", cliMode)
		}
		// Normalize legacy mode name
		if cliMode == "scheduled_managed" {
			return "managed"
		}
		return cliMode
	}

	// Config file value
	if configMode != "" {
		if !validModes[configMode] {
			log.Fatalf("Invalid config mode '%s'. Valid modes: managed, rest, machinery", configMode)
		}
		// Normalize legacy mode name
		if configMode == "scheduled_managed" {
			return "managed"
		}
		return configMode
	}

	// Default
	log.Println("No mode specified, defaulting to 'managed'")
	return "managed"
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

func main() {
	// Parse command line flags
	configPath := flag.String("config", "", "Path to configuration file")
	mode := flag.String("mode", "", "Operational mode: managed, rest, machinery (overrides config)")
	flag.Parse()

	// Load or create configuration
	var cfg *workerconfig.WorkerConfig
	var err error

	if *configPath != "" {
		cfg, err = workerconfig.LoadWorkerConfig(*configPath)
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}
		// Expand environment variables after loading
		config.ExpandEnvVars(cfg)
	} else {
		cfg = workerconfig.DefaultWorkerConfig("taskmaster")
		config.ExpandEnvVars(cfg)
	}

	// Override webhook.enabled from environment if set
	// (boolean env vars need special handling since JSON can't expand them)
	if webhookEnabled := os.Getenv("WEBHOOK_ENABLED"); webhookEnabled != "" {
		cfg.Webhook.Enabled = webhookEnabled == "true" || webhookEnabled == "1" || webhookEnabled == "yes"
	}

	// Resolve mode: CLI > Config > Default
	// Use GetExecutionMode() which considers both Dispatch and Execution config
	operationalMode := resolveOperationalMode(*mode, cfg.GetExecutionMode())

	// Log startup info with dispatch/execution details
	log.Printf("=== Taskmaster Worker ===")
	log.Printf("Worker ID:  %s", cfg.WorkerID)
	log.Printf("Dispatch:   %s", cfg.Dispatch.Type)
	log.Printf("Execution:  %s (%s mode)", cfg.Execution.Mode, operationalMode)
	log.Printf("=========================")

	// Create and run taskmaster worker
	worker, err := NewTaskmasterWorker(cfg, operationalMode)
	if err != nil {
		log.Fatalf("Failed to create taskmaster worker: %v", err)
	}

	if err := worker.Start(); err != nil {
		log.Fatalf("Failed to start taskmaster worker: %v", err)
	}

	if err := worker.Run(); err != nil {
		log.Fatalf("Taskmaster worker error: %v", err)
	}
}
