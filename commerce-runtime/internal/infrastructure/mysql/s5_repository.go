package mysql

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"time"

	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/recovery"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RecoveryContextModel struct {
	RecoveryID           string    `gorm:"column:recovery_id;type:varchar(128);primaryKey"`
	EpisodeID            string    `gorm:"column:episode_id;type:varchar(128);not null;uniqueIndex:uk_recovery_episode"`
	TriggerEventID       string    `gorm:"column:trigger_event_id;type:varchar(128);not null"`
	ReasonCode           string    `gorm:"column:reason_code;type:varchar(64);not null"`
	CurrentMerchantDID   string    `gorm:"column:current_merchant_did;type:varchar(128)"`
	CurrentCapabilityID  string    `gorm:"column:current_capability_id;type:varchar(128)"`
	CandidateSetID       string    `gorm:"column:candidate_set_id;type:varchar(128)"`
	AttemptedMerchants   []byte    `gorm:"column:attempted_merchants;type:json;not null"`
	PaymentIntentIDs     []byte    `gorm:"column:payment_intent_ids;type:json;not null"`
	SettledMinor         int64     `gorm:"column:settled_minor;not null"`
	RefundedMinor        int64     `gorm:"column:refunded_minor;not null"`
	ConsumedMinor        int64     `gorm:"column:consumed_minor;not null"`
	AvailableMinor       int64     `gorm:"column:available_minor;not null"`
	SunkCostMinor        int64     `gorm:"column:sunk_cost_minor;not null"`
	DeliveryAttemptCount int       `gorm:"column:delivery_attempt_count;not null"`
	RetryCount           int       `gorm:"column:retry_count;not null"`
	DeadlineAt           time.Time `gorm:"column:deadline_at;not null"`
	CreatedAt            time.Time `gorm:"column:created_at;not null"`
	FactsRef             string    `gorm:"column:facts_ref;type:varchar(255);not null"`
	PayloadHash          string    `gorm:"column:payload_hash;type:char(71);not null"`
}

func (RecoveryContextModel) TableName() string { return "recovery_contexts" }

type ParentApprovalRequestModel struct {
	ApprovalID                   string    `gorm:"column:approval_id;type:varchar(128);primaryKey"`
	EpisodeID                    string    `gorm:"column:episode_id;type:varchar(128);not null;index:idx_parent_approval_episode"`
	RecoveryID                   string    `gorm:"column:recovery_id;type:varchar(128);not null"`
	ReasonCode                   string    `gorm:"column:reason_code;type:varchar(64);not null"`
	RequestedAction              string    `gorm:"column:requested_action;type:varchar(64);not null"`
	ApprovalScope                string    `gorm:"column:approval_scope;type:varchar(64);not null"`
	CurrentBudgetMinor           int64     `gorm:"column:current_budget_minor;not null"`
	ConsumedMinor                int64     `gorm:"column:consumed_minor;not null"`
	AvailableMinor               int64     `gorm:"column:available_minor;not null"`
	SunkCostMinor                int64     `gorm:"column:sunk_cost_minor;not null"`
	CurrentMerchantDID           string    `gorm:"column:current_merchant_did;type:varchar(128)"`
	CandidateSetID               string    `gorm:"column:candidate_set_id;type:varchar(128)"`
	CandidateMerchantDID         string    `gorm:"column:candidate_merchant_did;type:varchar(128)"`
	CandidateCapabilityID        string    `gorm:"column:candidate_capability_id;type:varchar(128)"`
	RequestedBudgetIncreaseMinor int64     `gorm:"column:requested_budget_increase_minor;not null"`
	ExpiresAt                    time.Time `gorm:"column:expires_at;not null"`
	FactsRef                     string    `gorm:"column:facts_ref;type:varchar(255);not null"`
	PayloadHash                  string    `gorm:"column:payload_hash;type:char(71);not null"`
	CreatedAt                    time.Time `gorm:"column:created_at;not null"`
}

func (ParentApprovalRequestModel) TableName() string { return "parent_approval_requests" }

type ParentDecisionFactModel struct {
	ApprovalID  string    `gorm:"column:approval_id;type:varchar(128);primaryKey"`
	EpisodeID   string    `gorm:"column:episode_id;type:varchar(128);not null"`
	Decision    string    `gorm:"column:decision;type:varchar(16);not null"`
	ActorRef    string    `gorm:"column:actor_ref;type:varchar(255);not null"`
	OccurredAt  time.Time `gorm:"column:occurred_at;not null"`
	FactsRef    string    `gorm:"column:facts_ref;type:varchar(255);not null"`
	PayloadHash string    `gorm:"column:payload_hash;type:char(71);not null"`
}

func (ParentDecisionFactModel) TableName() string { return "parent_decisions" }

type BudgetAmendmentModel struct {
	AmendmentID   string    `gorm:"column:amendment_id;type:varchar(128);primaryKey"`
	EpisodeID     string    `gorm:"column:episode_id;type:varchar(128);not null;index:idx_budget_amendment_episode"`
	OldLimitMinor int64     `gorm:"column:old_limit_minor;not null"`
	NewLimitMinor int64     `gorm:"column:new_limit_minor;not null"`
	DeltaMinor    int64     `gorm:"column:delta_minor;not null"`
	ApprovedBy    string    `gorm:"column:approved_by;type:varchar(255);not null"`
	ApprovalRef   string    `gorm:"column:approval_ref;type:varchar(128);not null"`
	OccurredAt    time.Time `gorm:"column:occurred_at;not null"`
	FactsRef      string    `gorm:"column:facts_ref;type:varchar(255);not null"`
	PayloadHash   string    `gorm:"column:payload_hash;type:char(71);not null"`
}

func (BudgetAmendmentModel) TableName() string { return "budget_amendments" }

func recoveryContextToModel(v *recovery.RecoveryContext) (*RecoveryContextModel, error) {
	if v == nil || v.Validate() != nil {
		return nil, recovery.ErrInvalidRecoveryContext
	}
	a, e := json.Marshal(v.AttemptedMerchants)
	if e != nil {
		return nil, e
	}
	p, e := json.Marshal(v.PaymentIntentIDs)
	if e != nil {
		return nil, e
	}
	return &RecoveryContextModel{RecoveryID: v.RecoveryID, EpisodeID: v.EpisodeID, TriggerEventID: v.TriggerEventID, ReasonCode: string(v.ReasonCode), CurrentMerchantDID: v.CurrentMerchantDID, CurrentCapabilityID: v.CurrentCapabilityID, CandidateSetID: v.CandidateSetID, AttemptedMerchants: a, PaymentIntentIDs: p, SettledMinor: v.SettledMinor, RefundedMinor: v.RefundedMinor, ConsumedMinor: v.ConsumedMinor, AvailableMinor: v.AvailableMinor, SunkCostMinor: v.SunkCostMinor, DeliveryAttemptCount: v.DeliveryAttemptCount, RetryCount: v.RetryCount, DeadlineAt: v.DeadlineAt, CreatedAt: v.CreatedAt, FactsRef: v.FactsRef, PayloadHash: v.PayloadHash}, nil
}
func modelToRecoveryContext(m RecoveryContextModel) (*recovery.RecoveryContext, error) {
	v := &recovery.RecoveryContext{RecoveryID: m.RecoveryID, EpisodeID: m.EpisodeID, TriggerEventID: m.TriggerEventID, ReasonCode: recovery.ReasonCode(m.ReasonCode), CurrentMerchantDID: m.CurrentMerchantDID, CurrentCapabilityID: m.CurrentCapabilityID, CandidateSetID: m.CandidateSetID, SettledMinor: m.SettledMinor, RefundedMinor: m.RefundedMinor, ConsumedMinor: m.ConsumedMinor, AvailableMinor: m.AvailableMinor, SunkCostMinor: m.SunkCostMinor, DeliveryAttemptCount: m.DeliveryAttemptCount, RetryCount: m.RetryCount, DeadlineAt: m.DeadlineAt, CreatedAt: m.CreatedAt, FactsRef: m.FactsRef, PayloadHash: m.PayloadHash}
	if len(m.AttemptedMerchants) > 0 {
		if e := json.Unmarshal(m.AttemptedMerchants, &v.AttemptedMerchants); e != nil {
			return nil, e
		}
	}
	if len(m.PaymentIntentIDs) > 0 {
		if e := json.Unmarshal(m.PaymentIntentIDs, &v.PaymentIntentIDs); e != nil {
			return nil, e
		}
	}
	if e := v.Validate(); e != nil {
		return nil, e
	}
	return v, nil
}
func parentApprovalToModel(v *recovery.ParentApprovalRequest) (*ParentApprovalRequestModel, error) {
	if v == nil || v.Validate() != nil {
		return nil, recovery.ErrInvalidParentRequest
	}
	return &ParentApprovalRequestModel{ApprovalID: v.ApprovalID, EpisodeID: v.EpisodeID, RecoveryID: v.RecoveryID, ReasonCode: string(v.ReasonCode), RequestedAction: v.RequestedAction, ApprovalScope: string(v.ApprovalScope), CurrentBudgetMinor: v.CurrentBudgetMinor, ConsumedMinor: v.ConsumedMinor, AvailableMinor: v.AvailableMinor, SunkCostMinor: v.SunkCostMinor, CurrentMerchantDID: v.CurrentMerchantDID, CandidateSetID: v.CandidateSetID, CandidateMerchantDID: v.CandidateMerchantDID, CandidateCapabilityID: v.CandidateCapabilityID, RequestedBudgetIncreaseMinor: v.RequestedBudgetIncreaseMinor, ExpiresAt: v.ExpiresAt, FactsRef: v.FactsRef, PayloadHash: v.PayloadHash, CreatedAt: v.CreatedAt}, nil
}
func modelToParentApproval(m ParentApprovalRequestModel) (*recovery.ParentApprovalRequest, error) {
	v := &recovery.ParentApprovalRequest{ApprovalID: m.ApprovalID, EpisodeID: m.EpisodeID, RecoveryID: m.RecoveryID, ReasonCode: recovery.ReasonCode(m.ReasonCode), RequestedAction: m.RequestedAction, ApprovalScope: recovery.ApprovalScope(m.ApprovalScope), CurrentBudgetMinor: m.CurrentBudgetMinor, ConsumedMinor: m.ConsumedMinor, AvailableMinor: m.AvailableMinor, SunkCostMinor: m.SunkCostMinor, CurrentMerchantDID: m.CurrentMerchantDID, CandidateSetID: m.CandidateSetID, CandidateMerchantDID: m.CandidateMerchantDID, CandidateCapabilityID: m.CandidateCapabilityID, RequestedBudgetIncreaseMinor: m.RequestedBudgetIncreaseMinor, ExpiresAt: m.ExpiresAt, FactsRef: m.FactsRef, PayloadHash: m.PayloadHash, CreatedAt: m.CreatedAt}
	if e := v.Validate(); e != nil {
		return nil, e
	}
	return v, nil
}
func parentDecisionToModel(v *recovery.ParentDecisionFact) (*ParentDecisionFactModel, error) {
	if v == nil || v.Validate() != nil {
		return nil, recovery.ErrInvalidParentDecision
	}
	return &ParentDecisionFactModel{ApprovalID: v.ApprovalID, EpisodeID: v.EpisodeID, Decision: string(v.Decision), ActorRef: v.ActorRef, OccurredAt: v.OccurredAt, FactsRef: v.FactsRef, PayloadHash: v.PayloadHash}, nil
}
func modelToParentDecision(m ParentDecisionFactModel) (*recovery.ParentDecisionFact, error) {
	v := &recovery.ParentDecisionFact{ApprovalID: m.ApprovalID, EpisodeID: m.EpisodeID, Decision: recovery.Decision(m.Decision), ActorRef: m.ActorRef, OccurredAt: m.OccurredAt, FactsRef: m.FactsRef, PayloadHash: m.PayloadHash}
	if e := v.Validate(); e != nil {
		return nil, e
	}
	return v, nil
}
func amendmentToModel(v *recovery.BudgetAmendment) (*BudgetAmendmentModel, error) {
	if v == nil || v.Validate() != nil {
		return nil, recovery.ErrInvalidBudgetAmendment
	}
	return &BudgetAmendmentModel{AmendmentID: v.AmendmentID, EpisodeID: v.EpisodeID, OldLimitMinor: v.OldLimitMinor, NewLimitMinor: v.NewLimitMinor, DeltaMinor: v.DeltaMinor, ApprovedBy: v.ApprovedBy, ApprovalRef: v.ApprovalRef, OccurredAt: v.OccurredAt, FactsRef: v.FactsRef, PayloadHash: v.PayloadHash}, nil
}
func modelToAmendment(m BudgetAmendmentModel) (*recovery.BudgetAmendment, error) {
	v := &recovery.BudgetAmendment{AmendmentID: m.AmendmentID, EpisodeID: m.EpisodeID, OldLimitMinor: m.OldLimitMinor, NewLimitMinor: m.NewLimitMinor, DeltaMinor: m.DeltaMinor, ApprovedBy: m.ApprovedBy, ApprovalRef: m.ApprovalRef, OccurredAt: m.OccurredAt, FactsRef: m.FactsRef, PayloadHash: m.PayloadHash}
	if e := v.Validate(); e != nil {
		return nil, e
	}
	return v, nil
}

func (s *Store) SaveRecoveryContext(ctx context.Context, v *recovery.RecoveryContext) error {
	m, e := recoveryContextToModel(v)
	if e != nil {
		return e
	}
	var x RecoveryContextModel
	if q := s.db.WithContext(ctx).Where("recovery_id = ?", m.RecoveryID).First(&x).Error; q == nil {
		old, _ := modelToRecoveryContext(x)
		if old.EpisodeID != v.EpisodeID {
			return repository.ErrRecoveryConflict
		}
		return s.db.WithContext(ctx).Model(&RecoveryContextModel{}).Where("recovery_id = ?", m.RecoveryID).Updates(m).Error
	} else if !errors.Is(q, gorm.ErrRecordNotFound) {
		return q
	}
	return s.db.WithContext(ctx).Create(m).Error
}
func (s *Store) GetRecoveryContext(ctx context.Context, id string) (*recovery.RecoveryContext, error) {
	var m RecoveryContextModel
	if e := s.db.WithContext(ctx).Where("recovery_id = ?", id).First(&m).Error; errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, repository.ErrNotFound
	} else if e != nil {
		return nil, e
	}
	return modelToRecoveryContext(m)
}
func (s *Store) GetRecoveryContextByEpisode(ctx context.Context, id string) (*recovery.RecoveryContext, error) {
	var m RecoveryContextModel
	if e := s.db.WithContext(ctx).Where("episode_id = ?", id).First(&m).Error; errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, repository.ErrNotFound
	} else if e != nil {
		return nil, e
	}
	return modelToRecoveryContext(m)
}
func (s *Store) SaveParentApprovalRequest(ctx context.Context, v *recovery.ParentApprovalRequest) error {
	m, e := parentApprovalToModel(v)
	if e != nil {
		return e
	}
	var x ParentApprovalRequestModel
	if q := s.db.WithContext(ctx).Where("approval_id = ?", m.ApprovalID).First(&x).Error; q == nil {
		old, _ := modelToParentApproval(x)
		if old != nil && old.EpisodeID == v.EpisodeID && old.PayloadHash == v.PayloadHash {
			return nil
		}
		return repository.ErrFactConflict
	} else if !errors.Is(q, gorm.ErrRecordNotFound) {
		return q
	}
	return s.db.WithContext(ctx).Create(m).Error
}
func (s *Store) GetParentApprovalRequest(ctx context.Context, id string) (*recovery.ParentApprovalRequest, error) {
	var m ParentApprovalRequestModel
	if e := s.db.WithContext(ctx).Where("approval_id = ?", id).First(&m).Error; errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, repository.ErrNotFound
	} else if e != nil {
		return nil, e
	}
	return modelToParentApproval(m)
}

func (s *Store) ListParentApprovalRequests(ctx context.Context, episodeID string) ([]*recovery.ParentApprovalRequest, error) {
	var rows []ParentApprovalRequestModel
	if err := s.db.WithContext(ctx).Where("episode_id = ?", episodeID).Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]*recovery.ParentApprovalRequest, 0, len(rows))
	for _, row := range rows {
		value, err := modelToParentApproval(row)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, nil
}
func (s *Store) SaveParentDecision(ctx context.Context, v *recovery.ParentDecisionFact) error {
	m, e := parentDecisionToModel(v)
	if e != nil {
		return e
	}
	var x ParentDecisionFactModel
	if q := s.db.WithContext(ctx).Where("approval_id = ?", m.ApprovalID).First(&x).Error; q == nil {
		old, _ := modelToParentDecision(x)
		if old != nil && old.EpisodeID == v.EpisodeID && old.Decision == v.Decision && old.ActorRef == v.ActorRef && old.FactsRef == v.FactsRef {
			return nil
		}
		return repository.ErrParentDecisionConflict
	} else if !errors.Is(q, gorm.ErrRecordNotFound) {
		return q
	}
	return s.db.WithContext(ctx).Create(m).Error
}
func (s *Store) GetParentDecision(ctx context.Context, id string) (*recovery.ParentDecisionFact, error) {
	var m ParentDecisionFactModel
	if e := s.db.WithContext(ctx).Where("approval_id = ?", id).First(&m).Error; errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, repository.ErrNotFound
	} else if e != nil {
		return nil, e
	}
	return modelToParentDecision(m)
}
func (s *Store) SaveBudgetAmendment(ctx context.Context, v *recovery.BudgetAmendment) error {
	m, e := amendmentToModel(v)
	if e != nil {
		return e
	}
	var x BudgetAmendmentModel
	if q := s.db.WithContext(ctx).Where("amendment_id = ?", m.AmendmentID).First(&x).Error; q == nil {
		old, _ := modelToAmendment(x)
		if reflect.DeepEqual(old, v) {
			return nil
		}
		return repository.ErrFactConflict
	} else if !errors.Is(q, gorm.ErrRecordNotFound) {
		return q
	}
	return s.db.WithContext(ctx).Create(m).Error
}
func (s *Store) GetBudgetAmendment(ctx context.Context, id string) (*recovery.BudgetAmendment, error) {
	var m BudgetAmendmentModel
	if e := s.db.WithContext(ctx).Where("amendment_id = ?", id).First(&m).Error; errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, repository.ErrNotFound
	} else if e != nil {
		return nil, e
	}
	return modelToAmendment(m)
}

func (s *Store) CommitRecoveryTransition(ctx context.Context, t repository.RecoveryTransition) error {
	if t.NextEpisode == nil || t.Event == nil || t.RecoveryContext == nil {
		return errors.New("invalid recovery transition")
	}
	if e := t.NextEpisode.Validate(); e != nil {
		return e
	}
	if e := t.Event.Validate(); e != nil {
		return e
	}
	if e := t.RecoveryContext.Validate(); e != nil {
		return e
	}
	if t.Event.Sequence != t.ExpectedEpisodeVersion || t.NextEpisode.Version != t.ExpectedEpisodeVersion+1 {
		return repository.ErrVersionConflict
	}
	updates, e := episodeUpdates(t.NextEpisode)
	if e != nil {
		return e
	}
	em, e := eventToModel(t.Event)
	if e != nil {
		return e
	}
	rm, e := recoveryContextToModel(t.RecoveryContext)
	if e != nil {
		return e
	}
	var am *ParentApprovalRequestModel
	if t.ParentApproval != nil {
		am, e = parentApprovalToModel(t.ParentApproval)
		if e != nil {
			return e
		}
	}
	var cm *CandidateSetModel
	if t.CandidateSet != nil {
		if t.CandidateSet.EpisodeID != t.EpisodeID || t.CandidateSet.RequestID != t.NextEpisode.RequestID || t.CandidateSet.Generation != t.NextEpisode.DiscoveryGeneration {
			return repository.ErrCandidateSetConflict
		}
		cm, e = candidateSetToModel(t.CandidateSet)
		if e != nil {
			return e
		}
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing EventModel
		if q := tx.Where("episode_id = ? AND idempotency_key = ?", t.EpisodeID, t.Event.Action.IdempotencyKey).First(&existing).Error; q == nil {
			return repository.ErrIdempotentReplay
		} else if !errors.Is(q, gorm.ErrRecordNotFound) {
			return q
		}
		var cur EpisodeModel
		if q := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("episode_id = ?", t.EpisodeID).First(&cur).Error; q != nil {
			return q
		}
		if cur.Version != t.ExpectedEpisodeVersion || episode.State(cur.State) != t.Event.StateBefore {
			return repository.ErrVersionConflict
		}
		if cur.ContractSnapshotHash != t.NextEpisode.ContractSnapshotHash || !bytes.Equal(cur.ContractSnapshot, t.NextEpisode.ContractSnapshot) {
			return episode.ErrImmutableContract
		}
		if q := tx.Model(&EpisodeModel{}).Where("episode_id = ? AND version = ?", t.EpisodeID, t.ExpectedEpisodeVersion).Updates(updates).Error; q != nil {
			return q
		}
		if am != nil {
			var existing ParentApprovalRequestModel
			if q := tx.Where("approval_id = ?", am.ApprovalID).First(&existing).Error; errors.Is(q, gorm.ErrRecordNotFound) {
				if q = tx.Create(am).Error; q != nil {
					return q
				}
			} else if q != nil {
				return q
			} else {
				previous, decodeErr := modelToParentApproval(existing)
				if decodeErr != nil {
					return decodeErr
				}
				if previousHash, hashErr := previous.PayloadHashFor(); hashErr != nil || previousHash != am.PayloadHash {
					return repository.ErrFactConflict
				}
			}
		}
		if cm != nil {
			var existing CandidateSetModel
			if q := tx.Where("candidate_set_id = ?", cm.CandidateSetID).First(&existing).Error; errors.Is(q, gorm.ErrRecordNotFound) {
				if q = tx.Create(cm).Error; q != nil {
					return q
				}
			} else if q != nil {
				return q
			} else if existing.PayloadHash != cm.PayloadHash {
				return repository.ErrCandidateSetConflict
			}
		}
		var old RecoveryContextModel
		if q := tx.Where("recovery_id = ?", rm.RecoveryID).First(&old).Error; errors.Is(q, gorm.ErrRecordNotFound) {
			if q = tx.Create(rm).Error; q != nil {
				return q
			}
		} else if q != nil {
			return q
		} else {
			if q = tx.Model(&RecoveryContextModel{}).Where("recovery_id = ?", rm.RecoveryID).Updates(rm).Error; q != nil {
				return q
			}
		}
		return tx.Create(em).Error
	})
}

func (s *Store) CommitParentDecision(ctx context.Context, t repository.ParentDecisionTransition) error {
	if t.NextEpisode == nil || t.Event == nil || t.Decision == nil {
		return errors.New("invalid parent decision transition")
	}
	if e := t.NextEpisode.Validate(); e != nil {
		return e
	}
	if e := t.Event.Validate(); e != nil {
		return e
	}
	if e := t.Decision.Validate(); e != nil {
		return e
	}
	if t.BudgetAmendment != nil {
		if e := t.BudgetAmendment.Validate(); e != nil {
			return e
		}
	}
	if t.Event.Sequence != t.ExpectedEpisodeVersion || t.NextEpisode.Version != t.ExpectedEpisodeVersion+1 {
		return repository.ErrVersionConflict
	}
	updates, e := episodeUpdates(t.NextEpisode)
	if e != nil {
		return e
	}
	em, e := eventToModel(t.Event)
	if e != nil {
		return e
	}
	dm, e := parentDecisionToModel(t.Decision)
	if e != nil {
		return e
	}
	var bm *BudgetAmendmentModel
	if t.BudgetAmendment != nil {
		bm, e = amendmentToModel(t.BudgetAmendment)
		if e != nil {
			return e
		}
	}
	var rm *RecoveryContextModel
	if t.RecoveryContext != nil {
		if err := t.RecoveryContext.Validate(); err != nil {
			return err
		}
		rm, e = recoveryContextToModel(t.RecoveryContext)
		if e != nil {
			return e
		}
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var old ParentDecisionFactModel
		if q := tx.Where("approval_id = ?", dm.ApprovalID).First(&old).Error; q == nil {
			previous, _ := modelToParentDecision(old)
			if previous != nil && previous.EpisodeID == t.Decision.EpisodeID && previous.Decision == t.Decision.Decision && previous.ActorRef == t.Decision.ActorRef && previous.FactsRef == t.Decision.FactsRef {
				return repository.ErrIdempotentReplay
			}
			return repository.ErrParentDecisionConflict
		} else if !errors.Is(q, gorm.ErrRecordNotFound) {
			return q
		}
		var existing EventModel
		if q := tx.Where("episode_id = ? AND idempotency_key = ?", t.EpisodeID, t.Event.Action.IdempotencyKey).First(&existing).Error; q == nil {
			return repository.ErrIdempotentReplay
		} else if !errors.Is(q, gorm.ErrRecordNotFound) {
			return q
		}
		var cur EpisodeModel
		if q := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("episode_id = ?", t.EpisodeID).First(&cur).Error; q != nil {
			return q
		}
		if cur.Version != t.ExpectedEpisodeVersion || episode.State(cur.State) != t.Event.StateBefore {
			return repository.ErrVersionConflict
		}
		if q := tx.Model(&EpisodeModel{}).Where("episode_id = ? AND version = ?", t.EpisodeID, t.ExpectedEpisodeVersion).Updates(updates).Error; q != nil {
			return q
		}
		if q := tx.Create(dm).Error; q != nil {
			return q
		}
		if bm != nil {
			if q := tx.Create(bm).Error; q != nil {
				return q
			}
		}
		if rm != nil {
			if q := tx.Model(&RecoveryContextModel{}).Where("recovery_id = ? AND episode_id = ?", rm.RecoveryID, rm.EpisodeID).Updates(rm).Error; q != nil {
				return q
			}
		}
		return tx.Create(em).Error
	})
}
