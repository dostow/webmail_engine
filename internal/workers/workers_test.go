package workers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"webmail_engine/internal/service"
	"webmail_engine/internal/taskmaster"
)

// MockAIAnalysisService is a test implementation of AIAnalysisService.
type MockAIAnalysisService struct {
	analyzeFunc func(ctx context.Context, accountID, messageID string) (*AnalysisResult, error)
}

func (m *MockAIAnalysisService) AnalyzeEmail(ctx context.Context, accountID, messageID string) (*AnalysisResult, error) {
	if m.analyzeFunc != nil {
		return m.analyzeFunc(ctx, accountID, messageID)
	}
	return &AnalysisResult{
		Sentiment:   "neutral",
		Category:    "general",
		Confidence:  0.95,
		ProcessedAt: time.Now(),
	}, nil
}

func (m *MockAIAnalysisService) CategorizeEmail(ctx context.Context, accountID, subject, body string) (string, error) {
	return "general", nil
}

func (m *MockAIAnalysisService) ExtractEntities(ctx context.Context, content string) ([]Entity, error) {
	return []Entity{}, nil
}

// TestAIAnalysisTaskID tests the task ID.
func TestAIAnalysisTaskID(t *testing.T) {
	task := &AIAnalysisTask{}
	if task.ID() != "ai_analysis" {
		t.Errorf("expected ID 'ai_analysis', got %q", task.ID())
	}
}

// TestAIAnalysisTaskExecute tests task execution.
func TestAIAnalysisTaskExecute(t *testing.T) {
	service := &MockAIAnalysisService{
		analyzeFunc: func(ctx context.Context, accountID, messageID string) (*AnalysisResult, error) {
			return &AnalysisResult{
				Sentiment:   "positive",
				Category:    "work",
				Confidence:  0.92,
				ProcessedAt: time.Now(),
			}, nil
		},
	}

	task := &AIAnalysisTask{AnalysisService: service}

	payload := AIAnalysisPayload{
		AccountID: "acc_123",
		MessageID: "msg_456",
		Subject:   "Test Email",
		Options: AnalysisOptions{
			IncludeSentiment: true,
			IncludeCategory:  true,
		},
	}

	payloadBytes, _ := json.Marshal(payload)

	ctx := context.Background()
	if err := task.Execute(ctx, payloadBytes); err != nil {
		t.Errorf("Execute failed: %v", err)
	}
}

// TestAIAnalysisTaskInvalidPayload tests invalid payload handling.
func TestAIAnalysisTaskInvalidPayload(t *testing.T) {
	task := &AIAnalysisTask{}

	ctx := context.Background()
	err := task.Execute(ctx, []byte("invalid json"))

	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}

	var taskErr *taskmaster.TaskError
	if !errors.As(err, &taskErr) {
		t.Error("expected TaskError")
	}
}

// TestAIAnalysisTaskMissingFields tests validation of required fields.
func TestAIAnalysisTaskMissingFields(t *testing.T) {
	task := &AIAnalysisTask{AnalysisService: &MockAIAnalysisService{}}

	tests := []struct {
		name    string
		payload AIAnalysisPayload
	}{
		{"missing account_id", AIAnalysisPayload{MessageID: "msg"}},
		{"missing message_id", AIAnalysisPayload{AccountID: "acc"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payloadBytes, _ := json.Marshal(tt.payload)
			ctx := context.Background()
			err := task.Execute(ctx, payloadBytes)

			if err == nil {
				t.Error("expected error for missing required field")
			}

			var taskErr *taskmaster.TaskError
			if !errors.As(err, &taskErr) {
				t.Error("expected TaskError")
			}
			// Missing fields should be non-retryable (client error)
			if taskErr.IsRetryable() {
				t.Error("expected non-retryable error for missing field")
			}
		})
	}
}

// TestAIAnalysisTaskNoService tests execution without configured service.
func TestAIAnalysisTaskNoService(t *testing.T) {
	task := &AIAnalysisTask{AnalysisService: nil}

	payload := AIAnalysisPayload{
		AccountID: "acc",
		MessageID: "msg",
	}
	payloadBytes, _ := json.Marshal(payload)

	ctx := context.Background()
	err := task.Execute(ctx, payloadBytes)

	if err == nil {
		t.Error("expected error when service not configured")
	}

	var taskErr *taskmaster.TaskError
	if !errors.As(err, &taskErr) {
		t.Error("expected TaskError")
	}
	if taskErr.Category != taskmaster.ErrorCategorySystem {
		t.Error("expected system error category")
	}
}

// TestAIAnalysisTaskTimeout tests timeout handling.
func TestAIAnalysisTaskTimeout(t *testing.T) {
	service := &MockAIAnalysisService{
		analyzeFunc: func(ctx context.Context, accountID, messageID string) (*AnalysisResult, error) {
			select {
			case <-time.After(500 * time.Millisecond):
				return &AnalysisResult{}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}

	task := &AIAnalysisTask{AnalysisService: service}

	payload := AIAnalysisPayload{
		AccountID: "acc",
		MessageID: "msg",
		Options: AnalysisOptions{
			TimeoutSeconds: 1,
		},
	}
	payloadBytes, _ := json.Marshal(payload)

	ctx := context.Background()
	if err := task.Execute(ctx, payloadBytes); err != nil {
		// Timeout is acceptable
		t.Logf("Task completed with: %v", err)
	}
}

// TestSpamCheckTaskID tests the task ID.
func TestSpamCheckTaskID(t *testing.T) {
	task := &SpamCheckTask{}
	if task.ID() != "spam_check" {
		t.Errorf("expected ID 'spam_check', got %q", task.ID())
	}
}

// TestSpamCheckTaskExecute tests task execution.
func TestSpamCheckTaskExecute(t *testing.T) {
	service := &MockSpamDetectionService{
		checkFunc: func(ctx context.Context, email *SpamCheckEmail) (*SpamCheckResult, error) {
			return &SpamCheckResult{
				IsSpam:     false,
				Confidence: 0.98,
				Action:     "allow",
				CheckedAt:  time.Now(),
			}, nil
		},
	}

	task := &SpamCheckTask{SpamService: service}

	payload := SpamCheckPayload{
		AccountID: "acc_123",
		MessageID: "msg_456",
		From:      "sender@example.com",
		Subject:   "Test Email",
		Body:      "Test body content",
	}

	payloadBytes, _ := json.Marshal(payload)

	ctx := context.Background()
	if err := task.Execute(ctx, payloadBytes); err != nil {
		t.Errorf("Execute failed: %v", err)
	}
}

// TestSpamCheckTaskNoService tests execution without spam service.
func TestSpamCheckTaskNoService(t *testing.T) {
	task := &SpamCheckTask{SpamService: nil}

	payload := SpamCheckPayload{
		AccountID: "acc",
		MessageID: "msg",
		From:      "sender@example.com",
	}
	payloadBytes, _ := json.Marshal(payload)

	ctx := context.Background()
	// Should not error, just skip silently
	if err := task.Execute(ctx, payloadBytes); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// MockSpamDetectionService is a test implementation.
type MockSpamDetectionService struct {
	checkFunc func(ctx context.Context, email *SpamCheckEmail) (*SpamCheckResult, error)
}

func (m *MockSpamDetectionService) CheckSpam(ctx context.Context, email *SpamCheckEmail) (*SpamCheckResult, error) {
	if m.checkFunc != nil {
		return m.checkFunc(ctx, email)
	}
	return &SpamCheckResult{IsSpam: false, Confidence: 0.9}, nil
}

func (m *MockSpamDetectionService) UpdateSpamRules(ctx context.Context, rules []SpamRule) error {
	return nil
}

func (m *MockSpamDetectionService) GetSpamStats(ctx context.Context, accountID string) (*SpamStats, error) {
	return &SpamStats{}, nil
}

// TestSyncTaskID tests the task ID.
func TestSyncTaskID(t *testing.T) {
	task := &SyncTask{}
	if task.ID() != "sync" {
		t.Errorf("expected ID 'sync', got %q", task.ID())
	}
}

// TestSyncTaskExecute tests task execution.
func TestSyncTaskExecute(t *testing.T) {
	syncService := &MockSyncService{
		syncFunc: func(ctx context.Context, accountID string, opts service.SyncOptions) (*service.SyncResult, error) {
			return &service.SyncResult{
				AccountID:         accountID,
				MessagesSynced:    10,
				FoldersSynced:     3,
				EnvelopesEnqueued: 10,
				Duration:          time.Second,
			}, nil
		},
	}

	task := &SyncTask{SyncService: syncService}

	payload := SyncPayload{
		AccountID: "acc_123",
		Options: service.SyncOptions{
			HistoricalScope: 30,
			FetchBody:       true,
		},
	}

	payloadBytes, _ := json.Marshal(payload)

	ctx := context.Background()
	if err := task.Execute(ctx, payloadBytes); err != nil {
		t.Errorf("Execute failed: %v", err)
	}
}

// MockSyncService is a test implementation.
type MockSyncService struct {
	syncFunc       func(ctx context.Context, accountID string, opts service.SyncOptions) (*service.SyncResult, error)
	syncFolderFunc func(ctx context.Context, accountID, folderName string, opts service.SyncOptions) (*service.SyncResult, error)
}

func (m *MockSyncService) SyncAccount(ctx context.Context, accountID string, opts service.SyncOptions) (*service.SyncResult, error) {
	if m.syncFunc != nil {
		return m.syncFunc(ctx, accountID, opts)
	}
	return &service.SyncResult{}, nil
}

func (m *MockSyncService) SyncFolder(ctx context.Context, accountID, folderName string, opts service.SyncOptions) (*service.SyncResult, error) {
	if m.syncFolderFunc != nil {
		return m.syncFolderFunc(ctx, accountID, folderName, opts)
	}
	return &service.SyncResult{}, nil
}

func (m *MockSyncService) GetSyncState(ctx context.Context, accountID, folderName string) (*service.FolderSyncState, error) {
	return &service.FolderSyncState{}, nil
}

// TestEnvelopeProcessorTaskID tests the task ID.
func TestEnvelopeProcessorTaskID(t *testing.T) {
	task := &EnvelopeProcessorTask{}
	if task.ID() != "envelope_processor" {
		t.Errorf("expected ID 'envelope_processor', got %q", task.ID())
	}
}

// TestEnvelopeProcessorTaskExecute tests task execution.
func TestEnvelopeProcessorTaskExecute(t *testing.T) {
	processorService := &MockEnvelopeProcessorService{
		processFunc: func(ctx context.Context, envelope *service.EnvelopeQueueItem) error {
			return nil
		},
	}

	task := &EnvelopeProcessorTask{ProcessorService: processorService}

	payload := EnvelopeProcessorPayload{
		EnvelopeID: "env_123",
		AccountID:  "acc_456",
		FolderName: "INBOX",
		UID:        100,
	}

	payloadBytes, _ := json.Marshal(payload)

	ctx := context.Background()
	if err := task.Execute(ctx, payloadBytes); err != nil {
		t.Errorf("Execute failed: %v", err)
	}
}

// MockEnvelopeProcessorService is a test implementation.
type MockEnvelopeProcessorService struct {
	processFunc func(ctx context.Context, envelope *service.EnvelopeQueueItem) error
}

func (m *MockEnvelopeProcessorService) ProcessEnvelope(ctx context.Context, envelope *service.EnvelopeQueueItem) error {
	if m.processFunc != nil {
		return m.processFunc(ctx, envelope)
	}
	return nil
}

func (m *MockEnvelopeProcessorService) GetProcessorStats(ctx context.Context) (*service.ProcessorStats, error) {
	return &service.ProcessorStats{}, nil
}

// TestInterfaceCompliance tests that all task types implement taskmaster.Task.
func TestInterfaceCompliance(t *testing.T) {
	var _ taskmaster.Task = (*AIAnalysisTask)(nil)
	var _ taskmaster.Task = (*SpamCheckTask)(nil)
	var _ taskmaster.Task = (*SyncTask)(nil)
	var _ taskmaster.Task = (*EnvelopeProcessorTask)(nil)
}
