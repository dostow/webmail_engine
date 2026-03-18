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
	"webmail_engine/internal/cachekey"
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
	sessions       *pool.IMAPSessionPool
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
	sessions *pool.IMAPSessionPool,
	cache *cache.Cache,
	scheduler *scheduler.FairUseScheduler,
	accountService *AccountService,
	config MessageServiceConfig,
) (*MessageService, error) {
	parser := mimeparser.NewMIMEParser(config.TempStoragePath)
	storage := storage.NewAttachmentStorage(config.TempStoragePath)

	return &MessageService{
		sessions:       sessions,
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
	client, release, err := s.sessions.Acquire(imapCtx, accountID, imapConfig)
	if err != nil {
		log.Printf("IMAP connection failed: %v", err)
		// Check for authentication errors
		if errors.Is(err, models.ErrMailServerAuthFailed) {
			s.accountService.LogAuditEntry(ctx, accountID, account.Email, "auth_failure", "API request failed: invalid credentials", "remote")
			return nil, models.NewAuthError("Invalid mail server credentials")
		}
		return nil, models.NewServiceUnavailableError("IMAP server", err.Error())
	}
	defer release()

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

	// For large mailboxes, fetch messages in batches to avoid "Too long argument" errors
	// Maximum UIDs per FETCH command to stay under IMAP command length limits
	const maxUIDsPerFetch = 100

	// Get UIDs from cache or IMAP with intelligent invalidation
	uidCacheKey := fmt.Sprintf("uids:%s:%s:%d", accountID, folder, uidValidity)
	var allUIDs []uint32
	var currentMessageCount int

	// Get current folder info for comparison
	currentMessageCount = folderInfo.Messages
	currentModSeq := folderInfo.HighestModSeq

	if s.cache != nil {
		// Try to get cached UID list with full metadata
		cachedUIDs, cachedCount, cachedModSeq, qresyncCapable, err := s.getCachedUIDListWithMetadata(ctx, uidCacheKey)

		if err == nil && cachedUIDs != nil {
			log.Printf("UID cache hit: %d UIDs for folder %s (modseq=%d)", len(cachedUIDs), folder, cachedModSeq)
			refreshCache := false

			// Phase 1: Check for significant count changes (deletions without QRESYNC)
			countDiff := abs(currentMessageCount - cachedCount)
			if countDiff > cachedCount/10 && cachedCount > 0 {
				// More than 10% change - refresh cache
				log.Printf("Cache invalidation: message count changed significantly (%d -> %d), refreshing", cachedCount, currentMessageCount)
				refreshCache = true
			} else if qresyncCapable && client.HasQResync() && currentModSeq > cachedModSeq {
				// Phase 2: QRESYNC support - check for vanished messages
				log.Printf("QRESYNC: modseq changed (%d -> %d), checking for vanished messages", cachedModSeq, currentModSeq)
				vanished, err := client.UIDFetchVanished(cachedModSeq)
				if err == nil && len(vanished) > 0 {
					log.Printf("QRESYNC: %d messages vanished, updating cache", len(vanished))
					// Remove vanished UIDs from cache
					cachedUIDs = removeUIDsFromList(cachedUIDs, vanished)
					// Update cache with new UID list
					if err := s.setCachedUIDListWithMetadata(ctx, uidCacheKey, cachedUIDs, len(cachedUIDs), currentModSeq, true, 5*time.Minute); err != nil {
						log.Printf("Warning: failed to update UID cache: %v", err)
					}
					allUIDs = cachedUIDs
				} else {
					allUIDs = cachedUIDs
				}
				goto UseUIDs
			}

			if refreshCache {
				// Fall through to IMAP fetch
			} else {
				allUIDs = cachedUIDs
				goto UseUIDs
			}
		}

		// Cache miss or refresh needed - fetch from IMAP
		log.Printf("UID cache miss for folder %s, fetching from IMAP", folder)

		// Use server-side SORT if available (RFC 5256)
		if client.HasSort() {
			log.Printf("Using server-side SORT: sortBy=%s, sortOrder=%s", sortBy, sortOrder)
			allUIDs, err = client.SortMessages(sortBy, sortOrder, "ALL")
			if err != nil {
				log.Printf("Server SORT failed: %v, falling back to SEARCH", err)
				allUIDs, err = client.Search("ALL")
				if err != nil {
					log.Printf("IMAP search failed: %v", err)
					return nil, fmt.Errorf("search failed: %w", err)
				}
			}
		} else {
			allUIDs, err = client.Search("ALL")
			if err != nil {
				log.Printf("IMAP search failed: %v", err)
				return nil, fmt.Errorf("search failed: %w", err)
			}
		}

		// Cache the UID list with metadata for 5 minutes
		if err := s.setCachedUIDListWithMetadata(ctx, uidCacheKey, allUIDs, len(allUIDs), currentModSeq, client.HasQResync(), 5*time.Minute); err != nil {
			log.Printf("Warning: failed to cache UID list: %v", err)
		}
	} else {
		// No cache available - fetch from IMAP
		// Use server-side SORT if available
		if client.HasSort() {
			log.Printf("Using server-side SORT: sortBy=%s, sortOrder=%s", sortBy, sortOrder)
			allUIDs, err = client.SortMessages(sortBy, sortOrder, "ALL")
			if err != nil {
				log.Printf("Server SORT failed: %v, falling back to SEARCH", err)
				allUIDs, err = client.Search("ALL")
				if err != nil {
					log.Printf("IMAP search failed: %v", err)
					return nil, fmt.Errorf("search failed: %w", err)
				}
			}
		} else {
			allUIDs, err = client.Search("ALL")
			if err != nil {
				log.Printf("IMAP search failed: %v", err)
				return nil, fmt.Errorf("search failed: %w", err)
			}
		}
	}

UseUIDs:
	// Use actual UID count as total (folderInfo.Messages may include deleted messages)
	totalCount := len(allUIDs)

	log.Printf("Found %d messages in folder %s, page=%d, limit=%d", totalCount, folder, cursorData.Page, limit)

	// When using server-side SORT, UIDs are already sorted
	// For client-side sorting or no SORT support, we need to reverse for descending order
	var uids []uint32
	if client.HasSort() {
		// Server already sorted, use as-is
		uids = allUIDs
		log.Printf("Using server-sorted UIDs (first: %v, last: %v)",
			func() uint32 {
				if len(uids) > 0 {
					return uids[0]
				}
				return 0
			}(),
			func() uint32 {
				if len(uids) > 0 {
					return uids[len(uids)-1]
				}
				return 0
			}(),
		)
	} else if sortOrder == models.SortOrderDesc {
		// Reverse for descending order (newest first)
		uids = make([]uint32, 0, len(allUIDs))
		for i := len(allUIDs) - 1; i >= 0; i-- {
			uids = append(uids, allUIDs[i])
		}
		log.Printf("Reversed UIDs for client-side descending sort")
	} else {
		uids = allUIDs
	}

	log.Printf("UIDs prepared: %d items (first: %v, last: %v)", len(uids),
		func() uint32 {
			if len(uids) > 0 {
				return uids[0]
			}
			return 0
		}(),
		func() uint32 {
			if len(uids) > 0 {
				return uids[len(uids)-1]
			}
			return 0
		}(),
	)

	// Phase 3: Validate cursor anchor UID
	// Check if the LastUID in cursor still exists (wasn't deleted)
	// Calculate start index using LastUID for stable pagination
	var startIndex int
	if cursorData.LastUID > 0 && !validateCursorAnchor(cursorData, uids) {
		log.Printf("Cursor anchor UID %d was deleted, adjusting cursor", cursorData.LastUID)
		// Adjust start index to nearest surviving UID
		startIndex = adjustCursorForDeletedAnchor(cursorData, uids, pageSize)
		log.Printf("Adjusted startIndex to %d after anchor deletion", startIndex)
	} else {
		startIndex = cursorData.Page * pageSize
		if cursorData.LastUID > 0 {
			// Find the position of LastUID in the sorted UID list
			for i, uid := range uids {
				if uid == cursorData.LastUID {
					startIndex = i + 1
					log.Printf("Using LastUID %d for stable pagination, startIndex=%d", cursorData.LastUID, startIndex)
					break
				}
			}
		}
	}
	endIndex := startIndex + pageSize

	// Apply pagination to UIDs
	if startIndex >= len(uids) {
		log.Printf("Start index %d beyond available UIDs (%d), returning empty", startIndex, len(uids))
		// Cursor is beyond available messages, return empty
		totalPages := 1
		if totalCount > 0 {
			totalPages = (totalCount + limit - 1) / limit
		}
		messageList := &models.MessageList{
			Messages:    []models.MessageSummary{},
			TotalCount:  totalCount,
			PageSize:    limit,
			CurrentPage: cursorData.Page + 1,
			TotalPages:  totalPages,
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
	// Important: Preserve the sorted order of UIDs when fetching
	var allEnvelopes []pool.MessageEnvelope
	if len(pageUIDs) > 0 {
		// Create a map to store envelopes by UID for reordering
		envelopeMap := make(map[uint32]pool.MessageEnvelope)

		for i := 0; i < len(pageUIDs); i += maxUIDsPerFetch {
			batchEnd := i + maxUIDsPerFetch
			if batchEnd > len(pageUIDs) {
				batchEnd = len(pageUIDs)
			}
			batch := pageUIDs[i:batchEnd]

			// IMAP FETCH requires UIDs in ascending order, sort the batch for fetching
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

			// Store envelopes in map by UID
			for _, env := range batchEnvelopes {
				envelopeMap[env.UID] = env
			}
		}

		// Reconstruct envelopes in the original sorted order (from pageUIDs)
		for _, uid := range pageUIDs {
			if env, ok := envelopeMap[uid]; ok {
				allEnvelopes = append(allEnvelopes, env)
			}
		}
	}

	log.Printf("Total envelopes fetched: %d", len(allEnvelopes))

	// Convert to MessageSummary
	messages := s.convertToMessageSummary(allEnvelopes, folder)

	// Apply client-side sort only if server-side SORT is not available
	// When using server-side SORT, messages are already in correct order
	if !client.HasSort() {
		log.Printf("Applying client-side sort: sortBy=%s, sortOrder=%s", sortBy, sortOrder)
		log.Printf("Before sort: first message date=%s", func() string {
			if len(messages) > 0 {
				return messages[0].Date.String()
			}
			return "N/A"
		}())
		messages = s.sortMessages(messages, sortBy, sortOrder)
		log.Printf("After sort: first message date=%s, last=%s", func() string {
			if len(messages) > 0 {
				return messages[0].Date.String()
			}
			return "N/A"
		}(), func() string {
			if len(messages) > 0 {
				return messages[len(messages)-1].Date.String()
			}
			return "N/A"
		}())
	} else {
		log.Printf("Server-side SORT used, skipping client-side sort")
	}

	// Preload first 3 and last 3 message envelopes for instant display
	// This ensures content is ready regardless of scroll position after sort
	s.preloadStrategicMessages(ctx, accountID, messages)

	// Calculate next cursor based on actual UIDs, not totalCount
	// Include LastUID for stable pagination (prevents duplicates when new emails arrive)
	var nextCursor string
	if endIndex < len(uids) {
		nextCursorData := CursorData{
			Page:      cursorData.Page + 1,
			LastUID:   pageUIDs[len(pageUIDs)-1], // Last UID on current page for stable navigation
			SortBy:    sortBy,
			SortOrder: sortOrder,
			Timestamp: time.Now(),
		}
		nextCursor, _ = encodeCursor(nextCursorData)
	}

	// Deduct tokens
	_ = cost

	// Calculate total pages
	totalPages := 1
	if totalCount > 0 {
		totalPages = (totalCount + limit - 1) / limit // Ceiling division
	}

	messageList := &models.MessageList{
		Messages:    messages,
		TotalCount:  totalCount,
		PageSize:    limit,
		CurrentPage: cursorData.Page + 1,
		TotalPages:  totalPages,
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

	// Try cache first - check primary key (account/folder/uid)
	cachedMsg, err := s.getCachedMessage(ctx, accountID, folder, uid)
	if err == nil && cachedMsg != nil {
		// Add cache metadata
		if cachedMsg.ProcessingMetadata == nil {
			cachedMsg.ProcessingMetadata = &models.ProcessingMetadata{}
		}
		cachedMsg.ProcessingMetadata.CacheStatus = "hit"
		log.Printf("Message cache hit: account=%s, folder=%s, uid=%s", accountID, folder, uid)
		return cachedMsg, nil
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

	client, release, err := s.sessions.Acquire(imapCtx, accountID, imapConfig)
	if err != nil {
		// Check for authentication errors
		if errors.Is(err, models.ErrMailServerAuthFailed) {
			return nil, models.NewAuthError("Invalid mail server credentials")
		}
		return nil, models.NewServiceUnavailableError("IMAP server", err.Error())
	}
	defer release()

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

	// Set folder and UID context on the parsed message
	// (MIME parser doesn't have this context, so we add it here)
	parseResult.Message.Folder = folder
	parseResult.Message.UID = uid

	// Add cache metadata
	if parseResult.Message.ProcessingMetadata == nil {
		parseResult.Message.ProcessingMetadata = &models.ProcessingMetadata{}
	}
	parseResult.Message.ProcessingMetadata.CacheStatus = "miss"
	parseResult.Message.ProcessingMetadata.ProcessingTime = time.Since(time.Now()).Milliseconds()
	parseResult.Message.ProcessingMetadata.SizeOriginal = int64(len(rawData))

	// Cache the message with content-based deduplication
	if err := s.setCachedMessageWithDedup(ctx, accountID, parseResult.Message); err != nil {
		log.Printf("Warning: failed to cache message: %v", err)
	}

	// Mark message as seen (optional)
	// client.MarkSeen(uint32(uidNum))

	log.Printf("Successfully parsed message %s: %d bytes (cached)", uid, len(rawData))
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

	client, release, err := s.sessions.Acquire(imapCtx, query.AccountID, imapConfig)
	if err != nil {
		// Check for authentication errors
		if errors.Is(err, models.ErrMailServerAuthFailed) {
			return nil, models.NewAuthError("Invalid mail server credentials")
		}
		return nil, models.NewServiceUnavailableError("IMAP server", err.Error())
	}
	defer release()

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

// getCachedUIDList retrieves cached UID list for a folder
func (s *MessageService) getCachedUIDList(ctx context.Context, cacheKey string) ([]uint32, error) {
	data, err := s.cache.Get(ctx, cacheKey)
	if err != nil {
		return nil, err
	}

	// Try new format first (with metadata)
	var cachedData struct {
		UIDs           []uint32 `json:"uids"`
		MessageCount   int      `json:"message_count"`
		HighestModSeq  uint64   `json:"highest_modseq,omitempty"`
		CachedAt       int64    `json:"cached_at"`
		QResyncCapable bool     `json:"qresync_capable"`
	}

	if err := json.Unmarshal(data, &cachedData); err == nil && cachedData.UIDs != nil {
		return cachedData.UIDs, nil
	}

	// Fallback to old format (just UID array)
	var uids []uint32
	if err := json.Unmarshal(data, &uids); err != nil {
		log.Printf("Failed to unmarshal cached UID list: %v", err)
		return nil, err
	}

	return uids, nil
}

// setCachedUIDList stores UID list in cache with specified TTL
func (s *MessageService) setCachedUIDList(
	ctx context.Context,
	cacheKey string,
	uids []uint32,
	ttl time.Duration,
) error {
	cachedData := struct {
		UIDs           []uint32 `json:"uids"`
		MessageCount   int      `json:"message_count"`
		HighestModSeq  uint64   `json:"highest_modseq,omitempty"`
		CachedAt       int64    `json:"cached_at"`
		QResyncCapable bool     `json:"qresync_capable"`
	}{
		UIDs:           uids,
		MessageCount:   len(uids),
		CachedAt:       time.Now().Unix(),
		QResyncCapable: false, // Will be updated when QRESYNC is detected
	}

	data, err := json.Marshal(cachedData)
	if err != nil {
		log.Printf("Failed to marshal UID list for cache: %v", err)
		return err
	}

	if err := s.cache.Set(ctx, cacheKey, data, ttl); err != nil {
		log.Printf("Failed to cache UID list: %v", err)
		return err
	}

	log.Printf("Cached UID list: key=%s, count=%d", cacheKey, len(uids))
	return nil
}

// setCachedUIDListWithMetadata stores UID list with full metadata (QRESYNC support)
func (s *MessageService) setCachedUIDListWithMetadata(
	ctx context.Context,
	cacheKey string,
	uids []uint32,
	messageCount int,
	highestModSeq uint64,
	qresyncCapable bool,
	ttl time.Duration,
) error {
	cachedData := struct {
		UIDs           []uint32 `json:"uids"`
		MessageCount   int      `json:"message_count"`
		HighestModSeq  uint64   `json:"highest_modseq,omitempty"`
		CachedAt       int64    `json:"cached_at"`
		QResyncCapable bool     `json:"qresync_capable"`
	}{
		UIDs:           uids,
		MessageCount:   messageCount,
		HighestModSeq:  highestModSeq,
		CachedAt:       time.Now().Unix(),
		QResyncCapable: qresyncCapable,
	}

	data, err := json.Marshal(cachedData)
	if err != nil {
		log.Printf("Failed to marshal UID list for cache: %v", err)
		return err
	}

	if err := s.cache.Set(ctx, cacheKey, data, ttl); err != nil {
		log.Printf("Failed to cache UID list: %v", err)
		return err
	}

	log.Printf("Cached UID list with metadata: key=%s, count=%d, modseq=%d", cacheKey, len(uids), highestModSeq)
	return nil
}

// getCachedUIDListWithMetadata retrieves cached UID list with full metadata
func (s *MessageService) getCachedUIDListWithMetadata(
	ctx context.Context,
	cacheKey string,
) (uids []uint32, messageCount int, highestModSeq uint64, qresyncCapable bool, err error) {
	data, err := s.cache.Get(ctx, cacheKey)
	if err != nil {
		return nil, 0, 0, false, err
	}

	var cachedData struct {
		UIDs           []uint32 `json:"uids"`
		MessageCount   int      `json:"message_count"`
		HighestModSeq  uint64   `json:"highest_modseq,omitempty"`
		CachedAt       int64    `json:"cached_at"`
		QResyncCapable bool     `json:"qresync_capable"`
	}

	if err := json.Unmarshal(data, &cachedData); err != nil {
		return nil, 0, 0, false, err
	}

	return cachedData.UIDs, cachedData.MessageCount, cachedData.HighestModSeq, cachedData.QResyncCapable, nil
}

// generateQueryHash generates a hash for search query caching
func (s *MessageService) generateQueryHash(query models.SearchQuery) string {
	data, _ := json.Marshal(query)
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash)[:32]
}

// generateContentHash generates a content-based hash for message deduplication
// Uses Message-ID, Subject, and first 10KB of plain text body
func (s *MessageService) generateContentHash(msg *models.Message) string {
	if msg == nil {
		return ""
	}

	// Build hash components
	var builder strings.Builder
	builder.WriteString(msg.MessageID)
	builder.WriteString("|")
	builder.WriteString(msg.Subject)
	builder.WriteString("|")

	// Add body content (truncate to 10KB for hashing)
	if msg.Body != nil {
		bodyText := msg.Body.PlainText
		if bodyText == "" {
			bodyText = msg.Body.Text
		}
		const maxBodyLen = 10240 // 10KB
		if len(bodyText) > maxBodyLen {
			bodyText = bodyText[:maxBodyLen]
		}
		builder.WriteString(bodyText)
	}

	// Generate SHA-256 hash
	hash := sha256.Sum256([]byte(builder.String()))
	return fmt.Sprintf("%x", hash)[:16] // Return first 16 hex chars
}

// buildContentCacheKey builds the cache key for content-based deduplication
func (s *MessageService) buildContentCacheKey(contentHash string) string {
	return cachekey.ContentHashKeySafe(contentHash)
}

// buildMessageCacheKey builds the primary cache key for a message
// Uses safe builder that defaults folder to INBOX if empty
func (s *MessageService) buildMessageCacheKey(accountID, folder, uid string) string {
	return cachekey.MessageKeySafe(accountID, folder, uid)
}

// getCachedMessageByContent tries to get a message from cache using content hash
// Returns the message if found via content-based deduplication
func (s *MessageService) getCachedMessageByContent(
	ctx context.Context,
	accountID string,
	msg *models.Message,
) (*models.Message, error) {
	if s.cache == nil {
		return nil, nil
	}

	// Generate content hash
	contentHash := s.generateContentHash(msg)
	if contentHash == "" {
		return nil, nil
	}

	// Try to get the primary key from content hash index
	hashKey := s.buildContentCacheKey(contentHash)
	data, err := s.cache.Get(ctx, hashKey)
	if err != nil || len(data) == 0 {
		return nil, nil // Cache miss
	}

	// Get the primary cache key from the hash index
	primaryKey := string(data)

	// Now fetch the actual message from the primary key
	msgData, err := s.cache.Get(ctx, primaryKey)
	if err != nil || len(msgData) == 0 {
		// Hash index points to non-existent message, clean up
		_ = s.cache.Delete(ctx, hashKey)
		return nil, nil
	}

	// Unmarshal message
	var cachedMsg models.Message
	if err := json.Unmarshal(msgData, &cachedMsg); err != nil {
		log.Printf("Failed to unmarshal cached message: %v", err)
		return nil, err
	}

	log.Printf("Cache hit via content hash: hash=%s, primaryKey=%s", contentHash, primaryKey)
	return &cachedMsg, nil
}

// setCachedMessageWithDedup stores a message in cache with content-based deduplication
// Uses both primary key (account/folder/uid) and content hash index
func (s *MessageService) setCachedMessageWithDedup(
	ctx context.Context,
	accountID string,
	msg *models.Message,
) error {
	if s.cache == nil {
		return nil
	}

	// Build primary cache key with validation
	primaryKey := s.buildMessageCacheKey(accountID, msg.Folder, msg.UID)
	if primaryKey == "" {
		log.Printf("Warning: skipping cache for message with invalid key - account=%s, folder=%s, uid=%s", accountID, msg.Folder, msg.UID)
		return nil
	}

	// Marshal message
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal message for cache: %v", err)
		return err
	}

	// Store message at primary key with 24 hour TTL
	if err := s.cache.Set(ctx, primaryKey, data, cache.TTLMessage); err != nil {
		log.Printf("Failed to cache message: %v", err)
		return err
	}

	// Generate content hash and create index entry for deduplication
	contentHash := s.generateContentHash(msg)
	if contentHash != "" {
		hashKey := s.buildContentCacheKey(contentHash)
		// Store primary key reference with same TTL
		if err := s.cache.Set(ctx, hashKey, []byte(primaryKey), cache.TTLMessage); err != nil {
			log.Printf("Warning: failed to store content hash index: %v", err)
			// Non-fatal, continue
		}
	}

	log.Printf("Cached message with dedup: key=%s, hash=%s", primaryKey, contentHash)
	return nil
}

// getCachedMessage retrieves a message from cache using primary key
func (s *MessageService) getCachedMessage(
	ctx context.Context,
	accountID, folder, uid string,
) (*models.Message, error) {
	if s.cache == nil {
		return nil, nil
	}

	cacheKey := s.buildMessageCacheKey(accountID, folder, uid)
	data, err := s.cache.Get(ctx, cacheKey)
	if err != nil || len(data) == 0 {
		return nil, nil // Cache miss
	}

	var msg models.Message
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("Failed to unmarshal cached message: %v", err)
		return nil, err
	}

	log.Printf("Cache hit: key=%s", cacheKey)
	return &msg, nil
}

// deleteCachedMessage removes a message from cache (both primary key and content hash)
func (s *MessageService) deleteCachedMessage(
	ctx context.Context,
	accountID, folder, uid string,
	msg *models.Message,
) error {
	if s.cache == nil {
		return nil
	}

	// Delete primary key
	primaryKey := s.buildMessageCacheKey(accountID, folder, uid)
	if err := s.cache.Delete(ctx, primaryKey); err != nil {
		log.Printf("Warning: failed to delete message from cache: %v", err)
	}

	// Delete content hash index if message is provided
	if msg != nil {
		contentHash := s.generateContentHash(msg)
		if contentHash != "" {
			hashKey := s.buildContentCacheKey(contentHash)
			_ = s.cache.Delete(ctx, hashKey)
		}
	}

	log.Printf("Deleted message from cache: key=%s", primaryKey)
	return nil
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

	client, release, err := s.sessions.Acquire(imapCtx, accountID, imapConfig)
	if err != nil {
		if errors.Is(err, models.ErrMailServerAuthFailed) {
			return models.NewAuthError("Invalid mail server credentials")
		}
		return models.NewServiceUnavailableError("IMAP server", err.Error())
	}
	defer release()

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

	// Remove from cache (primary key only, content hash will be cleaned up on expiration)
	if err := s.deleteCachedMessage(ctx, accountID, folder, uid, nil); err != nil {
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

	client, release, err := s.sessions.Acquire(imapCtx, accountID, imapConfig)
	if err != nil {
		if errors.Is(err, models.ErrMailServerAuthFailed) {
			return models.NewAuthError("Invalid mail server credentials")
		}
		return models.NewServiceUnavailableError("IMAP server", err.Error())
	}
	defer release()

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

	client, release, err := s.sessions.Acquire(imapCtx, accountID, imapConfig)
	if err != nil {
		if errors.Is(err, models.ErrMailServerAuthFailed) {
			return nil, models.NewAuthError("Invalid mail server credentials")
		}
		return nil, models.NewServiceUnavailableError("IMAP server", err.Error())
	}
	defer release()

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

	client, release, err := s.sessions.Acquire(imapCtx, accountID, imapConfig)
	if err != nil {
		if errors.Is(err, models.ErrMailServerAuthFailed) {
			return nil, models.NewAuthError("Invalid mail server credentials")
		}
		return nil, models.NewServiceUnavailableError("IMAP server", err.Error())
	}
	defer release()

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

// preloadStrategicMessages preloads the first 3 and last 3 message envelopes
// to ensure instant display at both ends of the sorted list.
// This is called after sorting, so it preloads based on the final display order.
func (s *MessageService) preloadStrategicMessages(ctx context.Context, accountID string, messages []models.MessageSummary) {
	if s.cache == nil || len(messages) == 0 {
		return
	}

	// Collect UIDs to preload: first 3 and last 3
	// Avoid duplicates when list has fewer than 6 messages
	toPreload := make([]models.MessageSummary, 0, 6)

	// Preload first 3 (what user sees at top of list)
	for i := 0; i < min(3, len(messages)); i++ {
		toPreload = append(toPreload, messages[i])
	}

	// Preload last 3 (what user sees when scrolling to bottom)
	// Start from max(3, len-3) to avoid duplicates if len < 6
	startIdx := max(3, len(messages)-3)
	for i := startIdx; i < len(messages); i++ {
		toPreload = append(toPreload, messages[i])
	}

	if len(toPreload) == 0 {
		return
	}

	// Cache envelopes in background (non-blocking)
	go func() {
		if err := s.cache.SetEnvelopes(ctx, accountID, toPreload); err != nil {
			log.Printf("Warning: failed to preload %d envelopes: %v", len(toPreload), err)
		} else {
			log.Printf("Preloaded %d envelopes: first=%s, last=%s", len(toPreload),
				toPreload[0].Subject, toPreload[len(toPreload)-1].Subject)
		}
	}()
}

// CursorData represents pagination cursor data for stable navigation
type CursorData struct {
	Page      int              `json:"page"`
	LastUID   uint32           `json:"last_uid,omitempty"` // Last UID from previous page for stable pagination
	SortBy    models.SortField `json:"sort_by"`
	SortOrder models.SortOrder `json:"sort_order"`
	Timestamp time.Time        `json:"timestamp"`
}

// LegacyCursorData represents the old cursor format (for backward compatibility)
type LegacyCursorData struct {
	Offset int `json:"offset,omitempty"`
	Page   int `json:"page,omitempty"`
}

// encodeCursor encodes cursor data to a base64 string
func encodeCursor(data CursorData) (string, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(jsonData), nil
}

// decodeCursor decodes a base64 cursor string to CursorData.
// It handles both the current format and legacy {offset, page} format for backward compatibility.
// If decoding fails, it returns a default cursor starting from page 0.
func decodeCursor(cursor string) (CursorData, error) {
	var data CursorData

	if cursor == "" {
		return data, nil
	}

	jsonData, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		log.Printf("Cursor base64 decode failed: %v, starting from page 0", err)
		return data, nil
	}

	// Try to decode as current format first
	err = json.Unmarshal(jsonData, &data)
	if err == nil && data.Page >= 0 {
		return data, nil
	}

	// Try legacy format {offset, page}
	var legacy LegacyCursorData
	if err := json.Unmarshal(jsonData, &legacy); err == nil {
		if legacy.Page > 0 {
			data.Page = legacy.Page - 1 // Convert 1-based to 0-based
		} else if legacy.Offset > 0 {
			data.Page = legacy.Offset / 50 // Assume default page size of 50
		}
		log.Printf("Legacy cursor format detected, converted to page %d", data.Page)
		return data, nil
	}

	// If all decoding fails, start from page 0
	log.Printf("Invalid cursor format, starting from page 0")
	return data, nil
}

// abs returns the absolute value of an integer
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// removeUIDsFromList removes specified UIDs from a UID list
func removeUIDsFromList(uids []uint32, toRemove []uint32) []uint32 {
	if len(toRemove) == 0 {
		return uids
	}

	// Create a set of UIDs to remove for O(1) lookup
	removeSet := make(map[uint32]struct{}, len(toRemove))
	for _, uid := range toRemove {
		removeSet[uid] = struct{}{}
	}

	// Filter out removed UIDs
	result := make([]uint32, 0, len(uids))
	for _, uid := range uids {
		if _, found := removeSet[uid]; !found {
			result = append(result, uid)
		}
	}

	return result
}

// validateCursorAnchor checks if the anchor UID in cursor still exists in the UID list
// Returns true if valid, false if anchor was deleted
func validateCursorAnchor(cursor CursorData, uids []uint32) bool {
	if cursor.LastUID == 0 {
		return true // First page, no anchor to validate
	}

	for _, uid := range uids {
		if uid == cursor.LastUID {
			return true
		}
	}

	return false // Anchor UID not found - was deleted
}

// adjustCursorForDeletedAnchor adjusts cursor when anchor UID was deleted
// Finds the nearest surviving UID and returns adjusted start index
func adjustCursorForDeletedAnchor(cursor CursorData, uids []uint32, pageSize int) int {
	if cursor.LastUID == 0 {
		return cursor.Page * pageSize
	}

	// Find position where anchor UID would have been
	insertPos := sort.Search(len(uids), func(i int) bool {
		return uids[i] >= cursor.LastUID
	})

	// Use the position as new start index
	// This shows messages that were "next" after the deleted one
	if insertPos >= len(uids) {
		// All remaining UIDs deleted, return to beginning
		return 0
	}

	return insertPos
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

// GetPoolStats returns statistics about the IMAP session pool
func (s *MessageService) GetPoolStats() pool.SessionPoolStats {
	return s.sessions.Stats()
}
