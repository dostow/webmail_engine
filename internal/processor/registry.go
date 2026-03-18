package processor

import (
	"fmt"
	"sync"
)

// ProcessorFactory creates a new Processor instance given its meta configuration
type ProcessorFactory func(meta ProcessorMeta) (Processor, error)

// DetectorFactory creates a new Detector instance given its meta configuration
type DetectorFactory func(meta ProcessorMeta) (Detector, error)

// ProcessorInfo holds metadata about a registered processor
type ProcessorInfo struct {
	Factory         ProcessorFactory
	DetectorFactory DetectorFactory
	Description     string
}

// Registry manages processor registration and instantiation
type Registry struct {
	mu         sync.RWMutex
	processors map[string]*ProcessorInfo
}

// Global registry instance
var globalRegistry = NewRegistry()

// NewRegistry creates a new processor registry
func NewRegistry() *Registry {
	return &Registry{
		processors: make(map[string]*ProcessorInfo),
	}
}

// GlobalRegistry returns the global processor registry
func GlobalRegistry() *Registry {
	return globalRegistry
}

// RegisterProcessor registers a processor factory with the global registry
// Panics if called with duplicate type name (use MustRegister for init functions)
func RegisterProcessor(typeName string, factory ProcessorFactory, detectorFactory DetectorFactory, description string) error {
	return globalRegistry.Register(typeName, factory, detectorFactory, description)
}

// MustRegister registers a processor and panics on failure (for init functions)
func MustRegister(typeName string, factory ProcessorFactory, detectorFactory DetectorFactory, description string) {
	if err := globalRegistry.Register(typeName, factory, detectorFactory, description); err != nil {
		panic(err)
	}
}

// Register adds a processor factory to the registry
func (r *Registry) Register(typeName string, factory ProcessorFactory, detectorFactory DetectorFactory, description string) error {
	if typeName == "" {
		return fmt.Errorf("processor type name cannot be empty")
	}
	if factory == nil {
		return fmt.Errorf("processor factory cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.processors[typeName]; exists {
		return fmt.Errorf("processor type already registered: %s", typeName)
	}

	r.processors[typeName] = &ProcessorInfo{
		Factory:         factory,
		DetectorFactory: detectorFactory,
		Description:     description,
	}
	return nil
}

// NewProcessor creates a new processor instance of the specified type
func (r *Registry) NewProcessor(typeName string, meta ProcessorMeta) (Processor, error) {
	r.mu.RLock()
	info, exists := r.processors[typeName]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("processor type not registered: %s", typeName)
	}

	return info.Factory(meta)
}

// NewDetector creates a new detector instance for the specified processor type
func (r *Registry) NewDetector(typeName string, meta ProcessorMeta) (Detector, error) {
	r.mu.RLock()
	info, exists := r.processors[typeName]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("processor type not registered: %s", typeName)
	}

	if info.DetectorFactory == nil {
		return nil, nil // No detector, processor always runs
	}

	return info.DetectorFactory(meta)
}

// GetProcessorInfo returns information about a registered processor
func (r *Registry) GetProcessorInfo(typeName string) (*ProcessorInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info, exists := r.processors[typeName]
	if !exists {
		return nil, fmt.Errorf("processor type not registered: %s", typeName)
	}

	return info, nil
}

// ListRegisteredTypes returns all registered processor type names
func (r *Registry) ListRegisteredTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.processors))
	for t := range r.processors {
		types = append(types, t)
	}
	return types
}

// IsRegistered checks if a processor type is registered
func (r *Registry) IsRegistered(typeName string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.processors[typeName]
	return exists
}
