package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
	"webmail_engine/internal/cache"
	"webmail_engine/internal/cachekey"
	"webmail_engine/internal/models"
)

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
// Returns the message and any missing attachments if found via content-based deduplication
func (s *MessageService) getCachedMessageByContent(
	ctx context.Context,
	_ string,
	msg *models.Message,
) (*models.Message, []models.Attachment, error) {
	if s.cache == nil {
		return nil, nil, nil
	}

	// Generate content hash
	contentHash := s.generateContentHash(msg)
	if contentHash == "" {
		return nil, nil, nil
	}

	// Try to get the primary key from content hash index
	hashKey := s.buildContentCacheKey(contentHash)
	data, err := s.cache.Get(ctx, hashKey)
	if err != nil || len(data) == 0 {
		return nil, nil, nil // Cache miss
	}

	// Get the primary cache key from the hash index
	primaryKey := string(data)

	// Now fetch the actual message from the primary key
	msgData, err := s.cache.Get(ctx, primaryKey)
	if err != nil || len(msgData) == 0 {
		// Hash index points to non-existent message, clean up
		_ = s.cache.Delete(ctx, hashKey)
		return nil, nil, nil
	}

	// Unmarshal message
	var cachedMsg models.Message
	if err := json.Unmarshal(msgData, &cachedMsg); err != nil {
		log.Printf("Failed to unmarshal cached message: %v", err)
		return nil, nil, err
	}

	// Verify all attachments exist in the independent metadata cache
	var missing []models.Attachment
	for _, att := range cachedMsg.Attachments {
		cachedAtt, _ := s.cache.GetAttachmentInfo(ctx, att.ID)
		if cachedAtt == nil {
			log.Printf("[DEBUG] Content cache hit but attachment %s (name=%s, part=%s) is missing", att.ID, att.Filename, att.PartID)
			missing = append(missing, att)
		}
	}

	log.Printf("Cache hit via content hash: hash=%s, primaryKey=%s (missing %d attachments)", contentHash, primaryKey, len(missing))
	return &cachedMsg, missing, nil
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
) (*models.Message, []models.Attachment, error) {
	if s.cache == nil {
		return nil, nil, nil
	}

	cacheKey := s.buildMessageCacheKey(accountID, folder, uid)
	data, err := s.cache.Get(ctx, cacheKey)
	if err != nil || len(data) == 0 {
		return nil, nil, nil // Cache miss
	}

	var msg models.Message
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("Failed to unmarshal cached message: %v", err)
		return nil, nil, err
	}

	// Verify all attachments exist in the independent metadata cache
	var missing []models.Attachment
	for _, att := range msg.Attachments {
		cachedAtt, _ := s.cache.GetAttachmentInfo(ctx, att.ID)
		if cachedAtt == nil {
			log.Printf("[DEBUG] Message cache hit but attachment %s (name=%s, part=%s) is missing from metadata cache", att.ID, att.Filename, att.PartID)
			missing = append(missing, att)
		}
	}

	log.Printf("Cache hit: key=%s (missing %d attachments)", cacheKey, len(missing))
	return &msg, missing, nil
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
