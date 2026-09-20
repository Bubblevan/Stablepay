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
	"os"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/adapters"
	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/decision"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/evidence"
	"github.com/stablepay/commerce-runtime/internal/llm"
	"github.com/stablepay/commerce-runtime/internal/memory"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"github.com/stablepay/commerce-runtime/internal/trace"
	"github.com/stablepay/commerce-runtime/internal/validator"
)

const DefaultRuntimeVersion = "commerce-runtime-mvp.1"
const DefaultCandidateSetTTL = 5 * time.Minute

type Service struct {
	store                 repository.TransitionStore
	clock                 func() time.Time
	idGenerator           func(prefix string) string
	runtimeVersion        string
	candidateSetTTL       time.Duration
	guard                 decision.RuntimeGuard
	paymentDeps           PaymentDependencies
	merchantAdapter       adapters.MerchantAdapter
	validatorRegistry     *validator.Registry
	settlementPolicy      SettlementPolicy
	invocationStaleAfter  time.Duration
	llmProvider           *llm.LLMDecisionProvider
	evidenceRetriever     evidence.Retriever
	allowRuleFallback     bool
	memoryStore           memory.MemoryStore
	memoryProjector       *memory.Projector
	memoryRetrievalPolicy memory.MemoryRetrievalPolicy
	memoryUseTraceStore   memory.MemoryUseTraceStore
}

// SettlementPolicy binds merchant challenges to the configured payment
// environment. Production services reject payment quotes when Network or the
// currency-specific asset is missing. Tests/local fixtures must explicitly
// opt into an unconstrained policy.
type SettlementPolicy struct {
	Network                    string
	Assets                     map[string]string
	AllowUnconstrainedForTests bool
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

func WithSettlementPolicy(policy SettlementPolicy) Option {
	return func(s *Service) {
		s.settlementPolicy = SettlementPolicy{Network: strings.TrimSpace(policy.Network), Assets: copyStringMap(policy.Assets), AllowUnconstrainedForTests: policy.AllowUnconstrainedForTests}
	}
}

// WithUnconstrainedSettlementPolicyForTests is intentionally explicit. It is
// suitable for unit fixtures that do not model a real settlement environment;
// production composition roots must provide Network and every quote asset.
func WithUnconstrainedSettlementPolicyForTests() Option {
	return func(s *Service) {
		s.settlementPolicy.AllowUnconstrainedForTests = true
	}
}

func WithInvocationStaleAfter(after time.Duration) Option {
	return func(s *Service) {
		if after > 0 {
			s.invocationStaleAfter = after
		}
	}
}

func WithLLMDecisionProvider(provider *llm.LLMDecisionProvider) Option {
	return func(s *Service) { s.llmProvider = provider }
}

func WithEvidenceRetriever(retriever evidence.Retriever) Option {
	return func(s *Service) { s.evidenceRetriever = retriever }
}

func WithPersistentEvidenceStore(store evidence.PersistentStore) Option {
	return func(s *Service) {
		if store != nil {
			s.evidenceRetriever = evidence.LexicalRetriever{Registry: evidence.PersistentRegistry{Store: store}}
		}
	}
}

// WithRuleRecoveryFallback makes the fallback policy explicit. A production
// caller must opt in; the runtime never silently labels a rule proposal as an
// LLM proposal.
func WithRuleRecoveryFallback(enabled bool) Option {
	return func(s *Service) { s.allowRuleFallback = enabled }
}

// WithMemoryStore enables S7 derived-memory retrieval and post-terminal
// projection. The underlying runtime store must also implement memory's
// authoritative EpisodeSource surface; otherwise callers can still inject a
// projector explicitly for controlled fixtures.
func WithMemoryStore(store memory.MemoryStore) Option {
	return func(s *Service) {
		if store == nil {
			return
		}
		s.memoryStore = store
		if traceStore, ok := store.(memory.MemoryUseTraceStore); ok {
			s.memoryUseTraceStore = traceStore
		}
		if source, ok := s.store.(memory.EpisodeSource); ok {
			s.memoryProjector = memory.NewProjector(source, store, memory.WithProjectorClock(s.clock))
		}
	}
}

func WithMemoryRetrievalPolicy(policy memory.MemoryRetrievalPolicy) Option {
	return func(s *Service) {
		if policy != nil {
			s.memoryRetrievalPolicy = policy
		}
	}
}

func WithMemoryProjector(projector *memory.Projector) Option {
	return func(s *Service) {
		if projector != nil {
			s.memoryProjector = projector
		}
	}
}

func NewService(store repository.TransitionStore, options ...Option) *Service {
	service := &Service{
		store:                 store,
		clock:                 func() time.Time { return time.Now().UTC() },
		idGenerator:           randomID,
		runtimeVersion:        DefaultRuntimeVersion,
		candidateSetTTL:       DefaultCandidateSetTTL,
		guard:                 decision.NewRuntimeGuard(),
		validatorRegistry:     validator.NewBuiltinRegistry(),
		settlementPolicy:      settlementPolicyFromEnvironment(),
		invocationStaleAfter:  30 * time.Second,
		memoryRetrievalPolicy: memory.DeterministicMemoryRetrievalPolicy{},
	}
	for _, option := range options {
		option(service)
	}
	if service.memoryStore != nil {
		if source, ok := service.store.(memory.EpisodeSource); ok && service.memoryProjector == nil {
			service.memoryProjector = memory.NewProjector(source, service.memoryStore, memory.WithProjectorClock(service.clock))
		}
	}
	return service
}

func settlementPolicyFromEnvironment() SettlementPolicy {
	network := strings.TrimSpace(firstEnvironmentValue("COMMERCE_RUNTIME_SETTLEMENT_NETWORK", "SOLANA_NETWORK"))
	assets := make(map[string]string)
	if value := strings.TrimSpace(firstEnvironmentValue("COMMERCE_RUNTIME_USDC_MINT", "USDC_MINT")); value != "" {
		assets["USDC"] = value
	}
	if value := strings.TrimSpace(firstEnvironmentValue("COMMERCE_RUNTIME_USDT_MINT", "USDT_MINT")); value != "" {
		assets["USDT"] = value
	}
	if len(assets) == 0 {
		assets = nil
	}
	return SettlementPolicy{Network: network, Assets: assets}
}

func firstEnvironmentValue(keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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
	next := current.Clone()
	next.ActionCount++
	if err := next.ApplyCommittedState(episode.StateDiscovering, now, ""); err != nil {
		return DiscoverCapabilitiesResult{}, err
	}
	event, replay, err := s.commitS4Event(ctx, current, next,
		trace.Action{Type: trace.ActionDiscover, IdempotencyKey: actionKey},
		trace.Observation{Type: observationType, FactsRef: candidateSet.FactsRef, PayloadHash: candidateSet.PayloadHash}, nil, current.EpisodeID)
	if err != nil {
		return DiscoverCapabilitiesResult{}, err
	}
	latest := next
	if replay {
		latest, err = s.store.Get(ctx, current.EpisodeID)
		if err != nil {
			return DiscoverCapabilitiesResult{}, err
		}
	}
	result := DiscoverCapabilitiesResult{CandidateSet: candidateSet, Episode: latest, Event: event, NoEligible: len(candidateSet.Candidates) == 0, Replayed: replay}
	if len(candidateSet.Candidates) == 0 {
		stopNow := s.clock().UTC()
		stopProposal := decision.DecisionProposal{ProposalID: s.idGenerator("proposal"), EpisodeID: latest.EpisodeID,
			BasedOnEventSequence: latest.Version - 1, ProposedAction: trace.ActionStop, CandidateSetID: candidateSet.CandidateSetID,
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
	Proposal                 decision.DecisionProposal
	Action                   trace.Action
	Observation              trace.Observation
	Actor                    string
	TraceID                  string
	runtimeRecoveryValidated bool
}

type CommitResult struct {
	Episode  *episode.CommerceEpisode
	Event    *episode.EpisodeEvent
	Replayed bool
}

// RuntimeActionRequest is the narrow internal/runtime boundary for facts that
// are observed or committed by Commerce Runtime itself. Decision providers
// must use CommitProposal and are rejected for these action types.
type RuntimeActionRequest struct {
	EpisodeID   string
	Action      trace.Action
	Observation trace.Observation
	TraceID     string
}

func (s *Service) CommitRuntimeAction(ctx context.Context, request RuntimeActionRequest) (CommitResult, error) {
	if !runtimeOwnedProposalAction(request.Action.Type) {
		return CommitResult{}, decision.ErrActionNotAllowed
	}
	current, err := s.store.Get(ctx, request.EpisodeID)
	if err != nil {
		return CommitResult{}, err
	}
	if request.Action.IdempotencyKey == "" {
		return CommitResult{}, decision.ErrInvalidProposal
	}
	if existing, findErr := s.store.FindByIdempotencyKey(ctx, current.EpisodeID, request.Action.IdempotencyKey); findErr == nil {
		if existing.Action.Type != request.Action.Type || existing.Action.InputRef != request.Action.InputRef || existing.Action.InputHash != request.Action.InputHash || existing.Observation.Type != request.Observation.Type || existing.Observation.Code != request.Observation.Code || existing.Observation.FactsRef != request.Observation.FactsRef || existing.Observation.PayloadHash != request.Observation.PayloadHash {
			return CommitResult{}, repository.ErrIdempotencyConflict
		}
		latest, getErr := s.store.Get(ctx, current.EpisodeID)
		if getErr != nil {
			return CommitResult{}, getErr
		}
		return CommitResult{Episode: latest, Event: existing, Replayed: true}, nil
	} else if !errors.Is(findErr, repository.ErrNotFound) {
		return CommitResult{}, findErr
	}
	nextState, allowed := episode.StateForAction(current.State, request.Action.Type, request.Observation.Type)
	if !allowed {
		return CommitResult{}, decision.ErrActionNotAllowed
	}
	now := s.clock().UTC()
	if !now.Before(current.DeadlineAt) {
		return CommitResult{}, episode.ErrEpisodeExpired
	}
	next := current.Clone()
	next.ActionCount++
	if request.Action.Type == trace.ActionInvoke && current.State == episode.StateInvokingDelivery {
		next.DeliveryAttemptCount++
	}
	if err := next.ApplyCommittedState(nextState, now, request.Observation.Code); err != nil {
		return CommitResult{}, err
	}
	event, replay, err := s.commitS4Event(ctx, current, next, request.Action, request.Observation, nil, request.TraceID)
	if err != nil {
		return CommitResult{}, err
	}
	if replay {
		latest, getErr := s.store.Get(ctx, current.EpisodeID)
		if getErr != nil {
			return CommitResult{}, getErr
		}
		return CommitResult{Episode: latest, Event: event, Replayed: true}, nil
	}
	result := CommitResult{Episode: next, Event: event}
	if err := s.projectTerminalMemory(ctx, next); err != nil {
		return result, err
	}
	return result, nil
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
	if !providerAllowedAction(request.Action.Type) {
		return CommitResult{}, decision.ErrActionNotAllowed
	}
	switch request.Action.Type {
	case trace.ActionRetrySameMerchant:
		if request.Observation.Type == "" {
			request.Observation.Type = trace.ObservationDeliveryInvalid
		}
	case trace.ActionSwitchMerchant, trace.ActionRediscover:
		if request.Observation.Type == "" {
			request.Observation.Type = trace.ObservationCandidatesFound
		}
	case trace.ActionAskParent, trace.ActionStop:
		if request.Observation.Type == "" {
			request.Observation.Type = trace.ObservationPolicyDenied
		}
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
	current, err := s.store.Get(ctx, request.Proposal.EpisodeID)
	if err != nil {
		return CommitResult{}, err
	}
	events, err := s.store.ListByEpisode(ctx, current.EpisodeID)
	if err != nil {
		return CommitResult{}, err
	}
	knownEvidence := s.knownProposalEvidence(ctx, current, request.Proposal, events)
	knownMemory := map[string]struct{}(nil)
	if len(request.Proposal.MemoryRefs) > 0 {
		contextValue, contextErr := s.BuildDecisionContext(ctx, S6DecisionRequest{EpisodeID: current.EpisodeID})
		if contextErr != nil {
			return CommitResult{}, contextErr
		}
		knownMemory = llm.ContextMemoryRefs(contextValue)
	}
	now := s.clock().UTC()
	guardResult, err := s.guard.EvaluateWithMemory(current, request.Proposal, knownEvidence, knownMemory, request.Observation, now)
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
	// S5 actions have dedicated runtime boundaries because their guards depend
	// on persisted recovery/candidate facts. The common proposal guard above is
	// always evaluated first; the dedicated method only adds domain checks.
	switch request.Action.Type {
	case trace.ActionRetrySameMerchant:
		if !request.runtimeRecoveryValidated {
			return s.RetrySameMerchant(ctx, RetrySameMerchantRequest{EpisodeID: request.Proposal.EpisodeID, Proposal: request.Proposal, Action: request.Action, Observation: request.Observation, Actor: request.Actor, TraceID: request.TraceID})
		}
	case trace.ActionSwitchMerchant:
		return s.SwitchMerchant(ctx, SwitchMerchantRequest{EpisodeID: request.Proposal.EpisodeID, Proposal: request.Proposal, Action: request.Action, Observation: request.Observation, Actor: request.Actor, TraceID: request.TraceID})
	case trace.ActionRediscover:
		return s.Rediscover(ctx, RediscoverRequest{EpisodeID: request.Proposal.EpisodeID, Proposal: request.Proposal, Action: request.Action, Observation: request.Observation, Actor: request.Actor, TraceID: request.TraceID})
	case trace.ActionAskParent:
		return s.AskParent(ctx, AskParentRequest{EpisodeID: request.Proposal.EpisodeID, Proposal: request.Proposal, Action: request.Action, Observation: request.Observation, Actor: request.Actor, TraceID: request.TraceID, CandidateSetID: request.Proposal.CandidateSetID, CandidateMerchantDID: func() string {
			if request.Proposal.Target != nil {
				return request.Proposal.Target.MerchantDID
			}
			return ""
		}(), CandidateCapabilityID: func() string {
			if request.Proposal.Target != nil {
				return request.Proposal.Target.CapabilityID
			}
			return ""
		}()})
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
			CandidateSetID: request.Proposal.CandidateSetID, Target: eventTarget, EvidenceRefs: append([]string(nil), request.Proposal.EvidenceRefs...), MemoryRefs: append([]string(nil), request.Proposal.MemoryRefs...)},
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
	result := CommitResult{Episode: next, Event: event}
	if err := s.projectTerminalMemory(ctx, next); err != nil {
		return result, err
	}
	return result, nil
}

// Payment, budget and entitlement facts are committed by the deterministic
// runtime methods, never by a DecisionProvider proposal. This keeps proposal
// handling useful for orchestration while preventing a model from asserting a
// reservation, settlement or entitlement as if it were a trusted fact.
func runtimeOwnedProposalAction(action trace.ActionType) bool {
	switch action {
	case trace.ActionDiscover, trace.ActionInvoke, trace.ActionParse402, trace.ActionValidateDelivery,
		trace.ActionReserveBudget, trace.ActionNegotiateAndPay, trace.ActionCreatePayment,
		trace.ActionVerifyEntitlement, trace.ActionPaymentAuthorizationChecked,
		trace.ActionPaymentSubmitted, trace.ActionPaymentPending, trace.ActionPaymentStatusQueried,
		trace.ActionPaymentConfirmed, trace.ActionPaymentFailed, trace.ActionPaymentUnknown:
		return true
	default:
		return false
	}
}

// providerAllowedAction is intentionally positive. Runtime facts and every
// future action are denied until their proposal authority is explicitly
// reviewed and added.
func providerAllowedAction(action trace.ActionType) bool {
	return decision.ProviderAllowedAction(action)
}

func (s *Service) validateRecoveryProposal(ctx context.Context, current *episode.CommerceEpisode, proposal decision.DecisionProposal, action trace.ActionType, observation trace.Observation) (decision.GuardResult, error) {
	if !providerAllowedAction(action) || proposal.ProposedAction != action {
		return decision.GuardResult{}, decision.ErrActionNotAllowed
	}
	events, err := s.store.ListByEpisode(ctx, current.EpisodeID)
	if err != nil {
		return decision.GuardResult{}, err
	}
	knownMemory := map[string]struct{}(nil)
	if len(proposal.MemoryRefs) > 0 {
		contextValue, contextErr := s.BuildDecisionContext(ctx, S6DecisionRequest{EpisodeID: current.EpisodeID})
		if contextErr != nil {
			return decision.GuardResult{}, contextErr
		}
		knownMemory = llm.ContextMemoryRefs(contextValue)
	}
	return s.guard.ValidateRecoveryProposalWithMemory(current, proposal, s.knownProposalEvidence(ctx, current, proposal, events), knownMemory, observation, s.clock().UTC())
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

// ProjectEpisodeMemory is an explicit rebuild/replay hook. It is safe to run
// after a crash between the authoritative terminal commit and derived memory.
func (s *Service) ProjectEpisodeMemory(ctx context.Context, episodeID string) ([]memory.MemoryMutation, error) {
	if s == nil || s.memoryProjector == nil {
		return nil, repository.ErrRepositoryUnavailable
	}
	return s.memoryProjector.ProjectEpisode(ctx, episodeID)
}

func (s *Service) projectTerminalMemory(ctx context.Context, value *episode.CommerceEpisode) error {
	if s == nil || s.memoryProjector == nil || value == nil || !episode.IsTerminal(value.State) {
		return nil
	}
	_, err := s.memoryProjector.ProjectEpisode(ctx, value.EpisodeID)
	return err
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
		for _, ref := range event.Decision.EvidenceRefs {
			if ref != "" {
				refs[ref] = struct{}{}
			}
		}
	}
	return refs
}

func (s *Service) knownProposalEvidence(ctx context.Context, current *episode.CommerceEpisode, proposal decision.DecisionProposal, events []*episode.EpisodeEvent) map[string]struct{} {
	refs := evidenceReferences(events)
	if store, ok := s.store.(repository.EvidenceRepository); ok {
		if records, err := store.ListEvidenceRecords(ctx); err == nil {
			addPersistedEvidenceRefs(refs, records, s.clock().UTC())
		}
	}
	if store, err := s.s5Store(); err == nil {
		if recoveryContext, err := store.GetRecoveryContextByEpisode(ctx, current.EpisodeID); err == nil {
			refs[recoveryContext.FactsRef] = struct{}{}
			refs[recoveryContext.PayloadHash] = struct{}{}
		}
	}
	if store, err := s.s4Store(); err == nil {
		for _, validationID := range current.ValidationEvidenceRefs {
			validation, validationErr := store.GetValidationEvidence(ctx, validationID)
			if validationErr != nil {
				continue
			}
			refs[validation.PayloadHash] = struct{}{}
			for _, ref := range validation.EvidenceRefs {
				refs[ref] = struct{}{}
			}
		}
	}
	if strings.TrimSpace(proposal.CandidateSetID) != "" {
		if store, err := s.discoveryStore(); err == nil {
			if candidateSet, err := store.GetCandidateSet(ctx, proposal.CandidateSetID); err == nil && candidateSet.EpisodeID == current.EpisodeID && candidateSet.RequestID == current.RequestID {
				refs[candidateSet.FactsRef] = struct{}{}
				refs[candidateSet.PayloadHash] = struct{}{}
				for _, candidate := range candidateSet.Candidates {
					refs[candidate.CatalogSnapshotRef] = struct{}{}
					refs[candidate.CatalogSnapshotHash] = struct{}{}
				}
			}
		}
	}
	return refs
}

func addPersistedEvidenceRefs(refs map[string]struct{}, records []*evidence.EvidenceRecord, now time.Time) {
	for _, record := range records {
		if record == nil {
			continue
		}
		if record.ValidUntil != nil && !now.Before(*record.ValidUntil) {
			continue
		}
		refs[record.EvidenceRef] = struct{}{}
		refs[record.PayloadHash] = struct{}{}
		refs[record.ChunkHash] = struct{}{}
	}
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
