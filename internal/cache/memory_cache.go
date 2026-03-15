package cache

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemoryClient implements RedisClient interface using in-memory storage
type MemoryClient struct {
	mu      sync.RWMutex
	data    map[string][]byte
	expires map[string]time.Time
	maxSize int
}

// MemoryClientConfig holds configuration for in-memory cache
type MemoryClientConfig struct {
	MaxSize int // Maximum number of keys (0 = unlimited)
}

// DefaultMemoryClientConfig returns default configuration
func DefaultMemoryClientConfig() MemoryClientConfig {
	return MemoryClientConfig{
		MaxSize: 10000, // Default max 10k keys
	}
}

// NewMemoryClient creates a new in-memory cache client
func NewMemoryClient(config MemoryClientConfig) *MemoryClient {
	return &MemoryClient{
		data:    make(map[string][]byte),
		expires: make(map[string]time.Time),
		maxSize: config.MaxSize,
	}
}

// Get retrieves a value from memory cache
func (m *MemoryClient) Get(ctx context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check if key exists and not expired
	if exp, ok := m.expires[key]; ok {
		if time.Now().After(exp) {
			return nil, nil // Key expired, treat as not found
		}
	}

	val, ok := m.data[key]
	if !ok {
		return nil, nil
	}

	// Return a copy to prevent external modification
	result := make([]byte, len(val))
	copy(result, val)
	return result, nil
}

// Set stores a value in memory cache with TTL
func (m *MemoryClient) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check size limit before adding new key
	if _, exists := m.data[key]; !exists && m.maxSize > 0 && len(m.data) >= m.maxSize {
		m.evictOldest()
	}

	// Store value (copy to prevent external modification)
	valCopy := make([]byte, len(value))
	copy(valCopy, value)
	m.data[key] = valCopy

	// Set expiration if TTL provided
	if ttl > 0 {
		m.expires[key] = time.Now().Add(ttl)
	} else {
		delete(m.expires, key)
	}

	return nil
}

// Delete removes a value from memory cache
func (m *MemoryClient) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.data, key)
	delete(m.expires, key)
	return nil
}

// Exists checks if a key exists in memory cache
func (m *MemoryClient) Exists(ctx context.Context, key string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check if key exists and not expired
	if exp, ok := m.expires[key]; ok {
		if time.Now().After(exp) {
			return false, nil
		}
	}

	_, exists := m.data[key]
	return exists, nil
}

// HGet retrieves a field from a hash (not implemented for memory cache)
func (m *MemoryClient) HGet(ctx context.Context, key, field string) ([]byte, error) {
	return nil, fmt.Errorf("HGet not implemented for memory cache")
}

// HSet stores fields in a hash (not implemented for memory cache)
func (m *MemoryClient) HSet(ctx context.Context, key string, fields map[string]interface{}) error {
	return fmt.Errorf("HSet not implemented for memory cache")
}

// HGetAll retrieves all fields from a hash (not implemented for memory cache)
func (m *MemoryClient) HGetAll(ctx context.Context, key string) (map[string][]byte, error) {
	return nil, fmt.Errorf("HGetAll not implemented for memory cache")
}

// HDel removes fields from a hash (not implemented for memory cache)
func (m *MemoryClient) HDel(ctx context.Context, key string, fields ...string) error {
	return fmt.Errorf("HDel not implemented for memory cache")
}

// Incr increments a value (not implemented for memory cache)
func (m *MemoryClient) Incr(ctx context.Context, key string) (int64, error) {
	return 0, fmt.Errorf("Incr not implemented for memory cache")
}

// Decr decrements a value (not implemented for memory cache)
func (m *MemoryClient) Decr(ctx context.Context, key string) (int64, error) {
	return 0, fmt.Errorf("Decr not implemented for memory cache")
}

// Expire sets the expiration for a key
func (m *MemoryClient) Expire(ctx context.Context, key string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.data[key]; exists {
		m.expires[key] = time.Now().Add(ttl)
	}
	return nil
}

// TTL gets the TTL for a key
func (m *MemoryClient) TTL(ctx context.Context, key string) (time.Duration, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if exp, ok := m.expires[key]; ok {
		remaining := time.Until(exp)
		if remaining <= 0 {
			return 0, nil
		}
		return remaining, nil
	}
	return -1, nil // No expiration set
}

// Keys returns keys matching a pattern (supports * wildcard)
func (m *MemoryClient) Keys(ctx context.Context, pattern string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var matches []string

	// Convert glob pattern to simple matching
	prefix, hasWildcard := strings.CutSuffix(pattern, "*")

	for key := range m.data {
		var matched bool
		if hasWildcard {
			matched = strings.HasPrefix(key, prefix)
		} else {
			matched = key == pattern
		}

		if matched {
			// Check if not expired
			if exp, ok := m.expires[key]; ok {
				if time.Now().After(exp) {
					continue
				}
			}
			matches = append(matches, key)
		}
	}

	// Sort for consistent ordering
	sort.Strings(matches)
	return matches, nil
}

// Scan scans keys using cursor (simplified implementation)
func (m *MemoryClient) Scan(ctx context.Context, cursor uint64, match string, count int64) ([]string, uint64, error) {
	keys, err := m.Keys(ctx, match)
	if err != nil {
		return nil, 0, err
	}

	// Simple pagination based on cursor
	start := int(cursor)
	if start >= len(keys) {
		return nil, 0, nil
	}

	end := start + int(count)
	if end > len(keys) {
		end = len(keys)
	}

	var nextCursor uint64
	if end < len(keys) {
		nextCursor = uint64(end)
	}

	return keys[start:end], nextCursor, nil
}

// Ping checks if memory cache is available
func (m *MemoryClient) Ping(ctx context.Context) error {
	return nil // Always available
}

// Close closes the memory cache (no-op)
func (m *MemoryClient) Close() error {
	return nil
}

// evictOldest removes the oldest expired or least recently used key
func (m *MemoryClient) evictOldest() {
	// First try to find expired keys
	now := time.Now()
	for key, exp := range m.expires {
		if now.After(exp) {
			delete(m.data, key)
			delete(m.expires, key)
			return
		}
	}

	// If no expired keys, remove the first key (oldest insertion)
	// In a more sophisticated implementation, we could track access times
	for key := range m.data {
		delete(m.data, key)
		delete(m.expires, key)
		return
	}
}

// Stats returns cache statistics
func (m *MemoryClient) Stats() (keys int, expired int) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	now := time.Now()
	for _, exp := range m.expires {
		if now.After(exp) {
			expired++
		}
	}

	return len(m.data), expired
}
