package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"

	"webmail_engine/internal/api"
	"webmail_engine/internal/cache"
	"webmail_engine/internal/config"
	"webmail_engine/internal/models"
	"webmail_engine/internal/pool"
	"webmail_engine/internal/scheduler"
	"webmail_engine/internal/service"
	"webmail_engine/internal/storage"
	"webmail_engine/internal/store"
	"webmail_engine/internal/webhook"

	"github.com/gin-gonic/gin"
	"github.com/olivere/vite"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "", "Path to configuration file")
	isDev := flag.Bool("dev", false, "Run in development mode (with Vite dev server)")
	flag.Parse()

	// Load configuration
	var cfg *config.Config
	var err error

	if *configPath != "" {
		cfg, err = config.LoadFromFile(*configPath)
		if err != nil {
			log.Fatalf("Failed to load config file: %v", err)
		}
	} else {
		cfg = config.LoadFromEnv()
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	// Initialize components
	log.Println("Initializing webmail engine (API server)...")

	// Initialize cache backend
	var cacheClient cache.RedisClient
	var cacheBackend string

	redisClient, err := cache.NewRedisClient(cache.RedisConfig{
		Host:     cfg.Redis.Host,
		Port:     cfg.Redis.Port,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
		PoolSize: cfg.Redis.PoolSize,
	})
	if err != nil {
		// Fallback to in-memory cache
		log.Printf("Warning: Redis not available: %v", err)
		memClient := cache.NewMemoryClient(cache.DefaultMemoryClientConfig())
		cacheClient = memClient
		cacheBackend = fmt.Sprintf("In-memory (maxKeys=%d)", cache.DefaultMemoryClientConfig().MaxSize)
		log.Printf("Cache backend: %s", cacheBackend)
	} else {
		cacheClient = redisClient
		cacheBackend = fmt.Sprintf("Redis at %s:%d, DB: %d, PoolSize: %d",
			cfg.Redis.Host, cfg.Redis.Port, cfg.Redis.DB, cfg.Redis.PoolSize)
		log.Printf("Cache backend: %s", cacheBackend)
	}

	memCache := cache.NewCache(cacheClient)

	// Initialize connection pool for IMAP/SMTP
	connPool := pool.NewConnectionPool(pool.PoolConfig{
		MaxConnections: cfg.Pool.MaxConnections,
		IdleTimeout:    cfg.Pool.IdleTimeout,
		DialTimeout:    cfg.Pool.DialTimeout,
	})
	defer connPool.Close("all")

	// Initialize IMAP session pool

	// Start pool cleanup
	poolCtx, poolCancel := context.WithCancel(context.Background())
	defer poolCancel()
	go connPool.StartCleanup(poolCtx, cfg.Pool.CleanupInterval)

	// Initialize account store
	log.Printf("Initializing account store (type=%s)...", cfg.Store.Type)
	var accountStore store.AccountStore
	switch cfg.Store.Type {
	case "sqlite", "":
		accountStore, err = store.NewSQLiteStore(store.SQLiteConfig{
			Path:           cfg.Store.SQLite.Path,
			MaxConnections: cfg.Store.SQLite.MaxConnections,
			BusyTimeoutMs:  cfg.Store.SQLite.BusyTimeoutMs,
		})
	case "memory":
		accountStore = store.NewMemoryStore()
		log.Println("Memory store initialized (data will not persist)")

	default:
		log.Fatalf("Unknown store type: %s", cfg.Store.Type)
	}
	if err != nil {
		log.Fatalf("Failed to initialize account store: %v", err)
	}
	defer accountStore.Close()

	// Initialize fair-use scheduler
	fairUseScheduler := scheduler.NewFairUseScheduler()

	// Initialize IMAP Session Pool (with nil account service initially)
	// Will set account service after accountService is created
	imapSessionPool := pool.NewIMAPSessionPool(pool.DefaultSessionPoolConfig(), nil)
	go imapSessionPool.StartMaintenance(poolCtx)

	// Initialize services
	accountService, err := service.NewAccountService(
		accountStore,
		connPool,
		imapSessionPool,
		memCache,
		fairUseScheduler,
		nil, // syncMgr - nil since sync runs externally
		service.AccountServiceConfig{
			EncryptionKey: cfg.Security.EncryptionKey,
		},
	)
	if err != nil {
		log.Fatalf("Failed to create account service: %v", err)
	}

	// Set the account service in the session pool (circular dependency resolution)
	imapSessionPool.SetAccountService(accountService)

	// Initialize accounts from store
	if cfg.Scheduler.Enabled {
		log.Println("Loading accounts from store...")
		accounts, err := accountService.ListAccounts(context.Background())
		if err != nil {
			log.Printf("Warning: Failed to load accounts from store: %v", err)
		} else {
			log.Printf("Loaded %d accounts from store", len(accounts))

			// Restore active connections for each account
			for _, acc := range accounts {
				if acc.Status == models.AccountStatusActive {
					log.Printf("Restoring account %s (%s)", acc.ID, acc.Email)
					// Reinitialize fair-use scheduling
					fairUseScheduler.InitializeAccount(acc.ID, acc.SyncSettings.FairUsePolicy)
				}
			}
		}
	}

	messageService, err := service.NewMessageService(
		imapSessionPool,
		memCache,
		fairUseScheduler,
		accountService,
		service.MessageServiceConfig{
			TempStoragePath: cfg.Storage.AttachmentPath,
			MaxInlineSize:   cfg.Security.MaxAttachmentSize,
			AllowBodySearch: cfg.IMAP.Search.AllowBodySearch,
		},
	)
	if err != nil {
		log.Fatalf("Failed to create message service: %v", err)
	}

	attachmentStorage := storage.NewFileAttachmentStorage(cfg.Storage.AttachmentPath)

	// Note: Sync and envelope processing run as separate workers:
	// - cmd/sync_worker: Fetches envelopes from IMAP and enqueues for processing
	// - cmd/processor_worker: Processes envelopes (fetches bodies, extracts data)
	// This decoupling allows independent scaling and deployment.

	sendService, err := service.NewSendService(
		connPool,
		fairUseScheduler,
		attachmentStorage,
		accountStore,
		cfg.Security.EncryptionKey,
		service.SendServiceConfig{
			MaxRetries:      cfg.Webhook.MaxRetries,
			SendTimeout:     cfg.Webhook.Timeout,
			TempStoragePath: cfg.Storage.TempPath,
		},
	)
	if err != nil {
		log.Fatalf("Failed to create send service: %v", err)
	}

	// Initialize webhook handler
	webhookHandler := webhook.NewWebhookHandler(webhook.WebhookHandlerConfig{
		SecretKey: cfg.Security.WebhookSecret,
	})

	// Register default event handlers
	webhookHandler.RegisterHandler(models.EventNewMessage, webhook.NewMessageHandler())
	webhookHandler.RegisterHandler(models.EventMessageDeleted, webhook.MessageDeletedHandler())
	webhookHandler.RegisterHandler(models.EventAuthError, webhook.AuthErrorHandler())

	// Start webhook cleanup
	webhookCtx, webhookCancel := context.WithCancel(context.Background())
	defer webhookCancel()
	go webhookHandler.StartCleanup(webhookCtx, cfg.Webhook.CleanupInterval, cfg.Webhook.EventRetention)

	// Initialize API handler
	apiHandler := api.NewAPIHandler(accountService, messageService, sendService, accountStore, memCache, attachmentStorage)

	// Create Gin router
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	// Register API routes
	apiHandler.RegisterRoutes(router)

	// Register webhook route
	router.POST("/v1/webhooks", func(c *gin.Context) {
		webhookHandler.HandleWebhook(c.Writer, c.Request)
	})

	// Health check endpoint
	router.GET("/health", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})

	// Start HTTP server
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	// Initialize Vite handler for frontend
	var viteHandler *vite.Handler
	if *isDev {
		log.Println("Starting in development mode with Vite dev server")
		viteHandler, err = vite.NewHandler(vite.Config{
			FS:        os.DirFS("./frontend"),
			IsDev:     true,
			ViteURL:   "http://localhost:5173",
			ViteEntry: "src/main.tsx",
		})
		if err != nil {
			log.Fatalf("Failed to initialize Vite handler: %v", err)
		}
	} else {
		log.Println("Starting in production mode with compiled frontend assets")
		frontendPath := path.Join("..", "frontend", "dist")
		router.Use(staticMiddleware(frontendPath))

	}

	// Register Vite handler for frontend routes (SPA support)
	router.NoRoute(func(c *gin.Context) {
		reqPath := c.Request.URL.Path

		// Skip API routes
		if strings.HasPrefix(reqPath, "/v1/") || reqPath == "/health" {
			c.Status(http.StatusNotFound)
			return
		}

		// SPA Fallback: If the path does not have an extension, rewrite it to "/"
		// so Vite can serve the index.html template and React Router can take over.
		if path.Ext(reqPath) == "" {
			c.Request.URL.Path = "/"
			c.Status(http.StatusOK)
		}

		// Let Vite handler serve the request (handles both dev proxy and prod embedded assets)
		if viteHandler != nil {
			viteHandler.ServeHTTP(c.Writer, c.Request)
		} else {
			c.Status(http.StatusNotFound)
		}
	})

	// Create HTTP server
	server := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// Start server in goroutine
	serverErr := make(chan error, 1)
	go func() {
		log.Printf("Starting API server on %s", addr)
		if cfg.Server.TLSEnabled {
			err := server.ListenAndServeTLS(cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile)
			if err != nil && err != http.ErrServerClosed {
				serverErr <- err
			}
		} else {
			err := server.ListenAndServe()
			if err != nil && err != http.ErrServerClosed {
				serverErr <- err
			}
		}
	}()

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-quit:
		log.Println("Shutting down server...")
	case err := <-serverErr:
		log.Fatalf("Server error: %v", err)
	}

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	// Cleanup
	poolCancel()
	webhookCancel()
	if accountStore != nil {
		accountStore.Close()
	}
	attachmentStorage.Shutdown()
	if cacheClient != nil {
		cacheClient.Close()
	}

	log.Println("Webmail engine stopped")
}

// staticMiddleware serves static files from a directory
func staticMiddleware(root string) gin.HandlerFunc {
	fs := http.FileServer(http.Dir(root))
	return func(c *gin.Context) {
		// Check if file exists, otherwise serve index.html for SPA routing
		path := c.Request.URL.Path
		if _, err := os.Stat(root + path); os.IsNotExist(err) {
			c.Request.URL.Path = "/"
		}
		fs.ServeHTTP(c.Writer, c.Request)
	}
}
