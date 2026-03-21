package api

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"webmail_engine/internal/cache"
	"webmail_engine/internal/models"
	"webmail_engine/internal/service"
	"webmail_engine/internal/storage"
	"webmail_engine/internal/store"

	"github.com/gin-gonic/gin"
)

// APIHandler handles HTTP API requests
type APIHandler struct {
	accountService *service.AccountService
	messageService *service.MessageService
	sendService    *service.SendService
	store          store.AccountStore
	cache          *cache.Cache
	storage        storage.AttachmentStorage
}

// accountStatusMiddleware checks if an account is disabled and returns an error if so
// This middleware should be used for routes that access account resources
func (h *APIHandler) accountStatusMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID := c.Param("id")
		if accountID == "" {
			c.Next()
			return
		}

		// Get account to check status
		account, err := h.accountService.GetAccount(c.Request.Context(), accountID)
		if err != nil {
			respondError(c, err)
			c.Abort()
			return
		}

		// Check if account is disabled
		if account.Status == models.AccountStatusDisabled {
			respondError(c, models.NewAuthError("Account is disabled due to authentication failure. Please update your email credentials."))
			c.Abort()
			return
		}

		// Check if account requires authentication
		if account.Status == models.AccountStatusAuthRequired {
			respondError(c, models.NewAuthError("Account requires re-authentication. Please update your credentials."))
			c.Abort()
			return
		}

		c.Next()
	}
}

// NewAPIHandler creates a new API handler
func NewAPIHandler(
	accountService *service.AccountService,
	messageService *service.MessageService,
	sendService *service.SendService,
	store store.AccountStore,
	cache *cache.Cache,
	storage storage.AttachmentStorage,
) *APIHandler {
	return &APIHandler{
		accountService: accountService,
		messageService: messageService,
		sendService:    sendService,
		store:          store,
		cache:          cache,
		storage:        storage,
	}
}

// RegisterRoutes registers all API routes
func (h *APIHandler) RegisterRoutes(router *gin.Engine) {
	// Account routes
	router.GET("/v1/accounts", h.listAccounts)
	router.POST("/v1/accounts", h.createAccount)
	router.GET("/v1/accounts/:id", h.getAccount)
	router.PUT("/v1/accounts/:id", h.updateAccount)
	router.DELETE("/v1/accounts/:id", h.deleteAccount)
	router.PATCH("/v1/accounts/:id/sync-settings", h.updateSyncSettings)
	router.GET("/v1/accounts/:id/capabilities", h.getServerCapabilities)
	router.POST("/v1/accounts/:id/capabilities/refresh", h.refreshServerCapabilities)
	router.GET("/v1/accounts/:id/folders", h.getAccountFolders)

	// Message routes (with account status middleware)
	accountRoutes := router.Group("/v1/accounts/:id")
	accountRoutes.Use(h.accountStatusMiddleware())
	{
		accountRoutes.GET("/messages", h.getMessages)
		accountRoutes.GET("/messages/:uid", h.getMessage)
		accountRoutes.GET("/search", h.searchMessages)
		accountRoutes.POST("/search", h.searchMessages)
		accountRoutes.POST("/send", h.sendMessage)
		accountRoutes.POST("/messages/:uid/mark-read", h.markMessageRead)
		accountRoutes.POST("/messages/mark-read", h.markMessagesRead)
		accountRoutes.GET("/folders/live", h.listFolders)
		accountRoutes.GET("/folders/tree", h.getFolderTree)
		accountRoutes.GET("/messages/:uid/attachments/:attachmentId", h.downloadAttachment)
	}

	// Processor routes
	processorHandler := NewProcessorHandler(h.accountService, h.store)
	processorRoutes := router.Group("/v1")
	processorHandler.RegisterRoutes(processorRoutes)

	// Health routes (no middleware - should work even for disabled accounts)
	router.GET("/v1/health", h.getSystemHealth)
	router.GET("/v1/health/accounts/:id", h.getAccountStatus)
	router.GET("/v1/accounts/:id/stats", h.getAccountStats)
	router.GET("/v1/debug/pool-stats", h.getPoolStats)
}

// Account handlers

func (h *APIHandler) listAccounts(c *gin.Context) {
	accounts, err := h.accountService.ListAccounts(c.Request.Context())
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"accounts": accounts,
		"total":    len(accounts),
	})
}

func (h *APIHandler) createAccount(c *gin.Context) {
	var req models.AddAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("Failed to decode request: %v", err)
		respondError(c, models.NewValidationError("body", "Invalid request body"))
		return
	}

	// Validate required fields
	if req.Email == "" {
		respondError(c, models.NewValidationError("email", "Email is required"))
		return
	}
	if req.Password == "" && req.AccessToken == "" {
		respondError(c, models.NewValidationError("credentials", "Password or access token is required"))
		return
	}

	// Set defaults
	if req.ConnectionLimit == 0 {
		req.ConnectionLimit = 5
	}
	if req.IMAPPort == 0 {
		req.IMAPPort = 993
	}
	if req.SMTPPort == 0 {
		req.SMTPPort = 587
	}

	log.Printf("Creating account for email: %s, IMAP: %s:%d, SMTP: %s:%d",
		req.Email, req.IMAPHost, req.IMAPPort, req.SMTPHost, req.SMTPPort)

	response, err := h.accountService.AddAccount(c.Request.Context(), req)
	if err != nil {
		log.Printf("Failed to create account: %v", err)
		respondError(c, err)
		return
	}

	c.JSON(http.StatusCreated, response)
}

func (h *APIHandler) getAccount(c *gin.Context) {
	accountID := c.Param("id")
	if accountID == "" {
		respondError(c, models.NewValidationError("account_id", "Account ID is required"))
		return
	}

	account, err := h.accountService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, account)
}

func (h *APIHandler) updateAccount(c *gin.Context) {
	accountID := c.Param("id")
	if accountID == "" {
		respondError(c, models.NewValidationError("account_id", "Account ID is required"))
		return
	}

	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		respondError(c, models.NewValidationError("body", "Invalid request body"))
		return
	}

	account, err := h.accountService.UpdateAccount(c.Request.Context(), accountID, updates)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, account)
}

func (h *APIHandler) deleteAccount(c *gin.Context) {
	accountID := c.Param("id")
	if accountID == "" {
		respondError(c, models.NewValidationError("account_id", "Account ID is required"))
		return
	}

	if err := h.accountService.DeleteAccount(c.Request.Context(), accountID); err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusNoContent, nil)
}

// updateSyncSettings updates sync settings for an account
func (h *APIHandler) updateSyncSettings(c *gin.Context) {
	accountID := c.Param("id")
	if accountID == "" {
		respondError(c, models.NewValidationError("account_id", "Account ID is required"))
		return
	}

	var syncSettings models.SyncSettings
	if err := c.ShouldBindJSON(&syncSettings); err != nil {
		respondError(c, models.NewValidationError("body", "Invalid request body"))
		return
	}

	// Validate sync interval (minimum 60 seconds)
	if syncSettings.SyncInterval > 0 && syncSettings.SyncInterval < 60 {
		respondError(c, models.NewValidationError("sync_interval", "Sync interval must be at least 60 seconds (1 minute)"))
		return
	}

	account, err := h.accountService.UpdateAccount(c.Request.Context(), accountID, map[string]interface{}{
		"sync_settings": syncSettings,
	})
	if err != nil {
		respondError(c, err)
		return
	}

	log.Printf("Updated sync settings for account %s: auto_sync=%v, interval=%ds",
		accountID, syncSettings.AutoSync, syncSettings.SyncInterval)

	c.JSON(http.StatusOK, account)
}

// getServerCapabilities returns the IMAP server capabilities for an account
func (h *APIHandler) getServerCapabilities(c *gin.Context) {
	accountID := c.Param("id")
	if accountID == "" {
		respondError(c, models.NewValidationError("account_id", "Account ID is required"))
		return
	}

	capabilities, err := h.accountService.GetServerCapabilities(c.Request.Context(), accountID)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, capabilities)
}

// refreshServerCapabilities forces a fresh capability detection from the IMAP server
func (h *APIHandler) refreshServerCapabilities(c *gin.Context) {
	accountID := c.Param("id")
	if accountID == "" {
		respondError(c, models.NewValidationError("account_id", "Account ID is required"))
		return
	}

	capabilities, err := h.accountService.DetectServerCapabilities(c.Request.Context(), accountID)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"capabilities": capabilities,
		"refreshed":    true,
	})
}

// Message handlers

func (h *APIHandler) getMessages(c *gin.Context) {
	accountID := c.Param("id")
	if accountID == "" {
		respondError(c, models.NewValidationError("account_id", "Account ID is required"))
		return
	}

	// Parse query parameters
	folder := c.Query("folder")
	limit, _ := strconv.Atoi(c.Query("limit"))
	cursor := c.Query("cursor")
	sortBy := models.SortField(c.Query("sort_by"))
	sortOrder := models.SortOrder(c.Query("sort_order"))

	messageList, err := h.messageService.GetMessageList(c.Request.Context(), accountID, folder, limit, cursor, sortBy, sortOrder)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, messageList)
}

// getMessage retrieves a specific message with full content
func (h *APIHandler) getMessage(c *gin.Context) {
	accountID := c.Param("id")
	messageUID := c.Param("uid")
	folder := c.Query("folder")

	if accountID == "" {
		respondError(c, models.NewValidationError("account_id", "Account ID is required"))
		return
	}
	if messageUID == "" {
		respondError(c, models.NewValidationError("uid", "Message UID is required"))
		return
	}

	message, err := h.messageService.GetMessage(c.Request.Context(), accountID, messageUID, folder)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, message)
}

func (h *APIHandler) searchMessages(c *gin.Context) {
	accountID := c.Param("id")
	if accountID == "" {
		respondError(c, models.NewValidationError("account_id", "Account ID is required"))
		return
	}

	var query models.SearchQuery

	if c.Request.Method == http.MethodPost {
		if err := c.ShouldBindJSON(&query); err != nil {
			respondError(c, models.NewValidationError("body", "Invalid request body"))
			return
		}
	} else {
		// Parse from query params
		query.AccountID = accountID
		query.Keywords = splitStrings(c.Query("q"), " ")
		query.From = c.Query("from")
		query.To = c.Query("to")
		query.Subject = c.Query("subject")

		limit, _ := strconv.Atoi(c.Query("limit"))
		query.Limit = limit

		offset, _ := strconv.Atoi(c.Query("offset"))
		query.Offset = offset
	}

	result, err := h.messageService.SearchMessages(c.Request.Context(), query)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, result)
}

// Send handlers

func (h *APIHandler) sendMessage(c *gin.Context) {
	accountID := c.Param("id")
	if accountID == "" {
		respondError(c, models.NewValidationError("account_id", "Account ID is required"))
		return
	}

	var req models.SendEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, models.NewValidationError("body", "Invalid request body"))
		return
	}

	req.AccountID = accountID

	// Validate required fields
	if len(req.To) == 0 {
		respondError(c, models.NewValidationError("to", "At least one recipient is required"))
		return
	}
	if req.Subject == "" && req.TextBody == "" && req.HTMLBody == "" {
		respondError(c, models.NewValidationError("content", "Subject or body is required"))
		return
	}

	response, err := h.sendService.SendEmail(c.Request.Context(), req)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusCreated, response)
}

// Health handlers

func (h *APIHandler) getSystemHealth(c *gin.Context) {
	// In production, this would gather health from all components
	poolStats := h.messageService.GetPoolStats()
	health := &models.SystemHealthResponse{
		Status: "healthy",
		Score:  100,
		Components: map[string]models.ComponentHealth{
			"api": {
				Status: "healthy",
			},
			"cache": {
				Status: "healthy",
			},
			"pool": {
				Status:  "healthy",
				Details: poolStats,
			},
		},
	}

	c.JSON(http.StatusOK, health)
}

func (h *APIHandler) getAccountStatus(c *gin.Context) {
	accountID := c.Param("id")
	if accountID == "" {
		respondError(c, models.NewValidationError("account_id", "Account ID is required"))
		return
	}

	status, err := h.accountService.GetAccountStatus(c.Request.Context(), accountID)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, status)
}

// getAccountStats returns account statistics
func (h *APIHandler) getAccountStats(c *gin.Context) {
	accountID := c.Param("id")
	if accountID == "" {
		respondError(c, models.NewValidationError("account_id", "Account ID is required"))
		return
	}

	// Get account to verify it exists
	account, err := h.accountService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		respondError(c, err)
		return
	}

	// Determine connection status
	connectionStatus := "disconnected"
	if account.Status == "active" {
		connectionStatus = "connected"
	}

	// Return basic stats
	stats := gin.H{
		"account_id":        accountID,
		"email":             account.Email,
		"status":            account.Status,
		"connection_status": connectionStatus,
		"imap_host":         account.IMAPConfig.Host,
		"smtp_host":         account.SMTPConfig.Host,
	}

	c.JSON(http.StatusOK, stats)
}

// getPoolStats returns the current IMAP session pool statistics
func (h *APIHandler) getPoolStats(c *gin.Context) {
	stats := h.messageService.GetPoolStats()
	c.JSON(http.StatusOK, stats)
}

// Helper functions

func respondError(c *gin.Context, err error) {
	// Check if it's an APIError
	if apiErr, ok := err.(*models.APIError); ok {
		c.JSON(apiErr.StatusCode, gin.H{
			"error": gin.H{
				"code":    apiErr.Code,
				"message": apiErr.Message,
				"details": apiErr.Details,
			},
		})
		return
	}

	// Check for wrapped errors
	var apiErr *models.APIError

	// Handle common error types
	switch {
	case err == models.ErrAccountNotFound:
		apiErr = models.NewNotFoundError("Account", "unknown")
	case err == models.ErrAccountExists:
		apiErr = models.NewConflictError("Account", "unknown")
	case err == models.ErrAuthenticationFailed:
		apiErr = models.NewAuthError("Invalid credentials")
	case err == models.ErrMailServerAuthFailed:
		apiErr = models.NewAuthError("Invalid mail server credentials")
	case err == models.ErrPasswordDecryptionFailed:
		apiErr = models.NewAuthError("Unable to decrypt stored credentials - please re-authenticate")
	case err == models.ErrInsufficientTokens:
		apiErr = models.NewThrottleError(60)
	default:
		// Log the full error for debugging
		log.Printf("Unhandled error: %v", err)

		// Check if error message contains specific keywords
		errStr := err.Error()
		if strings.Contains(errStr, "already exists") || strings.Contains(errStr, "duplicate") {
			apiErr = models.NewConflictError("Resource", errStr)
		} else if strings.Contains(errStr, "authentication") || strings.Contains(errStr, "credentials") || strings.Contains(errStr, "disabled") {
			// Authentication-related errors
			apiErr = models.NewAuthError(errStr)
		} else if strings.Contains(errStr, "timeout") {
			apiErr = models.NewTimeoutError("Operation", 30)
		} else if strings.Contains(errStr, "connection") || strings.Contains(errStr, "unavailable") {
			apiErr = models.NewServiceUnavailableError("Service", errStr)
		} else {
			// Generic internal error
			apiErr = &models.APIError{
				Code:       "INTERNAL_ERROR",
				Message:    "Internal server error",
				StatusCode: 500,
			}
		}
	}

	c.JSON(apiErr.StatusCode, gin.H{
		"error": gin.H{
			"code":    apiErr.Code,
			"message": apiErr.Message,
			"details": apiErr.Details,
		},
	})
}

// FolderSyncInfo represents folder synchronization information
type FolderSyncInfo struct {
	Name          string    `json:"name"`
	Messages      int       `json:"messages"`
	Unseen        int       `json:"unseen"`
	LastSync      time.Time `json:"last_sync"`
	IsInitialized bool      `json:"is_initialized"`
	UIDValidity   uint32    `json:"uid_validity"`
}

// getAccountFolders returns folder sync information for an account
func (h *APIHandler) getAccountFolders(c *gin.Context) {
	accountID := c.Param("id")

	// Get folder sync states from store
	states, err := h.store.ListFolderSyncStates(c.Request.Context(), accountID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Convert to response format
	folders := make([]FolderSyncInfo, len(states))
	for i, state := range states {
		folders[i] = FolderSyncInfo{
			Name:          state.FolderName,
			Messages:      int(state.MessageCount),
			Unseen:        0, // Would need to fetch from cache or IMAP
			LastSync:      state.LastSyncTime,
			IsInitialized: state.IsInitialized,
			UIDValidity:   state.UIDValidity,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"account_id": accountID,
		"folders":    folders,
		"total":      len(folders),
	})
}

// listFolders returns live folder list from IMAP server
func (h *APIHandler) listFolders(c *gin.Context) {
	accountID := c.Param("id")
	if accountID == "" {
		respondError(c, models.NewValidationError("account_id", "Account ID is required"))
		return
	}

	folders, err := h.messageService.ListFolders(c.Request.Context(), accountID)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"account_id": accountID,
		"folders":    folders,
		"total":      len(folders),
	})
}

// getFolderTree returns folder hierarchy tree for an account
func (h *APIHandler) getFolderTree(c *gin.Context) {
	accountID := c.Param("id")
	if accountID == "" {
		respondError(c, models.NewValidationError("account_id", "Account ID is required"))
		return
	}

	tree, err := h.messageService.GetFolderTree(c.Request.Context(), accountID)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"account_id": accountID,
		"folders":    tree,
		"total":      len(tree),
	})
}

// markMessageRead marks a single message as read
func (h *APIHandler) markMessageRead(c *gin.Context) {
	accountID := c.Param("id")
	messageUID := c.Param("uid")
	folder := c.Query("folder")

	if accountID == "" {
		respondError(c, models.NewValidationError("account_id", "Account ID is required"))
		return
	}
	if messageUID == "" {
		respondError(c, models.NewValidationError("uid", "Message UID is required"))
		return
	}

	if folder == "" {
		folder = "INBOX"
	}

	err := h.messageService.MarkMessagesRead(c.Request.Context(), accountID, []string{messageUID}, folder)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"message": "Message marked as read",
	})
}

// markMessagesRead marks multiple messages as read
func (h *APIHandler) markMessagesRead(c *gin.Context) {
	accountID := c.Param("id")
	if accountID == "" {
		respondError(c, models.NewValidationError("account_id", "Account ID is required"))
		return
	}

	var req struct {
		UIDs   []string `json:"uids"`
		Folder string   `json:"folder"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, models.NewValidationError("body", "Invalid request body"))
		return
	}

	if len(req.UIDs) == 0 {
		respondError(c, models.NewValidationError("uids", "At least one message UID is required"))
		return
	}

	folder := req.Folder
	if folder == "" {
		folder = "INBOX"
	}

	err := h.messageService.MarkMessagesRead(c.Request.Context(), accountID, req.UIDs, folder)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"message": "Messages marked as read",
		"count":   len(req.UIDs),
	})
}

// downloadAttachment downloads an attachment file
func (h *APIHandler) downloadAttachment(c *gin.Context) {
	accountID := c.Param("id")
	messageUID := c.Param("uid")
	attachmentID := c.Param("attachmentId")
	folder := c.Query("folder")

	if accountID == "" {
		respondError(c, models.NewValidationError("account_id", "Account ID is required"))
		return
	}
	if messageUID == "" {
		respondError(c, models.NewValidationError("uid", "Message UID is required"))
		return
	}
	if attachmentID == "" {
		respondError(c, models.NewValidationError("attachment_id", "Attachment ID is required"))
		return
	}

	if folder == "" {
		folder = "INBOX"
	}

	// Get message from cache to retrieve attachment metadata
	msg, err := h.cache.GetMessage(c.Request.Context(), accountID, folder, messageUID)
	if err != nil {
		// Fallback: fetch from IMAP via message service
		msg, err = h.messageService.GetMessage(c.Request.Context(), accountID, messageUID, folder)
		if err != nil {
			respondError(c, models.ErrAttachmentNotFound)
			return
		}
	}

	// Find attachment in message
	var attachment *models.Attachment
	for i := range msg.Attachments {
		if msg.Attachments[i].ID == attachmentID {
			attachment = &msg.Attachments[i]
			break
		}
	}

	if attachment == nil {
		respondError(c, models.ErrAttachmentNotFound)
		return
	}

	// Get the actual attachment data from storage
	data, err := h.storage.Get(accountID, folder, messageUID, attachmentID)
	if err != nil {
		respondError(c, models.ErrAttachmentNotFound)
		return
	}

	// Set headers for file download
	c.Header("Content-Type", attachment.ContentType)
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", attachment.Filename))
	c.Header("Content-Length", fmt.Sprintf("%d", len(data)))

	// Send the file
	c.Data(http.StatusOK, attachment.ContentType, data)
}

// splitStrings splits a string by separator, filtering out empty strings
func splitStrings(s, sep string) []string {
	var result []string
	for _, part := range strings.Split(s, sep) {
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}
