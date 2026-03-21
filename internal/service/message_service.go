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
	"webmail_engine/internal/messagecache"
	"webmail_engine/internal/mimeparser"
	"webmail_engine/internal/models"
	"webmail_engine/internal/pool"
	"webmail_engine/internal/scheduler"
	"webmail_engine/internal/storage"

	"github.com/emersion/go-imap/v2"
)

// MessageService handles message operations
type MessageService struct {
	mu               sync.RWMutex
	sessions         *pool.IMAPSessionPool
	cache            *cache.Cache
	messageListCache *messagecache.MessageListCache
	uidListCache     *messagecache.UIDListCache
	scheduler        *scheduler.FairUseScheduler
	parser           *mimeparser.MIMEParser
	storage          storage.AttachmentStorage
	accountService   *AccountService
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
	storage := storage.NewFileAttachmentStorage(config.TempStoragePath)

	// Initialize cache helpers
	messageListCache := messagecache.NewMessageListCache(cache)
	uidListCache := messagecache.NewUIDListCache(cache)

	return &MessageService{
		sessions:         sessions,
		cache:            cache,
		messageListCache: messageListCache,
		uidListCache:     uidListCache,
		scheduler:        scheduler,
		accountService:   accountService,
		parser:           parser,
		storage:          storage,
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

	// Log cursor details for pagination debugging
	log.Printf("[PAGINATION] GetMessageList: accountID=%s, folder=%s, cursor.page=%d, cursor.last_uid=%d, limit=%d, sortBy=%s, sortOrder=%s",
		accountID, folder, cursorData.Page, cursorData.LastUID, limit, sortBy, sortOrder)

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

	uidValidity := folderInfo.UIDValidity
	highestModSeq := folderInfo.HighestModSeq

	// Build cache key WITHOUT modseq - modseq is checked on retrieval for smart invalidation
	cacheKey := s.messageListCache.BuildKey(
		accountID, folder, cursor, limit, sortBy, sortOrder, uidValidity)

	log.Printf("Cache key built: accountID=%s, folder=%s, limit=%d, sortBy=%s, sortOrder=%s, uidValidity=%d, currentModSeq=%d",
		accountID, folder, limit, sortBy, sortOrder, uidValidity, highestModSeq)

	// Try cache first with smart modseq checking
	if s.messageListCache != nil {
		cachedList, cacheHit := s.messageListCache.Get(ctx, cacheKey, highestModSeq)
		if cacheHit && cachedList != nil {
			_ = cost // Don't deduct tokens for cache hit
			log.Printf("Cache HIT with modseq %d for folder %s (page %d)", highestModSeq, folder, cachedList.CurrentPage)
			return cachedList, nil
		}
		// Cache miss or page 1 with modseq change - fetch from IMAP
		log.Printf("Cache MISS (modseq changed or first request)")
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
	uidCacheKey := s.uidListCache.BuildKey(accountID, folder, uidValidity)
	var allUIDs []uint32

	// Get current folder info for comparison
	currentMessageCount := folderInfo.Messages
	currentModSeq := folderInfo.HighestModSeq

	if s.uidListCache != nil {
		// Try to get cached UID list with full metadata
		cachedMetadata, err := s.uidListCache.Get(ctx, uidCacheKey)

		if err == nil && cachedMetadata != nil {
			// Use new smart cache validation
			if s.isCacheValid(cachedMetadata, currentModSeq, currentMessageCount, sortBy, sortOrder) {
				log.Printf("UID cache HIT: %d UIDs for folder %s (modseq=%d, sort=%s %s)",
					len(cachedMetadata.UIDs), folder, cachedMetadata.HighestModSeq, cachedMetadata.SortField, cachedMetadata.SortOrder)
				allUIDs = cachedMetadata.UIDs
				goto UseUIDs
			}
			log.Printf("UID cache INVALID, refreshing for folder %s", folder)
		}

		// Cache miss or refresh needed - fetch from IMAP
		log.Printf("UID cache MISS for folder %s, fetching from IMAP", folder)

		// Determine sorting strategy based on mailbox size
		var sortStrategy string
		var sortDuration time.Duration

		switch {
		case currentMessageCount < sortThreshold:
			// Small mailbox: Use server-side SORT
			sortStrategy = "server_sort"
			if client.HasSort() {
				log.Printf("Using server-side SORT: sortBy=%s, sortOrder=%s (%d messages < threshold %d)",
					sortBy, sortOrder, currentMessageCount, sortThreshold)
				sortStart := time.Now()
				allUIDs, err = client.SortMessages(sortBy, sortOrder, "ALL")
				sortDuration = time.Since(sortStart)

				if err != nil {
					log.Printf("Server SORT failed after %v: %v", sortDuration, err)
					allUIDs, err = s.handleSortFailure(ctx, client, folder, accountID, imapConfig, release, sortBy, sortOrder, err)
					if err != nil {
						return nil, err
					}
					sortStrategy = "fallback_search"
				} else {
					log.Printf("SORT completed in %v: %d UIDs", sortDuration, len(allUIDs))
					if sortDuration > 5*time.Second {
						log.Printf("WARN: SORT took >5s, consider date-range filtering for this mailbox")
					}
				}
			} else {
				// Server doesn't support SORT
				log.Printf("Server doesn't support SORT, using SEARCH")
				allUIDs, err = client.Search("ALL")
				sortStrategy = "search_no_sort_cap"
			}

		case currentMessageCount <= maxSortUIDs:
			// Medium-large mailbox: Use date-range filtering
			sortStrategy = "date_range"
			log.Printf("Using date-range filter (%d messages >= threshold %d)", currentMessageCount, sortThreshold)
			allUIDs, err = s.searchByDateRange(ctx, client, folder, sortBy, sortOrder, largeMailboxRecentDays)
			if err != nil {
				log.Printf("Date-range search failed: %v, falling back to SEARCH", err)
				allUIDs, err = client.Search("ALL")
				sortStrategy = "fallback_search"
			}

		default:
			// Very large mailbox: Limited SEARCH
			sortStrategy = "limited_search"
			log.Printf("Mailbox too large for SORT (%d > %d), using limited SEARCH", currentMessageCount, maxSortUIDs)
			allUIDs, err = client.Search("ALL")
			if err == nil && len(allUIDs) > maxSortUIDs {
				// Take last N UIDs (most recent in UID order)
				allUIDs = allUIDs[len(allUIDs)-maxSortUIDs:]
				log.Printf("Limited to last %d UIDs", len(allUIDs))
			}
		}

		if err != nil {
			log.Printf("IMAP search failed: %v", err)
			return nil, fmt.Errorf("search failed: %w", err)
		}

		// Validate UID list before caching to prevent caching nil data
		// Note: Empty UID list (len=0) is valid and SHOULD be cached
		// This handles cases like: empty folder, all messages deleted, empty search results
		if allUIDs == nil {
			allUIDs = []uint32{}
		}

		// Cache the UID list with metadata (including empty slices)
		if err := s.uidListCache.Set(ctx, uidCacheKey, allUIDs, len(allUIDs), currentModSeq, client.HasQResync(), string(sortBy), string(sortOrder), "ALL"); err != nil {
			log.Printf("Warning: failed to cache UID list: %v", err)
		}

		// Log sorting strategy metrics
		log.Printf("SORT_STATS: account=%s folder=%s strategy=%s message_count=%d duration_ms=%d uids_returned=%d",
			accountID, folder, sortStrategy, currentMessageCount, sortDuration.Milliseconds(), len(allUIDs))
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

	// Phase 3: Calculate start index for pagination
	// Primary: Use page-based calculation for consistent pagination
	// Secondary: Use LastUID only for stable pagination when messages are added/deleted
	var startIndex int

	// Base calculation: page * pageSize
	startIndex = cursorData.Page * pageSize

	// If LastUID is provided, use it for stable pagination (only if it matches expected position)
	// This handles cases where messages are added/removed between page loads
	if cursorData.LastUID > 0 {
		expectedLastUIDIndex := (cursorData.Page * pageSize) - 1
		if expectedLastUIDIndex >= 0 && expectedLastUIDIndex < len(uids) {
			// Check if the LastUID matches what we expect at the end of previous page
			if uids[expectedLastUIDIndex] == cursorData.LastUID {
				// LastUID matches expected position, use page-based startIndex
				log.Printf("LastUID %d matches expected position %d, using page-based startIndex=%d",
					cursorData.LastUID, expectedLastUIDIndex, startIndex)
			} else {
				// LastUID doesn't match - messages were added/removed
				// Find actual position of LastUID and adjust
				for i, uid := range uids {
					if uid == cursorData.LastUID {
						startIndex = i + 1
						log.Printf("LastUID %d at position %d (expected %d), adjusted startIndex=%d",
							cursorData.LastUID, i, expectedLastUIDIndex, startIndex)
						break
					}
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

	// Cache the message list with current modseq for smart invalidation
	if s.messageListCache != nil {
		// Validate message list before caching
		// Note: nil check is critical - nil means error/invalid
		// Empty Messages slice (len=0) is valid and SHOULD be cached
		// This handles: empty folder, all messages deleted, empty page (e.g., page 10 of 5)
		if messageList == nil {
			log.Printf("Skipping message list cache write: messageList is nil for folder %s, page %d", folder, cursorData.Page+1)
		} else {
			if err := s.messageListCache.Set(ctx, cacheKey, messageList, highestModSeq); err != nil {
				log.Printf("Warning: failed to cache message list: %v", err)
			}
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
		// If flags field is nil, it might be an old cache entry without flags.
		// Force a refresh from IMAP to populate them.
		if cachedMsg.Flags == nil {
			log.Printf("[DEBUG] Message cache hit but flags are nil, forcing refresh: account=%s, folder=%s, uid=%s", accountID, folder, uid)
		} else {
			// Add cache metadata
			if cachedMsg.ProcessingMetadata == nil {
				cachedMsg.ProcessingMetadata = &models.ProcessingMetadata{}
			}
			cachedMsg.ProcessingMetadata.CacheStatus = "hit"
			log.Printf("[DEBUG] Message cache hit: account=%s, folder=%s, uid=%s, flags=%v", accountID, folder, uid, cachedMsg.Flags)
			return cachedMsg, nil
		}
	}
	log.Printf("[DEBUG] Message cache miss: account=%s, folder=%s, uid=%s", accountID, folder, uid)

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

	// Fetch raw message along with flags
	log.Printf("Fetching message %s from folder %s", uid, folder)
	rawData, imapFlags, err := client.FetchMessageRawWithFlags(uint32(uidNum))
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

	// Set folder, UID and flags context on the parsed message
	// (MIME parser doesn't have this context, so we add it here)
	parseResult.Message.Folder = folder
	parseResult.Message.UID = uid
	parseResult.Message.Flags = s.parseFlags(imapFlags)
	log.Printf("[DEBUG] Message live fetch: account=%s, folder=%s, uid=%s, imapFlags=%v, parsedFlags=%v", 
		accountID, folder, uid, imapFlags, parseResult.Message.Flags)

	// Add cache metadata
	if parseResult.Message.ProcessingMetadata == nil {
		parseResult.Message.ProcessingMetadata = &models.ProcessingMetadata{}
	}
	parseResult.Message.ProcessingMetadata.CacheStatus = "miss"
	parseResult.Message.ProcessingMetadata.ProcessingTime = time.Since(time.Now()).Milliseconds()
	parseResult.Message.ProcessingMetadata.SizeOriginal = int64(len(rawData))

	// Store ALL attachments from parse result BEFORE caching
	// This ensures attachment IDs in the message match what's in storage
	for i := range parseResult.Attachments {
		att := &parseResult.Attachments[i]
		if att.Data != nil {
			id, err := s.storage.Store(accountID, folder, uid, att.Filename, att.Data)
			if err != nil {
				log.Printf("Warning: failed to store attachment %s: %v", att.Filename, err)
				continue
			}
			att.ID = id

			// Update corresponding message attachment
			for j := range parseResult.Message.Attachments {
				msgAtt := &parseResult.Message.Attachments[j]
				if msgAtt.ID == id || msgAtt.Filename == att.Filename {
					msgAtt.ID = id
					break
				}
			}
		}
	}

	// Cache the message with content-based deduplication
	if err := s.setCachedMessageWithDedup(ctx, accountID, parseResult.Message); err != nil {
		log.Printf("Warning: failed to cache message: %v", err)
	}

	// Generate signed URLs for attachments
	for i := range parseResult.Message.Attachments {
		att := &parseResult.Message.Attachments[i]
		if att.AccessURL == "" {
			baseURL := fmt.Sprintf("/v1/accounts/%s/messages/%s/attachments", accountID, uid)
			signedURL, expiry := mimeparser.GenerateSignedURL(
				att.ID,
				baseURL,
				"secret-key",
				24*time.Hour,
			)
			att.AccessURL = signedURL
			att.URLExpiry = &expiry
		}
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

	// Reverse messages so newest results appear first
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
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

	// Store large attachments (note: attachment data is in memory at this point)
	// Storage happens in GetMessage() where we have accountID, folder, uid context

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

// ==================== Legacy Cache Methods (Deprecated) ====================
// These methods are kept for backward compatibility but should not be used.
// Use s.messageListCache and s.uidListCache instead.

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
	cacheContext *models.CacheContext,
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
	folderInfo, err := client.SelectFolder(folder)
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

	// Targeted cache invalidation
	if cacheContext != nil {
		cacheKey := s.messageListCache.BuildKey(
			accountID, folder, cacheContext.Cursor, cacheContext.Limit, cacheContext.SortBy, cacheContext.SortOrder, folderInfo.UIDValidity)

		if data, getErr := s.cache.Get(ctx, cacheKey); getErr == nil && len(data) > 0 {
			var cachedList models.MessageList
			if unmarshalErr := json.Unmarshal(data, &cachedList); unmarshalErr == nil {
				found := false
				for _, msg := range cachedList.Messages {
					if msg.UID == uid {
						found = true
						break
					}
				}
				if found {
					if delErr := s.cache.Delete(ctx, cacheKey); delErr != nil {
						log.Printf("Warning: failed to delete targeted cache key %s: %v", cacheKey, delErr)
					} else {
						log.Printf("Successfully invalidated targeted cache page for folder %s", folder)
					}
				} else {
					log.Printf("Targeted cache invalidation bypassed: UID not found in cache page")
				}
			}
		}
	} else {
		// Invalidate message list cache for this folder
		if err := s.invalidateMessageListCache(ctx, accountID, folder); err != nil {
			log.Printf("Warning: failed to invalidate message list cache: %v", err)
		}
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
	cacheContext *models.CacheContext,
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
		if err := s.DeleteMessage(ctx, accountID, uid, folder, permanent, cacheContext); err != nil {
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
	cacheContext *models.CacheContext,
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
	folderInfo, err := client.SelectFolder(folder)
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

	// Remove from individual cache
	for _, uid := range uids {
		_ = s.deleteCachedMessage(ctx, accountID, folder, uid, nil)
	}

	// Targeted cache invalidation
	if cacheContext != nil {
		cacheKey := s.messageListCache.BuildKey(
			accountID, folder, cacheContext.Cursor, cacheContext.Limit, cacheContext.SortBy, cacheContext.SortOrder, folderInfo.UIDValidity)

		// Verify uid belongs to this cache page before deleting
		if data, getErr := s.cache.Get(ctx, cacheKey); getErr == nil && len(data) > 0 {
			var cachedList models.MessageList
			if unmarshalErr := json.Unmarshal(data, &cachedList); unmarshalErr == nil {
				found := false
				for _, msg := range cachedList.Messages {
					for _, targetUID := range uids {
						if msg.UID == targetUID {
							found = true
							break
						}
					}
					if found {
						break
					}
				}
				if found {
					if delErr := s.cache.Delete(ctx, cacheKey); delErr != nil {
						log.Printf("Warning: failed to delete targeted cache key %s: %v", cacheKey, delErr)
					} else {
						log.Printf("Successfully invalidated targeted cache page for folder %s", folder)
					}
				} else {
					log.Printf("Targeted cache invalidation bypassed: UIDs not found in cache page")
				}
			}
		}
	} else {
		// Fallback to flushing entire cache if no context provided
		if err := s.invalidateMessageListCache(ctx, accountID, folder); err != nil {
			log.Printf("Warning: failed to invalidate message list cache: %v", err)
		}
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
		// Get folder stats by selecting the folder
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

		// Select folder to get accurate counts (Unseen, Messages, etc.)
		selectedInfo, err := client.SelectFolder(f.Name)
		if err == nil && selectedInfo != nil {
			// Use the selected folder info for accurate counts
			folderInfo.Messages = selectedInfo.Messages
			folderInfo.Recent = selectedInfo.Recent
			folderInfo.Unseen = selectedInfo.Unseen
			folderInfo.UIDNext = selectedInfo.UIDNext
			folderInfo.UIDValidity = selectedInfo.UIDValidity
		} else {
			log.Printf("Warning: failed to select folder %s: %v", f.Name, err)
		}

		folders = append(folders, folderInfo)
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

// BuildFolderTree builds a hierarchical tree structure from flat folder list
func BuildFolderTree(folders []*models.FolderInfo) []*models.FolderTreeNode {
	if len(folders) == 0 {
		return nil
	}

	// Create a map for quick lookup
	folderMap := make(map[string]*models.FolderTreeNode)
	for _, f := range folders {
		folderMap[f.Name] = &models.FolderTreeNode{
			Folder:   f,
			Children: []*models.FolderTreeNode{},
			Path:     f.Name,
			Depth:    0,
		}
	}

	var rootFolders []*models.FolderTreeNode

	// Determine delimiter (use most common one from folders)
	delimiter := "/"
	for _, f := range folders {
		if f.Delimiter != "" && f.Delimiter != " " {
			delimiter = f.Delimiter
			break
		}
	}

	// Build tree structure
	for _, folder := range folders {
		node, exists := folderMap[folder.Name]
		if !exists {
			continue
		}

		// Set initial path to folder name
		node.Path = folder.Name

		// Check if this folder has a parent
		parts := strings.Split(folder.Name, delimiter)
		if len(parts) > 1 {
			// Try to find parent folder
			parentName := strings.Join(parts[:len(parts)-1], delimiter)
			if parent, exists := folderMap[parentName]; exists {
				// Add as child of parent
				node.Depth = parent.Depth + 1
				parent.Children = append(parent.Children, node)
				continue
			}
		}

		// No parent found, this is a root-level folder
		rootFolders = append(rootFolders, node)
	}

	// Sort root folders
	sortFolderTree(rootFolders)

	return rootFolders
}

// sortFolderTree sorts folder tree nodes alphabetically, with standard folders first
func sortFolderTree(nodes []*models.FolderTreeNode) {
	sort.Slice(nodes, func(i, j int) bool {
		iName := strings.ToUpper(nodes[i].Folder.Name)
		jName := strings.ToUpper(nodes[j].Folder.Name)

		// Standard folders order
		standardOrder := []string{"INBOX", "DRAFTS", "SENT", "TRASH", "JUNK", "ARCHIVE"}

		iIsStandard := false
		jIsStandard := false
		iIndex := -1
		jIndex := -1

		for idx, sf := range standardOrder {
			if iName == sf {
				iIsStandard = true
				iIndex = idx
			}
			if jName == sf {
				jIsStandard = true
				jIndex = idx
			}
		}

		// Standard folders come first
		if iIsStandard && !jIsStandard {
			return true
		}
		if !iIsStandard && jIsStandard {
			return false
		}

		// Sort standard folders by predefined order
		if iIsStandard && jIsStandard {
			return iIndex < jIndex
		}

		// Sort non-standard folders alphabetically
		return iName < jName
	})

	// Recursively sort children
	for _, node := range nodes {
		sortFolderTree(node.Children)
	}
}

// GetFolderTree returns folder hierarchy for an account
func (s *MessageService) GetFolderTree(
	ctx context.Context,
	accountID string,
) ([]*models.FolderTreeNode, error) {
	folders, err := s.ListFolders(ctx, accountID)
	if err != nil {
		return nil, err
	}

	return BuildFolderTree(folders), nil
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

// Sorting strategy constants
const (
	// sortThreshold - Use server-side SORT below this count
	sortThreshold = 10000

	// largeMailboxRecentDays - For large mailboxes, filter to recent messages
	largeMailboxRecentDays = 90

	// maxSortUIDs - Maximum UIDs to fetch in single SORT operation
	maxSortUIDs = 50000
)

// isConnectionErrorForService checks if an error indicates a dead IMAP connection
// This is a service-level wrapper around the pool's connection error detection
func isConnectionErrorForService(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// Check for common connection error indicators
	if strings.Contains(errStr, "use of closed network connection") ||
		strings.Contains(errStr, "closed network connection") ||
		strings.Contains(errStr, "i/o timeout") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "broken pipe") {
		return true
	}

	return false
}

// isTimeoutError checks if an error is a timeout
func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	return strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "i/o timeout") ||
		strings.Contains(errStr, "context deadline exceeded")
}

// isCacheValid checks if cached UID list is still valid
func (s *MessageService) isCacheValid(
	cached *messagecache.UIDListWithMetadata,
	currentModSeq uint64,
	currentCount int,
	sortField models.SortField,
	sortOrder models.SortOrder,
) bool {
	if cached == nil {
		return false
	}

	// Check if sort parameters match
	if cached.SortField != string(sortField) || cached.SortOrder != string(sortOrder) {
		log.Printf("Cache invalid: sort changed (cached=%s %s, requested=%s %s)",
			cached.SortField, cached.SortOrder, sortField, sortOrder)
		return false
	}

	// QRESYNC support: Check modseq only
	if cached.QResyncCapable {
		valid := currentModSeq <= cached.HighestModSeq
		if !valid {
			log.Printf("Cache invalid: modseq changed (cached=%d, current=%d)", cached.HighestModSeq, currentModSeq)
		}
		return valid
	}

	// Non-QRESYNC: Check for significant count changes (>10%)
	countDiff := abs(currentCount - cached.Count)
	if cached.Count > 0 && countDiff > cached.Count/10 {
		log.Printf("Cache invalid: count changed significantly (%d -> %d, diff=%d)", cached.Count, currentCount, countDiff)
		return false
	}

	// Check cache age (max 10 minutes)
	if time.Since(cached.CachedAt) > messagecache.TTLUIDList {
		log.Printf("Cache invalid: expired (age=%v)", time.Since(cached.CachedAt))
		return false
	}

	return true
}

// handleSortFailure handles SORT failures with appropriate fallback
func (s *MessageService) handleSortFailure(
	ctx context.Context,
	client *pool.IMAPAdapter,
	folder string,
	accountID string,
	imapConfig pool.IMAPConfig,
	release func(),
	sortBy models.SortField,
	sortOrder models.SortOrder,
	sortErr error,
) ([]uint32, error) {
	// Check if connection is dead - get fresh connection before fallback
	if isConnectionErrorForService(sortErr) {
		log.Printf("Connection dead during SORT, releasing session and getting fresh connection")
		release()

		// Get fresh session
		imapCtx2, cancel2 := context.WithTimeout(ctx, 30*time.Second)
		defer cancel2()

		var err error
		client, release, err = s.sessions.Acquire(imapCtx2, accountID, imapConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to get fresh connection after SORT failure: %w", err)
		}
		defer release()

		// Re-select folder on fresh connection
		_, err = client.SelectFolder(folder)
		if err != nil {
			return nil, fmt.Errorf("failed to select folder on fresh connection: %w", err)
		}
	}

	// Retry SEARCH with fresh connection
	allUIDs, err := client.Search("ALL")
	if err != nil {
		log.Printf("IMAP search failed: %v", err)
		return nil, fmt.Errorf("search failed: %w", err)
	}
	log.Printf("SEARCH fallback succeeded after SORT failure")
	return allUIDs, nil
}

// searchByDateRange performs date-filtered search for large mailboxes
// Returns UIDs from recent messages only (configurable time window)
func (s *MessageService) searchByDateRange(
	ctx context.Context,
	client *pool.IMAPAdapter,
	folder string,
	sortBy models.SortField,
	sortOrder models.SortOrder,
	days int,
) ([]uint32, error) {
	// Calculate date range
	sinceDate := time.Now().AddDate(0, 0, -days)
	searchCriteria := fmt.Sprintf("SINCE %s", sinceDate.Format("02-Jan-2006"))

	log.Printf("Date-range search: %s (last %d days)", searchCriteria, days)

	// Perform SEARCH (not SORT) - much faster on large mailboxes
	uids, err := client.Search(searchCriteria)
	if err != nil {
		return nil, fmt.Errorf("date-range search failed: %w", err)
	}

	log.Printf("Date-range search returned %d UIDs", len(uids))

	// Client-side sort on reduced set
	if len(uids) > 0 {
		// Fetch envelopes for sorting
		envelopes, err := client.FetchMessages(uids, false)
		if err != nil {
			log.Printf("Failed to fetch envelopes for sorting: %v, returning unsorted UIDs", err)
			return uids, nil
		}

		// Sort by date (or other field)
		sortedEnvelopes := s.sortEnvelopes(envelopes, sortBy, sortOrder)

		// Extract sorted UIDs
		sortedUIDs := make([]uint32, len(sortedEnvelopes))
		for i, env := range sortedEnvelopes {
			sortedUIDs[i] = env.UID
		}

		log.Printf("Client-side sort completed: %d UIDs sorted by %s %s", len(sortedUIDs), sortBy, sortOrder)
		return sortedUIDs, nil
	}

	return uids, nil
}

// sortEnvelopes sorts message envelopes client-side
func (s *MessageService) sortEnvelopes(
	envelopes []pool.MessageEnvelope,
	sortBy models.SortField,
	sortOrder models.SortOrder,
) []pool.MessageEnvelope {
	sorted := make([]pool.MessageEnvelope, len(envelopes))
	copy(sorted, envelopes)

	sort.Slice(sorted, func(i, j int) bool {
		var less bool
		switch sortBy {
		case models.SortByDate, "":
			less = sorted[i].Date.Before(sorted[j].Date)
		case models.SortByFrom:
			if len(sorted[i].From) == 0 || len(sorted[j].From) == 0 {
				return false
			}
			less = sorted[i].From[0].Address < sorted[j].From[0].Address
		case models.SortBySubject:
			less = sorted[i].Subject < sorted[j].Subject
		case models.SortBySize:
			less = sorted[i].Size < sorted[j].Size
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
