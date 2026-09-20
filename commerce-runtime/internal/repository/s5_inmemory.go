package repository

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"sort"

	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/recovery"
)

func (s *InMemoryStore) SaveRecoveryContext(ctx context.Context, value *recovery.RecoveryContext) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if value == nil || value.Validate() != nil {
		return recovery.ErrInvalidRecoveryContext
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.recoveryContexts[value.RecoveryID]; ok {
		if existing.EpisodeID != value.EpisodeID {
			return ErrRecoveryConflict
		}
		s.recoveryContexts[value.RecoveryID] = value.Clone()
		s.recoveryByEpisode[value.EpisodeID] = value.RecoveryID
		return nil
	}
	if existingID, ok := s.recoveryByEpisode[value.EpisodeID]; ok && existingID != value.RecoveryID {
		return ErrRecoveryConflict
	}
	s.recoveryContexts[value.RecoveryID] = value.Clone()
	s.recoveryByEpisode[value.EpisodeID] = value.RecoveryID
	return nil
}

func (s *InMemoryStore) GetRecoveryContext(ctx context.Context, id string) (*recovery.RecoveryContext, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.recoveryContexts[id]
	if !ok {
		return nil, ErrNotFound
	}
	return value.Clone(), nil
}
func (s *InMemoryStore) GetRecoveryContextByEpisode(ctx context.Context, episodeID string) (*recovery.RecoveryContext, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.recoveryByEpisode[episodeID]
	if !ok {
		return nil, ErrNotFound
	}
	return s.recoveryContexts[id].Clone(), nil
}

func (s *InMemoryStore) SaveParentApprovalRequest(ctx context.Context, value *recovery.ParentApprovalRequest) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if value == nil || value.Validate() != nil {
		return recovery.ErrInvalidParentRequest
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.parentApprovals[value.ApprovalID]; ok {
		if existing.PayloadHash == value.PayloadHash && existing.EpisodeID == value.EpisodeID {
			return nil
		}
		return ErrFactConflict
	}
	s.parentApprovals[value.ApprovalID] = value.Clone()
	return nil
}
func (s *InMemoryStore) GetParentApprovalRequest(ctx context.Context, id string) (*recovery.ParentApprovalRequest, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.parentApprovals[id]
	if !ok {
		return nil, ErrNotFound
	}
	return value.Clone(), nil
}

func (s *InMemoryStore) ListParentApprovalRequests(ctx context.Context, episodeID string) ([]*recovery.ParentApprovalRequest, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*recovery.ParentApprovalRequest, 0)
	for _, value := range s.parentApprovals {
		if value != nil && value.EpisodeID == episodeID {
			result = append(result, value.Clone())
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, nil
}
func (s *InMemoryStore) SaveParentDecision(ctx context.Context, value *recovery.ParentDecisionFact) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if value == nil || value.Validate() != nil {
		return recovery.ErrInvalidParentDecision
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.parentDecisions[value.ApprovalID]; ok {
		if existing.EpisodeID == value.EpisodeID && existing.Decision == value.Decision && existing.ActorRef == value.ActorRef && existing.FactsRef == value.FactsRef {
			return nil
		}
		return ErrParentDecisionConflict
	}
	s.parentDecisions[value.ApprovalID] = value.Clone()
	return nil
}
func (s *InMemoryStore) GetParentDecision(ctx context.Context, id string) (*recovery.ParentDecisionFact, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.parentDecisions[id]
	if !ok {
		return nil, ErrNotFound
	}
	return value.Clone(), nil
}
func (s *InMemoryStore) SaveBudgetAmendment(ctx context.Context, value *recovery.BudgetAmendment) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if value == nil || value.Validate() != nil {
		return recovery.ErrInvalidBudgetAmendment
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.budgetAmendments[value.AmendmentID]; ok {
		if reflect.DeepEqual(existing, value) {
			return nil
		}
		return ErrFactConflict
	}
	s.budgetAmendments[value.AmendmentID] = value.Clone()
	return nil
}
func (s *InMemoryStore) GetBudgetAmendment(ctx context.Context, id string) (*recovery.BudgetAmendment, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.budgetAmendments[id]
	if !ok {
		return nil, ErrNotFound
	}
	return value.Clone(), nil
}

func (s *InMemoryStore) CommitRecoveryTransition(ctx context.Context, transition RecoveryTransition) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if transition.NextEpisode == nil || transition.Event == nil || transition.RecoveryContext == nil || transition.EpisodeID == "" || transition.NextEpisode.EpisodeID != transition.EpisodeID || transition.Event.EpisodeID != transition.EpisodeID {
		return errors.New("invalid recovery transition")
	}
	if err := transition.NextEpisode.Validate(); err != nil {
		return err
	}
	if err := transition.Event.Validate(); err != nil {
		return err
	}
	if err := transition.RecoveryContext.Validate(); err != nil {
		return err
	}
	if transition.ExpectedEpisodeVersion == 0 || transition.Event.Sequence != transition.ExpectedEpisodeVersion || transition.NextEpisode.Version != transition.ExpectedEpisodeVersion+1 {
		return ErrVersionConflict
	}
	if transition.ParentApproval != nil {
		if err := transition.ParentApproval.Validate(); err != nil {
			return err
		}
	}
	if transition.CandidateSet != nil {
		if err := transition.CandidateSet.Validate(); err != nil {
			return err
		}
		if transition.CandidateSet.EpisodeID != transition.EpisodeID || transition.CandidateSet.RequestID != transition.NextEpisode.RequestID || transition.CandidateSet.Generation != transition.NextEpisode.DiscoveryGeneration {
			return ErrCandidateSetConflict
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.commitRecoveryLocked(transition)
}

func (s *InMemoryStore) commitRecoveryLocked(t RecoveryTransition) error {
	for _, existing := range s.events[t.EpisodeID] {
		if existing.Action.IdempotencyKey == t.Event.Action.IdempotencyKey {
			return ErrIdempotentReplay
		}
	}
	current, ok := s.episodes[t.EpisodeID]
	if !ok {
		return ErrNotFound
	}
	if current.Version != t.ExpectedEpisodeVersion || current.State != t.Event.StateBefore {
		return ErrVersionConflict
	}
	if current.ContractSnapshotHash != t.NextEpisode.ContractSnapshotHash || !bytes.Equal(current.ContractSnapshot, t.NextEpisode.ContractSnapshot) {
		return episode.ErrImmutableContract
	}
	if existing, ok := s.recoveryContexts[t.RecoveryContext.RecoveryID]; ok && existing.EpisodeID != t.RecoveryContext.EpisodeID {
		return ErrRecoveryConflict
	}
	if existingID, ok := s.recoveryByEpisode[t.EpisodeID]; ok && existingID != t.RecoveryContext.RecoveryID {
		// Recovery is one current durable context; updates replace the same id.
		return ErrRecoveryConflict
	}
	if t.CandidateSet != nil {
		if existing, ok := s.candidateSets[t.CandidateSet.CandidateSetID]; ok && existing.PayloadHash != t.CandidateSet.PayloadHash {
			return ErrCandidateSetConflict
		}
	}
	if t.ParentApproval != nil {
		if existing, ok := s.parentApprovals[t.ParentApproval.ApprovalID]; ok {
			if existing.PayloadHash != t.ParentApproval.PayloadHash || existing.EpisodeID != t.ParentApproval.EpisodeID {
				return ErrFactConflict
			}
		}
	}
	if len(s.events[t.EpisodeID])+1 != int(t.Event.Sequence) {
		return ErrEventSequenceConflict
	}
	s.episodes[t.EpisodeID] = t.NextEpisode.Clone()
	if t.CandidateSet != nil {
		if _, exists := s.candidateSets[t.CandidateSet.CandidateSetID]; !exists {
			s.candidateSets[t.CandidateSet.CandidateSetID] = t.CandidateSet.Clone()
		}
	}
	s.recoveryContexts[t.RecoveryContext.RecoveryID] = t.RecoveryContext.Clone()
	s.recoveryByEpisode[t.EpisodeID] = t.RecoveryContext.RecoveryID
	if t.ParentApproval != nil {
		if _, exists := s.parentApprovals[t.ParentApproval.ApprovalID]; !exists {
			s.parentApprovals[t.ParentApproval.ApprovalID] = t.ParentApproval.Clone()
		}
	}
	s.events[t.EpisodeID] = append(s.events[t.EpisodeID], t.Event.Clone())
	return nil
}

func (s *InMemoryStore) CommitParentDecision(ctx context.Context, t ParentDecisionTransition) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if t.NextEpisode == nil || t.Event == nil || t.Decision == nil || t.EpisodeID == "" || t.NextEpisode.EpisodeID != t.EpisodeID || t.Event.EpisodeID != t.EpisodeID {
		return errors.New("invalid parent decision transition")
	}
	if err := t.NextEpisode.Validate(); err != nil {
		return err
	}
	if err := t.Event.Validate(); err != nil {
		return err
	}
	if err := t.Decision.Validate(); err != nil {
		return err
	}
	if t.BudgetAmendment != nil {
		if err := t.BudgetAmendment.Validate(); err != nil {
			return err
		}
	}
	if t.RecoveryContext != nil {
		if err := t.RecoveryContext.Validate(); err != nil {
			return err
		}
	}
	if t.ExpectedEpisodeVersion == 0 || t.Event.Sequence != t.ExpectedEpisodeVersion || t.NextEpisode.Version != t.ExpectedEpisodeVersion+1 {
		return ErrVersionConflict
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.parentDecisions[t.Decision.ApprovalID]; ok {
		if existing.EpisodeID == t.Decision.EpisodeID && existing.Decision == t.Decision.Decision && existing.ActorRef == t.Decision.ActorRef && existing.FactsRef == t.Decision.FactsRef {
			return ErrIdempotentReplay
		}
		return ErrParentDecisionConflict
	}
	current, ok := s.episodes[t.EpisodeID]
	if !ok {
		return ErrNotFound
	}
	if current.Version != t.ExpectedEpisodeVersion || current.State != t.Event.StateBefore {
		return ErrVersionConflict
	}
	if len(s.events[t.EpisodeID])+1 != int(t.Event.Sequence) {
		return ErrEventSequenceConflict
	}
	s.episodes[t.EpisodeID] = t.NextEpisode.Clone()
	s.parentDecisions[t.Decision.ApprovalID] = t.Decision.Clone()
	if t.RecoveryContext != nil {
		s.recoveryContexts[t.RecoveryContext.RecoveryID] = t.RecoveryContext.Clone()
		s.recoveryByEpisode[t.EpisodeID] = t.RecoveryContext.RecoveryID
	}
	if t.BudgetAmendment != nil {
		s.budgetAmendments[t.BudgetAmendment.AmendmentID] = t.BudgetAmendment.Clone()
	}
	s.events[t.EpisodeID] = append(s.events[t.EpisodeID], t.Event.Clone())
	return nil
}
