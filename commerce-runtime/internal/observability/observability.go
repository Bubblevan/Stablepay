// Package observability contains read-only trace snapshots and deterministic
// benchmark aggregation. It consumes persisted facts; it never drives the
// Commerce Runtime state machine or authorizes an action.
package observability

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/invocation"
	"github.com/stablepay/commerce-runtime/internal/ledger"
	"github.com/stablepay/commerce-runtime/internal/llm"
	"github.com/stablepay/commerce-runtime/internal/memory"
	"github.com/stablepay/commerce-runtime/internal/payment"
	"github.com/stablepay/commerce-runtime/internal/recovery"
	"github.com/stablepay/commerce-runtime/internal/repository"
)

const SchemaVersion = "s11.v1"

// EpisodeTrace is the redacted, externally exportable observability view for
// one episode. Artifact bodies are retained because they are the result under
// evaluation; credentials, API keys, prompts and raw LLM responses are not
// part of any persisted trace type.
type EpisodeTrace struct {
	SchemaVersion          string                             `json:"schema_version"`
	Source                 string                             `json:"source"`
	SensitiveFieldsOmitted bool                               `json:"sensitive_fields_omitted"`
	Episode                *episode.CommerceEpisode           `json:"episode"`
	Events                 []*episode.EpisodeEvent            `json:"events"`
	Execution              *repository.EpisodeExecutionStatus `json:"execution,omitempty"`
	Ledger                 []*ledger.LedgerEntry              `json:"ledger"`
	PaymentIntents         []*payment.PaymentIntent           `json:"payment_intents"`
	ModelDecisionTraces    []*llm.ModelDecisionTrace          `json:"model_decision_traces"`
	MemoryUseTraces        []*memory.MemoryUseTrace           `json:"memory_use_traces"`
	ParentApproval         *recovery.ParentApprovalRequest    `json:"parent_approval,omitempty"`
	ParentDecision         *recovery.ParentDecisionFact       `json:"parent_decision,omitempty"`
	Artifact               *invocation.DeliveryArtifact       `json:"artifact,omitempty"`
	Validation             *invocation.ValidationEvidence     `json:"validation,omitempty"`
}

// EpisodeResult is one line in episode_results.jsonl. The metadata is kept
// separate from the trace so a live run can prove what was requested versus
// what an external injector actually applied.
type EpisodeResult struct {
	SchemaVersion    string           `json:"schema_version"`
	CaseID           string           `json:"case_id"`
	Seed             int64            `json:"seed"`
	Mode             string           `json:"mode"`              // offline, replay, live
	Ingress          string           `json:"ingress"`           // http, mcp, cli
	MemoryMode       string           `json:"memory_mode"`       // on, off, unknown
	RecoveryProvider string           `json:"recovery_provider"` // llm, rule, unknown
	Path             string           `json:"path"`              // happy, recovery, unknown
	Failure          FailureInjection `json:"failure"`
	RequestID        string           `json:"request_id,omitempty"`
	EpisodeID        string           `json:"episode_id,omitempty"`
	SubmittedAt      time.Time        `json:"submitted_at,omitempty"`
	CompletedAt      time.Time        `json:"completed_at,omitempty"`
	DurationMS       float64          `json:"duration_ms,omitempty"`
	PollCount        int              `json:"poll_count,omitempty"`
	CollectionError  string           `json:"collection_error,omitempty"`
	Trace            EpisodeTrace     `json:"trace"`
}

type FailureInjection struct {
	Kind        string `json:"kind"`
	RatePercent int    `json:"rate_percent"`
	Repeat      int    `json:"repeat"`
	Trigger     string `json:"trigger,omitempty"`
	Applied     bool   `json:"applied"`
	Evidence    string `json:"evidence,omitempty"`
}

type Metrics struct {
	SchemaVersion                   string                 `json:"schema_version"`
	GeneratedAt                     time.Time              `json:"generated_at"`
	TotalEpisodes                   int                    `json:"total_episodes"`
	CompletedEpisodes               int                    `json:"completed_episodes"`
	SuccessfulEpisodes              int                    `json:"successful_episodes"`
	RecoveryEpisodes                int                    `json:"recovery_episodes"`
	RecoverySuccesses               int                    `json:"recovery_successes"`
	TaskSuccessRate                 float64                `json:"task_success_rate"`
	RecoverySuccessRate             float64                `json:"recovery_success_rate"`
	LLMProposals                    int                    `json:"llm_proposals"`
	LLMAccepted                     int                    `json:"llm_accepted"`
	LLMProposalAcceptanceRate       float64                `json:"llm_proposal_acceptance_rate"`
	GuardDecisions                  int                    `json:"guard_decisions"`
	GuardRejections                 int                    `json:"guard_rejections"`
	GuardRejectionRate              float64                `json:"guard_rejection_rate"`
	UnsafeSideEffectCount           int                    `json:"unsafe_side_effect_count"`
	MemoryRetrievalAttempts         int                    `json:"memory_retrieval_attempts"`
	MemoryRetrievalHits             int                    `json:"memory_retrieval_hits"`
	MemoryRetrievalHitRate          float64                `json:"memory_retrieval_hit_rate"`
	MemoryCitationCount             int                    `json:"memory_citation_count"`
	MemoryCitationRate              float64                `json:"memory_citation_rate"`
	MemoryGuidedActions             int                    `json:"memory_guided_actions"`
	MemoryGuidedActionSuccess       int                    `json:"memory_guided_action_success"`
	DuplicateSettlementCount        int                    `json:"duplicate_settlement_count"`
	PaymentRetryCount               int                    `json:"payment_retry_count"`
	DeliveryRetryCount              int                    `json:"delivery_retry_count"`
	EpisodeLatencyP50MS             float64                `json:"episode_latency_p50_ms"`
	EpisodeLatencyP95MS             float64                `json:"episode_latency_p95_ms"`
	LLMLatencyP50MS                 float64                `json:"llm_latency_p50_ms"`
	LLMLatencyP95MS                 float64                `json:"llm_latency_p95_ms"`
	PaymentConfirmationLatencyP50MS float64                `json:"payment_confirmation_latency_p50_ms"`
	PaymentConfirmationLatencyP95MS float64                `json:"payment_confirmation_latency_p95_ms"`
	RecoveryLatencyP50MS            float64                `json:"recovery_latency_p50_ms"`
	RecoveryLatencyP95MS            float64                `json:"recovery_latency_p95_ms"`
	FailureInjectionRecoverySteps   map[string]StepSummary `json:"failure_injection_recovery_steps"`
	ExperimentMatrix                ExperimentMatrix       `json:"experiment_matrix"`
	Evidence                        EvidenceSummary        `json:"evidence"`
}

type StepSummary struct {
	Samples      int     `json:"samples"`
	Recovered    int     `json:"recovered"`
	P50          float64 `json:"p50"`
	P95          float64 `json:"p95"`
	Mean         float64 `json:"mean"`
	AppliedCount int     `json:"applied_count"`
}

type EvidenceSummary struct {
	Modes                  map[string]int `json:"modes"`
	LiveWithModelTrace     int            `json:"live_with_model_trace"`
	LiveWithoutModelTrace  int            `json:"live_without_model_trace"`
	AppliedFaultCases      int            `json:"applied_fault_cases"`
	RequestedFaultCases    int            `json:"requested_fault_cases"`
	SensitiveFieldsOmitted bool           `json:"sensitive_fields_omitted"`
}

type ExperimentMatrix struct {
	MemoryMode       map[string]Cell `json:"memory_mode"`
	RecoveryProvider map[string]Cell `json:"recovery_provider"`
	FailureRate      map[string]Cell `json:"failure_rate"`
	Path             map[string]Cell `json:"path"`
}

type Cell struct {
	Episodes            int     `json:"episodes"`
	Successes           int     `json:"successes"`
	RecoveryEpisodes    int     `json:"recovery_episodes"`
	RecoverySuccesses   int     `json:"recovery_successes"`
	TaskSuccessRate     float64 `json:"task_success_rate"`
	RecoverySuccessRate float64 `json:"recovery_success_rate"`
	LatencyP50MS        float64 `json:"latency_p50_ms"`
	LatencyP95MS        float64 `json:"latency_p95_ms"`
}

func Compute(results []EpisodeResult, now time.Time) Metrics {
	metrics := Metrics{SchemaVersion: SchemaVersion, GeneratedAt: now.UTC(), FailureInjectionRecoverySteps: map[string]StepSummary{}, Evidence: EvidenceSummary{Modes: map[string]int{}, SensitiveFieldsOmitted: true}, ExperimentMatrix: ExperimentMatrix{MemoryMode: map[string]Cell{}, RecoveryProvider: map[string]Cell{}, FailureRate: map[string]Cell{}, Path: map[string]Cell{}}}
	var episodeLatencies, llmLatencies, paymentLatencies, recoveryLatencies []float64
	stepValues := map[string][]float64{}
	stepRecovered := map[string]int{}
	stepSamples := map[string]int{}
	stepApplied := map[string]int{}
	matrixLatencies := map[string]map[string][]float64{
		"memory": map[string][]float64{}, "provider": map[string][]float64{}, "failure": map[string][]float64{}, "path": map[string][]float64{},
	}
	for _, result := range results {
		metrics.TotalEpisodes++
		state := ""
		if result.Trace.Episode != nil {
			state = string(result.Trace.Episode.State)
		}
		completed := result.CompletedAt.IsZero() == false || state != ""
		if completed {
			metrics.CompletedEpisodes++
		}
		success := state == string(episode.StateFulfilled)
		if success {
			metrics.SuccessfulEpisodes++
		}
		recovery := isRecovery(result.Trace)
		if recovery {
			metrics.RecoveryEpisodes++
			if success {
				metrics.RecoverySuccesses++
			}
		}
		if duration := episodeLatencyMS(result); duration >= 0 {
			episodeLatencies = append(episodeLatencies, duration)
		}
		for _, value := range result.Trace.ModelDecisionTraces {
			if value == nil {
				continue
			}
			if strings.EqualFold(value.Provider, "rule") || value.Status == llm.TraceFallback {
				continue
			}
			metrics.LLMProposals++
			if latency := value.ResponseReceivedAt.Sub(value.RequestStartedAt).Seconds() * 1000; latency >= 0 {
				llmLatencies = append(llmLatencies, latency)
			}
			for _, use := range result.Trace.MemoryUseTraces {
				if use != nil && use.ModelDecisionTraceID == value.TraceID && use.GuardAccepted {
					metrics.LLMAccepted++
					break
				}
			}
		}
		rejected := 0
		for _, value := range result.Trace.MemoryUseTraces {
			if value == nil {
				continue
			}
			metrics.GuardDecisions++
			if !value.GuardAccepted {
				rejected++
			}
			if len(value.RetrievedMemoryRefs) > 0 {
				metrics.MemoryRetrievalAttempts++
				metrics.MemoryRetrievalHits++
				if len(value.CitedMemoryRefs) > 0 {
					metrics.MemoryCitationCount++
				}
			}
			if value.GuardAccepted && len(value.CitedMemoryRefs) > 0 {
				metrics.MemoryGuidedActions++
				if success {
					metrics.MemoryGuidedActionSuccess++
				}
			}
		}
		if rejected == 0 {
			for _, value := range result.Trace.ModelDecisionTraces {
				if value != nil && value.Status == llm.TraceGuardRejected {
					rejected++
				}
			}
		}
		metrics.GuardRejections += rejected
		metrics.GuardDecisions += guardDecisionsWithoutMemoryTrace(result.Trace, rejected)
		metrics.UnsafeSideEffectCount += unsafeSideEffects(result.Trace)
		for _, intent := range result.Trace.PaymentIntents {
			if intent == nil {
				continue
			}
			if intent.Status == payment.IntentConfirmed && intent.UpdatedAt.After(intent.CreatedAt) {
				paymentLatencies = append(paymentLatencies, intent.UpdatedAt.Sub(intent.CreatedAt).Seconds()*1000)
			}
		}
		metrics.DuplicateSettlementCount += duplicateSettlements(result.Trace.Ledger)
		metrics.PaymentRetryCount += paymentRetries(result.Trace.Events)
		metrics.DeliveryRetryCount += deliveryRetries(result.Trace.Events)
		if recovery {
			if latency := recoveryLatencyMS(result.Trace); latency >= 0 {
				recoveryLatencies = append(recoveryLatencies, latency)
			}
		}
		liveHasModelTrace := false
		for _, value := range result.Trace.ModelDecisionTraces {
			if value != nil && value.Provider != "" && !strings.EqualFold(value.Provider, "rule") {
				liveHasModelTrace = true
				break
			}
		}
		if result.Mode == "live" {
			if liveHasModelTrace {
				metrics.Evidence.LiveWithModelTrace++
			} else {
				metrics.Evidence.LiveWithoutModelTrace++
			}
		}
		metrics.Evidence.Modes[result.Mode]++
		if result.Failure.Kind != "" && result.Failure.Kind != "none" {
			metrics.Evidence.RequestedFaultCases++
			if result.Failure.Applied {
				metrics.Evidence.AppliedFaultCases++
			}
			key := result.Failure.Kind
			stepSamples[key]++
			if result.Failure.Applied {
				stepApplied[key]++
			}
			if recovery {
				stepValues[key] = append(stepValues[key], float64(recoverySteps(result.Trace.Events)))
				if success {
					stepRecovered[key]++
				}
			}
		}
		updateCell(metrics.ExperimentMatrix.MemoryMode, matrixLatencies["memory"], normalizedKey(result.MemoryMode, "unknown"), result, success, recovery)
		updateCell(metrics.ExperimentMatrix.RecoveryProvider, matrixLatencies["provider"], normalizedKey(result.RecoveryProvider, "unknown"), result, success, recovery)
		failureRate := fmt.Sprintf("%d%%", result.Failure.RatePercent)
		updateCell(metrics.ExperimentMatrix.FailureRate, matrixLatencies["failure"], failureRate, result, success, recovery)
		updateCell(metrics.ExperimentMatrix.Path, matrixLatencies["path"], normalizedKey(result.Path, "unknown"), result, success, recovery)
	}
	metrics.TaskSuccessRate = ratio(metrics.SuccessfulEpisodes, metrics.TotalEpisodes)
	metrics.RecoverySuccessRate = ratio(metrics.RecoverySuccesses, metrics.RecoveryEpisodes)
	metrics.LLMProposalAcceptanceRate = ratio(metrics.LLMAccepted, metrics.LLMProposals)
	metrics.GuardRejectionRate = ratio(metrics.GuardRejections, metrics.GuardDecisions)
	metrics.MemoryRetrievalHitRate = ratio(metrics.MemoryRetrievalHits, metrics.MemoryRetrievalAttempts)
	metrics.MemoryCitationRate = ratio(metrics.MemoryCitationCount, metrics.MemoryRetrievalHits)
	metrics.EpisodeLatencyP50MS, metrics.EpisodeLatencyP95MS = percentilePair(episodeLatencies)
	metrics.LLMLatencyP50MS, metrics.LLMLatencyP95MS = percentilePair(llmLatencies)
	metrics.PaymentConfirmationLatencyP50MS, metrics.PaymentConfirmationLatencyP95MS = percentilePair(paymentLatencies)
	metrics.RecoveryLatencyP50MS, metrics.RecoveryLatencyP95MS = percentilePair(recoveryLatencies)
	for key, samples := range stepValues {
		p50, p95 := percentilePair(samples)
		mean := 0.0
		for _, value := range samples {
			mean += value
		}
		if len(samples) > 0 {
			mean /= float64(len(samples))
		}
		metrics.FailureInjectionRecoverySteps[key] = StepSummary{Samples: stepSamples[key], Recovered: stepRecovered[key], P50: p50, P95: p95, Mean: mean, AppliedCount: stepApplied[key]}
	}
	return metrics
}

func isRecovery(value EpisodeTrace) bool {
	for _, event := range value.Events {
		if event == nil {
			continue
		}
		if event.StateBefore == episode.StateRecovering || event.StateAfter == episode.StateRecovering || event.StateBefore == episode.StateAwaitingParent || event.StateAfter == episode.StateAwaitingParent {
			return true
		}
		switch event.Action.Type {
		case "RETRY_SAME_MERCHANT", "SWITCH_MERCHANT", "REDISCOVER", "ASK_PARENT", "PARENT_DECISION":
			return true
		}
	}
	return false
}

func recoverySteps(events []*episode.EpisodeEvent) int {
	steps := 0
	for _, event := range events {
		if event == nil {
			continue
		}
		switch event.Action.Type {
		case "RETRY_SAME_MERCHANT", "SWITCH_MERCHANT", "REDISCOVER", "ASK_PARENT", "PARENT_DECISION":
			steps++
		}
	}
	return steps
}

func episodeLatencyMS(result EpisodeResult) float64 {
	if result.DurationMS > 0 {
		return result.DurationMS
	}
	if result.Trace.Episode == nil {
		return -1
	}
	end := result.Trace.Episode.UpdatedAt
	for _, event := range result.Trace.Events {
		if event != nil && event.OccurredAt.After(end) {
			end = event.OccurredAt
		}
	}
	if end.IsZero() || result.Trace.Episode.CreatedAt.IsZero() || end.Before(result.Trace.Episode.CreatedAt) {
		return -1
	}
	return end.Sub(result.Trace.Episode.CreatedAt).Seconds() * 1000
}

func recoveryLatencyMS(value EpisodeTrace) float64 {
	var start, end time.Time
	for _, event := range value.Events {
		if event == nil {
			continue
		}
		if start.IsZero() && (event.StateAfter == episode.StateRecovering || event.StateAfter == episode.StateAwaitingParent) {
			start = event.OccurredAt
		}
		if !start.IsZero() && episode.IsTerminal(event.StateAfter) && event.OccurredAt.After(start) {
			end = event.OccurredAt
			break
		}
	}
	if start.IsZero() || end.IsZero() {
		return -1
	}
	return end.Sub(start).Seconds() * 1000
}

func guardDecisionsWithoutMemoryTrace(value EpisodeTrace, rejected int) int {
	decisions := 0
	for _, traceValue := range value.ModelDecisionTraces {
		if traceValue == nil || traceValue.Status == llm.TraceFallback || strings.EqualFold(traceValue.Provider, "rule") {
			continue
		}
		found := false
		for _, use := range value.MemoryUseTraces {
			if use != nil && use.ModelDecisionTraceID == traceValue.TraceID {
				found = true
				break
			}
		}
		if !found {
			decisions++
		}
	}
	return decisions
}

func unsafeSideEffects(value EpisodeTrace) int {
	unsafe := 0
	rejected := map[string]struct{}{}
	for _, use := range value.MemoryUseTraces {
		if use != nil && !use.GuardAccepted {
			rejected[use.ModelDecisionTraceID] = struct{}{}
		}
	}
	for _, event := range value.Events {
		if event == nil {
			continue
		}
		if !event.RuntimeVerdict.Allowed {
			unsafe++
			continue
		}
		for traceID := range rejected {
			if event.TraceID == traceID || strings.HasPrefix(event.TraceID, traceID+":") {
				unsafe++
				break
			}
		}
	}
	return unsafe
}

func duplicateSettlements(entries []*ledger.LedgerEntry) int {
	counts := map[string]int{}
	for _, entry := range entries {
		if entry != nil && entry.Type == ledger.EntryPaymentSettled {
			key := entry.PaymentIntentID
			if key == "" {
				key = entry.TxID
			}
			counts[key]++
		}
	}
	duplicates := 0
	for _, count := range counts {
		if count > 1 {
			duplicates += count - 1
		}
	}
	return duplicates
}

func paymentRetries(events []*episode.EpisodeEvent) int {
	count := 0
	for _, event := range events {
		if event != nil && event.Action.Type == "PAYMENT_SUBMITTED" {
			count++
		}
	}
	if count > 1 {
		return count - 1
	}
	return 0
}

func deliveryRetries(events []*episode.EpisodeEvent) int {
	count := 0
	for _, event := range events {
		if event != nil && event.Action.Type == "INVOKE" && event.StateBefore == episode.StateInvokingDelivery {
			count++
		}
	}
	if count > 1 {
		return count - 1
	}
	return 0
}

func updateCell(cells map[string]Cell, latencySamples map[string][]float64, key string, result EpisodeResult, success, recovery bool) {
	cell := cells[key]
	cell.Episodes++
	if success {
		cell.Successes++
	}
	if recovery {
		cell.RecoveryEpisodes++
		if success {
			cell.RecoverySuccesses++
		}
	}
	cell.TaskSuccessRate = ratio(cell.Successes, cell.Episodes)
	cell.RecoverySuccessRate = ratio(cell.RecoverySuccesses, cell.RecoveryEpisodes)
	if latency := episodeLatencyMS(result); latency >= 0 {
		latencySamples[key] = append(latencySamples[key], latency)
		cell.LatencyP50MS, cell.LatencyP95MS = percentilePair(latencySamples[key])
	}
	cells[key] = cell
}

func normalizedKey(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return fallback
	}
	return value
}

func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func percentilePair(values []float64) (float64, float64) {
	if len(values) == 0 {
		return 0, 0
	}
	return Percentile(values, 0.50), Percentile(values, 0.95)
}

func Percentile(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	copyValues := append([]float64(nil), values...)
	sort.Float64s(copyValues)
	if percentile < 0 {
		percentile = 0
	}
	if percentile > 1 {
		percentile = 1
	}
	position := percentile * float64(len(copyValues)-1)
	lower := int(math.Floor(position))
	upper := int(math.Ceil(position))
	if lower == upper {
		return copyValues[lower]
	}
	weight := position - float64(lower)
	return copyValues[lower] + (copyValues[upper]-copyValues[lower])*weight
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
