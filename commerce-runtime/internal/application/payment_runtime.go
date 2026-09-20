package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/adapters"
	"github.com/stablepay/commerce-runtime/internal/decision"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/ledger"
	"github.com/stablepay/commerce-runtime/internal/payment"
	"github.com/stablepay/commerce-runtime/internal/reconciliation"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

type PaymentDependencies struct {
	DID         adapters.DIDAdapter
	Payment     adapters.PaymentAdapter
	Status      adapters.PaymentStatusAdapter
	Chain       adapters.ChainStatusAdapter
	Entitlement adapters.EntitlementAdapter
}

func WithPaymentAdapters(dependencies PaymentDependencies) Option {
	return func(s *Service) {
		s.paymentDeps = dependencies
	}
}

// TrustedPaymentQuote is supplied by a deterministic quote/policy boundary.
// It is deliberately not a DecisionProposal and has no LLM-controlled fields.
type TrustedPaymentQuote struct {
	MerchantDID           string
	CapabilityID          string
	PayeeDID              string
	QuoteHash             string
	AmountMinor           int64
	Currency              string
	RequesterDID          string
	ExpiresAt             time.Time
	ProtocolVersion       string
	Scheme                string
	Network               string
	Asset                 string
	ResourceURL           string
	ProductID             string
	SkillDID              string
	PaymentRequirementRef string
}

func (q TrustedPaymentQuote) Validate(now time.Time, deadline time.Time) error {
	if strings.TrimSpace(q.MerchantDID) == "" || strings.TrimSpace(q.CapabilityID) == "" || strings.TrimSpace(q.PayeeDID) == "" || strings.TrimSpace(q.QuoteHash) == "" || strings.TrimSpace(q.RequesterDID) == "" || q.AmountMinor <= 0 || strings.TrimSpace(q.Currency) == "" || q.ExpiresAt.IsZero() {
		return payment.ErrInvalidIntent
	}
	if !q.ExpiresAt.After(now) || !q.ExpiresAt.Before(deadline) {
		return payment.ErrIntentExpired
	}
	return nil
}

type ReservePaymentIntentRequest struct {
	EpisodeID      string
	Quote          TrustedPaymentQuote
	IdempotencyKey string
	ProposalID     string
	Actor          string
	TraceID        string
}

type PaymentIntentResult struct {
	Intent   *payment.PaymentIntent
	Episode  *episode.CommerceEpisode
	Event    *episode.EpisodeEvent
	Replayed bool
}

type PaymentExecutionResult struct {
	Intent   *payment.PaymentIntent
	Episode  *episode.CommerceEpisode
	Event    *episode.EpisodeEvent
	Outcome  payment.PaymentOutcome
	Replayed bool
}

func (s *Service) financeStore() (repository.S2Store, error) {
	if s == nil || s.store == nil {
		return nil, repository.ErrRepositoryUnavailable
	}
	store, ok := s.store.(repository.S2Store)
	if !ok {
		return nil, repository.ErrRepositoryUnavailable
	}
	return store, nil
}

// ReservePaymentIntent atomically records BUDGET_RESERVED, the EpisodeEvent,
// the Paying projection and the runtime-owned PaymentIntent. The external
// payment plane is not called by this method.
func (s *Service) ReservePaymentIntent(ctx context.Context, request ReservePaymentIntentRequest) (PaymentIntentResult, error) {
	store, err := s.financeStore()
	if err != nil {
		return PaymentIntentResult{}, err
	}
	if strings.TrimSpace(request.IdempotencyKey) == "" {
		return PaymentIntentResult{}, payment.ErrInvalidIntent
	}
	if strings.TrimSpace(request.EpisodeID) == "" {
		return PaymentIntentResult{}, payment.ErrInvalidIntent
	}
	current, err := s.store.Get(ctx, request.EpisodeID)
	if err != nil {
		return PaymentIntentResult{}, err
	}
	return s.reservePaymentIntentForEpisode(ctx, store, current, request)
}

type ReservePaymentIntentForEpisodeRequest struct {
	EpisodeID      string
	Quote          TrustedPaymentQuote
	IdempotencyKey string
	ProposalID     string
	Actor          string
	TraceID        string
}

func (s *Service) ReservePaymentIntentForEpisode(ctx context.Context, request ReservePaymentIntentForEpisodeRequest) (PaymentIntentResult, error) {
	store, err := s.financeStore()
	if err != nil {
		return PaymentIntentResult{}, err
	}
	current, err := s.store.Get(ctx, request.EpisodeID)
	if err != nil {
		return PaymentIntentResult{}, err
	}
	return s.reservePaymentIntentForEpisode(ctx, store, current, ReservePaymentIntentRequest{
		EpisodeID: request.EpisodeID, Quote: request.Quote, IdempotencyKey: request.IdempotencyKey, ProposalID: request.ProposalID,
		Actor: request.Actor, TraceID: request.TraceID,
	})
}

func (s *Service) reservePaymentIntentForEpisode(ctx context.Context, store repository.S2Store, current *episode.CommerceEpisode, request ReservePaymentIntentRequest) (PaymentIntentResult, error) {
	if strings.TrimSpace(request.IdempotencyKey) == "" || strings.TrimSpace(request.EpisodeID) == "" {
		return PaymentIntentResult{}, payment.ErrInvalidIntent
	}
	// Resolve retries before state/deadline checks. Once the local transaction
	// has created an intent, replaying the same request must be safe even if the
	// caller observes the episode in PAYING or the wall clock has advanced.
	if existing, err := store.FindPaymentIntentByIdempotencyKey(ctx, current.EpisodeID, request.IdempotencyKey); err == nil {
		if !intentMatchesQuote(existing, current.EpisodeID, request.Quote) {
			return PaymentIntentResult{}, repository.ErrPaymentIntentConflict
		}
		return s.intentResult(ctx, existing, true)
	} else if !errors.Is(err, repository.ErrPaymentIntentNotFound) {
		return PaymentIntentResult{}, err
	}
	economicKey := payment.EconomicIdentityKey(current.EpisodeID, request.Quote.MerchantDID, request.Quote.CapabilityID, request.Quote.PayeeDID, request.Quote.QuoteHash, request.Quote.AmountMinor, request.Quote.Currency)
	if existing, err := store.FindPaymentIntentByEconomicKey(ctx, current.EpisodeID, economicKey); err == nil {
		if !intentMatchesQuote(existing, current.EpisodeID, request.Quote) {
			return PaymentIntentResult{}, repository.ErrPaymentIntentConflict
		}
		return s.intentResult(ctx, existing, true)
	} else if !errors.Is(err, repository.ErrPaymentIntentNotFound) {
		return PaymentIntentResult{}, err
	}
	now := s.clock().UTC()
	if current.State != episode.StateNegotiating {
		return PaymentIntentResult{}, decision.ErrActionNotAllowed
	}
	if err := request.Quote.Validate(now, current.DeadlineAt); err != nil {
		return PaymentIntentResult{}, err
	}
	if request.Quote.RequesterDID != current.RequesterDID {
		return PaymentIntentResult{}, decision.ErrPaymentBindingMismatch
	}
	if current.SelectedMerchantDID != "" && (current.SelectedMerchantDID != request.Quote.MerchantDID || current.SelectedCapabilityID != request.Quote.CapabilityID) {
		return PaymentIntentResult{}, decision.ErrPaymentBindingMismatch
	}
	if current.SelectedCandidateSetID != "" {
		discoveryStore, discoveryErr := s.discoveryStore()
		if discoveryErr != nil {
			return PaymentIntentResult{}, discoveryErr
		}
		candidateSet, getErr := discoveryStore.GetCandidateSet(ctx, current.SelectedCandidateSetID)
		if getErr != nil {
			return PaymentIntentResult{}, getErr
		}
		candidate, found := candidateSet.FindCandidate(current.SelectedMerchantDID, current.SelectedCapabilityID)
		if !found || candidate.CatalogVersion != current.SelectedCatalogVersion || candidate.CatalogSnapshotHash != current.SelectedCatalogSnapshotHash || candidate.CatalogSnapshotRef != current.SelectedCatalogSnapshotRef || !strings.EqualFold(candidate.PayeeDID, request.Quote.PayeeDID) {
			return PaymentIntentResult{}, decision.ErrPaymentBindingMismatch
		}
	}
	projection, err := s.currentProjection(ctx, store, current)
	if err != nil {
		return PaymentIntentResult{}, err
	}
	sequence, err := nextLedgerSequence(ctx, store, current.EpisodeID)
	if err != nil {
		return PaymentIntentResult{}, err
	}
	entry := &ledger.LedgerEntry{
		EntryID: s.idGenerator("led"), EpisodeID: current.EpisodeID, Sequence: sequence,
		Type: ledger.EntryBudgetReserved, Currency: strings.ToUpper(request.Quote.Currency), AmountMinor: request.Quote.AmountMinor,
		IdempotencyKey: request.IdempotencyKey + ":reserve", OccurredAt: now, TraceID: request.TraceID,
		ReferenceHash: ledger.HashReference(request.Quote.QuoteHash), MetadataHash: ledger.HashReference("budget reservation"),
	}
	intent := &payment.PaymentIntent{
		IntentID: s.idGenerator("pi"), EpisodeID: current.EpisodeID, MerchantDID: request.Quote.MerchantDID,
		CapabilityID: request.Quote.CapabilityID, PayeeDID: request.Quote.PayeeDID, QuoteHash: request.Quote.QuoteHash, AmountMinor: request.Quote.AmountMinor,
		Currency: strings.ToUpper(request.Quote.Currency), RequesterDID: request.Quote.RequesterDID,
		EpisodeVersion: current.Version + 1, BudgetReservation: request.Quote.AmountMinor, IdempotencyKey: request.IdempotencyKey,
		EconomicKey: economicKey, ExpiresAt: request.Quote.ExpiresAt, Status: payment.IntentCreated, CreatedAt: now, UpdatedAt: now,
		CredentialRef: "credential:" + request.IdempotencyKey,
	}
	intent.RequestFingerprint = payment.RequestFingerprint(*intent)
	entry.PaymentIntentID = intent.IntentID
	if err := projection.Apply(*entry); err != nil {
		if errors.Is(err, ledger.ErrInsufficientBudget) {
			return s.persistBudgetInsufficient(ctx, current, request.TraceID)
		}
		return PaymentIntentResult{}, err
	}
	next := current.Clone()
	next.SelectedMerchantDID = request.Quote.MerchantDID
	next.SelectedCapabilityID = request.Quote.CapabilityID
	next.CurrentQuoteHash = request.Quote.QuoteHash
	next.Budget = projection.ToEpisodeBudget()
	next.ActionCount++
	if err := next.ApplyCommittedState(episode.StatePaying, now, ""); err != nil {
		return PaymentIntentResult{}, err
	}
	intent.EpisodeVersion = next.Version
	proposalID := request.ProposalID
	if proposalID == "" {
		proposalID = "reserve-" + request.IdempotencyKey
	}
	event, err := s.newPaymentEvent(current, next, now, trace.ActionReserveBudget, request.IdempotencyKey, trace.Observation{Type: trace.ObservationQuoteValid, Code: request.Quote.QuoteHash}, proposalID, request.Actor, request.TraceID)
	if err != nil {
		return PaymentIntentResult{}, err
	}
	err = store.CommitFinanceTransition(ctx, repository.FinanceTransition{
		EpisodeID: current.EpisodeID, ExpectedEpisodeVersion: current.Version, NextEpisode: next, Event: event,
		LedgerEntries: []*ledger.LedgerEntry{entry}, IntentCreate: intent,
	})
	if err != nil {
		if existing, lookupErr := store.FindPaymentIntentByIdempotencyKey(ctx, current.EpisodeID, request.IdempotencyKey); lookupErr == nil {
			if intentMatchesQuote(existing, current.EpisodeID, request.Quote) {
				return s.intentResult(ctx, existing, true)
			}
			return PaymentIntentResult{}, repository.ErrPaymentIntentConflict
		}
		return PaymentIntentResult{}, err
	}
	return PaymentIntentResult{Intent: intent, Episode: next, Event: event}, nil
}

// ResolveSelectedPayeeDID reads the payee from the immutable selected
// CandidateSet fact. It is intentionally separate from catalog price hints
// and from DecisionProposal input.
func (s *Service) ResolveSelectedPayeeDID(ctx context.Context, episodeID string) (string, error) {
	current, err := s.store.Get(ctx, episodeID)
	if err != nil {
		return "", err
	}
	if current.SelectedCandidateSetID == "" {
		return "", decision.ErrPaymentBindingMismatch
	}
	store, err := s.discoveryStore()
	if err != nil {
		return "", err
	}
	set, err := store.GetCandidateSet(ctx, current.SelectedCandidateSetID)
	if err != nil {
		return "", err
	}
	candidate, ok := set.FindCandidate(current.SelectedMerchantDID, current.SelectedCapabilityID)
	if !ok || candidate.CatalogVersion != current.SelectedCatalogVersion || candidate.CatalogSnapshotHash != current.SelectedCatalogSnapshotHash {
		return "", decision.ErrPaymentBindingMismatch
	}
	return candidate.PayeeDID, nil
}

func (s *Service) intentResult(ctx context.Context, intent *payment.PaymentIntent, replayed bool) (PaymentIntentResult, error) {
	current, err := s.store.Get(ctx, intent.EpisodeID)
	if err != nil {
		return PaymentIntentResult{}, err
	}
	return PaymentIntentResult{Intent: intent, Episode: current, Replayed: replayed}, nil
}

func (s *Service) currentProjection(ctx context.Context, store repository.S2Store, current *episode.CommerceEpisode) (ledger.BudgetProjection, error) {
	entries, err := store.ListLedgerEntries(ctx, current.EpisodeID)
	if err != nil {
		return ledger.BudgetProjection{}, err
	}
	projection, err := ledger.BuildProjection(current.Budget.Currency, current.Budget.BudgetLimitMinor, current.Budget.RefundReusable, entries)
	if err != nil {
		return ledger.BudgetProjection{}, err
	}
	if projection.ToEpisodeBudget() != current.Budget {
		return ledger.BudgetProjection{}, ledger.ErrProjectionInvariant
	}
	return projection, nil
}

func nextLedgerSequence(ctx context.Context, store repository.S2Store, episodeID string) (uint64, error) {
	entries, err := store.ListLedgerEntries(ctx, episodeID)
	if err != nil {
		return 0, err
	}
	return uint64(len(entries)) + 1, nil
}

func intentMatchesQuote(intent *payment.PaymentIntent, episodeID string, quote TrustedPaymentQuote) bool {
	return intent != nil && intent.EpisodeID == episodeID && intent.MerchantDID == quote.MerchantDID && intent.CapabilityID == quote.CapabilityID && intent.PayeeDID == quote.PayeeDID && intent.QuoteHash == quote.QuoteHash && intent.AmountMinor == quote.AmountMinor && strings.EqualFold(intent.Currency, quote.Currency) && intent.RequesterDID == quote.RequesterDID
}

func (s *Service) AuthorizeAndSubmitPayment(ctx context.Context, intentID, traceID string) (PaymentExecutionResult, error) {
	store, err := s.financeStore()
	if err != nil {
		return PaymentExecutionResult{}, err
	}
	intent, err := store.GetPaymentIntent(ctx, intentID)
	if err != nil {
		return PaymentExecutionResult{}, err
	}
	if intent.Status == payment.IntentPending || intent.Status == payment.IntentUnknown || intent.Status == payment.IntentSubmitting {
		return s.ReconcilePayment(ctx, intentID, traceID)
	}
	if intent.Status == payment.IntentConfirmed || intent.Status == payment.IntentFailed || intent.Status == payment.IntentExpired {
		return s.persistedPaymentResult(ctx, intent, false)
	}
	if s.paymentDeps.Payment == nil {
		return PaymentExecutionResult{}, errors.New("payment adapter is not configured")
	}
	current, err := s.store.Get(ctx, intent.EpisodeID)
	if err != nil {
		return PaymentExecutionResult{}, err
	}
	authorization := adapters.AuthorizationResult{Allowed: true, RequesterDID: intent.RequesterDID, MerchantDID: intent.MerchantDID, CapabilityID: intent.CapabilityID, PayeeDID: intent.PayeeDID, QuoteHash: intent.QuoteHash, AmountMinor: intent.AmountMinor, Currency: intent.Currency, AuthorizationRef: intent.AuthorizationRef}
	if intent.Status == payment.IntentCreated {
		if s.paymentDeps.DID == nil {
			return PaymentExecutionResult{}, errors.New("DID adapter is not configured")
		}
		authorization, err = s.paymentDeps.DID.AuthorizePayment(ctx, adapters.AuthorizationRequest{Intent: *intent, PayeeDID: intent.PayeeDID, Now: s.clock().UTC()})
		if err != nil {
			return s.closePaymentIntent(ctx, intent, current, payment.IntentFailed, "PAYMENT_AUTHORIZATION_ERROR", traceID, err)
		}
		if err := authorization.Validate(); err != nil {
			return s.closePaymentIntent(ctx, intent, current, payment.IntentFailed, "PAYMENT_AUTHORIZATION_INVALID", traceID, err)
		}
		if !authorization.Allowed {
			return s.closePaymentIntent(ctx, intent, current, payment.IntentFailed, "PAYMENT_AUTHORIZATION_DENIED", traceID, decision.ErrPaymentAuthorization)
		}
		if err := s.guard.CheckPayment(current, intent, authorization, s.clock().UTC()); err != nil {
			return s.closePaymentIntent(ctx, intent, current, payment.IntentFailed, paymentGuardFailureCode(err), traceID, err)
		}
		intentNext := *intent
		intentNext.Status = payment.IntentAuthorized
		intentNext.AuthorizationRef = authorization.AuthorizationRef
		intentNext.RequestFingerprint = payment.RequestFingerprint(intentNext)
		intentNext.UpdatedAt = s.clock().UTC()
		currentNext := current.Clone()
		currentNext.ActionCount++
		if err := currentNext.ApplyCommittedState(current.State, intentNext.UpdatedAt, ""); err != nil {
			return PaymentExecutionResult{}, err
		}
		intentNext.EpisodeVersion = currentNext.Version
		event, err := s.newPaymentEvent(current, currentNext, intentNext.UpdatedAt, trace.ActionPaymentAuthorizationChecked, intent.IdempotencyKey+":authorization", trace.Observation{Type: trace.ObservationAuthorizationAllowed}, intent.IntentID+":authorization", "runtime", traceID)
		if err != nil {
			return PaymentExecutionResult{}, err
		}
		if err := store.CommitFinanceTransition(ctx, repository.FinanceTransition{EpisodeID: current.EpisodeID, ExpectedEpisodeVersion: current.Version, NextEpisode: currentNext, Event: event, IntentUpdate: &intentNext, ExpectedIntentStatus: payment.IntentCreated}); err != nil {
			if refreshed, refreshErr := store.GetPaymentIntent(ctx, intentID); refreshErr == nil && refreshed.Status != payment.IntentCreated {
				intent = refreshed
			} else {
				return PaymentExecutionResult{}, err
			}
		} else {
			intent = &intentNext
		}
	}
	current, err = s.store.Get(ctx, intent.EpisodeID)
	if err != nil {
		return PaymentExecutionResult{}, err
	}
	if intent.Status != payment.IntentAuthorized {
		return s.persistedPaymentResult(ctx, intent, true)
	}
	authorization = adapters.AuthorizationResult{Allowed: true, RequesterDID: intent.RequesterDID, MerchantDID: intent.MerchantDID, CapabilityID: intent.CapabilityID, PayeeDID: intent.PayeeDID, QuoteHash: intent.QuoteHash, AmountMinor: intent.AmountMinor, Currency: intent.Currency, AuthorizationRef: intent.AuthorizationRef}
	now := s.clock().UTC()
	if err := s.guard.CheckPayment(current, intent, authorization, now); err != nil {
		return s.closePaymentIntent(ctx, intent, current, payment.IntentFailed, paymentGuardFailureCode(err), traceID, err)
	}
	intentNext := *intent
	intentNext.Status = payment.IntentSubmitting
	intentNext.UpdatedAt = now
	next := current.Clone()
	next.ActionCount++
	next.PaymentAttemptCount++
	if err := next.ApplyCommittedState(current.State, now, ""); err != nil {
		return PaymentExecutionResult{}, err
	}
	intentNext.EpisodeVersion = next.Version
	event, err := s.newPaymentEvent(current, next, now, trace.ActionPaymentSubmitted, intent.IdempotencyKey+":submit", trace.Observation{Type: trace.ObservationPaymentSubmitted}, intent.IntentID+":submit", "runtime", traceID)
	if err != nil {
		return PaymentExecutionResult{}, err
	}
	if err := store.CommitFinanceTransition(ctx, repository.FinanceTransition{EpisodeID: current.EpisodeID, ExpectedEpisodeVersion: current.Version, NextEpisode: next, Event: event, IntentUpdate: &intentNext, ExpectedIntentStatus: payment.IntentAuthorized}); err != nil {
		if refreshed, refreshErr := store.GetPaymentIntent(ctx, intentID); refreshErr == nil {
			return s.persistedPaymentResult(ctx, refreshed, true)
		}
		return PaymentExecutionResult{}, err
	}
	outcome, adapterErr := s.paymentDeps.Payment.Submit(ctx, adapters.PaymentSubmitRequest{Intent: intentNext, Authorization: authorization, PayeeDID: intentNext.PayeeDID, RequestFingerprint: intentNext.RequestFingerprint, TraceID: traceID})
	if adapterErr != nil && outcome.Status == "" {
		outcome.Status = payment.OutcomeUnknown
	}
	return s.recordPaymentOutcome(ctx, intentID, payment.IntentSubmitting, outcome, traceID, adapterErr)
}

func (s *Service) ReconcilePayment(ctx context.Context, intentID, traceID string) (PaymentExecutionResult, error) {
	store, err := s.financeStore()
	if err != nil {
		return PaymentExecutionResult{}, err
	}
	intent, err := store.GetPaymentIntent(ctx, intentID)
	if err != nil {
		return PaymentExecutionResult{}, err
	}
	if intent.Status == payment.IntentConfirmed || intent.Status == payment.IntentFailed || intent.Status == payment.IntentExpired {
		return s.persistedPaymentResult(ctx, intent, false)
	}
	initial := outcomeFromIntent(intent)
	resolution, err := (reconciliation.Resolver{PaymentStatus: s.paymentDeps.Status, ChainStatus: s.paymentDeps.Chain, Entitlement: s.paymentDeps.Entitlement}).Resolve(ctx, *intent, initial)
	if err != nil {
		return PaymentExecutionResult{}, err
	}
	if resolution.Source != "unresolved" && outcomeStatusForIntent(resolution.Outcome.Status) == intent.Status && resolution.Outcome.TxID == intent.TxID && resolution.Outcome.TxHash == intent.TxHash {
		return s.persistedPaymentResult(ctx, intent, false)
	}
	if resolution.Source == "unresolved" && (intent.Status == payment.IntentSubmitting || intent.Status == payment.IntentUnknown) {
		return s.resubmitExactPayment(ctx, intent, traceID)
	}
	return s.recordPaymentOutcome(ctx, intentID, intent.Status, resolution.Outcome, traceID, nil)
}

// resubmitExactPayment is the crash-window recovery path. It never creates a
// second intent or increments the payment attempt count. The persisted intent,
// idempotency key, request fingerprint and opaque credential reference are the
// only source of the command sent to payment-service.
func (s *Service) resubmitExactPayment(ctx context.Context, intent *payment.PaymentIntent, traceID string) (PaymentExecutionResult, error) {
	if s.paymentDeps.Payment == nil {
		return PaymentExecutionResult{}, errors.New("payment adapter is not configured")
	}
	current, err := s.store.Get(ctx, intent.EpisodeID)
	if err != nil {
		return PaymentExecutionResult{}, err
	}
	authorization := adapters.AuthorizationResult{Allowed: true, RequesterDID: intent.RequesterDID, MerchantDID: intent.MerchantDID, CapabilityID: intent.CapabilityID, PayeeDID: intent.PayeeDID, QuoteHash: intent.QuoteHash, AmountMinor: intent.AmountMinor, Currency: intent.Currency, AuthorizationRef: intent.AuthorizationRef}
	if err := s.guard.CheckPayment(current, intent, authorization, s.clock().UTC()); err != nil {
		return s.closePaymentIntent(ctx, intent, current, payment.IntentFailed, paymentGuardFailureCode(err), traceID, err)
	}
	outcome, adapterErr := s.paymentDeps.Payment.Submit(ctx, adapters.PaymentSubmitRequest{Intent: *intent, Authorization: authorization, PayeeDID: intent.PayeeDID, RequestFingerprint: intent.RequestFingerprint, TraceID: traceID})
	if adapterErr != nil && outcome.Status == "" {
		outcome.Status = payment.OutcomeUnknown
	}
	return s.recordPaymentOutcome(ctx, intent.IntentID, intent.Status, outcome, traceID, adapterErr)
}

func (s *Service) recordPaymentOutcome(ctx context.Context, intentID string, expectedStatus payment.IntentStatus, outcome payment.PaymentOutcome, traceID string, adapterErr error) (PaymentExecutionResult, error) {
	store, err := s.financeStore()
	if err != nil {
		return PaymentExecutionResult{}, err
	}
	intent, err := store.GetPaymentIntent(ctx, intentID)
	if err != nil {
		return PaymentExecutionResult{}, err
	}
	if !paymentOutcomeIdentityMatches(*intent, outcome) {
		outcome = payment.PaymentOutcome{Status: payment.OutcomeUnknown, Reason: "adapter response identity did not match intent"}
	}
	outcome = fillPaymentOutcome(*intent, outcome)
	if outcome.Validate() != nil || (outcome.Status == payment.OutcomeConfirmed && (outcome.AmountMinor != intent.AmountMinor || !strings.EqualFold(outcome.Currency, intent.Currency))) {
		outcome = fillPaymentOutcome(*intent, payment.PaymentOutcome{Status: payment.OutcomeUnknown, Reason: "adapter response did not match intent"})
	}
	if adapterErr != nil && outcome.Status == payment.OutcomeUnknown && outcome.Reason == "" {
		outcome.Reason = adapterErr.Error()
	}
	if expectedStatus != "" && intent.Status != expectedStatus {
		return s.persistedPaymentResult(ctx, intent, true)
	}
	current, err := s.store.Get(ctx, intent.EpisodeID)
	if err != nil {
		return PaymentExecutionResult{}, err
	}
	if current.State != episode.StatePaying {
		return PaymentExecutionResult{}, decision.ErrActionNotAllowed
	}
	if current.CurrentQuoteHash != intent.QuoteHash || current.SelectedMerchantDID != intent.MerchantDID || current.SelectedCapabilityID != intent.CapabilityID || current.RequesterDID != intent.RequesterDID || current.Budget.ReservedAmount < intent.BudgetReservation {
		return PaymentExecutionResult{}, decision.ErrPaymentBindingMismatch
	}
	if outcomeStatusForIntent(outcome.Status) == intent.Status && outcome.TxID == intent.TxID && outcome.TxHash == intent.TxHash {
		return s.persistedPaymentResult(ctx, intent, false)
	}
	now := s.clock().UTC()
	intentNext := *intent
	intentNext.Status = outcomeStatusForIntent(outcome.Status)
	intentNext.TxID = outcome.TxID
	intentNext.TxHash = outcome.TxHash
	intentNext.FailureCode = outcome.Reason
	intentNext.UpdatedAt = now
	next := current.Clone()
	next.ActionCount++
	entries := make([]*ledger.LedgerEntry, 0, 2)
	stateAfter := episode.StatePaying
	action := trace.ActionPaymentPending
	observation := trace.Observation{Type: trace.ObservationPaymentPending, Code: outcome.Reason}
	projection, err := s.currentProjection(ctx, store, current)
	if err != nil {
		return PaymentExecutionResult{}, err
	}
	switch outcome.Status {
	case payment.OutcomeConfirmed:
		stateAfter = episode.StateClaiming
		action = trace.ActionPaymentConfirmed
		observation = trace.Observation{Type: trace.ObservationPaymentConfirmed, Code: outcome.TxID}
	case payment.OutcomeFailed:
		stateAfter = episode.StateFailed
		action = trace.ActionPaymentFailed
		observation = trace.Observation{Type: trace.ObservationPaymentFailed, Code: outcome.Reason}
	case payment.OutcomeUnknown:
		action = trace.ActionPaymentUnknown
		observation = trace.Observation{Type: trace.ObservationPaymentUnknown, Code: outcome.Reason}
	}
	if outcome.Status == payment.OutcomeConfirmed || outcome.Status == payment.OutcomeFailed {
		sequence, err := nextLedgerSequence(ctx, store, current.EpisodeID)
		if err != nil {
			return PaymentExecutionResult{}, err
		}
		release := &ledger.LedgerEntry{EntryID: s.idGenerator("led"), EpisodeID: current.EpisodeID, Sequence: sequence, Type: ledger.EntryBudgetReleased, Currency: intent.Currency, AmountMinor: intent.BudgetReservation, PaymentIntentID: intent.IntentID, TxID: outcome.TxID, IdempotencyKey: intent.IdempotencyKey + ":release", OccurredAt: now, TraceID: traceID, ReferenceHash: intent.EconomicKey, MetadataHash: ledger.HashReference("reservation released")}
		if err := projection.Apply(*release); err != nil {
			return PaymentExecutionResult{}, err
		}
		entries = append(entries, release)
		if outcome.Status == payment.OutcomeConfirmed {
			settled := &ledger.LedgerEntry{EntryID: s.idGenerator("led"), EpisodeID: current.EpisodeID, Sequence: sequence + 1, Type: ledger.EntryPaymentSettled, Currency: intent.Currency, AmountMinor: intent.AmountMinor, PaymentIntentID: intent.IntentID, TxID: outcome.TxID, IdempotencyKey: intent.IdempotencyKey + ":settled", OccurredAt: now, TraceID: traceID, ReferenceHash: intent.EconomicKey, MetadataHash: ledger.HashReference("payment settlement confirmation")}
			if err := projection.Apply(*settled); err != nil {
				return PaymentExecutionResult{}, err
			}
			entries = append(entries, settled)
		}
		next.Budget = projection.ToEpisodeBudget()
	}
	terminalReason := ""
	if outcome.Status == payment.OutcomeFailed {
		terminalReason = strings.TrimSpace(outcome.Reason)
		if terminalReason == "" {
			terminalReason = "PAYMENT_FAILED"
		}
		intentNext.FailureCode = terminalReason
	}
	if err := next.ApplyCommittedState(stateAfter, now, terminalReason); err != nil {
		return PaymentExecutionResult{}, err
	}
	intentNext.EpisodeVersion = next.Version
	eventKey := intent.IdempotencyKey + ":outcome:" + string(outcome.Status) + ":" + outcome.TxID
	event, err := s.newPaymentEvent(current, next, now, action, eventKey, observation, intent.IntentID+":"+string(outcome.Status), "runtime", traceID)
	if err != nil {
		return PaymentExecutionResult{}, err
	}
	if err := store.CommitFinanceTransition(ctx, repository.FinanceTransition{EpisodeID: current.EpisodeID, ExpectedEpisodeVersion: current.Version, NextEpisode: next, Event: event, LedgerEntries: entries, IntentUpdate: &intentNext, ExpectedIntentStatus: expectedStatus}); err != nil {
		if refreshed, refreshErr := store.GetPaymentIntent(ctx, intentID); refreshErr == nil && (expectedStatus == "" || refreshed.Status != expectedStatus) {
			return s.persistedPaymentResult(ctx, refreshed, true)
		}
		return PaymentExecutionResult{}, err
	}
	return PaymentExecutionResult{Intent: &intentNext, Episode: next, Event: event, Outcome: outcome}, nil
}

func (s *Service) VerifyPaymentEntitlement(ctx context.Context, intentID, traceID string) (PaymentExecutionResult, error) {
	store, err := s.financeStore()
	if err != nil {
		return PaymentExecutionResult{}, err
	}
	if s.paymentDeps.Entitlement == nil {
		return PaymentExecutionResult{}, errors.New("entitlement adapter is not configured")
	}
	intent, err := store.GetPaymentIntent(ctx, intentID)
	if err != nil {
		return PaymentExecutionResult{}, err
	}
	if intent.Status != payment.IntentConfirmed {
		return PaymentExecutionResult{}, payment.ErrIntentStateConflict
	}
	current, err := s.store.Get(ctx, intent.EpisodeID)
	if err != nil {
		return PaymentExecutionResult{}, err
	}
	if current.State != episode.StateClaiming {
		return PaymentExecutionResult{}, decision.ErrActionNotAllowed
	}
	result, err := s.paymentDeps.Entitlement.Verify(ctx, adapters.EntitlementQuery{EpisodeID: intent.EpisodeID, IntentID: intent.IntentID, TxID: intent.TxID, MerchantDID: intent.MerchantDID, CapabilityID: intent.CapabilityID, PayeeDID: intent.PayeeDID, RequesterDID: intent.RequesterDID})
	if err != nil {
		return PaymentExecutionResult{}, err
	}
	now := s.clock().UTC()
	stateAfter := episode.StateClaiming
	observationType := trace.ObservationEntitlementUnknown
	if result.Status == adapters.EntitlementValid {
		if !result.MatchesIntent(*intent) {
			result.Status = adapters.EntitlementInvalid
			result.Reason = "ENTITLEMENT_EVIDENCE_MISMATCH"
		} else {
			stateAfter = episode.StateInvokingDelivery
			observationType = trace.ObservationEntitlementValid
		}
	} else if result.Status == adapters.EntitlementInvalid {
		observationType = trace.ObservationEntitlementInvalid
	}
	terminalReason := ""
	if result.Status == adapters.EntitlementInvalid {
		stateAfter = episode.StateFailed
		terminalReason = "ENTITLEMENT_INVALID"
	}
	next := current.Clone()
	next.ActionCount++
	if result.Status == adapters.EntitlementValid {
		entitlementRef := strings.TrimSpace(result.EvidenceRef)
		if entitlementRef == "" {
			entitlementRef = strings.TrimSpace(result.Reference)
		}
		if entitlementRef != "" {
			next.EntitlementRefs = appendUnique(next.EntitlementRefs, entitlementRef)
		}
	}
	if err := next.ApplyCommittedState(stateAfter, now, terminalReason); err != nil {
		return PaymentExecutionResult{}, err
	}
	eventKey := intent.IdempotencyKey + ":entitlement:" + strings.ToLower(string(result.Status))
	event, err := s.newPaymentEvent(current, next, now, trace.ActionVerifyEntitlement, eventKey, trace.Observation{Type: observationType, Code: result.Reference}, intent.IntentID+":entitlement", "runtime", traceID)
	if err != nil {
		return PaymentExecutionResult{}, err
	}
	if err := store.CommitFinanceTransition(ctx, repository.FinanceTransition{EpisodeID: current.EpisodeID, ExpectedEpisodeVersion: current.Version, NextEpisode: next, Event: event}); err != nil {
		if existing, findErr := s.store.FindByIdempotencyKey(ctx, current.EpisodeID, eventKey); findErr == nil {
			return PaymentExecutionResult{Intent: intent, Episode: current, Event: existing, Replayed: true}, nil
		}
		return PaymentExecutionResult{}, err
	}
	return PaymentExecutionResult{Intent: intent, Episode: next, Event: event, Outcome: payment.PaymentOutcome{Status: payment.OutcomeConfirmed, IntentID: intent.IntentID, TxID: intent.TxID, TxHash: intent.TxHash, AmountMinor: intent.AmountMinor, Currency: intent.Currency}}, nil
}

func paymentGuardFailureCode(err error) string {
	switch {
	case errors.Is(err, decision.ErrPaymentIntentExpired), errors.Is(err, payment.ErrIntentExpired):
		return "PAYMENT_INTENT_EXPIRED"
	case errors.Is(err, decision.ErrPaymentAuthorization):
		return "PAYMENT_AUTHORIZATION_DENIED"
	case errors.Is(err, decision.ErrPaymentAttemptLimit):
		return "PAYMENT_ATTEMPT_LIMIT"
	case errors.Is(err, decision.ErrStalePaymentIntent):
		return "PAYMENT_INTENT_STALE"
	case errors.Is(err, decision.ErrPaymentQuoteMismatch):
		return "PAYMENT_QUOTE_MISMATCH"
	case errors.Is(err, decision.ErrPaymentBindingMismatch):
		return "PAYMENT_BINDING_MISMATCH"
	case errors.Is(err, decision.ErrPaymentBudgetReservation):
		return "PAYMENT_BUDGET_RESERVATION"
	default:
		return "PAYMENT_PRE_SUBMIT_REJECTED"
	}
}

// closePaymentIntent is the deterministic local compensation path for a
// rejection after reservation but before a payment can be accepted by the
// external plane. It releases the reservation, closes the intent and moves
// the episode to a terminal state in one finance transaction.
func (s *Service) closePaymentIntent(ctx context.Context, intent *payment.PaymentIntent, current *episode.CommerceEpisode, status payment.IntentStatus, reason, traceID string, cause error) (PaymentExecutionResult, error) {
	store, err := s.financeStore()
	if err != nil {
		return PaymentExecutionResult{}, err
	}
	if intent == nil || current == nil {
		return PaymentExecutionResult{}, cause
	}
	if intent.IsTerminal() {
		result, persistedErr := s.persistedPaymentResult(ctx, intent, true)
		if persistedErr != nil {
			return PaymentExecutionResult{}, persistedErr
		}
		return result, cause
	}
	if current.State != episode.StatePaying || current.Budget.ReservedAmount < intent.BudgetReservation {
		return PaymentExecutionResult{}, cause
	}
	now := s.clock().UTC()
	if reason == "PAYMENT_INTENT_EXPIRED" {
		status = payment.IntentExpired
	}
	projection, err := s.currentProjection(ctx, store, current)
	if err != nil {
		return PaymentExecutionResult{}, err
	}
	sequence, err := nextLedgerSequence(ctx, store, current.EpisodeID)
	if err != nil {
		return PaymentExecutionResult{}, err
	}
	release := &ledger.LedgerEntry{EntryID: s.idGenerator("led"), EpisodeID: current.EpisodeID, Sequence: sequence, Type: ledger.EntryBudgetReleased, Currency: intent.Currency, AmountMinor: intent.BudgetReservation, PaymentIntentID: intent.IntentID, TxID: intent.TxID, IdempotencyKey: intent.IdempotencyKey + ":release:" + reason, OccurredAt: now, TraceID: traceID, ReferenceHash: intent.EconomicKey, MetadataHash: ledger.HashReference(reason)}
	if err := projection.Apply(*release); err != nil {
		return PaymentExecutionResult{}, err
	}
	intentNext := *intent
	intentNext.Status = status
	intentNext.FailureCode = reason
	intentNext.UpdatedAt = now
	next := current.Clone()
	next.Budget = projection.ToEpisodeBudget()
	next.ActionCount++
	stateAfter := episode.StateFailed
	if reason == "PAYMENT_INTENT_EXPIRED" {
		stateAfter = episode.StateExpired
	}
	if err := next.ApplyCommittedState(stateAfter, now, reason); err != nil {
		return PaymentExecutionResult{}, err
	}
	intentNext.EpisodeVersion = next.Version
	eventKey := intent.IdempotencyKey + ":closed:" + reason
	event, err := s.newPaymentEvent(current, next, now, trace.ActionPaymentFailed, eventKey, trace.Observation{Type: trace.ObservationPaymentFailed, Code: reason}, intent.IntentID+":closed", "runtime", traceID)
	if err != nil {
		return PaymentExecutionResult{}, err
	}
	commitErr := store.CommitFinanceTransition(ctx, repository.FinanceTransition{EpisodeID: current.EpisodeID, ExpectedEpisodeVersion: current.Version, NextEpisode: next, Event: event, LedgerEntries: []*ledger.LedgerEntry{release}, IntentUpdate: &intentNext, ExpectedIntentStatus: intent.Status})
	if commitErr != nil {
		if refreshed, refreshErr := store.GetPaymentIntent(ctx, intent.IntentID); refreshErr == nil && refreshed.IsTerminal() {
			result, resultErr := s.persistedPaymentResult(ctx, refreshed, true)
			if resultErr == nil {
				return result, cause
			}
		}
		return PaymentExecutionResult{}, commitErr
	}
	result := PaymentExecutionResult{Intent: &intentNext, Episode: next, Event: event, Outcome: payment.PaymentOutcome{Status: payment.OutcomeFailed, IntentID: intent.IntentID, TxID: intent.TxID, TxHash: intent.TxHash, AmountMinor: intent.AmountMinor, Currency: intent.Currency, Reason: reason}}
	return result, cause
}

func (s *Service) newPaymentEvent(current, next *episode.CommerceEpisode, now time.Time, action trace.ActionType, key string, observation trace.Observation, proposalID, actor, traceID string) (*episode.EpisodeEvent, error) {
	if actor == "" {
		actor = "runtime"
	}
	return episode.NewEvent(s.idGenerator("evt"), current.EpisodeID, current.Version, now, current.State,
		trace.Action{Type: action, IdempotencyKey: key}, observation,
		trace.Decision{ProposedAction: action, ProposalID: proposalID, Reason: "deterministic payment runtime"},
		trace.RuntimeVerdict{Allowed: true}, next.State, actor, traceID, s.runtimeVersion)
}

func outcomeStatusForIntent(status payment.OutcomeStatus) payment.IntentStatus {
	switch status {
	case payment.OutcomeConfirmed:
		return payment.IntentConfirmed
	case payment.OutcomeFailed:
		return payment.IntentFailed
	case payment.OutcomePending:
		return payment.IntentPending
	default:
		return payment.IntentUnknown
	}
}

func fillPaymentOutcome(intent payment.PaymentIntent, outcome payment.PaymentOutcome) payment.PaymentOutcome {
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

func paymentOutcomeIdentityMatches(intent payment.PaymentIntent, outcome payment.PaymentOutcome) bool {
	if outcome.IntentID != "" && outcome.IntentID != intent.IntentID {
		return false
	}
	if intent.TxID != "" && outcome.TxID != "" && outcome.TxID != intent.TxID {
		return false
	}
	if intent.TxHash != "" && outcome.TxHash != "" && outcome.TxHash != intent.TxHash {
		return false
	}
	return true
}

func outcomeFromIntent(intent *payment.PaymentIntent) payment.PaymentOutcome {
	status := payment.OutcomeUnknown
	if intent.Status == payment.IntentPending {
		status = payment.OutcomePending
	}
	return fillPaymentOutcome(*intent, payment.PaymentOutcome{Status: status, Reason: intent.FailureCode})
}

func (s *Service) persistedPaymentResult(ctx context.Context, intent *payment.PaymentIntent, replayed bool) (PaymentExecutionResult, error) {
	episodeValue, err := s.store.Get(ctx, intent.EpisodeID)
	if err != nil {
		return PaymentExecutionResult{}, err
	}
	status := payment.OutcomeUnknown
	switch intent.Status {
	case payment.IntentPending:
		status = payment.OutcomePending
	case payment.IntentConfirmed:
		status = payment.OutcomeConfirmed
	case payment.IntentFailed:
		status = payment.OutcomeFailed
	}
	return PaymentExecutionResult{Intent: intent, Episode: episodeValue, Replayed: replayed, Outcome: fillPaymentOutcome(*intent, payment.PaymentOutcome{Status: status, Reason: intent.FailureCode})}, nil
}
