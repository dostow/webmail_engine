package processor

import (
	"context"
	"strings"
	"testing"
)

func TestLinkTracker_Process(t *testing.T) {
	meta := ProcessorMeta(`{"base_url": "https://track.example.com", "salt": "test-salt"}`)

	tracker, err := NewLinkTracker(meta)
	if err != nil {
		t.Fatalf("NewLinkTracker() error = %v", err)
	}

	email := &Email{
		ID: "test-email-123",
		Body: &EmailBody{
			HTML: `<a href="https://example.com/page">Link</a>`,
		},
	}

	state := NewPipelineState()
	err = tracker.Process(context.Background(), email, state)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	// Verify link was rewritten
	if !strings.Contains(email.Body.HTML, "https://track.example.com/track/") {
		t.Errorf("Process() expected tracking URL, got: %s", email.Body.HTML)
	}

	// Verify links_processed was stored in state
	linksProcessed := state.Get("links_processed")
	if linksProcessed == nil || linksProcessed.(int) == 0 {
		t.Error("Process() should store links_processed in state")
	}
}

func TestLinkTracker_IgnoreDomains(t *testing.T) {
	meta := ProcessorMeta(`{
		"base_url": "https://track.example.com",
		"salt": "test-salt",
		"ignore_domains": ["example.com", "trusted.org"]
	}`)

	tracker, err := NewLinkTracker(meta)
	if err != nil {
		t.Fatalf("NewLinkTracker() error = %v", err)
	}

	email := &Email{
		ID: "test-email-123",
		Body: &EmailBody{
			HTML: `<a href="https://example.com/page">Ignored</a>
			       <a href="https://other.com/page">Tracked</a>`,
		},
	}

	state := NewPipelineState()
	err = tracker.Process(context.Background(), email, state)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	// example.com should not be tracked (should still have original URL)
	if !strings.Contains(email.Body.HTML, `href="https://example.com/page"`) {
		t.Error("Process() should not track ignored domain - original URL should remain")
	}

	// other.com should be tracked (should have tracking URL)
	if !strings.Contains(email.Body.HTML, "https://track.example.com/track/") {
		t.Error("Process() should track non-ignored domain")
	}
}

func TestLinkTrackerDetector_ShouldProcess(t *testing.T) {
	detector := &LinkTrackerDetector{trackOnlyHTML: false}

	// Test with no body
	email := &Email{}
	state := NewPipelineState()
	shouldRun, err := detector.ShouldProcess(context.Background(), email, state)
	if err != nil {
		t.Fatalf("ShouldProcess() error = %v", err)
	}
	if shouldRun {
		t.Error("ShouldProcess() should return false for email with no body")
	}

	// Test with HTML body
	email = &Email{
		Body: &EmailBody{
			HTML: "<p>Content</p>",
		},
	}
	shouldRun, err = detector.ShouldProcess(context.Background(), email, state)
	if err != nil {
		t.Fatalf("ShouldProcess() error = %v", err)
	}
	if !shouldRun {
		t.Error("ShouldProcess() should return true for email with HTML body")
	}

	// Test trackOnlyHTML with text-only email
	detector = &LinkTrackerDetector{trackOnlyHTML: true}
	email = &Email{
		Body: &EmailBody{
			Text: "Plain text content",
		},
	}
	shouldRun, err = detector.ShouldProcess(context.Background(), email, state)
	if err != nil {
		t.Fatalf("ShouldProcess() error = %v", err)
	}
	if shouldRun {
		t.Error("ShouldProcess() should return false for text-only when trackOnlyHTML is true")
	}
}

func TestLinkTrackerDetector_SkipSpam(t *testing.T) {
	detector := &LinkTrackerDetector{}
	email := &Email{
		Body: &EmailBody{
			HTML: "<p>Content</p>",
		},
	}
	state := NewPipelineState()

	// Set spam result in state
	state.SetProcessorResult("spam_filter", &ProcessorResult{Success: true})
	state.Set("spam_flagged", true)

	shouldRun, err := detector.ShouldProcess(context.Background(), email, state)
	if err != nil {
		t.Fatalf("ShouldProcess() error = %v", err)
	}
	if shouldRun {
		t.Error("ShouldProcess() should return false for spam emails")
	}
}

func TestLinkTracker_buildTrackingURL(t *testing.T) {
	tracker := &LinkTracker{
		baseURL: "https://track.example.com",
		salt:    "test-salt",
	}

	url := tracker.buildTrackingURL("https://example.com/page", "email-123")

	// Check URL structure
	if !strings.HasPrefix(url, "https://track.example.com/track/") {
		t.Errorf("buildTrackingURL() = %v, should start with base URL", url)
	}
	if !strings.Contains(url, "email-123") {
		t.Error("buildTrackingURL() should contain email ID")
	}
	if !strings.Contains(url, "u=") {
		t.Error("buildTrackingURL() should contain encoded URL parameter")
	}
	if !strings.Contains(url, "s=") {
		t.Error("buildTrackingURL() should contain signature parameter")
	}
}

func TestLinkTrackerMeta_Validation(t *testing.T) {
	tests := []struct {
		name    string
		meta    ProcessorMeta
		wantErr bool
	}{
		{"missing base_url", ProcessorMeta(`{"salt": "test"}`), true},
		{"missing salt", ProcessorMeta(`{"base_url": "https://example.com"}`), true},
		{"valid", ProcessorMeta(`{"base_url": "https://example.com", "salt": "test"}`), false},
		{"valid with options", ProcessorMeta(`{"base_url": "https://example.com", "salt": "test", "track_only_html": true, "ignore_domains": ["a.com"]}`), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewLinkTracker(tt.meta)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewLinkTracker() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
