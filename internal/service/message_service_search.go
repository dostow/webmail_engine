package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
	"webmail_engine/internal/models"
	"webmail_engine/internal/pool"
	"webmail_engine/internal/scheduler"
)

// searchByDateRange performs date-filtered search for large mailboxes
// Returns UIDs from recent messages only (configurable time window)
func (s *MessageService) searchByDateRange(
	_ context.Context,
	client *pool.IMAPAdapter,
	_ string,
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
			// Return an error so the caller falls back to Search("ALL") with alreadySorted=false.
			// Returning unsorted UIDs here would cause GetMessageList to treat them as
			// already-sorted (alreadySorted=true), silently serving ascending order for desc requests.
			return nil, fmt.Errorf("failed to fetch envelopes for date-range sort: %w", err)
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

// SearchMessages searches for messages with pagination support
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

	// Build and execute search using the configured strategy
	searchCriteria := s.searchStrategy.BuildSearchQuery(query)
	log.Printf("Searching IMAP with criteria: %s", searchCriteria)

	searchStart := time.Now()

	// Execute search using strategy
	allUIDs, err := s.searchStrategy.ExecuteSearch(imapCtx, client, searchCriteria)
	if err != nil {
		log.Printf("IMAP search failed: %v", err)
		return nil, fmt.Errorf("search failed: %w", err)
	}

	searchTime := time.Since(searchStart).Milliseconds()

	// Apply pagination
	totalMatches := len(allUIDs)
	pageSize := query.Limit
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200 // Max page size
	}

	// Parse cursor to get page number
	var currentPage int
	if query.Cursor != "" {
		cursorData, err := decodeCursor(query.Cursor)
		if err != nil {
			log.Printf("Invalid cursor, starting from page 1: %v", err)
			currentPage = 0
		} else {
			currentPage = cursorData.Page
		}
	}

	// Calculate start and end indices
	startIndex := currentPage * pageSize
	endIndex := startIndex + pageSize

	// Check if cursor is beyond available results
	if startIndex >= totalMatches {
		// Return empty page with pagination info
		totalPages := 1
		if totalMatches > 0 {
			totalPages = (totalMatches + pageSize - 1) / pageSize
		}
		return &models.SearchResult{
			Messages:     []models.MessageSummary{},
			TotalMatches: totalMatches,
			SearchTime:   searchTime,
			CacheUsed:    false,
			CurrentPage:  currentPage + 1,
			TotalPages:   totalPages,
			PageSize:     pageSize,
			HasMore:      false,
			NextCursor:   "",
		}, nil
	}

	// Apply bounds
	if endIndex > totalMatches {
		endIndex = totalMatches
	}

	// Slice UIDs for current page
	pageUIDs := allUIDs[startIndex:endIndex]

	// Reverse UIDs for descending order (newest first)
	// IMAP SEARCH returns UIDs in ascending order
	for i, j := 0, len(pageUIDs)-1; i < j; i, j = i+1, j-1 {
		pageUIDs[i], pageUIDs[j] = pageUIDs[j], pageUIDs[i]
	}

	// Fetch message envelopes for current page only
	messages := []models.MessageSummary{}
	if len(pageUIDs) > 0 {
		envelopes, err := client.FetchMessages(pageUIDs, false)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch search result envelopes: %w", err)
		}

		// Build map for reordering (FetchMessages may return in different order)
		envelopeMap := make(map[uint32]pool.MessageEnvelope)
		for _, env := range envelopes {
			envelopeMap[env.UID] = env
		}

		// Reconstruct in correct order
		for _, uid := range pageUIDs {
			if env, ok := envelopeMap[uid]; ok {
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

	// Calculate pagination metadata
	totalPages := 1
	if totalMatches > 0 {
		totalPages = (totalMatches + pageSize - 1) / pageSize
	}

	// Build next cursor
	var nextCursor string
	hasMore := endIndex < totalMatches
	if hasMore {
		nextCursorData := CursorData{
			Page:      currentPage + 1,
			LastUID:   pageUIDs[len(pageUIDs)-1],
			SortBy:    query.SortBy,
			SortOrder: query.SortOrder,
			Timestamp: time.Now(),
		}
		nextCursor, _ = encodeCursor(nextCursorData)
	}

	return &models.SearchResult{
		Messages:     messages,
		TotalMatches: totalMatches,
		SearchTime:   searchTime,
		CacheUsed:    false,
		CurrentPage:  currentPage + 1,
		TotalPages:   totalPages,
		PageSize:     pageSize,
		HasMore:      hasMore,
		NextCursor:   nextCursor,
	}, nil
}
