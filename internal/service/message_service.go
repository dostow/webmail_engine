package service

import (
	"context"
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
	searchStrategy   SearchStrategy
}

// MessageServiceConfig holds service configuration
type MessageServiceConfig struct {
	TempStoragePath string
	MaxInlineSize   int64
	AllowBodySearch bool // Whether to allow BODY search (slow, performance impact)
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

	// Create search strategy based on config
	var searchStrategy SearchStrategy
	if config.AllowBodySearch {
		// Use body search strategy when enabled
		searchStrategy = NewBodySearchStrategy()
	} else {
		// Use default strategy (no body search) for performance
		searchStrategy = NewDefaultSearchStrategy()
	}

	return &MessageService{
		sessions:         sessions,
		cache:            cache,
		messageListCache: messageListCache,
		uidListCache:     uidListCache,
		scheduler:        scheduler,
		accountService:   accountService,
		parser:           parser,
		storage:          storage,
		searchStrategy:   searchStrategy,
	}, nil
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

// GetPoolStats returns statistics about the IMAP session pool
func (s *MessageService) GetPoolStats() pool.SessionPoolStats {
	return s.sessions.Stats()
}
