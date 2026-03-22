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
