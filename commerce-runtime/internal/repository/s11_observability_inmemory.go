package repository

import (
	"context"
	"reflect"
	"sort"
	"strings"

	"github.com/stablepay/commerce-runtime/internal/ledger"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

func (s *InMemoryStore) SaveDecisionOutcomeTrace(ctx context.Context, value *trace.DecisionOutcomeTrace) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if value == nil || value.Validate() != nil {
		return trace.ErrInvalidDecisionOutcomeTrace
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.decisionOutcomeTraces[value.DecisionOutcomeTraceID]; ok {
		if reflect.DeepEqual(existing, value) {
			return nil
		}
		return ErrDecisionOutcomeConflict
	}
	copy := *value
	s.decisionOutcomeTraces[value.DecisionOutcomeTraceID] = &copy
	return nil
}

func (s *InMemoryStore) ListDecisionOutcomeTraces(ctx context.Context, episodeID string) ([]*trace.DecisionOutcomeTrace, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*trace.DecisionOutcomeTrace, 0)
	for _, value := range s.decisionOutcomeTraces {
		if value != nil && value.EpisodeID == strings.TrimSpace(episodeID) {
			if err := value.Validate(); err != nil {
				return nil, err
			}
			copy := *value
			result = append(result, &copy)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if !result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].CreatedAt.Before(result[j].CreatedAt)
		}
		return result[i].DecisionOutcomeTraceID < result[j].DecisionOutcomeTraceID
	})
	return result, nil
}

func (s *InMemoryStore) SnapshotSideEffects(ctx context.Context, episodeID string) (trace.GuardSideEffectSnapshot, error) {
	if err := contextErr(ctx); err != nil {
		return trace.GuardSideEffectSnapshot{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	episodeID = strings.TrimSpace(episodeID)
	snapshot := trace.GuardSideEffectSnapshot{}
	for _, value := range s.intents {
		if value != nil && value.EpisodeID == episodeID {
			snapshot.PaymentIntentCount++
		}
	}
	for _, entries := range s.ledger {
		for _, value := range entries {
			if value != nil && value.EpisodeID == episodeID && value.Type == ledger.EntryPaymentSettled {
				snapshot.SettlementCount++
			}
		}
	}
	for _, value := range s.merchantInvocations {
		if value != nil && value.EpisodeID == episodeID {
			snapshot.MerchantInvocationCount++
		}
	}
	for _, value := range s.deliveryArtifacts {
		if value != nil && value.EpisodeID == episodeID {
			snapshot.DeliveryArtifactCount++
		}
	}
	for _, value := range s.budgetAmendments {
		if value != nil && value.EpisodeID == episodeID {
			snapshot.BudgetAmendmentCount++
		}
	}
	for _, value := range s.parentDecisions {
		if value != nil && value.EpisodeID == episodeID {
			snapshot.ParentDecisionCount++
		}
	}
	return snapshot, nil
}
