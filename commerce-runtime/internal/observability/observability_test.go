package observability

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/ledger"
	"github.com/stablepay/commerce-runtime/internal/llm"
	"github.com/stablepay/commerce-runtime/internal/memory"
	"github.com/stablepay/commerce-runtime/internal/payment"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

func TestComputeS11MetricsFromRedactedTrace(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	first := &episode.CommerceEpisode{EpisodeID: "ce-happy", State: episode.StateFulfilled, CreatedAt: start, UpdatedAt: start.Add(200 * time.Millisecond)}
	second := &episode.CommerceEpisode{EpisodeID: "ce-recovery", State: episode.StateFulfilled, CreatedAt: start, UpdatedAt: start.Add(2 * time.Second)}
	results := []EpisodeResult{
		{CaseID: "happy", Mode: "live", MemoryMode: "off", RecoveryProvider: "llm", Path: "happy", Trace: EpisodeTrace{Episode: first}},
		{CaseID: "recovery", Mode: "replay", MemoryMode: "on", RecoveryProvider: "rule", Path: "recovery", Failure: FailureInjection{Kind: "delivery_invalid", RatePercent: 10, Applied: true}, Trace: EpisodeTrace{
			Episode: second,
			Events: []*episode.EpisodeEvent{
				{StateBefore: episode.StateInvokingDelivery, StateAfter: episode.StateRecovering, Action: trace.Action{Type: trace.ActionValidateDelivery}, OccurredAt: start.Add(500 * time.Millisecond), RuntimeVerdict: trace.RuntimeVerdict{Allowed: true}},
				{StateBefore: episode.StateRecovering, StateAfter: episode.StateInvokingDelivery, Action: trace.Action{Type: trace.ActionRetrySameMerchant}, OccurredAt: start.Add(time.Second), RuntimeVerdict: trace.RuntimeVerdict{Allowed: true}},
				{StateBefore: episode.StateInvokingDelivery, StateAfter: episode.StateValidatingDelivery, Action: trace.Action{Type: trace.ActionInvoke}, OccurredAt: start.Add(1500 * time.Millisecond), RuntimeVerdict: trace.RuntimeVerdict{Allowed: true}},
				{StateBefore: episode.StateValidatingDelivery, StateAfter: episode.StateFulfilled, Action: trace.Action{Type: trace.ActionValidateDelivery}, OccurredAt: start.Add(2 * time.Second), RuntimeVerdict: trace.RuntimeVerdict{Allowed: true}},
			},
			ModelDecisionTraces: []*llm.ModelDecisionTrace{{TraceID: "trace-rule", EpisodeID: "ce-recovery", Provider: "rule", ModelRef: "rule-recovery", RequestStartedAt: start, ResponseReceivedAt: start.Add(20 * time.Millisecond), Status: llm.TraceFallback}},
			MemoryUseTraces:     []*memory.MemoryUseTrace{{ModelDecisionTraceID: "trace-rule", RetrievedMemoryRefs: []string{"memory://1"}, CitedMemoryRefs: []string{"memory://1"}, GuardAccepted: true}},
			Ledger:              []*ledger.LedgerEntry{{Type: ledger.EntryPaymentSettled, PaymentIntentID: "pi-1"}, {Type: ledger.EntryPaymentSettled, PaymentIntentID: "pi-1"}},
			PaymentIntents:      []*payment.PaymentIntent{{Status: payment.IntentConfirmed, CreatedAt: start, UpdatedAt: start.Add(400 * time.Millisecond)}},
		}},
	}
	metrics := Compute(results, start.Add(3*time.Second))
	if metrics.TaskSuccessRate != 1 || metrics.RecoverySuccessRate != 1 {
		t.Fatalf("unexpected success rates: %#v", metrics)
	}
	if metrics.DuplicateSettlementCount != 1 || metrics.DeliveryRetryCount != 0 {
		t.Fatalf("unexpected economic/retry metrics: %#v", metrics)
	}
	if metrics.MemoryRetrievalHitRate != 1 || metrics.MemoryCitationRate != 1 || metrics.MemoryGuidedActionSuccess != 1 {
		t.Fatalf("unexpected memory metrics: %#v", metrics)
	}
	if metrics.UnsafeSideEffectCount != 0 {
		t.Fatalf("guarded traces produced a side effect: %#v", metrics)
	}
	if _, ok := metrics.FailureInjectionRecoverySteps["delivery_invalid"]; !ok {
		t.Fatalf("missing fault recovery steps: %#v", metrics.FailureInjectionRecoverySteps)
	}
	markdown := RenderMarkdown(metrics, results)
	if !strings.Contains(markdown, "task_success_rate") || !strings.Contains(markdown, "not described as having passed through DeepSeek") {
		t.Fatalf("report omitted evidence boundary: %s", markdown)
	}
}

func TestPercentileUsesDeterministicInterpolation(t *testing.T) {
	if got := Percentile([]float64{1, 2, 3, 4}, 0.95); math.Abs(got-3.85) > 1e-9 {
		t.Fatalf("p95=%v", got)
	}
}
