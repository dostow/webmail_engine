package processor

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"
)

// AccountProcessorManager manages processors for a single sync account
type AccountProcessorManager struct {
	mu        sync.RWMutex
	accountID string
	configs   []AccountProcessorConfig
	pipeline  []Processor
	detectors []Detector
	registry  *Registry
}

// AccountProcessorConfig defines a processor enabled for a specific sync account
type AccountProcessorConfig struct {
	Type     string        `json:"type"`     // e.g., "llm_processor", "link_tracker"
	Meta     ProcessorMeta `json:"meta"`     // Type-specific configuration
	Enabled  bool          `json:"enabled"`  // Whether processor is active
	Priority int           `json:"priority"` // Execution order (lower = earlier)
}

// AccountProcessorManagerConfig holds manager configuration
type AccountProcessorManagerConfig struct {
	AccountID string                     `json:"account_id"`
	Configs   []AccountProcessorConfig   `json:"configs"`
	Registry  *Registry                  `json:"-"` // Optional custom registry
}

// NewAccountProcessorManager creates a new manager for an account
func NewAccountProcessorManager(cfg AccountProcessorManagerConfig) (*AccountProcessorManager, error) {
	if cfg.AccountID == "" {
		return nil, fmt.Errorf("account_id is required")
	}

	registry := cfg.Registry
	if registry == nil {
		registry = GlobalRegistry()
	}

	manager := &AccountProcessorManager{
		accountID: cfg.AccountID,
		configs:   cfg.Configs,
		registry:  registry,
	}

	// Build initial pipeline
	if err := manager.BuildPipeline(); err != nil {
		return nil, fmt.Errorf("failed to build pipeline: %w", err)
	}

	return manager, nil
}

// BuildPipeline instantiates processors and detectors based on configuration
func (m *AccountProcessorManager) BuildPipeline() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.pipeline = make([]Processor, 0)
	m.detectors = make([]Detector, 0)

	// Filter and sort configs
	var enabledConfigs []AccountProcessorConfig
	for _, cfg := range m.configs {
		if !cfg.Enabled {
			continue
		}
		enabledConfigs = append(enabledConfigs, cfg)
	}

	// Sort by priority (lower = earlier execution)
	sort.Slice(enabledConfigs, func(i, j int) bool {
		return enabledConfigs[i].Priority < enabledConfigs[j].Priority
	})

	// Instantiate processors and detectors
	for _, cfg := range enabledConfigs {
		processor, err := m.registry.NewProcessor(cfg.Type, cfg.Meta)
		if err != nil {
			log.Printf("Failed to create processor %s for account %s: %v",
				cfg.Type, m.accountID, err)
			continue // Skip failed processors, continue with others
		}

		// Get or create detector for this processor
		detector, err := m.registry.NewDetector(cfg.Type, cfg.Meta)
		if err != nil {
			log.Printf("Failed to create detector for processor %s for account %s: %v",
				cfg.Type, m.accountID, err)
			// Continue without detector - processor will always run
			detector = nil
		}

		// If processor has its own detector method, use that
		if processor.Detector() != nil {
			detector = processor.Detector()
		}

		m.pipeline = append(m.pipeline, processor)
		m.detectors = append(m.detectors, detector)
	}

	return nil
}

// ProcessEmail executes the processor pipeline on an email with shared state
func (m *AccountProcessorManager) ProcessEmail(ctx context.Context, email *Email) ([]ProcessorResult, error) {
	m.mu.RLock()
	pipeline := m.pipeline
	detectors := m.detectors
	m.mu.RUnlock()

	results := make([]ProcessorResult, 0, len(pipeline))

	// Create shared pipeline state (like middleware context)
	state := NewPipelineState()

	for i, processor := range pipeline {
		// Check if pipeline was aborted by previous processor
		if state.IsAborted() {
			log.Printf("Pipeline aborted for account %s: %s",
				m.accountID, state.AbortReason())
			break
		}

		// Get detector for this processor
		detector := detectors[i]

		// Evaluate detector if present
		if detector != nil {
			shouldRun, err := detector.ShouldProcess(ctx, email, state)
			if err != nil {
				log.Printf("Detector error for processor %s: %v", processor.Type(), err)
				// Continue processing - detector error doesn't stop pipeline
				shouldRun = true
			}
			if !shouldRun {
				log.Printf("Detector skipped processor %s for account %s",
					processor.Type(), m.accountID)
				continue // Skip this processor
			}
		}

		startTime := time.Now()
		result := ProcessorResult{
			ProcessorType: processor.Type(),
			Success:       true,
		}

		// Execute processor with pipeline state
		err := processor.Process(ctx, email, state)
		result.Duration = time.Since(startTime).Milliseconds()

		if err != nil {
			result.Success = false
			result.Error = err.Error()
			log.Printf("Processor %s failed for account %s: %v",
				processor.Type(), m.accountID, err)
			// Continue processing other processors (fail-safe)
		}

		// Store result in pipeline state for other processors
		state.SetProcessorResult(processor.Type(), &result)

		results = append(results, result)
	}

	return results, nil
}

// UpdateConfigs updates processor configurations and rebuilds the pipeline
func (m *AccountProcessorManager) UpdateConfigs(configs []AccountProcessorConfig) error {
	m.mu.Lock()
	m.configs = configs
	m.mu.Unlock()

	return m.BuildPipeline()
}

// GetConfigs returns current processor configurations
func (m *AccountProcessorManager) GetConfigs() []AccountProcessorConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	configs := make([]AccountProcessorConfig, len(m.configs))
	copy(configs, m.configs)
	return configs
}

// GetPipelineInfo returns information about the current pipeline
func (m *AccountProcessorManager) GetPipelineInfo() PipelineInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	info := PipelineInfo{
		AccountID:      m.accountID,
		ProcessorCount: len(m.pipeline),
		ProcessorTypes: make([]string, 0, len(m.pipeline)),
	}

	for _, p := range m.pipeline {
		info.ProcessorTypes = append(info.ProcessorTypes, p.Type())
	}

	return info
}

// PipelineInfo represents pipeline status information
type PipelineInfo struct {
	AccountID      string   `json:"account_id"`
	ProcessorCount int      `json:"processor_count"`
	ProcessorTypes []string `json:"processor_types"`
}

// PipelineStateAccessor provides access to pipeline state for external components
type PipelineStateAccessor struct {
	state *PipelineState
}

// Get retrieves a value from the pipeline state
func (a *PipelineStateAccessor) Get(key string) interface{} {
	return a.state.Get(key)
}

// GetString retrieves a string value from the pipeline state
func (a *PipelineStateAccessor) GetString(key string) string {
	return a.state.GetString(key)
}

// GetBool retrieves a boolean value from the pipeline state
func (a *PipelineStateAccessor) GetBool(key string) bool {
	return a.state.GetBool(key)
}

// GetProcessorResult retrieves the result of a previous processor
func (a *PipelineStateAccessor) GetProcessorResult(processorType string) *ProcessorResult {
	return a.state.GetProcessorResult(processorType)
}
