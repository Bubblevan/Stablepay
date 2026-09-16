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
	Intent   payment.PaymentIntent
	PayeeDID string
	Now      time.Time
}

type AuthorizationResult struct {
	Allowed          bool
	RequesterDID     string
	MerchantDID      string
	CapabilityID     string
	PayeeDID         string
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
	if strings.TrimSpace(r.RequesterDID) == "" || strings.TrimSpace(r.MerchantDID) == "" || strings.TrimSpace(r.CapabilityID) == "" || strings.TrimSpace(r.PayeeDID) == "" || strings.TrimSpace(r.QuoteHash) == "" || r.AmountMinor <= 0 || strings.TrimSpace(r.Currency) == "" || strings.TrimSpace(r.AuthorizationRef) == "" {
		return ErrInvalidAuthorization
	}
	return nil
}

type DIDAdapter interface {
	AuthorizePayment(context.Context, AuthorizationRequest) (AuthorizationResult, error)
}

type PaymentSubmitRequest struct {
	Intent             payment.PaymentIntent
	Authorization      AuthorizationResult
	PayeeDID           string
	RequestFingerprint string
	TraceID            string
}

type PaymentQuery struct {
	Intent   payment.PaymentIntent
	PayeeDID string
	TraceID  string
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
	PayeeDID     string
	RequesterDID string
}

type EntitlementStatus string

const (
	EntitlementValid   EntitlementStatus = "VALID"
	EntitlementInvalid EntitlementStatus = "INVALID"
	EntitlementUnknown EntitlementStatus = "UNKNOWN"
)

type EntitlementResult struct {
	Status          EntitlementStatus
	Reference       string
	PaymentIntentID string
	TxID            string
	TxHash          string
	EvidenceRef     string
	Reason          string
}

// MatchesIntent prevents a valid entitlement for another capability or
// previous purchase from becoming payment evidence for this intent.
func (r EntitlementResult) MatchesIntent(intent payment.PaymentIntent) bool {
	if r.Status != EntitlementValid || strings.TrimSpace(r.EvidenceRef) == "" && strings.TrimSpace(r.Reference) == "" {
		return false
	}
	matchedIdentity := false
	if r.PaymentIntentID != "" {
		if r.PaymentIntentID != intent.IntentID {
			return false
		}
		matchedIdentity = true
	}
	if r.TxID != "" {
		if r.TxID != intent.TxID {
			return false
		}
		matchedIdentity = true
	}
	if r.TxHash != "" {
		if r.TxHash != intent.TxHash {
			return false
		}
		matchedIdentity = true
	}
	return matchedIdentity
}

type EntitlementAdapter interface {
	Verify(context.Context, EntitlementQuery) (EntitlementResult, error)
}
