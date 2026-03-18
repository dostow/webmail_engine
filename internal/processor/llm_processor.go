package processor

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const LLMProcessorType = "llm_processor"

// LLMProcessorConfig represents LLM processor configuration
type LLMProcessorConfig struct {
	SystemPrompt string  `json:"system_prompt"` // System prompt for LLM
	UserPrompt   string  `json:"user_prompt"`   // User prompt template
	Provider     string  `json:"provider"`      // LLM provider (e.g., "openai", "anthropic")
	Model        string  `json:"model"`         // Model name
	APIKey       string  `json:"api_key"`       // API key (should be encrypted)
	Temperature  float64 `json:"temperature"`   // Sampling temperature
	MaxTokens    int     `json:"max_tokens"`    // Maximum output tokens
}

// LLMProcessorDetector determines if LLM processing should run
type LLMProcessorDetector struct {
	requireSummary bool
}

func (d *LLMProcessorDetector) ShouldProcess(ctx context.Context, email *Email, state *PipelineState) (bool, error) {
	// Check if LLM processor already ran (idempotency)
	if state.GetProcessorResult(LLMProcessorType) != nil {
		return false, nil
	}

	// If requireSummary is set, check if summary exists in state
	if d.requireSummary {
		summary := state.Get("summary")
		if summary == nil {
			return false, nil // Skip if no summary available
		}
	}

	return true, nil
}

// LLMProcessor performs custom LLM-based email processing
type LLMProcessor struct {
	systemPrompt string
	userPrompt   string
	provider     string
	model        string
	apiKey       string
	temperature  float64
	maxTokens    int
}

// LLMProcessorMeta extracts configuration from ProcessorMeta
func LLMProcessorMeta(meta ProcessorMeta) (*LLMProcessorConfig, error) {
	config := &LLMProcessorConfig{
		SystemPrompt: meta.GetString("system_prompt"),
		UserPrompt:   meta.GetString("user_prompt"),
		Provider:     meta.GetString("provider"),
		Model:        meta.GetString("model"),
		APIKey:       meta.GetString("api_key"),
		Temperature:  0.7,  // Default
		MaxTokens:    1024, // Default
	}

	// Validate required fields
	if config.SystemPrompt == "" {
		return nil, fmt.Errorf("llm_processor: system_prompt is required")
	}
	if config.UserPrompt == "" {
		return nil, fmt.Errorf("llm_processor: user_prompt is required")
	}
	if config.Provider == "" {
		config.Provider = "openai" // Default provider
	}

	// Parse optional fields
	if t := meta.Get("temperature"); t != nil {
		if temp, ok := t.(float64); ok {
			config.Temperature = temp
		}
	}
	if m := meta.Get("max_tokens"); m != nil {
		if maxTokens, ok := m.(float64); ok {
			config.MaxTokens = int(maxTokens)
		}
	}

	return config, nil
}

// NewLLMProcessor creates a new LLM processor
func NewLLMProcessor(meta ProcessorMeta) (Processor, error) {
	config, err := LLMProcessorMeta(meta)
	if err != nil {
		return nil, err
	}

	return &LLMProcessor{
		systemPrompt: config.SystemPrompt,
		userPrompt:   config.UserPrompt,
		provider:     config.Provider,
		model:        config.Model,
		apiKey:       config.APIKey,
		temperature:  config.Temperature,
		maxTokens:    config.MaxTokens,
	}, nil
}

// NewLLMProcessorDetector creates a new LLM processor detector
func NewLLMProcessorDetector(meta ProcessorMeta) (Detector, error) {
	return &LLMProcessorDetector{
		requireSummary: false, // Can be configured via meta if needed
	}, nil
}

func (p *LLMProcessor) Type() string {
	return LLMProcessorType
}

func (p *LLMProcessor) Detector() Detector {
	return &LLMProcessorDetector{
		requireSummary: false,
	}
}

func (p *LLMProcessor) Process(ctx context.Context, email *Email, state *PipelineState) error {
	// Build the prompt from template
	userPrompt := p.buildUserPrompt(email)

	// Read summary from state if available (for context)
	summary := state.GetString("summary")

	// Call LLM API (placeholder - implement actual API calls)
	result, err := p.callLLM(ctx, p.systemPrompt, userPrompt, summary)
	if err != nil {
		return fmt.Errorf("llm_processor failed: %w", err)
	}

	// Store result in email headers (would be persisted)
	if email.Headers == nil {
		email.Headers = make(map[string]string)
	}
	email.Headers["X-LLM-Result"] = result
	email.Headers["X-LLM-Provider"] = p.provider
	email.Headers["X-LLM-Timestamp"] = time.Now().Format(time.RFC3339)

	// Store in pipeline state for other processors
	state.Set("llm_result", result)

	return nil
}

func (p *LLMProcessor) buildUserPrompt(email *Email) string {
	// Replace template variables in user prompt
	prompt := p.userPrompt

	replacements := map[string]string{
		"{{subject}}":    email.Subject,
		"{{from}}":       email.From,
		"{{to}}":         strings.Join(email.To, ", "),
		"{{body}}":       p.getBodyText(email),
		"{{date}}":       email.Date.Format(time.RFC3339),
		"{{message_id}}": email.MessageID,
	}

	for key, value := range replacements {
		prompt = strings.ReplaceAll(prompt, key, value)
	}

	return prompt
}

func (p *LLMProcessor) getBodyText(email *Email) string {
	if email.Body == nil {
		return ""
	}
	if email.Body.PlainText != "" {
		return email.Body.PlainText
	}
	if email.Body.Text != "" {
		return email.Body.Text
	}
	return stripHTML(email.Body.HTML)
}

func (p *LLMProcessor) callLLM(ctx context.Context, systemPrompt, userPrompt, summary string) (string, error) {
	// Placeholder implementation
	// In production, this would:
	// 1. Select the appropriate API client based on p.provider
	// 2. Make the API call with systemPrompt, userPrompt, and config
	// 3. Parse and return the response

	// Example implementation structure:
	// switch p.provider {
	// case "openai":
	//     return p.callOpenAI(ctx, systemPrompt, userPrompt)
	// case "anthropic":
	//     return p.callAnthropic(ctx, systemPrompt, userPrompt)
	// case "local":
	//     return p.callLocalLLM(ctx, systemPrompt, userPrompt)
	// }

	// For now, return a placeholder result
	result := fmt.Sprintf("[LLM processed with provider=%s, model=%s]", p.provider, p.model)

	// Include summary in result if available
	if summary != "" {
		result += fmt.Sprintf(" | Summary: %s", summary)
	}

	return result, nil
}

func init() {
	MustRegister(LLMProcessorType, NewLLMProcessor, NewLLMProcessorDetector, "Performs custom LLM-based email processing")
}
