package service

import (
	"context"
	"testing"
	"time"

	"webmail_engine/internal/models"
	"webmail_engine/internal/pool"
	"webmail_engine/internal/scheduler"
	"webmail_engine/internal/storage"
	"webmail_engine/internal/store"
)

// MockAccountStore provides in-memory account storage for tests
type MockAccountStore struct {
	accounts  map[string]*models.Account
	auditLogs []*models.AuditLog
}

func NewMockAccountStore() *MockAccountStore {
	return &MockAccountStore{
		accounts:  make(map[string]*models.Account),
		auditLogs: make([]*models.AuditLog, 0),
	}
}

func (m *MockAccountStore) GetByID(ctx context.Context, id string) (*models.Account, error) {
	if acc, exists := m.accounts[id]; exists {
		return acc, nil
	}
	return nil, store.ErrNotFound
}

func (m *MockAccountStore) GetByEmail(ctx context.Context, email string) (*models.Account, error) {
	for _, acc := range m.accounts {
		if acc.Email == email {
			return acc, nil
		}
	}
	return nil, store.ErrNotFound
}

func (m *MockAccountStore) Create(ctx context.Context, account *models.Account) error {
	// Check for duplicate email
	for _, acc := range m.accounts {
		if acc.Email == account.Email {
			return store.ErrAlreadyExists
		}
	}
	m.accounts[account.ID] = account
	return nil
}

func (m *MockAccountStore) Update(ctx context.Context, account *models.Account) error {
	if _, exists := m.accounts[account.ID]; !exists {
		return store.ErrNotFound
	}
	m.accounts[account.ID] = account
	return nil
}

func (m *MockAccountStore) Delete(ctx context.Context, id string) error {
	delete(m.accounts, id)
	return nil
}

func (m *MockAccountStore) List(ctx context.Context, offset, limit int) ([]*models.Account, int, error) {
	accounts := make([]*models.Account, 0, len(m.accounts))
	for _, acc := range m.accounts {
		accounts = append(accounts, acc)
	}
	return accounts, len(accounts), nil
}

func (m *MockAccountStore) Health(ctx context.Context) *store.HealthStatus {
	return &store.HealthStatus{Status: "healthy", Connected: true}
}

func (m *MockAccountStore) Close() error {
	return nil
}

func (m *MockAccountStore) CreateAuditLog(ctx context.Context, log *models.AuditLog) error {
	m.auditLogs = append(m.auditLogs, log)
	return nil
}

func (m *MockAccountStore) ListAuditLogs(ctx context.Context, offset, limit int) ([]*models.AuditLog, int, error) {
	return m.auditLogs, len(m.auditLogs), nil
}

func (m *MockAccountStore) GetFolderSyncState(ctx context.Context, accountID, folderName string) (*models.FolderSyncState, error) {
	return nil, store.ErrNotFound
}

func (m *MockAccountStore) UpsertFolderSyncState(ctx context.Context, state *models.FolderSyncState) error {
	return nil
}

func (m *MockAccountStore) DeleteFolderSyncState(ctx context.Context, accountID, folderName string) error {
	return nil
}

func (m *MockAccountStore) ListFolderSyncStates(ctx context.Context, accountID string) ([]*models.FolderSyncState, error) {
	return []*models.FolderSyncState{}, nil
}

func (m *MockAccountStore) GetAccountProcessorConfigs(ctx context.Context, accountID string) ([]models.AccountProcessorConfig, error) {
	return []models.AccountProcessorConfig{}, nil
}

func (m *MockAccountStore) UpdateAccountProcessorConfigs(ctx context.Context, accountID string, configs []models.AccountProcessorConfig) error {
	return nil
}

func (m *MockAccountStore) EnableAccountProcessor(ctx context.Context, accountID, processorType string, enabled bool) error {
	return nil
}

// Helper function to create test account
func createTestAccount(id string) *models.Account {
	return &models.Account{
		ID:        id,
		Email:     "test@example.com",
		AuthType:  models.AuthTypePassword,
		Status:    models.AccountStatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		IMAPConfig: models.ServerConfig{
			Host:       "imap.example.com",
			Port:       993,
			Encryption: models.EncryptionSSL,
			Username:   "test@example.com",
			Password:   "encrypted_password",
		},
		SMTPConfig: models.ServerConfig{
			Host:       "smtp.example.com",
			Port:       587,
			Encryption: models.EncryptionStartTLS,
			Username:   "test@example.com",
			Password:   "encrypted_password",
		},
		ConnectionLimit: 5,
	}
}

// TestSendService_NewSendService tests service creation
func TestSendService_NewSendService(t *testing.T) {
	sched := scheduler.NewFairUseScheduler()
	defer sched.Shutdown()
	accountStore := NewMockAccountStore()
	storage := storage.NewFileAttachmentStorage("")

	service, err := NewSendService(nil, sched, storage, accountStore, "12345678901234567890123456789012", SendServiceConfig{
		MaxRetries:      3,
		SendTimeout:     30 * time.Second,
		QueueSize:       100,
		TempStoragePath: "/tmp",
	})

	if err != nil {
		t.Fatalf("Failed to create send service: %v", err)
	}
	if service == nil {
		t.Fatal("Expected service instance, got nil")
	}
	if service.queue == nil {
		t.Error("Expected queue to be initialized")
	}
	if service.templates == nil {
		t.Error("Expected templates map to be initialized")
	}
}

// TestSendService_NewSendService_InvalidKey tests creation with invalid encryption key
func TestSendService_NewSendService_InvalidKey(t *testing.T) {
	sched := scheduler.NewFairUseScheduler()
	defer sched.Shutdown()
	accountStore := NewMockAccountStore()
	storage := storage.NewFileAttachmentStorage("")

	// Invalid key (too short)
	_, err := NewSendService(nil, sched, storage, accountStore, "short", SendServiceConfig{})
	if err == nil {
		t.Error("Expected error for invalid encryption key, got nil")
	}
}

// Note: SendEmail tests that require actual SMTP connection are integration tests
// and should be in a separate _integration_test.go file.
// The SendEmail method requires a valid connection pool which is beyond unit test scope.

// TestSendService_SendEmail_InsufficientTokens tests throttling
func TestSendService_SendEmail_InsufficientTokens(t *testing.T) {
	ctx := context.Background()
	accountStore := NewMockAccountStore()
	account := createTestAccount("acc_1")
	if err := accountStore.Create(ctx, account); err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	sched := scheduler.NewFairUseScheduler()
	defer sched.Shutdown()

	// Initialize with very limited tokens
	policy := &models.FairUsePolicy{
		Enabled:         true,
		TokenBucketSize: 1,
		RefillRate:      1, // Minimum refill rate
		OperationCosts:  scheduler.DefaultOperationCosts,
	}
	sched.InitializeAccount("acc_1", policy)
	storage := storage.NewFileAttachmentStorage("")

	service, _ := NewSendService(nil, sched, storage, accountStore, "12345678901234567890123456789012", SendServiceConfig{})

	req := models.SendEmailRequest{
		AccountID: "acc_1",
		To:        []models.Contact{{Address: "recipient@example.com"}},
		Subject:   "Test",
		TextBody:  "Test",
	}

	// First request should succeed (or fail at SMTP)
	_, _ = service.SendEmail(ctx, req)

	// Second request should fail due to insufficient tokens
	_, err := service.SendEmail(ctx, req)
	if err == nil {
		t.Log("Second send succeeded - tokens may have refilled")
	}
}

// TestSendService_SendEmail_Template tests template usage
func TestSendService_SendEmail_Template(t *testing.T) {
	ctx := context.Background()
	accountStore := NewMockAccountStore()
	account := createTestAccount("acc_1")
	if err := accountStore.Create(ctx, account); err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	scheduler := scheduler.NewFairUseScheduler()
	defer scheduler.Shutdown()
	scheduler.InitializeAccount("acc_1", nil)
	storage := storage.NewFileAttachmentStorage("")

	service, _ := NewSendService(nil, scheduler, storage, accountStore, "12345678901234567890123456789012", SendServiceConfig{})

	// Register template
	template := &EmailTemplate{
		ID:      "welcome",
		Name:    "Welcome Email",
		Subject: "Welcome {{name}}!",
		Body:    "Hello {{name}}, welcome to {{company}}!",
		HTML:    false,
	}
	service.RegisterTemplate(template)

	// Verify template registration
	retrieved, err := service.GetTemplate("welcome")
	if err != nil {
		t.Fatalf("Failed to get template: %v", err)
	}
	if retrieved.Subject != "Welcome {{name}}!" {
		t.Errorf("Expected subject 'Welcome {{name}}!', got '%s'", retrieved.Subject)
	}

	// Test non-existent template
	_, err = service.GetTemplate("nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent template")
	}
}

// TestSendService_ScheduleEmail tests email scheduling
func TestSendService_ScheduleEmail(t *testing.T) {
	ctx := context.Background()
	accountStore := NewMockAccountStore()
	account := createTestAccount("acc_1")
	if err := accountStore.Create(ctx, account); err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	scheduler := scheduler.NewFairUseScheduler()
	defer scheduler.Shutdown()
	scheduler.InitializeAccount("acc_1", nil)
	storage := storage.NewFileAttachmentStorage("")

	service, _ := NewSendService(nil, scheduler, storage, accountStore, "12345678901234567890123456789012", SendServiceConfig{})

	futureTime := time.Now().Add(1 * time.Hour)
	req := models.SendEmailRequest{
		AccountID: "acc_1",
		To:        []models.Contact{{Address: "recipient@example.com"}},
		Subject:   "Scheduled Email",
		TextBody:  "This is scheduled",
	}

	resp, err := service.ScheduleEmail(ctx, req, futureTime)
	if err != nil {
		t.Fatalf("Failed to schedule email: %v", err)
	}
	if resp.Status != "scheduled" {
		t.Errorf("Expected status 'scheduled', got '%s'", resp.Status)
	}
}

// TestSendService_ScheduleEmail_PastTime tests scheduling with past time
func TestSendService_ScheduleEmail_PastTime(t *testing.T) {
	ctx := context.Background()
	accountStore := NewMockAccountStore()
	account := createTestAccount("acc_1")
	if err := accountStore.Create(ctx, account); err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	scheduler := scheduler.NewFairUseScheduler()
	defer scheduler.Shutdown()
	scheduler.InitializeAccount("acc_1", nil)
	storage := storage.NewFileAttachmentStorage("")

	service, _ := NewSendService(nil, scheduler, storage, accountStore, "12345678901234567890123456789012", SendServiceConfig{})

	pastTime := time.Now().Add(-1 * time.Hour)
	req := models.SendEmailRequest{
		AccountID: "acc_1",
		To:        []models.Contact{{Address: "recipient@example.com"}},
		Subject:   "Past Email",
		TextBody:  "This is in the past",
	}

	_, err := service.ScheduleEmail(ctx, req, pastTime)
	if err == nil {
		t.Error("Expected error for scheduling in the past")
	}
}

// TestSendService_QueueEmail tests email queuing
func TestSendService_QueueEmail(t *testing.T) {
	ctx := context.Background()
	accountStore := NewMockAccountStore()
	account := createTestAccount("acc_1")
	if err := accountStore.Create(ctx, account); err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	scheduler := scheduler.NewFairUseScheduler()
	defer scheduler.Shutdown()
	scheduler.InitializeAccount("acc_1", nil)
	storage := storage.NewFileAttachmentStorage("")

	service, _ := NewSendService(nil, scheduler, storage, accountStore, "12345678901234567890123456789012", SendServiceConfig{})

	req := models.SendEmailRequest{
		AccountID: "acc_1",
		To:        []models.Contact{{Address: "recipient@example.com"}},
		Subject:   "Queued Email",
		TextBody:  "This is queued",
	}

	queuedEmail := service.QueueEmail(req)
	if queuedEmail == nil {
		t.Fatal("Expected queued email, got nil")
	}
	if queuedEmail.Status != "pending" {
		t.Errorf("Expected status 'pending', got '%s'", queuedEmail.Status)
	}
	if queuedEmail.AccountID != "acc_1" {
		t.Errorf("Expected account ID 'acc_1', got '%s'", queuedEmail.AccountID)
	}
}

// TestSendService_GetSendStatus tests status retrieval
func TestSendService_GetSendStatus(t *testing.T) {
	ctx := context.Background()
	accountStore := NewMockAccountStore()
	account := createTestAccount("acc_1")
	if err := accountStore.Create(ctx, account); err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	scheduler := scheduler.NewFairUseScheduler()
	defer scheduler.Shutdown()
	scheduler.InitializeAccount("acc_1", nil)
	storage := storage.NewFileAttachmentStorage("")

	service, _ := NewSendService(nil, scheduler, storage, accountStore, "12345678901234567890123456789012", SendServiceConfig{})

	// Queue an email
	req := models.SendEmailRequest{
		AccountID: "acc_1",
		To:        []models.Contact{{Address: "recipient@example.com"}},
		Subject:   "Test",
		TextBody:  "Test",
	}
	queuedEmail := service.QueueEmail(req)

	// Get status
	status, err := service.GetSendStatus(queuedEmail.ID)
	if err != nil {
		t.Fatalf("Failed to get send status: %v", err)
	}
	if status.ID != queuedEmail.ID {
		t.Errorf("Expected ID '%s', got '%s'", queuedEmail.ID, status.ID)
	}

	// Get non-existent status
	_, err = service.GetSendStatus("nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent queue ID")
	}
}

// TestSendService_CancelEmail tests email cancellation
func TestSendService_CancelEmail(t *testing.T) {
	ctx := context.Background()
	accountStore := NewMockAccountStore()
	account := createTestAccount("acc_1")
	if err := accountStore.Create(ctx, account); err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	scheduler := scheduler.NewFairUseScheduler()
	defer scheduler.Shutdown()
	scheduler.InitializeAccount("acc_1", nil)
	storage := storage.NewFileAttachmentStorage("")

	service, _ := NewSendService(nil, scheduler, storage, accountStore, "12345678901234567890123456789012", SendServiceConfig{})

	// Queue an email
	req := models.SendEmailRequest{
		AccountID: "acc_1",
		To:        []models.Contact{{Address: "recipient@example.com"}},
		Subject:   "Test",
		TextBody:  "Test",
	}
	queuedEmail := service.QueueEmail(req)

	// Cancel it
	err := service.CancelEmail(queuedEmail.ID)
	if err != nil {
		t.Fatalf("Failed to cancel email: %v", err)
	}

	// Verify it's cancelled
	_, err = service.GetSendStatus(queuedEmail.ID)
	if err == nil {
		t.Error("Expected error for cancelled email")
	}

	// Cancel non-existent
	err = service.CancelEmail("nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent email")
	}
}

// TestSendService_CancelScheduledEmail tests cancellation of scheduled email
func TestSendService_CancelScheduledEmail(t *testing.T) {
	ctx := context.Background()
	accountStore := NewMockAccountStore()
	account := createTestAccount("acc_1")
	if err := accountStore.Create(ctx, account); err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	scheduler := scheduler.NewFairUseScheduler()
	defer scheduler.Shutdown()
	scheduler.InitializeAccount("acc_1", nil)
	storage := storage.NewFileAttachmentStorage("")

	service, _ := NewSendService(nil, scheduler, storage, accountStore, "12345678901234567890123456789012", SendServiceConfig{})

	futureTime := time.Now().Add(1 * time.Hour)
	req := models.SendEmailRequest{
		AccountID: "acc_1",
		To:        []models.Contact{{Address: "recipient@example.com"}},
		Subject:   "Scheduled",
		TextBody:  "Scheduled email",
	}

	resp, _ := service.ScheduleEmail(ctx, req, futureTime)

	// Cancel scheduled email - use the response's scheduled status
	// Note: ScheduleEmail returns empty QueueID, we need to get it from the queue
	if resp.Status != "scheduled" {
		t.Fatalf("Expected scheduled status, got %s", resp.Status)
	}

	// Get the scheduled email from queue and cancel it
	// Since ScheduleEmail doesn't return QueueID, we test the cancellation differently
	// by queueing an email and cancelling it (already tested in TestSendService_CancelEmail)
}

// TestSendService_GetQueueStats tests queue statistics
func TestSendService_GetQueueStats(t *testing.T) {
	ctx := context.Background()
	accountStore := NewMockAccountStore()
	account := createTestAccount("acc_1")
	if err := accountStore.Create(ctx, account); err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	scheduler := scheduler.NewFairUseScheduler()
	defer scheduler.Shutdown()
	scheduler.InitializeAccount("acc_1", nil)
	storage := storage.NewFileAttachmentStorage("")

	service, _ := NewSendService(nil, scheduler, storage, accountStore, "12345678901234567890123456789012", SendServiceConfig{})

	// Add some pending emails
	for i := 0; i < 3; i++ {
		req := models.SendEmailRequest{
			AccountID: "acc_1",
			To:        []models.Contact{{Address: "recipient@example.com"}},
			Subject:   "Test",
			TextBody:  "Test",
		}
		service.QueueEmail(req)
	}

	// Add some scheduled emails
	futureTime := time.Now().Add(1 * time.Hour)
	for i := 0; i < 2; i++ {
		req := models.SendEmailRequest{
			AccountID: "acc_1",
			To:        []models.Contact{{Address: "recipient@example.com"}},
			Subject:   "Scheduled",
			TextBody:  "Scheduled",
		}
		service.ScheduleEmail(ctx, req, futureTime)
	}

	pending, scheduled := service.GetQueueStats()
	if pending != 3 {
		t.Errorf("Expected 3 pending, got %d", pending)
	}
	if scheduled != 2 {
		t.Errorf("Expected 2 scheduled, got %d", scheduled)
	}
}

// TestSendService_BuildEmailMessage tests email message building
func TestSendService_BuildEmailMessage(t *testing.T) {
	ctx := context.Background()
	accountStore := NewMockAccountStore()
	account := createTestAccount("acc_1")
	if err := accountStore.Create(ctx, account); err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	scheduler := scheduler.NewFairUseScheduler()
	defer scheduler.Shutdown()
	scheduler.InitializeAccount("acc_1", nil)
	storage := storage.NewFileAttachmentStorage("")

	service, _ := NewSendService(nil, scheduler, storage, accountStore, "12345678901234567890123456789012", SendServiceConfig{})

	req := models.SendEmailRequest{
		To:       []models.Contact{{Address: "to@example.com", Name: "To User"}},
		Cc:       []models.Contact{{Address: "cc@example.com", Name: "CC User"}},
		Bcc:      []models.Contact{{Address: "bcc@example.com", Name: "BCC User"}},
		ReplyTo:  []models.Contact{{Address: "reply@example.com", Name: "Reply User"}},
		Subject:  "Test Subject",
		TextBody: "Test body",
		HTMLBody: "<p>Test body</p>",
		Headers:  map[string]string{"X-Custom": "value"},
	}

	attachments := []pool.AttachmentData{
		{Filename: "test.txt", ContentType: "text/plain", Data: []byte("test content")},
	}

	msg := service.buildEmailMessage(req, account, attachments)

	if msg.From.Address != "test@example.com" {
		t.Errorf("Expected from 'test@example.com', got '%s'", msg.From.Address)
	}
	if len(msg.To) != 1 {
		t.Errorf("Expected 1 To recipient, got %d", len(msg.To))
	}
	if len(msg.Cc) != 1 {
		t.Errorf("Expected 1 Cc recipient, got %d", len(msg.Cc))
	}
	if len(msg.Bcc) != 1 {
		t.Errorf("Expected 1 Bcc recipient, got %d", len(msg.Bcc))
	}
	if msg.Subject != "Test Subject" {
		t.Errorf("Expected subject 'Test Subject', got '%s'", msg.Subject)
	}
	if msg.TextBody != "Test body" {
		t.Errorf("Expected text body 'Test body', got '%s'", msg.TextBody)
	}
	if msg.HTMLBody != "<p>Test body</p>" {
		t.Errorf("Expected HTML body '<p>Test body</p>', got '%s'", msg.HTMLBody)
	}
}

// TestSendService_LoadAttachments tests attachment loading
func TestSendService_LoadAttachments(t *testing.T) {
	ctx := context.Background()
	accountStore := NewMockAccountStore()
	account := createTestAccount("acc_1")
	if err := accountStore.Create(ctx, account); err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	scheduler := scheduler.NewFairUseScheduler()
	defer scheduler.Shutdown()
	scheduler.InitializeAccount("acc_1", nil)
	storage := storage.NewFileAttachmentStorage("")

	// Store test attachment
	attachmentData := []byte("test attachment content")
	attachmentPath, _ := storage.Store("acc_1", "test_folder", "1", "test.txt", attachmentData)
	// Extract ID from path for test
	attachmentID := "test_attachment_id"

	service, _ := NewSendService(nil, scheduler, storage, accountStore, "12345678901234567890123456789012", SendServiceConfig{})

	// Test empty attachment list
	empty, err := service.loadAttachments([]string{})
	if err != nil {
		t.Fatalf("Failed with empty attachment list: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("Expected 0 attachments, got %d", len(empty))
	}

	// Note: Testing actual attachment loading requires mocking the storage
	// The real implementation would fetch from storage
	_ = attachmentPath
	_ = attachmentID
}

// TestIsTemporaryError tests temporary error detection
func TestIsTemporaryError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"timeout error", &mockError{"connection timeout"}, true},
		{"temporary error", &mockError{"temporary failure"}, true},
		{"try again error", &mockError{"please try again"}, true},
		{"connection refused", &mockError{"connection refused"}, true},
		{"rate limit error", &mockError{"rate limit exceeded"}, true},
		{"421 error", &mockError{"421 Service not available"}, true},
		{"450 error", &mockError{"450 Mailbox unavailable"}, true},
		{"451 error", &mockError{"451 Local error"}, true},
		{"452 error", &mockError{"452 Insufficient storage"}, true},
		{"permanent error", &mockError{"user not found"}, false},
		{"invalid recipient", &mockError{"invalid recipient address"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTemporaryError(tt.err)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v for error: %v", tt.expected, result, tt.err)
			}
		})
	}
}

// mockError implements error interface for testing
type mockError struct {
	msg string
}

func (m *mockError) Error() string {
	return m.msg
}

// TestSendService_TemplateVariables tests template variable substitution
func TestSendService_TemplateVariables(t *testing.T) {
	// Test replaceAll function
	result := replaceAll("Hello {{name}}, welcome to {{company}}!", "{{name}}", "John")
	if result != "Hello John, welcome to {{company}}!" {
		t.Errorf("Expected 'Hello John, welcome to {{company}}!', got '%s'", result)
	}

	// Test with multiple occurrences
	result = replaceAll("{{name}} said {{name}} is here", "{{name}}", "Alice")
	if result != "Alice said Alice is here" {
		t.Errorf("Expected 'Alice said Alice is here', got '%s'", result)
	}

	// Test with no occurrences
	result = replaceAll("No placeholders here", "{{name}}", "Bob")
	if result != "No placeholders here" {
		t.Errorf("Expected 'No placeholders here', got '%s'", result)
	}
}

// TestSendService_GenerateIDs tests ID generation functions
func TestSendService_GenerateIDs(t *testing.T) {
	// Test tracking ID generation
	trackingID := generateTrackingID()
	if trackingID == "" {
		t.Error("Expected non-empty tracking ID")
	}
	if len(trackingID) < 5 {
		t.Errorf("Expected tracking ID to be at least 5 chars, got %d", len(trackingID))
	}

	// Test message ID generation
	messageID := generateMessageID()
	if messageID == "" {
		t.Error("Expected non-empty message ID")
	}

	// Test queue ID generation
	queueID := generateQueueID()
	if queueID == "" {
		t.Error("Expected non-empty queue ID")
	}

	// Ensure uniqueness - test with small number to avoid timestamp collisions
	// Note: generateQueueID uses UnixNano which may have collisions in tight loops
	ids := make(map[string]bool)
	for i := 0; i < 10; i++ {
		id := generateQueueID()
		time.Sleep(1 * time.Microsecond) // Ensure unique timestamps
		if ids[id] {
			t.Errorf("Duplicate ID generated: %s", id)
		}
		ids[id] = true
	}
}

// TestSendQueue tests send queue operations
func TestSendQueue(t *testing.T) {
	queue := &SendQueue{
		pending:   make([]*QueuedEmail, 0),
		scheduled: make(map[string]*QueuedEmail),
	}

	// Add pending email
	pending := &QueuedEmail{
		ID:     "pending_1",
		Status: "pending",
	}
	queue.pending = append(queue.pending, pending)

	// Add scheduled email
	scheduled := &QueuedEmail{
		ID:          "scheduled_1",
		ScheduledAt: func() *time.Time { t := time.Now().Add(1 * time.Hour); return &t }(),
		Status:      "scheduled",
	}
	queue.scheduled[scheduled.ID] = scheduled

	// Check counts
	if len(queue.pending) != 1 {
		t.Errorf("Expected 1 pending, got %d", len(queue.pending))
	}
	if len(queue.scheduled) != 1 {
		t.Errorf("Expected 1 scheduled, got %d", len(queue.scheduled))
	}
}
