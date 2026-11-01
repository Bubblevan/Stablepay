package fiat

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func requestFixture() CreateRequest {
	return CreateRequest{RequestID: "purchase-1", IdempotencyKey: "idem-1", Nonce: "nonce-1", AgentDID: "did:solana:agent", SkillDID: "did:solana:skill",
		ProductID: "review", Subject: "采购预审服务", QuoteHash: "quote-hash", ApprovalRef: "approval-1", AmountMinor: 3, Currency: "CNY"}
}
func localService(t *testing.T) (*Service, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "orders.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	return NewService(store, LocalProvider{}), path
}
func TestConcurrentIdempotencyAndRestart(t *testing.T) {
	s, path := localService(t)
	var wg sync.WaitGroup
	ids := make(chan string, 20)
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			order, err := s.Create(requestFixture())
			if err != nil {
				t.Error(err)
				return
			}
			ids <- order.ID
		}()
	}
	wg.Wait()
	close(ids)
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatal("duplicate economic order")
		}
	}
	paid, err := s.Simulate(first)
	if err != nil || paid.VerifiedBy != "local-simulation" {
		t.Fatalf("%+v %v", paid, err)
	}
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewService(store, LocalProvider{})
	again, err := restarted.Create(requestFixture())
	if err != nil || again.ID != first || again.Status != "PAID" {
		t.Fatalf("restart replay failed: %+v %v", again, err)
	}
	changed := requestFixture()
	changed.AmountMinor = 4
	if _, err := s.Create(changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("amount rebinding accepted: %v", err)
	}
}

func TestNonceIsAtomicAndScopedToPayer(t *testing.T) {
	store := NewMemoryNonceStore()
	first, err := store.MarkIfNotExists(t.Context(), "agent:once", time.Minute)
	if err != nil || !first {
		t.Fatalf("claim failed: %v %v", first, err)
	}
	second, err := store.MarkIfNotExists(t.Context(), "agent:once", time.Minute)
	if err != nil || second {
		t.Fatalf("duplicate nonce was accepted: %v %v", second, err)
	}
	other, err := store.MarkIfNotExists(t.Context(), "other:once", time.Minute)
	if err != nil || !other {
		t.Fatalf("nonce scope collided: %v %v", other, err)
	}
}

func TestReceiptCannotRebindAmountOrTrade(t *testing.T) {
	s, _ := localService(t)
	first, _ := s.Create(requestFixture())
	bad := Receipt{OrderID: first.ID, AmountMinor: 4, TradeNo: "T1", Status: "PAID", VerifiedBy: "test-signed"}
	if _, err := s.applyReceipt(bad); !errors.Is(err, ErrConflict) {
		t.Fatal("wrong amount accepted")
	}
	bad.AmountMinor = 3
	if _, err := s.applyReceipt(bad); err != nil {
		t.Fatal(err)
	}
	secondRequest := requestFixture()
	secondRequest.RequestID = "purchase-2"
	secondRequest.IdempotencyKey = "idem-2"
	secondRequest.Nonce = "nonce-2"
	second, _ := s.Create(secondRequest)
	bad.OrderID = second.ID
	if _, err := s.applyReceipt(bad); !errors.Is(err, ErrConflict) {
		t.Fatal("trade reused across orders")
	}
	bad.OrderID = first.ID
	bad.Status = "CLOSED"
	result, err := s.applyReceipt(bad)
	if err != nil || result.Status != "PAID" {
		t.Fatal("late callback regressed success")
	}
}

func TestLocalSettlementIsUnavailableForSandboxMode(t *testing.T) {
	s, _ := localService(t)
	order, _ := s.Create(requestFixture())
	s.provider = &modeProvider{mode: "alipay_sandbox"}
	if _, err := s.Simulate(order.ID); err == nil {
		t.Fatal("local settlement enabled for Alipay")
	}
}

type modeProvider struct{ mode string }

func (p *modeProvider) Mode() string                                      { return p.mode }
func (*modeProvider) Checkout(_ context.Context, _ Order) (string, error) { return "", nil }
func (*modeProvider) Query(_ context.Context, order Order) (Receipt, error) {
	return Receipt{OrderID: order.ID, Status: order.Status}, nil
}
func (*modeProvider) VerifyNotification(context.Context, map[string]string) (Receipt, error) {
	return Receipt{}, errors.New("disabled")
}
