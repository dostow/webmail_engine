package processor

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// Email represents the email data to be processed
type Email struct {
	ID          string            `json:"id"`
	AccountID   string            `json:"account_id"`
	FolderName  string            `json:"folder_name"`
	UID         uint32            `json:"uid"`
	MessageID   string            `json:"message_id"`
	Headers     map[string]string `json:"headers,omitempty"`
	Subject     string            `json:"subject"`
	From        string            `json:"from"`
	To          []string          `json:"to"`
	Body        *EmailBody        `json:"body,omitempty"`
	Attachments []Attachment      `json:"attachments,omitempty"`
	Links       []string          `json:"links,omitempty"`
	Flags       []string          `json:"flags"`
	Date        time.Time         `json:"date"`
	Size        int64             `json:"size"`
}

// EmailBody represents email content
type EmailBody struct {
	Text      string `json:"text,omitempty"`
	HTML      string `json:"html,omitempty"`
	PlainText string `json:"plain_text,omitempty"`
}

// Attachment represents an email attachment
type Attachment struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	ContentID   string `json:"content_id,omitempty"`
	Data        []byte `json:"-"` // Raw bytes for processing
}

// ProcessorMeta holds configuration arguments specific to a processor type
// Uses json.RawMessage for flexible, type-safe configuration
type ProcessorMeta json.RawMessage

// Get retrieves a value from the meta by key
func (m ProcessorMeta) Get(key string) interface{} {
	var data map[string]interface{}
	if err := json.Unmarshal(m, &data); err != nil {
		return nil
	}
	return data[key]
}

// GetString retrieves a string value from the meta
func (m ProcessorMeta) GetString(key string) string {
	if v, ok := m.Get(key).(string); ok {
		return v
	}
	return ""
}

// GetInt retrieves an integer value from the meta
func (m ProcessorMeta) GetInt(key string) int {
	if v, ok := m.Get(key).(float64); ok {
		return int(v)
	}
	return 0
}

// GetBool retrieves a boolean value from the meta
func (m ProcessorMeta) GetBool(key string) bool {
	if v, ok := m.Get(key).(bool); ok {
		return v
	}
	return false
}

// GetStringSlice retrieves a string slice from the meta
func (m ProcessorMeta) GetStringSlice(key string) []string {
	var result []string
	if items, ok := m.Get(key).([]interface{}); ok {
		for _, item := range items {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
	}
	return result
}

// Detector determines if a processor should run on a given email
type Detector interface {
	// ShouldProcess returns true if the processor should be executed
	// Can examine email and pipeline state to make decision
	ShouldProcess(ctx context.Context, email *Email, state *PipelineState) (bool, error)
}

// PipelineState holds shared state across the processor pipeline
// Similar to middleware context in routing systems
type PipelineState struct {
	mu          sync.RWMutex
	data        map[string]interface{}
	aborted     bool
	abortReason string
}

// NewPipelineState creates a new pipeline state
func NewPipelineState() *PipelineState {
	return &PipelineState{
		data:    make(map[string]interface{}),
		aborted: false,
	}
}

// Get retrieves a value from the pipeline state
func (s *PipelineState) Get(key string) interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data[key]
}

// Set stores a value in the pipeline state
func (s *PipelineState) Set(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
}

// GetString retrieves a string value from the pipeline state
func (s *PipelineState) GetString(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if v, ok := s.data[key].(string); ok {
		return v
	}
	return ""
}

// GetBool retrieves a boolean value from the pipeline state
func (s *PipelineState) GetBool(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if v, ok := s.data[key].(bool); ok {
		return v
	}
	return false
}

// GetProcessorResult retrieves the result of a previous processor
func (s *PipelineState) GetProcessorResult(processorType string) *ProcessorResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := "processor_result_" + processorType
	if v, ok := s.data[key].(*ProcessorResult); ok {
		return v
	}
	return nil
}

// SetProcessorResult stores a processor result for use by subsequent processors
func (s *PipelineState) SetProcessorResult(processorType string, result *ProcessorResult) {
	s.Set("processor_result_"+processorType, result)
}

// Abort stops pipeline execution with a reason
func (s *PipelineState) Abort(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.aborted = true
	s.abortReason = reason
}

// IsAborted checks if the pipeline has been aborted
func (s *PipelineState) IsAborted() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.aborted
}

// AbortReason returns the abort reason if aborted
func (s *PipelineState) AbortReason() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.abortReason
}

// Processor is the core interface all processors must implement
type Processor interface {
	// Process executes the processor on the given email
	// Can read/write pipeline state to share data with other processors
	Process(ctx context.Context, email *Email, state *PipelineState) error

	// Type returns the canonical processor type name (e.g., "link_tracker")
	Type() string

	// Detector returns the detector for this processor
	// Returns nil if processor should always run
	Detector() Detector
}

// ProcessorResult represents the outcome of processor execution
type ProcessorResult struct {
	ProcessorType string                 `json:"processor_type"`
	Success       bool                   `json:"success"`
	Error         string                 `json:"error,omitempty"`
	Data          map[string]interface{} `json:"data,omitempty"` // Processor-specific output
	Duration      int64                  `json:"duration_ms"`    // Processing time in milliseconds
}

// DetectorFunc is a function adapter for the Detector interface
type DetectorFunc func(ctx context.Context, email *Email, state *PipelineState) (bool, error)

func (f DetectorFunc) ShouldProcess(ctx context.Context, email *Email, state *PipelineState) (bool, error) {
	return f(ctx, email, state)
}
