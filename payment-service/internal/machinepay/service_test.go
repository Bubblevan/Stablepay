package machinepay

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

type fakeStore struct {
	orders map[string]Order
	keys   map[string]string
}

func newFakeStore() *fakeStore {
	return &fakeStore{orders: map[string]Order{}, keys: map[string]string{}}
}
func (s *fakeStore) GetOrCreate(_ context.Context, key, resource, goods string, amount int64, created, expires time.Time) (Order, error) {
	keyID := resource + ":" + key
	if id, ok := s.keys[keyID]; ok {
		order := s.orders[id]
		if order.GoodsName != goods || order.AmountMinor != amount {
			return Order{}, ErrConflict
		}
		return order, nil
	}
	order := Order{ID: fmt.Sprintf("MP%030d", len(s.orders)+1), IdempotencyKey: key, ResourceID: resource, GoodsName: goods,
		AmountMinor: amount, Status: "AWAITING_PAYMENT", CreatedAt: created, ExpiresAt: expires}
	s.orders[order.ID] = order
	s.keys[keyID] = order.ID
	return order, nil
}
func (s *fakeStore) SavePaymentLink(_ context.Context, id, link string) (Order, error) {
	order, ok := s.orders[id]
	if !ok {
		return Order{}, ErrNotFound
	}
	order.PaymentLink = link
	s.orders[id] = order
	return order, nil
}
func (s *fakeStore) Find(_ context.Context, id string) (Order, error) {
	order, ok := s.orders[id]
	if !ok {
		return Order{}, ErrNotFound
	}
	return order, nil
}
func (s *fakeStore) Settle(_ context.Context, id string, receipt Receipt) (Order, error) {
	order, ok := s.orders[id]
	if !ok {
		return Order{}, ErrNotFound
	}
	if receipt.OrderID != id || receipt.ResourceID != order.ResourceID || receipt.AmountMinor != order.AmountMinor || !receipt.Active || receipt.TradeNo == "" || receipt.VerifiedBy == "" {
		return Order{}, ErrConflict
	}
	if order.Status == "PAID" {
		if order.TradeNo != receipt.TradeNo {
			return Order{}, ErrConflict
		}
		return order, nil
	}
	if order.Status != "AWAITING_PAYMENT" || time.Now().After(order.ExpiresAt) {
		return Order{}, ErrConflict
	}
	now := time.Now().UTC()
	order.Status, order.TradeNo, order.PaidAt = "PAID", receipt.TradeNo, &now
	s.orders[id] = order
	return order, nil
}
func (s *fakeStore) PendingEvents(context.Context, int) ([]SettledEvent, error) { return nil, nil }
func (s *fakeStore) MarkEventPublished(context.Context, string) error           { return nil }

type fakeProvider struct {
	precreates int
	receipt    Receipt
}

func (p *fakeProvider) Precreate(_ context.Context, _ Order) (string, error) {
	p.precreates++
	return "sandbox-link", nil
}
func (p *fakeProvider) VerifyProof(context.Context, string) (Receipt, error) { return p.receipt, nil }

func TestAccessCreatesStableChallengeAndSettlesBoundProof(t *testing.T) {
	store, provider := newFakeStore(), &fakeProvider{}
	service := NewService(store, provider)
	request := AccessRequest{IdempotencyKey: "call-1", ResourceID: "verify-batch", GoodsName: "Batch API", AmountMinor: 1}
	challenge, err := service.Access(context.Background(), request)
	if err != nil || challenge.Authorized || challenge.PaymentLink != "sandbox-link" || challenge.Status != "AWAITING_PAYMENT" {
		t.Fatalf("challenge=%+v err=%v", challenge, err)
	}
	request.IdempotencyKey = "call-1"
	again, err := service.Access(context.Background(), request)
	if err != nil || again.OrderID != challenge.OrderID || provider.precreates != 1 {
		t.Fatalf("challenge wasn't idempotent: %+v err=%v precreates=%d", again, err, provider.precreates)
	}
	provider.receipt = Receipt{OrderID: challenge.OrderID, TradeNo: "ALI-1", ResourceID: request.ResourceID, AmountMinor: 1, Active: true, VerifiedBy: "alipay-machinepay-proof"}
	request.PaymentProof = "base64-proof"
	settled, err := service.Access(context.Background(), request)
	if err != nil || !settled.Authorized || settled.Status != "PAID" || settled.OrderID != challenge.OrderID {
		t.Fatalf("settled=%+v err=%v", settled, err)
	}
	replayed, err := service.Access(context.Background(), request)
	if err != nil || !replayed.Authorized || replayed.Status != "PAID" {
		t.Fatalf("idempotent paid retry=%+v err=%v", replayed, err)
	}
}

func TestAccessRejectsWrongResourceAndMissingIdempotency(t *testing.T) {
	store, provider := newFakeStore(), &fakeProvider{}
	service := NewService(store, provider)
	request := AccessRequest{IdempotencyKey: "call-2", ResourceID: "verify-batch", GoodsName: "Batch API", AmountMinor: 1}
	challenge, err := service.Access(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	provider.receipt = Receipt{OrderID: challenge.OrderID, TradeNo: "ALI-2", ResourceID: "other-resource", AmountMinor: 1, Active: true, VerifiedBy: "alipay-machinepay-proof"}
	request.PaymentProof = "wrong-resource-proof"
	if _, err := service.Access(context.Background(), request); !errors.Is(err, ErrConflict) {
		t.Fatalf("wrong resource error=%v", err)
	}
	if _, err := service.Access(context.Background(), AccessRequest{ResourceID: "verify-batch", GoodsName: "Batch API", AmountMinor: 1}); err == nil {
		t.Fatal("missing idempotency key was accepted")
	}
	if _, err := service.Access(context.Background(), AccessRequest{ResourceID: "verify-batch", GoodsName: "Batch API", AmountMinor: 1, PaymentProof: "proof"}); err == nil {
		t.Fatal("payment proof without idempotency key was accepted")
	}
	if _, err := service.Access(context.Background(), AccessRequest{IdempotencyKey: "other-call", ResourceID: "verify-batch", GoodsName: "Batch API", AmountMinor: 1, PaymentProof: "proof"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("proof bound to a different idempotency key was accepted: %v", err)
	}
}
