// Package application coordinates the domain aggregate, proposal guard and
// atomic repository commit. Decision providers only create proposals; this
// package owns the commit authority.
package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/decision"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

const DefaultRuntimeVersion = "commerce-runtime-mvp.1"

type Service struct {
	store          repository.TransitionStore
	clock          func() time.Time
	idGenerator    func(prefix string) string
	runtimeVersion string
	guard          decision.RuntimeGuard
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

func NewService(store repository.TransitionStore, options ...Option) *Service {
	service := &Service{
		store:          store,
		clock:          func() time.Time { return time.Now().UTC() },
		idGenerator:    randomID,
		runtimeVersion: DefaultRuntimeVersion,
		guard:          decision.NewRuntimeGuard(),
	}
	for _, option := range options {
		option(service)
	}
	return service
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
	created, err := episode.New(s.idGenerator("ce"), normalized, s.clock())
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
		return CommitResult{}, err
	}

	next := current.Clone()
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
	if err := next.ApplyTransition(guardResult.NextState, now, reason); err != nil {
		return CommitResult{}, err
	}
	event, err := episode.NewEvent(
		s.idGenerator("evt"), current.EpisodeID, current.Version,
		now, current.State, request.Action, request.Observation,
		trace.Decision{ProposedAction: request.Proposal.ProposedAction, ProposalID: request.Proposal.ProposalID, Reason: request.Proposal.Rationale,
			Target: proposalTarget(request.Proposal.Target), EvidenceRefs: append([]string(nil), request.Proposal.EvidenceRefs...)},
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
			}
		}
		return CommitResult{}, err
	}
	return CommitResult{Episode: next, Event: event}, nil
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
		targetEqual(event.Decision.Target, proposalTarget(request.Proposal.Target)) &&
		stringsEqual(event.Decision.EvidenceRefs, request.Proposal.EvidenceRefs)
}

func proposalTarget(target *decision.ProposalTarget) *trace.Target {
	if target == nil {
		return nil
	}
	return &trace.Target{MerchantDID: target.MerchantDID, CapabilityID: target.CapabilityID}
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
