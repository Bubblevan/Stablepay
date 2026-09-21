package mysql

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/payment"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"gorm.io/gorm"
)

type PaymentTransportTraceModel struct {
	TraceID            string    `gorm:"column:trace_id;type:varchar(255);primaryKey"`
	EpisodeID          string    `gorm:"column:episode_id;type:varchar(128);not null;index:idx_payment_transport_episode"`
	PaymentIntentID    string    `gorm:"column:payment_intent_id;type:varchar(128);not null;index:idx_payment_transport_intent"`
	Kind               string    `gorm:"column:kind;type:varchar(32);not null"`
	IdempotencyKey     string    `gorm:"column:idempotency_key;type:varchar(255);not null"`
	RequestFingerprint string    `gorm:"column:request_fingerprint;type:char(71);not null"`
	StartedAt          time.Time `gorm:"column:started_at;not null"`
	FinishedAt         time.Time `gorm:"column:finished_at;not null"`
	ResultStatus       string    `gorm:"column:result_status;type:varchar(64);not null"`
	PayloadHash        string    `gorm:"column:payload_hash;type:char(71);not null"`
}

func (PaymentTransportTraceModel) TableName() string { return "payment_transport_traces" }

func paymentTransportToModel(value *payment.PaymentTransportTrace) (*PaymentTransportTraceModel, error) {
	if value == nil || value.Validate() != nil {
		return nil, payment.ErrInvalidPaymentTransportTrace
	}
	return &PaymentTransportTraceModel{TraceID: value.TraceID, EpisodeID: value.EpisodeID, PaymentIntentID: value.PaymentIntentID, Kind: value.Kind, IdempotencyKey: value.IdempotencyKey, RequestFingerprint: value.RequestFingerprint, StartedAt: value.StartedAt, FinishedAt: value.FinishedAt, ResultStatus: value.ResultStatus, PayloadHash: value.PayloadHash}, nil
}

func modelToPaymentTransport(value PaymentTransportTraceModel) (*payment.PaymentTransportTrace, error) {
	result := &payment.PaymentTransportTrace{TraceID: value.TraceID, EpisodeID: value.EpisodeID, PaymentIntentID: value.PaymentIntentID, Kind: value.Kind, IdempotencyKey: value.IdempotencyKey, RequestFingerprint: value.RequestFingerprint, StartedAt: value.StartedAt, FinishedAt: value.FinishedAt, ResultStatus: value.ResultStatus, PayloadHash: value.PayloadHash}
	if err := result.Validate(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) SavePaymentTransportTrace(ctx context.Context, value *payment.PaymentTransportTrace) error {
	model, err := paymentTransportToModel(value)
	if err != nil {
		return err
	}
	var existing PaymentTransportTraceModel
	lookup := s.db.WithContext(ctx).Where("trace_id = ?", model.TraceID).First(&existing).Error
	if lookup == nil {
		previous, decodeErr := modelToPaymentTransport(existing)
		if decodeErr == nil && reflect.DeepEqual(previous, value) {
			return nil
		}
		return repository.ErrFactConflict
	}
	if !errors.Is(lookup, gorm.ErrRecordNotFound) {
		return lookup
	}
	if err := s.db.WithContext(ctx).Create(model).Error; err != nil {
		if isDuplicateKey(err) {
			return repository.ErrFactConflict
		}
		return err
	}
	return nil
}

func (s *Store) ListPaymentTransportTraces(ctx context.Context, episodeID string) ([]*payment.PaymentTransportTrace, error) {
	var models []PaymentTransportTraceModel
	if err := s.db.WithContext(ctx).Where("episode_id = ?", strings.TrimSpace(episodeID)).Order("started_at ASC, trace_id ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	result := make([]*payment.PaymentTransportTrace, 0, len(models))
	for _, model := range models {
		value, err := modelToPaymentTransport(model)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if !result[i].StartedAt.Equal(result[j].StartedAt) {
			return result[i].StartedAt.Before(result[j].StartedAt)
		}
		return result[i].TraceID < result[j].TraceID
	})
	return result, nil
}

var _ repository.PaymentTransportTraceRepository = (*Store)(nil)
