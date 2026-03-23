package scheduler

import (
	"context"
	"sync"
	"time"

	"webmail_engine/internal/models"
)

// Operation types for fair-use tracking
const (
	OpFetch       = "FETCH"
	OpSearch      = "SEARCH"
	OpSend        = "SEND"
	OpList        = "LIST"
	OpRetrieve    = "RETRIEVE"
	OpAttachment  = "ATTACHMENT"
	OpIdle        = "IDLE"
	OpSync        = "SYNC"
)

// Default operation costs
var DefaultOperationCosts = map[string]int{
	OpFetch:       1,
	OpSearch:      5,
	OpSend:        3,
	OpList:        1,
	OpRetrieve:    2,
	OpAttachment:  3,
	OpIdle:        0,
	OpSync:        2,
}

// TokenBucket implements the token bucket algorithm
type TokenBucket struct {
	mu           sync.Mutex
	accountID    string
	tokens       int
	maxTokens    int
	refillRate   int // tokens per minute
	lastRefill   time.Time
	refillTicker *time.Ticker
	stopChan     chan struct{}
}

// FairUseScheduler manages fair-use scheduling for all accounts
type FairUseScheduler struct {
	mu            sync.RWMutex
	buckets       map[string]*TokenBucket
	policies      map[string]*models.FairUsePolicy
	defaultPolicy *models.FairUsePolicy
	queue         *OperationQueue
	stats         *SchedulerStats
}

// SchedulerStats tracks scheduler statistics
type SchedulerStats struct {
	TotalOperations   int64
	QueuedOperations  int64
	ThrottledRequests int64
	LastUpdate        time.Time
}

// QueuedOperation represents an operation waiting in queue
type QueuedOperation struct {
	ID         string
	AccountID  string
	Operation  string
	Cost       int
	Priority   int
	Submitted  time.Time
	Execute    chan struct{}
	Cancel     chan struct{}
	Result     chan OperationResult
}

// OperationResult represents the result of a queued operation
type OperationResult struct {
	Success bool
	Error   error
	Tokens  int
}

// OperationQueue manages queued operations
type OperationQueue struct {
	mu        sync.Mutex
	queues    map[string][]*QueuedOperation
	maxQueue  int
}

// NewOperationQueue creates a new operation queue
func NewOperationQueue(maxQueue int) *OperationQueue {
	return &OperationQueue{
		queues:   make(map[string][]*QueuedOperation),
		maxQueue: maxQueue,
	}
}

// NewFairUseScheduler creates a new fair-use scheduler
func NewFairUseScheduler() *FairUseScheduler {
	scheduler := &FairUseScheduler{
		buckets:  make(map[string]*TokenBucket),
		policies: make(map[string]*models.FairUsePolicy),
		defaultPolicy: &models.FairUsePolicy{
			Enabled:         true,
			TokenBucketSize: 100,
			RefillRate:      10, // 10 tokens per minute
			OperationCosts:  DefaultOperationCosts,
			PriorityLevels:  map[string]int{"low": 1, "normal": 5, "high": 10},
		},
		queue: NewOperationQueue(1000),
		stats: &SchedulerStats{
			LastUpdate: time.Now(),
		},
	}
	
	return scheduler
}

// InitializeAccount initializes fair-use tracking for an account
func (s *FairUseScheduler) InitializeAccount(accountID string, policy *models.FairUsePolicy) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if policy == nil {
		policy = s.defaultPolicy
	}

	s.policies[accountID] = policy

	bucket := &TokenBucket{
		accountID:  accountID,
		tokens:     policy.TokenBucketSize,
		maxTokens:  policy.TokenBucketSize,
		refillRate: policy.RefillRate,
		lastRefill: time.Now(),
		stopChan:   make(chan struct{}),
	}

	// Start refill ticker
	bucket.refillTicker = time.NewTicker(time.Minute / time.Duration(policy.RefillRate))
	go bucket.startRefill(bucket.refillTicker, bucket.stopChan)

	s.buckets[accountID] = bucket
}

// initializeAccountLazy initializes an account with lazy initialization (thread-safe)
// Only initializes if the account doesn't already exist
func (s *FairUseScheduler) initializeAccountLazy(accountID string, policy *models.FairUsePolicy) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring write lock (another goroutine may have initialized)
	if _, exists := s.buckets[accountID]; exists {
		return
	}

	if policy == nil {
		policy = s.defaultPolicy
	}

	s.policies[accountID] = policy

	bucket := &TokenBucket{
		accountID:  accountID,
		tokens:     policy.TokenBucketSize,
		maxTokens:  policy.TokenBucketSize,
		refillRate: policy.RefillRate,
		lastRefill: time.Now(),
		stopChan:   make(chan struct{}),
	}

	// Start refill ticker
	bucket.refillTicker = time.NewTicker(time.Minute / time.Duration(policy.RefillRate))
	go bucket.startRefill(bucket.refillTicker, bucket.stopChan)

	s.buckets[accountID] = bucket
}

// ConsumeTokens attempts to consume tokens for an operation
// If the account is not initialized, it will be lazily initialized with default policy
func (s *FairUseScheduler) ConsumeTokens(accountID, operation string, priority string) (bool, int, error) {
	s.mu.RLock()
	bucket, exists := s.buckets[accountID]
	policy, hasPolicy := s.policies[accountID]
	s.mu.RUnlock()

	// Lazy initialization if bucket doesn't exist
	if !exists {
		s.initializeAccountLazy(accountID, nil)
		s.mu.RLock()
		bucket, _ = s.buckets[accountID]
		policy, hasPolicy = s.policies[accountID]
		s.mu.RUnlock()
	}

	if !hasPolicy {
		policy = s.defaultPolicy
	}

	if !policy.Enabled {
		return true, 0, nil
	}

	// Get operation cost
	cost := policy.OperationCosts[operation]
	if cost == 0 {
		cost = DefaultOperationCosts[operation]
	}

	// Try to consume tokens
	if bucket.tryConsume(cost) {
		s.stats.TotalOperations++
		return true, cost, nil
	}

	// Not enough tokens
	s.stats.ThrottledRequests++
	return false, cost, models.ErrInsufficientTokens
}

// QueueOperation queues an operation for later execution
func (s *FairUseScheduler) QueueOperation(accountID, operation string, priority string) *QueuedOperation {
	s.mu.RLock()
	policy, exists := s.policies[accountID]
	s.mu.RUnlock()
	
	if !exists {
		policy = s.defaultPolicy
	}
	
	cost := policy.OperationCosts[operation]
	if cost == 0 {
		cost = DefaultOperationCosts[operation]
	}
	
	priorityLevel := 5 // default normal
	if policy.PriorityLevels != nil {
		if level, ok := policy.PriorityLevels[priority]; ok {
			priorityLevel = level
		}
	}
	
	op := &QueuedOperation{
		ID:        generateOperationID(),
		AccountID: accountID,
		Operation: operation,
		Cost:      cost,
		Priority:  priorityLevel,
		Submitted: time.Now(),
		Execute:   make(chan struct{}),
		Cancel:    make(chan struct{}),
		Result:    make(chan OperationResult, 1),
	}
	
	s.queue.enqueue(accountID, op)
	s.stats.QueuedOperations++
	
	return op
}

// ReleaseTokens returns tokens to the bucket (for failed operations)
func (s *FairUseScheduler) ReleaseTokens(accountID string, tokens int) {
	s.mu.RLock()
	bucket, exists := s.buckets[accountID]
	s.mu.RUnlock()
	
	if !exists {
		return
	}
	
	bucket.mu.Lock()
	defer bucket.mu.Unlock()
	
	bucket.tokens += tokens
	if bucket.tokens > bucket.maxTokens {
		bucket.tokens = bucket.maxTokens
	}
}

// GetTokenBucket returns the current token bucket state for an account
func (s *FairUseScheduler) GetTokenBucket(accountID string) *models.TokenBucket {
	s.mu.RLock()
	bucket, exists := s.buckets[accountID]
	s.mu.RUnlock()
	
	if !exists {
		return nil
	}
	
	bucket.mu.Lock()
	defer bucket.mu.Unlock()
	
	return &models.TokenBucket{
		AccountID:  bucket.accountID,
		Tokens:     bucket.tokens,
		MaxTokens:  bucket.maxTokens,
		LastRefill: bucket.lastRefill,
		RefillRate: bucket.refillRate,
	}
}

// UpdatePolicy updates the fair-use policy for an account
func (s *FairUseScheduler) UpdatePolicy(accountID string, policy *models.FairUsePolicy) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	s.policies[accountID] = policy
	
	// Update bucket if exists
	if bucket, exists := s.buckets[accountID]; exists {
		bucket.mu.Lock()
		bucket.maxTokens = policy.TokenBucketSize
		bucket.refillRate = policy.RefillRate
		if bucket.tokens > bucket.maxTokens {
			bucket.tokens = bucket.maxTokens
		}
		bucket.mu.Unlock()
	}
}

// RemoveAccount removes fair-use tracking for an account
func (s *FairUseScheduler) RemoveAccount(accountID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if bucket, exists := s.buckets[accountID]; exists {
		close(bucket.stopChan)
		bucket.refillTicker.Stop()
		delete(s.buckets, accountID)
	}
	
	delete(s.policies, accountID)
	s.queue.clear(accountID)
}

// GetStats returns scheduler statistics
func (s *FairUseScheduler) GetStats() *SchedulerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	stats := *s.stats
	stats.LastUpdate = time.Now()
	return &stats
}

// Shutdown gracefully shuts down the scheduler
func (s *FairUseScheduler) Shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	for _, bucket := range s.buckets {
		close(bucket.stopChan)
		bucket.refillTicker.Stop()
	}
	
	s.buckets = make(map[string]*TokenBucket)
}

// TokenBucket methods

func (b *TokenBucket) tryConsume(cost int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	if b.tokens >= cost {
		b.tokens -= cost
		return true
	}
	return false
}

func (b *TokenBucket) startRefill(ticker *time.Ticker, stopChan chan struct{}) {
	for {
		select {
		case <-ticker.C:
			b.mu.Lock()
			if b.tokens < b.maxTokens {
				b.tokens++
			}
			b.lastRefill = time.Now()
			b.mu.Unlock()
		case <-stopChan:
			return
		}
	}
}

// OperationQueue methods

func (q *OperationQueue) enqueue(accountID string, op *QueuedOperation) {
	q.mu.Lock()
	defer q.mu.Unlock()
	
	queue := q.queues[accountID]
	if len(queue) >= q.maxQueue {
		// Queue full, reject operation
		op.Result <- OperationResult{
			Success: false,
			Error:   models.NewCapacityError(),
		}
		return
	}
	
	q.queues[accountID] = append(queue, op)
}

func (q *OperationQueue) dequeue(accountID string) *QueuedOperation {
	q.mu.Lock()
	defer q.mu.Unlock()
	
	queue := q.queues[accountID]
	if len(queue) == 0 {
		return nil
	}
	
	op := queue[0]
	q.queues[accountID] = queue[1:]
	
	return op
}

func (q *OperationQueue) clear(accountID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	
	delete(q.queues, accountID)
}

func (q *OperationQueue) len(accountID string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	
	return len(q.queues[accountID])
}

// ProcessQueue processes queued operations for an account
func (s *FairUseScheduler) ProcessQueue(ctx context.Context, accountID string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			op := s.queue.dequeue(accountID)
			if op == nil {
				return
			}

			// Wait for tokens
			for {
				success, _, _ := s.ConsumeTokens(accountID, op.Operation, "normal")
				if success {
					op.Execute <- struct{}{}
					break
				}

				// Check for cancellation
				select {
				case <-op.Cancel:
					op.Result <- OperationResult{
						Success: false,
						Error:   context.Canceled,
					}
					return
				case <-time.After(time.Second):
					// Try again
				}
			}
			
			// Wait for result
			select {
			case result := <-op.Result:
				if !result.Success {
					// Release tokens back
					s.ReleaseTokens(accountID, op.Cost)
				}
				s.stats.QueuedOperations--
			case <-ctx.Done():
				return
			}
		}
	}
}

// Utility functions

func generateOperationID() string {
	return time.Now().Format("20060102150405.000000000")
}

// WaitForTokens waits until tokens are available or context is done
func (s *FairUseScheduler) WaitForTokens(ctx context.Context, accountID string, cost int) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			success, _, _ := s.ConsumeTokens(accountID, "WAIT", "normal")
			if success {
				// Release the tokens we just consumed for checking
				s.ReleaseTokens(accountID, 1)
				
				// Now try to consume the actual cost
				success, _, err := s.ConsumeTokens(accountID, "ACTUAL", "normal")
				if success {
					return nil
				}
				if err != models.ErrInsufficientTokens {
					return err
				}
			}
		}
	}
}

// EstimateWaitTime estimates how long until tokens are available
func (s *FairUseScheduler) EstimateWaitTime(accountID string, cost int) time.Duration {
	s.mu.RLock()
	bucket, exists := s.buckets[accountID]
	policy, hasPolicy := s.policies[accountID]
	s.mu.RUnlock()
	
	if !exists {
		return 0
	}
	
	if !hasPolicy {
		policy = s.defaultPolicy
	}
	
	bucket.mu.Lock()
	defer bucket.mu.Unlock()
	
	if bucket.tokens >= cost {
		return 0
	}
	
	tokensNeeded := cost - bucket.tokens
	refillsNeeded := tokensNeeded / policy.RefillRate
	
	return time.Duration(refillsNeeded) * time.Minute
}

// CanExecute checks if an operation can be executed immediately
func (s *FairUseScheduler) CanExecute(accountID, operation string) bool {
	success, _, _ := s.ConsumeTokens(accountID, operation, "normal")
	if success {
		// Release tokens since we're just checking
		s.ReleaseTokens(accountID, 1)
		return true
	}
	return false
}
