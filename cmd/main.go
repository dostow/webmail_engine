package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"webmail_engine"

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
	log.Println("Initializing webmail engine...")

	// Initialize Redis cache
	redisClient, err := cache.NewRedisClient(cache.RedisConfig{
		Host:     cfg.Redis.Host,
		Port:     cfg.Redis.Port,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
		PoolSize: cfg.Redis.PoolSize,
	})
	if err != nil {
		log.Printf("Warning: Redis not available, using in-memory cache: %v", err)
		// In production, you might want to use an in-memory fallback
		// For now, we'll continue without cache
	}

	var memCache *cache.Cache
	if redisClient != nil {
		memCache = cache.NewCache(redisClient)
	}

	// Initialize connection pool
	connPool := pool.NewConnectionPool(pool.PoolConfig{
		MaxConnections: cfg.Pool.MaxConnections,
		IdleTimeout:    cfg.Pool.IdleTimeout,
		DialTimeout:    cfg.Pool.DialTimeout,
	})

	// Start pool cleanup
	poolCtx, poolCancel := context.WithCancel(context.Background())
	defer poolCancel()
	go connPool.StartCleanup(poolCtx, cfg.Pool.CleanupInterval)

	// Initialize account store
	log.Printf("Initializing account store (type=%s)...", cfg.Store.Type)
	var accountStore store.AccountStore

	switch cfg.Store.Type {
	case "sqlite", "":
		sqliteConfig := store.SQLiteConfig{
			Path:           cfg.Store.SQLite.Path,
			MaxConnections: cfg.Store.SQLite.MaxConnections,
			BusyTimeoutMs:  cfg.Store.SQLite.BusyTimeoutMs,
		}
		accountStore, err = store.NewSQLiteStore(sqliteConfig)
		if err != nil {
			log.Fatalf("Failed to initialize SQLite store: %v", err)
		}
		log.Printf("SQLite store initialized at %s", cfg.Store.SQLite.Path)

	case "memory":
		accountStore = store.NewMemoryStore()
		log.Println("Memory store initialized (data will not persist)")

	default:
		log.Fatalf("Unknown store type: %s", cfg.Store.Type)
	}

	// Initialize fair-use scheduler
	fairUseScheduler := scheduler.NewFairUseScheduler()

	// Initialize services
	accountService, err := service.NewAccountService(
		accountStore,
		connPool,
		memCache,
		fairUseScheduler,
		nil, // syncMgr will be set later
		service.AccountServiceConfig{
			EncryptionKey: cfg.Security.EncryptionKey,
		},
	)
	if err != nil {
		log.Fatalf("Failed to create account service: %v", err)
	}

	// Load existing accounts from store on startup
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

	messageService, err := service.NewMessageService(
		connPool,
		memCache,
		fairUseScheduler,
		accountService,
		service.MessageServiceConfig{
			TempStoragePath: cfg.Storage.AttachmentPath,
			MaxInlineSize:   cfg.Security.MaxAttachmentSize,
		},
	)
	if err != nil {
		log.Fatalf("Failed to create message service: %v", err)
	}

	attachmentStorage := storage.NewAttachmentStorage(cfg.Storage.AttachmentPath)

	// Initialize sync manager with services
	syncMgr := service.NewSyncManager(messageService, accountService)
	// Update account service with sync manager
	accountService.SetSyncManager(syncMgr)

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
	apiHandler := api.NewAPIHandler(accountService, messageService, sendService)

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
		log.Println("Starting in production mode with embedded frontend assets")
		viteHandler, err = vite.NewHandler(vite.Config{
			FS:    webmail_engine.GetDistFS(),
			IsDev: false,
		})
		if err != nil {
			log.Fatalf("Failed to initialize Vite handler: %v", err)
		}
	}

	// Register Vite handler for frontend routes (SPA support)
	router.NoRoute(func(c *gin.Context) {
		// Skip API routes
		if strings.HasPrefix(c.Request.URL.Path, "/v1/") || c.Request.URL.Path == "/health" {
			c.Status(http.StatusNotFound)
			return
		}

		// For dev mode, proxy to Vite dev server
		if *isDev {
			viteHandler.ServeHTTP(c.Writer, c.Request)
			return
		}

		// For production mode, serve index.html for SPA routing
		c.File("./frontend/dist/index.html")
	})

	// Create HTTP server
	server := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// Start server in goroutine
	serverErr := make(chan error, 1)
	go func() {
		log.Printf("Starting server on %s:%d", cfg.Server.Host, cfg.Server.Port)
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
	if syncMgr != nil {
		syncMgr.StopAll()
	}
	if accountStore != nil {
		accountStore.Close()
	}
	attachmentStorage.Shutdown()
	if redisClient != nil {
		redisClient.Close()
	}

	log.Println("Webmail engine stopped")
}
