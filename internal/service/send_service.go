package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"webmail_engine/internal/models"
	"webmail_engine/internal/pool"
	"webmail_engine/internal/scheduler"
	"webmail_engine/internal/storage"
)

// SendService handles email sending
type SendService struct {
	mu         sync.RWMutex
	pool       *pool.ConnectionPool
	scheduler  *scheduler.FairUseScheduler
	storage    *storage.AttachmentStorage
	queue      *SendQueue
	templates  map[string]*EmailTemplate
}

// SendQueue manages queued emails
type SendQueue struct {
	mu       sync.RWMutex
	pending  []*QueuedEmail
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
	MaxRetries     int
	SendTimeout    time.Duration
	QueueSize      int
	TempStoragePath string
}

// NewSendService creates a new send service
func NewSendService(
	pool *pool.ConnectionPool,
	scheduler *scheduler.FairUseScheduler,
	storage *storage.AttachmentStorage,
	config SendServiceConfig,
) (*SendService, error) {
	service := &SendService{
		pool:      pool,
		scheduler: scheduler,
		storage:   storage,
		queue: &SendQueue{
			pending:   make([]*QueuedEmail, 0),
			scheduled: make(map[string]*QueuedEmail),
		},
		templates: make(map[string]*EmailTemplate),
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

	// Send immediately (simplified - production would use connection pool)
	sentAt := time.Now()
	messageID := fmt.Sprintf("<%s@localhost>", generateMessageID())

	return &models.SendEmailResponse{
		Status:        "sent",
		MessageID:     messageID,
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
	
	// Send email (simplified)
	// In production, this would use SMTP connection from pool
	
	return nil
}

// buildEmailMessage builds an SMTP email message
func (s *SendService) buildEmailMessage(req models.SendEmailRequest) pool.EmailMessage {
	emailMsg := pool.EmailMessage{
		From: models.Contact{
			Address: req.AccountID, // In production, get from account
		},
		To:      req.To,
		Cc:      req.Cc,
		Bcc:     req.Bcc,
		ReplyTo: req.ReplyTo,
		Subject: req.Subject,
		TextBody: req.TextBody,
		HTMLBody: req.HTMLBody,
		Headers:  req.Headers,
	}

	// Add attachments
	for range req.AttachmentIDs {
		// In production, fetch attachment from storage
		// For now, skip
	}

	return emailMsg
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
