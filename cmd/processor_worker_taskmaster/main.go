package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"webmail_engine/internal/config"
	"webmail_engine/internal/envelopequeue"
	"webmail_engine/internal/models"
	"webmail_engine/internal/service"
	"webmail_engine/internal/store"
	"webmail_engine/internal/taskmaster"
	"webmail_engine/internal/workerconfig"

	"webmail_engine/internal/workers"
)

// ProcessorWorkerWithTaskmaster is a processor worker that uses taskmaster for task execution.
// It reads envelopes from the queue and dispatches envelope processor tasks.
type ProcessorWorkerWithTaskmaster struct {
	Config     *workerconfig.WorkerConfig
	Queue      envelopequeue.EnvelopeQueue
	Store      store.AccountStore
	Dispatcher taskmaster.FullDispatcher
}

// NewProcessorWorkerWithTaskmaster creates a new processor worker using taskmaster.
func NewProcessorWorkerWithTaskmaster(cfg *workerconfig.WorkerConfig) (*ProcessorWorkerWithTaskmaster, error) {
	// Initialize queue
	queue, err := createQueue(cfg)
	if err != nil {
		return nil, err
	}

	// Initialize store
	accountStore, err := createStore(cfg)
	if err != nil {
		queue.Close()
		return nil, err
	}

	// Create dispatcher in managed mode with configurable worker count
	dispatcher := taskmaster.NewDispatcher(
		taskmaster.WithMode(taskmaster.ManagedMode),
		taskmaster.WithWorkerCount(cfg.ProcessorConfig.Concurrency),
		taskmaster.WithTaskTimeout(2*time.Minute), // Envelope processing timeout
		taskmaster.WithQueueSize(cfg.ProcessorConfig.BatchSize*2),
	)

	// Register the envelope processor task
	processorTask := &workers.EnvelopeProcessorTask{
		// ProcessorService would be injected here in a real implementation
	}
	if err := dispatcher.Register(processorTask); err != nil {
		queue.Close()
		accountStore.Close()
		return nil, err
	}

	return &ProcessorWorkerWithTaskmaster{
		Config:     cfg,
		Queue:      queue,
		Store:      accountStore,
		Dispatcher: dispatcher,
	}, nil
}

// Start begins the dispatcher and starts processing envelopes from the queue.
func (w *ProcessorWorkerWithTaskmaster) Start() error {
	log.Printf("Starting processor worker with taskmaster (ID: %s, Concurrency: %d)",
		w.Config.WorkerID, w.Config.ProcessorConfig.Concurrency)

	ctx := context.Background()

	// Start the dispatcher
	if err := w.Dispatcher.Start(ctx); err != nil {
		return err
	}

	// Start envelope processing loop
	go w.processEnvelopes(ctx)

	log.Println("Processor worker with taskmaster is running")
	return nil
}

// processEnvelopes reads envelopes from the queue and dispatches them as tasks.
func (w *ProcessorWorkerWithTaskmaster) processEnvelopes(ctx context.Context) {
	ticker := time.NewTicker(w.Config.ProcessorConfig.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Envelope processing loop stopped")
			return
		case <-ticker.C:
			w.processBatch(ctx)
		}
	}
}

// processBatch fetches a batch of envelopes and dispatches them as tasks.
func (w *ProcessorWorkerWithTaskmaster) processBatch(ctx context.Context) {
	// Get pending envelopes by priority
	priorityQueues, err := w.Queue.GetPendingByPriority(ctx, "")
	if err != nil {
		log.Printf("Failed to get pending envelopes: %v", err)
		return
	}

	// Process by priority order
	priorities := []models.EnvelopeProcessingPriority{
		models.PriorityHigh,
		models.PriorityNormal,
		models.PriorityLow,
	}
	totalDispatched := 0

	for _, priority := range priorities {
		envelopes := priorityQueues[priority]
		if len(envelopes) == 0 {
			continue
		}

		// Process up to BatchSize envelopes
		batchSize := w.Config.ProcessorConfig.BatchSize
		if len(envelopes) < batchSize {
			batchSize = len(envelopes)
		}

		for i := 0; i < batchSize; i++ {
			envelope := envelopes[i]

			// Create envelope processor payload
			payload := workers.EnvelopeProcessorPayload{
				EnvelopeID: envelope.ID,
				AccountID:  envelope.AccountID,
				FolderName: envelope.FolderName,
				UID:        envelope.UID,
				Options: workers.ProcessOptions{
					FetchBody:          true,
					ExtractLinks:       true,
					ProcessAttachments: true,
					TriggerWebhooks:    true,
				},
			}

			payloadBytes, err := json.Marshal(payload)
			if err != nil {
				log.Printf("Failed to marshal payload for envelope %s: %v", envelope.ID, err)
				continue
			}

			// Dispatch task
			if err := w.Dispatcher.Dispatch(ctx, "envelope_processor", payloadBytes); err != nil {
				log.Printf("Failed to dispatch envelope %s: %v", envelope.ID, err)
				continue
			}

			totalDispatched++

			// Update envelope status to processing
			if err := w.Queue.UpdateStatus(ctx, envelope.ID, "processing", ""); err != nil {
				log.Printf("Failed to update envelope status: %v", err)
			}
		}
	}

	if totalDispatched > 0 {
		log.Printf("Dispatched %d envelope processor tasks", totalDispatched)
	}
}

// Run blocks until shutdown signal.
func (w *ProcessorWorkerWithTaskmaster) Run() error {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	return w.Stop()
}

// Stop gracefully shuts down the worker.
func (w *ProcessorWorkerWithTaskmaster) Stop() error {
	log.Println("Shutting down processor worker with taskmaster...")

	ctx, cancel := context.WithTimeout(context.Background(), w.Config.ShutdownTimeout)
	defer cancel()

	// Stop dispatcher
	if err := w.Dispatcher.Stop(ctx); err != nil {
		log.Printf("Error stopping dispatcher: %v", err)
	}

	// Close queue and store
	if w.Queue != nil {
		w.Queue.Close()
	}
	if w.Store != nil {
		w.Store.Close()
	}

	log.Println("Processor worker with taskmaster stopped")
	return nil
}

// Helper functions

func createQueue(cfg *workerconfig.WorkerConfig) (envelopequeue.EnvelopeQueue, error) {
	switch cfg.Queue.Type {
	case "memory":
		return envelopequeue.NewMemoryEnvelopeQueue(envelopequeue.DefaultMemoryEnvelopeQueueConfig()), nil
	case "redis":
		// Would use Machinery queue for Redis
		config := cfg.ToEnvelopeQueueConfig()
		return envelopequeue.NewMachineryEnvelopeQueue(config)
	default:
		return envelopequeue.NewMemoryEnvelopeQueue(envelopequeue.DefaultMemoryEnvelopeQueueConfig()), nil
	}
}

func createStore(cfg *workerconfig.WorkerConfig) (store.AccountStore, error) {
	var accountStore store.AccountStore
	var err error

	switch cfg.Store.Type {
	case "sql":
		if cfg.Store.SQL == nil {
			cfg.Store.SQL = &config.SQLConfig{Driver: "sqlite", DSN: "./data/webmail.db", MaxConnections: 10}
		}
		accountStore, err = store.NewSQLStore(*cfg.Store.SQL)
	case "memory":
		accountStore = store.NewMemoryStore()
	default:
		return nil, err
	}

	return accountStore, err
}

func main() {
	// Parse command line flags
	configPath := flag.String("config", "", "Path to configuration file")
	queueType := flag.String("queue", "memory", "Queue type: memory, redis")
	redisURL := flag.String("redis", "redis://localhost:6379", "Redis URL")
	storeType := flag.String("store", "sql", "Store type: sql, memory")
	storeDriver := flag.String("store-driver", "sqlite", "SQL driver: sqlite, postgres")
	storeDSN := flag.String("store-dsn", "./data/webmail.db", "SQL DSN")
	concurrency := flag.Int("concurrency", 4, "Number of processor workers")
	flag.Parse()

	// Load or create configuration
	var cfg *workerconfig.WorkerConfig
	var err error

	if *configPath != "" {
		cfg, err = workerconfig.LoadWorkerConfig(*configPath)
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}
	} else {
		cfg = workerconfig.DefaultWorkerConfig("processor")
		cfg.Queue.Type = *queueType
		cfg.Queue.RedisURL = *redisURL
		cfg.Store.Type = *storeType
		cfg.Store.SQL = &config.SQLConfig{
			Driver: *storeDriver,
			DSN:    *storeDSN,
		}
		cfg.ProcessorConfig = &service.EnvelopeProcessorConfig{
			Concurrency:     *concurrency,
			BatchSize:       20,
			PollInterval:    5 * time.Second,
			CleanupInterval: 1 * time.Hour,
			CleanupAge:      24 * time.Hour,
			TempStoragePath: "data/attachments",
		}
		config.ExpandEnvVars(cfg)
	}

	// Create and run processor worker with taskmaster
	processorWorker, err := NewProcessorWorkerWithTaskmaster(cfg)
	if err != nil {
		log.Fatalf("Failed to create processor worker: %v", err)
	}

	if err := processorWorker.Start(); err != nil {
		log.Fatalf("Failed to start processor worker: %v", err)
	}

	if err := processorWorker.Run(); err != nil {
		log.Fatalf("Processor worker error: %v", err)
	}
}
