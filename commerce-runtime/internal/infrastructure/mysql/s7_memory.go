package mysql

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/memory"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type MemoryRecordModel struct {
	MemoryID            string     `gorm:"column:memory_id;type:varchar(128);primaryKey"`
	Type                string     `gorm:"column:type;type:varchar(48);not null;uniqueIndex:uk_memory_identity"`
	Scope               string     `gorm:"column:scope;type:varchar(48);not null;uniqueIndex:uk_memory_identity"`
	RequesterDID        string     `gorm:"column:requester_did;type:varchar(128);not null;uniqueIndex:uk_memory_identity"`
	ParentSessionID     string     `gorm:"column:parent_session_id;type:varchar(128);not null;uniqueIndex:uk_memory_identity"`
	MerchantDID         string     `gorm:"column:merchant_did;type:varchar(128);not null;uniqueIndex:uk_memory_identity"`
	CapabilityID        string     `gorm:"column:capability_id;type:varchar(128);not null;uniqueIndex:uk_memory_identity"`
	CatalogVersion      string     `gorm:"column:catalog_version;type:varchar(64);not null;uniqueIndex:uk_memory_identity"`
	CatalogSnapshotHash string     `gorm:"column:catalog_snapshot_hash;type:char(71);not null"`
	Summary             string     `gorm:"column:summary;type:varchar(1024);not null"`
	StructuredFacts     []byte     `gorm:"column:structured_facts;type:json;not null"`
	SourceEpisodeIDs    []byte     `gorm:"column:source_episode_ids;type:json;not null"`
	SourceEventRefs     []byte     `gorm:"column:source_event_refs;type:json;not null"`
	SourceEvidenceRefs  []byte     `gorm:"column:source_evidence_refs;type:json"`
	ObservationCount    int        `gorm:"column:observation_count;not null"`
	Confidence          float64    `gorm:"column:confidence;not null"`
	FirstObservedAt     time.Time  `gorm:"column:first_observed_at;type:datetime(6);not null"`
	LastObservedAt      time.Time  `gorm:"column:last_observed_at;type:datetime(6);not null;index:idx_memory_last_observed"`
	ValidFrom           time.Time  `gorm:"column:valid_from;type:datetime(6);not null"`
	ValidUntil          *time.Time `gorm:"column:valid_until;type:datetime(6);index:idx_memory_valid_until"`
	CreatedAt           time.Time  `gorm:"column:created_at;type:datetime(6);not null"`
	UpdatedAt           time.Time  `gorm:"column:updated_at;type:datetime(6);not null"`
	FactsRef            string     `gorm:"column:facts_ref;type:varchar(255);not null"`
	PayloadHash         string     `gorm:"column:payload_hash;type:char(71);not null"`
}

func (MemoryRecordModel) TableName() string { return "memory_records" }

type MemoryObservationModel struct {
	ObservationID       string    `gorm:"column:observation_id;type:varchar(128);primaryKey"`
	MemoryID            string    `gorm:"column:memory_id;type:varchar(128);not null;index:idx_memory_observation_memory"`
	SourceEpisodeID     string    `gorm:"column:source_episode_id;type:varchar(128);not null;uniqueIndex:uk_memory_observation_identity"`
	SourceEventRef      string    `gorm:"column:source_event_ref;type:varchar(255);not null"`
	SourceEvidenceRefs  []byte    `gorm:"column:source_evidence_refs;type:json"`
	ObservationKind     string    `gorm:"column:observation_kind;type:varchar(64);not null;uniqueIndex:uk_memory_observation_identity"`
	Outcome             string    `gorm:"column:outcome;type:varchar(128)"`
	ObservedAt          time.Time `gorm:"column:observed_at;type:datetime(6);not null"`
	CatalogVersion      string    `gorm:"column:catalog_version;type:varchar(64)"`
	CatalogSnapshotHash string    `gorm:"column:catalog_snapshot_hash;type:char(71)"`
	PayloadHash         string    `gorm:"column:payload_hash;type:char(71);not null"`
}

func (MemoryObservationModel) TableName() string { return "memory_observations" }

func memoryRecordToModel(value *memory.MemoryRecord) (*MemoryRecordModel, error) {
	if value == nil || value.Validate() != nil {
		return nil, memory.ErrInvalidMemory
	}
	facts, err := json.Marshal(value.StructuredFacts)
	if err != nil {
		return nil, err
	}
	episodes, err := json.Marshal(value.SourceEpisodeIDs)
	if err != nil {
		return nil, err
	}
	events, err := json.Marshal(value.SourceEventRefs)
	if err != nil {
		return nil, err
	}
	evidence, err := json.Marshal(value.SourceEvidenceRefs)
	if err != nil {
		return nil, err
	}
	return &MemoryRecordModel{MemoryID: value.MemoryID, Type: string(value.Type), Scope: string(value.Scope), RequesterDID: value.RequesterDID, ParentSessionID: value.ParentSessionID, MerchantDID: value.MerchantDID, CapabilityID: value.CapabilityID, CatalogVersion: value.CatalogVersion, CatalogSnapshotHash: value.CatalogSnapshotHash, Summary: value.Summary, StructuredFacts: facts, SourceEpisodeIDs: episodes, SourceEventRefs: events, SourceEvidenceRefs: evidence, ObservationCount: value.ObservationCount, Confidence: value.Confidence, FirstObservedAt: value.FirstObservedAt, LastObservedAt: value.LastObservedAt, ValidFrom: value.ValidFrom, ValidUntil: value.ValidUntil, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, FactsRef: value.FactsRef, PayloadHash: value.PayloadHash}, nil
}

func modelToMemoryRecord(row MemoryRecordModel) (*memory.MemoryRecord, error) {
	value := &memory.MemoryRecord{MemoryID: row.MemoryID, Type: memory.MemoryType(row.Type), Scope: memory.MemoryScope(row.Scope), RequesterDID: row.RequesterDID, ParentSessionID: row.ParentSessionID, MerchantDID: row.MerchantDID, CapabilityID: row.CapabilityID, CatalogVersion: row.CatalogVersion, CatalogSnapshotHash: row.CatalogSnapshotHash, Summary: row.Summary, ObservationCount: row.ObservationCount, Confidence: row.Confidence, FirstObservedAt: row.FirstObservedAt, LastObservedAt: row.LastObservedAt, ValidFrom: row.ValidFrom, ValidUntil: row.ValidUntil, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, FactsRef: row.FactsRef, PayloadHash: row.PayloadHash}
	if err := json.Unmarshal(row.StructuredFacts, &value.StructuredFacts); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(row.SourceEpisodeIDs, &value.SourceEpisodeIDs); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(row.SourceEventRefs, &value.SourceEventRefs); err != nil {
		return nil, err
	}
	if len(row.SourceEvidenceRefs) > 0 {
		if err := json.Unmarshal(row.SourceEvidenceRefs, &value.SourceEvidenceRefs); err != nil {
			return nil, err
		}
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return value, nil
}

func memoryObservationToModel(value *memory.MemoryObservation) (*MemoryObservationModel, error) {
	if value == nil || value.Validate() != nil {
		return nil, memory.ErrInvalidObservation
	}
	refs, err := json.Marshal(value.SourceEvidenceRefs)
	if err != nil {
		return nil, err
	}
	return &MemoryObservationModel{ObservationID: value.ObservationID, MemoryID: value.MemoryID, SourceEpisodeID: value.SourceEpisodeID, SourceEventRef: value.SourceEventRef, SourceEvidenceRefs: refs, ObservationKind: value.ObservationKind, Outcome: value.Outcome, ObservedAt: value.ObservedAt, CatalogVersion: value.CatalogVersion, CatalogSnapshotHash: value.CatalogSnapshotHash, PayloadHash: value.PayloadHash}, nil
}

func modelToMemoryObservation(row MemoryObservationModel) (*memory.MemoryObservation, error) {
	value := &memory.MemoryObservation{ObservationID: row.ObservationID, MemoryID: row.MemoryID, SourceEpisodeID: row.SourceEpisodeID, SourceEventRef: row.SourceEventRef, ObservationKind: row.ObservationKind, Outcome: row.Outcome, ObservedAt: row.ObservedAt, CatalogVersion: row.CatalogVersion, CatalogSnapshotHash: row.CatalogSnapshotHash, PayloadHash: row.PayloadHash}
	if len(row.SourceEvidenceRefs) > 0 {
		if err := json.Unmarshal(row.SourceEvidenceRefs, &value.SourceEvidenceRefs); err != nil {
			return nil, err
		}
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return value, nil
}

func (s *Store) SaveObservationAndUpdateAggregate(ctx context.Context, value *memory.MemoryRecord, observation *memory.MemoryObservation, delta memory.OutcomeFacts) error {
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		lastErr = s.saveObservationAndUpdateAggregate(ctx, value, observation, delta)
		if !isRetryableMemoryWriteError(lastErr) {
			return lastErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if attempt < 4 {
			timer := time.NewTimer(time.Duration(attempt+1) * 10 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return lastErr
}

// isRetryableMemoryWriteError classifies database-level serialization failures
// separately from logical identity conflicts. The transaction is the
// correctness boundary; retrying a rolled-back transaction only helps it make
// progress after a concurrent writer, and does not move correctness into a
// process-local lock.
func isRetryableMemoryWriteError(err error) bool {
	if errors.Is(err, memory.ErrMemoryConflict) || errors.Is(err, memory.ErrObservationConflict) {
		return true
	}
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "deadlock") ||
		strings.Contains(message, "lock wait timeout") ||
		strings.Contains(message, "serialization failure") ||
		strings.Contains(message, "error 1213") ||
		strings.Contains(message, "error 1205")
}

func (s *Store) saveObservationAndUpdateAggregate(ctx context.Context, value *memory.MemoryRecord, observation *memory.MemoryObservation, delta memory.OutcomeFacts) error {
	if value == nil || observation == nil || observation.MemoryID != value.MemoryID {
		return memory.ErrInvalidMemory
	}
	if err := observation.Validate(); err != nil {
		return err
	}
	if err := delta.Validate(); err != nil {
		return err
	}
	recordModel, err := memoryRecordToModel(value)
	if err != nil {
		return err
	}
	observationModel, err := memoryObservationToModel(observation)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existingObservation MemoryObservationModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("observation_id = ?", observation.ObservationID).First(&existingObservation).Error; err == nil {
			if existingObservation.PayloadHash == observation.PayloadHash && existingObservation.MemoryID == observation.MemoryID {
				return nil
			}
			return memory.ErrObservationConflict
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var existingRow MemoryRecordModel
		lookup := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("memory_id = ?", value.MemoryID).First(&existingRow).Error
		if errors.Is(lookup, gorm.ErrRecordNotFound) {
			var identity MemoryRecordModel
			identityErr := tx.Where("type = ? AND scope = ? AND requester_did = ? AND parent_session_id = ? AND merchant_did = ? AND capability_id = ? AND catalog_version = ?", recordModel.Type, recordModel.Scope, recordModel.RequesterDID, recordModel.ParentSessionID, recordModel.MerchantDID, recordModel.CapabilityID, recordModel.CatalogVersion).First(&identity).Error
			if identityErr == nil && identity.MemoryID != value.MemoryID {
				return memory.ErrMemoryConflict
			} else if !errors.Is(identityErr, gorm.ErrRecordNotFound) && identityErr != nil {
				return identityErr
			}
			// The first observation is the aggregate's initial state. Keep the
			// create path equivalent to the update path so the aggregate cannot
			// silently omit the first delta when the row is first inserted.
			candidate := value.Clone()
			candidate.StructuredFacts = delta
			candidate.ObservationCount = 1
			candidate.Confidence = 0.5
			candidate.Summary = memory.Summarize(*candidate)
			if err := candidate.RefreshPayloadHash(); err != nil {
				return err
			}
			recordModel, err = memoryRecordToModel(candidate)
			if err != nil {
				return err
			}
			if err := tx.Create(recordModel).Error; err != nil {
				if isDuplicateKey(err) {
					return memory.ErrMemoryConflict
				}
				return err
			}
		} else if lookup != nil {
			return lookup
		} else {
			existing, err := modelToMemoryRecord(existingRow)
			if err != nil {
				return err
			}
			if !sameMemoryIdentity(existing, value) {
				return memory.ErrMemoryConflict
			}
			mergeMemoryRecord(existing, value, observation, delta)
			updated, err := memoryRecordToModel(existing)
			if err != nil {
				return err
			}
			if err := tx.Model(&MemoryRecordModel{}).Where("memory_id = ?", existing.MemoryID).Updates(map[string]any{
				"summary": updated.Summary, "structured_facts": updated.StructuredFacts, "source_episode_ids": updated.SourceEpisodeIDs, "source_event_refs": updated.SourceEventRefs, "source_evidence_refs": updated.SourceEvidenceRefs,
				"observation_count": updated.ObservationCount, "confidence": updated.Confidence, "last_observed_at": updated.LastObservedAt, "valid_until": updated.ValidUntil, "updated_at": updated.UpdatedAt, "payload_hash": updated.PayloadHash,
			}).Error; err != nil {
				return err
			}
		}
		if err := tx.Create(observationModel).Error; err != nil {
			if isDuplicateKey(err) {
				return memory.ErrObservationConflict
			}
			return err
		}
		return nil
	})
}

func (s *Store) GetMemory(ctx context.Context, id string) (*memory.MemoryRecord, error) {
	var row MemoryRecordModel
	if err := s.db.WithContext(ctx).Where("memory_id = ?", strings.TrimSpace(id)).First(&row).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, memory.ErrMemoryNotFound
	} else if err != nil {
		return nil, err
	}
	return modelToMemoryRecord(row)
}

func (s *Store) ListMemories(ctx context.Context, query memory.MemoryQuery) ([]*memory.MemoryRecord, error) {
	return s.listMemories(ctx, query, false)
}

func (s *Store) Retrieve(ctx context.Context, query memory.MemoryQuery) ([]*memory.MemoryRecord, error) {
	return s.SearchMemories(ctx, query)
}

func (s *Store) SearchMemories(ctx context.Context, query memory.MemoryQuery) ([]*memory.MemoryRecord, error) {
	return s.listMemories(ctx, query, true)
}

func (s *Store) listMemories(ctx context.Context, query memory.MemoryQuery, lexical bool) ([]*memory.MemoryRecord, error) {
	var rows []MemoryRecordModel
	if err := s.db.WithContext(ctx).Order("last_observed_at DESC, memory_id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	now := query.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	result := make([]*memory.MemoryRecord, 0, len(rows))
	for _, row := range rows {
		value, err := modelToMemoryRecord(row)
		if err != nil {
			return nil, err
		}
		if value.ValidUntil != nil && !now.Before(*value.ValidUntil) || !memoryQueryMatches(value, query) {
			continue
		}
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

func sameMemoryIdentity(left, right *memory.MemoryRecord) bool {
	return left.MemoryID == right.MemoryID && left.Type == right.Type && left.Scope == right.Scope && left.RequesterDID == right.RequesterDID && left.ParentSessionID == right.ParentSessionID && left.MerchantDID == right.MerchantDID && left.CapabilityID == right.CapabilityID && left.CatalogVersion == right.CatalogVersion
}

func mergeMemoryRecord(value, proposed *memory.MemoryRecord, observation *memory.MemoryObservation, delta memory.OutcomeFacts) {
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
	value.Confidence = float64(value.ObservationCount) / float64(value.ObservationCount+1)
	if observation.ObservedAt.After(value.LastObservedAt) {
		value.LastObservedAt = observation.ObservedAt.UTC()
	}
	if proposed != nil && proposed.ValidUntil != nil {
		if value.ValidUntil == nil || proposed.ValidUntil.After(*value.ValidUntil) {
			expires := proposed.ValidUntil.UTC()
			value.ValidUntil = &expires
		}
	}
	value.UpdatedAt = value.LastObservedAt
	value.SourceEpisodeIDs = appendUnique(value.SourceEpisodeIDs, observation.SourceEpisodeID)
	value.SourceEventRefs = appendUnique(value.SourceEventRefs, observation.SourceEventRef)
	value.SourceEvidenceRefs = appendUnique(value.SourceEvidenceRefs, observation.SourceEvidenceRefs...)
	value.Summary = memory.Summarize(*value)
	_ = value.RefreshPayloadHash()
}

func appendUnique(values []string, additions ...string) []string {
	for _, addition := range additions {
		found := false
		for _, value := range values {
			if value == addition {
				found = true
				break
			}
		}
		if !found && strings.TrimSpace(addition) != "" {
			values = append(values, addition)
		}
	}
	return values
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

func (s *Store) GetMemoryEpisode(ctx context.Context, id string) (memory.EpisodeView, error) {
	value, err := s.Get(ctx, id)
	if err != nil {
		return memory.EpisodeView{}, err
	}
	return mysqlEpisodeView(value), nil
}

func (s *Store) ListMemoryEpisodeEvents(ctx context.Context, id string) ([]memory.EventView, error) {
	values, err := s.ListByEpisode(ctx, id)
	if err != nil {
		return nil, err
	}
	result := make([]memory.EventView, 0, len(values))
	for _, value := range values {
		view := memory.EventView{EventID: value.EventID, Sequence: value.Sequence, OccurredAt: value.OccurredAt, Action: string(value.Action.Type), Observation: string(value.Observation.Type), ObservationCode: value.Observation.Code, FactsRef: value.Observation.FactsRef, PayloadHash: value.Observation.PayloadHash}
		if value.Decision.Target != nil {
			view.TargetMerchantDID = value.Decision.Target.MerchantDID
			view.CapabilityID = value.Decision.Target.CapabilityID
		}
		result = append(result, view)
	}
	return result, nil
}

func (s *Store) ListMemoryPaymentIntents(ctx context.Context, id string) ([]memory.PaymentView, error) {
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

func (s *Store) GetMemoryDeliveryArtifact(ctx context.Context, id string) (memory.DeliveryView, error) {
	value, err := s.GetDeliveryArtifact(ctx, id)
	if err != nil {
		return memory.DeliveryView{}, err
	}
	return memory.DeliveryView{DeliveryID: value.DeliveryID, EpisodeID: value.EpisodeID, InvocationID: value.InvocationID, MerchantDID: value.MerchantDID, CapabilityID: value.CapabilityID, Attempt: value.Attempt, ReceivedAt: value.ReceivedAt}, nil
}

func (s *Store) GetMemoryValidationEvidence(ctx context.Context, id string) (memory.ValidationView, error) {
	value, err := s.GetValidationEvidence(ctx, id)
	if err != nil {
		return memory.ValidationView{}, err
	}
	return memory.ValidationView{ValidationID: value.ValidationID, DeliveryID: value.DeliveryID, Valid: value.Valid, ReasonCode: value.ReasonCode, EvidenceRefs: append([]string(nil), value.EvidenceRefs...), CreatedAt: value.CreatedAt}, nil
}

func mysqlEpisodeView(value *episode.CommerceEpisode) memory.EpisodeView {
	return memory.EpisodeView{EpisodeID: value.EpisodeID, RequesterDID: value.RequesterDID, ParentSessionID: value.SessionID, State: string(value.State), TerminalReason: value.TerminalReason, SelectedMerchantDID: value.SelectedMerchantDID, SelectedCapabilityID: value.SelectedCapabilityID, SelectedCatalogVersion: value.SelectedCatalogVersion, SelectedCatalogHash: value.SelectedCatalogSnapshotHash, DeliveryRefs: append([]string(nil), value.DeliveryRefs...), ValidationRefs: append([]string(nil), value.ValidationEvidenceRefs...), AttemptedMerchants: append([]string(nil), value.AttemptedMerchants...), CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}

var _ memory.MemoryStore = (*Store)(nil)
var _ memory.EpisodeSource = (*Store)(nil)
