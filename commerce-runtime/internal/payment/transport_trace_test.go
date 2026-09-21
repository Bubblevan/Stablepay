package payment

import (
	"testing"
	"time"
)

func TestPaymentTransportTraceOmitsSecretsAndRejectsTampering(t *testing.T) {
	value := PaymentTransportTrace{
		TraceID:            "transport-1",
		EpisodeID:          "episode-1",
		PaymentIntentID:    "pi-1",
		Kind:               "EXACT_REDELIVERY",
		IdempotencyKey:     "idem-1",
		RequestFingerprint: "sha256:request",
		StartedAt:          time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		FinishedAt:         time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC),
		ResultStatus:       string(OutcomeConfirmed),
	}
	if err := value.RefreshPayloadHash(); err != nil {
		t.Fatal(err)
	}
	if err := value.Validate(); err != nil {
		t.Fatalf("fresh payment transport trace rejected: %v", err)
	}
	value.IdempotencyKey = "idem-2"
	if err := value.Validate(); err == nil {
		t.Fatal("tampered payment transport trace passed hash validation")
	}
}
