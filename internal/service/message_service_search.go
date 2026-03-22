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

	// Build and execute search using the configured strategy
	searchCriteria := s.searchStrategy.BuildSearchQuery(query)
	log.Printf("Searching IMAP with criteria: %s", searchCriteria)

	// Execute search using strategy
	uids, err := s.searchStrategy.ExecuteSearch(imapCtx, client, searchCriteria)
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
	messages := []models.MessageSummary{} // Initialize as empty slice, not nil
	if len(uids) > 0 {
		envelopes, err := client.FetchMessages(uids, false)
		if err != nil {
			// Propagate the error: returning empty messages when totalMatches > 0
			// is contradictory and hides real failures from the caller.
			return nil, fmt.Errorf("failed to fetch search result envelopes: %w", err)
		}
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
