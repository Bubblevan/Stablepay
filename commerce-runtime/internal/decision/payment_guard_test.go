package decision

import (
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/adapters"
	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/payment"
)

func guardFixtures(t *testing.T) (*episode.CommerceEpisode, *payment.PaymentIntent, adapters.AuthorizationResult, time.Time) {
	t.Helper()
	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	request := contract.AcquireCapabilityRequest{
		RequestID: "request-1", RequesterDID: "did:agent:1", AcquisitionGoal: contract.AcquisitionGoal{TaskType: "task", Description: "description"},
		Input:          contract.Input{URI: "object://input", ContentType: "text/plain"},
		Constraints:    contract.Constraints{BudgetLimitMinor: 1000, Currency: "USDC", DeadlineAt: now.Add(time.Hour), MaxTotalAttempts: 10, MaxPaymentAttempts: 2, MaxDeliveryAttempts: 2},
		ExpectedOutput: contract.ExpectedOutput{Schema: "output", ContentType: "text/plain"}, Validator: contract.ValidatorRef{Kind: "builtin", Name: "validator", Version: "v1"},
	}
	current, err := episode.New("episode-1", request, now)
	if err != nil {
		t.Fatal(err)
	}
	current.State = episode.StatePaying
	current.Version = 5
	current.SelectedMerchantDID = "did:merchant:1"
	current.SelectedCapabilityID = "capability-1"
	current.CurrentQuoteHash = "sha256:quote"
	current.Budget.ReservedAmount = 300
	current.Budget.AvailableBudget = 700
	if err := current.Validate(); err != nil {
		t.Fatal(err)
	}
	intent := &payment.PaymentIntent{
		IntentID: "pi-1", EpisodeID: current.EpisodeID, MerchantDID: current.SelectedMerchantDID, CapabilityID: current.SelectedCapabilityID,
		QuoteHash: current.CurrentQuoteHash, AmountMinor: 300, Currency: "USDC", RequesterDID: current.RequesterDID,
		EpisodeVersion: current.Version, BudgetReservation: 300, IdempotencyKey: "pay-1", EconomicKey: payment.EconomicIdentityKey(current.EpisodeID, current.SelectedMerchantDID, current.SelectedCapabilityID, current.CurrentQuoteHash, 300, "USDC"),
		ExpiresAt: now.Add(30 * time.Minute), Status: payment.IntentAuthorized, CreatedAt: now, UpdatedAt: now,
	}
	authorization := adapters.AuthorizationResult{Allowed: true, RequesterDID: intent.RequesterDID, MerchantDID: intent.MerchantDID, CapabilityID: intent.CapabilityID,
		QuoteHash: intent.QuoteHash, AmountMinor: intent.AmountMinor, Currency: intent.Currency, AuthorizationRef: "auth-1"}
	return current, intent, authorization, now
}

func TestPaymentGuardRejectsStaleOrMismatchedIntent(t *testing.T) {
	current, intent, authorization, now := guardFixtures(t)
	intent.EpisodeVersion = current.Version + 1
	if err := (RuntimeGuard{}).CheckPayment(current, intent, authorization, now); err != ErrStalePaymentIntent {
		t.Fatalf("expected stale intent, got %v", err)
	}
	intent.EpisodeVersion = current.Version
	current.Budget.ReservedAmount = 299
	current.Budget.AvailableBudget = 701
	if err := (RuntimeGuard{}).CheckPayment(current, intent, authorization, now); err != ErrPaymentBudgetReservation {
		t.Fatalf("expected reservation mismatch, got %v", err)
	}
	current.Budget.ReservedAmount = 300
	current.Budget.AvailableBudget = 700
	intent.QuoteHash = "sha256:other"
	intent.EconomicKey = payment.EconomicIdentityKey(intent.EpisodeID, intent.MerchantDID, intent.CapabilityID, intent.QuoteHash, intent.AmountMinor, intent.Currency)
	if err := (RuntimeGuard{}).CheckPayment(current, intent, authorization, now); err != ErrPaymentQuoteMismatch {
		t.Fatalf("expected quote mismatch, got %v", err)
	}
}

func TestPaymentGuardRejectsExpiryAndAttemptLimit(t *testing.T) {
	current, intent, authorization, now := guardFixtures(t)
	if err := (RuntimeGuard{}).CheckPayment(current, intent, authorization, intent.ExpiresAt); err != ErrPaymentIntentExpired {
		t.Fatalf("expected expired intent, got %v", err)
	}
	current.PaymentAttemptCount = current.MaxPaymentAttempts
	if err := (RuntimeGuard{}).CheckPayment(current, intent, authorization, now); err != ErrPaymentAttemptLimit {
		t.Fatalf("expected payment attempt limit, got %v", err)
	}
}

func TestPaymentGuardRequiresTrustedAuthorizationBinding(t *testing.T) {
	current, intent, authorization, now := guardFixtures(t)
	authorization.AmountMinor++
	if err := (RuntimeGuard{}).CheckPayment(current, intent, authorization, now); err != ErrPaymentBindingMismatch {
		t.Fatalf("expected authorization binding mismatch, got %v", err)
	}
	authorization.AmountMinor = intent.AmountMinor
	authorization.Allowed = false
	authorization.Reason = "policy denied"
	if err := (RuntimeGuard{}).CheckPayment(current, intent, authorization, now); err != ErrPaymentAuthorization {
		t.Fatalf("expected authorization denial, got %v", err)
	}
}
