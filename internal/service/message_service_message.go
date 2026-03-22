package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"time"
	"webmail_engine/internal/mimeparser"
	"webmail_engine/internal/models"
	"webmail_engine/internal/pool"
	"webmail_engine/internal/scheduler"
)

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
	// alreadySorted tracks whether allUIDs is in the correct final display order (strategy-sorted).
	// true  → use as-is (server_sort or date_range already ordered the slice)
	// false → reverse for descending sort (raw ascending IMAP order from SEARCH)
	// Must be declared here (function scope) because it is set in the cache-hit path
	// (via goto UseUIDs) and also in the live-fetch switch block below.
	var alreadySorted bool

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
				// Restore the alreadySorted flag that was saved when this cache entry was written.
				// Without this, the post-processing block below would use the zero-value (false)
				// and incorrectly reverse an already-sorted list from the date_range strategy.
				alreadySorted = cachedMetadata.AlreadySorted
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
					// fallback_search returns raw ascending UIDs — not pre-sorted
					alreadySorted = false
				} else {
					log.Printf("SORT completed in %v: %d UIDs", sortDuration, len(allUIDs))
					if sortDuration > 5*time.Second {
						log.Printf("WARN: SORT took >5s, consider date-range filtering for this mailbox")
					}
					// Server SORT returns UIDs in the requested order — already sorted
					alreadySorted = true
				}
			} else {
				// Server doesn't support SORT
				log.Printf("Server doesn't support SORT, using SEARCH")
				allUIDs, err = client.Search("ALL")
				sortStrategy = "search_no_sort_cap"
				// Raw SEARCH returns ascending UIDs — not pre-sorted
				alreadySorted = false
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
				// fallback_search returns raw ascending UIDs — not pre-sorted
				alreadySorted = false
			} else {
				// searchByDateRange fetches envelopes and sorts them — already in display order
				alreadySorted = true
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
			// SEARCH returns ascending UIDs — not pre-sorted for display order
			alreadySorted = false
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
		if err := s.uidListCache.Set(ctx, uidCacheKey, allUIDs, len(allUIDs), currentModSeq, client.HasQResync(), string(sortBy), string(sortOrder), "ALL", alreadySorted); err != nil {
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

	// Apply final order to UIDs based on whether the strategy already produced a sorted list.
	// We deliberately avoid using client.HasSort() here: that flag indicates server CAPABILITY,
	// not which strategy actually ran. Using it caused incorrect reversal for the date_range
	// and limited_search paths.
	var uids []uint32
	if alreadySorted {
		// UIDs are already in the correct display order (server_sort or date_range)
		uids = allUIDs
		log.Printf("UIDs already in display order (strategy produced sorted list): first=%v last=%v",
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
		// Raw ascending UIDs from SEARCH — reverse for newest-first
		uids = make([]uint32, 0, len(allUIDs))
		for i := len(allUIDs) - 1; i >= 0; i-- {
			uids = append(uids, allUIDs[i])
		}
		log.Printf("Reversed UIDs for descending sort (strategy returned raw ascending order)")
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
	cachedMsg, missingAtts, err := s.getCachedMessage(ctx, accountID, folder, uid)
	if err == nil && cachedMsg != nil {
		// Auto-healing: If some attachments are missing from the cache/storage,
		// fetch ONLY those parts from IMAP instead of re-fetching the whole message.
		if len(missingAtts) > 0 {
			log.Printf("[DEBUG] Message cache hit but %d attachments are missing, starting selective healing: account=%s, folder=%s, uid=%s", len(missingAtts), accountID, folder, uid)

			// Re-fetch only the missing parts
			if err := s.healMissingAttachments(ctx, accountID, folder, uid, cachedMsg, missingAtts); err != nil {
				log.Printf("Warning: focused healing failed: %v. Falling back to full re-fetch.", err)
				// Fall through to full re-fetch if healing fails
			} else {
				// Healing succeeded, return the healed message
				if cachedMsg.ProcessingMetadata == nil {
					cachedMsg.ProcessingMetadata = &models.ProcessingMetadata{}
				}
				cachedMsg.ProcessingMetadata.CacheStatus = "hit"
				cachedMsg.ProcessingMetadata.ProcessingTime = 0 // Healed from cache+parts
				return cachedMsg, nil
			}
		} else if cachedMsg.Flags == nil {
			// If flags field is nil, it might be an old cache entry without flags.
			// Force a refresh from IMAP to populate them.
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
	log.Printf("[DEBUG] Message cache miss or invalid: account=%s, folder=%s, uid=%s", accountID, folder, uid)

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
	// Use independent attachment cache to avoid redundant storage
	for i := range parseResult.Attachments {
		att := &parseResult.Attachments[i]
		if att.Data != nil {
			oldID := att.ID

			// Check independent attachment cache first
			// Calculate content-based ID manually to check cache
			contentHash := sha256.Sum256(att.Data)
			hashInput := fmt.Sprintf("%s:%s:%s:%s:%x", accountID, folder, uid, att.Filename, contentHash)
			id := fmt.Sprintf("%x", sha256.Sum256([]byte(hashInput)))[:16]

			cachedAtt, _ := s.cache.GetAttachmentInfo(ctx, id)
			if cachedAtt != nil {
				log.Printf("[DEBUG] Attachment cache hit: id=%s, filename=%s", id, att.Filename)
				att.ID = id
			} else {
				// Cache miss or invalidated, store on disk
				newID, err := s.storage.Store(accountID, folder, uid, att.Filename, att.Data)
				if err != nil {
					log.Printf("Warning: failed to store attachment %s: %v", att.Filename, err)
					continue
				}
				att.ID = newID

				// Update independent attachment cache
				modelAtt := &models.Attachment{
					ID:          att.ID,
					PartID:      att.PartID,
					Filename:    att.Filename,
					ContentType: att.ContentType,
					Size:        att.Size,
					Disposition: att.Disposition,
					ContentID:   att.ContentID,
					Checksum:    att.Checksum,
				}
				if err := s.cache.SetAttachmentInfo(ctx, modelAtt); err != nil {
					log.Printf("Warning: failed to cache attachment info: %v", err)
				}
			}

			// Update corresponding message attachment references
			for j := range parseResult.Message.Attachments {
				msgAtt := &parseResult.Message.Attachments[j]
				if msgAtt.ID == oldID || msgAtt.Filename == att.Filename {
					msgAtt.ID = att.ID
					msgAtt.PartID = att.PartID
					msgAtt.Checksum = att.Checksum
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
	folder string,
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

	if folder == "" {
		folder = "INBOX"
	}

	// Get account with decrypted credentials
	account, err := s.accountService.GetAccountWithCredentials(ctx, accountID)
	if err != nil {
		return fmt.Errorf("failed to get account: %w", err)
	}

	// Create IMAP connection with timeout
	imapCtx, cancel := context.WithTimeout(ctx, 60*time.Second) // Longer timeout for streaming
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

	// Convert UID string to uint32
	uidNum, err := strconv.ParseUint(uid, 10, 32)
	if err != nil {
		return fmt.Errorf("invalid UID: %w", err)
	}

	log.Printf("[DEBUG] Streaming message: account=%s, folder=%s, uid=%s, chunkSize=%d", accountID, folder, uid, chunkSize)

	// Call the IMAP client stream method
	return client.StreamMessage(uint32(uidNum), chunkSize, handler)
}
