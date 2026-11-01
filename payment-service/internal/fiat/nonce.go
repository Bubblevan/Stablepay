package fiat

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisNonceStore struct {
	client nonceSetter
	prefix string
}

type nonceSetter interface {
	SetNX(context.Context, string, interface{}, time.Duration) *redis.BoolCmd
}

func NewRedisNonceStore(client nonceSetter, prefix string) *RedisNonceStore {
	return &RedisNonceStore{client: client, prefix: prefix}
}
func (s *RedisNonceStore) MarkIfNotExists(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return s.client.SetNX(ctx, s.prefix+key, "1", ttl).Result()
}

type MemoryNonceStore struct {
	mu      sync.Mutex
	expires map[string]time.Time
}

func NewMemoryNonceStore() *MemoryNonceStore {
	return &MemoryNonceStore{expires: map[string]time.Time{}}
}
func (s *MemoryNonceStore) MarkIfNotExists(_ context.Context, key string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for existing, expiry := range s.expires {
		if !now.Before(expiry) {
			delete(s.expires, existing)
		}
	}
	if _, exists := s.expires[key]; exists {
		return false, nil
	}
	s.expires[key] = now.Add(ttl)
	return true, nil
}
