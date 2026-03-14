package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"webmail_engine/internal/cache"
	"webmail_engine/internal/mimeparser"
	"webmail_engine/internal/models"
	"webmail_engine/internal/pool"
	"webmail_engine/internal/scheduler"
	"webmail_engine/internal/storage"
)

// MessageService handles message operations
type MessageService struct {
	mu             sync.RWMutex
	pool           *pool.ConnectionPool
	cache          *cache.Cache
	scheduler      *scheduler.FairUseScheduler
	parser         *mimeparser.MIMEParser
	storage        *storage.AttachmentStorage
	accountService *AccountService
}

// MessageServiceConfig holds service configuration
type MessageServiceConfig struct {
	TempStoragePath string
	MaxInlineSize   int64
}

// NewMessageService creates a new message service
func NewMessageService(
	pool *pool.ConnectionPool,
	cache *cache.Cache,
	scheduler *scheduler.FairUseScheduler,
	accountService *AccountService,
	config MessageServiceConfig,
) (*MessageService, error) {
	parser := mimeparser.NewMIMEParser(config.TempStoragePath)
	storage := storage.NewAttachmentStorage(config.TempStoragePath)

	return &MessageService{
		pool:           pool,
		cache:          cache,
		scheduler:      scheduler,
		accountService: accountService,
		parser:         parser,
		storage:        storage,
	}, nil
}

// GetMessageList retrieves a list of messages
func (s *MessageService) GetMessageList(
	ctx context.Context,
	accountID string,
	folder string,
	limit int,
	cursor string,
) (*models.MessageList, error) {
	// Check fair-use tokens
	success, cost, err := s.scheduler.ConsumeTokens(accountID, scheduler.OpList, "normal")
	if err != nil {
		return nil, err
	}
	if !success {
		return nil, models.NewThrottleError(60)
	}

	// Default values
	if folder == "" {
		folder = "INBOX"
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	// Try cache first
	if s.cache != nil {
		cachedList, err := s.getCachedMessageList(ctx, accountID, folder, cursor, limit)
		if err == nil && cachedList != nil {
			_ = cost // Don't deduct tokens for cache hit
			return cachedList, nil
		}
	}

	// Get account with decrypted credentials
	account, err := s.accountService.GetAccountWithCredentials(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to get account: %w", err)
	}

	// Create IMAP connection with timeout
	imapCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	log.Printf("[DEBUG] Creating IMAP config for account %s, password length: %d", accountID, len(account.IMAPConfig.Password))
	imapConfig := pool.IMAPConfig{
		Host:       account.IMAPConfig.Host,
		Port:       account.IMAPConfig.Port,
		Username:   account.IMAPConfig.Username,
		Password:   account.IMAPConfig.Password, // Decrypted
		Encryption: account.IMAPConfig.Encryption,
	}

	log.Printf("Connecting to IMAP %s:%d for account %s as user %s", imapConfig.Host, imapConfig.Port, accountID, imapConfig.Username)
	client, err := pool.ConnectIMAPv2(imapCtx, imapConfig)
	if err != nil {
		log.Printf("IMAP connection failed: %v", err)
		return nil, models.NewServiceUnavailableError("IMAP server", err.Error())
	}
	defer client.Close()

	// Select folder
	_, err = client.SelectFolder(folder)
	if err != nil {
		log.Printf("Failed to select folder %s: %v", folder, err)
		return nil, fmt.Errorf("failed to select folder: %w", err)
	}

	// Search for ALL messages
	uids, err := client.Search("ALL")
	if err != nil {
		log.Printf("IMAP search failed: %v", err)
		return nil, fmt.Errorf("search failed: %w", err)
	}

	// Limit results
	totalCount := len(uids)
	if len(uids) > limit {
		uids = uids[len(uids)-limit:] // Get most recent
	}

	// Fetch message envelopes
	var messages []models.MessageSummary
	if len(uids) > 0 {
		envelopes, err := client.FetchMessages(uids, false)
		if err != nil {
			log.Printf("Failed to fetch messages: %v", err)
			// Continue with empty list
			envelopes = []pool.MessageEnvelope{}
		}

		// Convert to MessageSummary
		for _, env := range envelopes {
			from := models.Contact{}
			if len(env.From) > 0 {
				from = env.From[0]
			}
			msg := models.MessageSummary{
				UID:       fmt.Sprintf("%d", env.UID),
				MessageID: env.MessageID,
				Subject:   env.Subject,
				From:      from,
				To:        env.To,
				Date:      env.Date,
				Flags:     []models.MessageFlag{},
				Size:      env.Size,
				ThreadID:  env.MessageID, // Minimal fallback for ThreadID
				Folder:    folder,
			}
			messages = append(messages, msg)
		}
	}

	// Deduct tokens
	_ = cost

	messageList := &models.MessageList{
		Messages:    messages,
		TotalCount:  totalCount,
		PageSize:    len(messages),
		CurrentPage: 1,
		HasMore:     totalCount > limit,
		Folder:      folder,
		DataSource:  "live",
		Freshness:   time.Now(),
	}

	// Cache the message list for future requests
	if s.cache != nil {
		if err := s.setCachedMessageList(ctx, accountID, folder, cursor, limit, messageList); err != nil {
			log.Printf("Warning: failed to cache message list: %v", err)
		}
	}

	log.Printf("Fetched %d messages from folder %s for account %s", len(messages), folder, accountID)
	return messageList, nil
}

// GetMessage retrieves a specific message with full content
func (s *MessageService) GetMessage(
	ctx context.Context,
	accountID string,
	uid string,
	folder string,
) (*models.Message, error) {
	// Check fair-use tokens
	success, _, err := s.scheduler.ConsumeTokens(accountID, scheduler.OpRetrieve, "normal")
	if err != nil {
		return nil, err
	}
	if !success {
		return nil, models.NewThrottleError(60)
	}

	if folder == "" {
		folder = "INBOX"
	}

	// Get account with decrypted credentials
	account, err := s.accountService.GetAccountWithCredentials(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to get account: %w", err)
	}

	// Create IMAP connection with timeout
	imapCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	imapConfig := pool.IMAPConfig{
		Host:       account.IMAPConfig.Host,
		Port:       account.IMAPConfig.Port,
		Username:   account.IMAPConfig.Username,
		Password:   account.IMAPConfig.Password,
		Encryption: account.IMAPConfig.Encryption,
	}

	client, err := pool.ConnectIMAPv2(imapCtx, imapConfig)
	if err != nil {
		return nil, models.NewServiceUnavailableError("IMAP server", err.Error())
	}
	defer client.Close()

	// Select folder
	_, err = client.SelectFolder(folder)
	if err != nil {
		return nil, fmt.Errorf("failed to select folder: %w", err)
	}

	// Convert UID string to uint32
	uidNum, err := strconv.ParseUint(uid, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid UID: %w", err)
	}

	// Fetch raw message
	log.Printf("Fetching message %s from folder %s", uid, folder)
	rawData, err := client.FetchMessageRaw(uint32(uidNum))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch message: %w", err)
	}

	// Parse MIME content
	parseConfig := mimeparser.ParseConfig{
		IncludeHeaders:     true,
		IncludeBody:        true,
		IncludeAttachments: true,
		MaxInlineSize:      1024 * 1024, // 1MB
		Format:             "standard",
		ExtractLinks:       true,
		ExtractContacts:    true,
	}

	parseResult, err := s.parser.Parse(rawData, parseConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to parse message: %w", err)
	}

	// Mark message as seen (optional)
	// client.MarkSeen(uint32(uidNum))

	log.Printf("Successfully parsed message %s: %d bytes", uid, len(rawData))
	return parseResult.Message, nil
}

// SearchMessages searches for messages
func (s *MessageService) SearchMessages(
	ctx context.Context,
	query models.SearchQuery,
) (*models.SearchResult, error) {
	// Check fair-use tokens
	success, _, err := s.scheduler.ConsumeTokens(query.AccountID, scheduler.OpSearch, "normal")
	if err != nil {
		return nil, err
	}
	if !success {
		return nil, models.NewThrottleError(60)
	}

	// Get account with decrypted credentials
	account, err := s.accountService.GetAccountWithCredentials(ctx, query.AccountID)
	if err != nil {
		return nil, fmt.Errorf("failed to get account: %w", err)
	}

	// Create IMAP connection with timeout
	imapCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	imapConfig := pool.IMAPConfig{
		Host:       account.IMAPConfig.Host,
		Port:       account.IMAPConfig.Port,
		Username:   account.IMAPConfig.Username,
		Password:   account.IMAPConfig.Password,
		Encryption: account.IMAPConfig.Encryption,
	}

	client, err := pool.ConnectIMAPv2(imapCtx, imapConfig)
	if err != nil {
		return nil, models.NewServiceUnavailableError("IMAP server", err.Error())
	}
	defer client.Close()

	// Select folder
	folder := query.Folder
	if folder == "" {
		folder = "INBOX"
	}
	_, err = client.SelectFolder(folder)
	if err != nil {
		return nil, fmt.Errorf("failed to select folder: %w", err)
	}

	// Build IMAP search query
	searchCriteria := s.buildIMAPSearchQuery(query)
	log.Printf("Searching IMAP with criteria: %s", searchCriteria)

	// Execute search
	uids, err := client.Search(searchCriteria)
	if err != nil {
		log.Printf("IMAP search failed: %v", err)
		return nil, fmt.Errorf("search failed: %w", err)
	}

	// Limit results
	totalMatches := len(uids)
	limit := query.Limit
	if limit <= 0 {
		limit = 50
	}
	if len(uids) > limit {
		uids = uids[len(uids)-limit:]
	}

	// Fetch message envelopes
	var messages []models.MessageSummary
	if len(uids) > 0 {
		envelopes, err := client.FetchMessages(uids, false)
		if err != nil {
			log.Printf("Failed to fetch messages: %v", err)
		} else {
			for _, env := range envelopes {
				from := models.Contact{}
				if len(env.From) > 0 {
					from = env.From[0]
				}
				msg := models.MessageSummary{
					UID:     fmt.Sprintf("%d", env.UID),
					Subject: env.Subject,
					From:    from,
					To:      env.To,
					Date:    env.Date,
					Size:    env.Size,
					Folder:  folder,
				}
				messages = append(messages, msg)
			}
		}
	}

	return &models.SearchResult{
		Messages:     messages,
		TotalMatches: totalMatches,
		SearchTime:   0,
		CacheUsed:    false,
		NextOffset:   query.Offset + len(messages),
	}, nil
}

// buildIMAPSearchQuery builds an IMAP search query from search criteria
func (s *MessageService) buildIMAPSearchQuery(query models.SearchQuery) string {
	var parts []string

	// Add keyword search (BODY or TEXT)
	if len(query.Keywords) > 0 && query.Keywords[0] != "" {
		parts = append(parts, fmt.Sprintf(`BODY "%s"`, query.Keywords[0]))
	}

	// Add FROM search
	if query.From != "" {
		parts = append(parts, fmt.Sprintf(`FROM "%s"`, query.From))
	}

	// Add TO search
	if query.To != "" {
		parts = append(parts, fmt.Sprintf(`TO "%s"`, query.To))
	}

	// Add SUBJECT search
	if query.Subject != "" {
		parts = append(parts, fmt.Sprintf(`SUBJECT "%s"`, query.Subject))
	}

	// Add date range
	if query.Since != nil {
		parts = append(parts, fmt.Sprintf("SINCE %s", query.Since.Format("02-Jan-2006")))
	}
	if query.Before != nil {
		parts = append(parts, fmt.Sprintf("BEFORE %s", query.Before.Format("02-Jan-2006")))
	}

	// Add UNSEEN flag
	for _, flag := range query.HasFlags {
		if flag == "seen" {
			parts = append(parts, "UNSEEN")
		}
	}

	// Default to ALL if no criteria
	if len(parts) == 0 {
		return "ALL"
	}

	return joinStrings(parts, " ")
}

func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += sep + parts[i]
	}
	return result
}

// ParseMessage parses raw MIME data
func (s *MessageService) ParseMessage(
	ctx context.Context,
	rawData []byte,
	config mimeparser.ParseConfig,
) (*mimeparser.ParseResult, error) {
	startTime := time.Now()

	result, err := s.parser.Parse(rawData, config)
	if err != nil {
		return nil, err
	}

	// Store large attachments
	for i, att := range result.Attachments {
		if int64(len(att.Data)) > config.MaxInlineSize {
			// Store attachment
			_, err := s.storage.Store(att.Data, att.Checksum)
			if err != nil {
				return nil, fmt.Errorf("failed to store attachment: %w", err)
			}

			// Generate signed URL
			signedURL, expiry := mimeparser.GenerateSignedURL(
				att.ID,
				"/api/v1/attachments",
				"secret-key",
				24*time.Hour,
			)

			result.Message.Attachments[i].AccessURL = signedURL
			result.Message.Attachments[i].URLExpiry = &expiry
		}
	}

	// Set processing metadata
	if result.Message.ProcessingMetadata == nil {
		result.Message.ProcessingMetadata = &models.ProcessingMetadata{}
	}
	result.Message.ProcessingMetadata.ProcessingTime = time.Since(startTime).Milliseconds()
	result.Message.ProcessingMetadata.SizeOriginal = int64(len(rawData))

	return result, nil
}

// StreamMessage streams a message in chunks
func (s *MessageService) StreamMessage(
	ctx context.Context,
	accountID string,
	uid string,
	chunkSize int,
	handler func(chunk []byte) error,
) error {
	// Check fair-use tokens
	success, _, err := s.scheduler.ConsumeTokens(accountID, scheduler.OpRetrieve, "normal")
	if err != nil {
		return err
	}
	if !success {
		return models.NewThrottleError(60)
	}

	// In production, this would use IMAPClient.StreamMessage
	// For now, simulate streaming

	return nil
}

// GetAttachmentAccess returns secure access to an attachment
func (s *MessageService) GetAttachmentAccess(
	ctx context.Context,
	accountID string,
	messageUID string,
	attachmentID string,
) (*models.AttachmentAccessResponse, error) {
	// Check fair-use tokens
	success, _, err := s.scheduler.ConsumeTokens(accountID, scheduler.OpAttachment, "normal")
	if err != nil {
		return nil, err
	}
	if !success {
		return nil, models.NewThrottleError(60)
	}

	// Get attachment info from cache
	attachment, err := s.cache.GetAttachmentInfo(ctx, attachmentID)
	if err != nil {
		return nil, models.ErrAttachmentNotFound
	}

	// Generate signed URL
	signedURL, expiry := mimeparser.GenerateSignedURL(
		attachmentID,
		"/api/v1/attachments",
		"secret-key",
		24*time.Hour,
	)

	return &models.AttachmentAccessResponse{
		AttachmentID: attachmentID,
		AccessURL:    signedURL,
		URLExpiry:    expiry,
		Filename:     attachment.Filename,
		ContentType:  attachment.ContentType,
		Size:         attachment.Size,
		Checksum:     attachment.Checksum,
		AccessMethod: "download",
		MaxDownloads: 10,
	}, nil
}

// getCachedMessageList tries to get message list from cache
func (s *MessageService) getCachedMessageList(
	ctx context.Context,
	accountID string,
	folder string,
	cursor string,
	limit int,
) (*models.MessageList, error) {
	// Build cache key for message list
	cacheKey := fmt.Sprintf("msglist:%s:%s:%s:%d", accountID, folder, cursor, limit)

	// Try to get from cache
	data, err := s.cache.Get(ctx, cacheKey)
	if err != nil {
		return nil, err // Cache miss or error
	}

	// Unmarshal message list
	var messageList models.MessageList
	if err := json.Unmarshal(data, &messageList); err != nil {
		log.Printf("Failed to unmarshal cached message list: %v", err)
		return nil, err
	}

	// Check freshness (cache is stale if older than 5 minutes)
	if time.Since(messageList.Freshness) > 5*time.Minute {
		log.Printf("Cached message list is stale (age: %v)", time.Since(messageList.Freshness))
		return nil, nil
	}

	log.Printf("Cache hit for message list: account=%s, folder=%s, count=%d", accountID, folder, len(messageList.Messages))
	return &messageList, nil
}

// setCachedMessageList stores message list in cache
func (s *MessageService) setCachedMessageList(
	ctx context.Context,
	accountID string,
	folder string,
	cursor string,
	limit int,
	messageList *models.MessageList,
) error {
	// Build cache key for message list
	cacheKey := fmt.Sprintf("msglist:%s:%s:%s:%d", accountID, folder, cursor, limit)

	// Marshal message list
	data, err := json.Marshal(messageList)
	if err != nil {
		log.Printf("Failed to marshal message list for cache: %v", err)
		return err
	}

	// Store in cache with 5 minute TTL
	if err := s.cache.Set(ctx, cacheKey, data, 5*time.Minute); err != nil {
		log.Printf("Failed to cache message list: %v", err)
		return err
	}

	log.Printf("Cached message list: account=%s, folder=%s, count=%d", accountID, folder, len(messageList.Messages))
	return nil
}

// generateQueryHash generates a hash for search query caching
func (s *MessageService) generateQueryHash(query models.SearchQuery) string {
	data, _ := json.Marshal(query)
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash)[:32]
}

// FetchMessages fetches multiple messages
func (s *MessageService) FetchMessages(
	ctx context.Context,
	accountID string,
	uids []string,
	folder string,
) ([]*models.Message, error) {
	// Check fair-use tokens
	success, cost, err := s.scheduler.ConsumeTokens(accountID, scheduler.OpFetch, "normal")
	if err != nil {
		return nil, err
	}
	if !success {
		return nil, models.NewThrottleError(60)
	}

	if folder == "" {
		folder = "INBOX"
	}

	messages := make([]*models.Message, 0, len(uids))

	for _, uid := range uids {
		// Try cache first
		cachedMsg, err := s.cache.GetMessage(ctx, accountID, uid, folder)
		if err == nil && cachedMsg != nil {
			cachedMsg.ProcessingMetadata = &models.ProcessingMetadata{
				CacheStatus: "hit",
			}
			messages = append(messages, cachedMsg)
			continue
		}

		// Fetch from IMAP (simulated)
		message := &models.Message{
			UID:    uid,
			Folder: folder,
		}

		messages = append(messages, message)
	}

	_ = cost

	return messages, nil
}

// SyncMessages syncs messages for an account
func (s *MessageService) SyncMessages(
	ctx context.Context,
	accountID string,
	folder string,
) (int, error) {
	// Check fair-use tokens
	success, _, err := s.scheduler.ConsumeTokens(accountID, scheduler.OpSync, "normal")
	if err != nil {
		return 0, err
	}
	if !success {
		return 0, models.NewThrottleError(60)
	}

	if folder == "" {
		folder = "INBOX"
	}

	// In production, this would:
	// 1. Connect to IMAP
	// 2. Select folder
	// 3. Get UID validity and next UID
	// 4. Fetch new messages
	// 5. Update cache

	return 0, nil
}

// DeleteMessage deletes a message
func (s *MessageService) DeleteMessage(
	ctx context.Context,
	accountID string,
	uid string,
	folder string,
) error {
	// Check fair-use tokens
	success, _, err := s.scheduler.ConsumeTokens(accountID, scheduler.OpFetch, "normal")
	if err != nil {
		return err
	}
	if !success {
		return models.NewThrottleError(60)
	}

	if folder == "" {
		folder = "INBOX"
	}

	// In production, this would:
	// 1. Connect to IMAP
	// 2. Select folder
	// 3. Mark message as DELETED
	// 4. EXPUNGE folder

	// Remove from cache
	if err := s.cache.DeleteMessage(ctx, accountID, uid, folder); err != nil {
		// Non-fatal
	}

	return nil
}

// MarkMessageRead marks a message as read
func (s *MessageService) MarkMessageRead(
	ctx context.Context,
	accountID string,
	uid string,
	folder string,
) error {
	// Check fair-use tokens
	success, _, err := s.scheduler.ConsumeTokens(accountID, scheduler.OpFetch, "low")
	if err != nil {
		return err
	}
	if !success {
		return models.NewThrottleError(60)
	}

	if folder == "" {
		folder = "INBOX"
	}

	// In production, this would use IMAP STORE +FLAGS
	return nil
}
