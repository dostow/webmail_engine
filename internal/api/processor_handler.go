package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"webmail_engine/internal/models"
	"webmail_engine/internal/processor"
	"webmail_engine/internal/service"
	"webmail_engine/internal/store"

	"github.com/gin-gonic/gin"
)

// ProcessorHandler handles processor configuration API requests
type ProcessorHandler struct {
	accountService *service.AccountService
	store          store.AccountStore
}

// NewProcessorHandler creates a new processor handler
func NewProcessorHandler(accountService *service.AccountService, store store.AccountStore) *ProcessorHandler {
	return &ProcessorHandler{
		accountService: accountService,
		store:          store,
	}
}

// RegisterRoutes registers processor API routes
func (h *ProcessorHandler) RegisterRoutes(router *gin.RouterGroup) {
	processorGroup := router.Group("/processors")
	{
		// Get available processor types
		processorGroup.GET("/types", h.listProcessorTypes)

		// Get account processor configs
		processorGroup.GET("/accounts/:account_id", h.getAccountProcessors)

		// Update account processor configs
		processorGroup.PUT("/accounts/:account_id", h.updateAccountProcessors)

		// Enable/disable specific processor
		processorGroup.PATCH("/accounts/:account_id/:processor_type", h.toggleProcessor)

		// Get processor type info
		processorGroup.GET("/types/:processor_type", h.getProcessorTypeInfo)

		// Create/link processor to account
		processorGroup.POST("/accounts/:account_id/processors", h.createAccountProcessor)

		// Remove processor from account
		processorGroup.DELETE("/accounts/:account_id/processors/:processor_type", h.deleteAccountProcessor)
	}
}

// ListProcessorTypesResponse represents the response for listing processor types
type ListProcessorTypesResponse struct {
	Types []string `json:"types"`
}

// listProcessorTypes returns all registered processor types
func (h *ProcessorHandler) listProcessorTypes(c *gin.Context) {
	types := processor.GlobalRegistry().ListRegisteredTypes()

	c.JSON(http.StatusOK, ListProcessorTypesResponse{
		Types: types,
	})
}

// AccountProcessorConfigRequest represents a processor config in request
type AccountProcessorConfigRequest struct {
	Type     string                 `json:"type" binding:"required"`
	Meta     map[string]interface{} `json:"meta"`
	Enabled  bool                   `json:"enabled"`
	Priority int                    `json:"priority"`
}

// GetAccountProcessorsResponse represents account processor configs
type GetAccountProcessorsResponse struct {
	AccountID string                          `json:"account_id"`
	Configs   []AccountProcessorConfigRequest `json:"configs"`
}

// getAccountProcessors retrieves processor configs for an account
func (h *ProcessorHandler) getAccountProcessors(c *gin.Context) {
	accountID := c.Param("account_id")

	configs, err := h.store.GetAccountProcessorConfigs(c.Request.Context(), accountID)
	if err != nil {
		if store.IsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Convert to response format
	responseConfigs := make([]AccountProcessorConfigRequest, len(configs))
	for i, cfg := range configs {
		responseConfigs[i] = AccountProcessorConfigRequest{
			Type:     cfg.Type,
			Meta:     metaToMap(processor.ProcessorMeta(cfg.Meta)),
			Enabled:  cfg.Enabled,
			Priority: cfg.Priority,
		}
	}

	c.JSON(http.StatusOK, GetAccountProcessorsResponse{
		AccountID: accountID,
		Configs:   responseConfigs,
	})
}

// UpdateAccountProcessorsRequest represents the request to update processor configs
type UpdateAccountProcessorsRequest struct {
	Configs []AccountProcessorConfigRequest `json:"configs" binding:"required"`
}

// updateAccountProcessors updates processor configs for an account
func (h *ProcessorHandler) updateAccountProcessors(c *gin.Context) {
	accountID := c.Param("account_id")

	var req UpdateAccountProcessorsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate processor types are registered
	registry := processor.GlobalRegistry()
	for _, cfg := range req.Configs {
		if !registry.IsRegistered(cfg.Type) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf("unknown processor type: %s", cfg.Type),
			})
			return
		}
	}

	// Convert to model format
	configs := make([]models.AccountProcessorConfig, len(req.Configs))
	for i, cfg := range req.Configs {
		metaJSON, _ := json.Marshal(cfg.Meta)
		configs[i] = models.AccountProcessorConfig{
			Type:     cfg.Type,
			Meta:     json.RawMessage(metaJSON),
			Enabled:  cfg.Enabled,
			Priority: cfg.Priority,
		}
	}

	// Update in database
	err := h.store.UpdateAccountProcessorConfigs(c.Request.Context(), accountID, configs)
	if err != nil {
		if store.IsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}

// ToggleProcessorRequest represents the request to toggle a processor
type ToggleProcessorRequest struct {
	Enabled bool `json:"enabled"`
}

// toggleProcessor enables or disables a specific processor for an account
func (h *ProcessorHandler) toggleProcessor(c *gin.Context) {
	accountID := c.Param("account_id")
	processorType := c.Param("processor_type")

	var req ToggleProcessorRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err := h.store.EnableAccountProcessor(
		c.Request.Context(),
		accountID,
		processorType,
		req.Enabled,
	)
	if err != nil {
		if store.IsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}

// CreateAccountProcessorRequest represents a request to link a processor to an account
type CreateAccountProcessorRequest struct {
	Type     string                 `json:"type" binding:"required"`
	Meta     map[string]interface{} `json:"meta"`
	Enabled  bool                   `json:"enabled"`
	Priority int                    `json:"priority"`
}

// createAccountProcessor links a processor to an account
func (h *ProcessorHandler) createAccountProcessor(c *gin.Context) {
	accountID := c.Param("account_id")

	var req CreateAccountProcessorRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate processor type
	registry := processor.GlobalRegistry()
	if !registry.IsRegistered(req.Type) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("unknown processor type: %s", req.Type),
		})
		return
	}

	// Get existing configs
	existingConfigs, err := h.store.GetAccountProcessorConfigs(c.Request.Context(), accountID)
	if err != nil && !store.IsNotFound(err) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Marshal meta
	metaJSON, _ := json.Marshal(req.Meta)

	// Check if processor already exists
	exists := false
	for i, cfg := range existingConfigs {
		if cfg.Type == req.Type {
			// Update existing
			existingConfigs[i].Meta = json.RawMessage(metaJSON)
			existingConfigs[i].Enabled = req.Enabled
			existingConfigs[i].Priority = req.Priority
			exists = true
			break
		}
	}

	if !exists {
		// Add new
		existingConfigs = append(existingConfigs, models.AccountProcessorConfig{
			Type:     req.Type,
			Meta:     json.RawMessage(metaJSON),
			Enabled:  req.Enabled,
			Priority: req.Priority,
		})
	}

	// Save updated configs
	err = h.store.UpdateAccountProcessorConfigs(c.Request.Context(), accountID, existingConfigs)
	if err != nil {
		if store.IsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "created", "type": req.Type})
}

// deleteAccountProcessor removes a processor from an account
func (h *ProcessorHandler) deleteAccountProcessor(c *gin.Context) {
	accountID := c.Param("account_id")
	processorType := c.Param("processor_type")

	configs, err := h.store.GetAccountProcessorConfigs(c.Request.Context(), accountID)
	if err != nil {
		if store.IsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Filter out the processor to delete
	filtered := make([]models.AccountProcessorConfig, 0, len(configs))
	for _, cfg := range configs {
		if cfg.Type != processorType {
			filtered = append(filtered, cfg)
		}
	}

	err = h.store.UpdateAccountProcessorConfigs(c.Request.Context(), accountID, filtered)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "deleted", "type": processorType})
}

// ProcessorTypeInfo represents information about a processor type
type ProcessorTypeInfo struct {
	Type           string                 `json:"type"`
	Description    string                 `json:"description"`
	MetaSchema     map[string]interface{} `json:"meta_schema"` // JSON schema for meta configuration
	DefaultConfig  map[string]interface{} `json:"default_config,omitempty"`
	RequiresAPIKey bool                   `json:"requires_api_key"`
}

// getProcessorTypeInfo returns information about a processor type
func (h *ProcessorHandler) getProcessorTypeInfo(c *gin.Context) {
	processorType := c.Param("processor_type")

	registry := processor.GlobalRegistry()
	if !registry.IsRegistered(processorType) {
		c.JSON(http.StatusNotFound, gin.H{"error": "processor type not found"})
		return
	}

	info, err := registry.GetProcessorInfo(processorType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Return type info with description and schema
	c.JSON(http.StatusOK, ProcessorTypeInfo{
		Type:           processorType,
		Description:    info.Description,
		MetaSchema:     getMetaSchemaForProcessor(processorType).(map[string]interface{}),
		DefaultConfig:  getDefaultConfigForProcessor(processorType),
		RequiresAPIKey: processorType == "llm_processor", // LLM processor requires API key
	})
}

// Helper functions
func metaToMap(meta processor.ProcessorMeta) map[string]interface{} {
	var result map[string]interface{}
	if err := json.Unmarshal(meta, &result); err != nil {
		return make(map[string]interface{})
	}
	return result
}

func getMetaSchemaForProcessor(processorType string) interface{} {
	schemas := map[string]interface{}{
		"link_tracker": map[string]interface{}{
			"type":     "object",
			"required": []string{"base_url", "salt"},
			"properties": map[string]interface{}{
				"base_url":        map[string]interface{}{"type": "string", "description": "Base URL for tracking links"},
				"salt":            map[string]interface{}{"type": "string", "description": "Salt for HMAC signing"},
				"track_only_html": map[string]interface{}{"type": "boolean", "default": false},
				"ignore_domains":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			},
		},
		"message_summarizer": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"max_length":        map[string]interface{}{"type": "integer", "default": 100},
				"provider":          map[string]interface{}{"type": "string", "default": "local"},
				"model":             map[string]interface{}{"type": "string"},
				"min_body_length":   map[string]interface{}{"type": "integer", "default": 50},
				"skip_replies":      map[string]interface{}{"type": "boolean", "default": false},
				"skip_short_emails": map[string]interface{}{"type": "boolean", "default": false},
			},
		},
		"llm_processor": map[string]interface{}{
			"type":     "object",
			"required": []string{"system_prompt", "user_prompt"},
			"properties": map[string]interface{}{
				"system_prompt": map[string]interface{}{"type": "string"},
				"user_prompt":   map[string]interface{}{"type": "string"},
				"provider":      map[string]interface{}{"type": "string", "default": "openai"},
				"model":         map[string]interface{}{"type": "string"},
				"api_key":       map[string]interface{}{"type": "string"},
				"temperature":   map[string]interface{}{"type": "number", "default": 0.7},
				"max_tokens":    map[string]interface{}{"type": "integer", "default": 1024},
			},
		},
		"spam_filter": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"threshold":     map[string]interface{}{"type": "number", "default": 0.8},
				"action":        map[string]interface{}{"type": "string", "default": "tag"},
				"spam_folder":   map[string]interface{}{"type": "string", "default": "Spam"},
				"keywords":      map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
				"whitelist":     map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
				"blacklist":     map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
				"abort_on_spam": map[string]interface{}{"type": "boolean", "default": false},
			},
		},
	}
	return schemas[processorType]
}

// getDefaultConfigForProcessor returns default configuration for a processor type
func getDefaultConfigForProcessor(processorType string) map[string]interface{} {
	configs := map[string]interface{}{
		"link_tracker": map[string]interface{}{
			"base_url":        "",
			"salt":            "",
			"track_only_html": false,
			"ignore_domains":  []string{},
		},
		"message_summarizer": map[string]interface{}{
			"max_length":        100,
			"provider":          "local",
			"model":             "",
			"min_body_length":   50,
			"skip_replies":      false,
			"skip_short_emails": false,
		},
		"llm_processor": map[string]interface{}{
			"system_prompt": "You are a helpful email assistant.",
			"user_prompt":   "Analyze this email: {{body}}",
			"provider":      "openai",
			"model":         "gpt-3.5-turbo",
			"api_key":       "",
			"temperature":   0.7,
			"max_tokens":    1024,
		},
		"spam_filter": map[string]interface{}{
			"threshold":     0.8,
			"action":        "tag",
			"spam_folder":   "Spam",
			"keywords":      []string{},
			"whitelist":     []string{},
			"blacklist":     []string{},
			"abort_on_spam": false,
		},
	}
	if config, ok := configs[processorType]; ok {
		return config.(map[string]interface{})
	}
	return make(map[string]interface{})
}
