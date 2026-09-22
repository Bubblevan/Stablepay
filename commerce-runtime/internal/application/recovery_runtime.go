package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/decision"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/ledger"
	"github.com/stablepay/commerce-runtime/internal/recovery"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

func (s *Service) s5Store() (repository.S5Store, error) {
	if s == nil || s.store == nil {
		return nil, repository.ErrRepositoryUnavailable
	}
	store, ok := s.store.(repository.S5Store)
	if !ok {
		return nil, repository.ErrRepositoryUnavailable
	}
	return store, nil
}

type SwitchMerchantRequest struct {
	EpisodeID   string
	Proposal    decision.DecisionProposal
	Action      trace.Action
	Observation trace.Observation
	Actor       string
	TraceID     string
}

type RediscoverRequest struct {
	EpisodeID   string
	Proposal    decision.DecisionProposal
	Action      trace.Action
	Observation trace.Observation
	Actor       string
	TraceID     string
	ExpiresAt   time.Time
}

type AskParentRequest struct {
	EpisodeID                    string
	Proposal                     decision.DecisionProposal
	Action                       trace.Action
	Observation                  trace.Observation
	Actor                        string
	TraceID                      string
	ApprovalID                   string
	ApprovalScope                recovery.ApprovalScope
	RequestedAction              trace.ActionType
	CandidateSetID               string
	CandidateMerchantDID         string
	CandidateCapabilityID        string
	RequestedBudgetIncreaseMinor int64
	ExpiresAt                    time.Time
}

// ParentConfirmationRequest is a runtime-owned policy gate. It is used when
// a trusted quote exceeds a contract threshold; no model proposal is accepted
// and the same durable S5 approval/decision path is used for resumption.
type ParentConfirmationRequest struct {
	EpisodeID  string
	ApprovalID string
	TraceID    string
	ExpiresAt  time.Time
}

type ParentDecisionRequest struct {
	ApprovalID  string
	Decision    recovery.Decision
	ActorRef    string
	FactsRef    string
	PayloadHash string
	OccurredAt  time.Time
	TraceID     string
}

type StopRecoveryRequest struct {
	EpisodeID   string
	Action      trace.Action
	Observation trace.Observation
	Actor       string
	TraceID     string
}

func normalizeRecoveryProposal(s *Service, current *episode.CommerceEpisode, proposal *decision.DecisionProposal, action trace.ActionType) {
	now := s.clock().UTC()
	if proposal.ProposedAction == "" {
		proposal.ProposedAction = action
	}
	if proposal.ProposalID == "" {
		proposal.ProposalID = fmt.Sprintf("%s:%s:%d", strings.ToLower(string(action)), current.EpisodeID, current.ActionCount+1)
	}
	if proposal.EpisodeID == "" {
		proposal.EpisodeID = current.EpisodeID
	}
	if proposal.BasedOnEventSequence == 0 {
		proposal.BasedOnEventSequence = current.Version - 1
	}
	if proposal.CreatedAt.IsZero() {
		proposal.CreatedAt = now.Add(-time.Nanosecond)
	}
	if proposal.ExpiresAt.IsZero() {
		proposal.ExpiresAt = now.Add(time.Minute)
	}
	if proposal.Confidence == 0 {
		proposal.Confidence = 1
	}
}

func (s *Service) currentRecoveryContext(ctx context.Context, current *episode.CommerceEpisode, reason recovery.ReasonCode, candidateSetID, trigger string) (*recovery.RecoveryContext, error) {
	store, err := s.s5Store()
	if err != nil {
		return nil, err
	}
	var existing *recovery.RecoveryContext
	if value, e := store.GetRecoveryContextByEpisode(ctx, current.EpisodeID); e == nil {
		existing = value
	} else if !errors.Is(e, repository.ErrNotFound) {
		return nil, e
	}
	finance, err := s.financeStore()
	if err != nil {
		return nil, err
	}
	projection, err := s.currentProjection(ctx, finance, current)
	if err != nil {
		return nil, err
	}
	intents, err := finance.ListPaymentIntents(ctx, current.EpisodeID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(intents))
	for _, intent := range intents {
		ids = append(ids, intent.IntentID)
	}
	if existing != nil {
		value := existing.Clone()
		value.ReasonCode = reason
		if candidateSetID != "" {
			value.CandidateSetID = candidateSetID
		}
		if trigger != "" {
			value.TriggerEventID = trigger
		}
		value.CurrentMerchantDID = current.SelectedMerchantDID
		value.CurrentCapabilityID = current.SelectedCapabilityID
		value.AttemptedMerchants = append([]string(nil), current.AttemptedMerchants...)
		value.DeliveryAttemptCount = current.DeliveryAttemptCount
		value.RetryCount = current.RetryCount
		value.PaymentIntentIDs = ids
		value.SettledMinor = projection.SettledAmount
		value.RefundedMinor = projection.RefundedAmount
		value.ConsumedMinor = projection.ConsumedAmount
		value.AvailableMinor = projection.AvailableBudget
		value.SunkCostMinor = projection.SunkCost
		if value.RefreshPayloadHash() != nil {
			return nil, recovery.ErrInvalidRecoveryContext
		}
		return value, nil
	}
	value := &recovery.RecoveryContext{RecoveryID: s.idGenerator("recovery"), EpisodeID: current.EpisodeID, TriggerEventID: trigger, ReasonCode: reason, CurrentMerchantDID: current.SelectedMerchantDID, CurrentCapabilityID: current.SelectedCapabilityID, CandidateSetID: candidateSetID, AttemptedMerchants: append([]string(nil), current.AttemptedMerchants...), PaymentIntentIDs: ids, SettledMinor: projection.SettledAmount, RefundedMinor: projection.RefundedAmount, ConsumedMinor: projection.ConsumedAmount, AvailableMinor: projection.AvailableBudget, SunkCostMinor: projection.SunkCost, DeliveryAttemptCount: current.DeliveryAttemptCount, RetryCount: current.RetryCount, DeadlineAt: current.DeadlineAt, CreatedAt: s.clock().UTC(), FactsRef: "recovery://" + current.EpisodeID}
	if value.RefreshPayloadHash() != nil {
		return nil, recovery.ErrInvalidRecoveryContext
	}
	return value, nil
}

func appendAttemptedMerchant(values []string, merchant string) []string {
	merchant = strings.TrimSpace(merchant)
	for _, value := range values {
		if value == merchant {
			return append([]string(nil), values...)
		}
	}
	return append(append([]string(nil), values...), merchant)
}
func attemptedMerchant(values []string, merchant string) bool {
	for _, value := range values {
		if value == merchant {
			return true
		}
	}
	return false
}

func (s *Service) SwitchMerchant(ctx context.Context, request SwitchMerchantRequest) (CommitResult, error) {
	store, err := s.s5Store()
	if err != nil {
		return CommitResult{}, err
	}
	current, err := s.store.Get(ctx, request.EpisodeID)
	if err != nil {
		return CommitResult{}, err
	}
	if strings.TrimSpace(request.Action.IdempotencyKey) != "" {
		if existing, replayErr := s.store.FindByIdempotencyKey(ctx, current.EpisodeID, request.Action.IdempotencyKey); replayErr == nil {
			return CommitResult{Episode: current, Event: existing, Replayed: true}, nil
		} else if !errors.Is(replayErr, repository.ErrNotFound) {
			return CommitResult{}, replayErr
		}
	}
	if current.State != episode.StateRecovering {
		return CommitResult{}, decision.ErrActionNotAllowed
	}
	if len(request.Proposal.EvidenceRefs) == 0 {
		return CommitResult{}, decision.ErrUnknownEvidenceReference
	}
	normalizeRecoveryProposal(s, current, &request.Proposal, trace.ActionSwitchMerchant)
	if request.Action.Type == "" {
		request.Action.Type = trace.ActionSwitchMerchant
	}
	if request.Action.IdempotencyKey == "" {
		request.Action.IdempotencyKey = "switch:" + current.EpisodeID + ":" + fmt.Sprint(current.ActionCount+1)
	}
	if request.Observation.Type == "" {
		request.Observation.Type = trace.ObservationCandidatesFound
	}
	if request.Actor == "" {
		request.Actor = "runtime"
	}
	if request.TraceID == "" {
		request.TraceID = current.EpisodeID + ":switch"
	}
	if existing, e := s.store.FindByIdempotencyKey(ctx, current.EpisodeID, request.Action.IdempotencyKey); e == nil {
		return CommitResult{Episode: current, Event: existing, Replayed: true}, nil
	}
	if _, err := s.validateRecoveryProposal(ctx, current, request.Proposal, request.Action.Type, request.Observation); err != nil {
		return CommitResult{}, err
	}
	if request.Proposal.ProposedAction != trace.ActionSwitchMerchant || request.Proposal.Target == nil || request.Proposal.CandidateSetID == "" {
		return CommitResult{}, decision.ErrInvalidProposal
	}
	if len(request.Proposal.EvidenceRefs) == 0 {
		return CommitResult{}, decision.ErrUnknownEvidenceReference
	}
	discovery, err := s.discoveryStore()
	if err != nil {
		return CommitResult{}, err
	}
	set, err := discovery.GetCandidateSet(ctx, request.Proposal.CandidateSetID)
	if err != nil {
		return CommitResult{}, err
	}
	if rc, rcErr := store.GetRecoveryContextByEpisode(ctx, current.EpisodeID); rcErr == nil && rc.CandidateSetID != "" && rc.CandidateSetID != set.CandidateSetID {
		return CommitResult{}, decision.ErrCandidateSetMismatch
	}
	if rc, rcErr := store.GetRecoveryContextByEpisode(ctx, current.EpisodeID); rcErr == nil {
		known := map[string]struct{}{set.FactsRef: {}, set.PayloadHash: {}, rc.FactsRef: {}, rc.PayloadHash: {}}
		for _, candidate := range set.Candidates {
			known[candidate.CatalogSnapshotRef] = struct{}{}
			known[candidate.CatalogSnapshotHash] = struct{}{}
		}
		if evidenceStore, ok := s.store.(repository.EvidenceRepository); ok {
			if records, listErr := evidenceStore.ListEvidenceRecords(ctx); listErr == nil {
				for _, record := range records {
					if record == nil {
						continue
					}
					if record.ValidUntil != nil && !s.clock().UTC().Before(*record.ValidUntil) {
						continue
					}
					known[record.EvidenceRef] = struct{}{}
					known[record.PayloadHash] = struct{}{}
					known[record.ChunkHash] = struct{}{}
				}
			}
		}
		if validationStore, validationErr := s.s4Store(); validationErr == nil {
			for _, validationID := range current.ValidationEvidenceRefs {
				validation, getErr := validationStore.GetValidationEvidence(ctx, validationID)
				if getErr != nil {
					continue
				}
				known[validation.PayloadHash] = struct{}{}
				for _, ref := range validation.EvidenceRefs {
					known[ref] = struct{}{}
				}
			}
		}
		for _, ref := range request.Proposal.EvidenceRefs {
			if _, ok := known[ref]; !ok {
				return CommitResult{}, fmt.Errorf("%w: %s", decision.ErrUnknownEvidenceReference, ref)
			}
		}
	}
	if err = set.ValidateAt(s.clock().UTC()); err != nil {
		return CommitResult{}, err
	}
	candidate, ok := set.FindCandidate(request.Proposal.Target.MerchantDID, request.Proposal.Target.CapabilityID)
	if !ok {
		return CommitResult{}, decision.ErrMerchantNotInCandidateSet
	}
	if attemptedMerchant(current.AttemptedMerchants, candidate.MerchantDID) || candidate.MerchantDID == current.SelectedMerchantDID {
		return CommitResult{}, errors.New("recovery candidate was already attempted")
	}
	if request.Proposal.Target.CatalogVersion != "" && request.Proposal.Target.CatalogVersion != candidate.CatalogVersion {
		return CommitResult{}, decision.ErrCatalogSnapshotMismatch
	}
	now := s.clock().UTC()
	if !now.Before(current.DeadlineAt) {
		return CommitResult{}, episode.ErrEpisodeExpired
	}
	if current.ActionCount >= current.MaxTotalAttempts {
		return CommitResult{}, decision.ErrAttemptLimit
	}
	next := current.Clone()
	next.SelectedMerchantDID = candidate.MerchantDID
	next.SelectedCapabilityID = candidate.CapabilityID
	next.SelectedCandidateSetID = set.CandidateSetID
	next.SelectedCatalogVersion = candidate.CatalogVersion
	next.SelectedCatalogSnapshotHash = candidate.CatalogSnapshotHash
	next.SelectedCatalogSnapshotRef = candidate.CatalogSnapshotRef
	next.CurrentQuoteHash = ""
	next.AttemptedMerchants = appendAttemptedMerchant(next.AttemptedMerchants, candidate.MerchantDID)
	next.ActionCount++
	if err := next.ApplyCommittedState(episode.StateInvoking, now, ""); err != nil {
		return CommitResult{}, err
	}
	event, err := episode.NewEvent(s.idGenerator("evt"), current.EpisodeID, current.Version, now, current.State, request.Action, request.Observation, trace.Decision{ProposedAction: trace.ActionSwitchMerchant, ProposalID: request.Proposal.ProposalID, Reason: request.Proposal.Rationale, CandidateSetID: set.CandidateSetID, Target: &trace.Target{MerchantDID: candidate.MerchantDID, CapabilityID: candidate.CapabilityID, CatalogVersion: candidate.CatalogVersion, CatalogSnapshotHash: candidate.CatalogSnapshotHash, CatalogSnapshotRef: candidate.CatalogSnapshotRef}, EvidenceRefs: append([]string(nil), request.Proposal.EvidenceRefs...), MemoryRefs: append([]string(nil), request.Proposal.MemoryRefs...)}, trace.RuntimeVerdict{Allowed: true, Checks: []trace.RuntimeCheck{trace.Check("recovery_candidate", true, "unattempted candidate")}}, next.State, request.Actor, request.TraceID, s.runtimeVersion)
	if err != nil {
		return CommitResult{}, err
	}
	reason := recovery.ReasonDeliveryInvalid
	if old, e := store.GetRecoveryContextByEpisode(ctx, current.EpisodeID); e == nil {
		reason = old.ReasonCode
	}
	rc, err := s.currentRecoveryContext(ctx, next, reason, set.CandidateSetID, event.EventID)
	if err != nil {
		return CommitResult{}, err
	}
	next.RecoveryID = rc.RecoveryID
	if err = store.CommitRecoveryTransition(ctx, repository.RecoveryTransition{EpisodeID: current.EpisodeID, ExpectedEpisodeVersion: current.Version, NextEpisode: next, Event: event, RecoveryContext: rc}); err != nil {
		return CommitResult{}, err
	}
	return CommitResult{Episode: next, Event: event}, nil
}

func (s *Service) Rediscover(ctx context.Context, request RediscoverRequest) (CommitResult, error) {
	store, err := s.s5Store()
	if err != nil {
		return CommitResult{}, err
	}
	current, err := s.store.Get(ctx, request.EpisodeID)
	if err != nil {
		return CommitResult{}, err
	}
	if strings.TrimSpace(request.Action.IdempotencyKey) != "" {
		if existing, replayErr := s.store.FindByIdempotencyKey(ctx, current.EpisodeID, request.Action.IdempotencyKey); replayErr == nil {
			return CommitResult{Episode: current, Event: existing, Replayed: true}, nil
		} else if !errors.Is(replayErr, repository.ErrNotFound) {
			return CommitResult{}, replayErr
		}
	}
	if current.State != episode.StateRecovering {
		return CommitResult{}, decision.ErrActionNotAllowed
	}
	if len(request.Proposal.EvidenceRefs) == 0 {
		return CommitResult{}, decision.ErrUnknownEvidenceReference
	}
	normalizeRecoveryProposal(s, current, &request.Proposal, trace.ActionRediscover)
	if request.Action.Type == "" {
		request.Action.Type = trace.ActionRediscover
	}
	if request.Action.IdempotencyKey == "" {
		request.Action.IdempotencyKey = fmt.Sprintf("rediscover:%s:%d", current.EpisodeID, current.DiscoveryGeneration+1)
	}
	if request.Observation.Type == "" {
		request.Observation.Type = trace.ObservationCandidatesFound
	}
	if request.Actor == "" {
		request.Actor = "runtime"
	}
	if request.TraceID == "" {
		request.TraceID = current.EpisodeID + ":rediscover"
	}
	if existing, e := s.store.FindByIdempotencyKey(ctx, current.EpisodeID, request.Action.IdempotencyKey); e == nil {
		return CommitResult{Episode: current, Event: existing, Replayed: true}, nil
	}
	if _, err := s.validateRecoveryProposal(ctx, current, request.Proposal, request.Action.Type, request.Observation); err != nil {
		return CommitResult{}, err
	}
	var acquire contract.AcquireCapabilityRequest
	if err = json.Unmarshal(current.ContractSnapshot, &acquire); err != nil {
		return CommitResult{}, err
	}
	query, err := catalog.FromAcquireCapabilityRequest(acquire)
	if err != nil {
		return CommitResult{}, err
	}
	now := s.clock().UTC()
	generation := current.DiscoveryGeneration + 1
	setID := fmt.Sprintf("cs:%s:%d", current.EpisodeID, generation)
	discovery, err := s.discoveryStore()
	if err != nil {
		return CommitResult{}, err
	}
	set, setErr := discovery.GetCandidateSet(ctx, setID)
	if errors.Is(setErr, repository.ErrNotFound) {
		caps, listErr := discovery.ListActiveCapabilities(ctx)
		if listErr != nil {
			return CommitResult{}, listErr
		}
		filtered := make([]*catalog.MerchantCapability, 0, len(caps))
		for _, cap := range caps {
			if !attemptedMerchant(current.AttemptedMerchants, cap.MerchantDID) {
				filtered = append(filtered, cap)
			}
		}
		expires := request.ExpiresAt
		if expires.IsZero() {
			expires = now.Add(s.candidateSetTTL)
		}
		if expires.After(current.DeadlineAt) {
			expires = current.DeadlineAt
		}
		set, err = catalog.BuildCandidateSet(setID, current.EpisodeID, current.RequestID, query, filtered, now, expires)
		if err != nil {
			return CommitResult{}, err
		}
		set.Generation = generation
		set.PayloadHash, err = set.PayloadHashFor()
		if err != nil {
			return CommitResult{}, err
		}
	} else if setErr != nil {
		return CommitResult{}, setErr
	} else {
		if set.EpisodeID != current.EpisodeID || set.RequestID != current.RequestID || set.Generation != generation {
			return CommitResult{}, repository.ErrCandidateSetConflict
		}
		if err := set.Validate(); err != nil {
			return CommitResult{}, err
		}
	}
	next := current.Clone()
	next.DiscoveryGeneration = generation
	next.ActionCount++
	if err = next.ApplyCommittedState(episode.StateDiscovering, now, ""); err != nil {
		return CommitResult{}, err
	}
	event, err := episode.NewEvent(s.idGenerator("evt"), current.EpisodeID, current.Version, now, current.State, request.Action, request.Observation, trace.Decision{ProposedAction: trace.ActionRediscover, ProposalID: request.Proposal.ProposalID, CandidateSetID: request.Proposal.CandidateSetID, Target: proposalTarget(request.Proposal.Target), EvidenceRefs: append([]string(nil), request.Proposal.EvidenceRefs...), MemoryRefs: append([]string(nil), request.Proposal.MemoryRefs...)}, trace.RuntimeVerdict{Allowed: true}, next.State, request.Actor, request.TraceID, s.runtimeVersion)
	if err != nil {
		return CommitResult{}, err
	}
	reason := recovery.ReasonCatalogStale
	if len(set.Candidates) == 0 {
		reason = recovery.ReasonNoRecoveryCandidate
	}
	rc, err := s.currentRecoveryContext(ctx, next, reason, setID, event.EventID)
	if err != nil {
		return CommitResult{}, err
	}
	next.RecoveryID = rc.RecoveryID
	if err = store.CommitRecoveryTransition(ctx, repository.RecoveryTransition{EpisodeID: current.EpisodeID, ExpectedEpisodeVersion: current.Version, NextEpisode: next, Event: event, RecoveryContext: rc, CandidateSet: set}); err != nil {
		return CommitResult{}, err
	}
	return CommitResult{Episode: next, Event: event}, nil
}

func (s *Service) AskParent(ctx context.Context, request AskParentRequest) (CommitResult, error) {
	store, err := s.s5Store()
	if err != nil {
		return CommitResult{}, err
	}
	current, err := s.store.Get(ctx, request.EpisodeID)
	if err != nil {
		return CommitResult{}, err
	}
	if strings.TrimSpace(request.Action.IdempotencyKey) != "" {
		if existing, replayErr := s.store.FindByIdempotencyKey(ctx, current.EpisodeID, request.Action.IdempotencyKey); replayErr == nil {
			return CommitResult{Episode: current, Event: existing, Replayed: true}, nil
		} else if !errors.Is(replayErr, repository.ErrNotFound) {
			return CommitResult{}, replayErr
		}
	}
	if current.State != episode.StateRecovering {
		return CommitResult{}, decision.ErrActionNotAllowed
	}
	if len(request.Proposal.EvidenceRefs) == 0 {
		return CommitResult{}, decision.ErrUnknownEvidenceReference
	}
	normalizeRecoveryProposal(s, current, &request.Proposal, trace.ActionAskParent)
	if request.Action.Type == "" {
		request.Action.Type = trace.ActionAskParent
	}
	if request.Action.IdempotencyKey == "" {
		request.Action.IdempotencyKey = "ask-parent:" + current.EpisodeID + ":" + fmt.Sprint(current.ActionCount+1)
	}
	if request.Actor == "" {
		request.Actor = "runtime"
	}
	if request.Observation.Type == "" {
		request.Observation.Type = trace.ObservationPolicyDenied
	}
	if request.TraceID == "" {
		request.TraceID = current.EpisodeID + ":ask-parent"
	}
	if request.ApprovalID == "" {
		request.ApprovalID = "approval:" + current.EpisodeID + ":" + fmt.Sprint(current.ActionCount+1)
	}
	if request.ApprovalScope == "" {
		request.ApprovalScope = recovery.AllowSwitch
	}
	if request.RequestedAction == "" {
		request.RequestedAction = trace.ActionSwitchMerchant
	}
	now := s.clock().UTC()
	if request.ExpiresAt.IsZero() {
		request.ExpiresAt = now.Add(10 * time.Minute)
	}
	if !request.ExpiresAt.After(now) {
		return CommitResult{}, recovery.ErrInvalidParentRequest
	}
	if request.ExpiresAt.After(current.DeadlineAt) {
		request.ExpiresAt = current.DeadlineAt
	}
	if !request.ExpiresAt.After(now) {
		return CommitResult{}, recovery.ErrInvalidParentRequest
	}
	if existing, e := s.store.FindByIdempotencyKey(ctx, current.EpisodeID, request.Action.IdempotencyKey); e == nil {
		return CommitResult{Episode: current, Event: existing, Replayed: true}, nil
	}
	if _, err := s.validateRecoveryProposal(ctx, current, request.Proposal, request.Action.Type, request.Observation); err != nil {
		return CommitResult{}, err
	}
	parentReason := recovery.ReasonBudgetInsufficient
	if existing, existingErr := store.GetRecoveryContextByEpisode(ctx, current.EpisodeID); existingErr == nil {
		parentReason = existing.ReasonCode
	} else if !errors.Is(existingErr, repository.ErrNotFound) {
		return CommitResult{}, existingErr
	}
	rc, err := s.currentRecoveryContext(ctx, current, parentReason, "", "")
	if err != nil {
		return CommitResult{}, err
	}
	approval := &recovery.ParentApprovalRequest{ApprovalID: request.ApprovalID, EpisodeID: current.EpisodeID, RecoveryID: rc.RecoveryID, ReasonCode: rc.ReasonCode, RequestedAction: string(request.RequestedAction), ApprovalScope: request.ApprovalScope, CurrentBudgetMinor: current.Budget.BudgetLimitMinor, ConsumedMinor: current.Budget.ConsumedAmount, AvailableMinor: current.Budget.AvailableBudget, SunkCostMinor: current.Budget.SunkCost, CurrentMerchantDID: current.SelectedMerchantDID, CandidateSetID: request.CandidateSetID, CandidateMerchantDID: request.CandidateMerchantDID, CandidateCapabilityID: request.CandidateCapabilityID, RequestedBudgetIncreaseMinor: request.RequestedBudgetIncreaseMinor, ExpiresAt: request.ExpiresAt, FactsRef: "parent-approval://" + request.ApprovalID, CreatedAt: now}
	if err = approval.RefreshPayloadHash(); err != nil {
		return CommitResult{}, err
	}
	if err = approval.Validate(); err != nil {
		return CommitResult{}, err
	}
	next := current.Clone()
	next.ActionCount++
	if err = next.ApplyCommittedState(episode.StateAwaitingParent, now, ""); err != nil {
		return CommitResult{}, err
	}
	event, err := episode.NewEvent(s.idGenerator("evt"), current.EpisodeID, current.Version, now, current.State, request.Action, request.Observation, trace.Decision{ProposedAction: trace.ActionAskParent, ProposalID: request.Proposal.ProposalID, Reason: request.Proposal.Rationale, CandidateSetID: request.Proposal.CandidateSetID, Target: proposalTarget(request.Proposal.Target), EvidenceRefs: append([]string(nil), request.Proposal.EvidenceRefs...), MemoryRefs: append([]string(nil), request.Proposal.MemoryRefs...)}, trace.RuntimeVerdict{Allowed: true}, next.State, request.Actor, request.TraceID, s.runtimeVersion)
	if err != nil {
		return CommitResult{}, err
	}
	rc.TriggerEventID = event.EventID
	if err := rc.RefreshPayloadHash(); err != nil {
		return CommitResult{}, err
	}
	next.RecoveryID = rc.RecoveryID
	if err = store.CommitRecoveryTransition(ctx, repository.RecoveryTransition{EpisodeID: current.EpisodeID, ExpectedEpisodeVersion: current.Version, NextEpisode: next, Event: event, RecoveryContext: rc, ParentApproval: approval}); err != nil {
		return CommitResult{}, err
	}
	return CommitResult{Episode: next, Event: event}, nil
}

func (s *Service) RequestParentConfirmation(ctx context.Context, request ParentConfirmationRequest) (CommitResult, error) {
	store, err := s.s5Store()
	if err != nil {
		return CommitResult{}, err
	}
	current, err := s.store.Get(ctx, request.EpisodeID)
	if err != nil {
		return CommitResult{}, err
	}
	if current.State != episode.StateNegotiating {
		return CommitResult{}, decision.ErrActionNotAllowed
	}
	now := s.clock().UTC()
	idempotencyKey := "parent-threshold:" + current.EpisodeID + ":" + fmt.Sprint(current.ActionCount+1)
	if existing, findErr := s.store.FindByIdempotencyKey(ctx, current.EpisodeID, idempotencyKey); findErr == nil {
		return CommitResult{Episode: current, Event: existing, Replayed: true}, nil
	} else if !errors.Is(findErr, repository.ErrNotFound) {
		return CommitResult{}, findErr
	}
	if request.ApprovalID == "" {
		request.ApprovalID = "approval:" + current.EpisodeID + ":" + fmt.Sprint(current.ActionCount+1)
	}
	if request.ExpiresAt.IsZero() {
		request.ExpiresAt = now.Add(10 * time.Minute)
	}
	if request.ExpiresAt.After(current.DeadlineAt) {
		request.ExpiresAt = current.DeadlineAt
	}
	if !request.ExpiresAt.After(now) {
		return CommitResult{}, recovery.ErrInvalidParentRequest
	}
	rc, err := s.currentRecoveryContext(ctx, current, recovery.ReasonParentConfirmation, current.SelectedCandidateSetID, "")
	if err != nil {
		return CommitResult{}, err
	}
	approval := &recovery.ParentApprovalRequest{
		ApprovalID: request.ApprovalID, EpisodeID: current.EpisodeID, RecoveryID: rc.RecoveryID,
		ReasonCode: recovery.ReasonParentConfirmation, RequestedAction: string(trace.ActionRetrySameMerchant),
		ApprovalScope: recovery.AllowSwitch, CurrentBudgetMinor: current.Budget.BudgetLimitMinor,
		ConsumedMinor: current.Budget.ConsumedAmount, AvailableMinor: current.Budget.AvailableBudget,
		SunkCostMinor: current.Budget.SunkCost, CurrentMerchantDID: current.SelectedMerchantDID,
		CandidateSetID: current.SelectedCandidateSetID, CandidateCapabilityID: current.SelectedCapabilityID,
		ExpiresAt: request.ExpiresAt, FactsRef: "parent-approval://" + request.ApprovalID, CreatedAt: now,
	}
	if err := approval.RefreshPayloadHash(); err != nil {
		return CommitResult{}, err
	}
	if err := approval.Validate(); err != nil {
		return CommitResult{}, err
	}
	next := current.Clone()
	next.ActionCount++
	if err := next.ApplyCommittedState(episode.StateAwaitingParent, now, string(recovery.ReasonParentConfirmation)); err != nil {
		return CommitResult{}, err
	}
	if request.TraceID == "" {
		request.TraceID = current.EpisodeID + ":parent-threshold"
	}
	event, err := episode.NewEvent(s.idGenerator("evt"), current.EpisodeID, current.Version, now, current.State,
		trace.Action{Type: trace.ActionAskParent, IdempotencyKey: idempotencyKey},
		trace.Observation{Type: trace.ObservationPolicyDenied, Code: string(recovery.ReasonParentConfirmation), FactsRef: approval.FactsRef, PayloadHash: approval.PayloadHash},
		trace.Decision{ProposedAction: trace.ActionAskParent, ProposalID: approval.ApprovalID, Reason: "trusted quote exceeds parent confirmation threshold"},
		trace.RuntimeVerdict{Allowed: true}, next.State, "runtime", request.TraceID, s.runtimeVersion)
	if err != nil {
		return CommitResult{}, err
	}
	rc.TriggerEventID = event.EventID
	if err := rc.RefreshPayloadHash(); err != nil {
		return CommitResult{}, err
	}
	next.RecoveryID = rc.RecoveryID
	if err := store.CommitRecoveryTransition(ctx, repository.RecoveryTransition{
		EpisodeID: current.EpisodeID, ExpectedEpisodeVersion: current.Version, NextEpisode: next,
		Event: event, RecoveryContext: rc, ParentApproval: approval,
	}); err != nil {
		return CommitResult{}, err
	}
	return CommitResult{Episode: next, Event: event}, nil
}

func (s *Service) RecordParentDecision(ctx context.Context, request ParentDecisionRequest) (CommitResult, error) {
	store, err := s.s5Store()
	if err != nil {
		return CommitResult{}, err
	}
	approval, err := store.GetParentApprovalRequest(ctx, request.ApprovalID)
	if err != nil {
		return CommitResult{}, err
	}
	current, err := s.store.Get(ctx, approval.EpisodeID)
	if err != nil {
		return CommitResult{}, err
	}
	if request.ActorRef == "" || (request.Decision != recovery.Approve && request.Decision != recovery.Deny) {
		return CommitResult{}, recovery.ErrInvalidParentDecision
	}
	if existing, e := store.GetParentDecision(ctx, request.ApprovalID); e == nil {
		if existing.EpisodeID != approval.EpisodeID || existing.Decision != request.Decision || existing.ActorRef != request.ActorRef || (!request.OccurredAt.IsZero() && !request.OccurredAt.Equal(existing.OccurredAt)) || (request.FactsRef != "" && request.FactsRef != existing.FactsRef) {
			return CommitResult{}, repository.ErrParentDecisionConflict
		}
		return CommitResult{Episode: current, Replayed: true}, nil
	} else if !errors.Is(e, repository.ErrNotFound) {
		return CommitResult{}, e
	}
	if current.State != episode.StateAwaitingParent {
		return CommitResult{}, decision.ErrActionNotAllowed
	}
	now := s.clock().UTC()
	if !now.Before(approval.ExpiresAt) {
		return CommitResult{}, recovery.ErrInvalidParentDecision
	}
	if request.OccurredAt.IsZero() {
		request.OccurredAt = now
	}
	if request.FactsRef == "" {
		request.FactsRef = "parent-decision://" + request.ApprovalID
	}
	decisionFact := &recovery.ParentDecisionFact{ApprovalID: request.ApprovalID, EpisodeID: approval.EpisodeID, Decision: request.Decision, ActorRef: request.ActorRef, OccurredAt: request.OccurredAt, FactsRef: request.FactsRef}
	if err = decisionFact.RefreshPayloadHash(); err != nil {
		return CommitResult{}, err
	}
	next := current.Clone()
	next.ActionCount++
	var amendment *recovery.BudgetAmendment
	after := episode.StateRecovering
	observation := trace.ObservationParentDenied
	reason := recovery.ReasonParentDenied
	if request.Decision == recovery.Approve {
		observation = trace.ObservationParentApproved
		reason = approval.ReasonCode
		if approval.ReasonCode == recovery.ReasonParentConfirmation {
			after = episode.StateNegotiating
		}
		if approval.ApprovalScope == recovery.BudgetIncrease {
			newLimit := current.Budget.BudgetLimitMinor + approval.RequestedBudgetIncreaseMinor
			if newLimit < current.Budget.ConsumedAmount+current.Budget.ReservedAmount {
				return CommitResult{}, ledger.ErrInsufficientBudget
			}
			next.Budget.BudgetLimitMinor = newLimit
			if err = next.Budget.Recalculate(); err != nil {
				return CommitResult{}, err
			}
			amendment = &recovery.BudgetAmendment{AmendmentID: "amendment:" + request.ApprovalID, EpisodeID: current.EpisodeID, OldLimitMinor: current.Budget.BudgetLimitMinor, NewLimitMinor: newLimit, DeltaMinor: approval.RequestedBudgetIncreaseMinor, ApprovedBy: request.ActorRef, ApprovalRef: request.ApprovalID, OccurredAt: request.OccurredAt, FactsRef: "budget-amendment://" + request.ApprovalID}
			if err = amendment.RefreshPayloadHash(); err != nil {
				return CommitResult{}, err
			}
		}
	}
	rc, err := s.currentRecoveryContext(ctx, current, reason, approval.CandidateSetID, "parent-decision:"+request.ApprovalID)
	if err != nil {
		return CommitResult{}, err
	}
	rc.SettledMinor = next.Budget.SettledAmount
	rc.RefundedMinor = next.Budget.RefundedAmount
	rc.ConsumedMinor = next.Budget.ConsumedAmount
	rc.AvailableMinor = next.Budget.AvailableBudget
	rc.SunkCostMinor = next.Budget.SunkCost
	if request.Decision == recovery.Deny {
		rc.ReasonCode = recovery.ReasonParentDenied
	}
	if err := rc.RefreshPayloadHash(); err != nil {
		return CommitResult{}, err
	}
	next.RecoveryID = rc.RecoveryID
	if err = next.ApplyCommittedState(after, now, string(reason)); err != nil {
		return CommitResult{}, err
	}
	eventType := trace.ActionParentDecision
	if request.TraceID == "" {
		request.TraceID = current.EpisodeID + ":parent:" + request.ApprovalID
	}
	event, err := episode.NewEvent(s.idGenerator("evt"), current.EpisodeID, current.Version, now, current.State, trace.Action{Type: eventType, IdempotencyKey: "parent-decision:" + request.ApprovalID}, trace.Observation{Type: observation, Code: string(reason), FactsRef: request.FactsRef, PayloadHash: decisionFact.PayloadHash}, trace.Decision{ProposedAction: eventType, ProposalID: request.ApprovalID, Reason: string(request.Decision), EvidenceRefs: []string{approval.FactsRef, request.FactsRef}}, trace.RuntimeVerdict{Allowed: true}, next.State, "parent", request.TraceID, s.runtimeVersion)
	if err != nil {
		return CommitResult{}, err
	}
	if err = store.CommitParentDecision(ctx, repository.ParentDecisionTransition{EpisodeID: current.EpisodeID, ExpectedEpisodeVersion: current.Version, NextEpisode: next, Event: event, Decision: decisionFact, BudgetAmendment: amendment, RecoveryContext: rc}); err != nil {
		return CommitResult{}, err
	}
	return CommitResult{Episode: next, Event: event}, nil
}

func (s *Service) StopRecovery(ctx context.Context, request StopRecoveryRequest) (CommitResult, error) {
	current, err := s.store.Get(ctx, request.EpisodeID)
	if err != nil {
		return CommitResult{}, err
	}
	if current.State == episode.StateAwaitingParent {
		return CommitResult{}, decision.ErrActionNotAllowed
	}
	if request.Action.Type == "" {
		request.Action.Type = trace.ActionStop
	}
	if request.Action.IdempotencyKey == "" {
		request.Action.IdempotencyKey = "stop-recovery:" + current.EpisodeID
	}
	if request.Observation.Type == "" {
		request.Observation.Type = trace.ObservationPolicyDenied
	}
	if request.Observation.Code == "" {
		if store, e := s.s5Store(); e == nil {
			if rc, e := store.GetRecoveryContextByEpisode(ctx, current.EpisodeID); e == nil && rc.ReasonCode.Valid() {
				request.Observation.Code = string(rc.ReasonCode)
			}
		}
	}
	if request.Actor == "" {
		request.Actor = "runtime"
	}
	if request.TraceID == "" {
		request.TraceID = current.EpisodeID + ":stop-recovery"
	}
	proposal := decision.DecisionProposal{ProposalID: "stop-recovery:" + current.EpisodeID, EpisodeID: current.EpisodeID, BasedOnEventSequence: current.Version - 1, ProposedAction: trace.ActionStop, Confidence: 1, CreatedAt: s.clock().UTC().Add(-time.Nanosecond), ExpiresAt: s.clock().UTC().Add(time.Minute)}
	return s.CommitProposal(ctx, CommitRequest{Proposal: proposal, Action: request.Action, Observation: request.Observation, Actor: request.Actor, TraceID: request.TraceID})
}

func (s *Service) persistBudgetInsufficient(ctx context.Context, current *episode.CommerceEpisode, traceID string) (PaymentIntentResult, error) {
	store, err := s.s5Store()
	if err != nil {
		return PaymentIntentResult{}, err
	}
	key := "budget-insufficient:" + current.EpisodeID + ":" + current.CurrentQuoteHash
	if existing, findErr := s.store.FindByIdempotencyKey(ctx, current.EpisodeID, key); findErr == nil {
		latest, getErr := s.store.Get(ctx, current.EpisodeID)
		if getErr != nil {
			return PaymentIntentResult{}, getErr
		}
		return PaymentIntentResult{Episode: latest, Event: existing}, ledger.ErrInsufficientBudget
	}
	now := s.clock().UTC()
	next := current.Clone()
	next.ActionCount++
	if err := next.ApplyCommittedState(episode.StateRecovering, now, ""); err != nil {
		return PaymentIntentResult{}, err
	}
	event, err := episode.NewEvent(s.idGenerator("evt"), current.EpisodeID, current.Version, now, current.State,
		trace.Action{Type: trace.ActionReserveBudget, IdempotencyKey: key},
		trace.Observation{Type: trace.ObservationPolicyDenied, Code: string(recovery.ReasonBudgetInsufficient), FactsRef: "budget://" + current.EpisodeID},
		trace.Decision{ProposedAction: trace.ActionReserveBudget, ProposalID: "runtime:" + key, Reason: "available budget is insufficient"},
		trace.RuntimeVerdict{Allowed: true}, next.State, "runtime", traceID, s.runtimeVersion)
	if err != nil {
		return PaymentIntentResult{}, err
	}
	rc, err := s.currentRecoveryContext(ctx, current, recovery.ReasonBudgetInsufficient, current.SelectedCandidateSetID, event.EventID)
	if err != nil {
		return PaymentIntentResult{}, err
	}
	next.RecoveryID = rc.RecoveryID
	if err := store.CommitRecoveryTransition(ctx, repository.RecoveryTransition{EpisodeID: current.EpisodeID, ExpectedEpisodeVersion: current.Version, NextEpisode: next, Event: event, RecoveryContext: rc}); err != nil {
		return PaymentIntentResult{}, err
	}
	return PaymentIntentResult{Episode: next, Event: event}, ledger.ErrInsufficientBudget
}
