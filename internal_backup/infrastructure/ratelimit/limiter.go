package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type Limiter interface {
	Allow(ctx context.Context, key string, max int, window time.Duration) (bool, error)
}

type MemoryLimiter struct {
	mu       sync.Mutex
	counters map[string]int
	expires  map[string]time.Time
}

func NewMemoryLimiter() *MemoryLimiter {
	return &MemoryLimiter{
		counters: make(map[string]int),
		expires:  make(map[string]time.Time),
	}
}

func (l *MemoryLimiter) Allow(_ context.Context, key string, max int, window time.Duration) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if exp, ok := l.expires[key]; !ok || now.After(exp) {
		l.expires[key] = now.Add(window)
		l.counters[key] = 0
	}
	l.counters[key]++
	return l.counters[key] <= max, nil
}

type RedisLimiter struct {
	client *redis.Client
	prefix string
}

func NewRedisLimiter(client *redis.Client, prefix string) *RedisLimiter {
	return &RedisLimiter{
		client: client,
		prefix: prefix,
	}
}

func (l *RedisLimiter) Allow(ctx context.Context, key string, max int, window time.Duration) (bool, error) {
	rKey := fmt.Sprintf("%s%s", l.prefix, key)
	count, err := l.client.Incr(ctx, rKey).Result()
	if err != nil {
		return false, err
	}
	if count == 1 {
		if err := l.client.Expire(ctx, rKey, window).Err(); err != nil {
			return false, err
		}
	}
	return int(count) <= max, nil
}
