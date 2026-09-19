// Package application coordinates the domain aggregate, proposal guard and
// atomic repository commit. Decision providers only create proposals; this
// package owns the commit authority.
package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/adapters"
	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/decision"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"github.com/stablepay/commerce-runtime/internal/trace"
	"github.com/stablepay/commerce-runtime/internal/validator"
)

const DefaultRuntimeVersion = "commerce-runtime-mvp.1"
const DefaultCandidateSetTTL = 5 * time.Minute

type Service struct {
	store             repository.TransitionStore
	clock             func() time.Time
	idGenerator       func(prefix string) string
	runtimeVersion    string
	candidateSetTTL   time.Duration
	guard             decision.RuntimeGuard
	paymentDeps       PaymentDependencies
	merchantAdapter   adapters.MerchantAdapter
	validatorRegistry *validator.Registry
}

type Option func(*Service)

func WithClock(clock func() time.Time) Option {
	return func(s *Service) {
		if clock != nil {
			s.clock = clock
		}
	}
}

func WithIDGenerator(generator func(prefix string) string) Option {
	return func(s *Service) {
		if generator != nil {
			s.idGenerator = generator
		}
	}
}

func WithRuntimeVersion(version string) Option {
	return func(s *Service) {
		if strings.TrimSpace(version) != "" {
			s.runtimeVersion = version
		}
	}
}

func WithCandidateSetTTL(ttl time.Duration) Option {
	return func(s *Service) {
		if ttl > 0 {
			s.candidateSetTTL = ttl
		}
	}
}

func WithMerchantAdapter(adapter adapters.MerchantAdapter) Option {
	return func(s *Service) { s.merchantAdapter = adapter }
}

func WithValidatorRegistry(registry *validator.Registry) Option {
	return func(s *Service) {
		if registry != nil {
			s.validatorRegistry = registry
		}
	}
}

func NewService(store repository.TransitionStore, options ...Option) *Service {
	service := &Service{
		store:             store,
		clock:             func() time.Time { return time.Now().UTC() },
		idGenerator:       randomID,
		runtimeVersion:    DefaultRuntimeVersion,
		candidateSetTTL:   DefaultCandidateSetTTL,
		guard:             decision.NewRuntimeGuard(),
		validatorRegistry: validator.NewBuiltinRegistry(),
	}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) discoveryStore() (repository.DiscoveryRepository, error) {
	if s == nil || s.store == nil {
		return nil, repository.ErrRepositoryUnavailable
	}
	store, ok := s.store.(repository.DiscoveryRepository)
	if !ok {
		return nil, repository.ErrRepositoryUnavailable
	}
	return store, nil
}

type CreateEpisodeResult struct {
	Episode  *episode.CommerceEpisode
	Replayed bool
}

func (s *Service) CreateEpisode(ctx context.Context, request contract.AcquireCapabilityRequest) (CreateEpisodeResult, error) {
	if s == nil || s.store == nil {
		return CreateEpisodeResult{}, repository.ErrRepositoryUnavailable
	}
	normalized, err := request.Normalize()
	if err != nil {
		return CreateEpisodeResult{}, err
	}
	hash, err := normalized.SnapshotHash()
	if err != nil {
		return CreateEpisodeResult{}, err
	}
	existing, findErr := s.store.FindByRequestID(ctx, normalized.RequestID)
	if findErr == nil {
		if existing.ContractSnapshotHash != hash {
			return CreateEpisodeResult{}, repository.ErrRequestIDConflict
		}
		return CreateEpisodeResult{Episode: existing, Replayed: true}, nil
	}
	if !errors.Is(findErr, repository.ErrNotFound) {
		return CreateEpisodeResult{}, findErr
	}
	now := s.clock().UTC()
	if err := normalized.ValidateAt(now); err != nil {
		return CreateEpisodeResult{}, err
	}
	created, err := episode.New(s.idGenerator("ce"), normalized, now)
	if err != nil {
		return CreateEpisodeResult{}, err
	}
	if err := s.store.Create(ctx, created); err != nil {
		if errors.Is(err, repository.ErrRequestIDConflict) {
			// Another creator may have won the unique request_id race. Read the
			// winner and apply the same snapshot comparison as the fast path.
			winner, readErr := s.store.FindByRequestID(ctx, normalized.RequestID)
			if readErr == nil && winner.ContractSnapshotHash == hash {
				return CreateEpisodeResult{Episode: winner, Replayed: true}, nil
			}
			if readErr == nil {
				return CreateEpisodeResult{}, repository.ErrRequestIDConflict
			}
		}
		return CreateEpisodeResult{}, err
	}
	return CreateEpisodeResult{Episode: created}, nil
}

type DiscoverCapabilitiesRequest struct {
	EpisodeID      string
	CandidateSetID string
	ExpiresAt      time.Time
}

type DiscoverCapabilitiesResult struct {
	CandidateSet  *catalog.CandidateSet
	Episode       *episode.CommerceEpisode
	Event         *episode.EpisodeEvent
	TerminalEvent *episode.EpisodeEvent
	NoEligible    bool
	Replayed      bool
}

// RegisterCapabilityVersion is the catalog write boundary. It stores a new
// immutable version and never turns a catalog hint into a payment quote.
func (s *Service) RegisterCapabilityVersion(ctx context.Context, value *catalog.MerchantCapability) error {
	store, err := s.discoveryStore()
	if err != nil {
		return err
	}
	return store.SaveCapabilityVersion(ctx, value)
}

// DiscoverCapabilities derives a structured query from the immutable Episode
// contract, persists the CandidateSet fact, and emits the discovery
// observation. An empty set follows the deterministic terminal path without
// consulting a DecisionProvider.
func (s *Service) DiscoverCapabilities(ctx context.Context, request DiscoverCapabilitiesRequest) (DiscoverCapabilitiesResult, error) {
	store, err := s.discoveryStore()
	if err != nil {
		return DiscoverCapabilitiesResult{}, err
	}
	if strings.TrimSpace(request.EpisodeID) == "" {
		return DiscoverCapabilitiesResult{}, repository.ErrNotFound
	}
	current, err := s.store.Get(ctx, request.EpisodeID)
	if err != nil {
		return DiscoverCapabilitiesResult{}, err
	}
	candidateSetID := strings.TrimSpace(request.CandidateSetID)
	if candidateSetID == "" {
		// One Episode has one logical S3 discovery operation in this stage.
		// Keeping this identity stable lets a caller recover a committed fact
		// after losing the response without re-reading the catalog.
		candidateSetID = "cs:" + current.EpisodeID
	}
	actionKey := "discover:" + candidateSetID
	if current.State != episode.StateAccepted {
		events, listErr := s.store.ListByEpisode(ctx, current.EpisodeID)
		if listErr != nil {
			return DiscoverCapabilitiesResult{}, listErr
		}
		var discoveryEvent, terminalEvent *episode.EpisodeEvent
		for _, event := range events {
			if event.Action.Type == trace.ActionDiscover && event.Action.IdempotencyKey == actionKey {
				discoveryEvent = event
			}
			if event.Action.Type == trace.ActionStop && event.Action.IdempotencyKey == actionKey+":stop" {
				terminalEvent = event
			}
		}
		if discoveryEvent == nil {
			return DiscoverCapabilitiesResult{}, decision.ErrActionNotAllowed
		}
		candidateSet, getErr := store.GetCandidateSet(ctx, candidateSetID)
		if getErr != nil {
			return DiscoverCapabilitiesResult{}, getErr
		}
		latest, getErr := s.store.Get(ctx, current.EpisodeID)
		if getErr != nil {
			return DiscoverCapabilitiesResult{}, getErr
		}
		return DiscoverCapabilitiesResult{CandidateSet: candidateSet, Episode: latest, Event: discoveryEvent,
			TerminalEvent: terminalEvent, NoEligible: len(candidateSet.Candidates) == 0, Replayed: true}, nil
	}
	var acquire contract.AcquireCapabilityRequest
	if err := json.Unmarshal(current.ContractSnapshot, &acquire); err != nil {
		return DiscoverCapabilitiesResult{}, fmt.Errorf("decode episode contract snapshot: %w", err)
	}
	query, err := catalog.FromAcquireCapabilityRequest(acquire)
	if err != nil {
		return DiscoverCapabilitiesResult{}, err
	}
	now := s.clock().UTC()
	expiresAt := request.ExpiresAt.UTC()
	if expiresAt.IsZero() {
		expiresAt = now.Add(s.candidateSetTTL)
	}
	if expiresAt.After(current.DeadlineAt) {
		expiresAt = current.DeadlineAt
	}
	capabilities, err := store.ListActiveCapabilities(ctx)
	if err != nil {
		return DiscoverCapabilitiesResult{}, err
	}
	candidateSet, err := catalog.BuildCandidateSet(candidateSetID, current.EpisodeID, current.RequestID, query, capabilities, now, expiresAt)
	if err != nil {
		return DiscoverCapabilitiesResult{}, err
	}
	if err := store.SaveCandidateSet(ctx, candidateSet); err != nil {
		return DiscoverCapabilitiesResult{}, err
	}
	observationType := trace.ObservationCandidatesFound
	if len(candidateSet.Candidates) == 0 {
		observationType = trace.ObservationNoEligibleCandidate
	}
	discoverProposal := decision.DecisionProposal{ProposalID: s.idGenerator("proposal"), EpisodeID: current.EpisodeID,
		BasedOnEventSequence: current.Version - 1, ProposedAction: trace.ActionDiscover, CandidateSetID: candidateSet.CandidateSetID,
		// The discovery observation creates the first persisted evidence refs;
		// they cannot be referenced by the same event before it is committed.
		Confidence: 1,
		CreatedAt:  now.Add(-time.Nanosecond), ExpiresAt: now.Add(time.Minute)}
	commit, err := s.CommitProposal(ctx, CommitRequest{Proposal: discoverProposal,
		Action:      trace.Action{Type: trace.ActionDiscover, IdempotencyKey: actionKey},
		Observation: trace.Observation{Type: observationType, FactsRef: candidateSet.FactsRef, PayloadHash: candidateSet.PayloadHash},
		Actor:       "runtime", TraceID: current.EpisodeID})
	if err != nil {
		return DiscoverCapabilitiesResult{}, err
	}
	result := DiscoverCapabilitiesResult{CandidateSet: candidateSet, Episode: commit.Episode, Event: commit.Event, NoEligible: len(candidateSet.Candidates) == 0, Replayed: commit.Replayed}
	if len(candidateSet.Candidates) == 0 {
		stopNow := s.clock().UTC()
		stopProposal := decision.DecisionProposal{ProposalID: s.idGenerator("proposal"), EpisodeID: commit.Episode.EpisodeID,
			BasedOnEventSequence: commit.Episode.Version - 1, ProposedAction: trace.ActionStop, CandidateSetID: candidateSet.CandidateSetID,
			EvidenceRefs: []string{candidateSet.FactsRef, candidateSet.PayloadHash}, Confidence: 1,
			CreatedAt: stopNow.Add(-time.Nanosecond), ExpiresAt: stopNow.Add(time.Minute)}
		terminal, stopErr := s.CommitProposal(ctx, CommitRequest{Proposal: stopProposal,
			Action:      trace.Action{Type: trace.ActionStop, IdempotencyKey: actionKey + ":stop"},
			Observation: trace.Observation{Type: trace.ObservationNoEligibleCandidate, Code: "NO_ELIGIBLE_MERCHANT", FactsRef: candidateSet.FactsRef, PayloadHash: candidateSet.PayloadHash},
			Actor:       "runtime", TraceID: current.EpisodeID})
		if stopErr != nil {
			return DiscoverCapabilitiesResult{}, stopErr
		}
		result.Episode = terminal.Episode
		result.TerminalEvent = terminal.Event
	}
	return result, nil
}

// Discover is a concise alias for callers that use the domain operation name.
func (s *Service) Discover(ctx context.Context, request DiscoverCapabilitiesRequest) (DiscoverCapabilitiesResult, error) {
	return s.DiscoverCapabilities(ctx, request)
}

type SelectMerchantRequest struct {
	Proposal    decision.DecisionProposal
	Action      trace.Action
	Observation trace.Observation
	Actor       string
	TraceID     string
}

// CommitMerchantSelection is a convenience boundary for runtime code. The
// generic CommitProposal path enforces the same catalog guard.
func (s *Service) CommitMerchantSelection(ctx context.Context, request SelectMerchantRequest) (CommitResult, error) {
	if request.Action.Type == "" {
		request.Action.Type = trace.ActionSelectMerchant
	}
	if request.Proposal.ProposedAction == "" {
		request.Proposal.ProposedAction = trace.ActionSelectMerchant
	}
	if request.Observation.Type == "" {
		request.Observation.Type = trace.ObservationCandidatesFound
	}
	return s.CommitProposal(ctx, CommitRequest{Proposal: request.Proposal, Action: request.Action, Observation: request.Observation, Actor: request.Actor, TraceID: request.TraceID})
}

type CommitRequest struct {
	Proposal    decision.DecisionProposal
	Action      trace.Action
	Observation trace.Observation
	Actor       string
	TraceID     string
}

type CommitResult struct {
	Episode  *episode.CommerceEpisode
	Event    *episode.EpisodeEvent
	Replayed bool
}

// CommitProposal is the only write path for proposal-driven state changes.
// It performs the runtime guard before asking the store to atomically update
// the projection and append the event.
func (s *Service) CommitProposal(ctx context.Context, request CommitRequest) (CommitResult, error) {
	if s == nil || s.store == nil {
		return CommitResult{}, repository.ErrRepositoryUnavailable
	}
	if strings.TrimSpace(request.Action.IdempotencyKey) == "" {
		return CommitResult{}, fmt.Errorf("%w: action idempotency key is required", decision.ErrInvalidProposal)
	}
	if request.Action.Type == "" {
		request.Action.Type = request.Proposal.ProposedAction
	}
	if request.Action.Type != request.Proposal.ProposedAction {
		return CommitResult{}, fmt.Errorf("%w: action type does not match proposal", decision.ErrInvalidProposal)
	}

	// Idempotency is checked before proposal expiry/sequence validation: a
	// retried request returns the already committed deterministic result.
	existingEvent, err := s.store.FindByIdempotencyKey(ctx, request.Proposal.EpisodeID, request.Action.IdempotencyKey)
	if err == nil {
		if !sameTransition(existingEvent, request) {
			return CommitResult{}, repository.ErrIdempotencyConflict
		}
		current, getErr := s.store.Get(ctx, request.Proposal.EpisodeID)
		if getErr != nil {
			return CommitResult{}, getErr
		}
		return CommitResult{Episode: current, Event: existingEvent, Replayed: true}, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return CommitResult{}, err
	}
	if runtimeOwnedProposalAction(request.Action.Type) {
		return CommitResult{}, decision.ErrActionNotAllowed
	}

	current, err := s.store.Get(ctx, request.Proposal.EpisodeID)
	if err != nil {
		return CommitResult{}, err
	}
	events, err := s.store.ListByEpisode(ctx, current.EpisodeID)
	if err != nil {
		return CommitResult{}, err
	}
	knownEvidence := evidenceReferences(events)
	now := s.clock().UTC()
	guardResult, err := s.guard.Evaluate(current, request.Proposal, knownEvidence, request.Observation, now)
	if err != nil {
		// The idempotency precheck and the projection read are intentionally
		// separate for throughput. If the winning transaction commits between
		// them, resolve the key once more before returning a stale/expired error.
		if replay, replayErr := s.replayIfCommitted(ctx, current.EpisodeID, request); replayErr == nil {
			return replay, nil
		} else if errors.Is(replayErr, repository.ErrIdempotencyConflict) {
			return CommitResult{}, replayErr
		}
		return CommitResult{}, err
	}
	var selectedCandidate *catalog.Candidate
	if request.Action.Type == trace.ActionSelectMerchant {
		discoveryStore, discoveryErr := s.discoveryStore()
		if discoveryErr != nil {
			return CommitResult{}, discoveryErr
		}
		candidateSet, getErr := discoveryStore.GetCandidateSet(ctx, request.Proposal.CandidateSetID)
		if getErr != nil {
			return CommitResult{}, getErr
		}
		guardResult, err = s.guard.EvaluateMerchantSelection(current, request.Proposal, knownEvidence, request.Observation, now, candidateSet)
		if err != nil {
			return CommitResult{}, err
		}
		selectedCandidate = guardResult.SelectedCandidate
	}

	next := current.Clone()
	if selectedCandidate != nil {
		next.SelectedMerchantDID = selectedCandidate.MerchantDID
		next.SelectedCapabilityID = selectedCandidate.CapabilityID
		next.SelectedCandidateSetID = request.Proposal.CandidateSetID
		next.SelectedCatalogVersion = selectedCandidate.CatalogVersion
		next.SelectedCatalogSnapshotHash = selectedCandidate.CatalogSnapshotHash
		next.SelectedCatalogSnapshotRef = selectedCandidate.CatalogSnapshotRef
	}
	next.ActionCount++
	switch request.Action.Type {
	case trace.ActionCreatePayment:
		next.PaymentAttemptCount++
	case trace.ActionRetrySameMerchant:
		next.RetryCount++
	case trace.ActionInvoke:
		if current.State == episode.StateInvokingDelivery {
			next.DeliveryAttemptCount++
		}
	}
	reason := request.Observation.Code
	if reason == "" && episode.IsTerminal(guardResult.NextState) {
		reason = string(request.Observation.Type)
	}
	if err := next.ApplyCommittedState(guardResult.NextState, now, reason); err != nil {
		return CommitResult{}, err
	}
	eventTarget := proposalTarget(request.Proposal.Target)
	if selectedCandidate != nil {
		eventTarget = &trace.Target{MerchantDID: selectedCandidate.MerchantDID, CapabilityID: selectedCandidate.CapabilityID,
			CatalogVersion: selectedCandidate.CatalogVersion, CatalogSnapshotHash: selectedCandidate.CatalogSnapshotHash, CatalogSnapshotRef: selectedCandidate.CatalogSnapshotRef}
	}
	event, err := episode.NewEvent(
		s.idGenerator("evt"), current.EpisodeID, current.Version,
		now, current.State, request.Action, request.Observation,
		trace.Decision{ProposedAction: request.Proposal.ProposedAction, ProposalID: request.Proposal.ProposalID, Reason: request.Proposal.Rationale,
			CandidateSetID: request.Proposal.CandidateSetID, Target: eventTarget, EvidenceRefs: append([]string(nil), request.Proposal.EvidenceRefs...)},
		guardResult.Verdict, next.State, request.Actor, request.TraceID, s.runtimeVersion,
	)
	if err != nil {
		return CommitResult{}, err
	}
	if err := s.store.CommitTransition(ctx, current.EpisodeID, current.Version, next, event); err != nil {
		if errors.Is(err, repository.ErrIdempotentReplay) {
			if replay, replayErr := s.replayIfCommitted(ctx, current.EpisodeID, request); replayErr == nil {
				return replay, nil
			}
			return CommitResult{}, repository.ErrIdempotencyConflict
		}
		// With MySQL, a concurrent transaction using the same key can observe
		// the optimistic version conflict after the first transaction commits.
		// Resolve that race to the same deterministic replay result.
		if errors.Is(err, repository.ErrVersionConflict) {
			if replay, replayErr := s.replayIfCommitted(ctx, current.EpisodeID, request); replayErr == nil {
				return replay, nil
			} else if errors.Is(replayErr, repository.ErrIdempotencyConflict) {
				return CommitResult{}, replayErr
			}
		}
		return CommitResult{}, err
	}
	return CommitResult{Episode: next, Event: event}, nil
}

// Payment, budget and entitlement facts are committed by the deterministic
// runtime methods, never by a DecisionProvider proposal. This keeps proposal
// handling useful for orchestration while preventing a model from asserting a
// reservation, settlement or entitlement as if it were a trusted fact.
func runtimeOwnedProposalAction(action trace.ActionType) bool {
	switch action {
	case trace.ActionReserveBudget, trace.ActionNegotiateAndPay, trace.ActionCreatePayment,
		trace.ActionVerifyEntitlement, trace.ActionPaymentAuthorizationChecked,
		trace.ActionPaymentSubmitted, trace.ActionPaymentPending, trace.ActionPaymentStatusQueried,
		trace.ActionPaymentConfirmed, trace.ActionPaymentFailed, trace.ActionPaymentUnknown:
		return true
	default:
		return false
	}
}

func (s *Service) replayIfCommitted(ctx context.Context, episodeID string, request CommitRequest) (CommitResult, error) {
	committed, err := s.store.FindByIdempotencyKey(ctx, episodeID, request.Action.IdempotencyKey)
	if err != nil {
		return CommitResult{}, err
	}
	if !sameTransition(committed, request) {
		return CommitResult{}, repository.ErrIdempotencyConflict
	}
	latest, err := s.store.Get(ctx, episodeID)
	if err != nil {
		return CommitResult{}, err
	}
	return CommitResult{Episode: latest, Event: committed, Replayed: true}, nil
}

func (s *Service) GetEpisode(ctx context.Context, episodeID string) (*episode.CommerceEpisode, error) {
	return s.store.Get(ctx, episodeID)
}

func (s *Service) ListEvents(ctx context.Context, episodeID string) ([]*episode.EpisodeEvent, error) {
	return s.store.ListByEpisode(ctx, episodeID)
}

func evidenceReferences(events []*episode.EpisodeEvent) map[string]struct{} {
	refs := make(map[string]struct{})
	for _, event := range events {
		if event.Observation.FactsRef != "" {
			refs[event.Observation.FactsRef] = struct{}{}
		}
		if event.Observation.PayloadHash != "" {
			refs[event.Observation.PayloadHash] = struct{}{}
		}
	}
	return refs
}

func sameTransition(event *episode.EpisodeEvent, request CommitRequest) bool {
	if event == nil {
		return false
	}
	return event.EpisodeID == request.Proposal.EpisodeID &&
		event.Action.Type == request.Action.Type &&
		event.Action.IdempotencyKey == request.Action.IdempotencyKey &&
		event.Action.InputRef == request.Action.InputRef &&
		event.Action.InputHash == request.Action.InputHash &&
		event.Observation == request.Observation &&
		event.Decision.ProposalID == request.Proposal.ProposalID &&
		event.Decision.ProposedAction == request.Proposal.ProposedAction &&
		event.Decision.Reason == request.Proposal.Rationale &&
		event.Decision.CandidateSetID == request.Proposal.CandidateSetID &&
		targetMatchesProposal(event.Decision.Target, request.Proposal.Target, request.Proposal.ProposedAction) &&
		stringsEqual(event.Decision.EvidenceRefs, request.Proposal.EvidenceRefs)
}

func proposalTarget(target *decision.ProposalTarget) *trace.Target {
	if target == nil {
		return nil
	}
	return &trace.Target{MerchantDID: target.MerchantDID, CapabilityID: target.CapabilityID, CatalogVersion: target.CatalogVersion, CatalogSnapshotHash: target.CatalogSnapshotHash, CatalogSnapshotRef: target.CatalogSnapshotRef}
}

func targetMatchesProposal(eventTarget *trace.Target, proposalTargetValue *decision.ProposalTarget, action trace.ActionType) bool {
	requestTarget := proposalTarget(proposalTargetValue)
	if action != trace.ActionSelectMerchant {
		return targetEqual(eventTarget, requestTarget)
	}
	if eventTarget == nil || requestTarget == nil || eventTarget.MerchantDID != requestTarget.MerchantDID || eventTarget.CapabilityID != requestTarget.CapabilityID {
		return eventTarget == nil && requestTarget == nil
	}
	if requestTarget.CatalogVersion != "" && eventTarget.CatalogVersion != requestTarget.CatalogVersion {
		return false
	}
	if requestTarget.CatalogSnapshotHash != "" && eventTarget.CatalogSnapshotHash != requestTarget.CatalogSnapshotHash {
		return false
	}
	if requestTarget.CatalogSnapshotRef != "" && eventTarget.CatalogSnapshotRef != requestTarget.CatalogSnapshotRef {
		return false
	}
	return true
}

func targetEqual(left, right *trace.Target) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func stringsEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func randomID(prefix string) string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return prefix + "-unavailable"
	}
	return prefix + "_" + hex.EncodeToString(buffer)
}
