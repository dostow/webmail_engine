package messagecache

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"webmail_engine/internal/cache"
	"webmail_engine/internal/models"
)

const (
	// Key prefixes for different cache types
	KeyPrefixMessageList = "msglist:"
	KeyPrefixUIDList     = "uids:"

	// Default TTL values
	TTLMessageList = 5 * time.Minute
	TTLUIDList     = 10 * time.Minute // Increased for better cache hit rate
)

// CacheStats tracks cache statistics
type CacheStats struct {
	Hits       int64     `json:"hits"`
	Misses     int64     `json:"misses"`
	Sets       int64     `json:"sets"`
	Errors     int64     `json:"errors"`
	HitRate    float64   `json:"hit_rate"`
	LastUpdate time.Time `json:"last_update"`
}

// MessageListCache handles caching of message lists
type MessageListCache struct {
	cache *cache.Cache
	stats CacheStats
	mu    interface{} // Will use sync.Mutex in actual implementation
}

// UIDListCache handles caching of UID lists with modseq tracking
type UIDListCache struct {
	cache *cache.Cache
	mu    interface{}
}

// CacheOptions holds optional cache configuration
type CacheOptions struct {
	TTL           time.Duration
	SkipOnMiss    bool // Don't invalidate on cache miss
	IncludeModSeq bool // Include modseq in cache key for change detection
}

// NewMessageListCache creates a new message list cache
func NewMessageListCache(c *cache.Cache) *MessageListCache {
	return &MessageListCache{
		cache: c,
		stats: CacheStats{
			LastUpdate: time.Now(),
		},
	}
}

// NewUIDListCache creates a new UID list cache
func NewUIDListCache(c *cache.Cache) *UIDListCache {
	return &UIDListCache{
		cache: c,
	}
}

// ==================== Message List Cache ====================

// CachedMessageList wraps a message list with cache metadata
type CachedMessageList struct {
	MessageList  *models.MessageList `json:"message_list"`
	CachedModSeq uint64              `json:"cached_modseq"` // ModSeq when cached
	CachedAt     time.Time           `json:"cached_at"`
}

// BuildKey generates a cache key for a message list
// The key includes parameters that define the result SET, not freshness
// ModSeq is stored in the value for smart invalidation
func (c *MessageListCache) BuildKey(
	accountID string,
	folder string,
	cursor string,
	limit int,
	sortBy models.SortField,
	sortOrder models.SortOrder,
	uidValidity uint32,
) string {
	// Hash cursor to avoid special character issues
	cursorHash := buildCursorHash(cursor)

	// Key without modseq - modseq is checked on retrieval
	return fmt.Sprintf("%s%s:%s:%s:%d:%s:%s:uv%d",
		KeyPrefixMessageList,
		accountID, folder, cursorHash, limit, sortBy, sortOrder, uidValidity)
}

// Get retrieves a cached message list with smart modseq checking
// Returns the message list if valid, or nil if cache miss/stale
// currentCount is used as a fallback for invalidation when modseq is 0 (unsupported)
func (c *MessageListCache) Get(ctx context.Context, key string, currentModSeq uint64, currentCount int) (*models.MessageList, bool) {
	if c.cache == nil {
		return nil, false
	}

	data, err := c.cache.Get(ctx, key)
	if err != nil {
		c.stats.Misses++
		return nil, false
	}

	// Cache integrity check: detect empty or corrupt cache entries
	if len(data) == 0 {
		log.Printf("Cache integrity check failed: empty data for key=%s", key)
		c.stats.Misses++
		return nil, false
	}

	// Quick JSON validation before unmarshal to provide better error messages
	if !isValidJSON(data) {
		log.Printf("Cache integrity check failed: invalid JSON for key=%s, data length=%d", key, len(data))
		c.stats.Misses++
		// Optionally delete the corrupt cache entry
		_ = c.cache.Delete(ctx, key)
		return nil, false
	}

	var cached CachedMessageList
	if err := json.Unmarshal(data, &cached); err != nil {
		log.Printf("Failed to unmarshal cached message list: %v", err)
		c.stats.Misses++
		// Delete corrupt cache entry to prevent repeated failures
		_ = c.cache.Delete(ctx, key)
		return nil, false
	}

	// Additional integrity check: ensure MessageList is not nil
	if cached.MessageList == nil {
		log.Printf("Cache integrity check failed: MessageList is nil for key=%s", key)
		c.stats.Misses++
		_ = c.cache.Delete(ctx, key)
		return nil, false
	}

	// Check freshness (cache is stale if older than TTL)
	if time.Since(cached.CachedAt) > TTLMessageList {
		log.Printf("Cached message list is stale (age: %v)", time.Since(cached.CachedAt))
		c.stats.Misses++
		return nil, false
	}

	// Smart modseq checking:
	// - If modseq unchanged, cache is definitely valid
	// - If modseq changed, cache MIGHT still be valid for older pages
	modseqUnchanged := cached.CachedModSeq == currentModSeq

	// Fallback for servers that do not support CONDSTORE (modseq is 0)
	if currentModSeq == 0 && cached.CachedModSeq == 0 {
		if cached.MessageList.TotalCount != currentCount {
			log.Printf("Fallback cache invalidation: message count changed (%d -> %d) when modseq=0",
				cached.MessageList.TotalCount, currentCount)
			modseqUnchanged = false
		}
	}

	if !modseqUnchanged {
		// Modseq (or count) changed - mailbox was modified
		// For descending date sort (newest first), check if this is likely page 1
		// Page 1 would be affected by new messages, older pages usually aren't
		if cached.MessageList.CurrentPage == 1 {
			log.Printf("Cache modseq mismatch (cached=%d, current=%d), page 1 likely changed",
				cached.CachedModSeq, currentModSeq)
			c.stats.Misses++
			return nil, false
		}
		// For pages > 1, assume content unchanged unless proven otherwise
		// This is a trade-off: might show slightly stale data but better cache hit rate
		log.Printf("Cache modseq mismatch but page %d likely unchanged", cached.MessageList.CurrentPage)
	}

	c.stats.Hits++
	log.Printf("Cache hit for message list: key=%s, count=%d, modseq_unchanged=%v",
		key, len(cached.MessageList.Messages), modseqUnchanged)
	return cached.MessageList, true
}

// Set stores a message list in cache with modseq metadata
func (c *MessageListCache) Set(ctx context.Context, key string, messageList *models.MessageList, modSeq uint64) error {
	if c.cache == nil {
		return nil
	}

	// Validate message list before caching to prevent corrupt cache entries
	// Note: Empty message list (len=0) is valid - e.g., folder emptied, empty page
	if messageList == nil {
		log.Printf("Cache write skipped: messageList is nil for key=%s", key)
		return nil
	}

	// Marshal first to validate data integrity
	cached := CachedMessageList{
		MessageList:  messageList,
		CachedModSeq: modSeq,
		CachedAt:     time.Now(),
	}

	data, err := json.Marshal(cached)
	if err != nil {
		log.Printf("Failed to marshal message list for cache: %v", err)
		return err
	}

	// Validate marshaled data is not empty and is valid JSON
	if len(data) == 0 {
		log.Printf("Cache write skipped: marshaled data is empty for key=%s", key)
		return nil
	}

	// Quick JSON validation before writing to cache
	if !isValidJSON(data) {
		log.Printf("Cache write skipped: invalid JSON generated for key=%s", key)
		return fmt.Errorf("invalid JSON generated during marshaling")
	}

	if err := c.cache.Set(ctx, key, data, TTLMessageList); err != nil {
		log.Printf("Failed to cache message list: %v", err)
		return err
	}

	c.stats.Sets++
	log.Printf("Cached message list: key=%s, count=%d, modseq=%d", key, len(messageList.Messages), modSeq)
	return nil
}

// Delete removes a message list from cache
func (c *MessageListCache) Delete(ctx context.Context, key string) error {
	if c.cache == nil {
		return nil
	}
	return c.cache.Delete(ctx, key)
}

// GetStats returns cache statistics
func (c *MessageListCache) GetStats() CacheStats {
	total := c.stats.Hits + c.stats.Misses
	if total > 0 {
		c.stats.HitRate = float64(c.stats.Hits) / float64(total)
	}
	c.stats.LastUpdate = time.Now()
	return c.stats
}

// ==================== UID List Cache ====================

// UIDListWithMetadata represents cached UID list with metadata
type UIDListWithMetadata struct {
	UIDs           []uint32  `json:"uids"`
	Count          int       `json:"count"`
	HighestModSeq  uint64    `json:"highest_modseq"`
	QResyncCapable bool      `json:"qresync_capable"`
	CachedAt       time.Time `json:"cached_at"`
	SortField      string    `json:"sort_field,omitempty"`      // Track sort field
	SortOrder      string    `json:"sort_order,omitempty"`      // Track sort order
	SearchCriteria string    `json:"search_criteria,omitempty"` // Track search criteria used
	// AlreadySorted indicates whether UIDs are stored in the final display order
	// (i.e., already reversed for descending sort). When true, the caller must NOT
	// reverse the slice again; when false, the caller must reverse for desc order.
	AlreadySorted  bool      `json:"already_sorted"`
}

// BuildUIDKey generates a cache key for a UID list
func (c *UIDListCache) BuildKey(accountID string, folder string, uidValidity uint32) string {
	return fmt.Sprintf("%s%s:%s:%d", KeyPrefixUIDList, accountID, folder, uidValidity)
}

// Get retrieves a cached UID list with metadata
func (c *UIDListCache) Get(ctx context.Context, key string) (*UIDListWithMetadata, error) {
	if c.cache == nil {
		return nil, nil
	}

	data, err := c.cache.Get(ctx, key)
	if err != nil {
		return nil, err
	}

	// Cache integrity check: detect empty or corrupt cache entries
	if len(data) == 0 {
		log.Printf("Cache integrity check failed: empty data for UID key=%s", key)
		return nil, fmt.Errorf("cache entry is empty")
	}

	// Quick JSON validation before unmarshal
	if !isValidJSON(data) {
		log.Printf("Cache integrity check failed: invalid JSON for UID key=%s, data length=%d", key, len(data))
		// Delete corrupt cache entry to prevent repeated failures
		_ = c.cache.Delete(ctx, key)
		return nil, fmt.Errorf("cache entry contains invalid JSON")
	}

	var metadata UIDListWithMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		log.Printf("Failed to unmarshal cached UID list: %v", err)
		// Delete corrupt cache entry
		_ = c.cache.Delete(ctx, key)
		return nil, err
	}

	// Integrity check: ensure UIDs slice is not nil
	if metadata.UIDs == nil {
		log.Printf("Cache integrity check failed: UIDs is nil for key=%s", key)
		_ = c.cache.Delete(ctx, key)
		return nil, fmt.Errorf("cache entry has nil UIDs")
	}

	log.Printf("UID cache hit: %d UIDs (modseq=%d)", len(metadata.UIDs), metadata.HighestModSeq)
	return &metadata, nil
}

// Set stores a UID list with metadata in cache.
// alreadySorted must be true when uids are already in the final display order
// (caller must not reverse again); false when uids are in raw ascending IMAP order.
func (c *UIDListCache) Set(
	ctx context.Context,
	key string,
	uids []uint32,
	count int,
	modSeq uint64,
	qresyncCapable bool,
	sortField string,
	sortOrder string,
	searchCriteria string,
	alreadySorted bool,
) error {
	if c.cache == nil {
		return nil
	}

	// Validate UIDs before caching to prevent corrupt cache entries
	// Note: Empty UID list (len=0) is valid - e.g., empty folder, all messages deleted
	// We distinguish between nil (error) and empty slice (valid result)
	if uids == nil {
		log.Printf("Cache write skipped: uids is nil for key=%s", key)
		return nil
	}

	metadata := UIDListWithMetadata{
		UIDs:           uids,
		Count:          count,
		HighestModSeq:  modSeq,
		QResyncCapable: qresyncCapable,
		CachedAt:       time.Now(),
		SortField:      sortField,
		SortOrder:      sortOrder,
		SearchCriteria: searchCriteria,
		AlreadySorted:  alreadySorted,
	}

	// Marshal first to validate data integrity
	data, err := json.Marshal(metadata)
	if err != nil {
		log.Printf("Failed to marshal UID list for cache: %v", err)
		return err
	}

	// Validate marshaled data is not empty and is valid JSON
	if len(data) == 0 {
		log.Printf("Cache write skipped: marshaled data is empty for key=%s", key)
		return nil
	}

	// Quick JSON validation before writing to cache
	if !isValidJSON(data) {
		log.Printf("Cache write skipped: invalid JSON generated for key=%s", key)
		return fmt.Errorf("invalid JSON generated during marshaling")
	}

	if err := c.cache.Set(ctx, key, data, TTLUIDList); err != nil {
		log.Printf("Failed to cache UID list: %v", err)
		return err
	}

	log.Printf("Cached UID list with metadata: key=%s, count=%d, modseq=%d, sort=%s %s, alreadySorted=%v", key, count, modSeq, sortField, sortOrder, alreadySorted)
	return nil
}

// Delete removes a UID list from cache
func (c *UIDListCache) Delete(ctx context.Context, key string) error {
	if c.cache == nil {
		return nil
	}
	return c.cache.Delete(ctx, key)
}

// ==================== Cache Invalidation ====================

// InvalidateFolder invalidates all cache entries for a folder
func (c *MessageListCache) InvalidateFolder(ctx context.Context, accountID string, folder string) error {
	if c.cache == nil {
		return nil
	}

	// Delete all message list cache entries for this folder
	pattern := fmt.Sprintf("%s%s:%s:*", KeyPrefixMessageList, accountID, folder)
	keys, err := c.cache.Keys(ctx, pattern)
	if err != nil {
		return err
	}

	for _, key := range keys {
		if err := c.cache.Delete(ctx, key); err != nil {
			log.Printf("Warning: failed to delete cache key %s: %v", key, err)
		}
	}

	log.Printf("Invalidated %d message list cache entries for folder %s", len(keys), folder)
	return nil
}

// InvalidateUIDs invalidates UID cache for a folder
func (c *UIDListCache) InvalidateFolder(ctx context.Context, accountID string, folder string) error {
	if c.cache == nil {
		return nil
	}

	pattern := fmt.Sprintf("%s%s:%s:*", KeyPrefixUIDList, accountID, folder)
	keys, err := c.cache.Keys(ctx, pattern)
	if err != nil {
		return err
	}

	for _, key := range keys {
		if err := c.cache.Delete(ctx, key); err != nil {
			log.Printf("Warning: failed to delete UID cache key %s: %v", key, err)
		}
	}

	log.Printf("Invalidated %d UID cache entries for folder %s", len(keys), folder)
	return nil
}

// InvalidateAccount invalidates all cache entries for an account
func (c *MessageListCache) InvalidateAccount(ctx context.Context, accountID string) error {
	if c.cache == nil {
		return nil
	}

	pattern := fmt.Sprintf("%s%s:*", KeyPrefixMessageList, accountID)
	keys, err := c.cache.Keys(ctx, pattern)
	if err != nil {
		return err
	}

	for _, key := range keys {
		_ = c.cache.Delete(ctx, key)
	}

	log.Printf("Invalidated %d cache entries for account %s", len(keys), accountID)
	return nil
}

// ==================== Helpers ====================

// isValidJSON performs a quick validation that data is non-empty valid JSON
func isValidJSON(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	// Quick check: valid JSON must start with {, [, or "
	firstChar := data[0]
	if firstChar != '{' && firstChar != '[' && firstChar != '"' {
		return false
	}
	// Full validation: try to unmarshal into interface{}
	var js json.RawMessage
	return json.Unmarshal(data, &js) == nil
}

// buildCursorHash generates a short hash for the cursor
func buildCursorHash(cursor string) string {
	if cursor == "" {
		return "c0" // Default for first page
	}

	// Use first 4 bytes of hash for shorter key
	hash := sha256.Sum256([]byte(cursor))
	return fmt.Sprintf("c%x", hash[:4])
}

// EncodeCursor encodes cursor data to a base64 string
func EncodeCursor(data map[string]interface{}) (string, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(jsonData), nil
}

// DecodeCursor decodes a base64 cursor string
func DecodeCursor(cursor string) (map[string]interface{}, error) {
	if cursor == "" {
		return map[string]interface{}{}, nil
	}

	jsonData, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return nil, err
	}

	var data map[string]interface{}
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return nil, err
	}

	return data, nil
}
