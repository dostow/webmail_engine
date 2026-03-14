package api

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"webmail_engine/internal/models"
	"webmail_engine/internal/service"

	"github.com/gin-gonic/gin"
)

// APIHandler handles HTTP API requests
type APIHandler struct {
	accountService *service.AccountService
	messageService *service.MessageService
	sendService    *service.SendService
}

// NewAPIHandler creates a new API handler
func NewAPIHandler(
	accountService *service.AccountService,
	messageService *service.MessageService,
	sendService *service.SendService,
) *APIHandler {
	return &APIHandler{
		accountService: accountService,
		messageService: messageService,
		sendService:    sendService,
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

	// Message routes
	router.GET("/v1/accounts/:id/messages", h.getMessages)
	router.GET("/v1/accounts/:id/messages/:uid", h.getMessage)
	router.GET("/v1/accounts/:id/search", h.searchMessages)
	router.POST("/v1/accounts/:id/search", h.searchMessages)

	// Send routes
	router.POST("/v1/accounts/:id/send", h.sendMessage)

	// Health routes
	router.GET("/v1/health", h.getSystemHealth)
	router.GET("/v1/health/accounts/:id", h.getAccountStatus)
	router.GET("/v1/accounts/:id/stats", h.getAccountStats)
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

	messageList, err := h.messageService.GetMessageList(c.Request.Context(), accountID, folder, limit, cursor)
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
				Status: "healthy",
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
