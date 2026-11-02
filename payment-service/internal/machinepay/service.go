package machinepay

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrConflict = errors.New("Machine Pay request conflicts with an existing order")
	ErrNotFound = errors.New("Machine Pay order not found")
)

type Order struct {
	ID             string
	IdempotencyKey string
	ResourceID     string
	GoodsName      string
	AmountMinor    int64
	PaymentLink    string
	Status         string
	TradeNo        string
	CreatedAt      time.Time
	ExpiresAt      time.Time
	PaidAt         *time.Time
}

type Receipt struct {
	OrderID     string
	TradeNo     string
	ResourceID  string
	AmountMinor int64
	Active      bool
	VerifiedBy  string
}

type AccessRequest struct {
	IdempotencyKey string
	ResourceID     string
	GoodsName      string
	AmountMinor    int64
	PaymentProof   string
}

type AccessResult struct {
	Authorized  bool   `json:"authorized"`
	OrderID     string `json:"order_id"`
	PaymentLink string `json:"payment_link,omitempty"`
	ResourceID  string `json:"resource_id"`
	AmountMinor int64  `json:"amount_minor"`
	Status      string `json:"status"`
}

type Provider interface {
	Precreate(context.Context, Order) (string, error)
	VerifyProof(context.Context, string) (Receipt, error)
}

type Store interface {
	GetOrCreate(context.Context, string, string, string, int64, time.Time, time.Time) (Order, error)
	SavePaymentLink(context.Context, string, string) (Order, error)
	Find(context.Context, string) (Order, error)
	Settle(context.Context, string, Receipt) (Order, error)
	PendingEvents(context.Context, int) ([]SettledEvent, error)
	MarkEventPublished(context.Context, string) error
}

type Service struct {
	store    Store
	provider Provider
	ttl      time.Duration
}

func NewService(store Store, provider Provider) *Service {
	return &Service{store: store, provider: provider, ttl: 10 * time.Minute}
}

// Access returns a precreated Alipay payment challenge when proof is absent.
// A valid proof is verified by Alipay Adapter, bound to the persisted resource
// order, then atomically settled before the gateway releases the API response.
func (s *Service) Access(ctx context.Context, req AccessRequest) (AccessResult, error) {
	if s == nil || s.store == nil || s.provider == nil {
		return AccessResult{}, errors.New("Machine Pay service is unavailable")
	}
	if err := validateRequest(req); err != nil {
		return AccessResult{}, err
	}
	if strings.TrimSpace(req.PaymentProof) != "" {
		if strings.TrimSpace(req.IdempotencyKey) == "" || len(req.IdempotencyKey) > 128 {
			return AccessResult{}, errors.New("X-Idempotency-Key is required for Machine Pay resources")
		}
		receipt, err := s.provider.VerifyProof(ctx, req.PaymentProof)
		if err != nil {
			return AccessResult{}, err
		}
		if !receipt.Active || receipt.VerifiedBy == "" || receipt.TradeNo == "" || receipt.OrderID == "" ||
			receipt.ResourceID != req.ResourceID || receipt.AmountMinor != req.AmountMinor {
			return AccessResult{}, ErrConflict
		}
		order, err := s.store.Find(ctx, receipt.OrderID)
		if err != nil {
			return AccessResult{}, err
		}
		if order.IdempotencyKey != req.IdempotencyKey || order.ResourceID != req.ResourceID || order.AmountMinor != req.AmountMinor || order.GoodsName != req.GoodsName ||
			!order.ExpiresAt.IsZero() && time.Now().After(order.ExpiresAt) {
			return AccessResult{}, ErrConflict
		}
		settled, err := s.store.Settle(ctx, order.ID, receipt)
		if err != nil {
			return AccessResult{}, err
		}
		return accessResult(settled, true), nil
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" || len(req.IdempotencyKey) > 128 {
		return AccessResult{}, errors.New("X-Idempotency-Key is required for Machine Pay resources")
	}
	now := time.Now().UTC()
	order, err := s.store.GetOrCreate(ctx, req.IdempotencyKey, req.ResourceID, req.GoodsName, req.AmountMinor, now, now.Add(s.ttl))
	if err != nil {
		return AccessResult{}, err
	}
	if order.Status == "PAID" {
		return accessResult(order, true), nil
	}
	if order.Status != "AWAITING_PAYMENT" {
		return AccessResult{}, ErrConflict
	}
	if strings.TrimSpace(order.PaymentLink) == "" {
		link, err := s.provider.Precreate(ctx, order)
		if err != nil {
			return AccessResult{}, err
		}
		order, err = s.store.SavePaymentLink(ctx, order.ID, link)
		if err != nil {
			return AccessResult{}, err
		}
	}
	return accessResult(order, false), nil
}

func validateRequest(req AccessRequest) error {
	if strings.TrimSpace(req.ResourceID) == "" || len([]rune(req.ResourceID)) > 32 ||
		strings.TrimSpace(req.GoodsName) == "" || len([]rune(req.GoodsName)) > 32 ||
		req.AmountMinor < 1 || req.AmountMinor > 5000 {
		return errors.New("Machine Pay requires resource and goods names up to 32 characters and CNY 0.01-50.00")
	}
	if len(req.PaymentProof) > 32<<10 {
		return errors.New("Payment-Proof header is too large")
	}
	return nil
}

func accessResult(order Order, authorized bool) AccessResult {
	return AccessResult{Authorized: authorized, OrderID: order.ID, PaymentLink: order.PaymentLink,
		ResourceID: order.ResourceID, AmountMinor: order.AmountMinor, Status: order.Status}
}

func newOrderID() (string, error) {
	var raw [15]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate Machine Pay order ID: %w", err)
	}
	return "MP" + hex.EncodeToString(raw[:]), nil
}
