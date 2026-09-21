// Package observability contains read-only trace snapshots and deterministic
// benchmark aggregation. It consumes persisted facts; it never drives the
// Commerce Runtime state machine or authorizes an action.
package observability

import (
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/stablepay/commerce-runtime/internal/trace"
)

const SchemaVersion = "s11.v1"

const (
	ModeOffline = "offline"
	ModeReplay  = "replay"
	ModeLive    = "live"
)

// EpisodeTrace is the redacted, externally exportable observability view for
// one episode. Artifact bodies are omitted by default; the public export can
// opt in explicitly. Credentials, API keys, prompts and raw LLM responses are
// not part of any persisted trace type.
type EpisodeTrace struct {
	SchemaVersion          string                             `json:"schema_version"`
	Source                 string                             `json:"source"`
	SensitiveFieldsOmitted bool                               `json:"sensitive_fields_omitted"`
	Episode                *episode.CommerceEpisode           `json:"episode"`
	Events                 []*episode.EpisodeEvent            `json:"events"`
	Execution              *repository.EpisodeExecutionStatus `json:"execution,omitempty"`
	Ledger                 []*ledger.LedgerEntry              `json:"ledger"`
	PaymentIntents         []*payment.PaymentIntent           `json:"payment_intents"`
	PaymentTransportTraces []*payment.PaymentTransportTrace   `json:"payment_transport_traces"`
	ModelDecisionTraces    []*llm.ModelDecisionTrace          `json:"model_decision_traces"`
	MemoryUseTraces        []*memory.MemoryUseTrace           `json:"memory_use_traces"`
	DecisionOutcomeTraces  []*trace.DecisionOutcomeTrace      `json:"decision_outcome_traces"`
	RuntimeVariant         RuntimeVariant                     `json:"runtime_variant"`
	GuardSideEffects       *trace.GuardSideEffectSnapshot     `json:"guard_side_effects,omitempty"`
	ParentApproval         *recovery.ParentApprovalRequest    `json:"parent_approval,omitempty"`
	ParentDecision         *recovery.ParentDecisionFact       `json:"parent_decision,omitempty"`
	Artifact               *ArtifactSnapshot                  `json:"artifact,omitempty"`
	Validation             *invocation.ValidationEvidence     `json:"validation,omitempty"`
}

type ArtifactSnapshot struct {
	DeliveryID           string    `json:"delivery_id"`
	EpisodeID            string    `json:"episode_id"`
	InvocationID         string    `json:"invocation_id"`
	MerchantDID          string    `json:"merchant_did"`
	CapabilityID         string    `json:"capability_id"`
	ContentType          string    `json:"content_type"`
	PayloadRef           string    `json:"payload_ref"`
	PayloadHash          string    `json:"payload_hash"`
	Body                 []byte    `json:"body,omitempty"`
	BodyHash             string    `json:"body_hash,omitempty"`
	ArtifactBodyIncluded bool      `json:"artifact_body_included"`
	PaymentIntentID      string    `json:"payment_intent_id,omitempty"`
	EntitlementRef       string    `json:"entitlement_ref,omitempty"`
	Attempt              int       `json:"attempt"`
	HTTPStatus           int       `json:"http_status"`
	ReceivedAt           time.Time `json:"received_at"`
}

func newArtifactSnapshot(value *invocation.DeliveryArtifact, includeBody bool) *ArtifactSnapshot {
	if value == nil {
		return nil
	}
	digest := sha256.Sum256(value.Body)
	result := &ArtifactSnapshot{DeliveryID: value.DeliveryID, EpisodeID: value.EpisodeID, InvocationID: value.InvocationID, MerchantDID: value.MerchantDID, CapabilityID: value.CapabilityID, ContentType: value.ContentType, PayloadRef: value.PayloadRef, PayloadHash: value.PayloadHash, BodyHash: "sha256:" + hex.EncodeToString(digest[:]), ArtifactBodyIncluded: includeBody, PaymentIntentID: value.PaymentIntentID, EntitlementRef: value.EntitlementRef, Attempt: value.Attempt, HTTPStatus: value.HTTPStatus, ReceivedAt: value.ReceivedAt}
	if includeBody {
		result.Body = append([]byte(nil), value.Body...)
	}
	return result
}

// EpisodeResult is one line in episode_results.jsonl. The metadata is kept
// separate from the trace so a live run can prove what was requested versus
// what an external injector actually applied.
type EpisodeResult struct {
	SchemaVersion              string           `json:"schema_version"`
	CaseID                     string           `json:"case_id"`
	Seed                       int64            `json:"seed"`
	Mode                       string           `json:"mode"`              // offline, replay, live
	Ingress                    string           `json:"ingress"`           // http, mcp, cli
	MemoryMode                 string           `json:"memory_mode"`       // on, off, unknown
	RecoveryProvider           string           `json:"recovery_provider"` // llm, rule, unknown
	Path                       string           `json:"path"`              // happy, recovery, unknown
	Suite                      string           `json:"suite,omitempty"`   // regression, benchmark, live_local, real_business
	Environment                string           `json:"environment,omitempty"`
	TaskID                     string           `json:"task_id,omitempty"`
	TrialIndex                 int              `json:"trial_index,omitempty"`
	ExpectedTerminal           string           `json:"expected_terminal,omitempty"`
	ExpectedVariant            RuntimeVariant   `json:"expected_variant,omitempty"`
	ExpectedPaymentIntents     *int             `json:"expected_payment_intents,omitempty"`
	ExpectedSettlementCount    *int             `json:"expected_settlement_count,omitempty"`
	ExpectedEntitlementTxID    string           `json:"expected_entitlement_tx_id,omitempty"`
	RequirePayment             bool             `json:"require_payment,omitempty"`
	RequireRecovery            bool             `json:"require_recovery,omitempty"`
	MustNotCreateSecondPayment bool             `json:"must_not_create_second_payment,omitempty"`
	Failure                    FailureInjection `json:"failure"`
	RequestID                  string           `json:"request_id,omitempty"`
	EpisodeID                  string           `json:"episode_id,omitempty"`
	SubmittedAt                time.Time        `json:"submitted_at,omitempty"`
	CompletedAt                time.Time        `json:"completed_at,omitempty"`
	DurationMS                 float64          `json:"duration_ms,omitempty"`
	PollCount                  int              `json:"poll_count,omitempty"`
	CollectionError            string           `json:"collection_error,omitempty"`
	Grade                      GradeResult      `json:"grade"`
	Trace                      EpisodeTrace     `json:"trace"`
}

type FailureInjection struct {
	Kind           string    `json:"kind"`
	RatePercent    int       `json:"rate_percent"`
	Repeat         int       `json:"repeat"`
	Trigger        string    `json:"trigger,omitempty"`
	Configured     bool      `json:"configured"`
	Triggered      bool      `json:"triggered"`
	InjectionCount int       `json:"injection_count"`
	RequestCount   int       `json:"request_count,omitempty"`
	EligibleCount  int       `json:"eligible_count,omitempty"`
	LastInjectedAt time.Time `json:"last_injected_at,omitempty"`
	Applied        bool      `json:"applied"` // deprecated compatibility field; derived by EffectiveApplied.
	Evidence       string    `json:"evidence,omitempty"`
}

func (f FailureInjection) EffectiveApplied() bool {
	return f.Triggered && f.InjectionCount > 0
}

// ValidateObserved checks the persisted/controller portion of a fault record.
// A requested but unconfigured fault is valid; an observed injection is not
// valid unless the controller reported both configured and triggered state.
func (f FailureInjection) ValidateObserved() error {
	if f.RatePercent < 0 || f.RatePercent > 100 || f.Repeat < 0 || f.InjectionCount < 0 || f.RequestCount < 0 || f.EligibleCount < 0 {
		return fmt.Errorf("invalid failure injection counters")
	}
	if f.Triggered != (f.InjectionCount > 0) {
		return fmt.Errorf("triggered must match injection_count")
	}
	if f.InjectionCount > 0 && !f.Configured {
		return fmt.Errorf("injection_count requires configured=true")
	}
	return nil
}

type GradeAssertion struct {
	Name         string   `json:"name"`
	Passed       bool     `json:"passed"`
	Required     bool     `json:"required"`
	Expected     string   `json:"expected,omitempty"`
	Observed     string   `json:"observed,omitempty"`
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
}

type GradeResult struct {
	Passed     bool             `json:"passed"`
	Assertions []GradeAssertion `json:"assertions"`
}

type ModeHeadline struct {
	Trials                        int      `json:"trials"`
	Completed                     int      `json:"completed"`
	Successful                    int      `json:"successful"`
	TaskSuccessRate               *float64 `json:"task_success_rate"`
	TaskSuccessNumerator          int      `json:"task_success_numerator"`
	TaskSuccessDenominator        int      `json:"task_success_denominator"`
	RecoverySuccessRate           *float64 `json:"recovery_success_rate"`
	RecoverySuccessNumerator      int      `json:"recovery_success_numerator"`
	RecoverySuccessDenominator    int      `json:"recovery_success_denominator"`
	LLMProposalAcceptanceRate     *float64 `json:"llm_proposal_acceptance_rate"`
	LLMProposalNumerator          int      `json:"llm_proposal_numerator"`
	LLMProposalDenominator        int      `json:"llm_proposal_denominator"`
	GuardRejectionRate            *float64 `json:"guard_rejection_rate"`
	GuardRejectionNumerator       int      `json:"guard_rejection_numerator"`
	GuardRejectionDenominator     int      `json:"guard_rejection_denominator"`
	MemoryRetrievalHitRate        *float64 `json:"memory_retrieval_hit_rate"`
	MemoryRetrievalHitNumerator   int      `json:"memory_retrieval_hit_numerator"`
	MemoryRetrievalHitDenominator int      `json:"memory_retrieval_hit_denominator"`
	LatencyP50MS                  *float64 `json:"latency_p50_ms"`
	LatencyP95MS                  *float64 `json:"latency_p95_ms"`
	DescriptiveOnly               bool     `json:"descriptive_only"`
}

type GuardStageMetrics struct {
	TransportFailureRate             *float64 `json:"transport_failure_rate"`
	TransportFailureNumerator        int      `json:"transport_failure_numerator"`
	TransportFailureDenominator      int      `json:"transport_failure_denominator"`
	ParseFailureRate                 *float64 `json:"parse_failure_rate"`
	ParseFailureNumerator            int      `json:"parse_failure_numerator"`
	ParseFailureDenominator          int      `json:"parse_failure_denominator"`
	ContextValidationRejectionRate   *float64 `json:"context_validation_rejection_rate"`
	ContextValidationNumerator       int      `json:"context_validation_numerator"`
	ContextValidationDenominator     int      `json:"context_validation_denominator"`
	RuntimeGuardRejectionRate        *float64 `json:"runtime_guard_rejection_rate"`
	RuntimeGuardRejectionNumerator   int      `json:"runtime_guard_rejection_numerator"`
	RuntimeGuardRejectionDenominator int      `json:"runtime_guard_rejection_denominator"`
}

type ReliabilityMetrics struct {
	PassP1    map[string]int            `json:"pass^1"`
	PassP2    map[string]int            `json:"pass^2"`
	PassP4    map[string]int            `json:"pass^4"`
	PassP8    map[string]int            `json:"pass^8"`
	Aggregate map[string]PassKAggregate `json:"aggregate"`
}

type PassKAggregate struct {
	EligibleTasks int     `json:"eligible_tasks"`
	PassingTasks  int     `json:"passing_tasks"`
	Rate          float64 `json:"rate"`
}

type FaultCoverage struct {
	Requested               int `json:"requested"`
	Configured              int `json:"configured"`
	Triggered               int `json:"triggered"`
	TaskRecovered           int `json:"task_recovered"`
	BusinessRecoveryEntered int `json:"business_recovery_entered"`
	OperationalRetryCases   int `json:"operational_retry_cases"`
	Recovered               int `json:"recovered"`
}

type Metrics struct {
	SchemaVersion                          string                   `json:"schema_version"`
	GeneratedAt                            time.Time                `json:"generated_at"`
	TotalEpisodes                          int                      `json:"total_episodes"`
	CompletedEpisodes                      int                      `json:"completed_episodes"`
	OperationalCompletedEpisodes           int                      `json:"operational_completed_episodes"`
	SuccessfulEpisodes                     int                      `json:"successful_episodes"`
	RecoveryEpisodes                       int                      `json:"recovery_episodes"`
	RecoverySuccesses                      int                      `json:"recovery_successes"`
	TaskSuccessRate                        float64                  `json:"task_success_rate"`
	RecoverySuccessRate                    float64                  `json:"recovery_success_rate"`
	FaultRecoverySuccessRate               float64                  `json:"fault_recovery_success_rate"`
	BusinessRecoverySuccessRate            float64                  `json:"business_recovery_success_rate"`
	FaultTriggered                         int                      `json:"fault_triggered"`
	FaultTaskRecovered                     int                      `json:"fault_task_recovered"`
	BusinessRecoveryEntered                int                      `json:"business_recovery_entered"`
	OperationalRetryCases                  int                      `json:"operational_retry_cases"`
	LLMProposals                           int                      `json:"llm_proposals"`
	LLMAccepted                            int                      `json:"llm_accepted"`
	LLMProposalAcceptanceRate              float64                  `json:"llm_proposal_acceptance_rate"`
	LLMDecisionAttempts                    int                      `json:"llm_decision_attempts"`
	LLMTransportFailures                   int                      `json:"llm_transport_failures"`
	LLMParseFailures                       int                      `json:"llm_parse_failures"`
	ContextEvidenceRejections              int                      `json:"context_evidence_rejections"`
	ContextMemoryRejections                int                      `json:"context_memory_rejections"`
	RuntimeGuardRejections                 int                      `json:"runtime_guard_rejections"`
	AcceptedDecisions                      int                      `json:"accepted_decisions"`
	FallbackDecisions                      int                      `json:"fallback_decisions"`
	LLMParsedProposalAcceptanceRate        float64                  `json:"llm_parsed_proposal_acceptance_rate"`
	LLMEndToEndDecisionSuccessRate         float64                  `json:"llm_end_to_end_decision_success_rate"`
	GuardDecisions                         int                      `json:"guard_decisions"`
	GuardRejections                        int                      `json:"guard_rejections"`
	GuardRejectionRate                     float64                  `json:"guard_rejection_rate"`
	UnsafeSideEffectCount                  int                      `json:"unsafe_side_effect_count"`
	MemoryRetrievalAttempts                int                      `json:"memory_retrieval_attempts"`
	MemoryRetrievalHits                    int                      `json:"memory_retrieval_hits"`
	MemoryRetrievalHitRate                 float64                  `json:"memory_retrieval_hit_rate"`
	MemoryCitationCount                    int                      `json:"memory_citation_count"`
	MemoryCitationRate                     float64                  `json:"memory_citation_rate"`
	MemoryGuidedActions                    int                      `json:"-"` // deprecated; use MemoryCitedActions.
	MemoryGuidedActionSuccess              int                      `json:"-"` // deprecated descriptive alias.
	MemoryCitedActions                     int                      `json:"memory_cited_actions"`
	MemoryCitedActionsInSuccessfulEpisodes int                      `json:"memory_cited_actions_in_successful_episodes"`
	DuplicateSettlementCount               int                      `json:"duplicate_settlement_count"`
	PaymentRetryCount                      int                      `json:"-"` // deprecated; use business_payment_resubmission_count.
	DeliveryRetryCount                     int                      `json:"delivery_retry_count"`
	EpisodeLatencyP50MS                    float64                  `json:"episode_latency_p50_ms"`
	EpisodeLatencyP95MS                    float64                  `json:"episode_latency_p95_ms"`
	LLMLatencyP50MS                        float64                  `json:"llm_latency_p50_ms"`
	LLMLatencyP95MS                        float64                  `json:"llm_latency_p95_ms"`
	PaymentConfirmationLatencyP50MS        float64                  `json:"payment_confirmation_latency_p50_ms"`
	PaymentConfirmationLatencyP95MS        float64                  `json:"payment_confirmation_latency_p95_ms"`
	RecoveryLatencyP50MS                   float64                  `json:"recovery_latency_p50_ms"`
	RecoveryLatencyP95MS                   float64                  `json:"recovery_latency_p95_ms"`
	FailureInjectionRecoverySteps          map[string]StepSummary   `json:"failure_injection_recovery_steps"`
	ExperimentMatrix                       ExperimentMatrix         `json:"experiment_matrix"`
	Evidence                               EvidenceSummary          `json:"evidence"`
	Headline                               map[string]ModeHeadline  `json:"headline"`
	GuardStages                            GuardStageMetrics        `json:"guard_stages"`
	Reliability                            ReliabilityMetrics       `json:"reliability"`
	FaultCoverage                          map[string]FaultCoverage `json:"fault_coverage"`
	BusinessPaymentResubmissionCount       int                      `json:"business_payment_resubmission_count"`
	PaymentExactRedeliveryCount            int                      `json:"payment_exact_redelivery_count"`
}

type StepSummary struct {
	Samples                 int     `json:"samples"`
	Recovered               int     `json:"recovered"`
	TaskRecovered           int     `json:"task_recovered"`
	BusinessRecoveryEntered int     `json:"business_recovery_entered"`
	OperationalRetryCases   int     `json:"operational_retry_cases"`
	P50                     float64 `json:"p50"`
	P95                     float64 `json:"p95"`
	Mean                    float64 `json:"mean"`
	AppliedCount            int     `json:"applied_count"`
}

type EvidenceSummary struct {
	Modes                  map[string]int `json:"modes"`
	Environments           map[string]int `json:"environments"`
	SubmittedTrials        int            `json:"submitted_trials"`
	ValidTrials            int            `json:"valid_trials"`
	CollectionErrorCount   int            `json:"collection_error_count"`
	CollectionErrorRate    float64        `json:"collection_error_rate"`
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

func computeLegacy(results []EpisodeResult, now time.Time) Metrics {
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
		grade := result.Grade
		if len(grade.Assertions) == 0 {
			grade = GradeEpisode(result)
		}
		completed := episode.IsTerminal(episode.State(state))
		if completed {
			metrics.CompletedEpisodes++
		}
		success := grade.Passed
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
			if result.Failure.EffectiveApplied() {
				metrics.Evidence.AppliedFaultCases++
			}
			key := result.Failure.Kind
			stepSamples[key]++
			if result.Failure.EffectiveApplied() {
				stepApplied[key]++
			}
			if recovery {
				stepValues[key] = append(stepValues[key], float64(recoverySteps(result.Trace.Events)))
				if success {
					stepRecovered[key]++
				}
			}
		}
		updateCellLegacy(metrics.ExperimentMatrix.MemoryMode, matrixLatencies["memory"], normalizedKey(result.MemoryMode, "unknown"), result, success, recovery)
		updateCellLegacy(metrics.ExperimentMatrix.RecoveryProvider, matrixLatencies["provider"], normalizedKey(result.RecoveryProvider, "unknown"), result, success, recovery)
		failureRate := fmt.Sprintf("%d%%", result.Failure.RatePercent)
		updateCellLegacy(metrics.ExperimentMatrix.FailureRate, matrixLatencies["failure"], failureRate, result, success, recovery)
		updateCellLegacy(metrics.ExperimentMatrix.Path, matrixLatencies["path"], normalizedKey(result.Path, "unknown"), result, success, recovery)
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
	applyIntegrityMetrics(results, &metrics)
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

func updateCellLegacy(cells map[string]Cell, latencySamples map[string][]float64, key string, result EpisodeResult, success, recovery bool) {
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
