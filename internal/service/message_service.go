package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"webmail_engine/internal/cache"
	"webmail_engine/internal/mimeparser"
	"webmail_engine/internal/models"
	"webmail_engine/internal/pool"
	"webmail_engine/internal/scheduler"
	"webmail_engine/internal/storage"

	"github.com/emersion/go-imap/v2"
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

// GetMessageList retrieves a list of messages with sorting and pagination
func (s *MessageService) GetMessageList(
	ctx context.Context,
	accountID string,
	folder string,
	limit int,
	cursor string,
	sortBy models.SortField,
	sortOrder models.SortOrder,
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

	// Default sorting: by date, descending (newest first)
	if sortBy == "" {
		sortBy = models.SortByDate
	}
	if sortOrder == "" {
		sortOrder = models.SortOrderDesc
	}

	log.Printf("Sort parameters: sortBy=%s, sortOrder=%s", sortBy, sortOrder)

	// Parse cursor to get page info
	var cursorData CursorData
	if cursor != "" {
		cursorData, err = decodeCursor(cursor)
		if err != nil {
			log.Printf("Invalid cursor, starting from beginning: %v", err)
			cursorData = CursorData{Page: 0}
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
		// Check for authentication errors
		if errors.Is(err, models.ErrMailServerAuthFailed) {
			return nil, models.NewAuthError("Invalid mail server credentials")
		}
		return nil, models.NewServiceUnavailableError("IMAP server", err.Error())
	}
	defer client.Close()

	// Select folder and get UID validity for cache key
	folderInfo, err := client.SelectFolder(folder)
	if err != nil {
		log.Printf("Failed to select folder %s: %v", folder, err)
		return nil, fmt.Errorf("failed to select folder: %w", err)
	}

	// Build cache key with UID validity to detect folder changes
	uidValidity := folderInfo.UIDValidity
	cacheKey := fmt.Sprintf("msglist:%s:%s:%s:%d:%s:%s:%d",
		accountID, folder, cursor, limit, sortBy, sortOrder, uidValidity)

	// Try cache first (with UID validity check)
	if s.cache != nil {
		cachedList, err := s.getCachedMessageListByKey(ctx, cacheKey)
		if err == nil && cachedList != nil {
			_ = cost // Don't deduct tokens for cache hit
			log.Printf("Cache hit with valid UID validity %d for folder %s", uidValidity, folder)
			return cachedList, nil
		}
		// Cache miss - could be due to UID validity change or first request
		// Invalidate old cache entries (without current UID validity)
		if err := s.invalidateMessageListCache(ctx, accountID, folder); err != nil {
			log.Printf("Warning: failed to invalidate old cache: %v", err)
		}
	}

	// Calculate pagination
	pageSize := limit
	if pageSize <= 0 {
		pageSize = 50
	}
	startIndex := cursorData.Page * pageSize
	endIndex := startIndex + pageSize

	// For large mailboxes, fetch messages in batches to avoid "Too long argument" errors
	// Maximum UIDs per FETCH command to stay under IMAP command length limits
	const maxUIDsPerFetch = 100

	// Search for ALL messages to get UIDs
	allUIDs, err := client.Search("ALL")
	if err != nil {
		log.Printf("IMAP search failed: %v", err)
		return nil, fmt.Errorf("search failed: %w", err)
	}

	// Use actual UID count as total (folderInfo.Messages may include deleted messages)
	totalCount := len(allUIDs)

	log.Printf("Found %d messages in folder %s, page=%d, limit=%d", totalCount, folder, cursorData.Page, limit)

	// Sort UIDs based on sort order (IMAP returns UIDs in ascending order)
	var uids []uint32
	if sortOrder == models.SortOrderDesc {
		// Reverse for descending order (newest first)
		uids = make([]uint32, 0, len(allUIDs))  // Pre-allocate capacity, not length
		for i := len(allUIDs) - 1; i >= 0; i-- {
			uids = append(uids, allUIDs[i])
		}
	} else {
		uids = allUIDs
	}

	log.Printf("UIDs prepared: %d items (first: %v, last: %v)", len(uids),
		func() uint32 { if len(uids) > 0 { return uids[0] }; return 0 }(),
		func() uint32 { if len(uids) > 0 { return uids[len(uids)-1] }; return 0 }(),
	)

	// Apply pagination to UIDs
	if startIndex >= len(uids) {
		log.Printf("Start index %d beyond available UIDs (%d), returning empty", startIndex, len(uids))
		// Cursor is beyond available messages, return empty
		messageList := &models.MessageList{
			Messages:    []models.MessageSummary{},
			TotalCount:  totalCount,
			PageSize:    0,
			CurrentPage: cursorData.Page + 1,
			HasMore:     false,
			NextCursor:  "",
			Folder:      folder,
			DataSource:  "live",
			Freshness:   time.Now(),
		}
		return messageList, nil
	}

	if endIndex > len(uids) {
		endIndex = len(uids)
	}
	pageUIDs := uids[startIndex:endIndex]

	log.Printf("Pagination: start=%d, end=%d, pageUIDs=%d items", startIndex, endIndex, len(pageUIDs))
	if len(pageUIDs) > 0 {
		log.Printf("Page UIDs range: %d to %d", pageUIDs[0], pageUIDs[len(pageUIDs)-1])
	}

	// Fetch messages in batches to avoid IMAP command length limits
	var allEnvelopes []pool.MessageEnvelope
	if len(pageUIDs) > 0 {
		for i := 0; i < len(pageUIDs); i += maxUIDsPerFetch {
			batchEnd := i + maxUIDsPerFetch
			if batchEnd > len(pageUIDs) {
				batchEnd = len(pageUIDs)
			}
			batch := pageUIDs[i:batchEnd]

			// IMAP FETCH requires UIDs in ascending order, sort the batch
			sortedBatch := make([]uint32, len(batch))
			copy(sortedBatch, batch)
			sort.Slice(sortedBatch, func(a, b int) bool { return sortedBatch[a] < sortedBatch[b] })

			log.Printf("Fetching batch %d-%d of %d (UIDs %d to %d)", i, batchEnd, len(pageUIDs), sortedBatch[0], sortedBatch[len(sortedBatch)-1])
			batchEnvelopes, err := client.FetchMessages(sortedBatch, false)
			if err != nil {
				log.Printf("Failed to fetch message batch: %v", err)
				continue
			}
			log.Printf("Batch fetched: %d envelopes", len(batchEnvelopes))
			allEnvelopes = append(allEnvelopes, batchEnvelopes...)
		}
	}

	log.Printf("Total envelopes fetched: %d", len(allEnvelopes))

	// Convert to MessageSummary
	messages := s.convertToMessageSummary(allEnvelopes, folder)

	// Always apply client-side sort to ensure correct order
	// (IMAP fetch returns in UID order, but we need to respect sortBy/sortOrder)
	log.Printf("Before sort: first message date=%s", func() string { if len(messages) > 0 { return messages[0].Date.String() }; return "N/A" }())
	messages = s.sortMessages(messages, sortBy, sortOrder)
	log.Printf("After sort: first message date=%s, last=%s", func() string { if len(messages) > 0 { return messages[0].Date.String() }; return "N/A" }(), func() string { if len(messages) > 0 { return messages[len(messages)-1].Date.String() }; return "N/A" }())

	// Calculate next cursor based on actual UIDs, not totalCount
	var nextCursor string
	if endIndex < len(uids) {
		nextCursorData := CursorData{
			Page:      cursorData.Page + 1,
			SortBy:    sortBy,
			SortOrder: sortOrder,
			Timestamp: time.Now(),
		}
		nextCursor, _ = encodeCursor(nextCursorData)
	}

	// Deduct tokens
	_ = cost

	messageList := &models.MessageList{
		Messages:    messages,
		TotalCount:  totalCount,
		PageSize:    len(messages),
		CurrentPage: cursorData.Page + 1,
		HasMore:     endIndex < len(uids),
		NextCursor:  nextCursor,
		Folder:      folder,
		DataSource:  "live",
		Freshness:   time.Now(),
	}

	// Set UID validity for cache validation
	messageList.UIDValidity = uidValidity

	// Cache the message list with UID validity key
	if s.cache != nil {
		if err := s.setCachedMessageListByKey(ctx, cacheKey, messageList); err != nil {
			log.Printf("Warning: failed to cache message list: %v", err)
		}
	}

	log.Printf("Fetched %d messages from folder %s for account %s (UID validity: %d)", len(messages), folder, accountID, uidValidity)
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
		// Check for authentication errors
		if errors.Is(err, models.ErrMailServerAuthFailed) {
			return nil, models.NewAuthError("Invalid mail server credentials")
		}
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
		// Check for authentication errors
		if errors.Is(err, models.ErrMailServerAuthFailed) {
			return nil, models.NewAuthError("Invalid mail server credentials")
		}
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

// getCachedMessageListByKey tries to get message list from cache using a pre-built key
func (s *MessageService) getCachedMessageListByKey(
	ctx context.Context,
	cacheKey string,
) (*models.MessageList, error) {
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

	log.Printf("Cache hit for message list: key=%s, count=%d", cacheKey, len(messageList.Messages))
	return &messageList, nil
}

// getCachedMessageList tries to get message list from cache (legacy method)
func (s *MessageService) getCachedMessageList(
	ctx context.Context,
	accountID string,
	folder string,
	cursor string,
	limit int,
	sortBy models.SortField,
	sortOrder models.SortOrder,
) (*models.MessageList, error) {
	// Build cache key for message list (without UID validity - for backward compatibility)
	cacheKey := fmt.Sprintf("msglist:%s:%s:%s:%d:%s:%s", accountID, folder, cursor, limit, sortBy, sortOrder)
	return s.getCachedMessageListByKey(ctx, cacheKey)
}

// setCachedMessageList stores message list in cache
func (s *MessageService) setCachedMessageList(
	ctx context.Context,
	accountID string,
	folder string,
	cursor string,
	limit int,
	sortBy models.SortField,
	sortOrder models.SortOrder,
	messageList *models.MessageList,
) error {
	// Build cache key for message list (without UID validity for general caching)
	cacheKey := fmt.Sprintf("msglist:%s:%s:%s:%d:%s:%s", accountID, folder, cursor, limit, sortBy, sortOrder)
	return s.setCachedMessageListByKey(ctx, cacheKey, messageList)
}

// setCachedMessageListByKey stores message list in cache using a pre-built key
func (s *MessageService) setCachedMessageListByKey(
	ctx context.Context,
	cacheKey string,
	messageList *models.MessageList,
) error {
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

	log.Printf("Cached message list: key=%s, count=%d", cacheKey, len(messageList.Messages))
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
	permanent bool,
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

	// Get account with decrypted credentials
	account, err := s.accountService.GetAccountWithCredentials(ctx, accountID)
	if err != nil {
		return fmt.Errorf("failed to get account: %w", err)
	}

	// Create IMAP connection
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
		if errors.Is(err, models.ErrMailServerAuthFailed) {
			return models.NewAuthError("Invalid mail server credentials")
		}
		return models.NewServiceUnavailableError("IMAP server", err.Error())
	}
	defer client.Close()

	// Select folder
	_, err = client.SelectFolder(folder)
	if err != nil {
		return fmt.Errorf("failed to select folder: %w", err)
	}

	// Convert UID to uint32
	uidNum, err := strconv.ParseUint(uid, 10, 32)
	if err != nil {
		return fmt.Errorf("invalid UID: %w", err)
	}

	// Mark message as deleted
	err = client.Store([]uint32{uint32(uidNum)}, []imap.Flag{imap.FlagDeleted}, true)
	if err != nil {
		return fmt.Errorf("failed to mark message as deleted: %w", err)
	}

	if permanent {
		// EXPUNGE to permanently delete
		err = client.Expunge()
		if err != nil {
			return fmt.Errorf("failed to expunge: %w", err)
		}
	}

	// Remove from cache
	if err := s.cache.DeleteMessage(ctx, accountID, uid, folder); err != nil {
		log.Printf("Warning: failed to delete message from cache: %v", err)
	}

	// Invalidate message list cache for this folder
	if err := s.invalidateMessageListCache(ctx, accountID, folder); err != nil {
		log.Printf("Warning: failed to invalidate message list cache: %v", err)
	}

	log.Printf("Deleted message %s from folder %s (permanent: %v)", uid, folder, permanent)
	return nil
}

// DeleteMessages deletes multiple messages
func (s *MessageService) DeleteMessages(
	ctx context.Context,
	accountID string,
	uids []string,
	folder string,
	permanent bool,
) (int, error) {
	// Check fair-use tokens
	success, _, err := s.scheduler.ConsumeTokens(accountID, scheduler.OpFetch, "normal")
	if err != nil {
		return 0, err
	}
	if !success {
		return 0, models.NewThrottleError(60)
	}

	if folder == "" {
		folder = "INBOX"
	}

	deletedCount := 0
	for _, uid := range uids {
		if err := s.DeleteMessage(ctx, accountID, uid, folder, permanent); err != nil {
			log.Printf("Failed to delete message %s: %v", uid, err)
			continue
		}
		deletedCount++
	}

	log.Printf("Deleted %d/%d messages from folder %s", deletedCount, len(uids), folder)
	return deletedCount, nil
}

// MarkMessagesRead marks multiple messages as read
func (s *MessageService) MarkMessagesRead(
	ctx context.Context,
	accountID string,
	uids []string,
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

	// Get account with decrypted credentials
	account, err := s.accountService.GetAccountWithCredentials(ctx, accountID)
	if err != nil {
		return fmt.Errorf("failed to get account: %w", err)
	}

	// Create IMAP connection
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
		if errors.Is(err, models.ErrMailServerAuthFailed) {
			return models.NewAuthError("Invalid mail server credentials")
		}
		return models.NewServiceUnavailableError("IMAP server", err.Error())
	}
	defer client.Close()

	// Select folder
	_, err = client.SelectFolder(folder)
	if err != nil {
		return fmt.Errorf("failed to select folder: %w", err)
	}

	// Build UID set
	uidNums := make([]uint32, 0, len(uids))
	for _, uid := range uids {
		uidNum, err := strconv.ParseUint(uid, 10, 32)
		if err != nil {
			continue
		}
		uidNums = append(uidNums, uint32(uidNum))
	}

	if len(uidNums) == 0 {
		return nil
	}

	// Mark as seen using Store method
	err = client.Store(uidNums, []imap.Flag{imap.FlagSeen}, true)
	if err != nil {
		return fmt.Errorf("failed to mark messages as read: %w", err)
	}

	log.Printf("Marked %d messages as read in folder %s", len(uidNums), folder)
	return nil
}

// ListFolders lists all folders for an account
func (s *MessageService) ListFolders(
	ctx context.Context,
	accountID string,
) ([]*models.FolderInfo, error) {
	// Check fair-use tokens
	success, _, err := s.scheduler.ConsumeTokens(accountID, scheduler.OpList, "low")
	if err != nil {
		return nil, err
	}
	if !success {
		return nil, models.NewThrottleError(60)
	}

	// Try cache first
	if s.cache != nil {
		cachedFolders, err := s.getCachedFolders(ctx, accountID)
		if err == nil && cachedFolders != nil {
			return cachedFolders, nil
		}
	}

	// Get account with decrypted credentials
	account, err := s.accountService.GetAccountWithCredentials(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to get account: %w", err)
	}

	// Create IMAP connection
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
		if errors.Is(err, models.ErrMailServerAuthFailed) {
			return nil, models.NewAuthError("Invalid mail server credentials")
		}
		return nil, models.NewServiceUnavailableError("IMAP server", err.Error())
	}
	defer client.Close()

	// List folders
	imapFolders, err := client.ListFolders()
	if err != nil {
		return nil, fmt.Errorf("failed to list folders: %w", err)
	}

	// Convert to models.FolderInfo with message counts
	folders := make([]*models.FolderInfo, 0, len(imapFolders))
	for _, f := range imapFolders {
		// Get folder stats
		_, err := client.SelectFolder(f.Name)
		folderInfo := &models.FolderInfo{
			Name:        f.Name,
			Delimiter:   f.Delimiter,
			Attributes:  f.Attributes,
			Messages:    f.Messages,
			Recent:      f.Recent,
			Unseen:      f.Unseen,
			UIDNext:     f.UIDNext,
			UIDValidity: f.UIDValidity,
			LastSync:    time.Now(),
		}
		folders = append(folders, folderInfo)
		_ = err // Ignore select errors, continue with other folders
	}

	// Cache folder list
	if s.cache != nil {
		if err := s.setCachedFolders(ctx, accountID, folders); err != nil {
			log.Printf("Warning: failed to cache folders: %v", err)
		}
	}

	log.Printf("Listed %d folders for account %s", len(folders), accountID)
	return folders, nil
}

// GetFolderInfo gets information about a specific folder
func (s *MessageService) GetFolderInfo(
	ctx context.Context,
	accountID string,
	folder string,
) (*models.FolderInfo, error) {
	if folder == "" {
		folder = "INBOX"
	}

	// Check fair-use tokens
	success, _, err := s.scheduler.ConsumeTokens(accountID, scheduler.OpList, "low")
	if err != nil {
		return nil, err
	}
	if !success {
		return nil, models.NewThrottleError(60)
	}

	// Get account with decrypted credentials
	account, err := s.accountService.GetAccountWithCredentials(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to get account: %w", err)
	}

	// Create IMAP connection
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
		if errors.Is(err, models.ErrMailServerAuthFailed) {
			return nil, models.NewAuthError("Invalid mail server credentials")
		}
		return nil, models.NewServiceUnavailableError("IMAP server", err.Error())
	}
	defer client.Close()

	// Select folder and get info
	info, err := client.SelectFolder(folder)
	if err != nil {
		return nil, fmt.Errorf("failed to select folder: %w", err)
	}

	return &models.FolderInfo{
		Name:        folder,
		Messages:    info.Messages,
		Recent:      info.Recent,
		Unseen:      info.Unseen,
		UIDNext:     info.UIDNext,
		UIDValidity: info.UIDValidity,
		LastSync:    time.Now(),
	}, nil
}

// invalidateMessageListCache invalidates all message list cache entries for a folder
func (s *MessageService) invalidateMessageListCache(
	ctx context.Context,
	accountID string,
	folder string,
) error {
	if s.cache == nil {
		return nil
	}

	// Delete all message list cache entries for this folder
	pattern := fmt.Sprintf("msglist:%s:%s:*", accountID, folder)
	keys, err := s.cache.Keys(ctx, pattern)
	if err != nil {
		return err
	}

	for _, key := range keys {
		if err := s.cache.Delete(ctx, key); err != nil {
			log.Printf("Warning: failed to delete cache key %s: %v", key, err)
		}
	}

	log.Printf("Invalidated %d cache entries for folder %s", len(keys), folder)
	return nil
}

// getCachedFolders tries to get folder list from cache
func (s *MessageService) getCachedFolders(
	ctx context.Context,
	accountID string,
) ([]*models.FolderInfo, error) {
	cacheKey := fmt.Sprintf("folders:%s", accountID)

	data, err := s.cache.Get(ctx, cacheKey)
	if err != nil {
		return nil, err
	}

	var folders []*models.FolderInfo
	if err := json.Unmarshal(data, &folders); err != nil {
		return nil, err
	}

	return folders, nil
}

// setCachedFolders stores folder list in cache
func (s *MessageService) setCachedFolders(
	ctx context.Context,
	accountID string,
	folders []*models.FolderInfo,
) error {
	cacheKey := fmt.Sprintf("folders:%s", accountID)

	data, err := json.Marshal(folders)
	if err != nil {
		return err
	}

	return s.cache.Set(ctx, cacheKey, data, 30*time.Minute)
}

// convertToMessageSummary converts IMAP envelopes to MessageSummary
func (s *MessageService) convertToMessageSummary(
	envelopes []pool.MessageEnvelope,
	folder string,
) []models.MessageSummary {
	messages := make([]models.MessageSummary, 0, len(envelopes))
	for i, env := range envelopes {
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
			Flags:     s.parseFlags(env.Flags),
			Size:      env.Size,
			ThreadID:  env.MessageID,
			Folder:    folder,
		}
		messages = append(messages, msg)
		if i < 3 {
			log.Printf("Envelope %d: UID=%d, Date=%s, Subject=%s", i, env.UID, env.Date, env.Subject)
		}
	}
	return messages
}

// parseFlags parses IMAP flags to MessageFlag
func (s *MessageService) parseFlags(imapFlags []string) []models.MessageFlag {
	flags := make([]models.MessageFlag, 0, len(imapFlags))
	for _, f := range imapFlags {
		switch strings.ToLower(strings.Trim(f, "\\")) {
		case "seen":
			flags = append(flags, models.FlagSeen)
		case "answered":
			flags = append(flags, models.FlagAnswered)
		case "flagged":
			flags = append(flags, models.FlagFlagged)
		case "deleted":
			flags = append(flags, models.FlagDeleted)
		case "draft":
			flags = append(flags, models.FlagDraft)
		case "recent":
			flags = append(flags, models.FlagRecent)
		}
	}
	return flags
}

// sortMessages sorts messages by the specified field and order
func (s *MessageService) sortMessages(
	messages []models.MessageSummary,
	sortBy models.SortField,
	sortOrder models.SortOrder,
) []models.MessageSummary {
	// Create a copy to avoid modifying the original
	sorted := make([]models.MessageSummary, len(messages))
	copy(sorted, messages)

	// Sort based on field
	sort.Slice(sorted, func(i, j int) bool {
		var less bool
		switch sortBy {
		case models.SortByDate, "":
			less = sorted[i].Date.Before(sorted[j].Date)
		case models.SortByFrom:
			less = sorted[i].From.Address < sorted[j].From.Address
		case models.SortBySubject:
			less = sorted[i].Subject < sorted[j].Subject
		case models.SortByTo:
			less = len(sorted[i].To) < len(sorted[j].To)
		case models.SortBySize:
			less = sorted[i].Size < sorted[j].Size
		case models.SortByHasAttachments:
			less = false // Would need attachment info
		default:
			less = sorted[i].Date.Before(sorted[j].Date)
		}

		if sortOrder == models.SortOrderDesc {
			return !less
		}
		return less
	})

	return sorted
}

// CursorData represents pagination cursor data
type CursorData struct {
	Page      int                 `json:"page"`
	LastUID   string              `json:"last_uid,omitempty"`
	SortBy    models.SortField    `json:"sort_by"`
	SortOrder models.SortOrder    `json:"sort_order"`
	Timestamp time.Time           `json:"timestamp"`
}

// encodeCursor encodes cursor data to a base64 string
func encodeCursor(data CursorData) (string, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(jsonData), nil
}

// decodeCursor decodes a base64 cursor string to CursorData
func decodeCursor(cursor string) (CursorData, error) {
	var data CursorData
	jsonData, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return data, err
	}
	err = json.Unmarshal(jsonData, &data)
	return data, err
}

// buildUIDSet builds an IMAP UID set string from a slice of UIDs
func buildUIDSet(uids []uint32) string {
	if len(uids) == 0 {
		return ""
	}

	if len(uids) == 1 {
		return strconv.Itoa(int(uids[0]))
	}

	// Sort UIDs
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })

	// Build ranges
	var sets []string
	start := uids[0]
	end := uids[0]

	for i := 1; i < len(uids); i++ {
		if uids[i] == end+1 {
			end = uids[i]
		} else {
			if start == end {
				sets = append(sets, strconv.Itoa(int(start)))
			} else {
				sets = append(sets, fmt.Sprintf("%d:%d", start, end))
			}
			start = uids[i]
			end = uids[i]
		}
	}

	if start == end {
		sets = append(sets, strconv.Itoa(int(start)))
	} else {
		sets = append(sets, fmt.Sprintf("%d:%d", start, end))
	}

	return strings.Join(sets, ",")
}
