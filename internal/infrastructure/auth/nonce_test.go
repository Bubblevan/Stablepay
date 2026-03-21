package auth

import (
	"context"
	"testing"
	"time"
)

func TestMemoryNonceStore(t *testing.T) {
	store := NewMemoryNonceStore()
	ok, err := store.MarkIfNotExists(context.Background(), "nonce-1", time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("first nonce should pass")
	}
	ok, err = store.MarkIfNotExists(context.Background(), "nonce-1", time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("replay nonce should fail")
	}
}
