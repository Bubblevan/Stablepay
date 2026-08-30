// Package redis Redis 连接管理
package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/redis/go-redis/v9"
	"github.com/stablepay/payment-service/internal/infrastructure/config"
)

// RedisClient Redis 客户端包装
type RedisClient struct {
	client *redis.Client
}

// NewRedisClient 创建 Redis 客户端
func NewRedisClient(cfg *config.Config) (*RedisClient, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         cfg.GetRedisAddr(),
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		PoolSize:     cfg.Redis.PoolSize,
		MinIdleConns: cfg.Redis.MinIdleConns,
	})

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	hlog.Info("Redis connected successfully")
	return &RedisClient{client: client}, nil
}

// Close 关闭连接
func (r *RedisClient) Close() error {
	return r.client.Close()
}

// IsDuplicate 检查 nonce 是否重复
func (r *RedisClient) IsDuplicate(ctx context.Context, nonce string) (bool, error) {
	key := fmt.Sprintf("payment:nonce:%s", nonce)
	exists, err := r.client.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return exists > 0, nil
}

// RecordNonce 记录 nonce
func (r *RedisClient) RecordNonce(ctx context.Context, nonce string, ttlMinutes int) error {
	key := fmt.Sprintf("payment:nonce:%s", nonce)
	return r.client.Set(ctx, key, "1", time.Duration(ttlMinutes)*time.Minute).Err()
}

// Set 设置键值
func (r *RedisClient) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	return r.client.Set(ctx, key, value, ttl).Err()
}

// Get 获取键值
func (r *RedisClient) Get(ctx context.Context, key string) (string, error) {
	return r.client.Get(ctx, key).Result()
}

// Del 删除键
func (r *RedisClient) Del(ctx context.Context, keys ...string) error {
	return r.client.Del(ctx, keys...).Err()
}

// Incr 递增
func (r *RedisClient) Incr(ctx context.Context, key string) (int64, error) {
	return r.client.Incr(ctx, key).Result()
}

// Expire 设置过期时间
func (r *RedisClient) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return r.client.Expire(ctx, key, ttl).Err()
}
