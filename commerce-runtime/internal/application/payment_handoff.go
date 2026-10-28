package application

import (
	"context"
	"errors"

	"github.com/stablepay/commerce-runtime/internal/adapters"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/payment"
)

// GetPaymentHandoff reuses the existing persisted intent. It cannot create an
// intent, auto-confirm a buyer offer, submit a second payment, or settle money.
func (s *Service) GetPaymentHandoff(ctx context.Context, episodeID string) (adapters.PaymentHandoff, error) {
	store, err := s.financeStore()
	if err != nil {
		return adapters.PaymentHandoff{}, err
	}
	current, err := s.store.Get(ctx, episodeID)
	if err != nil {
		return adapters.PaymentHandoff{}, err
	}
	if current.State != episode.StatePaying || !s.clock().Before(current.DeadlineAt) {
		return adapters.PaymentHandoff{}, errors.New("episode is not awaiting payment")
	}
	intents, err := store.ListPaymentIntents(ctx, episodeID)
	if err != nil {
		return adapters.PaymentHandoff{}, err
	}
	adapter, ok := s.paymentDeps.Payment.(adapters.PaymentHandoffAdapter)
	if !ok {
		return adapters.PaymentHandoff{}, errors.New("payment handoff adapter is not configured")
	}
	for _, intent := range intents {
		if intent.Currency == "CNY" && intent.Status == payment.IntentPending && intent.PaymentRequirementID == current.CurrentPaymentRequirementID && intent.QuoteHash == current.CurrentQuoteHash && intent.MerchantDID == current.SelectedMerchantDID && intent.CapabilityID == current.SelectedCapabilityID && intent.RequesterDID == current.RequesterDID && intent.BudgetReservation <= current.Budget.ReservedAmount && s.clock().Before(intent.ExpiresAt) {
			return adapter.Handoff(ctx, adapters.PaymentQuery{Intent: *intent, PayeeDID: intent.PayeeDID})
		}
	}
	return adapters.PaymentHandoff{}, errors.New("no approved pending CNY intent is available")
}
