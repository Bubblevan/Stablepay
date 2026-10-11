package application

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/stablepay/commerce-runtime/internal/adapters"
	"github.com/stablepay/commerce-runtime/internal/buyer"
	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/invocation"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

var (
	ErrBuyerConfirmationRequired       = errors.New("buyer confirmation is required before payment")
	ErrBuyerConfirmationPending        = errors.New("buyer confirmation is not pending")
	ErrBuyerConfirmationInvalid        = errors.New("buyer confirmation does not match the current quote")
	ErrBuyerQuoteValidatorUnavailable  = errors.New("live buyer quote validation is not configured")
	ErrBuyerQuoteValidationUnavailable = errors.New("live buyer quote could not be verified")
	ErrBuyerQuoteStale                 = errors.New("buyer quote or availability changed; reconfirmation is required")
	ErrBuyerQuoteRefreshUnavailable    = errors.New("merchant could not provide a refreshed buyer quote")
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
	OfferRef      string    `json:"offer_ref"`
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
	OfferRef       string
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
	return s.buyerOfferForRequirement(ctx, current, current.CurrentPaymentRequirementID)
}

// GetBuyerOfferForRequirement reconstructs the immutable offer named by a
// persisted requirement. Refresh idempotency replays use this to return the
// original response even if a later refresh has since become current.
func (s *Service) GetBuyerOfferForRequirement(ctx context.Context, episodeID, requirementID string) (BuyerOffer, error) {
	current, err := s.store.Get(ctx, strings.TrimSpace(episodeID))
	if err != nil {
		return BuyerOffer{}, err
	}
	var request contract.AcquireCapabilityRequest
	if err := json.Unmarshal(current.ContractSnapshot, &request); err != nil {
		return BuyerOffer{}, fmt.Errorf("decode purchase contract: %w", err)
	}
	if !request.Constraints.RequireBuyerConfirmation {
		return BuyerOffer{}, ErrBuyerConfirmationPending
	}
	return s.buyerOfferForRequirement(ctx, current, requirementID)
}

func (s *Service) buyerOfferForRequirement(ctx context.Context, current *episode.CommerceEpisode, requirementID string) (BuyerOffer, error) {
	store, err := s.s4Store()
	if err != nil {
		return BuyerOffer{}, err
	}
	requirement, err := store.GetPaymentRequirementFact(ctx, strings.TrimSpace(requirementID))
	if err != nil {
		return BuyerOffer{}, err
	}
	if requirement.EpisodeID != current.EpisodeID || strings.TrimSpace(requirement.ProductID) == "" ||
		(requirement.PaymentRequirementID == current.CurrentPaymentRequirementID &&
			(requirement.CanonicalQuoteHash != current.CurrentQuoteHash || requirement.MerchantDID != current.SelectedMerchantDID || requirement.CapabilityID != current.SelectedCapabilityID)) {
		return BuyerOffer{}, ErrBuyerConfirmationInvalid
	}
	offer := BuyerOffer{EpisodeID: current.EpisodeID, RequestID: current.RequestID, RequesterDID: current.RequesterDID,
		MerchantDID: requirement.MerchantDID, CapabilityID: requirement.CapabilityID, ProductID: requirement.ProductID,
		OfferRef: requirement.PaymentRequirementID, QuoteHash: requirement.CanonicalQuoteHash, AmountMinor: requirement.BusinessAmountMinor, Currency: strings.ToUpper(requirement.Currency),
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

type RefreshBuyerOfferRequest struct {
	EpisodeID      string
	IdempotencyKey string
	TraceID        string
}

// RefreshBuyerOffer invokes the already-selected merchant again through the
// S4 adapter, persists a new x402 requirement and leaves the episode awaiting
// a fresh signature. It never reserves budget or submits payment.
func (s *Service) RefreshBuyerOffer(ctx context.Context, request RefreshBuyerOfferRequest) (PaymentRequirementResult, error) {
	request.EpisodeID = strings.TrimSpace(request.EpisodeID)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if request.EpisodeID == "" || request.IdempotencyKey == "" {
		return PaymentRequirementResult{}, ErrBuyerConfirmationInvalid
	}
	current, err := s.store.Get(ctx, request.EpisodeID)
	if err != nil {
		return PaymentRequirementResult{}, err
	}
	store, err := s.s4Store()
	if err != nil {
		return PaymentRequirementResult{}, err
	}
	keyDigest := sha256.Sum256([]byte(request.EpisodeID + "\x00" + request.IdempotencyKey))
	durableKey := "buyer-quote-refresh:" + hex.EncodeToString(keyDigest[:])
	if existing, findErr := store.FindMerchantInvocationByIdempotencyKey(ctx, current.EpisodeID, durableKey); findErr == nil {
		if existing.Phase != invocation.PhaseQuoteRefresh {
			return PaymentRequirementResult{}, repository.ErrIdempotencyConflict
		}
		if requirement, requirementErr := store.FindPaymentRequirementByInvocation(ctx, current.EpisodeID, existing.InvocationID); requirementErr == nil {
			quote := trustedQuoteFromRequirement(*requirement)
			quote.RequesterDID = current.RequesterDID
			event, eventErr := s.store.FindByIdempotencyKey(ctx, current.EpisodeID, "refresh-buyer-offer:"+existing.InvocationID)
			if eventErr != nil {
				return PaymentRequirementResult{}, eventErr
			}
			return PaymentRequirementResult{Episode: current, Invocation: existing, Requirement: requirement, Quote: quote, Event: event, Replayed: true}, nil
		} else if !errors.Is(requirementErr, repository.ErrFactNotFound) {
			return PaymentRequirementResult{}, requirementErr
		}
	} else if !errors.Is(findErr, repository.ErrFactNotFound) {
		return PaymentRequirementResult{}, findErr
	}
	if current.State != episode.StateAwaitingBuyer {
		return PaymentRequirementResult{}, ErrBuyerConfirmationPending
	}
	var purchase contract.AcquireCapabilityRequest
	if err := json.Unmarshal(current.ContractSnapshot, &purchase); err != nil {
		return PaymentRequirementResult{}, err
	}
	if !purchase.Constraints.RequireBuyerConfirmation {
		return PaymentRequirementResult{}, ErrBuyerConfirmationRequired
	}
	capability, input, err := s.selectedCapabilityAndInput(ctx, current)
	if err != nil {
		return PaymentRequirementResult{}, err
	}
	payload, contentType, payloadHash, err := s.resolveInputPayload(ctx, input)
	if err != nil {
		return PaymentRequirementResult{}, err
	}
	traceID := strings.TrimSpace(request.TraceID)
	if traceID == "" {
		traceID = current.EpisodeID + ":quote-refresh"
	}
	operation := adapters.MerchantInvokeRequest{EpisodeID: current.EpisodeID, RequesterDID: current.RequesterDID,
		MerchantDID: current.SelectedMerchantDID, CapabilityID: current.SelectedCapabilityID,
		CatalogVersion: current.SelectedCatalogVersion, CatalogSnapshotHash: current.SelectedCatalogSnapshotHash,
		CatalogSnapshotRef: current.SelectedCatalogSnapshotRef, InputRef: inputRef(input), InputHash: input.SHA256,
		InputPayload: payload, InputContentType: contentType, InputPayloadHash: payloadHash,
		Attempt: 1, Phase: invocation.PhaseQuoteRefresh, TraceID: traceID, IdempotencyKey: durableKey,
		Endpoint: capability.InvokeEndpoint}
	fact, response, adapterErr, replayed, err := s.executeMerchantInvocation(ctx, store, operation)
	if err != nil {
		return PaymentRequirementResult{}, err
	}
	if adapterErr != nil {
		return PaymentRequirementResult{}, fmt.Errorf("%w: %v", ErrBuyerQuoteRefreshUnavailable, adapterErr)
	}
	if transientMerchantResponse(response) {
		return PaymentRequirementResult{}, fmt.Errorf("%w: merchant returned HTTP %d", ErrBuyerQuoteRefreshUnavailable, response.HTTPStatus)
	}
	if response.HTTPStatus != 402 {
		return PaymentRequirementResult{}, fmt.Errorf("%w: expected a fresh x402 challenge, received HTTP %d", ErrBuyerQuoteStale, response.HTTPStatus)
	}
	result, err := s.ParsePaymentRequirement(ctx, ParsePaymentRequirementRequest{EpisodeID: current.EpisodeID, InvocationID: fact.InvocationID, TraceID: traceID})
	if err != nil {
		return PaymentRequirementResult{}, err
	}
	result.Replayed = result.Replayed || replayed
	return result, nil
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
	now := s.clock().UTC()
	next := current.Clone()
	next.ActionCount++
	if err := next.ApplyCommittedState(episode.StateAwaitingBuyer, now, ""); err != nil {
		return CommitResult{}, err
	}
	key := fmt.Sprintf("buyer-confirmation-required:%s:%s:%d", current.EpisodeID, offer.OfferRef, current.Version)
	if existing, findErr := s.store.FindByIdempotencyKey(ctx, current.EpisodeID, key); findErr == nil {
		latest, getErr := s.store.Get(ctx, current.EpisodeID)
		if getErr != nil {
			return CommitResult{}, getErr
		}
		return CommitResult{Episode: latest, Event: existing, Replayed: true}, nil
	}
	event, err := episode.NewEvent(s.idGenerator("evt"), current.EpisodeID, current.Version, now,
		current.State, trace.Action{Type: trace.ActionRequestBuyerConfirmation, IdempotencyKey: key, InputRef: "buyer-offer://" + offer.OfferRef, InputHash: offer.QuoteHash},
		trace.Observation{Type: trace.ObservationBuyerConfirmationRequired, Code: offer.QuoteHash, FactsRef: "buyer-offer://" + offer.OfferRef, PayloadHash: offer.QuoteHash},
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
		if existing.OfferRef != request.OfferRef || existing.QuoteHash != request.QuoteHash || existing.AmountMinor != request.AmountMinor || !strings.EqualFold(existing.Currency, request.Currency) || existing.Accepted != request.Accepted {
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
	if request.OfferRef != offer.OfferRef || request.QuoteHash != offer.QuoteHash || request.AmountMinor != offer.AmountMinor || !strings.EqualFold(request.Currency, offer.Currency) ||
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
	if err := s.validateBuyerOffer(ctx, offer); err != nil {
		return BuyerConfirmationResult{}, err
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
	// episode_events.idempotency_key is globally unique and varchar(128), while
	// the public buyer key is Episode-scoped and can itself be 128 characters.
	// Hash both parts into a bounded durable key so cross-Episode reuse remains
	// distinct without exceeding the event column width.
	keyDigest := sha256.Sum256([]byte(current.EpisodeID + "\x00" + request.IdempotencyKey))
	key := "buyer-confirmation:" + hex.EncodeToString(keyDigest[:])
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
	requirement, err := store.GetPaymentRequirementFact(ctx, current.CurrentPaymentRequirementID)
	if err != nil {
		return BuyerOffer{}, err
	}
	if requirement.CanonicalQuoteHash != current.CurrentQuoteHash || strings.TrimSpace(requirement.ProductID) == "" {
		return BuyerOffer{}, ErrBuyerConfirmationInvalid
	}
	return BuyerOffer{EpisodeID: current.EpisodeID, RequestID: current.RequestID, RequesterDID: current.RequesterDID,
		MerchantDID: current.SelectedMerchantDID, CapabilityID: current.SelectedCapabilityID, ProductID: requirement.ProductID,
		OfferRef: requirement.PaymentRequirementID, QuoteHash: requirement.CanonicalQuoteHash, AmountMinor: requirement.BusinessAmountMinor, Currency: strings.ToUpper(requirement.Currency),
		PayeeDID: requirement.PayeeDID, Network: requirement.Network, Asset: requirement.Asset, ResourceURL: requirement.ResourceURL,
		ExpiresAt: requirement.ExpiresAt.UTC()}, nil
}

// ValidateCurrentBuyerOffer rechecks price and service availability after the
// buyer signs and immediately before the Runtime may reserve payment budget.
func (s *Service) ValidateCurrentBuyerOffer(ctx context.Context, episodeID string) error {
	current, err := s.store.Get(ctx, strings.TrimSpace(episodeID))
	if err != nil {
		return err
	}
	offer, err := s.currentBuyerOffer(ctx, current)
	if err != nil {
		return err
	}
	return s.validateBuyerOffer(ctx, offer)
}

func (s *Service) validateBuyerOffer(ctx context.Context, offer BuyerOffer) error {
	if !s.clock().UTC().Before(offer.ExpiresAt) {
		return ErrBuyerQuoteStale
	}
	if s.buyerQuoteValidator == nil {
		return ErrBuyerQuoteValidatorUnavailable
	}
	if err := s.buyerQuoteValidator.ValidateBuyerOffer(ctx, offer); err != nil {
		if errors.Is(err, ErrBuyerQuoteValidationUnavailable) {
			return fmt.Errorf("%w: %v", ErrBuyerQuoteValidatorUnavailable, err)
		}
		return fmt.Errorf("%w: %v", ErrBuyerQuoteStale, err)
	}
	return nil
}

func (s *Service) validateBuyerOfferForQuote(ctx context.Context, current *episode.CommerceEpisode, quote TrustedPaymentQuote) error {
	if current == nil || quote.OfferRef == "" || quote.OfferRef != current.CurrentPaymentRequirementID {
		return ErrBuyerConfirmationInvalid
	}
	offer, err := s.currentBuyerOffer(ctx, current)
	if err != nil {
		return err
	}
	if offer.OfferRef != quote.OfferRef || offer.QuoteHash != quote.QuoteHash || offer.AmountMinor != quote.AmountMinor ||
		!strings.EqualFold(offer.Currency, quote.Currency) || offer.ProductID != quote.ProductID || offer.PayeeDID != quote.PayeeDID {
		return ErrBuyerConfirmationInvalid
	}
	return s.validateBuyerOffer(ctx, offer)
}

func confirmationForOffer(offer BuyerOffer, accepted bool) *buyer.Confirmation {
	return &buyer.Confirmation{EpisodeID: offer.EpisodeID, RequestID: offer.RequestID, RequesterDID: offer.RequesterDID,
		MerchantDID: offer.MerchantDID, CapabilityID: offer.CapabilityID, ProductID: offer.ProductID,
		OfferRef: offer.OfferRef, QuoteHash: offer.QuoteHash, AmountMinor: offer.AmountMinor, Currency: offer.Currency, Accepted: accepted,
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
