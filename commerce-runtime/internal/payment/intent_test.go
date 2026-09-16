package payment

import (
	"errors"
	"testing"
	"time"
)

func validIntent(now time.Time) PaymentIntent {
	intent := PaymentIntent{
		IntentID: "intent-1", EpisodeID: "episode-1", MerchantDID: "merchant-1", CapabilityID: "capability-1", PayeeDID: "payee-1",
		QuoteHash: "sha256:quote", AmountMinor: 300, Currency: "USDC", RequesterDID: "did:requester",
		EpisodeVersion: 5, BudgetReservation: 300, IdempotencyKey: "pay-1",
		EconomicKey: EconomicIdentityKey("episode-1", "merchant-1", "capability-1", "payee-1", "sha256:quote", 300, "USDC"),
		ExpiresAt:   now.Add(time.Hour), Status: IntentCreated, CredentialRef: "credential:pay-1", CreatedAt: now, UpdatedAt: now,
	}
	intent.RequestFingerprint = RequestFingerprint(intent)
	return intent
}

func TestPaymentIntentEconomicIdentityAndExpiry(t *testing.T) {
	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	intent := validIntent(now)
	if err := intent.Validate(); err != nil {
		t.Fatal(err)
	}
	if intent.EconomicKey != EconomicIdentityKey("episode-1", "merchant-1", "capability-1", "payee-1", "sha256:quote", 300, "USDC") {
		t.Fatal("economic key is not deterministic")
	}
	if intent.CanSubmitAt(now.Add(2*time.Hour)) == nil || !errors.Is(intent.CanSubmitAt(now.Add(2*time.Hour)), ErrIntentExpired) {
		t.Fatal("expired intent was accepted")
	}
	changedAmount := EconomicIdentityKey("episode-1", "merchant-1", "capability-1", "payee-1", "sha256:quote", 301, "USDC")
	if changedAmount == intent.EconomicKey {
		t.Fatal("different amount reused economic identity")
	}
}
