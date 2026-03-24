// Package workers contains domain-specific task implementations for the webmail engine.
// Each task implements the taskmaster.Task interface and contains business logic.
package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"webmail_engine/internal/taskmaster"
)

// AIAnalysisTask performs AI-powered email analysis.
// This includes sentiment analysis, categorization, and smart labeling.
type AIAnalysisTask struct {
	// AnalysisService is the AI analysis service (injected dependency)
	AnalysisService AIAnalysisService
}

// AIAnalysisService defines the interface for AI analysis operations.
type AIAnalysisService interface {
	AnalyzeEmail(ctx context.Context, accountID, messageID string) (*AnalysisResult, error)
	CategorizeEmail(ctx context.Context, accountID, subject, body string) (string, error)
	ExtractEntities(ctx context.Context, content string) ([]Entity, error)
}

// AnalysisResult holds the result of an AI analysis.
type AnalysisResult struct {
	Sentiment    string   `json:"sentiment"`
	Category     string   `json:"category"`
	Confidence   float64  `json:"confidence"`
	Entities     []Entity `json:"entities"`
	ProcessedAt  time.Time `json:"processed_at"`
}

// Entity represents an extracted entity from email content.
type Entity struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// AIAnalysisPayload is the payload for AI analysis tasks.
type AIAnalysisPayload struct {
	AccountID string `json:"account_id"`
	MessageID string `json:"message_id"`
	Subject   string `json:"subject,omitempty"`
	Body      string `json:"body,omitempty"`
	Options   AnalysisOptions `json:"options,omitempty"`
}

// AnalysisOptions configures the analysis behavior.
type AnalysisOptions struct {
	IncludeSentiment  bool `json:"include_sentiment"`
	IncludeCategory   bool `json:"include_category"`
	IncludeEntities   bool `json:"include_entities"`
	TimeoutSeconds    int  `json:"timeout_seconds"`
}

// ID returns the unique task identifier.
func (t *AIAnalysisTask) ID() string {
	return "ai_analysis"
}

// Execute performs AI analysis on an email.
func (t *AIAnalysisTask) Execute(ctx context.Context, payload []byte) error {
	// Parse payload
	var req AIAnalysisPayload
	if err := json.Unmarshal(payload, &req); err != nil {
		return taskmaster.NewNonRetryableTaskError(t.ID(), "invalid payload format", err)
	}

	// Validate required fields
	if req.AccountID == "" {
		return taskmaster.NewNonRetryableTaskError(t.ID(), "account_id is required", nil)
	}
	if req.MessageID == "" {
		return taskmaster.NewNonRetryableTaskError(t.ID(), "message_id is required", nil)
	}

	// Check if service is available
	if t.AnalysisService == nil {
		return taskmaster.NewSystemTaskError(t.ID(), "AI analysis service not configured", nil)
	}

	// Apply timeout if specified
	execCtx := ctx
	if req.Options.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, time.Duration(req.Options.TimeoutSeconds)*time.Second)
		defer cancel()
	}

	// Perform analysis
	result, err := t.AnalysisService.AnalyzeEmail(execCtx, req.AccountID, req.MessageID)
	if err != nil {
		return taskmaster.WrapError(t.ID(), "failed to analyze email", err)
	}

	// Log success
	fmt.Printf("AI analysis completed for message %s: sentiment=%s, category=%s\n",
		req.MessageID, result.Sentiment, result.Category)

	return nil
}

// Ensure interface compliance
var _ taskmaster.Task = (*AIAnalysisTask)(nil)
