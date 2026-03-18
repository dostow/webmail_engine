package processor

import (
	"context"
	"testing"
)

func TestRegistry_Register(t *testing.T) {
	registry := NewRegistry()

	// Test successful registration
	err := registry.Register("test_processor", func(meta ProcessorMeta) (Processor, error) {
		return nil, nil
	}, func(meta ProcessorMeta) (Detector, error) {
		return nil, nil
	}, "Test processor")

	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// Test duplicate registration
	err = registry.Register("test_processor", func(meta ProcessorMeta) (Processor, error) {
		return nil, nil
	}, nil, "Test processor")

	if err == nil {
		t.Error("Register() expected error for duplicate registration, got nil")
	}

	// Test empty name
	err = registry.Register("", func(meta ProcessorMeta) (Processor, error) {
		return nil, nil
	}, nil, "Test processor")

	if err == nil {
		t.Error("Register() expected error for empty name, got nil")
	}

	// Test nil factory
	err = registry.Register("test2", nil, nil, "Test processor")

	if err == nil {
		t.Error("Register() expected error for nil factory, got nil")
	}
}

func TestRegistry_NewProcessor(t *testing.T) {
	registry := NewRegistry()

	// Register a test processor
	registry.Register("test_processor", func(meta ProcessorMeta) (Processor, error) {
		return &testProcessor{name: "test"}, nil
	}, nil, "Test processor")

	// Test successful creation
	processor, err := registry.NewProcessor("test_processor", ProcessorMeta(`{}`))
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	if processor == nil {
		t.Error("NewProcessor() expected processor, got nil")
	}
	if processor.Type() != "test" {
		t.Errorf("NewProcessor() Type() = %v, want %v", processor.Type(), "test")
	}

	// Test unregistered processor
	_, err = registry.NewProcessor("unknown_processor", ProcessorMeta(`{}`))
	if err == nil {
		t.Error("NewProcessor() expected error for unregistered processor, got nil")
	}
}

func TestRegistry_NewDetector(t *testing.T) {
	registry := NewRegistry()

	// Register with detector factory
	registry.Register("test_with_detector",
		func(meta ProcessorMeta) (Processor, error) {
			return &testProcessor{name: "test"}, nil
		},
		func(meta ProcessorMeta) (Detector, error) {
			return &testDetector{}, nil
		},
		"Test processor with detector")

	// Test detector creation
	detector, err := registry.NewDetector("test_with_detector", ProcessorMeta(`{}`))
	if err != nil {
		t.Fatalf("NewDetector() error = %v", err)
	}
	if detector == nil {
		t.Error("NewDetector() expected detector, got nil")
	}

	// Test detector for processor without detector factory
	registry.Register("test_no_detector",
		func(meta ProcessorMeta) (Processor, error) {
			return &testProcessor{name: "test"}, nil
		},
		nil,
		"Test processor without detector")

	detector, err = registry.NewDetector("test_no_detector", ProcessorMeta(`{}`))
	if err != nil {
		t.Fatalf("NewDetector() error = %v", err)
	}
	if detector != nil {
		t.Error("NewDetector() expected nil for processor without detector factory")
	}
}

func TestRegistry_ListRegisteredTypes(t *testing.T) {
	registry := NewRegistry()

	// Initially empty
	types := registry.ListRegisteredTypes()
	if len(types) != 0 {
		t.Errorf("ListRegisteredTypes() = %v, want empty", types)
	}

	// Register some processors
	registry.Register("processor_a", func(meta ProcessorMeta) (Processor, error) {
		return nil, nil
	}, nil, "A")
	registry.Register("processor_b", func(meta ProcessorMeta) (Processor, error) {
		return nil, nil
	}, nil, "B")

	types = registry.ListRegisteredTypes()
	if len(types) != 2 {
		t.Errorf("ListRegisteredTypes() length = %v, want 2", len(types))
	}
}

func TestRegistry_IsRegistered(t *testing.T) {
	registry := NewRegistry()

	if registry.IsRegistered("test") {
		t.Error("IsRegistered() = true for unregistered processor")
	}

	registry.Register("test", func(meta ProcessorMeta) (Processor, error) {
		return nil, nil
	}, nil, "Test")

	if !registry.IsRegistered("test") {
		t.Error("IsRegistered() = false for registered processor")
	}
}

func TestPipelineState(t *testing.T) {
	state := NewPipelineState()

	// Test Get/Set
	state.Set("key1", "value1")
	if state.Get("key1") != "value1" {
		t.Errorf("Get() = %v, want value1", state.Get("key1"))
	}

	// Test GetString
	state.Set("key2", "value2")
	if state.GetString("key2") != "value2" {
		t.Errorf("GetString() = %v, want value2", state.GetString("key2"))
	}

	// Test GetBool
	state.Set("key3", true)
	if !state.GetBool("key3") {
		t.Error("GetBool() = false, want true")
	}

	// Test Abort
	state.Abort("test reason")
	if !state.IsAborted() {
		t.Error("IsAborted() = false, want true")
	}
	if state.AbortReason() != "test reason" {
		t.Errorf("AbortReason() = %v, want 'test reason'", state.AbortReason())
	}
}

func TestPipelineState_ProcessorResult(t *testing.T) {
	state := NewPipelineState()

	result := &ProcessorResult{
		ProcessorType: "test_processor",
		Success:       true,
		Duration:      100,
	}

	state.SetProcessorResult("test_processor", result)

	retrieved := state.GetProcessorResult("test_processor")
	if retrieved == nil {
		t.Error("GetProcessorResult() = nil, want result")
	}
	if retrieved.ProcessorType != "test_processor" {
		t.Errorf("GetProcessorResult().ProcessorType = %v, want test_processor", retrieved.ProcessorType)
	}
	if retrieved.Duration != 100 {
		t.Errorf("GetProcessorResult().Duration = %v, want 100", retrieved.Duration)
	}
}

func TestProcessorMeta(t *testing.T) {
	meta := ProcessorMeta(`{"string_key": "value", "int_key": 42, "bool_key": true}`)

	// Test GetString
	if meta.GetString("string_key") != "value" {
		t.Errorf("GetString() = %v, want value", meta.GetString("string_key"))
	}

	// Test GetInt
	if meta.GetInt("int_key") != 42 {
		t.Errorf("GetInt() = %v, want 42", meta.GetInt("int_key"))
	}

	// Test GetBool
	if !meta.GetBool("bool_key") {
		t.Error("GetBool() = false, want true")
	}

	// Test GetStringSlice
	metaSlice := ProcessorMeta(`{"items": ["a", "b", "c"]}`)
	items := metaSlice.GetStringSlice("items")
	if len(items) != 3 {
		t.Errorf("GetStringSlice() length = %v, want 3", len(items))
	}
}

// testProcessor is a mock processor for testing
type testProcessor struct {
	name string
}

func (p *testProcessor) Process(ctx context.Context, email *Email, state *PipelineState) error {
	return nil
}

func (p *testProcessor) Type() string {
	return p.name
}

func (p *testProcessor) Detector() Detector {
	return nil
}

// testDetector is a mock detector for testing
type testDetector struct{}

func (d *testDetector) ShouldProcess(ctx context.Context, email *Email, state *PipelineState) (bool, error) {
	return true, nil
}
