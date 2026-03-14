package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisWrapper implements the RedisClient interface
type RedisWrapper struct {
	client *redis.Client
}

// RedisConfig holds Redis connection configuration
type RedisConfig struct {
	Host     string
	Port     int
	Password string
	DB       int
	PoolSize int
}

// DefaultRedisConfig returns default Redis configuration
func DefaultRedisConfig() RedisConfig {
	return RedisConfig{
		Host:     "localhost",
		Port:     6379,
		Password: "",
		DB:       0,
		PoolSize: 10,
	}
}

// NewRedisClient creates a new Redis client
func NewRedisClient(config RedisConfig) (*RedisWrapper, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", config.Host, config.Port),
		Password: config.Password,
		DB:       config.DB,
		PoolSize: config.PoolSize,
	})
	
	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}
	
	return &RedisWrapper{
		client: client,
	}, nil
}

// Get retrieves a value from Redis
func (r *RedisWrapper) Get(ctx context.Context, key string) ([]byte, error) {
	val, err := r.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}
	return val, nil
}

// Set stores a value in Redis
func (r *RedisWrapper) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return r.client.Set(ctx, key, value, ttl).Err()
}

// Delete removes a value from Redis
func (r *RedisWrapper) Delete(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

// Exists checks if a key exists in Redis
func (r *RedisWrapper) Exists(ctx context.Context, key string) (bool, error) {
	n, err := r.client.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// HGet retrieves a field from a hash
func (r *RedisWrapper) HGet(ctx context.Context, key, field string) ([]byte, error) {
	val, err := r.client.HGet(ctx, key, field).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}
	return val, nil
}

// HSet stores fields in a hash
func (r *RedisWrapper) HSet(ctx context.Context, key string, fields map[string]interface{}) error {
	return r.client.HSet(ctx, key, fields).Err()
}

// HGetAll retrieves all fields from a hash
func (r *RedisWrapper) HGetAll(ctx context.Context, key string) (map[string][]byte, error) {
	val, err := r.client.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	
	result := make(map[string][]byte)
	for k, v := range val {
		result[k] = []byte(v)
	}
	return result, nil
}

// HDel removes fields from a hash
func (r *RedisWrapper) HDel(ctx context.Context, key string, fields ...string) error {
	return r.client.HDel(ctx, key, fields...).Err()
}

// Incr increments a value
func (r *RedisWrapper) Incr(ctx context.Context, key string) (int64, error) {
	return r.client.Incr(ctx, key).Result()
}

// Decr decrements a value
func (r *RedisWrapper) Decr(ctx context.Context, key string) (int64, error) {
	return r.client.Decr(ctx, key).Result()
}

// Expire sets the expiration for a key
func (r *RedisWrapper) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return r.client.Expire(ctx, key, ttl).Err()
}

// TTL gets the TTL for a key
func (r *RedisWrapper) TTL(ctx context.Context, key string) (time.Duration, error) {
	return r.client.TTL(ctx, key).Result()
}

// Keys returns keys matching a pattern
func (r *RedisWrapper) Keys(ctx context.Context, pattern string) ([]string, error) {
	return r.client.Keys(ctx, pattern).Result()
}

// Scan scans keys using cursor
func (r *RedisWrapper) Scan(ctx context.Context, cursor uint64, match string, count int64) ([]string, uint64, error) {
	return r.client.Scan(ctx, cursor, match, count).Result()
}

// Ping checks Redis connectivity
func (r *RedisWrapper) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

// Close closes the Redis connection
func (r *RedisWrapper) Close() error {
	return r.client.Close()
}
