package auth

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type NonceStore interface {
	MarkIfNotExists(ctx context.Context, key string, ttl time.Duration) (bool, error)
}

type MemoryNonceStore struct {
	mu    sync.Mutex
	items map[string]time.Time
}

func NewMemoryNonceStore() *MemoryNonceStore {
	return &MemoryNonceStore{items: make(map[string]time.Time)}
}

func (s *MemoryNonceStore) MarkIfNotExists(_ context.Context, key string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, exp := range s.items {
		if now.After(exp) {
			delete(s.items, k)
		}
	}
	if _, exists := s.items[key]; exists {
		return false, nil
	}
	s.items[key] = now.Add(ttl)
	return true, nil
}

type RedisNonceStore struct {
	client *redis.Client
	prefix string
}

func NewRedisNonceStore(client *redis.Client, prefix string) *RedisNonceStore {
	return &RedisNonceStore{client: client, prefix: prefix}
}

func (s *RedisNonceStore) MarkIfNotExists(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return s.client.SetNX(ctx, s.prefix+key, "1", ttl).Result()
}
