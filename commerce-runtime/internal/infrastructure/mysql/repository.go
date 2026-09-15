// Package mysql provides the GORM/MySQL adapter for the commerce runtime.
package mysql

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"github.com/stablepay/commerce-runtime/internal/trace"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type EpisodeModel struct {
	EpisodeID               string    `gorm:"column:episode_id;type:varchar(128);primaryKey"`
	RequestID               string    `gorm:"column:request_id;type:varchar(128);not null;uniqueIndex:uk_commerce_episode_request_id"`
	SessionID               string    `gorm:"column:session_id;type:varchar(128)"`
	State                   string    `gorm:"column:state;type:varchar(32);not null"`
	TerminalReason          string    `gorm:"column:terminal_reason;type:varchar(128)"`
	ContractSnapshotHash    string    `gorm:"column:contract_snapshot_hash;type:char(71);not null"`
	ContractSnapshot        []byte    `gorm:"column:contract_snapshot;type:longtext;not null"`
	SelectedMerchantDID     string    `gorm:"column:selected_merchant_did;type:varchar(128)"`
	SelectedCapabilityID    string    `gorm:"column:selected_capability_id;type:varchar(128)"`
	SelectedWorkflowVersion string    `gorm:"column:selected_workflow_version;type:varchar(64)"`
	CurrentQuoteHash        string    `gorm:"column:current_quote_hash;type:char(71)"`
	EntitlementRefs         []byte    `gorm:"column:entitlement_refs;type:json"`
	DeliveryRefs            []byte    `gorm:"column:delivery_refs;type:json"`
	ValidationEvidenceRefs  []byte    `gorm:"column:validation_evidence_refs;type:json"`
	AttemptedMerchants      []byte    `gorm:"column:attempted_merchants;type:json"`
	ActionCount             int       `gorm:"column:action_count;not null"`
	PaymentAttemptCount     int       `gorm:"column:payment_attempt_count;not null"`
	DeliveryAttemptCount    int       `gorm:"column:delivery_attempt_count;not null"`
	RetryCount              int       `gorm:"column:retry_count;not null"`
	MaxTotalAttempts        int       `gorm:"column:max_total_attempts;not null"`
	MaxPaymentAttempts      int       `gorm:"column:max_payment_attempts;not null"`
	MaxDeliveryAttempts     int       `gorm:"column:max_delivery_attempts;not null"`
	BudgetCurrency          string    `gorm:"column:budget_currency;type:varchar(16);not null"`
	BudgetLimitMinor        int64     `gorm:"column:budget_limit_minor;not null"`
	ReservedAmount          int64     `gorm:"column:reserved_amount;not null"`
	SettledAmount           int64     `gorm:"column:settled_amount;not null"`
	RefundedAmount          int64     `gorm:"column:refunded_amount;not null"`
	ConsumedAmount          int64     `gorm:"column:consumed_amount;not null"`
	AvailableBudget         int64     `gorm:"column:available_budget;not null"`
	SunkCost                int64     `gorm:"column:sunk_cost;not null"`
	RefundReusable          bool      `gorm:"column:refund_reusable;not null;default:true"`
	DeadlineAt              time.Time `gorm:"column:deadline_at;not null"`
	Version                 uint64    `gorm:"column:version;not null"`
	CreatedAt               time.Time `gorm:"column:created_at;not null"`
	UpdatedAt               time.Time `gorm:"column:updated_at;not null"`
}

func (EpisodeModel) TableName() string { return "commerce_episodes" }

type EventModel struct {
	EventID            string    `gorm:"column:event_id;type:varchar(128);primaryKey"`
	EpisodeID          string    `gorm:"column:episode_id;type:varchar(128);not null;uniqueIndex:uk_episode_sequence;index:idx_episode_events_episode"`
	Sequence           uint64    `gorm:"column:sequence;not null;uniqueIndex:uk_episode_sequence"`
	OccurredAt         time.Time `gorm:"column:occurred_at;not null"`
	StateBefore        string    `gorm:"column:state_before;type:varchar(32);not null"`
	ActionPayload      []byte    `gorm:"column:action_payload;type:json;not null"`
	ObservationPayload []byte    `gorm:"column:observation_payload;type:json;not null"`
	DecisionPayload    []byte    `gorm:"column:decision_payload;type:json;not null"`
	VerdictPayload     []byte    `gorm:"column:runtime_verdict_payload;type:json;not null"`
	StateAfter         string    `gorm:"column:state_after;type:varchar(32);not null"`
	Actor              string    `gorm:"column:actor;type:varchar(32);not null"`
	TraceID            string    `gorm:"column:trace_id;type:varchar(128)"`
	RuntimeVersion     string    `gorm:"column:runtime_version;type:varchar(64);not null"`
	IdempotencyKey     string    `gorm:"column:idempotency_key;type:varchar(128);not null;uniqueIndex:uk_episode_idempotency"`
}

func (EventModel) TableName() string { return "episode_events" }

type Store struct{ db *gorm.DB }

func Open(dsn string) (*gorm.DB, error) {
	if dsn == "" {
		return nil, errors.New("mysql dsn is required")
	}
	return gorm.Open(mysql.Open(dsn), &gorm.Config{})
}

func NewStore(db *gorm.DB) *Store { return &Store{db: db} }

func AutoMigrate(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return errors.New("mysql db is required")
	}
	return db.WithContext(ctx).AutoMigrate(&EpisodeModel{}, &EventModel{})
}

func (s *Store) Create(ctx context.Context, value *episode.CommerceEpisode) error {
	model, err := episodeToModel(value)
	if err != nil {
		return err
	}
	err = s.db.WithContext(ctx).Create(model).Error
	if isDuplicateKey(err) {
		return repository.ErrRequestIDConflict
	}
	return err
}

func (s *Store) Get(ctx context.Context, episodeID string) (*episode.CommerceEpisode, error) {
	var row EpisodeModel
	err := s.db.WithContext(ctx).Where("episode_id = ?", episodeID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return modelToEpisode(row)
}

func (s *Store) FindByRequestID(ctx context.Context, requestID string) (*episode.CommerceEpisode, error) {
	var row EpisodeModel
	err := s.db.WithContext(ctx).Where("request_id = ?", requestID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return modelToEpisode(row)
}

func (s *Store) UpdateOptimistic(ctx context.Context, episodeID string, expectedVersion uint64, next *episode.CommerceEpisode) error {
	if next == nil {
		return errors.New("episode is required")
	}
	var current EpisodeModel
	err := s.db.WithContext(ctx).Where("episode_id = ?", episodeID).First(&current).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return repository.ErrNotFound
	}
	if err != nil {
		return err
	}
	if current.Version != expectedVersion {
		return repository.ErrVersionConflict
	}
	if current.ContractSnapshotHash != next.ContractSnapshotHash || !bytes.Equal(current.ContractSnapshot, next.ContractSnapshot) {
		return episode.ErrImmutableContract
	}
	updates, err := episodeUpdates(next)
	if err != nil {
		return err
	}
	result := s.db.WithContext(ctx).Model(&EpisodeModel{}).
		Where("episode_id = ? AND version = ?", episodeID, expectedVersion).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return repository.ErrVersionConflict
	}
	return nil
}

func (s *Store) Append(ctx context.Context, value *episode.EpisodeEvent) error {
	model, err := eventToModel(value)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Create(model).Error
}

func (s *Store) ListByEpisode(ctx context.Context, episodeID string) ([]*episode.EpisodeEvent, error) {
	var rows []EventModel
	if err := s.db.WithContext(ctx).Where("episode_id = ?", episodeID).Order("sequence ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]*episode.EpisodeEvent, 0, len(rows))
	for _, row := range rows {
		value, err := modelToEvent(row)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

func (s *Store) FindByIdempotencyKey(ctx context.Context, episodeID, key string) (*episode.EpisodeEvent, error) {
	var row EventModel
	err := s.db.WithContext(ctx).Where("episode_id = ? AND idempotency_key = ?", episodeID, key).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return modelToEvent(row)
}

func (s *Store) CommitTransition(ctx context.Context, episodeID string, expectedVersion uint64, next *episode.CommerceEpisode, event *episode.EpisodeEvent) error {
	if next == nil || event == nil || next.EpisodeID != episodeID || event.EpisodeID != episodeID {
		return errors.New("transition records do not refer to the same episode")
	}
	if err := next.Validate(); err != nil {
		return err
	}
	if err := event.Validate(); err != nil {
		return err
	}
	if event.Sequence != expectedVersion {
		return repository.ErrEventSequenceConflict
	}
	updates, err := episodeUpdates(next)
	if err != nil {
		return err
	}
	eventModel, err := eventToModel(event)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing EventModel
		if err := tx.Where("episode_id = ? AND idempotency_key = ?", episodeID, event.Action.IdempotencyKey).First(&existing).Error; err == nil {
			return repository.ErrIdempotentReplay
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		var current EpisodeModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("episode_id = ?", episodeID).First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return repository.ErrNotFound
			}
			return err
		}
		if current.Version != expectedVersion || next.Version != expectedVersion+1 {
			return repository.ErrVersionConflict
		}
		if event.StateBefore != episode.State(current.State) || event.StateAfter != next.State {
			return errors.New("transition event does not match episode projection")
		}
		if current.ContractSnapshotHash != next.ContractSnapshotHash || !bytes.Equal(current.ContractSnapshot, next.ContractSnapshot) {
			return episode.ErrImmutableContract
		}
		result := tx.Model(&EpisodeModel{}).
			Where("episode_id = ? AND version = ?", episodeID, expectedVersion).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return repository.ErrVersionConflict
		}
		if err := tx.Create(eventModel).Error; err != nil {
			return err
		}
		return nil
	})
}

func episodeToModel(value *episode.CommerceEpisode) (*EpisodeModel, error) {
	if value == nil {
		return nil, errors.New("episode is required")
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	encode := func(values []string) ([]byte, error) {
		if values == nil {
			values = []string{}
		}
		return json.Marshal(values)
	}
	entitlement, err := encode(value.EntitlementRefs)
	if err != nil {
		return nil, err
	}
	delivery, err := encode(value.DeliveryRefs)
	if err != nil {
		return nil, err
	}
	evidence, err := encode(value.ValidationEvidenceRefs)
	if err != nil {
		return nil, err
	}
	attempted, err := encode(value.AttemptedMerchants)
	if err != nil {
		return nil, err
	}
	return &EpisodeModel{
		EpisodeID: value.EpisodeID, RequestID: value.RequestID, SessionID: value.SessionID,
		State: string(value.State), TerminalReason: value.TerminalReason,
		ContractSnapshotHash: value.ContractSnapshotHash, ContractSnapshot: append([]byte(nil), value.ContractSnapshot...),
		SelectedMerchantDID: value.SelectedMerchantDID, SelectedCapabilityID: value.SelectedCapabilityID,
		SelectedWorkflowVersion: value.SelectedWorkflowVersion, CurrentQuoteHash: value.CurrentQuoteHash,
		EntitlementRefs: entitlement, DeliveryRefs: delivery, ValidationEvidenceRefs: evidence, AttemptedMerchants: attempted,
		ActionCount: value.ActionCount, PaymentAttemptCount: value.PaymentAttemptCount, DeliveryAttemptCount: value.DeliveryAttemptCount,
		RetryCount: value.RetryCount, MaxTotalAttempts: value.MaxTotalAttempts, MaxPaymentAttempts: value.MaxPaymentAttempts,
		MaxDeliveryAttempts: value.MaxDeliveryAttempts, BudgetCurrency: value.Budget.Currency,
		BudgetLimitMinor: value.Budget.BudgetLimitMinor, ReservedAmount: value.Budget.ReservedAmount,
		SettledAmount: value.Budget.SettledAmount, RefundedAmount: value.Budget.RefundedAmount,
		ConsumedAmount: value.Budget.ConsumedAmount, AvailableBudget: value.Budget.AvailableBudget, SunkCost: value.Budget.SunkCost,
		RefundReusable: value.Budget.RefundReusable,
		DeadlineAt:     value.DeadlineAt, Version: value.Version, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}, nil
}

func modelToEpisode(row EpisodeModel) (*episode.CommerceEpisode, error) {
	decode := func(value []byte) ([]string, error) {
		if len(value) == 0 {
			return nil, nil
		}
		var result []string
		if err := json.Unmarshal(value, &result); err != nil {
			return nil, err
		}
		return result, nil
	}
	entitlement, err := decode(row.EntitlementRefs)
	if err != nil {
		return nil, err
	}
	delivery, err := decode(row.DeliveryRefs)
	if err != nil {
		return nil, err
	}
	evidence, err := decode(row.ValidationEvidenceRefs)
	if err != nil {
		return nil, err
	}
	attempted, err := decode(row.AttemptedMerchants)
	if err != nil {
		return nil, err
	}
	value := &episode.CommerceEpisode{
		EpisodeID: row.EpisodeID, RequestID: row.RequestID, SessionID: row.SessionID,
		State: episode.State(row.State), TerminalReason: row.TerminalReason,
		ContractSnapshotHash: row.ContractSnapshotHash, ContractSnapshot: append([]byte(nil), row.ContractSnapshot...),
		SelectedMerchantDID: row.SelectedMerchantDID, SelectedCapabilityID: row.SelectedCapabilityID,
		SelectedWorkflowVersion: row.SelectedWorkflowVersion, CurrentQuoteHash: row.CurrentQuoteHash,
		EntitlementRefs: entitlement, DeliveryRefs: delivery, ValidationEvidenceRefs: evidence, AttemptedMerchants: attempted,
		ActionCount: row.ActionCount, PaymentAttemptCount: row.PaymentAttemptCount, DeliveryAttemptCount: row.DeliveryAttemptCount,
		RetryCount: row.RetryCount, MaxTotalAttempts: row.MaxTotalAttempts, MaxPaymentAttempts: row.MaxPaymentAttempts,
		MaxDeliveryAttempts: row.MaxDeliveryAttempts, Budget: episode.BudgetSnapshot{
			Currency: row.BudgetCurrency, BudgetLimitMinor: row.BudgetLimitMinor, ReservedAmount: row.ReservedAmount,
			SettledAmount: row.SettledAmount, RefundedAmount: row.RefundedAmount, AvailableBudget: row.AvailableBudget, SunkCost: row.SunkCost,
			ConsumedAmount: row.ConsumedAmount, RefundReusable: row.RefundReusable,
		}, DeadlineAt: row.DeadlineAt, Version: row.Version, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return value, nil
}

func episodeUpdates(value *episode.CommerceEpisode) (map[string]any, error) {
	model, err := episodeToModel(value)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"session_id": model.SessionID, "state": model.State, "terminal_reason": model.TerminalReason,
		"selected_merchant_did": model.SelectedMerchantDID, "selected_capability_id": model.SelectedCapabilityID,
		"selected_workflow_version": model.SelectedWorkflowVersion, "current_quote_hash": model.CurrentQuoteHash,
		"entitlement_refs": model.EntitlementRefs, "delivery_refs": model.DeliveryRefs,
		"validation_evidence_refs": model.ValidationEvidenceRefs, "attempted_merchants": model.AttemptedMerchants,
		"action_count": model.ActionCount, "payment_attempt_count": model.PaymentAttemptCount,
		"delivery_attempt_count": model.DeliveryAttemptCount, "retry_count": model.RetryCount,
		"max_total_attempts": model.MaxTotalAttempts, "max_payment_attempts": model.MaxPaymentAttempts,
		"max_delivery_attempts": model.MaxDeliveryAttempts, "budget_currency": model.BudgetCurrency,
		"budget_limit_minor": model.BudgetLimitMinor, "reserved_amount": model.ReservedAmount,
		"settled_amount": model.SettledAmount, "refunded_amount": model.RefundedAmount,
		"consumed_amount": model.ConsumedAmount, "available_budget": model.AvailableBudget, "sunk_cost": model.SunkCost,
		"refund_reusable": model.RefundReusable, "deadline_at": model.DeadlineAt,
		"version": model.Version, "updated_at": model.UpdatedAt,
	}, nil
}

func eventToModel(value *episode.EpisodeEvent) (*EventModel, error) {
	if value == nil {
		return nil, errors.New("event is required")
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	action, err := json.Marshal(value.Action)
	if err != nil {
		return nil, err
	}
	observation, err := json.Marshal(value.Observation)
	if err != nil {
		return nil, err
	}
	decision, err := json.Marshal(value.Decision)
	if err != nil {
		return nil, err
	}
	verdict, err := json.Marshal(value.RuntimeVerdict)
	if err != nil {
		return nil, err
	}
	return &EventModel{EventID: value.EventID, EpisodeID: value.EpisodeID, Sequence: value.Sequence,
		OccurredAt: value.OccurredAt, StateBefore: string(value.StateBefore), ActionPayload: action,
		ObservationPayload: observation, DecisionPayload: decision, VerdictPayload: verdict,
		StateAfter: string(value.StateAfter), Actor: value.Actor, TraceID: value.TraceID,
		RuntimeVersion: value.RuntimeVersion, IdempotencyKey: value.Action.IdempotencyKey}, nil
}

func modelToEvent(row EventModel) (*episode.EpisodeEvent, error) {
	var action trace.Action
	var observation trace.Observation
	var decision trace.Decision
	var verdict trace.RuntimeVerdict
	if err := json.Unmarshal(row.ActionPayload, &action); err != nil {
		return nil, fmt.Errorf("decode action: %w", err)
	}
	if err := json.Unmarshal(row.ObservationPayload, &observation); err != nil {
		return nil, fmt.Errorf("decode observation: %w", err)
	}
	if err := json.Unmarshal(row.DecisionPayload, &decision); err != nil {
		return nil, fmt.Errorf("decode decision: %w", err)
	}
	if err := json.Unmarshal(row.VerdictPayload, &verdict); err != nil {
		return nil, fmt.Errorf("decode verdict: %w", err)
	}
	value := &episode.EpisodeEvent{EventID: row.EventID, EpisodeID: row.EpisodeID, Sequence: row.Sequence,
		OccurredAt: row.OccurredAt, StateBefore: episode.State(row.StateBefore), Action: action,
		Observation: observation, Decision: decision, RuntimeVerdict: verdict, StateAfter: episode.State(row.StateAfter),
		Actor: row.Actor, TraceID: row.TraceID, RuntimeVersion: row.RuntimeVersion}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return value, nil
}

func isDuplicateKey(err error) bool {
	var mysqlErr *mysqlDriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
