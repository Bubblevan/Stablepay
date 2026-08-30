package ratelimit

import (
	"context"
	"testing"
	"time"
)

func TestMemoryLimiterAllow(t *testing.T) {
	limiter := NewMemoryLimiter()
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		allowed, err := limiter.Allow(ctx, "k1", 3, time.Minute)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !allowed {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	allowed, err := limiter.Allow(ctx, "k1", 3, time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatal("request should be rejected")
	}
}
