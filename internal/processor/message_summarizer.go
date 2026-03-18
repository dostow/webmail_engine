package processor

import (
	"context"
	"fmt"
	"strings"
)

const MessageSummarizerType = "message_summarizer"

// MessageSummarizerConfig represents summarizer configuration
type MessageSummarizerConfig struct {
	MaxLength       int    `json:"max_length"`        // Maximum summary length
	Provider        string `json:"provider"`          // Summarization provider (e.g., "openai", "local")
	Model           string `json:"model"`             // Model name if using LLM
	MinBodyLength   int    `json:"min_body_length"`   // Minimum body length to summarize
	SkipReplies     bool   `json:"skip_replies"`      // Skip if email is a reply
	SkipShortEmails bool   `json:"skip_short_emails"` // Skip emails below min_body_length
}

// MessageSummarizerDetector determines if summarization should run
type MessageSummarizerDetector struct {
	minBodyLength   int
	skipReplies     bool
	skipShortEmails bool
}

func (d *MessageSummarizerDetector) ShouldProcess(ctx context.Context, email *Email, state *PipelineState) (bool, error) {
	// Skip if no body
	if email.Body == nil {
		return false, nil
	}

	content := d.getContentLength(email)

	// Skip short emails if configured
	if d.skipShortEmails && content < d.minBodyLength {
		return false, nil
	}

	// Skip replies if configured
	if d.skipReplies && d.isReply(email) {
		return false, nil
	}

	// Check if summarizer already ran (idempotency)
	if state.GetProcessorResult(MessageSummarizerType) != nil {
		return false, nil
	}

	return true, nil
}

func (d *MessageSummarizerDetector) getContentLength(email *Email) int {
	if email.Body.PlainText != "" {
		return len(email.Body.PlainText)
	}
	if email.Body.Text != "" {
		return len(email.Body.Text)
	}
	if email.Body.HTML != "" {
		return len(stripHTML(email.Body.HTML))
	}
	return 0
}

func (d *MessageSummarizerDetector) isReply(email *Email) bool {
	subjectLower := strings.ToLower(email.Subject)
	return strings.HasPrefix(subjectLower, "re:") ||
		strings.HasPrefix(subjectLower, "aw:") ||
		strings.Contains(subjectLower, "original message")
}

// MessageSummarizer generates email summaries
type MessageSummarizer struct {
	maxLength     int
	provider      string
	model         string
	minBodyLength int
}

// MessageSummarizerMeta extracts configuration from ProcessorMeta
func MessageSummarizerMeta(meta ProcessorMeta) (*MessageSummarizerConfig, error) {
	config := &MessageSummarizerConfig{
		MaxLength:       meta.GetInt("max_length"),
		Provider:        meta.GetString("provider"),
		Model:           meta.GetString("model"),
		MinBodyLength:   meta.GetInt("min_body_length"),
		SkipReplies:     meta.GetBool("skip_replies"),
		SkipShortEmails: meta.GetBool("skip_short_emails"),
	}

	if config.MaxLength <= 0 {
		config.MaxLength = 100 // Default
	}
	if config.Provider == "" {
		config.Provider = "local" // Default to local summarization
	}
	if config.MinBodyLength <= 0 {
		config.MinBodyLength = 50 // Default minimum
	}

	return config, nil
}

// NewMessageSummarizer creates a new message summarizer processor
func NewMessageSummarizer(meta ProcessorMeta) (Processor, error) {
	config, err := MessageSummarizerMeta(meta)
	if err != nil {
		return nil, err
	}

	return &MessageSummarizer{
		maxLength:     config.MaxLength,
		provider:      config.Provider,
		model:         config.Model,
		minBodyLength: config.MinBodyLength,
	}, nil
}

// NewMessageSummarizerDetector creates a new message summarizer detector
func NewMessageSummarizerDetector(meta ProcessorMeta) (Detector, error) {
	config, err := MessageSummarizerMeta(meta)
	if err != nil {
		return nil, err
	}

	return &MessageSummarizerDetector{
		minBodyLength:   config.MinBodyLength,
		skipReplies:     config.SkipReplies,
		skipShortEmails: config.SkipShortEmails,
	}, nil
}

func (p *MessageSummarizer) Type() string {
	return MessageSummarizerType
}

func (p *MessageSummarizer) Detector() Detector {
	return &MessageSummarizerDetector{
		minBodyLength:   p.minBodyLength,
		skipReplies:     false,
		skipShortEmails: false,
	}
}

func (p *MessageSummarizer) Process(ctx context.Context, email *Email, state *PipelineState) error {
	if email.Body == nil {
		return nil
	}

	// Get content to summarize
	content := p.getContentToSummarize(email)
	if content == "" {
		return nil
	}

	// Generate summary based on provider
	summary, err := p.generateSummary(ctx, content)
	if err != nil {
		return fmt.Errorf("summarizer failed: %w", err)
	}

	// Store summary in email metadata (would be persisted)
	if email.Headers == nil {
		email.Headers = make(map[string]string)
	}
	email.Headers["X-Summary"] = summary

	// Store in pipeline state for other processors
	state.Set("summary", summary)
	state.Set("summary_length", len(summary))

	return nil
}

func (p *MessageSummarizer) getContentToSummarize(email *Email) string {
	// Prefer plain text for summarization
	if email.Body.PlainText != "" {
		return email.Body.PlainText
	}
	if email.Body.Text != "" {
		return email.Body.Text
	}
	// Strip HTML tags if only HTML available
	if email.Body.HTML != "" {
		return stripHTML(email.Body.HTML)
	}
	return ""
}

func (p *MessageSummarizer) generateSummary(ctx context.Context, content string) (string, error) {
	// Simple extractive summarization (first N characters/words)
	// In production, this would call an LLM API based on provider config

	if len(content) <= p.maxLength {
		return content, nil
	}

	// Truncate at word boundary
	summary := content[:p.maxLength]
	if lastSpace := strings.LastIndex(summary, " "); lastSpace > 0 {
		summary = summary[:lastSpace]
	}

	return summary + "...", nil
}

func init() {
	MustRegister(MessageSummarizerType, NewMessageSummarizer, NewMessageSummarizerDetector, "Generates short summaries of email content")
}
