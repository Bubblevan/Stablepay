package buyer

import (
	"testing"
	"time"
)

func TestSameIdempotentRequestIgnoresServerGeneratedIdentity(t *testing.T) {
	expires := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	first := &Confirmation{ConfirmationID: "buyer-confirmation-a", EpisodeID: "episode-1", RequestID: "request-1",
		RequesterDID: "did:solana:buyer", MerchantDID: "did:merchant:one", CapabilityID: "sku-service", ProductID: "sku-1",
		OfferRef: "offer-1", QuoteHash: "sha256:quote", AmountMinor: 25, Currency: "USDC", Accepted: true,
		IdempotencyKey: "approval-1", PayloadHash: "sha256:payload", Signature: "signature-a", ExpiresAt: expires,
		CreatedAt: expires.Add(-time.Minute)}
	retry := first.Clone()
	retry.ConfirmationID = "buyer-confirmation-b"
	retry.CreatedAt = expires.Add(-30 * time.Second)
	retry.Signature = "different-valid-signature"
	if !SameIdempotentRequest(first, retry) {
		t.Fatal("same signed approval payload was not recognized as an idempotent replay")
	}

	changed := retry.Clone()
	changed.AmountMinor++
	if SameIdempotentRequest(first, changed) {
		t.Fatal("changed approval amount was incorrectly accepted as an idempotent replay")
	}
}
