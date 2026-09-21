package repository

import (
	"context"
	"reflect"
	"sort"
	"strings"

	"github.com/stablepay/commerce-runtime/internal/payment"
)

func (s *InMemoryStore) SavePaymentTransportTrace(ctx context.Context, value *payment.PaymentTransportTrace) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if value == nil || value.Validate() != nil {
		return payment.ErrInvalidPaymentTransportTrace
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.paymentTransportTraces[value.TraceID]; ok {
		if reflect.DeepEqual(existing, value) {
			return nil
		}
		return ErrFactConflict
	}
	s.paymentTransportTraces[value.TraceID] = value.Clone()
	return nil
}

func (s *InMemoryStore) ListPaymentTransportTraces(ctx context.Context, episodeID string) ([]*payment.PaymentTransportTrace, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*payment.PaymentTransportTrace, 0)
	for _, value := range s.paymentTransportTraces {
		if err := value.Validate(); err != nil {
			return nil, err
		}
		if value.EpisodeID == strings.TrimSpace(episodeID) {
			result = append(result, value.Clone())
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if !result[i].StartedAt.Equal(result[j].StartedAt) {
			return result[i].StartedAt.Before(result[j].StartedAt)
		}
		return result[i].TraceID < result[j].TraceID
	})
	return result, nil
}

var _ PaymentTransportTraceRepository = (*InMemoryStore)(nil)
