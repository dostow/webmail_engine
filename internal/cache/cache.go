package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"webmail_engine/internal/models"
)

// CacheKey prefixes for different cache entities
const (
	KeyPrefixAccount     = "acct:"
	KeyPrefixMessage     = "msg:"
	KeyPrefixEnvelope    = "env:"
	KeyPrefixThread      = "thd:"
	KeyPrefixFolder      = "fld:"
	KeyPrefixSearch      = "srch:"
	KeyPrefixTokenBucket = "tkn:"
	KeyPrefixAttachment  = "att:"
	KeyPrefixSession     = "sess:"
)

// Default TTL values
const (
	TTLAccount      = 1 * time.Hour
	TTLMessage      = 24 * time.Hour
	TTLEnvelope     = 12 * time.Hour
	TTLThread       = 6 * time.Hour
	TTLFolder       = 30 * time.Minute
	TTLSearch       = 15 * time.Minute
	TTLTokenBucket  = 1 * time.Minute
	TTLAttachment   = 24 * time.Hour
	TTLSession      = 2 * time.Hour
)

// RedisClient is an interface for Redis operations
type RedisClient interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	HGet(ctx context.Context, key, field string) ([]byte, error)
	HSet(ctx context.Context, key string, fields map[string]interface{}) error
	HGetAll(ctx context.Context, key string) (map[string][]byte, error)
	HDel(ctx context.Context, key string, fields ...string) error
	Incr(ctx context.Context, key string) (int64, error)
	Decr(ctx context.Context, key string) (int64, error)
	Expire(ctx context.Context, key string, ttl time.Duration) error
	TTL(ctx context.Context, key string) (time.Duration, error)
	Keys(ctx context.Context, pattern string) ([]string, error)
	Scan(ctx context.Context, cursor uint64, match string, count int64) ([]string, uint64, error)
	Ping(ctx context.Context) error
	Close() error
}

// Cache provides caching functionality for the webmail engine
type Cache struct {
	client RedisClient
}

// CacheStats tracks cache statistics
type CacheStats struct {
	Hits       int64 `json:"hits"`
	Misses     int64 `json:"misses"`
	Sets       int64 `json:"sets"`
	Deletes    int64 `json:"deletes"`
	Errors     int64 `json:"errors"`
	HitRate    float64 `json:"hit_rate"`
	LastUpdate time.Time `json:"last_update"`
}

// NewCache creates a new cache instance
func NewCache(client RedisClient) *Cache {
	return &Cache{
		client: client,
	}
}

// Account cache operations

// GetAccount retrieves an account from cache
func (c *Cache) GetAccount(ctx context.Context, accountID string) (*models.Account, error) {
	if c == nil || c.client == nil {
		return nil, nil // Cache unavailable, not an error
	}
	
	key := fmt.Sprintf("%s%s", KeyPrefixAccount, accountID)
	data, err := c.client.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	
	var account models.Account
	if err := json.Unmarshal(data, &account); err != nil {
		return nil, err
	}
	
	return &account, nil
}

// SetAccount stores an account in cache
func (c *Cache) SetAccount(ctx context.Context, account *models.Account) error {
	if c == nil || c.client == nil {
		return nil // Cache unavailable, not an error
	}
	
	key := fmt.Sprintf("%s%s", KeyPrefixAccount, account.ID)
	data, err := json.Marshal(account)
	if err != nil {
		return err
	}
	
	return c.client.Set(ctx, key, data, TTLAccount)
}

// DeleteAccount removes an account from cache
func (c *Cache) DeleteAccount(ctx context.Context, accountID string) error {
	if c == nil || c.client == nil {
		return nil
	}
	
	key := fmt.Sprintf("%s%s", KeyPrefixAccount, accountID)
	return c.client.Delete(ctx, key)
}

// Message cache operations

// GetMessage retrieves a message from cache
func (c *Cache) GetMessage(ctx context.Context, accountID, uid, folder string) (*models.Message, error) {
	if c == nil || c.client == nil {
		return nil, nil
	}
	
	key := fmt.Sprintf("%s%s:%s:%s", KeyPrefixMessage, accountID, folder, uid)
	data, err := c.client.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	
	var message models.Message
	if err := json.Unmarshal(data, &message); err != nil {
		return nil, err
	}
	
	return &message, nil
}

// SetMessage stores a message in cache
func (c *Cache) SetMessage(ctx context.Context, accountID string, message *models.Message) error {
	if c == nil || c.client == nil {
		return nil
	}
	
	key := fmt.Sprintf("%s%s:%s:%s", KeyPrefixMessage, accountID, message.Folder, message.UID)
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	
	return c.client.Set(ctx, key, data, TTLMessage)
}

// DeleteMessage removes a message from cache
func (c *Cache) DeleteMessage(ctx context.Context, accountID, uid, folder string) error {
	if c == nil || c.client == nil {
		return nil
	}
	
	key := fmt.Sprintf("%s%s:%s:%s", KeyPrefixMessage, accountID, folder, uid)
	return c.client.Delete(ctx, key)
}

// Envelope cache operations (lightweight message metadata)

// GetEnvelope retrieves a message envelope from cache
func (c *Cache) GetEnvelope(ctx context.Context, accountID, uid, folder string) (*models.MessageSummary, error) {
	if c == nil || c.client == nil {
		return nil, nil
	}
	
	key := fmt.Sprintf("%s%s:%s:%s", KeyPrefixEnvelope, accountID, folder, uid)
	data, err := c.client.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	
	var envelope models.MessageSummary
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	
	return &envelope, nil
}

// SetEnvelope stores a message envelope in cache
func (c *Cache) SetEnvelope(ctx context.Context, accountID string, envelope *models.MessageSummary) error {
	if c == nil || c.client == nil {
		return nil
	}
	
	key := fmt.Sprintf("%s%s:%s:%s", KeyPrefixEnvelope, accountID, envelope.Folder, envelope.UID)
	data, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	
	return c.client.Set(ctx, key, data, TTLEnvelope)
}

// SetEnvelopes stores multiple envelopes in cache
func (c *Cache) SetEnvelopes(ctx context.Context, accountID string, envelopes []models.MessageSummary) error {
	if c == nil || c.client == nil {
		return nil
	}
	
	for _, env := range envelopes {
		if err := c.SetEnvelope(ctx, accountID, &env); err != nil {
			return err
		}
	}
	return nil
}

// Thread cache operations

// GetThread retrieves messages in a thread
func (c *Cache) GetThread(ctx context.Context, accountID, threadID string) ([]string, error) {
	key := fmt.Sprintf("%s%s:%s", KeyPrefixThread, accountID, threadID)
	data, err := c.client.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	
	var uids []string
	if err := json.Unmarshal(data, &uids); err != nil {
		return nil, err
	}
	
	return uids, nil
}

// SetThread stores thread message UIDs in cache
func (c *Cache) SetThread(ctx context.Context, accountID, threadID string, uids []string) error {
	key := fmt.Sprintf("%s%s:%s", KeyPrefixThread, accountID, threadID)
	data, err := json.Marshal(uids)
	if err != nil {
		return err
	}
	
	return c.client.Set(ctx, key, data, TTLThread)
}

// Folder cache operations

// GetFolderInfo retrieves folder information
func (c *Cache) GetFolderInfo(ctx context.Context, accountID, folder string) (*models.FolderInfo, error) {
	key := fmt.Sprintf("%s%s:%s", KeyPrefixFolder, accountID, folder)
	data, err := c.client.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	
	var info models.FolderInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}
	
	return &info, nil
}

// SetFolderInfo stores folder information in cache
func (c *Cache) SetFolderInfo(ctx context.Context, accountID, folder string, info *models.FolderInfo) error {
	key := fmt.Sprintf("%s%s:%s", KeyPrefixFolder, accountID, folder)
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	
	return c.client.Set(ctx, key, data, TTLFolder)
}

// Search cache operations

// GetSearchResults retrieves cached search results
func (c *Cache) GetSearchResults(ctx context.Context, queryHash string) (*models.SearchResult, error) {
	if c == nil || c.client == nil {
		return nil, nil
	}
	
	key := fmt.Sprintf("%s%s", KeyPrefixSearch, queryHash)
	data, err := c.client.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	
	var result models.SearchResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	
	return &result, nil
}

// SetSearchResults stores search results in cache
func (c *Cache) SetSearchResults(ctx context.Context, queryHash string, result *models.SearchResult) error {
	if c == nil || c.client == nil {
		return nil
	}
	
	key := fmt.Sprintf("%s%s", KeyPrefixSearch, queryHash)
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	
	return c.client.Set(ctx, key, data, TTLSearch)
}

// Token bucket cache operations

// GetTokenBucket retrieves token bucket state
func (c *Cache) GetTokenBucket(ctx context.Context, accountID string) (*models.TokenBucket, error) {
	key := fmt.Sprintf("%s%s", KeyPrefixTokenBucket, accountID)
	data, err := c.client.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	
	var bucket models.TokenBucket
	if err := json.Unmarshal(data, &bucket); err != nil {
		return nil, err
	}
	
	return &bucket, nil
}

// SetTokenBucket stores token bucket state in cache
func (c *Cache) SetTokenBucket(ctx context.Context, bucket *models.TokenBucket) error {
	key := fmt.Sprintf("%s%s", KeyPrefixTokenBucket, bucket.AccountID)
	data, err := json.Marshal(bucket)
	if err != nil {
		return err
	}
	
	return c.client.Set(ctx, key, data, TTLTokenBucket)
}

// Attachment cache operations

// GetAttachmentInfo retrieves attachment metadata
func (c *Cache) GetAttachmentInfo(ctx context.Context, attachmentID string) (*models.Attachment, error) {
	key := fmt.Sprintf("%s%s", KeyPrefixAttachment, attachmentID)
	data, err := c.client.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	
	var attachment models.Attachment
	if err := json.Unmarshal(data, &attachment); err != nil {
		return nil, err
	}
	
	return &attachment, nil
}

// SetAttachmentInfo stores attachment metadata in cache
func (c *Cache) SetAttachmentInfo(ctx context.Context, attachment *models.Attachment) error {
	key := fmt.Sprintf("%s%s", KeyPrefixAttachment, attachment.ID)
	data, err := json.Marshal(attachment)
	if err != nil {
		return err
	}
	
	return c.client.Set(ctx, key, data, TTLAttachment)
}

// Cache status and utilities

// GetStats retrieves cache statistics
func (c *Cache) GetStats(ctx context.Context) (*CacheStats, error) {
	// This would typically track hits/misses in memory
	// For now, return basic stats
	return &CacheStats{
		HitRate:    0.0,
		LastUpdate: time.Now(),
	}, nil
}

// Ping checks if cache is available
func (c *Cache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx)
}

// Close closes the cache connection
func (c *Cache) Close() error {
	return c.client.Close()
}

// InvalidateAccount invalidates all cache entries for an account
func (c *Cache) InvalidateAccount(ctx context.Context, accountID string) error {
	patterns := []string{
		fmt.Sprintf("%s%s*", KeyPrefixMessage, accountID),
		fmt.Sprintf("%s%s*", KeyPrefixEnvelope, accountID),
		fmt.Sprintf("%s%s*", KeyPrefixThread, accountID),
		fmt.Sprintf("%s%s*", KeyPrefixFolder, accountID),
		fmt.Sprintf("%s%s*", KeyPrefixSearch, accountID),
	}

	for _, pattern := range patterns {
		keys, err := c.client.Keys(ctx, pattern)
		if err != nil {
			continue
		}

		for _, key := range keys {
			c.client.Delete(ctx, key)
		}
	}

	// Also delete account itself
	c.DeleteAccount(ctx, accountID)

	return nil
}

// Generic cache operations

// Get retrieves raw bytes from cache
func (c *Cache) Get(ctx context.Context, key string) ([]byte, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("cache unavailable")
	}

	data, err := c.client.Get(ctx, key)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// Set stores raw bytes in cache with TTL
func (c *Cache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if c == nil || c.client == nil {
		return nil // Cache unavailable, not an error
	}

	return c.client.Set(ctx, key, value, ttl)
}
