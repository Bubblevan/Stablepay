package repository

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/memory"
)

func (s *InMemoryStore) ensureMemoryMaps() {
	if s.memoryRecords == nil {
		s.memoryRecords = make(map[string]*memory.MemoryRecord)
	}
	if s.memoryObservations == nil {
		s.memoryObservations = make(map[string]*memory.MemoryObservation)
	}
}

func (s *InMemoryStore) SaveObservationAndUpdateAggregate(ctx context.Context, value *memory.MemoryRecord, observation *memory.MemoryObservation, delta memory.OutcomeFacts) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if value == nil || observation == nil || observation.MemoryID != value.MemoryID || delta.Validate() != nil {
		return memory.ErrInvalidMemory
	}
	if err := observation.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureMemoryMaps()
	if existing, ok := s.memoryObservations[observation.ObservationID]; ok {
		if existing.PayloadHash == observation.PayloadHash && existing.MemoryID == observation.MemoryID {
			return nil
		}
		return memory.ErrObservationConflict
	}
	if existing, ok := s.memoryRecords[value.MemoryID]; ok {
		if !sameMemoryIdentity(existing, value) {
			return memory.ErrMemoryConflict
		}
		applyMemoryDelta(existing, value, observation, delta)
		if err := existing.RefreshPayloadHash(); err != nil {
			return err
		}
		if err := existing.Validate(); err != nil {
			return err
		}
	} else {
		candidate := value.Clone()
		candidate.StructuredFacts = delta
		candidate.ObservationCount = 1
		candidate.Confidence = confidenceFor(1)
		candidate.Summary = memory.Summarize(*candidate)
		if err := candidate.RefreshPayloadHash(); err != nil {
			return err
		}
		if err := candidate.Validate(); err != nil {
			return err
		}
		s.memoryRecords[candidate.MemoryID] = candidate
	}
	s.memoryObservations[observation.ObservationID] = observation.Clone()
	return nil
}

func (s *InMemoryStore) GetMemory(ctx context.Context, id string) (*memory.MemoryRecord, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.memoryRecords[strings.TrimSpace(id)]
	if !ok {
		return nil, memory.ErrMemoryNotFound
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return value.Clone(), nil
}

func (s *InMemoryStore) ListMemories(ctx context.Context, query memory.MemoryQuery) ([]*memory.MemoryRecord, error) {
	return s.searchMemories(ctx, query, false)
}

func (s *InMemoryStore) Retrieve(ctx context.Context, query memory.MemoryQuery) ([]*memory.MemoryRecord, error) {
	return s.SearchMemories(ctx, query)
}

func (s *InMemoryStore) SearchMemories(ctx context.Context, query memory.MemoryQuery) ([]*memory.MemoryRecord, error) {
	return s.searchMemories(ctx, query, true)
}

func (s *InMemoryStore) searchMemories(ctx context.Context, query memory.MemoryQuery, lexical bool) ([]*memory.MemoryRecord, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := query.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	result := make([]*memory.MemoryRecord, 0, len(s.memoryRecords))
	for _, source := range s.memoryRecords {
		value := source.Clone()
		if value.ValidUntil != nil && !now.Before(*value.ValidUntil) {
			continue
		}
		if !memoryQueryMatches(value, query) {
			continue
		}
		// SearchMemories currently uses the same structured retrieval surface as
		// ListMemories. A future lexical score must remain secondary to scope and
		// merchant/capability filters.
		_ = lexical
		value.Applicability = memory.ApplicabilityCurrent
		if query.CatalogVersion != "" && value.CatalogVersion != "" && value.CatalogVersion != query.CatalogVersion {
			value.Applicability = memory.ApplicabilityHistoricalVersion
		}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return memoryRank(result[i], query) < memoryRank(result[j], query) })
	if query.Limit > 0 && len(result) > query.Limit {
		result = result[:query.Limit]
	}
	return result, nil
}

func memoryQueryMatches(value *memory.MemoryRecord, query memory.MemoryQuery) bool {
	switch value.Scope {
	case memory.ScopeRequester:
		return query.RequesterDID != "" && value.RequesterDID == query.RequesterDID
	case memory.ScopeParentSession:
		return query.ParentSessionID != "" && value.ParentSessionID == query.ParentSessionID && (query.RequesterDID == "" || value.RequesterDID == query.RequesterDID)
	case memory.ScopeMerchant:
		return query.MerchantDID != "" && value.MerchantDID == query.MerchantDID
	case memory.ScopeMerchantCapability:
		return query.MerchantDID != "" && query.CapabilityID != "" && value.MerchantDID == query.MerchantDID && value.CapabilityID == strings.ToLower(query.CapabilityID)
	default:
		return false
	}
}

func memoryRank(value *memory.MemoryRecord, query memory.MemoryQuery) string {
	score := "9"
	if value.Scope == memory.ScopeMerchantCapability && value.MerchantDID == query.MerchantDID && value.CapabilityID == strings.ToLower(query.CapabilityID) {
		score = "1"
	} else if value.Scope == memory.ScopeMerchant && value.MerchantDID == query.MerchantDID {
		score = "2"
	} else if value.Scope == memory.ScopeParentSession && value.ParentSessionID == query.ParentSessionID {
		score = "3"
	} else if value.Scope == memory.ScopeRequester && value.RequesterDID == query.RequesterDID {
		score = "4"
	}
	return score + "|" + value.LastObservedAt.UTC().Format(time.RFC3339Nano) + "|" + value.MemoryID
}

func sameMemoryIdentity(left, right *memory.MemoryRecord) bool {
	return left.MemoryID == right.MemoryID && left.Type == right.Type && left.Scope == right.Scope && left.RequesterDID == right.RequesterDID && left.ParentSessionID == right.ParentSessionID && left.MerchantDID == right.MerchantDID && left.CapabilityID == right.CapabilityID && left.CatalogVersion == right.CatalogVersion
}

func applyMemoryDelta(value, proposed *memory.MemoryRecord, observation *memory.MemoryObservation, delta memory.OutcomeFacts) {
	value.StructuredFacts.AttemptCount += delta.AttemptCount
	value.StructuredFacts.FulfilledCount += delta.FulfilledCount
	value.StructuredFacts.DeliveryValidCount += delta.DeliveryValidCount
	value.StructuredFacts.DeliveryInvalidCount += delta.DeliveryInvalidCount
	value.StructuredFacts.PaymentFailedCount += delta.PaymentFailedCount
	value.StructuredFacts.MerchantErrorCount += delta.MerchantErrorCount
	value.StructuredFacts.SwitchAwayCount += delta.SwitchAwayCount
	value.StructuredFacts.RecoveryAttemptCount += delta.RecoveryAttemptCount
	value.StructuredFacts.RecoverySuccessCount += delta.RecoverySuccessCount
	value.StructuredFacts.ParentApprovalCount += delta.ParentApprovalCount
	value.StructuredFacts.ParentDenialCount += delta.ParentDenialCount
	if delta.LastOutcome != "" {
		if delta.LastOutcome == "DELIVERY_VALID" {
			value.StructuredFacts.RecentFailureStreak = 0
		} else if delta.LastOutcome == "DELIVERY_INVALID" {
			value.StructuredFacts.RecentFailureStreak += delta.RecentFailureStreak
		}
		if value.StructuredFacts.LastOutcomeAt.IsZero() || delta.LastOutcomeAt.After(value.StructuredFacts.LastOutcomeAt) {
			value.StructuredFacts.LastOutcome = delta.LastOutcome
			value.StructuredFacts.LastOutcomeAt = delta.LastOutcomeAt
		}
	}
	if delta.RecoveryAction != "" {
		value.StructuredFacts.RecoveryAction = delta.RecoveryAction
	}
	if delta.PreferenceKey != "" {
		value.StructuredFacts.PreferenceKey = delta.PreferenceKey
		value.StructuredFacts.PreferenceValue = delta.PreferenceValue
	}
	value.ObservationCount++
	value.Confidence = confidenceFor(value.ObservationCount)
	value.LastObservedAt = maxTime(value.LastObservedAt, observation.ObservedAt)
	value.UpdatedAt = maxTime(value.UpdatedAt, observation.ObservedAt)
	if proposed != nil && proposed.ValidUntil != nil {
		if value.ValidUntil == nil || proposed.ValidUntil.After(*value.ValidUntil) {
			expires := proposed.ValidUntil.UTC()
			value.ValidUntil = &expires
		}
	}
	value.SourceEpisodeIDs = appendUniqueString(value.SourceEpisodeIDs, observation.SourceEpisodeID)
	value.SourceEventRefs = appendUniqueString(value.SourceEventRefs, observation.SourceEventRef)
	value.SourceEvidenceRefs = appendUniqueStrings(value.SourceEvidenceRefs, observation.SourceEvidenceRefs...)
	value.Summary = memory.Summarize(*value)
}

func confidenceFor(observations int) float64 {
	if observations <= 0 {
		return 0
	}
	return float64(observations) / float64(observations+1)
}

func maxTime(left, right time.Time) time.Time {
	if right.After(left) {
		return right.UTC()
	}
	return left.UTC()
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func appendUniqueStrings(values []string, additions ...string) []string {
	for _, addition := range additions {
		values = appendUniqueString(values, addition)
	}
	return values
}

// EpisodeSource implementation. These adapters intentionally expose copies
// of authoritative facts and never expose mutable repository state.
func (s *InMemoryStore) GetMemoryEpisode(ctx context.Context, id string) (memory.EpisodeView, error) {
	value, err := s.Get(ctx, id)
	if err != nil {
		return memory.EpisodeView{}, err
	}
	return episodeView(value), nil
}

func (s *InMemoryStore) ListMemoryEpisodeEvents(ctx context.Context, id string) ([]memory.EventView, error) {
	values, err := s.ListByEpisode(ctx, id)
	if err != nil {
		return nil, err
	}
	result := make([]memory.EventView, 0, len(values))
	for _, value := range values {
		result = append(result, eventView(value))
	}
	return result, nil
}

func (s *InMemoryStore) ListMemoryPaymentIntents(ctx context.Context, id string) ([]memory.PaymentView, error) {
	values, err := s.ListPaymentIntents(ctx, id)
	if err != nil {
		return nil, err
	}
	result := make([]memory.PaymentView, 0, len(values))
	for _, value := range values {
		result = append(result, memory.PaymentView{IntentID: value.IntentID, MerchantDID: value.MerchantDID, CapabilityID: value.CapabilityID, Status: string(value.Status), UpdatedAt: value.UpdatedAt})
	}
	return result, nil
}

func (s *InMemoryStore) GetMemoryDeliveryArtifact(ctx context.Context, id string) (memory.DeliveryView, error) {
	value, err := s.GetDeliveryArtifact(ctx, id)
	if err != nil {
		return memory.DeliveryView{}, err
	}
	return memory.DeliveryView{DeliveryID: value.DeliveryID, EpisodeID: value.EpisodeID, InvocationID: value.InvocationID, MerchantDID: value.MerchantDID, CapabilityID: value.CapabilityID, Attempt: value.Attempt, ReceivedAt: value.ReceivedAt}, nil
}

func (s *InMemoryStore) GetMemoryValidationEvidence(ctx context.Context, id string) (memory.ValidationView, error) {
	value, err := s.GetValidationEvidence(ctx, id)
	if err != nil {
		return memory.ValidationView{}, err
	}
	return memory.ValidationView{ValidationID: value.ValidationID, DeliveryID: value.DeliveryID, Valid: value.Valid, ReasonCode: value.ReasonCode, EvidenceRefs: append([]string(nil), value.EvidenceRefs...), CreatedAt: value.CreatedAt}, nil
}

func episodeView(value *episode.CommerceEpisode) memory.EpisodeView {
	return memory.EpisodeView{EpisodeID: value.EpisodeID, RequesterDID: value.RequesterDID, ParentSessionID: value.SessionID, State: string(value.State), TerminalReason: value.TerminalReason, SelectedMerchantDID: value.SelectedMerchantDID, SelectedCapabilityID: value.SelectedCapabilityID, SelectedCatalogVersion: value.SelectedCatalogVersion, SelectedCatalogHash: value.SelectedCatalogSnapshotHash, DeliveryRefs: append([]string(nil), value.DeliveryRefs...), ValidationRefs: append([]string(nil), value.ValidationEvidenceRefs...), AttemptedMerchants: append([]string(nil), value.AttemptedMerchants...), CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}

func eventView(value *episode.EpisodeEvent) memory.EventView {
	result := memory.EventView{EventID: value.EventID, Sequence: value.Sequence, OccurredAt: value.OccurredAt, Action: string(value.Action.Type), Observation: string(value.Observation.Type), ObservationCode: value.Observation.Code, FactsRef: value.Observation.FactsRef, PayloadHash: value.Observation.PayloadHash}
	if value.Decision.Target != nil {
		result.TargetMerchantDID = value.Decision.Target.MerchantDID
		result.CapabilityID = value.Decision.Target.CapabilityID
	}
	return result
}

var _ memory.MemoryStore = (*InMemoryStore)(nil)
var _ memory.EpisodeSource = (*InMemoryStore)(nil)
