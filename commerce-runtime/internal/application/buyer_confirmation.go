package application

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/stablepay/commerce-runtime/internal/buyer"
	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

var (
	ErrBuyerConfirmationRequired      = errors.New("buyer confirmation is required before payment")
	ErrBuyerConfirmationPending       = errors.New("buyer confirmation is not pending")
	ErrBuyerConfirmationInvalid       = errors.New("buyer confirmation does not match the current quote")
	ErrBuyerQuoteValidatorUnavailable = errors.New("live buyer quote validation is not configured")
	ErrBuyerQuoteStale                = errors.New("buyer quote or availability changed; reconfirmation is required")
)

// BuyerQuoteValidator must compare the displayed offer with a live merchant
// quote/availability read. It is a read-only boundary and must not authorize
// or submit payment.
type BuyerQuoteValidator interface {
	ValidateBuyerOffer(context.Context, BuyerOffer) error
}

type BuyerOffer struct {
	EpisodeID     string    `json:"episode_id"`
	RequestID     string    `json:"request_id"`
	RequesterDID  string    `json:"requester_did"`
	MerchantDID   string    `json:"merchant_did"`
	CapabilityID  string    `json:"capability_id"`
	ProductID     string    `json:"product_id"`
	QuoteHash     string    `json:"quote_hash"`
	AmountMinor   int64     `json:"amount_minor"`
	Currency      string    `json:"currency"`
	PayeeDID      string    `json:"payee_did"`
	Network       string    `json:"network"`
	Asset         string    `json:"asset"`
	ResourceURL   string    `json:"resource_url"`
	ExpiresAt     time.Time `json:"expires_at"`
	AcceptMessage string    `json:"accept_message_base64"`
	RejectMessage string    `json:"reject_message_base64"`
}

type BuyerConfirmationRequest struct {
	EpisodeID      string
	QuoteHash      string
	AmountMinor    int64
	Currency       string
	Accepted       bool
	Signature      string
	IdempotencyKey string
	TraceID        string
}

type BuyerConfirmationResult struct {
	Episode      *episode.CommerceEpisode
	Confirmation *buyer.Confirmation
	Event        *episode.EpisodeEvent
	Replayed     bool
}

func WithBuyerQuoteValidator(validator BuyerQuoteValidator) Option {
	return func(s *Service) { s.buyerQuoteValidator = validator }
}

func (s *Service) GetBuyerOffer(ctx context.Context, episodeID string) (BuyerOffer, error) {
	current, err := s.store.Get(ctx, strings.TrimSpace(episodeID))
	if err != nil {
		return BuyerOffer{}, err
	}
	if current.State != episode.StateAwaitingBuyer {
		return BuyerOffer{}, ErrBuyerConfirmationPending
	}
	var request contract.AcquireCapabilityRequest
	if err := json.Unmarshal(current.ContractSnapshot, &request); err != nil {
		return BuyerOffer{}, fmt.Errorf("decode purchase contract: %w", err)
	}
	if !request.Constraints.RequireBuyerConfirmation {
		return BuyerOffer{}, ErrBuyerConfirmationPending
	}
	store, err := s.s4Store()
	if err != nil {
		return BuyerOffer{}, err
	}
	key := "invoke:initial:" + current.EpisodeID + ":" + current.SelectedMerchantDID
	invocationFact, err := store.FindMerchantInvocationByIdempotencyKey(ctx, current.EpisodeID, key)
	if err != nil {
		return BuyerOffer{}, err
	}
	requirement, err := store.FindPaymentRequirementByInvocation(ctx, current.EpisodeID, invocationFact.InvocationID)
	if err != nil {
		return BuyerOffer{}, err
	}
	if requirement.CanonicalQuoteHash != current.CurrentQuoteHash || strings.TrimSpace(requirement.ProductID) == "" {
		return BuyerOffer{}, ErrBuyerConfirmationInvalid
	}
	offer := BuyerOffer{EpisodeID: current.EpisodeID, RequestID: current.RequestID, RequesterDID: current.RequesterDID,
		MerchantDID: current.SelectedMerchantDID, CapabilityID: current.SelectedCapabilityID, ProductID: requirement.ProductID,
		QuoteHash: requirement.CanonicalQuoteHash, AmountMinor: requirement.BusinessAmountMinor, Currency: strings.ToUpper(requirement.Currency),
		PayeeDID: requirement.PayeeDID, Network: requirement.Network, Asset: requirement.Asset, ResourceURL: requirement.ResourceURL,
		ExpiresAt: requirement.ExpiresAt.UTC()}
	accept, err := buyer.ApprovalMessage(*confirmationForOffer(offer, true))
	if err != nil {
		return BuyerOffer{}, err
	}
	reject, err := buyer.ApprovalMessage(*confirmationForOffer(offer, false))
	if err != nil {
		return BuyerOffer{}, err
	}
	offer.AcceptMessage = base64.StdEncoding.EncodeToString(accept)
	offer.RejectMessage = base64.StdEncoding.EncodeToString(reject)
	return offer, nil
}

func (s *Service) RequestBuyerConfirmation(ctx context.Context, episodeID, traceID string) (CommitResult, error) {
	current, err := s.store.Get(ctx, strings.TrimSpace(episodeID))
	if err != nil {
		return CommitResult{}, err
	}
	if current.State != episode.StateNegotiating {
		if current.State == episode.StateAwaitingBuyer {
			return CommitResult{Episode: current, Replayed: true}, nil
		}
		return CommitResult{}, ErrBuyerConfirmationPending
	}
	var request contract.AcquireCapabilityRequest
	if err := json.Unmarshal(current.ContractSnapshot, &request); err != nil {
		return CommitResult{}, err
	}
	if !request.Constraints.RequireBuyerConfirmation {
		return CommitResult{}, ErrBuyerConfirmationRequired
	}
	offer, err := s.currentBuyerOffer(ctx, current)
	if err != nil {
		return CommitResult{}, err
	}
	if !s.clock().UTC().Before(offer.ExpiresAt) {
		return CommitResult{}, episode.ErrEpisodeExpired
	}
	now := s.clock().UTC()
	next := current.Clone()
	next.ActionCount++
	if err := next.ApplyCommittedState(episode.StateAwaitingBuyer, now, ""); err != nil {
		return CommitResult{}, err
	}
	key := "buyer-confirmation-required:" + current.EpisodeID + ":" + offer.QuoteHash
	if existing, findErr := s.store.FindByIdempotencyKey(ctx, current.EpisodeID, key); findErr == nil {
		latest, getErr := s.store.Get(ctx, current.EpisodeID)
		if getErr != nil {
			return CommitResult{}, getErr
		}
		return CommitResult{Episode: latest, Event: existing, Replayed: true}, nil
	}
	event, err := episode.NewEvent(s.idGenerator("evt"), current.EpisodeID, current.Version, now,
		current.State, trace.Action{Type: trace.ActionRequestBuyerConfirmation, IdempotencyKey: key, InputRef: "payment-requirement://" + offer.QuoteHash, InputHash: offer.QuoteHash},
		trace.Observation{Type: trace.ObservationBuyerConfirmationRequired, Code: offer.QuoteHash, FactsRef: "buyer-offer://" + current.EpisodeID, PayloadHash: offer.QuoteHash},
		trace.Decision{ProposedAction: trace.ActionRequestBuyerConfirmation, ProposalID: key},
		trace.RuntimeVerdict{Allowed: true, Checks: []trace.RuntimeCheck{trace.Check("buyer_confirmation_gate", true, "payment is paused until the exact quote is approved")}},
		next.State, "runtime", traceID, s.runtimeVersion)
	if err != nil {
		return CommitResult{}, err
	}
	if err := s.store.CommitTransition(ctx, current.EpisodeID, current.Version, next, event); err != nil {
		if errors.Is(err, repository.ErrVersionConflict) || errors.Is(err, repository.ErrIdempotentReplay) {
			if existing, findErr := s.store.FindByIdempotencyKey(ctx, current.EpisodeID, key); findErr == nil {
				latest, _ := s.store.Get(ctx, current.EpisodeID)
				return CommitResult{Episode: latest, Event: existing, Replayed: true}, nil
			}
		}
		return CommitResult{}, err
	}
	return CommitResult{Episode: next, Event: event}, nil
}

func (s *Service) RecordBuyerConfirmation(ctx context.Context, request BuyerConfirmationRequest) (BuyerConfirmationResult, error) {
	request.EpisodeID = strings.TrimSpace(request.EpisodeID)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if request.EpisodeID == "" || request.IdempotencyKey == "" {
		return BuyerConfirmationResult{}, ErrBuyerConfirmationInvalid
	}
	store, ok := s.store.(repository.BuyerConfirmationStore)
	if !ok {
		return BuyerConfirmationResult{}, repository.ErrRepositoryUnavailable
	}
	current, err := s.store.Get(ctx, request.EpisodeID)
	if err != nil {
		return BuyerConfirmationResult{}, err
	}
	if existing, findErr := store.FindBuyerConfirmationByIdempotencyKey(ctx, current.EpisodeID, request.IdempotencyKey); findErr == nil {
		if existing.QuoteHash != request.QuoteHash || existing.AmountMinor != request.AmountMinor || !strings.EqualFold(existing.Currency, request.Currency) || existing.Accepted != request.Accepted {
			return BuyerConfirmationResult{}, repository.ErrBuyerConfirmationConflict
		}
		return BuyerConfirmationResult{Episode: current, Confirmation: existing, Replayed: true}, nil
	} else if !errors.Is(findErr, repository.ErrNotFound) {
		return BuyerConfirmationResult{}, findErr
	}
	if current.State != episode.StateAwaitingBuyer {
		return BuyerConfirmationResult{}, ErrBuyerConfirmationPending
	}
	offer, err := s.GetBuyerOffer(ctx, current.EpisodeID)
	if err != nil {
		return BuyerConfirmationResult{}, err
	}
	if request.QuoteHash != offer.QuoteHash || request.AmountMinor != offer.AmountMinor || !strings.EqualFold(request.Currency, offer.Currency) ||
		!s.clock().UTC().Before(offer.ExpiresAt) {
		return BuyerConfirmationResult{}, ErrBuyerConfirmationInvalid
	}
	confirmation := confirmationForOffer(offer, request.Accepted)
	confirmation.ConfirmationID = s.idGenerator("buyer-confirmation")
	confirmation.IdempotencyKey = request.IdempotencyKey
	confirmation.Signature = strings.TrimSpace(request.Signature)
	confirmation.CreatedAt = s.clock().UTC()
	if confirmation.CreatedAt.After(confirmation.ExpiresAt) || !confirmation.ExpiresAt.After(confirmation.CreatedAt) {
		return BuyerConfirmationResult{}, ErrBuyerConfirmationInvalid
	}
	message, err := buyer.ApprovalMessage(*confirmation)
	if err != nil {
		return BuyerConfirmationResult{}, err
	}
	confirmation.PayloadHash = buyer.HashPayload(message)
	if err := verifyBuyerSignature(confirmation); err != nil {
		return BuyerConfirmationResult{}, err
	}
	if s.buyerQuoteValidator == nil {
		return BuyerConfirmationResult{}, ErrBuyerQuoteValidatorUnavailable
	}
	if err := s.buyerQuoteValidator.ValidateBuyerOffer(ctx, offer); err != nil {
		return BuyerConfirmationResult{}, fmt.Errorf("%w: %v", ErrBuyerQuoteStale, err)
	}
	next := current.Clone()
	next.ActionCount++
	to := episode.StateAborted
	observation := trace.ObservationBuyerRejected
	if confirmation.Accepted {
		to = episode.StateNegotiating
		observation = trace.ObservationBuyerApproved
	}
	if err := next.ApplyCommittedState(to, confirmation.CreatedAt, "buyer rejected purchase"); err != nil {
		return BuyerConfirmationResult{}, err
	}
	key := "buyer-confirmation:" + request.IdempotencyKey
	event, err := episode.NewEvent(s.idGenerator("evt"), current.EpisodeID, current.Version, confirmation.CreatedAt,
		current.State, trace.Action{Type: trace.ActionRecordBuyerConfirmation, IdempotencyKey: key, InputRef: "buyer-confirmation://" + confirmation.ConfirmationID, InputHash: confirmation.PayloadHash},
		trace.Observation{Type: observation, Code: confirmation.QuoteHash, FactsRef: "buyer-confirmation://" + confirmation.ConfirmationID, PayloadHash: confirmation.PayloadHash},
		trace.Decision{ProposedAction: trace.ActionRecordBuyerConfirmation, ProposalID: key},
		trace.RuntimeVerdict{Allowed: true, Checks: []trace.RuntimeCheck{trace.Check("buyer_signature", true, "buyer signed the exact current offer")}},
		next.State, confirmation.RequesterDID, request.TraceID, s.runtimeVersion)
	if err != nil {
		return BuyerConfirmationResult{}, err
	}
	if err := store.CommitBuyerConfirmationTransition(ctx, repository.BuyerConfirmationTransition{EpisodeID: current.EpisodeID, ExpectedEpisodeVersion: current.Version, NextEpisode: next, Event: event, Confirmation: confirmation}); err != nil {
		if errors.Is(err, repository.ErrIdempotentReplay) || errors.Is(err, repository.ErrVersionConflict) {
			if existing, findErr := store.FindBuyerConfirmationByIdempotencyKey(ctx, current.EpisodeID, request.IdempotencyKey); findErr == nil {
				latest, _ := s.store.Get(ctx, current.EpisodeID)
				return BuyerConfirmationResult{Episode: latest, Confirmation: existing, Replayed: true}, nil
			}
		}
		return BuyerConfirmationResult{}, err
	}
	return BuyerConfirmationResult{Episode: next, Confirmation: confirmation, Event: event}, nil
}

func (s *Service) currentBuyerOffer(ctx context.Context, current *episode.CommerceEpisode) (BuyerOffer, error) {
	store, err := s.s4Store()
	if err != nil {
		return BuyerOffer{}, err
	}
	key := "invoke:initial:" + current.EpisodeID + ":" + current.SelectedMerchantDID
	call, err := store.FindMerchantInvocationByIdempotencyKey(ctx, current.EpisodeID, key)
	if err != nil {
		return BuyerOffer{}, err
	}
	requirement, err := store.FindPaymentRequirementByInvocation(ctx, current.EpisodeID, call.InvocationID)
	if err != nil {
		return BuyerOffer{}, err
	}
	if requirement.CanonicalQuoteHash != current.CurrentQuoteHash || strings.TrimSpace(requirement.ProductID) == "" {
		return BuyerOffer{}, ErrBuyerConfirmationInvalid
	}
	return BuyerOffer{EpisodeID: current.EpisodeID, RequestID: current.RequestID, RequesterDID: current.RequesterDID,
		MerchantDID: current.SelectedMerchantDID, CapabilityID: current.SelectedCapabilityID, ProductID: requirement.ProductID,
		QuoteHash: requirement.CanonicalQuoteHash, AmountMinor: requirement.BusinessAmountMinor, Currency: strings.ToUpper(requirement.Currency),
		PayeeDID: requirement.PayeeDID, Network: requirement.Network, Asset: requirement.Asset, ResourceURL: requirement.ResourceURL,
		ExpiresAt: requirement.ExpiresAt.UTC()}, nil
}

func confirmationForOffer(offer BuyerOffer, accepted bool) *buyer.Confirmation {
	return &buyer.Confirmation{EpisodeID: offer.EpisodeID, RequestID: offer.RequestID, RequesterDID: offer.RequesterDID,
		MerchantDID: offer.MerchantDID, CapabilityID: offer.CapabilityID, ProductID: offer.ProductID,
		QuoteHash: offer.QuoteHash, AmountMinor: offer.AmountMinor, Currency: offer.Currency, Accepted: accepted,
		ExpiresAt: offer.ExpiresAt.UTC()}
}

func verifyBuyerSignature(confirmation *buyer.Confirmation) error {
	if confirmation == nil || !strings.HasPrefix(strings.ToLower(confirmation.RequesterDID), "did:solana:") {
		return ErrBuyerConfirmationInvalid
	}
	publicKeyText := strings.TrimSpace(confirmation.RequesterDID[len("did:solana:"):])
	publicKey, err := solana.PublicKeyFromBase58(publicKeyText)
	if err != nil {
		return ErrBuyerConfirmationInvalid
	}
	signature, err := base64.StdEncoding.DecodeString(confirmation.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return ErrBuyerConfirmationInvalid
	}
	message, err := buyer.ApprovalMessage(*confirmation)
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(publicKey[:]), message, signature) {
		return ErrBuyerConfirmationInvalid
	}
	return nil
}
