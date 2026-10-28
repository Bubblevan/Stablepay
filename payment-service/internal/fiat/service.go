// Package fiat extends payment-service with provider-neutral CNY orders.
// On-chain token amounts and fiat minor units deliberately have separate APIs.
package fiat

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/stablepay/payment-service/pkg/agentharness"
)

var ErrConflict = errors.New("payment request conflicts with an existing order")
var ErrNotFound = errors.New("payment order not found")

type Order struct {
	ID            string     `json:"id"`
	RequestID     string     `json:"request_id"`
	ProductID     string     `json:"product_id"`
	Subject       string     `json:"subject"`
	QuoteHash     string     `json:"quote_hash"`
	ApprovalRef   string     `json:"approval_ref"`
	AmountMinor   int64      `json:"amount_minor"`
	Currency      string     `json:"currency"`
	Mode          string     `json:"mode"`
	Status        string     `json:"status"`
	TradeNo       string     `json:"trade_no,omitempty"`
	VerifiedBy    string     `json:"verified_by,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	PaidAt        *time.Time `json:"paid_at,omitempty"`
	HarnessPolicy string     `json:"harness_policy,omitempty"`
}

type CreateRequest struct {
	RequestID   string `json:"request_id"`
	ProductID   string `json:"product_id"`
	Subject     string `json:"subject"`
	QuoteHash   string `json:"quote_hash"`
	ApprovalRef string `json:"approval_ref"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
}

type Receipt struct {
	OrderID     string
	TradeNo     string
	AmountMinor int64
	Status      string
	VerifiedBy  string
}

type Provider interface {
	Mode() string
	Checkout(Order) (string, error)
	Query(context.Context, Order) (Receipt, error)
	VerifyNotification(map[string]string) (Receipt, error)
}

type Store interface {
	// Update commits a complete snapshot atomically. Implementations serialize
	// writers, including callback/query races and idempotency checks.
	Update(func(map[string]Order) error) error
}

type Service struct {
	store          Store
	provider       Provider
	MaxAmountMinor int64
}

func NewService(store Store, provider Provider) *Service {
	return &Service{store: store, provider: provider, MaxAmountMinor: 10000}
}
func (s *Service) Mode() string { return s.provider.Mode() }

func (s *Service) Create(req CreateRequest) (Order, error) {
	// Reuse the existing Agent Harness policy. A reference to the immutable
	// upstream user confirmation is still mandatory below; policy alone is not
	// proof of consent or an instruction to initiate a payment.
	policy := agentharness.Policy{Version: "agent-payment-cny-v1", MaxAmountMinor: s.MaxAmountMinor}
	if result := policy.Evaluate(req.ProductID, req.AmountMinor); result.Decision == "DENY" {
		return Order{}, errors.New("CNY payment denied by Agent Harness policy")
	}
	if req.Currency != "CNY" || req.AmountMinor <= 0 || req.AmountMinor > s.MaxAmountMinor ||
		strings.TrimSpace(req.RequestID) == "" || len(req.RequestID) > 128 ||
		strings.TrimSpace(req.ProductID) == "" || strings.TrimSpace(req.Subject) == "" || len(req.Subject) > 180 ||
		strings.ContainsAny(req.Subject, "/=&") || strings.TrimSpace(req.QuoteHash) == "" || strings.TrimSpace(req.ApprovalRef) == "" {
		return Order{}, errors.New("invalid CNY payment request")
	}
	var result Order
	err := s.store.Update(func(orders map[string]Order) error {
		for _, existing := range orders {
			if existing.RequestID == req.RequestID {
				if existing.ProductID != req.ProductID || existing.AmountMinor != req.AmountMinor || existing.Currency != req.Currency ||
					existing.QuoteHash != req.QuoteHash || existing.ApprovalRef != req.ApprovalRef || existing.Subject != req.Subject || existing.Mode != s.Mode() {
					return ErrConflict
				}
				result = existing
				return nil
			}
		}
		if len(orders) >= 1000 {
			return errors.New("fiat demo order capacity reached; archive before creating more orders")
		}
		result = Order{ID: "SP" + ID(), RequestID: req.RequestID, ProductID: req.ProductID, Subject: req.Subject,
			QuoteHash: req.QuoteHash, ApprovalRef: req.ApprovalRef, AmountMinor: req.AmountMinor, Currency: "CNY",
			Mode: s.Mode(), Status: "AWAITING_PAYMENT", CreatedAt: time.Now().UTC(), HarnessPolicy: policy.Version}
		orders[result.ID] = result
		return nil
	})
	return result, err
}

func (s *Service) Get(id string) (Order, error) {
	var result Order
	err := s.store.Update(func(orders map[string]Order) error {
		var ok bool
		result, ok = orders[id]
		if !ok {
			return ErrNotFound
		}
		return nil
	})
	if err == nil && result.Mode != s.Mode() {
		return Order{}, errors.New("payment mode changed; use the original provider environment")
	}
	return result, err
}

// FindByRequest recovers the create-response crash window without issuing a
// second economic command. The endpoint is protected by the internal token.
func (s *Service) FindByRequest(requestID string) (Order, error) {
	var found Order
	err := s.store.Update(func(orders map[string]Order) error {
		for _, order := range orders {
			if order.RequestID == requestID {
				found = order
				return nil
			}
		}
		return ErrNotFound
	})
	if err == nil && found.Mode != s.Mode() {
		return Order{}, ErrConflict
	}
	return found, err
}

func (s *Service) Checkout(id string) (string, error) {
	order, err := s.Get(id)
	if err != nil {
		return "", err
	}
	if order.Status != "AWAITING_PAYMENT" {
		return "", ErrConflict
	}
	return s.provider.Checkout(order)
}

func (s *Service) Reconcile(ctx context.Context, id string) (Order, error) {
	order, err := s.Get(id)
	if err != nil {
		return Order{}, err
	}
	if order.Status == "PAID" || order.Status == "CLOSED" {
		return order, nil
	}
	receipt, err := s.provider.Query(ctx, order)
	if err != nil {
		return Order{}, err
	}
	if receipt.OrderID != order.ID || (s.Mode() != "local" && receipt.AmountMinor != order.AmountMinor) {
		return Order{}, ErrConflict
	}
	if receipt.Status == "AWAITING_PAYMENT" {
		return order, nil
	}
	return s.applyReceipt(receipt)
}

func (s *Service) Notify(params map[string]string) (Order, error) {
	receipt, err := s.provider.VerifyNotification(params)
	if err != nil {
		return Order{}, err
	}
	if receipt.Status == "AWAITING_PAYMENT" {
		order, err := s.Get(receipt.OrderID)
		if err == nil && receipt.AmountMinor != order.AmountMinor {
			return Order{}, ErrConflict
		}
		return order, err
	}
	return s.applyReceipt(receipt)
}

func (s *Service) Simulate(id string) (Order, error) {
	if s.Mode() != "local" {
		return Order{}, errors.New("local settlement is disabled for Alipay providers")
	}
	order, err := s.Get(id)
	if err != nil {
		return Order{}, err
	}
	return s.applyReceipt(Receipt{OrderID: id, TradeNo: "LOCAL_" + id, AmountMinor: order.AmountMinor, Status: "PAID", VerifiedBy: "local-simulation"})
}

func (s *Service) applyReceipt(receipt Receipt) (Order, error) {
	var result Order
	err := s.store.Update(func(orders map[string]Order) error {
		order, ok := orders[receipt.OrderID]
		if !ok {
			return ErrNotFound
		}
		if order.Mode != s.Mode() || receipt.AmountMinor != order.AmountMinor || receipt.VerifiedBy == "" {
			return ErrConflict
		}
		if receipt.Status != "PAID" && receipt.Status != "CLOSED" {
			return ErrConflict
		}
		if receipt.TradeNo == "" {
			return ErrConflict
		}
		for _, other := range orders {
			if other.ID != order.ID && other.TradeNo == receipt.TradeNo {
				return ErrConflict
			}
		}
		if order.Status == "PAID" {
			if receipt.TradeNo != order.TradeNo {
				return ErrConflict
			}
			result = order
			return nil // Never regress a verified success on reordered callbacks.
		}
		if order.Status == "CLOSED" && receipt.Status != "CLOSED" {
			return ErrConflict
		}
		order.Status, order.TradeNo, order.VerifiedBy = receipt.Status, receipt.TradeNo, receipt.VerifiedBy
		if receipt.Status == "PAID" {
			now := time.Now().UTC()
			order.PaidAt = &now
		}
		orders[order.ID] = order
		result = order
		return nil
	})
	return result, err
}

func ID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes[:])
}
func Digest(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func Yuan(cents int64) string { return fmt.Sprintf("%d.%02d", cents/100, cents%100) }
func ParseYuan(raw string) (int64, error) {
	parts := strings.Split(raw, ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, errors.New("invalid CNY amount")
	}
	whole := int64(0)
	for _, char := range parts[0] {
		if char < '0' || char > '9' || whole > 100000000 {
			return 0, errors.New("invalid CNY amount")
		}
		whole = whole*10 + int64(char-'0')
	}
	fraction := "00"
	if len(parts) == 2 {
		if len(parts[1]) < 1 || len(parts[1]) > 2 {
			return 0, errors.New("invalid CNY precision")
		}
		fraction = parts[1] + "0"
	}
	if fraction[0] < '0' || fraction[0] > '9' || fraction[1] < '0' || fraction[1] > '9' {
		return 0, errors.New("invalid CNY amount")
	}
	return whole*100 + int64(fraction[0]-'0')*10 + int64(fraction[1]-'0'), nil
}

type LocalProvider struct{}

func (LocalProvider) Mode() string                   { return "local" }
func (LocalProvider) Checkout(Order) (string, error) { return "", nil }
func (LocalProvider) Query(_ context.Context, order Order) (Receipt, error) {
	return Receipt{OrderID: order.ID, Status: order.Status}, nil
}
func (LocalProvider) VerifyNotification(map[string]string) (Receipt, error) {
	return Receipt{}, errors.New("Alipay notification is disabled in local mode")
}
