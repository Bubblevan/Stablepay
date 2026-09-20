package adapters

import (
	"context"
	"errors"
	"strings"
)

// RealDIDPolicyAdapter is the runtime authorization policy for the unified
// E2E. It is intentionally not a fake cryptographic verifier: payment-service
// is the signature authority and verifies the PaymentCredentials through the
// real DID service. This adapter enforces the runtime's immutable intent
// binding before Kitex submission (requester, merchant, capability, payee,
// quote, amount and currency).
type RealDIDPolicyAdapter struct{}

func (RealDIDPolicyAdapter) AuthorizePayment(_ context.Context, request AuthorizationRequest) (AuthorizationResult, error) {
	intent := request.Intent
	if request.PayeeDID != intent.PayeeDID || strings.TrimSpace(intent.RequesterDID) == "" {
		return AuthorizationResult{Allowed: false, Reason: "runtime DID/payment binding mismatch"}, nil
	}
	if err := intent.Validate(); err != nil {
		return AuthorizationResult{Allowed: false, Reason: err.Error()}, nil
	}
	if intent.AmountMinor <= 0 || strings.TrimSpace(intent.Currency) == "" {
		return AuthorizationResult{Allowed: false, Reason: errors.New("invalid payment amount or currency").Error()}, nil
	}
	return AuthorizationResult{
		Allowed: true, RequesterDID: intent.RequesterDID, MerchantDID: intent.MerchantDID,
		CapabilityID: intent.CapabilityID, PayeeDID: intent.PayeeDID, QuoteHash: intent.QuoteHash,
		AmountMinor: intent.AmountMinor, Currency: intent.Currency,
		AuthorizationRef: "did-policy:" + intent.IntentID,
	}, nil
}

var _ DIDAdapter = RealDIDPolicyAdapter{}
