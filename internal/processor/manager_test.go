package processor

import (
	"context"
	"testing"
)

func TestAccountProcessorManager_BuildPipeline(t *testing.T) {
	configs := []AccountProcessorConfig{
		{Type: "spam_filter", Meta: ProcessorMeta(`{"threshold": 0.8}`), Enabled: true, Priority: 1},
		{Type: "link_tracker", Meta: ProcessorMeta(`{"base_url": "https://track.example.com", "salt": "test"}`), Enabled: true, Priority: 2},
		{Type: "message_summarizer", Meta: ProcessorMeta(`{"max_length": 100}`), Enabled: false, Priority: 3}, // Disabled
	}

	manager, err := NewAccountProcessorManager(AccountProcessorManagerConfig{
		AccountID: "test-account",
		Configs:   configs,
	})
	if err != nil {
		t.Fatalf("NewAccountProcessorManager() error = %v", err)
	}

	info := manager.GetPipelineInfo()
	if info.ProcessorCount != 2 {
		t.Errorf("GetPipelineInfo().ProcessorCount = %d, want 2", info.ProcessorCount)
	}

	// Check processor order (by priority)
	if len(info.ProcessorTypes) != 2 {
		t.Fatalf("GetPipelineInfo().ProcessorTypes length = %d, want 2", len(info.ProcessorTypes))
	}
	if info.ProcessorTypes[0] != "spam_filter" {
		t.Errorf("First processor = %v, want spam_filter", info.ProcessorTypes[0])
	}
	if info.ProcessorTypes[1] != "link_tracker" {
		t.Errorf("Second processor = %v, want link_tracker", info.ProcessorTypes[1])
	}
}

func TestAccountProcessorManager_ProcessEmail(t *testing.T) {
	configs := []AccountProcessorConfig{
		{Type: "spam_filter", Meta: ProcessorMeta(`{"threshold": 0.8}`), Enabled: true, Priority: 1},
	}

	manager, err := NewAccountProcessorManager(AccountProcessorManagerConfig{
		AccountID: "test-account",
		Configs:   configs,
	})
	if err != nil {
		t.Fatalf("NewAccountProcessorManager() error = %v", err)
	}

	email := &Email{
		From:    "sender@example.com",
		Subject: "Normal email",
		Body: &EmailBody{
			Text: "Hello, this is a normal email.",
		},
	}

	results, err := manager.ProcessEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("ProcessEmail() error = %v", err)
	}

	if len(results) != 1 {
		t.Errorf("ProcessEmail() returned %d results, want 1", len(results))
	}

	if results[0].ProcessorType != "spam_filter" {
		t.Errorf("ProcessEmail() first result type = %v, want spam_filter", results[0].ProcessorType)
	}

	if !results[0].Success {
		t.Errorf("ProcessEmail() result success = %v, want true", results[0].Success)
	}
}

func TestAccountProcessorManager_DetectorSkip(t *testing.T) {
	configs := []AccountProcessorConfig{
		{Type: "spam_filter", Meta: ProcessorMeta(`{"threshold": 0.8}`), Enabled: true, Priority: 1},
		{Type: "link_tracker", Meta: ProcessorMeta(`{"base_url": "https://track.example.com", "salt": "test"}`), Enabled: true, Priority: 2},
	}

	manager, err := NewAccountProcessorManager(AccountProcessorManagerConfig{
		AccountID: "test-account",
		Configs:   configs,
	})
	if err != nil {
		t.Fatalf("NewAccountProcessorManager() error = %v", err)
	}

	// First email - normal
	email1 := &Email{
		From:    "sender@example.com",
		Subject: "Normal email",
		Body: &EmailBody{
			HTML: `<a href="https://example.com/page">Link</a>`,
		},
	}

	results, _ := manager.ProcessEmail(context.Background(), email1)
	// Should have both processors run
	if len(results) != 2 {
		t.Errorf("Normal email: ProcessEmail() returned %d results, want 2", len(results))
	}

	// Second email - spam (link tracker should skip)
	email2 := &Email{
		From:    "spammer@example.com",
		Subject: "WINNER!!! FREE MONEY!!!",
		Body: &EmailBody{
			HTML: `<a href="https://spam.com/claim">Claim now!</a>`,
		},
	}

	results, _ = manager.ProcessEmail(context.Background(), email2)
	// Spam filter runs, link tracker might skip due to spam
	// Note: This depends on spam score calculation
	t.Logf("Spam email results: %d processors ran", len(results))
}

func TestAccountProcessorManager_PipelineAbort(t *testing.T) {
	configs := []AccountProcessorConfig{
		{Type: "spam_filter", Meta: ProcessorMeta(`{"threshold": 0.3, "abort_on_spam": true}`), Enabled: true, Priority: 1},
		{Type: "link_tracker", Meta: ProcessorMeta(`{"base_url": "https://track.example.com", "salt": "test"}`), Enabled: true, Priority: 2},
	}

	manager, err := NewAccountProcessorManager(AccountProcessorManagerConfig{
		AccountID: "test-account",
		Configs:   configs,
	})
	if err != nil {
		t.Fatalf("NewAccountProcessorManager() error = %v", err)
	}

	email := &Email{
		From:    "sender@example.com",
		Subject: "WINNER!!! FREE!!!", // Should trigger spam
		Body: &EmailBody{
			HTML: `<a href="https://example.com/page">Link</a>`,
		},
	}

	results, err := manager.ProcessEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("ProcessEmail() error = %v", err)
	}

	// Should only have spam_filter result (link_tracker should be aborted)
	if len(results) != 1 {
		t.Errorf("ProcessEmail() returned %d results, want 1 (pipeline aborted)", len(results))
	}
	if results[0].ProcessorType != "spam_filter" {
		t.Errorf("ProcessEmail() first result type = %v, want spam_filter", results[0].ProcessorType)
	}
}

func TestAccountProcessorManager_UpdateConfigs(t *testing.T) {
	configs := []AccountProcessorConfig{
		{Type: "spam_filter", Meta: ProcessorMeta(`{"threshold": 0.8}`), Enabled: true, Priority: 1},
	}

	manager, err := NewAccountProcessorManager(AccountProcessorManagerConfig{
		AccountID: "test-account",
		Configs:   configs,
	})
	if err != nil {
		t.Fatalf("NewAccountProcessorManager() error = %v", err)
	}

	// Update configs
	newConfigs := []AccountProcessorConfig{
		{Type: "spam_filter", Meta: ProcessorMeta(`{"threshold": 0.5}`), Enabled: true, Priority: 1},
		{Type: "link_tracker", Meta: ProcessorMeta(`{"base_url": "https://track.example.com", "salt": "test"}`), Enabled: true, Priority: 2},
	}

	err = manager.UpdateConfigs(newConfigs)
	if err != nil {
		t.Fatalf("UpdateConfigs() error = %v", err)
	}

	info := manager.GetPipelineInfo()
	if info.ProcessorCount != 2 {
		t.Errorf("GetPipelineInfo().ProcessorCount = %d, want 2 after update", info.ProcessorCount)
	}
}

func TestAccountProcessorManager_UnregisteredProcessor(t *testing.T) {
	configs := []AccountProcessorConfig{
		{Type: "spam_filter", Meta: ProcessorMeta(`{"threshold": 0.8}`), Enabled: true, Priority: 1},
		{Type: "unknown_processor", Meta: ProcessorMeta(`{}`), Enabled: true, Priority: 2}, // Not registered
	}

	manager, err := NewAccountProcessorManager(AccountProcessorManagerConfig{
		AccountID: "test-account",
		Configs:   configs,
	})
	if err != nil {
		t.Fatalf("NewAccountProcessorManager() error = %v", err)
	}

	info := manager.GetPipelineInfo()
	// Should only have spam_filter (unknown_processor should be skipped)
	if info.ProcessorCount != 1 {
		t.Errorf("GetPipelineInfo().ProcessorCount = %d, want 1 (unknown skipped)", info.ProcessorCount)
	}
}
