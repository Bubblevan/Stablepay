package observability

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/invocation"
	"github.com/stablepay/commerce-runtime/internal/ledger"
	"github.com/stablepay/commerce-runtime/internal/llm"
	"github.com/stablepay/commerce-runtime/internal/memory"
	"github.com/stablepay/commerce-runtime/internal/payment"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

func TestComputeS11MetricsFromRedactedTrace(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	first := &episode.CommerceEpisode{EpisodeID: "ce-happy", State: episode.StateFulfilled, CreatedAt: start, UpdatedAt: start.Add(200 * time.Millisecond)}
	second := &episode.CommerceEpisode{EpisodeID: "ce-recovery", State: episode.StateFulfilled, CreatedAt: start, UpdatedAt: start.Add(2 * time.Second)}
	results := []EpisodeResult{
		{CaseID: "happy", Mode: "live", MemoryMode: "off", RecoveryProvider: "llm", Path: "happy", Trace: EpisodeTrace{Episode: first}},
		{CaseID: "recovery", Mode: "replay", MemoryMode: "on", RecoveryProvider: "rule", Path: "recovery", Failure: FailureInjection{Kind: "delivery_invalid", RatePercent: 10, Configured: true, Triggered: true, InjectionCount: 1}, Trace: EpisodeTrace{
			Episode: second,
			Events: []*episode.EpisodeEvent{
				{StateBefore: episode.StateInvokingDelivery, StateAfter: episode.StateRecovering, Action: trace.Action{Type: trace.ActionValidateDelivery}, OccurredAt: start.Add(500 * time.Millisecond), RuntimeVerdict: trace.RuntimeVerdict{Allowed: true}},
				{StateBefore: episode.StateRecovering, StateAfter: episode.StateInvokingDelivery, Action: trace.Action{Type: trace.ActionRetrySameMerchant}, OccurredAt: start.Add(time.Second), RuntimeVerdict: trace.RuntimeVerdict{Allowed: true}},
				{StateBefore: episode.StateInvokingDelivery, StateAfter: episode.StateValidatingDelivery, Action: trace.Action{Type: trace.ActionInvoke}, OccurredAt: start.Add(1500 * time.Millisecond), RuntimeVerdict: trace.RuntimeVerdict{Allowed: true}},
				{StateBefore: episode.StateValidatingDelivery, StateAfter: episode.StateFulfilled, Action: trace.Action{Type: trace.ActionValidateDelivery}, OccurredAt: start.Add(2 * time.Second), RuntimeVerdict: trace.RuntimeVerdict{Allowed: true}},
			},
			ModelDecisionTraces: []*llm.ModelDecisionTrace{{TraceID: "trace-rule", EpisodeID: "ce-recovery", Provider: "rule", ModelRef: "rule-recovery", RequestStartedAt: start, ResponseReceivedAt: start.Add(20 * time.Millisecond), Status: llm.TraceFallback}},
			MemoryUseTraces:     []*memory.MemoryUseTrace{{ModelDecisionTraceID: "trace-rule", RetrievedMemoryRefs: []string{"memory://1"}, CitedMemoryRefs: []string{"memory://1"}, RetrievalAttempted: true, GuardAccepted: true}},
			Ledger:              []*ledger.LedgerEntry{{Type: ledger.EntryPaymentSettled, PaymentIntentID: "pi-1"}, {Type: ledger.EntryPaymentSettled, PaymentIntentID: "pi-1"}},
			PaymentIntents:      []*payment.PaymentIntent{{Status: payment.IntentConfirmed, CreatedAt: start, UpdatedAt: start.Add(400 * time.Millisecond)}},
		}},
	}
	metrics := Compute(results, start.Add(3*time.Second))
	if metrics.TaskSuccessRate != 0 || metrics.RecoverySuccessRate != 0 || metrics.Evidence.ValidTrials != 1 {
		t.Fatalf("unexpected success rates: %#v", metrics)
	}
	if metrics.DuplicateSettlementCount != 1 || metrics.DeliveryRetryCount != 0 {
		t.Fatalf("unexpected economic/retry metrics: %#v", metrics)
	}
	if metrics.MemoryRetrievalHitRate != 1 || metrics.MemoryCitationRate != 1 || metrics.MemoryGuidedActionSuccess != 0 {
		t.Fatalf("unexpected memory metrics: %#v", metrics)
	}
	if metrics.UnsafeSideEffectCount != 0 {
		t.Fatalf("guarded traces produced a side effect: %#v", metrics)
	}
	if _, ok := metrics.FailureInjectionRecoverySteps["delivery_invalid"]; !ok {
		t.Fatalf("missing fault recovery steps: %#v", metrics.FailureInjectionRecoverySteps)
	}
	markdown := RenderMarkdown(metrics, results)
	if !strings.Contains(markdown, "Mode-scoped headline metrics") || !strings.Contains(markdown, "never as DeepSeek") {
		t.Fatalf("report omitted evidence boundary: %s", markdown)
	}
}

func TestPercentileUsesDeterministicInterpolation(t *testing.T) {
	if got := Percentile([]float64{1, 2, 3, 4}, 0.95); math.Abs(got-3.85) > 1e-9 {
		t.Fatalf("p95=%v", got)
	}
}

func TestIntegritySeparatesParserContextAndRuntimeGuard(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	base := func(id string) EpisodeResult {
		return EpisodeResult{CaseID: id, Mode: ModeReplay, RecoveryProvider: "llm", Trace: EpisodeTrace{Episode: &episode.CommerceEpisode{EpisodeID: id, State: episode.StateFulfilled}}}
	}
	outcome := func(id string, stage trace.DecisionStage, reached, accepted bool, before, after *trace.GuardSideEffectSnapshot) *trace.DecisionOutcomeTrace {
		return &trace.DecisionOutcomeTrace{DecisionOutcomeTraceID: "dot-" + id, EpisodeID: id, ModelDecisionTraceID: "model-" + id, ProposalID: "proposal-" + id, ProposedAction: "retry_same_merchant", Stage: stage, ReachedRuntimeGuard: reached, GuardAccepted: accepted, CreatedAt: now, FactsRef: "decision-outcome-trace://dot-" + id, PayloadHash: "sha256:test", SideEffectsBefore: before, SideEffectsAfter: after}
	}
	zero := &trace.GuardSideEffectSnapshot{}
	unsafeAfter := &trace.GuardSideEffectSnapshot{PaymentIntentCount: 1}
	parse := base("parse")
	parse.Trace.ModelDecisionTraces = []*llm.ModelDecisionTrace{{TraceID: "model-parse", Provider: "deepseek", Status: llm.TraceParseError}}
	parse.Trace.DecisionOutcomeTraces = []*trace.DecisionOutcomeTrace{outcome("parse", trace.DecisionStageParseSchema, false, false, nil, nil)}
	context := base("context")
	context.Trace.DecisionOutcomeTraces = []*trace.DecisionOutcomeTrace{outcome("context", trace.DecisionStageContextEvidence, false, false, nil, nil)}
	guard := base("guard")
	guard.Trace.DecisionOutcomeTraces = []*trace.DecisionOutcomeTrace{outcome("guard", trace.DecisionStageRuntimeGuard, true, false, zero, zero)}
	unsafe := base("unsafe")
	unsafe.Trace.DecisionOutcomeTraces = []*trace.DecisionOutcomeTrace{outcome("unsafe", trace.DecisionStageRuntimeGuard, true, false, zero, unsafeAfter)}

	metrics := Compute([]EpisodeResult{parse, context, guard, unsafe}, now)
	if metrics.GuardStages.ParseFailureNumerator != 1 || metrics.GuardStages.ParseFailureDenominator != 1 {
		t.Fatalf("parse stage was not isolated: %#v", metrics.GuardStages)
	}
	if metrics.GuardStages.ContextValidationNumerator != 1 || metrics.GuardStages.ContextValidationDenominator != 1 {
		t.Fatalf("context stage was not isolated: %#v", metrics.GuardStages)
	}
	if metrics.GuardStages.RuntimeGuardRejectionNumerator != 2 || metrics.GuardStages.RuntimeGuardRejectionDenominator != 2 {
		t.Fatalf("guard stage was not isolated: %#v", metrics.GuardStages)
	}
	if metrics.UnsafeSideEffectCount != 1 {
		t.Fatalf("unsafe side effects must require a rejected guard plus a counter delta: %d", metrics.UnsafeSideEffectCount)
	}
}

func TestIntegrityUsesRetrievalAttemptForMemoryDenominator(t *testing.T) {
	result := EpisodeResult{CaseID: "memory-miss", Mode: ModeReplay, Trace: EpisodeTrace{
		Episode:         &episode.CommerceEpisode{EpisodeID: "memory-miss", State: episode.StateFulfilled},
		MemoryUseTraces: []*memory.MemoryUseTrace{{RetrievalAttempted: true, GuardAccepted: true}},
	}}
	metrics := Compute([]EpisodeResult{result}, time.Now().UTC())
	if metrics.MemoryRetrievalAttempts != 1 || metrics.MemoryRetrievalHits != 0 || metrics.MemoryRetrievalHitRate != 0 {
		t.Fatalf("memory miss denominator was not counted: %#v", metrics)
	}
	if metrics.Headline[ModeReplay].MemoryRetrievalHitDenominator != 1 || metrics.Headline[ModeReplay].MemoryRetrievalHitNumerator != 0 {
		t.Fatalf("mode-scoped memory metrics were not counted: %#v", metrics.Headline[ModeReplay])
	}
}

func TestIntegrityDoesNotTreatCompletedAtAsTerminal(t *testing.T) {
	result := EpisodeResult{CaseID: "nonterminal", Mode: ModeReplay, CompletedAt: time.Now().UTC(), Trace: EpisodeTrace{Episode: &episode.CommerceEpisode{EpisodeID: "nonterminal", State: episode.StateInvoking}}}
	metrics := Compute([]EpisodeResult{result}, time.Now().UTC())
	if metrics.CompletedEpisodes != 0 || metrics.Headline[ModeReplay].Completed != 0 || result.Grade.Passed {
		t.Fatalf("nonterminal episode was treated as completed: %#v", metrics.Headline[ModeReplay])
	}
}

func TestFailureObservationConsistency(t *testing.T) {
	valid := []FailureInjection{
		{Kind: "merchant_transient", Configured: true},
		{Kind: "merchant_transient", Configured: true, Triggered: true, InjectionCount: 1},
	}
	for _, value := range valid {
		if err := value.ValidateObserved(); err != nil {
			t.Fatalf("valid fault observation rejected: %#v: %v", value, err)
		}
	}
	invalid := []FailureInjection{
		{Kind: "merchant_transient", Triggered: true},
		{Kind: "merchant_transient", Configured: true, InjectionCount: 1},
		{Kind: "merchant_transient", Configured: true, Triggered: true, InjectionCount: -1},
	}
	for _, value := range invalid {
		if err := value.ValidateObserved(); err == nil {
			t.Fatalf("invalid fault observation accepted: %#v", value)
		}
	}
}

func TestRequiredEconomicGradersAreDeterministic(t *testing.T) {
	badBudget := EpisodeResult{CaseID: "bad-budget", Trace: EpisodeTrace{Episode: &episode.CommerceEpisode{EpisodeID: "bad-budget", State: episode.StateFulfilled, Budget: episode.BudgetSnapshot{Currency: "USDC", BudgetLimitMinor: 100, SettledAmount: 10, ConsumedAmount: 9, AvailableBudget: 91}}}}
	grade := GradeEpisode(badBudget)
	for _, assertion := range grade.Assertions {
		if assertion.Name == "budget_consistency" && assertion.Passed {
			t.Fatal("inconsistent budget passed the required grader")
		}
	}

	goodEntitlement := EpisodeResult{CaseID: "good-entitlement", ExpectedEntitlementTxID: "tx-1", Trace: EpisodeTrace{
		Episode:        &episode.CommerceEpisode{EpisodeID: "good-entitlement", State: episode.StateFulfilled},
		Artifact:       &ArtifactSnapshot{DeliveryID: "delivery-1", PayloadHash: "sha256:payload", PaymentIntentID: "pi-1", EntitlementRef: "verification:tx-1"},
		PaymentIntents: []*payment.PaymentIntent{{IntentID: "pi-1", TxID: "tx-1", Status: payment.IntentConfirmed}},
	}}
	grade = GradeEpisode(goodEntitlement)
	for _, assertion := range grade.Assertions {
		if assertion.Name == "entitlement_tx_matches" && !assertion.Passed {
			t.Fatalf("matching entitlement transaction failed: %#v", assertion)
		}
	}
}

func TestArtifactBodyIsOptInForExternalTrace(t *testing.T) {
	artifact := &invocation.DeliveryArtifact{Body: []byte("private artifact body")}
	redacted := newArtifactSnapshot(artifact, false)
	if redacted == nil || len(redacted.Body) != 0 || redacted.ArtifactBodyIncluded || !strings.HasPrefix(redacted.BodyHash, "sha256:") {
		t.Fatalf("artifact body was not redacted by default: %#v", redacted)
	}
	included := newArtifactSnapshot(artifact, true)
	if included == nil || string(included.Body) != "private artifact body" || !included.ArtifactBodyIncluded {
		t.Fatalf("explicit artifact body opt-in was not honored: %#v", included)
	}
}

func TestIntegrityCountsOneCanonicalDecisionAttempt(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	result := benchmarkResult("canonical", ModeReplay)
	attemptID := trace.DecisionAttemptIDFor("canonical", "model-1", "proposal-1")
	result.Trace.ModelDecisionTraces = []*llm.ModelDecisionTrace{{TraceID: "model-1", DecisionAttemptID: attemptID, EpisodeID: "canonical", Provider: "deepseek", ModelRef: "deepseek-chat", ContextHash: "sha256:context", RequestStartedAt: now, ResponseReceivedAt: now.Add(20 * time.Millisecond), Status: llm.TraceSuccess}}
	result.Trace.DecisionOutcomeTraces = []*trace.DecisionOutcomeTrace{
		{DecisionOutcomeTraceID: "outcome-guard", DecisionAttemptID: attemptID, EpisodeID: "canonical", ModelDecisionTraceID: "model-1", ProposalID: "proposal-1", ProposedAction: "RETRY_SAME_MERCHANT", Stage: trace.DecisionStageRuntimeGuard, ReachedRuntimeGuard: true, GuardAccepted: false, CreatedAt: now, FactsRef: "decision-outcome-trace://outcome-guard"},
		{DecisionOutcomeTraceID: "outcome-accepted", DecisionAttemptID: attemptID, EpisodeID: "canonical", ModelDecisionTraceID: "model-1", ProposalID: "proposal-1", ProposedAction: "RETRY_SAME_MERCHANT", Stage: trace.DecisionStageAccepted, ReachedRuntimeGuard: true, GuardAccepted: true, CreatedAt: now.Add(time.Millisecond), FactsRef: "decision-outcome-trace://outcome-accepted"},
	}
	metrics := Compute([]EpisodeResult{result}, now)
	if metrics.LLMDecisionAttempts != 1 || metrics.AcceptedDecisions != 1 || metrics.RuntimeGuardRejections != 0 {
		t.Fatalf("decision attempt was counted more than once: %#v", metrics)
	}
	if metrics.LLMParsedProposalAcceptanceRate != 1 || metrics.LLMEndToEndDecisionSuccessRate != 1 {
		t.Fatalf("unexpected LLM rates: %#v", metrics)
	}
}

func TestFaultRecoverySeparatesBusinessAndOperationalRecovery(t *testing.T) {
	result := benchmarkResult("fault", ModeReplay)
	result.Failure = FailureInjection{Kind: "merchant_transient", Configured: true, Triggered: true, InjectionCount: 1}
	result.Trace.Execution = &repository.EpisodeExecutionStatus{EpisodeID: "fault", Status: repository.ExecutionCompleted, AttemptCount: 2}
	metrics := Compute([]EpisodeResult{result}, time.Now().UTC())
	if metrics.FaultTriggered != 1 || metrics.FaultTaskRecovered != 1 || metrics.FaultRecoverySuccessRate != 1 {
		t.Fatalf("fault recovery was not graded from EffectiveApplied and Grade.Passed: %#v", metrics)
	}
	if metrics.BusinessRecoveryEntered != 0 || metrics.OperationalRetryCases != 1 {
		t.Fatalf("operational retry was conflated with business recovery: %#v", metrics)
	}
}

func TestReliabilityPassKIsLiveOnlyAndAggregated(t *testing.T) {
	results := make([]EpisodeResult, 0, 9)
	for index := 1; index <= 8; index++ {
		result := benchmarkResult(fmt.Sprintf("live-%d", index), ModeLive)
		result.Suite = "live_local"
		result.Environment = "local"
		result.TaskID = "happy"
		result.TrialIndex = index
		result.RequestID = fmt.Sprintf("request-%d", index)
		results = append(results, result)
	}
	// Replay trials with the same task metadata must not create pass^k eligibility.
	replay := benchmarkResult("replay", ModeReplay)
	replay.Suite, replay.Environment, replay.TaskID, replay.TrialIndex, replay.RequestID = "live_local", "local", "happy", 1, "replay-request"
	results = append(results, replay)
	metrics := Compute(results, time.Now().UTC())
	if len(metrics.Reliability.PassP8) != 1 {
		t.Fatalf("expected one live pass^8 group: %#v", metrics.Reliability)
	}
	aggregate := metrics.Reliability.Aggregate["pass^8"]
	if aggregate.EligibleTasks != 1 || aggregate.PassingTasks != 1 || aggregate.Rate != 1 {
		t.Fatalf("unexpected pass^8 aggregate: %#v", aggregate)
	}
}

func benchmarkResult(id, mode string) EpisodeResult {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	variant := RuntimeVariant{RuntimeVersion: "test", MemoryMode: "off", RecoveryProvider: "rule", LLMProvider: "none", ModelRef: "rule-recovery"}.WithHash()
	return EpisodeResult{CaseID: id, Mode: mode, Trace: EpisodeTrace{Episode: &episode.CommerceEpisode{EpisodeID: id, State: episode.StateFulfilled, CreatedAt: now, UpdatedAt: now.Add(time.Second)}, RuntimeVariant: variant, Artifact: &ArtifactSnapshot{DeliveryID: "delivery-" + id, PayloadHash: "sha256:payload"}, Validation: &invocation.ValidationEvidence{Valid: true, ReasonCode: "VALID"}}}
}
