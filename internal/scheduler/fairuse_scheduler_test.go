package scheduler

import (
	"testing"
	"time"

	"webmail_engine/internal/models"
)

func TestFairUseScheduler_InitializeAccount(t *testing.T) {
	scheduler := NewFairUseScheduler()
	defer scheduler.Shutdown()

	accountID := "test_account"
	scheduler.InitializeAccount(accountID, nil)

	bucket := scheduler.GetTokenBucket(accountID)
	if bucket == nil {
		t.Fatal("Expected token bucket to be initialized")
	}

	if bucket.Tokens != 100 {
		t.Errorf("Expected 100 tokens, got %d", bucket.Tokens)
	}

	if bucket.MaxTokens != 100 {
		t.Errorf("Expected 100 max tokens, got %d", bucket.MaxTokens)
	}
}

func TestFairUseScheduler_ConsumeTokens(t *testing.T) {
	scheduler := NewFairUseScheduler()
	defer scheduler.Shutdown()

	accountID := "test_account"
	scheduler.InitializeAccount(accountID, nil)

	// Consume some tokens
	success, cost, err := scheduler.ConsumeTokens(accountID, "FETCH", "normal")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !success {
		t.Error("Expected to successfully consume tokens")
	}
	if cost != 1 {
		t.Errorf("Expected cost of 1, got %d", cost)
	}

	bucket := scheduler.GetTokenBucket(accountID)
	if bucket.Tokens != 99 {
		t.Errorf("Expected 99 tokens after consumption, got %d", bucket.Tokens)
	}
}

func TestFairUseScheduler_InsufficientTokens(t *testing.T) {
	scheduler := NewFairUseScheduler()
	defer scheduler.Shutdown()

	accountID := "test_account"
	policy := &models.FairUsePolicy{
		Enabled:         true,
		TokenBucketSize: 5,
		RefillRate:      1,
		OperationCosts:  DefaultOperationCosts,
	}
	scheduler.InitializeAccount(accountID, policy)

	// Consume all tokens
	for i := 0; i < 5; i++ {
		scheduler.ConsumeTokens(accountID, "FETCH", "normal")
	}

	// Try to consume more
	success, _, err := scheduler.ConsumeTokens(accountID, "SEARCH", "normal")
	if success {
		t.Error("Expected to fail due to insufficient tokens")
	}
	if err == nil {
		t.Error("Expected error for insufficient tokens")
	}
}

func TestTokenBucket_Refill(t *testing.T) {
	bucket := &TokenBucket{
		accountID:  "test",
		tokens:     0,
		maxTokens:  10,
		refillRate: 1,
		lastRefill: time.Now(),
		stopChan:   make(chan struct{}),
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	go bucket.startRefill(ticker, bucket.stopChan)

	// Wait for refill
	time.Sleep(150 * time.Millisecond)

	bucket.mu.Lock()
	tokens := bucket.tokens
	bucket.mu.Unlock()

	if tokens < 1 {
		t.Errorf("Expected at least 1 token after refill, got %d", tokens)
	}

	close(bucket.stopChan)
}
