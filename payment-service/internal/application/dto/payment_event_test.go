package dto

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPaymentEventCanonicalSerializationRoundTrip(t *testing.T) {
	event := &MQPaymentEvent{
		EventID: "event-1", EventType: "payment.success", SchemaVersion: 1,
		IdempotencyKey: "agent-skill-nonce", TxID: "pay-1", AgentDID: "did:agent",
		SkillDID: "did:skill", AmountMinor: 5000000, Currency: "USDC", Status: "COMPLETED",
		OccurredAt: time.Date(2026, 9, 10, 1, 2, 3, 0, time.UTC).Format(time.RFC3339),
		RequestID:  "req-1", TraceID: "trace-1",
	}
	if err := event.Validate(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var decoded MQPaymentEvent
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.EventID != event.EventID || decoded.EventType != event.EventType || decoded.AmountMinor != event.AmountMinor || decoded.TraceID != event.TraceID {
		t.Fatalf("event changed across JSON round-trip: got %+v", decoded)
	}
}

func TestPaymentEventRejectsTopicAsEventType(t *testing.T) {
	event := &MQPaymentEvent{EventID: "event-1", EventType: "payment_events", SchemaVersion: 1, IdempotencyKey: "key", TxID: "tx", AgentDID: "a", SkillDID: "s", AmountMinor: 1, Currency: "USDC", OccurredAt: time.Now().UTC().Format(time.RFC3339)}
	if err := event.Validate(); err == nil {
		t.Fatal("expected physical topic to be rejected as event_type")
	}
}
