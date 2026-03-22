package service

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"
	"webmail_engine/internal/messagecache"
	"webmail_engine/internal/models"
	"webmail_engine/internal/pool"
)

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

// isCacheValid checks if cached UID list is still valid.
// Returns:
//   - valid=true  → cached UIDs can be used (either as-is or after incremental update)
//   - valid=false → cache must be fully discarded and re-fetched
//   - needsIncremental=true → modseq advanced; caller should perform CONDSTORE delta
//     fetch and merge before using the cache (only set when valid=true)
func (s *MessageService) isCacheValid(
	cached *messagecache.UIDListWithMetadata,
	currentModSeq uint64,
	currentCount int,
	sortField models.SortField,
	sortOrder models.SortOrder,
) (valid bool, needsIncremental bool) {
	if cached == nil {
		return false, false
	}

	// Sort change always requires full reload
	if cached.SortField != string(sortField) || cached.SortOrder != string(sortOrder) {
		log.Printf("Cache invalid: sort changed (cached=%s %s, requested=%s %s)",
			cached.SortField, cached.SortOrder, sortField, sortOrder)
		return false, false
	}

	// Expired: full reload needed
	if time.Since(cached.CachedAt) > messagecache.TTLUIDList {
		log.Printf("Cache invalid: expired (age=%v)", time.Since(cached.CachedAt))
		return false, false
	}

	// Count changed dramatically (>10% drop or large unexpected jump) → full reload.
	// Note: small increases (new emails) are handled by the incremental path below.
	countDiff := abs(currentCount - cached.Count)
	if cached.Count > 0 && countDiff > cached.Count/10 && currentCount < cached.Count {
		log.Printf("Cache invalid: count dropped significantly (%d -> %d, diff=%d)", cached.Count, currentCount, countDiff)
		return false, false
	}

	// ModSeq advanced → cache is warm but stale; caller should do incremental sync
	if currentModSeq > cached.HighestModSeq {
		log.Printf("Cache warm but modseq advanced (cached=%d, current=%d); incremental update needed",
			cached.HighestModSeq, currentModSeq)
		return true, true
	}

	// Fallback for servers lacking CONDSTORE (modseq is 0)
	// If the count changes, we can't do an incremental CONDSTORE fetch, so we must fully invalidate.
	if currentModSeq == 0 && currentCount != cached.Count {
		log.Printf("Cache invalid: no modseq and count changed (%d -> %d)", cached.Count, currentCount)
		return false, false
	}

	// Cache is fully fresh
	return true, false
}

// fetchIncrementalUpdates performs a CONDSTORE delta fetch: it finds UIDs changed
// since lastModSeq, deduplicates against the cached list, and prepends new UIDs
// at the front (for descending date-sort) or appends them (ascending).
// Returns the merged UID list ready to be re-cached with the new modseq.
func (s *MessageService) fetchIncrementalUpdates(
	ctx context.Context,
	client *pool.IMAPAdapter,
	existingUIDs []uint32,
	lastModSeq uint64,
	sortOrder models.SortOrder,
) ([]uint32, error) {
	newUIDs, err := client.SearchChangedSince(lastModSeq)
	if err != nil {
		return nil, err
	}

	if len(newUIDs) == 0 {
		log.Printf("Incremental update: no new UIDs since modseq=%d", lastModSeq)
		return existingUIDs, nil
	}

	// Build a set of already-known UIDs for fast dedup
	known := make(map[uint32]struct{}, len(existingUIDs))
	for _, uid := range existingUIDs {
		known[uid] = struct{}{}
	}

	added := make([]uint32, 0, len(newUIDs))
	for _, uid := range newUIDs {
		if _, exists := known[uid]; !exists {
			added = append(added, uid)
		}
	}

	log.Printf("Incremental update: %d new UIDs since modseq=%d (out of %d changed)", len(added), lastModSeq, len(newUIDs))

	if len(added) == 0 {
		return existingUIDs, nil
	}

	// For descending sort (newest first) prepend new UIDs; ascending → append
	var merged []uint32
	if sortOrder == models.SortOrderDesc {
		// Reverse added so highest UID is first
		for i, j := 0, len(added)-1; i < j; i, j = i+1, j-1 {
			added[i], added[j] = added[j], added[i]
		}
		merged = append(added, existingUIDs...)
	} else {
		merged = append(existingUIDs, added...)
	}

	return merged, nil
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

// healMissingAttachments attempts to recover missing attachments by fetching only the necessary parts from IMAP
func (s *MessageService) healMissingAttachments(
	ctx context.Context,
	accountID, folder, uidStr string,
	msg *models.Message,
	missing []models.Attachment,
) error {
	uid, _ := strconv.ParseUint(uidStr, 10, 32)

	// Get account with decrypted credentials
	account, err := s.accountService.GetAccountWithCredentials(ctx, accountID)
	if err != nil {
		return fmt.Errorf("failed to get account: %w", err)
	}

	imapConfig := pool.IMAPConfig{
		Host:       account.IMAPConfig.Host,
		Port:       account.IMAPConfig.Port,
		Username:   account.IMAPConfig.Username,
		Password:   account.IMAPConfig.Password,
		Encryption: account.IMAPConfig.Encryption,
	}

	// Acquire IMAP session
	client, release, err := s.sessions.Acquire(ctx, accountID, imapConfig)
	if err != nil {
		return fmt.Errorf("failed to acquire IMAP session: %w", err)
	}
	defer release()

	// Select folder
	if _, err := client.SelectFolder(folder); err != nil {
		return fmt.Errorf("failed to select folder %s: %w", folder, err)
	}

	healedCount := 0
	for _, miss := range missing {
		if miss.PartID == "" {
			log.Printf("[DEBUG] Cannot heal attachment %s: missing PartID", miss.Filename)
			continue
		}

		log.Printf("[DEBUG] Healing attachment %s (part %s) from IMAP...", miss.Filename, miss.PartID)
		data, err := client.FetchPart(uint32(uid), miss.PartID)
		if err != nil {
			log.Printf("Warning: failed to fetch part %s for healing: %v", miss.PartID, err)
			continue
		}

		// Store and cache newly fetched attachment
		newID, err := s.storage.Store(accountID, folder, uidStr, miss.Filename, data)
		if err != nil {
			log.Printf("Warning: failed to store healed attachment %s: %v", miss.Filename, err)
			continue
		}

		// Update independent attachment cache
		modelAtt := miss // Copy
		modelAtt.ID = newID
		if err := s.cache.SetAttachmentInfo(ctx, &modelAtt); err != nil {
			log.Printf("Warning: failed to cache healed attachment info: %v", err)
		}

		// Update message attachment reference
		for i := range msg.Attachments {
			if msg.Attachments[i].PartID == miss.PartID || msg.Attachments[i].Filename == miss.Filename {
				msg.Attachments[i].ID = newID
				break
			}
		}
		healedCount++
	}

	if healedCount == 0 {
		return fmt.Errorf("failed to heal any attachments")
	}

	// Re-cache the healed message
	if err := s.setCachedMessageWithDedup(ctx, accountID, msg); err != nil {
		log.Printf("Warning: failed to update message cache after healing: %v", err)
	}

	log.Printf("[DEBUG] Successfully healed %d/%d attachments for message %s", healedCount, len(missing), uidStr)
	return nil
}
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
