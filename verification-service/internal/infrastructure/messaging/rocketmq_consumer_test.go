package messaging

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stablepay/verification-service/internal/domain/entity"
)

type fakePurchaseRepository struct {
	recordsByPair  map[string]*entity.PurchaseRecord
	recordsByEvent map[string]*entity.PurchaseRecord
}

func (f *fakePurchaseRepository) Find(_ context.Context, agentDID, skillDID string) (*entity.PurchaseRecord, error) {
	record, ok := f.recordsByPair[agentDID+"|"+skillDID]
	if !ok {
		return nil, errors.New("not found")
	}
	return record, nil
}

func (f *fakePurchaseRepository) FindByEventID(_ context.Context, eventID string) (*entity.PurchaseRecord, error) {
	record, ok := f.recordsByEvent[eventID]
	if !ok {
		return nil, errors.New("not found")
	}
	return record, nil
}

func (f *fakePurchaseRepository) Create(_ context.Context, record *entity.PurchaseRecord) error {
	if f.recordsByPair == nil {
		f.recordsByPair = map[string]*entity.PurchaseRecord{}
	}
	if f.recordsByEvent == nil {
		f.recordsByEvent = map[string]*entity.PurchaseRecord{}
	}
	f.recordsByPair[record.AgentDID+"|"+record.SkillDID] = record
	f.recordsByEvent[record.EventID] = record
	return nil
}

func TestIsSuccessfulPaymentEvent(t *testing.T) {
	cases := []struct {
		name  string
		tag   string
		event PaymentEvent
		want  bool
	}{
		{name: "success tag", tag: "PAYMENT_SUCCEEDED", want: true},
		{name: "completed status", event: PaymentEvent{Status: "completed"}, want: true},
		{name: "failed status", event: PaymentEvent{Status: "failed"}, want: false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := isSuccessful(test.tag, test.event); got != test.want {
				t.Fatalf("isSuccessful()=%v, want %v", got, test.want)
			}
		})
	}
}

func TestPaymentEventNormalization(t *testing.T) {
	if got := currencyCode("USDT"); got != 2 {
		t.Fatalf("USDT currency code=%d, want 2", got)
	}
	if got := currencyCode("USDC"); got != 1 {
		t.Fatalf("USDC currency code=%d, want 1", got)
	}
	when := timeFromUnix(1)
	if !when.Equal(time.Unix(1, 0).UTC()) {
		t.Fatalf("timestamp=%v, want unix timestamp", when)
	}
	if len(resolveNameServers([]string{"namesrv-a", "namesrv-b:9876"})) != 2 {
		t.Fatalf("expected two resolved name servers")
	}
}

func TestPaymentEventProjectionIsIdempotentByEventID(t *testing.T) {
	repository := &fakePurchaseRepository{}
	consumer := NewConsumer([]string{"127.0.0.1:9876"}, "verification_group", "payment_events", repository)
	event := PaymentEvent{
		EventID: "event-1", EventType: "payment.success", SchemaVersion: 1,
		IdempotencyKey: "payment-key", TxID: "tx-1", AgentDID: "did:agent", SkillDID: "did:skill",
		AmountMinor: 5000000, Currency: "USDC", Status: "COMPLETED",
		OccurredAt: "2026-09-10T01:02:03Z",
	}
	if err := consumer.persistEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := consumer.persistEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if len(repository.recordsByEvent) != 1 || len(repository.recordsByPair) != 1 {
		t.Fatalf("replayed event created duplicate projection: events=%d pairs=%d", len(repository.recordsByEvent), len(repository.recordsByPair))
	}
}

func TestPaymentEventRequiresCanonicalEnvelope(t *testing.T) {
	event := PaymentEvent{EventID: "event-1", EventType: "payment.success", SchemaVersion: 1, TxID: "tx-1", AgentDID: "a", SkillDID: "s", AmountMinor: 1, Currency: "USDC", OccurredAt: "2026-09-10T01:02:03Z"}
	if err := event.Validate(); err == nil {
		t.Fatal("expected missing idempotency_key to fail")
	}
}
