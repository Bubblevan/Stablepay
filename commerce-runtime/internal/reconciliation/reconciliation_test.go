package reconciliation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/adapters"
	"github.com/stablepay/commerce-runtime/internal/payment"
)

func reconciliationIntent() payment.PaymentIntent {
	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	intent := payment.PaymentIntent{
		IntentID: "pi-1", EpisodeID: "episode-1", MerchantDID: "did:merchant:1", CapabilityID: "capability-1", PayeeDID: "did:payee:1",
		QuoteHash: "sha256:quote", AmountMinor: 300, Currency: "USDC", RequesterDID: "did:agent:1",
		EpisodeVersion: 5, BudgetReservation: 300, IdempotencyKey: "pay-1", EconomicKey: payment.EconomicIdentityKey("episode-1", "did:merchant:1", "capability-1", "did:payee:1", "sha256:quote", 300, "USDC"),
		ExpiresAt: now.Add(time.Hour), Status: payment.IntentPending, CredentialRef: "credential:pay-1", TxID: "tx-1", TxHash: "hash-1", CreatedAt: now, UpdatedAt: now,
	}
	intent.RequestFingerprint = payment.RequestFingerprint(intent)
	return intent
}

type paymentStatusFunc func(context.Context, adapters.PaymentQuery) (payment.PaymentOutcome, error)

func (f paymentStatusFunc) Query(ctx context.Context, query adapters.PaymentQuery) (payment.PaymentOutcome, error) {
	return f(ctx, query)
}

type chainStatusFunc func(context.Context, adapters.PaymentQuery) (payment.PaymentOutcome, error)

func (f chainStatusFunc) QueryTransaction(ctx context.Context, query adapters.PaymentQuery) (payment.PaymentOutcome, error) {
	return f(ctx, query)
}

type entitlementFunc func(context.Context, adapters.EntitlementQuery) (adapters.EntitlementResult, error)

func (f entitlementFunc) Verify(ctx context.Context, query adapters.EntitlementQuery) (adapters.EntitlementResult, error) {
	return f(ctx, query)
}

func TestResolverReturnsConfirmedFromPaymentStatus(t *testing.T) {
	intent := reconciliationIntent()
	called := false
	result, err := (Resolver{PaymentStatus: paymentStatusFunc(func(_ context.Context, query adapters.PaymentQuery) (payment.PaymentOutcome, error) {
		called = true
		if query.Intent.IntentID != intent.IntentID {
			t.Fatalf("query used the wrong intent: %#v", query.Intent)
		}
		return payment.PaymentOutcome{Status: payment.OutcomeConfirmed, TxID: "tx-1", TxHash: "hash-1", AmountMinor: 300, Currency: "USDC"}, nil
	})}).Resolve(context.Background(), intent, payment.PaymentOutcome{Status: payment.OutcomePending})
	if err != nil || !called || result.Source != "payment_status" || result.Outcome.Status != payment.OutcomeConfirmed {
		t.Fatalf("unexpected confirmation resolution: %#v err=%v called=%v", result, err, called)
	}
}

func TestResolverReturnsFailedAndKeepsPendingWithoutSubmission(t *testing.T) {
	intent := reconciliationIntent()
	failed, err := (Resolver{PaymentStatus: paymentStatusFunc(func(context.Context, adapters.PaymentQuery) (payment.PaymentOutcome, error) {
		return payment.PaymentOutcome{Status: payment.OutcomeFailed, Reason: "declined"}, nil
	})}).Resolve(context.Background(), intent, payment.PaymentOutcome{Status: payment.OutcomePending})
	if err != nil || failed.Outcome.Status != payment.OutcomeFailed || failed.Source != "payment_status" {
		t.Fatalf("unexpected failed resolution: %#v err=%v", failed, err)
	}

	pending, err := (Resolver{PaymentStatus: paymentStatusFunc(func(context.Context, adapters.PaymentQuery) (payment.PaymentOutcome, error) {
		return payment.PaymentOutcome{Status: payment.OutcomePending}, nil
	})}).Resolve(context.Background(), intent, payment.PaymentOutcome{Status: payment.OutcomePending})
	if err != nil || pending.Outcome.Status != payment.OutcomePending || pending.Source != "unresolved" {
		t.Fatalf("unexpected pending resolution: %#v err=%v", pending, err)
	}
}

func TestResolverUsesChainAndEntitlementEvidenceForUnknownOutcome(t *testing.T) {
	intent := reconciliationIntent()
	chain, err := (Resolver{
		PaymentStatus: paymentStatusFunc(func(context.Context, adapters.PaymentQuery) (payment.PaymentOutcome, error) {
			return payment.PaymentOutcome{}, errors.New("status timeout")
		}),
		ChainStatus: chainStatusFunc(func(_ context.Context, query adapters.PaymentQuery) (payment.PaymentOutcome, error) {
			if query.Intent.TxHash != intent.TxHash {
				t.Fatalf("chain query lost transaction identity")
			}
			return payment.PaymentOutcome{Status: payment.OutcomeConfirmed, TxID: "tx-1", TxHash: "hash-1", AmountMinor: 300, Currency: "USDC"}, nil
		}),
	}).Resolve(context.Background(), intent, payment.PaymentOutcome{Status: payment.OutcomeUnknown, Reason: "submit timeout"})
	if err != nil || chain.Outcome.Status != payment.OutcomeConfirmed || chain.Source != "chain_status" {
		t.Fatalf("unexpected chain resolution: %#v err=%v", chain, err)
	}

	entitlement, err := (Resolver{
		PaymentStatus: paymentStatusFunc(func(context.Context, adapters.PaymentQuery) (payment.PaymentOutcome, error) {
			return payment.PaymentOutcome{}, errors.New("status timeout")
		}),
		Entitlement: entitlementFunc(func(_ context.Context, query adapters.EntitlementQuery) (adapters.EntitlementResult, error) {
			if query.IntentID != intent.IntentID || query.TxID != intent.TxID {
				t.Fatalf("entitlement query lost payment identity: %#v", query)
			}
			return adapters.EntitlementResult{Status: adapters.EntitlementValid, PaymentIntentID: intent.IntentID, EvidenceRef: "entitlement-1"}, nil
		}),
	}).Resolve(context.Background(), intent, payment.PaymentOutcome{Status: payment.OutcomeUnknown, Reason: "rpc timeout"})
	if err != nil || entitlement.Outcome.Status != payment.OutcomeConfirmed || entitlement.Source != "entitlement" {
		t.Fatalf("unexpected entitlement resolution: %#v err=%v", entitlement, err)
	}
}

func TestResolverNeverSubmitsOrRetries(t *testing.T) {
	intent := reconciliationIntent()
	queries := 0
	resolver := Resolver{PaymentStatus: paymentStatusFunc(func(context.Context, adapters.PaymentQuery) (payment.PaymentOutcome, error) {
		queries++
		return payment.PaymentOutcome{Status: payment.OutcomePending}, nil
	})}
	first, err := resolver.Resolve(context.Background(), intent, payment.PaymentOutcome{Status: payment.OutcomePending})
	if err != nil || first.Outcome.Status != payment.OutcomePending {
		t.Fatalf("pending resolution failed: %#v err=%v", first, err)
	}
	second, err := resolver.Resolve(context.Background(), intent, first.Outcome)
	if err != nil || second.Outcome.Status != payment.OutcomePending || queries != 2 {
		t.Fatalf("reconciliation did not remain pending deterministically: first=%#v second=%#v queries=%d err=%v", first, second, queries, err)
	}
}

func TestResolverRejectsPaymentEvidenceForAnotherIntent(t *testing.T) {
	intent := reconciliationIntent()
	resolution, err := (Resolver{PaymentStatus: paymentStatusFunc(func(context.Context, adapters.PaymentQuery) (payment.PaymentOutcome, error) {
		return payment.PaymentOutcome{Status: payment.OutcomeConfirmed, IntentID: "other-intent", TxID: "other-tx", TxHash: "other-hash", AmountMinor: intent.AmountMinor, Currency: intent.Currency}, nil
	})}).Resolve(context.Background(), intent, payment.PaymentOutcome{Status: payment.OutcomeUnknown})
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Outcome.Status == payment.OutcomeConfirmed || resolution.Source != "unresolved" {
		t.Fatalf("cross-intent payment status became confirmation: %#v", resolution)
	}
}
