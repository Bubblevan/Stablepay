package observability

import (
	"sort"
	"strings"

	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/llm"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

type integrityAccumulator struct {
	trials, completed, successful                           int
	recoveryTrials, recoverySuccess                         int
	llm, llmAccepted                                        int
	guard, guardRejected                                    int
	memoryAttempted, memoryHits                             int
	memoryCitations, memoryCitedActions, memoryCitedSuccess int
	latencies                                               []float64
}

func applyIntegrityMetrics(results []EpisodeResult, metrics *Metrics) {
	accumulators := map[string]*integrityAccumulator{"all": {}, ModeLive: {}, ModeReplay: {}, ModeOffline: {}}
	metrics.Headline = map[string]ModeHeadline{}
	metrics.FaultCoverage = map[string]FaultCoverage{}
	metrics.Reliability = ReliabilityMetrics{PassP1: map[string]int{}, PassP2: map[string]int{}, PassP4: map[string]int{}, PassP8: map[string]int{}}
	metrics.CompletedEpisodes = 0
	metrics.OperationalCompletedEpisodes = 0
	metrics.SuccessfulEpisodes = 0
	metrics.RecoveryEpisodes = 0
	metrics.RecoverySuccesses = 0
	metrics.LLMProposals = 0
	metrics.LLMAccepted = 0
	metrics.GuardDecisions = 0
	metrics.GuardRejections = 0
	metrics.UnsafeSideEffectCount = 0
	metrics.GuardStages = GuardStageMetrics{}
	metrics.BusinessPaymentResubmissionCount = 0
	metrics.PaymentExactRedeliveryCount = 0
	contextRejections, contextDecisions := 0, 0
	runtimeGuardRejected, runtimeGuardReached := 0, 0
	for _, result := range results {
		grade := result.Grade
		if len(grade.Assertions) == 0 {
			grade = GradeEpisode(result)
		}
		state := result.TraceState()
		completed := episode.IsTerminal(state)
		success := grade.Passed
		recovery := isRecovery(result.Trace)
		for _, key := range []string{"all", strings.ToLower(result.Mode)} {
			acc, ok := accumulators[key]
			if !ok {
				continue
			}
			acc.trials++
			if completed {
				acc.completed++
			}
			if success {
				acc.successful++
			}
			if recovery {
				acc.recoveryTrials++
				if success {
					acc.recoverySuccess++
				}
			}
			if latency := episodeLatencyMS(result); latency >= 0 {
				acc.latencies = append(acc.latencies, latency)
			}
		}
		if completed {
			metrics.CompletedEpisodes++
		}
		if result.Trace.Execution != nil && strings.EqualFold(string(result.Trace.Execution.Status), "COMPLETED") {
			metrics.OperationalCompletedEpisodes++
		}
		if success {
			metrics.SuccessfulEpisodes++
		}
		if recovery {
			metrics.RecoveryEpisodes++
			if success {
				metrics.RecoverySuccesses++
			}
		}
		if result.Failure.EffectiveApplied() {
			coverage := metrics.FaultCoverage[result.Failure.Kind]
			coverage.Triggered++
			if success && recovery {
				coverage.Recovered++
			}
			metrics.FaultCoverage[result.Failure.Kind] = coverage
		}
		if result.Failure.Configured {
			coverage := metrics.FaultCoverage[result.Failure.Kind]
			coverage.Configured++
			metrics.FaultCoverage[result.Failure.Kind] = coverage
		}
		for _, model := range result.Trace.ModelDecisionTraces {
			if model == nil || strings.EqualFold(model.Provider, "rule") {
				continue
			}
			metrics.GuardStages = addModelStage(metrics.GuardStages, model)
			for _, key := range []string{"all", strings.ToLower(result.Mode)} {
				if acc := accumulators[key]; acc != nil {
					acc.llm++
				}
			}
		}
		for _, outcome := range result.Trace.DecisionOutcomeTraces {
			if outcome == nil {
				continue
			}
			if outcome.Stage == trace.DecisionStageContextEvidence || outcome.Stage == trace.DecisionStageContextMemory {
				contextDecisions++
				if !outcome.GuardAccepted {
					contextRejections++
				}
			}
			if outcome.ReachedRuntimeGuard {
				runtimeGuardReached++
				if !outcome.GuardAccepted {
					runtimeGuardRejected++
				}
				for _, key := range []string{"all", strings.ToLower(result.Mode)} {
					if acc := accumulators[key]; acc != nil {
						acc.guard++
						if !outcome.GuardAccepted {
							acc.guardRejected++
						}
					}
				}
				if !outcome.GuardAccepted && sideEffectDelta(outcome).AnyIncrease() {
					metrics.UnsafeSideEffectCount++
				}
			}
			if outcome.Stage == trace.DecisionStageAccepted && isLLMOutcome(result.Trace, outcome) {
				for _, key := range []string{"all", strings.ToLower(result.Mode)} {
					if acc := accumulators[key]; acc != nil {
						acc.llmAccepted++
					}
				}
			}
		}
		for _, use := range result.Trace.MemoryUseTraces {
			if use == nil {
				continue
			}
			if use.RetrievalAttempted {
				for _, key := range []string{"all", strings.ToLower(result.Mode)} {
					if acc := accumulators[key]; acc != nil {
						acc.memoryAttempted++
						if len(use.RetrievedMemoryRefs) > 0 {
							acc.memoryHits++
						}
					}
				}
				if len(use.CitedMemoryRefs) > 0 {
					for _, key := range []string{"all", strings.ToLower(result.Mode)} {
						if acc := accumulators[key]; acc != nil {
							acc.memoryCitations++
						}
					}
				}
			}
			if len(use.CitedMemoryRefs) > 0 && use.GuardAccepted {
				for _, key := range []string{"all", strings.ToLower(result.Mode)} {
					if acc := accumulators[key]; acc != nil {
						acc.memoryCitedActions++
						if success {
							acc.memoryCitedSuccess++
						}
					}
				}
			}
		}
		business, exact := paymentRetryCounts(result.Trace.Events)
		metrics.BusinessPaymentResubmissionCount += business
		metrics.PaymentExactRedeliveryCount += exact
	}
	metrics.PaymentRetryCount = metrics.BusinessPaymentResubmissionCount
	for key, acc := range accumulators {
		metrics.Headline[key] = headlineFor(*acc)
	}
	metrics.TaskSuccessRate = rateOrZero(accumulators["all"].successful, accumulators["all"].trials)
	metrics.RecoverySuccessRate = rateOrZero(accumulators["all"].recoverySuccess, accumulators["all"].recoveryTrials)
	metrics.LLMProposals = accumulators["all"].llm
	metrics.LLMAccepted = accumulators["all"].llmAccepted
	metrics.GuardDecisions = accumulators["all"].guard
	metrics.GuardRejections = accumulators["all"].guardRejected
	metrics.GuardRejectionRate = rateOrZero(accumulators["all"].guardRejected, accumulators["all"].guard)
	metrics.LLMProposalAcceptanceRate = rateOrZero(accumulators["all"].llmAccepted, accumulators["all"].llm)
	metrics.MemoryRetrievalHitRate = rateOrZero(accumulators["all"].memoryHits, accumulators["all"].memoryAttempted)
	metrics.MemoryRetrievalAttempts = accumulators["all"].memoryAttempted
	metrics.MemoryRetrievalHits = accumulators["all"].memoryHits
	metrics.MemoryCitationCount = accumulators["all"].memoryCitations
	metrics.MemoryCitationRate = rateOrZero(accumulators["all"].memoryCitations, accumulators["all"].memoryHits)
	metrics.MemoryCitedActions = accumulators["all"].memoryCitedActions
	metrics.MemoryCitedActionsInSuccessfulEpisodes = accumulators["all"].memoryCitedSuccess
	metrics.MemoryGuidedActions = metrics.MemoryCitedActions
	metrics.MemoryGuidedActionSuccess = metrics.MemoryCitedActionsInSuccessfulEpisodes
	metrics.RecoverySuccesses = accumulators["all"].recoverySuccess
	metrics.FailureInjectionRecoverySteps = recomputeFaultSteps(results)
	metrics.Reliability = reliabilityMetrics(results)
	metrics.GuardStages.ContextValidationNumerator = contextRejections
	metrics.GuardStages.ContextValidationDenominator = contextDecisions
	metrics.GuardStages.ContextValidationRejectionRate = nullableRate(contextRejections, contextDecisions)
	metrics.GuardStages.RuntimeGuardRejectionNumerator = runtimeGuardRejected
	metrics.GuardStages.RuntimeGuardRejectionDenominator = runtimeGuardReached
	metrics.GuardStages.RuntimeGuardRejectionRate = nullableRate(runtimeGuardRejected, runtimeGuardReached)
	metrics.GuardStages.TransportFailureRate = nullableRate(metrics.GuardStages.TransportFailureNumerator, metrics.GuardStages.TransportFailureDenominator)
	metrics.GuardStages.ParseFailureRate = nullableRate(metrics.GuardStages.ParseFailureNumerator, metrics.GuardStages.ParseFailureDenominator)
}

func isLLMOutcome(value EpisodeTrace, outcome *trace.DecisionOutcomeTrace) bool {
	if outcome == nil {
		return false
	}
	for _, model := range value.ModelDecisionTraces {
		if model != nil && model.TraceID == outcome.ModelDecisionTraceID {
			return !strings.EqualFold(model.Provider, "rule") && model.Status != llm.TraceFallback
		}
	}
	return false
}

func headlineFor(acc integrityAccumulator) ModeHeadline {
	p50, p95 := percentilePair(acc.latencies)
	return ModeHeadline{Trials: acc.trials, Completed: acc.completed, Successful: acc.successful, TaskSuccessRate: nullableRate(acc.successful, acc.trials), TaskSuccessNumerator: acc.successful, TaskSuccessDenominator: acc.trials, RecoverySuccessRate: nullableRate(acc.recoverySuccess, acc.recoveryTrials), RecoverySuccessNumerator: acc.recoverySuccess, RecoverySuccessDenominator: acc.recoveryTrials, LLMProposalAcceptanceRate: nullableRate(acc.llmAccepted, acc.llm), LLMProposalNumerator: acc.llmAccepted, LLMProposalDenominator: acc.llm, GuardRejectionRate: nullableRate(acc.guardRejected, acc.guard), GuardRejectionNumerator: acc.guardRejected, GuardRejectionDenominator: acc.guard, MemoryRetrievalHitRate: nullableRate(acc.memoryHits, acc.memoryAttempted), MemoryRetrievalHitNumerator: acc.memoryHits, MemoryRetrievalHitDenominator: acc.memoryAttempted, LatencyP50MS: nullableFloat(p50, len(acc.latencies)), LatencyP95MS: nullableFloat(p95, len(acc.latencies)), DescriptiveOnly: acc.trials < 30}
}

func nullableRate(numerator, denominator int) *float64 {
	if denominator == 0 {
		return nil
	}
	value := float64(numerator) / float64(denominator)
	return &value
}

func nullableFloat(value float64, samples int) *float64 {
	if samples == 0 {
		return nil
	}
	return &value
}

func rateOrZero(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func sideEffectDelta(outcome *trace.DecisionOutcomeTrace) trace.GuardSideEffectSnapshot {
	if outcome == nil || outcome.SideEffectsBefore == nil || outcome.SideEffectsAfter == nil {
		return trace.GuardSideEffectSnapshot{}
	}
	return outcome.SideEffectsBefore.Delta(*outcome.SideEffectsAfter)
}

func addModelStage(metrics GuardStageMetrics, model *llm.ModelDecisionTrace) GuardStageMetrics {
	metrics.TransportFailureDenominator++
	switch model.Status {
	case llm.TraceTransportError:
		metrics.TransportFailureNumerator++
	case llm.TraceParseError:
		metrics.ParseFailureNumerator++
		metrics.ParseFailureDenominator++
	default:
		metrics.ParseFailureDenominator++
	}
	return metrics
}

func paymentRetryCounts(events []*episode.EpisodeEvent) (business, exact int) {
	seenKeys := map[string]int{}
	for _, event := range events {
		if event == nil || event.Action.Type != "PAYMENT_SUBMITTED" {
			continue
		}
		seenKeys[event.Action.IdempotencyKey]++
	}
	for key, count := range seenKeys {
		if count > 1 {
			business += count - 1
			if key != "" {
				exact += count - 1
			}
		}
	}
	return business, exact
}

func recomputeFaultSteps(results []EpisodeResult) map[string]StepSummary {
	values := map[string][]float64{}
	triggered := map[string]int{}
	recovered := map[string]int{}
	for _, result := range results {
		if !result.Failure.EffectiveApplied() {
			continue
		}
		key := result.Failure.Kind
		triggered[key]++
		grade := result.Grade
		if len(grade.Assertions) == 0 {
			grade = GradeEpisode(result)
		}
		if grade.Passed && isRecovery(result.Trace) {
			recovered[key]++
		}
		if !isRecovery(result.Trace) {
			continue
		}
		values[key] = append(values[key], float64(recoverySteps(result.Trace.Events)))
	}
	result := map[string]StepSummary{}
	for key, count := range triggered {
		samples := values[key]
		sort.Float64s(samples)
		p50, p95 := percentilePair(samples)
		mean := 0.0
		for _, sample := range samples {
			mean += sample
		}
		if len(samples) > 0 {
			mean /= float64(len(samples))
		}
		result[key] = StepSummary{Samples: len(samples), Recovered: recovered[key], P50: p50, P95: p95, Mean: mean, AppliedCount: count}
	}
	return result
}

func reliabilityMetrics(results []EpisodeResult) ReliabilityMetrics {
	type sample struct {
		trial  int
		caseID string
		passed bool
	}
	groups := map[string][]sample{}
	for _, result := range results {
		key := result.TaskID
		if key == "" {
			key = result.CaseID
		}
		grade := result.Grade
		if len(grade.Assertions) == 0 {
			grade = GradeEpisode(result)
		}
		groups[key] = append(groups[key], sample{trial: result.TrialIndex, caseID: result.CaseID, passed: grade.Passed})
	}
	result := ReliabilityMetrics{PassP1: map[string]int{}, PassP2: map[string]int{}, PassP4: map[string]int{}, PassP8: map[string]int{}}
	for task, samples := range groups {
		sort.SliceStable(samples, func(i, j int) bool {
			if samples[i].trial != samples[j].trial {
				if samples[i].trial == 0 {
					return false
				}
				if samples[j].trial == 0 {
					return true
				}
				return samples[i].trial < samples[j].trial
			}
			return samples[i].caseID < samples[j].caseID
		})
		for _, k := range []int{1, 2, 4, 8} {
			if len(samples) < k {
				continue
			}
			passed := true
			for _, value := range samples[:k] {
				passed = passed && value.passed
			}
			switch k {
			case 1:
				result.PassP1[task] = boolInt(passed)
			case 2:
				result.PassP2[task] = boolInt(passed)
			case 4:
				result.PassP4[task] = boolInt(passed)
			case 8:
				result.PassP8[task] = boolInt(passed)
			}
		}
	}
	return result
}
