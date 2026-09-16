// Package reconciliation resolves uncertain payment outcomes with ordered,
// deterministic facts. It never retries a payment submission.
package reconciliation

import (
	"context"
	"errors"

	"github.com/stablepay/commerce-runtime/internal/adapters"
	"github.com/stablepay/commerce-runtime/internal/payment"
)

var ErrInvalidResolution = errors.New("invalid payment reconciliation resolution")

type Resolution struct {
	Outcome payment.PaymentOutcome
	Source  string
}

type Resolver struct {
	PaymentStatus adapters.PaymentStatusAdapter
	ChainStatus   adapters.ChainStatusAdapter
	Entitlement   adapters.EntitlementAdapter
}

// Resolve follows the S2 order: payment identity, chain identity if needed,
// then entitlement evidence. A missing or failed query is not permission to
// submit again; unresolved cases remain UNKNOWN.
func (r Resolver) Resolve(ctx context.Context, intent payment.PaymentIntent, initial payment.PaymentOutcome) (Resolution, error) {
	if err := intent.Validate(); err != nil {
		return Resolution{}, err
	}
	if err := initial.Validate(); err != nil {
		return Resolution{}, err
	}
	initial = fillIdentity(intent, initial)
	if initial.Status == payment.OutcomeConfirmed || initial.Status == payment.OutcomeFailed {
		return Resolution{Outcome: initial, Source: "submit"}, nil
	}

	latest := initial
	if r.PaymentStatus != nil {
		if queried, err := r.PaymentStatus.Query(ctx, adapters.PaymentQuery{Intent: intent}); err == nil {
			latest = fillIdentity(intent, queried)
			if latest.Status == payment.OutcomeConfirmed || latest.Status == payment.OutcomeFailed {
				return Resolution{Outcome: latest, Source: "payment_status"}, nil
			}
		}
	}
	if r.ChainStatus != nil && (latest.Status == payment.OutcomeUnknown || latest.Status == payment.OutcomePending) && intent.TxHash != "" {
		if queried, err := r.ChainStatus.QueryTransaction(ctx, adapters.PaymentQuery{Intent: intent}); err == nil {
			latest = fillIdentity(intent, queried)
			if latest.Status == payment.OutcomeConfirmed || latest.Status == payment.OutcomeFailed {
				return Resolution{Outcome: latest, Source: "chain_status"}, nil
			}
		}
	}
	if r.Entitlement != nil && (latest.Status == payment.OutcomeUnknown || latest.Status == payment.OutcomePending) {
		result, err := r.Entitlement.Verify(ctx, adapters.EntitlementQuery{
			EpisodeID: intent.EpisodeID, IntentID: intent.IntentID, TxID: intent.TxID,
			MerchantDID: intent.MerchantDID, CapabilityID: intent.CapabilityID, RequesterDID: intent.RequesterDID,
		})
		if err == nil && result.Status == adapters.EntitlementValid {
			latest.Status = payment.OutcomeConfirmed
			latest.Reason = "entitlement evidence confirms payment"
			return Resolution{Outcome: latest, Source: "entitlement"}, nil
		}
	}
	if latest.Status != payment.OutcomePending && latest.Status != payment.OutcomeUnknown {
		return Resolution{}, ErrInvalidResolution
	}
	return Resolution{Outcome: latest, Source: "unresolved"}, nil
}

func fillIdentity(intent payment.PaymentIntent, outcome payment.PaymentOutcome) payment.PaymentOutcome {
	if outcome.IntentID == "" {
		outcome.IntentID = intent.IntentID
	}
	if outcome.TxID == "" {
		outcome.TxID = intent.TxID
	}
	if outcome.TxHash == "" {
		outcome.TxHash = intent.TxHash
	}
	if outcome.AmountMinor == 0 {
		outcome.AmountMinor = intent.AmountMinor
	}
	if outcome.Currency == "" {
		outcome.Currency = intent.Currency
	}
	return outcome
}
