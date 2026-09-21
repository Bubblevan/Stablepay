package observability

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/llm"
	"github.com/stablepay/commerce-runtime/internal/payment"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

type integrityAccumulator struct {
	trials, completed, successful                                                        int
	businessRecovery, businessRecoverySuccess                                            int
	llmAttempts, llmParsed, llmAccepted                                                  int
	guard, guardRejected                                                                 int
	memoryAttempted, memoryHits, memoryCitations, memoryCitedActions, memoryCitedSuccess int
	latencies                                                                            []float64
}

type canonicalDecisionAttempt struct {
	id      string
	outcome *trace.DecisionOutcomeTrace
	model   *llm.ModelDecisionTrace
}

func Compute(results []EpisodeResult, now time.Time) Metrics {
	metrics := Metrics{SchemaVersion: SchemaVersion, GeneratedAt: now.UTC(), FailureInjectionRecoverySteps: map[string]StepSummary{}, Evidence: EvidenceSummary{Modes: map[string]int{}, Environments: map[string]int{}, SensitiveFieldsOmitted: true}, ExperimentMatrix: ExperimentMatrix{MemoryMode: map[string]Cell{}, RecoveryProvider: map[string]Cell{}, FailureRate: map[string]Cell{}, Path: map[string]Cell{}}}
	metrics.Evidence.SubmittedTrials = len(results)
	for _, result := range results {
		metrics.Evidence.Modes[result.Mode]++
		if strings.TrimSpace(result.Environment) != "" {
			metrics.Evidence.Environments[result.Mode+"/"+result.Environment]++
		}
		if strings.TrimSpace(result.CollectionError) != "" {
			metrics.Evidence.CollectionErrorCount++
		}
		if ValidBenchmarkTrial(result) {
			metrics.Evidence.ValidTrials++
		}
	}
	metrics.Evidence.CollectionErrorRate = ratio(metrics.Evidence.CollectionErrorCount, metrics.Evidence.SubmittedTrials)
	applyIntegrityMetrics(results, &metrics)
	return metrics
}

func applyIntegrityMetrics(results []EpisodeResult, metrics *Metrics) {
	accumulators := map[string]*integrityAccumulator{"all": {}, ModeLive: {}, ModeReplay: {}, ModeOffline: {}}
	metrics.Headline = map[string]ModeHeadline{}
	metrics.TotalEpisodes = len(results)
	metrics.FaultCoverage = map[string]FaultCoverage{}
	metrics.Reliability = ReliabilityMetrics{PassP1: map[string]int{}, PassP2: map[string]int{}, PassP4: map[string]int{}, PassP8: map[string]int{}, Aggregate: map[string]PassKAggregate{}}
	metrics.CompletedEpisodes, metrics.OperationalCompletedEpisodes, metrics.SuccessfulEpisodes = 0, 0, 0
	metrics.RecoveryEpisodes, metrics.RecoverySuccesses = 0, 0
	metrics.FaultTriggered, metrics.FaultTaskRecovered, metrics.BusinessRecoveryEntered, metrics.OperationalRetryCases = 0, 0, 0, 0
	metrics.LLMProposals, metrics.LLMAccepted, metrics.LLMDecisionAttempts = 0, 0, 0
	metrics.LLMTransportFailures, metrics.LLMParseFailures = 0, 0
	metrics.ContextEvidenceRejections, metrics.ContextMemoryRejections, metrics.RuntimeGuardRejections, metrics.AcceptedDecisions, metrics.FallbackDecisions = 0, 0, 0, 0, 0
	metrics.GuardDecisions, metrics.GuardRejections, metrics.UnsafeSideEffectCount = 0, 0, 0
	metrics.MemoryRetrievalAttempts, metrics.MemoryRetrievalHits, metrics.MemoryCitationCount = 0, 0, 0
	metrics.MemoryCitedActions, metrics.MemoryCitedActionsInSuccessfulEpisodes = 0, 0
	metrics.BusinessPaymentResubmissionCount, metrics.PaymentExactRedeliveryCount = 0, 0
	metrics.GuardStages = GuardStageMetrics{}
	var llmLatencies, paymentLatencies, recoveryLatencies []float64
	cellLatencies := map[string]map[string][]float64{"memory": {}, "provider": {}, "failure": {}, "path": {}}
	validResults := make([]EpisodeResult, 0, len(results))
	for _, result := range results {
		if !ValidBenchmarkTrial(result) {
			continue
		}
		validResults = append(validResults, result)
		grade := result.Grade
		if len(grade.Assertions) == 0 {
			grade = GradeEpisode(result)
		}
		state := result.TraceState()
		completed := episode.IsTerminal(state)
		success := grade.Passed
		businessRecovery := isRecovery(result.Trace)
		faultTriggered := result.Failure.EffectiveApplied()
		taskRecovered := faultTriggered && success
		operationalRetry := faultTriggered && !businessRecovery && operationalRetryObserved(result)
		for _, key := range accumulatorKeys(result) {
			if _, ok := accumulators[key]; !ok {
				accumulators[key] = &integrityAccumulator{}
			}
		}
		for _, key := range accumulatorKeys(result) {
			if acc := accumulators[key]; acc != nil {
				acc.trials++
				if completed {
					acc.completed++
				}
				if success {
					acc.successful++
				}
				if businessRecovery {
					acc.businessRecovery++
					if success {
						acc.businessRecoverySuccess++
					}
				}
				if latency := episodeLatencyMS(result); latency >= 0 {
					acc.latencies = append(acc.latencies, latency)
				}
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
		metrics.DuplicateSettlementCount += duplicateSettlements(result.Trace.Ledger)
		metrics.DeliveryRetryCount += deliveryRetries(result.Trace.Events)
		if result.Mode == ModeLive {
			liveHasModelTrace := false
			for _, model := range result.Trace.ModelDecisionTraces {
				if model != nil && strings.TrimSpace(model.Provider) != "" && !strings.EqualFold(model.Provider, "rule") {
					liveHasModelTrace = true
					break
				}
			}
			if liveHasModelTrace {
				metrics.Evidence.LiveWithModelTrace++
			} else {
				metrics.Evidence.LiveWithoutModelTrace++
			}
		}
		if businessRecovery {
			metrics.RecoveryEpisodes++
			if success {
				metrics.RecoverySuccesses++
			}
			metrics.BusinessRecoveryEntered++
		}
		if faultTriggered {
			metrics.Evidence.RequestedFaultCases++
			if result.Failure.Configured {
				metrics.Evidence.AppliedFaultCases++
			}
			metrics.FaultTriggered++
			if taskRecovered {
				metrics.FaultTaskRecovered++
			}
			if operationalRetry {
				metrics.OperationalRetryCases++
			}
			coverage := metrics.FaultCoverage[result.Failure.Kind]
			coverage.Requested++
			coverage.Triggered++
			if result.Failure.Configured {
				coverage.Configured++
			}
			if taskRecovered {
				coverage.TaskRecovered++
				coverage.Recovered++
			}
			if businessRecovery {
				coverage.BusinessRecoveryEntered++
			}
			if operationalRetry {
				coverage.OperationalRetryCases++
			}
			metrics.FaultCoverage[result.Failure.Kind] = coverage
		} else if result.Failure.Kind != "" && result.Failure.Kind != "none" {
			metrics.Evidence.RequestedFaultCases++
			coverage := metrics.FaultCoverage[result.Failure.Kind]
			coverage.Requested++
			if result.Failure.Configured {
				coverage.Configured++
			}
			metrics.FaultCoverage[result.Failure.Kind] = coverage
		}
		attempts := canonicalDecisionAttempts(result.Trace)
		for _, attempt := range attempts {
			if attempt.outcome == nil {
				continue
			}
			if attempt.outcome.Stage == trace.DecisionStageFallback || !attemptIsLLM(attempt) {
				if attempt.outcome.Stage == trace.DecisionStageFallback {
					metrics.FallbackDecisions++
				}
				continue
			}
			metrics.LLMDecisionAttempts++
			for _, key := range accumulatorKeys(result) {
				if acc := accumulators[key]; acc != nil {
					acc.llmAttempts++
					if attempt.outcome.Stage != trace.DecisionStageTransport {
						acc.llmParsed++
					}
				}
			}
			switch attempt.outcome.Stage {
			case trace.DecisionStageTransport:
				metrics.LLMTransportFailures++
				metrics.GuardStages.TransportFailureDenominator++
			case trace.DecisionStageParseSchema:
				metrics.LLMParseFailures++
				metrics.GuardStages.ParseFailureNumerator++
				metrics.GuardStages.ParseFailureDenominator++
			case trace.DecisionStageContextEvidence:
				metrics.ContextEvidenceRejections++
			case trace.DecisionStageContextMemory:
				metrics.ContextMemoryRejections++
			case trace.DecisionStageRuntimeGuard:
				if !attempt.outcome.GuardAccepted {
					metrics.RuntimeGuardRejections++
				}
			case trace.DecisionStageAccepted:
				metrics.AcceptedDecisions++
				for _, key := range accumulatorKeys(result) {
					if acc := accumulators[key]; acc != nil {
						acc.llmAccepted++
					}
				}
			}
			if attempt.outcome.Stage == trace.DecisionStageContextEvidence || attempt.outcome.Stage == trace.DecisionStageContextMemory {
				metrics.GuardStages.ContextValidationDenominator++
				metrics.GuardStages.ContextValidationNumerator++
			}
			if attempt.outcome.ReachedRuntimeGuard {
				metrics.GuardStages.RuntimeGuardRejectionDenominator++
				for _, key := range accumulatorKeys(result) {
					if acc := accumulators[key]; acc != nil {
						acc.guard++
					}
				}
				if !attempt.outcome.GuardAccepted {
					metrics.GuardStages.RuntimeGuardRejectionNumerator++
					for _, key := range accumulatorKeys(result) {
						if acc := accumulators[key]; acc != nil {
							acc.guardRejected++
						}
					}
					if sideEffectDelta(attempt.outcome).AnyIncrease() {
						metrics.UnsafeSideEffectCount++
					}
				}
			}
			if attempt.model != nil {
				if latency := attempt.model.ResponseReceivedAt.Sub(attempt.model.RequestStartedAt).Seconds() * 1000; latency >= 0 {
					llmLatencies = append(llmLatencies, latency)
				}
			}
		}
		for _, use := range result.Trace.MemoryUseTraces {
			if use == nil {
				continue
			}
			if use.RetrievalAttempted {
				metrics.MemoryRetrievalAttempts++
				if len(use.RetrievedMemoryRefs) > 0 {
					metrics.MemoryRetrievalHits++
				}
				if len(use.CitedMemoryRefs) > 0 {
					metrics.MemoryCitationCount++
				}
				for _, key := range accumulatorKeys(result) {
					if acc := accumulators[key]; acc != nil {
						acc.memoryAttempted++
						if len(use.RetrievedMemoryRefs) > 0 {
							acc.memoryHits++
						}
						if len(use.CitedMemoryRefs) > 0 {
							acc.memoryCitations++
						}
					}
				}
			}
			if len(use.CitedMemoryRefs) > 0 && use.GuardAccepted {
				metrics.MemoryCitedActions++
				if success {
					metrics.MemoryCitedActionsInSuccessfulEpisodes++
				}
				for _, key := range accumulatorKeys(result) {
					if acc := accumulators[key]; acc != nil {
						acc.memoryCitedActions++
						if success {
							acc.memoryCitedSuccess++
						}
					}
				}
			}
		}
		for _, intent := range result.Trace.PaymentIntents {
			if intent != nil && intent.Status == payment.IntentConfirmed && intent.UpdatedAt.After(intent.CreatedAt) {
				paymentLatencies = append(paymentLatencies, intent.UpdatedAt.Sub(intent.CreatedAt).Seconds()*1000)
			}
		}
		business, _ := paymentRetryCounts(result.Trace.Events)
		metrics.BusinessPaymentResubmissionCount += business
		for _, transport := range result.Trace.PaymentTransportTraces {
			if transport != nil && transport.Kind == "EXACT_REDELIVERY" {
				metrics.PaymentExactRedeliveryCount++
			}
		}
		if businessRecovery {
			if latency := recoveryLatencyMS(result.Trace); latency >= 0 {
				recoveryLatencies = append(recoveryLatencies, latency)
			}
		}
		updateCell(metrics.ExperimentMatrix.MemoryMode, cellLatencies["memory"], normalizedKey(result.Trace.RuntimeVariant.MemoryMode, normalizedKey(result.MemoryMode, "unknown")), result, success, businessRecovery)
		updateCell(metrics.ExperimentMatrix.RecoveryProvider, cellLatencies["provider"], normalizedKey(result.Trace.RuntimeVariant.RecoveryProvider, normalizedKey(result.RecoveryProvider, "unknown")), result, success, businessRecovery)
		updateCell(metrics.ExperimentMatrix.FailureRate, cellLatencies["failure"], failureRateKey(result.Failure.RatePercent), result, success, businessRecovery)
		updateCell(metrics.ExperimentMatrix.Path, cellLatencies["path"], normalizedKey(result.Path, "unknown"), result, success, businessRecovery)
	}
	metrics.TaskSuccessRate = rateOrZero(metrics.SuccessfulEpisodes, len(validResults))
	metrics.RecoverySuccessRate = rateOrZero(metrics.RecoverySuccesses, metrics.RecoveryEpisodes)
	metrics.BusinessRecoverySuccessRate = metrics.RecoverySuccessRate
	metrics.FaultRecoverySuccessRate = rateOrZero(metrics.FaultTaskRecovered, metrics.FaultTriggered)
	metrics.PaymentRetryCount = metrics.BusinessPaymentResubmissionCount
	metrics.LLMProposals = metrics.ContextEvidenceRejections + metrics.ContextMemoryRejections + metrics.RuntimeGuardRejections + metrics.AcceptedDecisions
	metrics.LLMAccepted = metrics.AcceptedDecisions
	metrics.LLMProposalAcceptanceRate = rateOrZero(metrics.LLMAccepted, metrics.LLMProposals)
	metrics.LLMParsedProposalAcceptanceRate = metrics.LLMProposalAcceptanceRate
	metrics.LLMEndToEndDecisionSuccessRate = rateOrZero(metrics.AcceptedDecisions, metrics.LLMDecisionAttempts)
	metrics.GuardDecisions = metrics.GuardStages.RuntimeGuardRejectionDenominator
	metrics.GuardRejections = metrics.GuardStages.RuntimeGuardRejectionNumerator
	metrics.GuardRejectionRate = rateOrZero(metrics.GuardRejections, metrics.GuardDecisions)
	metrics.MemoryRetrievalHitRate = rateOrZero(metrics.MemoryRetrievalHits, metrics.MemoryRetrievalAttempts)
	metrics.MemoryCitationRate = rateOrZero(metrics.MemoryCitationCount, metrics.MemoryRetrievalAttempts)
	metrics.MemoryGuidedActions = metrics.MemoryCitedActions
	metrics.MemoryGuidedActionSuccess = metrics.MemoryCitedActionsInSuccessfulEpisodes
	metrics.EpisodeLatencyP50MS, metrics.EpisodeLatencyP95MS = percentilePair(extractAllLatencies(validResults))
	metrics.LLMLatencyP50MS, metrics.LLMLatencyP95MS = percentilePair(llmLatencies)
	metrics.PaymentConfirmationLatencyP50MS, metrics.PaymentConfirmationLatencyP95MS = percentilePair(paymentLatencies)
	metrics.RecoveryLatencyP50MS, metrics.RecoveryLatencyP95MS = percentilePair(recoveryLatencies)
	metrics.FailureInjectionRecoverySteps = recomputeFaultSteps(validResults)
	metrics.Reliability = reliabilityMetrics(validResults)
	metrics.GuardStages.TransportFailureNumerator = metrics.LLMTransportFailures
	metrics.GuardStages.ContextValidationRejectionRate = nullableRate(metrics.GuardStages.ContextValidationNumerator, metrics.GuardStages.ContextValidationDenominator)
	metrics.GuardStages.RuntimeGuardRejectionRate = nullableRate(metrics.GuardStages.RuntimeGuardRejectionNumerator, metrics.GuardStages.RuntimeGuardRejectionDenominator)
	metrics.GuardStages.TransportFailureRate = nullableRate(metrics.GuardStages.TransportFailureNumerator, metrics.GuardStages.TransportFailureDenominator)
	metrics.GuardStages.ParseFailureRate = nullableRate(metrics.GuardStages.ParseFailureNumerator, metrics.GuardStages.ParseFailureDenominator)
	for key, acc := range accumulators {
		metrics.Headline[key] = headlineFor(*acc)
	}
}

func accumulatorKeys(result EpisodeResult) []string {
	keys := []string{"all", strings.ToLower(result.Mode)}
	if strings.TrimSpace(result.Environment) != "" {
		keys = append(keys, strings.ToLower(result.Mode)+"/"+strings.ToLower(strings.TrimSpace(result.Environment)))
	}
	return keys
}

func canonicalDecisionOutcomes(value EpisodeTrace) []*trace.DecisionOutcomeTrace {
	attempts := canonicalDecisionAttempts(value)
	result := make([]*trace.DecisionOutcomeTrace, 0, len(attempts))
	for _, attempt := range attempts {
		if attempt.outcome != nil {
			result = append(result, attempt.outcome)
		}
	}
	return result
}

func canonicalDecisionAttempts(value EpisodeTrace) []canonicalDecisionAttempt {
	models := map[string]*llm.ModelDecisionTrace{}
	for _, model := range value.ModelDecisionTraces {
		if model == nil {
			continue
		}
		if model.DecisionAttemptID != "" {
			if _, ok := models[model.DecisionAttemptID]; !ok {
				models[model.DecisionAttemptID] = model
			}
		}
		models[model.TraceID] = model
	}
	byID := map[string]canonicalDecisionAttempt{}
	for _, outcome := range value.DecisionOutcomeTraces {
		if outcome == nil {
			continue
		}
		id := outcome.DecisionAttemptID
		if id == "" {
			id = trace.DecisionAttemptIDFor(outcome.EpisodeID, outcome.ModelDecisionTraceID, outcome.ProposalID)
		}
		candidate := canonicalDecisionAttempt{id: id, outcome: outcome, model: models[outcome.ModelDecisionTraceID]}
		if candidate.model == nil {
			candidate.model = models[id]
		}
		previous, exists := byID[id]
		if !exists || decisionStageRank(outcome.Stage) > decisionStageRank(previous.outcome.Stage) || (decisionStageRank(outcome.Stage) == decisionStageRank(previous.outcome.Stage) && outcome.CreatedAt.After(previous.outcome.CreatedAt)) {
			byID[id] = candidate
		}
	}
	result := make([]canonicalDecisionAttempt, 0, len(byID))
	for _, value := range byID {
		result = append(result, value)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].id < result[j].id })
	return result
}

func decisionStageRank(stage trace.DecisionStage) int {
	switch stage {
	case trace.DecisionStageAccepted:
		return 6
	case trace.DecisionStageRuntimeGuard:
		return 5
	case trace.DecisionStageContextMemory:
		return 4
	case trace.DecisionStageContextEvidence:
		return 3
	case trace.DecisionStageParseSchema:
		return 2
	case trace.DecisionStageTransport:
		return 1
	case trace.DecisionStageFallback:
		return 0
	default:
		return -1
	}
}

func attemptIsLLM(attempt canonicalDecisionAttempt) bool {
	if attempt.model == nil {
		return true
	}
	return !strings.EqualFold(attempt.model.Provider, "rule") && attempt.model.Status != llm.TraceFallback
}

func extractAllLatencies(results []EpisodeResult) []float64 {
	values := make([]float64, 0, len(results))
	for _, result := range results {
		if latency := episodeLatencyMS(result); latency >= 0 {
			values = append(values, latency)
		}
	}
	return values
}

func operationalRetryObserved(result EpisodeResult) bool {
	return result.Trace.Execution != nil && result.Trace.Execution.AttemptCount > 1
}

func recomputeFaultSteps(results []EpisodeResult) map[string]StepSummary {
	result := map[string]StepSummary{}
	values := map[string][]float64{}
	for _, value := range results {
		if !value.Failure.EffectiveApplied() {
			continue
		}
		key := value.Failure.Kind
		summary := result[key]
		summary.AppliedCount++
		if isRecovery(value.Trace) {
			summary.BusinessRecoveryEntered++
			summary.Samples++
			values[key] = append(values[key], float64(recoverySteps(value.Trace.Events)))
		}
		grade := value.Grade
		if len(grade.Assertions) == 0 {
			grade = GradeEpisode(value)
		}
		if grade.Passed {
			summary.TaskRecovered++
			if isRecovery(value.Trace) {
				summary.Recovered++
			}
		}
		if operationalRetryObserved(value) && !isRecovery(value.Trace) {
			summary.OperationalRetryCases++
		}
		result[key] = summary
	}
	for key, summary := range result {
		p50, p95 := percentilePair(values[key])
		mean := 0.0
		for _, sample := range values[key] {
			mean += sample
		}
		if len(values[key]) > 0 {
			mean /= float64(len(values[key]))
		}
		summary.P50, summary.P95, summary.Mean = p50, p95, mean
		result[key] = summary
	}
	return result
}

func reliabilityMetrics(results []EpisodeResult) ReliabilityMetrics {
	type sample struct {
		trial     int
		requestID string
		passed    bool
	}
	groups := map[string][]sample{}
	seen := map[string]map[string]struct{}{}
	for _, result := range results {
		if result.Mode != ModeLive || result.TrialIndex <= 0 || strings.TrimSpace(result.RequestID) == "" {
			continue
		}
		variant := result.Trace.RuntimeVariant.WithHash()
		key := strings.Join([]string{result.TaskID, result.Suite, result.Environment, variant.ConfigHash}, "\x00")
		if seen[key] == nil {
			seen[key] = map[string]struct{}{}
		}
		if _, exists := seen[key][result.RequestID]; exists {
			continue
		}
		seen[key][result.RequestID] = struct{}{}
		grade := result.Grade
		if len(grade.Assertions) == 0 {
			grade = GradeEpisode(result)
		}
		groups[key] = append(groups[key], sample{trial: result.TrialIndex, requestID: result.RequestID, passed: grade.Passed})
	}
	result := ReliabilityMetrics{PassP1: map[string]int{}, PassP2: map[string]int{}, PassP4: map[string]int{}, PassP8: map[string]int{}, Aggregate: map[string]PassKAggregate{}}
	for key, samples := range groups {
		sort.SliceStable(samples, func(i, j int) bool {
			if samples[i].trial != samples[j].trial {
				return samples[i].trial < samples[j].trial
			}
			return samples[i].requestID < samples[j].requestID
		})
		for _, k := range []int{1, 2, 4, 8} {
			if len(samples) < k {
				continue
			}
			passed := true
			for _, sample := range samples[:k] {
				passed = passed && sample.passed
			}
			switch k {
			case 1:
				result.PassP1[key] = boolInt(passed)
			case 2:
				result.PassP2[key] = boolInt(passed)
			case 4:
				result.PassP4[key] = boolInt(passed)
			case 8:
				result.PassP8[key] = boolInt(passed)
			}
			aggregateKey := "pass^" + fmt.Sprint(k)
			aggregate := result.Aggregate[aggregateKey]
			aggregate.EligibleTasks++
			if passed {
				aggregate.PassingTasks++
			}
			aggregate.Rate = rateOrZero(aggregate.PassingTasks, aggregate.EligibleTasks)
			result.Aggregate[aggregateKey] = aggregate
		}
	}
	return result
}

func sideEffectDelta(outcome *trace.DecisionOutcomeTrace) trace.GuardSideEffectSnapshot {
	if outcome == nil || outcome.SideEffectsBefore == nil || outcome.SideEffectsAfter == nil {
		return trace.GuardSideEffectSnapshot{}
	}
	return outcome.SideEffectsBefore.Delta(*outcome.SideEffectsAfter)
}

func paymentRetryCounts(events []*episode.EpisodeEvent) (business, exact int) {
	seenKeys := map[string]int{}
	for _, event := range events {
		if event != nil && event.Action.Type == trace.ActionPaymentSubmitted {
			seenKeys[event.Action.IdempotencyKey]++
		}
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

func headlineFor(acc integrityAccumulator) ModeHeadline {
	p50, p95 := percentilePair(acc.latencies)
	return ModeHeadline{Trials: acc.trials, Completed: acc.completed, Successful: acc.successful, TaskSuccessRate: nullableRate(acc.successful, acc.trials), TaskSuccessNumerator: acc.successful, TaskSuccessDenominator: acc.trials, RecoverySuccessRate: nullableRate(acc.businessRecoverySuccess, acc.businessRecovery), RecoverySuccessNumerator: acc.businessRecoverySuccess, RecoverySuccessDenominator: acc.businessRecovery, LLMProposalAcceptanceRate: nullableRate(acc.llmAccepted, acc.llmParsed), LLMProposalNumerator: acc.llmAccepted, LLMProposalDenominator: acc.llmParsed, GuardRejectionRate: nullableRate(acc.guardRejected, acc.guard), GuardRejectionNumerator: acc.guardRejected, GuardRejectionDenominator: acc.guard, MemoryRetrievalHitRate: nullableRate(acc.memoryHits, acc.memoryAttempted), MemoryRetrievalHitNumerator: acc.memoryHits, MemoryRetrievalHitDenominator: acc.memoryAttempted, LatencyP50MS: nullableFloat(p50, len(acc.latencies)), LatencyP95MS: nullableFloat(p95, len(acc.latencies)), DescriptiveOnly: acc.trials < 30}
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
	if latency := episodeLatencyMS(result); latency >= 0 {
		latencySamples[key] = append(latencySamples[key], latency)
		cell.LatencyP50MS, cell.LatencyP95MS = percentilePair(latencySamples[key])
	}
	cell.TaskSuccessRate = rateOrZero(cell.Successes, cell.Episodes)
	cell.RecoverySuccessRate = rateOrZero(cell.RecoverySuccesses, cell.RecoveryEpisodes)
	cells[key] = cell
}

func failureRateKey(value int) string {
	if value < 0 {
		value = 0
	}
	return fmt.Sprintf("%d%%", value)
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
