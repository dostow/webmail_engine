package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"webmail_engine/internal/crypto"
	"webmail_engine/internal/models"
	"webmail_engine/internal/pool"
	"webmail_engine/internal/scheduler"
	"webmail_engine/internal/storage"
	"webmail_engine/internal/store"
)

// SendService handles email sending
type SendService struct {
	mu           sync.RWMutex
	pool         *pool.ConnectionPool
	scheduler    *scheduler.FairUseScheduler
	storage      *storage.AttachmentStorage
	queue        *SendQueue
	templates    map[string]*EmailTemplate
	msgStore     *store.MemoryMessageStore
	config       SendServiceConfig
	accountStore store.AccountStore
	encryptor    *crypto.Encryptor
}

// SendQueue manages queued emails
type SendQueue struct {
	mu        sync.RWMutex
	pending   []*QueuedEmail
	scheduled map[string]*QueuedEmail
}

// QueuedEmail represents an email waiting to be sent
type QueuedEmail struct {
	ID          string
	AccountID   string
	Request     *models.SendEmailRequest
	ScheduledAt *time.Time
	RetryCount  int
	MaxRetries  int
	Status      string
	Error       string
}

// EmailTemplate represents an email template
type EmailTemplate struct {
	ID      string
	Name    string
	Subject string
	Body    string
	HTML    bool
}

// SendServiceConfig holds send service configuration
type SendServiceConfig struct {
	MaxRetries      int
	SendTimeout     time.Duration
	QueueSize       int
	TempStoragePath string
}

// NewSendService creates a new send service
func NewSendService(
	pool *pool.ConnectionPool,
	scheduler *scheduler.FairUseScheduler,
	storage *storage.AttachmentStorage,
	accountStore store.AccountStore,
	encryptKey string,
	config SendServiceConfig,
) (*SendService, error) {
	encryptor, err := crypto.NewEncryptor(encryptKey)
	if err != nil {
		return nil, fmt.Errorf("invalid encryption key: %w", err)
	}

	service := &SendService{
		pool:         pool,
		scheduler:    scheduler,
		storage:      storage,
		accountStore: accountStore,
		queue: &SendQueue{
			pending:   make([]*QueuedEmail, 0),
			scheduled: make(map[string]*QueuedEmail),
		},
		templates: make(map[string]*EmailTemplate),
		msgStore:  store.NewMemoryMessageStore(),
		config:    config,
		encryptor: encryptor,
	}

	// Start queue processor
	go service.processQueue()

	return service, nil
}

// SendEmail sends an email immediately
func (s *SendService) SendEmail(ctx context.Context, req models.SendEmailRequest) (*models.SendEmailResponse, error) {
	// Check fair-use tokens
	success, cost, err := s.scheduler.ConsumeTokens(req.AccountID, scheduler.OpSend, "normal")
	if err != nil {
		return nil, err
	}
	if !success {
		return nil, models.NewThrottleError(60)
	}

	// Apply template if specified
	if req.TemplateID != "" {
		template, exists := s.templates[req.TemplateID]
		if !exists {
			return nil, fmt.Errorf("template not found: %s", req.TemplateID)
		}

		req.Subject = template.Subject
		req.TextBody = template.Body
		if template.HTML {
			req.HTMLBody = template.Body
		}

		// Apply template variables
		for key, value := range req.TemplateVars {
			placeholder := fmt.Sprintf("{{%s}}", key)
			req.Subject = replaceAll(req.Subject, placeholder, value)
			req.TextBody = replaceAll(req.TextBody, placeholder, value)
			req.HTMLBody = replaceAll(req.HTMLBody, placeholder, value)
		}
	}

	// Generate tracking ID
	trackingID := generateTrackingID()

	// Add tracking headers if enabled
	if req.TrackingEnabled {
		if req.Headers == nil {
			req.Headers = make(map[string]string)
		}
		req.Headers["X-Tracking-ID"] = trackingID
	}

	// Get account to retrieve SMTP config
	account, err := s.accountStore.GetByID(ctx, req.AccountID)
	if err != nil {
		return nil, fmt.Errorf("failed to get account: %w", err)
	}

	// Load attachments
	attachments, err := s.loadAttachments(req.AttachmentIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to load attachments: %w", err)
	}

	// Build email message
	emailMsg := s.buildEmailMessage(req, account, attachments)

	// Send via SMTP using connection pool
	sendCtx := ctx
	if s.config.SendTimeout > 0 {
		var cancel context.CancelFunc
		sendCtx, cancel = context.WithTimeout(ctx, s.config.SendTimeout)
		defer cancel()
	}

	result, err := s.sendViaSMTP(sendCtx, account.SMTPConfig, emailMsg)
	if err != nil {
		// Check if it's a temporary error - queue for retry
		if isTemporaryError(err) {
			queuedEmail := s.QueueEmail(req)
			queuedEmail.Error = err.Error()
			queuedEmail.Status = "pending_retry"
			return &models.SendEmailResponse{
				Status:        "queued",
				QueueID:       queuedEmail.ID,
				ResourceUsage: cost,
			}, nil
		}
		// Permanent error - release tokens
		s.scheduler.ReleaseTokens(req.AccountID, cost)
		return nil, fmt.Errorf("failed to send email: %w", err)
	}

	// Store sent message record
	sentMessage := &models.Message{
		MessageID: result.MessageID,
		Subject:   req.Subject,
		From:      emailMsg.From,
		To:        emailMsg.To,
		Cc:        emailMsg.Cc,
		Bcc:       emailMsg.Bcc,
		ReplyTo:   emailMsg.ReplyTo,
		Date:      result.SentAt,
		Headers:   req.Headers,
		Body: &models.MessageBody{
			Text: req.TextBody,
			HTML: req.HTMLBody,
		},
		Size: int64(len(req.TextBody) + len(req.HTMLBody)),
	}
	if err := s.msgStore.StoreSentMessage(sentMessage); err != nil {
		log.Printf("Warning: failed to store sent message record: %v", err)
	}

	sentAt := result.SentAt
	return &models.SendEmailResponse{
		Status:        "sent",
		MessageID:     result.MessageID,
		TrackingID:    trackingID,
		SentAt:        &sentAt,
		ResourceUsage: cost,
	}, nil
}

// ScheduleEmail schedules an email for later delivery
func (s *SendService) ScheduleEmail(ctx context.Context, req models.SendEmailRequest, scheduleAt time.Time) (*models.SendEmailResponse, error) {
	// Validate schedule time
	if scheduleAt.Before(time.Now()) {
		return nil, fmt.Errorf("schedule time must be in the future")
	}

	// Create queued email
	queuedEmail := &QueuedEmail{
		ID:          generateQueueID(),
		AccountID:   req.AccountID,
		Request:     &req,
		ScheduledAt: &scheduleAt,
		MaxRetries:  3,
		Status:      "scheduled",
	}

	// Add to scheduled queue
	s.queue.mu.Lock()
	s.queue.scheduled[queuedEmail.ID] = queuedEmail
	s.queue.mu.Unlock()

	return &models.SendEmailResponse{
		Status:          "scheduled",
		MessageID:       "",
		ScheduledAt:     &scheduleAt,
		EstimatedSendAt: &scheduleAt,
	}, nil
}

// QueueEmail queues an email for sending (retry or rate-limited)
func (s *SendService) QueueEmail(req models.SendEmailRequest) *QueuedEmail {
	queuedEmail := &QueuedEmail{
		ID:         generateQueueID(),
		AccountID:  req.AccountID,
		Request:    &req,
		MaxRetries: 3,
		Status:     "pending",
	}

	s.queue.mu.Lock()
	s.queue.pending = append(s.queue.pending, queuedEmail)
	s.queue.mu.Unlock()

	return queuedEmail
}

// GetSendStatus returns the status of a sent email
func (s *SendService) GetSendStatus(queueID string) (*QueuedEmail, error) {
	s.queue.mu.RLock()
	defer s.queue.mu.RUnlock()

	// Check scheduled queue
	if email, exists := s.queue.scheduled[queueID]; exists {
		return email, nil
	}

	// Check pending queue
	for _, email := range s.queue.pending {
		if email.ID == queueID {
			return email, nil
		}
	}

	return nil, fmt.Errorf("email not found")
}

// CancelEmail cancels a scheduled or pending email
func (s *SendService) CancelEmail(queueID string) error {
	s.queue.mu.Lock()
	defer s.queue.mu.Unlock()

	// Check scheduled queue
	if _, exists := s.queue.scheduled[queueID]; exists {
		delete(s.queue.scheduled, queueID)
		return nil
	}

	// Check pending queue
	for i, email := range s.queue.pending {
		if email.ID == queueID {
			s.queue.pending = append(s.queue.pending[:i], s.queue.pending[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("email not found")
}

// RegisterTemplate registers an email template
func (s *SendService) RegisterTemplate(template *EmailTemplate) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.templates[template.ID] = template
}

// GetTemplate retrieves a template
func (s *SendService) GetTemplate(templateID string) (*EmailTemplate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	template, exists := s.templates[templateID]
	if !exists {
		return nil, fmt.Errorf("template not found")
	}

	return template, nil
}

// processQueue processes the send queue
func (s *SendService) processQueue() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		s.processScheduled()
		s.processPending()
	}
}

// processScheduled processes scheduled emails
func (s *SendService) processScheduled() {
	s.queue.mu.Lock()
	defer s.queue.mu.Unlock()

	now := time.Now()

	for id, email := range s.queue.scheduled {
		if email.ScheduledAt != nil && !email.ScheduledAt.After(now) {
			// Time to send
			delete(s.queue.scheduled, id)
			s.queue.pending = append(s.queue.pending, email)
		}
	}
}

// processPending processes pending emails
func (s *SendService) processPending() {
	s.queue.mu.Lock()
	defer s.queue.mu.Unlock()

	if len(s.queue.pending) == 0 {
		return
	}

	// Get first pending email
	email := s.queue.pending[0]
	s.queue.pending = s.queue.pending[1:]

	// Try to send
	if err := s.sendQueuedEmail(email); err != nil {
		email.RetryCount++
		email.Error = err.Error()

		if email.RetryCount < email.MaxRetries {
			// Re-queue with backoff
			backoff := time.Duration(email.RetryCount) * time.Minute
			scheduleAt := time.Now().Add(backoff)
			email.ScheduledAt = &scheduleAt
			s.queue.scheduled[email.ID] = email
		} else {
			email.Status = "failed"
		}
	} else {
		email.Status = "sent"
	}
}

// sendQueuedEmail sends a queued email
func (s *SendService) sendQueuedEmail(email *QueuedEmail) error {
	// Check fair-use tokens
	success, _, err := s.scheduler.ConsumeTokens(email.AccountID, scheduler.OpSend, "normal")
	if err != nil {
		return err
	}
	if !success {
		return models.ErrInsufficientTokens
	}

	// Get account to retrieve SMTP config
	ctx := context.Background()
	account, err := s.accountStore.GetByID(ctx, email.AccountID)
	if err != nil {
		return fmt.Errorf("failed to get account: %w", err)
	}

	// Load attachments
	attachments, err := s.loadAttachments(email.Request.AttachmentIDs)
	if err != nil {
		return fmt.Errorf("failed to load attachments: %w", err)
	}

	// Build email message
	emailMsg := s.buildEmailMessage(*email.Request, account, attachments)

	// Send via SMTP
	_, err = s.sendViaSMTP(ctx, account.SMTPConfig, emailMsg)
	return err
}

// buildEmailMessage builds an SMTP email message
func (s *SendService) buildEmailMessage(req models.SendEmailRequest, account *models.Account, attachments []pool.AttachmentData) pool.EmailMessage {
	emailMsg := pool.EmailMessage{
		From: models.Contact{
			Name:    account.Email,
			Address: account.Email,
		},
		To:          req.To,
		Cc:          req.Cc,
		Bcc:         req.Bcc,
		ReplyTo:     req.ReplyTo,
		Subject:     req.Subject,
		TextBody:    req.TextBody,
		HTMLBody:    req.HTMLBody,
		Headers:     req.Headers,
		Attachments: attachments,
	}

	return emailMsg
}

// loadAttachments loads attachments from storage
func (s *SendService) loadAttachments(attachmentIDs []string) ([]pool.AttachmentData, error) {
	if len(attachmentIDs) == 0 {
		return []pool.AttachmentData{}, nil
	}

	attachments := make([]pool.AttachmentData, 0, len(attachmentIDs))
	for _, id := range attachmentIDs {
		data, err := s.storage.Get(id)
		if err != nil {
			return nil, fmt.Errorf("failed to load attachment %s: %w", id, err)
		}

		// Get attachment info for metadata
		info, err := s.storage.GetInfo(id)
		if err != nil {
			return nil, fmt.Errorf("failed to get attachment info %s: %w", id, err)
		}

		attachments = append(attachments, pool.AttachmentData{
			Filename:    info.Checksum, // Use checksum as filename placeholder
			ContentType: "application/octet-stream",
			Data:        data,
			Disposition: "attachment",
		})
	}

	return attachments, nil
}

// sendViaSMTP sends an email via SMTP
func (s *SendService) sendViaSMTP(ctx context.Context, smtpConfig models.ServerConfig, msg pool.EmailMessage) (*pool.SendResult, error) {
	// Decrypt password
	password := smtpConfig.Password
	if password != "" {
		decrypted, err := s.encryptor.Decrypt(password)
		if err != nil {
			log.Printf("Warning: failed to decrypt SMTP password: %v", err)
			// Use as-is if decryption fails (might already be decrypted)
		} else {
			password = decrypted
		}
	}

	// Create SMTP client using the adapter
	smtpAdapter, err := pool.ConnectSMTPv2(ctx, pool.SMTPConfig{
		Host:       smtpConfig.Host,
		Port:       smtpConfig.Port,
		Username:   smtpConfig.Username,
		Password:   password,
		Encryption: smtpConfig.Encryption,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create SMTP adapter: %w", err)
	}
	defer smtpAdapter.Close()

	// Send the message
	result, err := smtpAdapter.Send(msg)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// isTemporaryError checks if an error is temporary (retryable)
func isTemporaryError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// Common temporary error indicators
	temporaryErrors := []string{
		"timeout",
		"temporary",
		"try again",
		"connection refused",
		"connection reset",
		"network unreachable",
		"server busy",
		"rate limit",
		"421",
		"450",
		"451",
		"452",
	}
	for _, temp := range temporaryErrors {
		if strings.Contains(strings.ToLower(errStr), temp) {
			return true
		}
	}
	return false
}

// GetQueueStats returns queue statistics
func (s *SendService) GetQueueStats() (pending, scheduled int) {
	s.queue.mu.RLock()
	defer s.queue.mu.RUnlock()

	return len(s.queue.pending), len(s.queue.scheduled)
}

// Utility functions

func generateTrackingID() string {
	return fmt.Sprintf("trk_%d", time.Now().UnixNano())
}

func generateMessageID() string {
	return fmt.Sprintf("msg_%d", time.Now().UnixNano())
}

func generateQueueID() string {
	return fmt.Sprintf("queue_%d", time.Now().UnixNano())
}

func replaceAll(s, old, new string) string {
	// Simple string replacement
	result := s
	for {
		newResult := replaceOnce(result, old, new)
		if newResult == result {
			break
		}
		result = newResult
	}
	return result
}

func replaceOnce(s, old, new string) string {
	idx := findSubstring(s, old)
	if idx == -1 {
		return s
	}
	return s[:idx] + new + s[idx+len(old):]
}

func findSubstring(s, substr string) int {
	if len(substr) == 0 {
		return 0
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
