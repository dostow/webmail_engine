package processor

import (
	"context"
	"testing"
)

func TestSpamFilter_Process(t *testing.T) {
	meta := ProcessorMeta(`{"threshold": 0.8, "action": "tag"}`)

	filter, err := NewSpamFilter(meta)
	if err != nil {
		t.Fatalf("NewSpamFilter() error = %v", err)
	}

	email := &Email{
		From:    "sender@example.com",
		Subject: "Normal email",
		Body: &EmailBody{
			Text: "Hello, this is a normal email.",
		},
		Headers: make(map[string]string),
	}

	state := NewPipelineState()
	err = filter.Process(context.Background(), email, state)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	// Check spam score was set
	score := email.Headers["X-Spam-Score"]
	if score == "" {
		t.Error("Process() expected X-Spam-Score header to be set")
	}

	// Check state was updated
	if state.GetBool("spam_flagged") {
		t.Error("Process() should not flag normal email as spam")
	}
}

func TestSpamFilter_Blacklist(t *testing.T) {
	meta := ProcessorMeta(`{"threshold": 0.8, "blacklist": ["spammer@example.com"]}`)

	filter, err := NewSpamFilter(meta)
	if err != nil {
		t.Fatalf("NewSpamFilter() error = %v", err)
	}

	email := &Email{
		From:    "spammer@example.com",
		Subject: "You won a million dollars!",
		Body: &EmailBody{
			Text: "Click here to claim your prize!",
		},
		Headers: make(map[string]string),
		Flags:   []string{},
	}

	state := NewPipelineState()
	err = filter.Process(context.Background(), email, state)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	// Check email was marked as spam
	if !state.GetBool("spam_flagged") {
		t.Error("Process() should flag blacklisted sender as spam")
	}

	// Check spam flag was added
	found := false
	for _, flag := range email.Flags {
		if flag == "$Spam" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Process() should add $Spam flag")
	}
}

func TestSpamFilter_Whitelist(t *testing.T) {
	meta := ProcessorMeta(`{"threshold": 0.8, "whitelist": ["trusted@example.com"]}`)

	filter, err := NewSpamFilter(meta)
	if err != nil {
		t.Fatalf("NewSpamFilter() error = %v", err)
	}

	email := &Email{
		From:    "trusted@example.com",
		Subject: "Free money!!!", // Would normally trigger spam
		Body: &EmailBody{
			Text: "This is from a trusted sender.",
		},
		Headers: make(map[string]string),
	}

	state := NewPipelineState()
	err = filter.Process(context.Background(), email, state)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	// Check whitelisted sender was not flagged
	if state.GetBool("spam_flagged") {
		t.Error("Process() should not flag whitelisted sender")
	}
	if !state.GetBool("spam_whitelisted") {
		t.Error("Process() should set spam_whitelisted in state")
	}
}

func TestSpamFilter_AbortOnSpam(t *testing.T) {
	meta := ProcessorMeta(`{"threshold": 0.2, "abort_on_spam": true}`)

	filter, err := NewSpamFilter(meta)
	if err != nil {
		t.Fatalf("NewSpamFilter() error = %v", err)
	}

	email := &Email{
		From:    "sender@example.com",
		Subject: "Congratulations! You won!!!",
		Body: &EmailBody{
			Text: "Click here now to claim your free prize!",
		},
		Headers: make(map[string]string),
	}

	state := NewPipelineState()
	err = filter.Process(context.Background(), email, state)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	// Check pipeline was aborted
	if !state.IsAborted() {
		t.Error("Process() should abort pipeline when abort_on_spam is true")
	}
}

func TestSpamFilterDetector_ShouldProcess(t *testing.T) {
	detector := &SpamFilterDetector{}
	state := NewPipelineState()
	email := &Email{
		Subject: "Test email",
	}

	// First run should process
	shouldRun, err := detector.ShouldProcess(context.Background(), email, state)
	if err != nil {
		t.Fatalf("ShouldProcess() error = %v", err)
	}
	if !shouldRun {
		t.Error("ShouldProcess() should return true on first run")
	}

	// After processor ran, should skip
	state.SetProcessorResult(SpamFilterType, &ProcessorResult{Success: true})
	shouldRun, err = detector.ShouldProcess(context.Background(), email, state)
	if err != nil {
		t.Fatalf("ShouldProcess() error = %v", err)
	}
	if shouldRun {
		t.Error("ShouldProcess() should return false after processor already ran")
	}
}

func TestSpamFilter_CalculateSpamScore(t *testing.T) {
	meta := ProcessorMeta(`{"threshold": 0.8}`)
	filter, _ := NewSpamFilter(meta)
	spamFilter := filter.(*SpamFilter)

	tests := []struct {
		name        string
		subject     string
		body        string
		expectScore float64
	}{
		{
			name:        "normal email",
			subject:     "Meeting tomorrow",
			body:        "Hi, let's meet tomorrow at 10am.",
			expectScore: 0.0,
		},
		{
			name:        "spam subject",
			subject:     "Congratulations winner",
			body:        "Normal body text.",
			expectScore: 0.2, // "congratulations" and "winner"
		},
		{
			name:        "excessive punctuation",
			subject:     "Amazing offer!!!!",
			body:        "Check this out.",
			expectScore: 0.1, // 4 exclamation marks (>3)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			email := &Email{
				Subject: tt.subject,
				Body: &EmailBody{
					Text: tt.body,
				},
			}
			score := spamFilter.calculateSpamScore(email)
			if score != tt.expectScore {
				t.Errorf("calculateSpamScore() = %v, want %v", score, tt.expectScore)
			}
		})
	}
}
