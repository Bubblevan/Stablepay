package decision

import (
	"errors"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/adapters"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/payment"
)

var (
	ErrStalePaymentIntent       = errors.New("payment intent is stale for the current episode")
	ErrPaymentQuoteMismatch     = errors.New("payment intent quote does not match the current episode")
	ErrPaymentBindingMismatch   = errors.New("payment intent binding does not match trusted authorization")
	ErrPaymentBudgetReservation = errors.New("payment intent has no current budget reservation")
	ErrPaymentAttemptLimit      = errors.New("payment attempt limit has been reached")
	ErrPaymentIntentExpired     = errors.New("payment intent has expired")
	ErrPaymentAuthorization     = errors.New("payment authorization was denied")
)

// CheckPayment is the final deterministic permission check immediately before
// an adapter call. All spend fields come from the persisted intent and trusted
// authorization result; a proposal cannot override any of them.
func (RuntimeGuard) CheckPayment(current *episode.CommerceEpisode, intent *payment.PaymentIntent, authorization adapters.AuthorizationResult, now time.Time) error {
	if current == nil || intent == nil {
		return ErrInvalidProposal
	}
	if err := current.Validate(); err != nil {
		return err
	}
	if err := intent.Validate(); err != nil {
		return err
	}
	if err := authorization.Validate(); err != nil {
		return err
	}
	if !authorization.Allowed {
		return ErrPaymentAuthorization
	}
	if current.State != episode.StatePaying {
		return ErrActionNotAllowed
	}
	if current.Version != intent.EpisodeVersion {
		return ErrStalePaymentIntent
	}
	if current.CurrentQuoteHash != intent.QuoteHash || current.SelectedMerchantDID != intent.MerchantDID || current.SelectedCapabilityID != intent.CapabilityID {
		return ErrPaymentQuoteMismatch
	}
	if current.RequesterDID == "" || current.RequesterDID != intent.RequesterDID {
		return ErrPaymentBindingMismatch
	}
	if intent.AmountMinor != intent.BudgetReservation || current.Budget.Currency != intent.Currency || current.Budget.ReservedAmount < intent.BudgetReservation {
		return ErrPaymentBudgetReservation
	}
	if authorization.RequesterDID != intent.RequesterDID || authorization.MerchantDID != intent.MerchantDID || authorization.CapabilityID != intent.CapabilityID || authorization.PayeeDID != intent.PayeeDID || authorization.QuoteHash != intent.QuoteHash || authorization.AmountMinor != intent.AmountMinor || strings.ToUpper(authorization.Currency) != strings.ToUpper(intent.Currency) {
		return ErrPaymentBindingMismatch
	}
	if !now.Before(current.DeadlineAt) || !now.Before(intent.ExpiresAt) {
		return ErrPaymentIntentExpired
	}
	if current.PaymentAttemptCount >= current.MaxPaymentAttempts {
		return ErrPaymentAttemptLimit
	}
	return nil
}
