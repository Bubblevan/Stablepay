// Package adapters contains the only runtime boundary to external DID,
// payment, chain and entitlement services. Decision providers do not receive
// these interfaces and cannot cause a financial side effect directly.
package adapters

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/payment"
)

var ErrInvalidAuthorization = errors.New("invalid payment authorization result")

type AuthorizationRequest struct {
	Intent payment.PaymentIntent
	Now    time.Time
}

type AuthorizationResult struct {
	Allowed          bool
	RequesterDID     string
	MerchantDID      string
	CapabilityID     string
	QuoteHash        string
	AmountMinor      int64
	Currency         string
	AuthorizationRef string
	Reason           string
}

func (r AuthorizationResult) Validate() error {
	if !r.Allowed {
		if strings.TrimSpace(r.Reason) == "" {
			return ErrInvalidAuthorization
		}
		return nil
	}
	if strings.TrimSpace(r.RequesterDID) == "" || strings.TrimSpace(r.MerchantDID) == "" || strings.TrimSpace(r.CapabilityID) == "" || strings.TrimSpace(r.QuoteHash) == "" || r.AmountMinor <= 0 || strings.TrimSpace(r.Currency) == "" || strings.TrimSpace(r.AuthorizationRef) == "" {
		return ErrInvalidAuthorization
	}
	return nil
}

type DIDAdapter interface {
	AuthorizePayment(context.Context, AuthorizationRequest) (AuthorizationResult, error)
}

type PaymentSubmitRequest struct {
	Intent        payment.PaymentIntent
	Authorization AuthorizationResult
	TraceID       string
}

type PaymentQuery struct {
	Intent  payment.PaymentIntent
	TraceID string
}

type PaymentAdapter interface {
	Submit(context.Context, PaymentSubmitRequest) (payment.PaymentOutcome, error)
}

type PaymentStatusAdapter interface {
	Query(context.Context, PaymentQuery) (payment.PaymentOutcome, error)
}

type ChainStatusAdapter interface {
	QueryTransaction(context.Context, PaymentQuery) (payment.PaymentOutcome, error)
}

type EntitlementQuery struct {
	EpisodeID    string
	IntentID     string
	TxID         string
	MerchantDID  string
	CapabilityID string
	RequesterDID string
}

type EntitlementStatus string

const (
	EntitlementValid   EntitlementStatus = "VALID"
	EntitlementInvalid EntitlementStatus = "INVALID"
	EntitlementUnknown EntitlementStatus = "UNKNOWN"
)

type EntitlementResult struct {
	Status    EntitlementStatus
	Reference string
	Reason    string
}

type EntitlementAdapter interface {
	Verify(context.Context, EntitlementQuery) (EntitlementResult, error)
}
