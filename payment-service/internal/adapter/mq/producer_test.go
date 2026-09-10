package mq

import (
	"testing"

	"github.com/stablepay/payment-service/internal/application/dto"
)

func TestEventTagUsesCanonicalEventType(t *testing.T) {
	if got := eventTagForEvent(&dto.MQPaymentEvent{EventType: "payment.success"}); got != "payment.success" {
		t.Fatalf("event tag=%q, want payment.success", got)
	}
	if got := eventTagForEvent(&dto.MQPaymentEvent{Status: "FAILED"}); got != "payment.failed" {
		t.Fatalf("status fallback tag=%q, want payment.failed", got)
	}
}
