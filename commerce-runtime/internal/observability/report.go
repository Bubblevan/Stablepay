package observability

import (
	"fmt"
	"sort"
	"strings"
)

func RenderMarkdown(metrics Metrics, results []EpisodeResult) string {
	var out strings.Builder
	onlyReplay := len(metrics.Evidence.Modes) == 1 && metrics.Evidence.Modes[ModeReplay] > 0
	out.WriteString("# StablePay S11.2 Agent Eval / Final Metrics Semantics\n\n")
	if onlyReplay {
		out.WriteString("**EXAMPLE / REPLAY - NOT A LIVE BENCHMARK**\n\n")
	}
	out.WriteString("- Schema: `" + metrics.SchemaVersion + "`\n")
	out.WriteString("- Generated: `" + metrics.GeneratedAt.UTC().Format("2006-01-02T15:04:05Z07:00") + "`\n")
	out.WriteString("- Sensitive fields omitted: `true`; artifact body included: `false` by default\n")
	out.WriteString("- Rates include numerator/denominator. Small samples are `descriptive only`; no statistical-significance claim is made.\n\n")

	out.WriteString("## Evidence boundary\n\n")
	out.WriteString("`offline`, `replay`, `live_local`, and `live_real_business` are reported separately. Scenario labels never prove provider, memory, or recovery configuration; live claims require the persisted RuntimeVariant and trace evidence. A live row without a non-rule ModelDecisionTrace is reported as `no_llm_call`/unknown, never as DeepSeek.\n\n")
	out.WriteString(fmt.Sprintf("Modes: `%s`; environments: `%s`; submitted trials: `%d`; valid trials: `%d`; collection errors: `%d` (%.2f%%); live rows with model trace: `%d`; live rows without model trace: `%d`; requested fault cases: `%d`; configured: `%d`; triggered: `%d`.\n\n", renderModes(metrics.Evidence.Modes), renderModes(metrics.Evidence.Environments), metrics.Evidence.SubmittedTrials, metrics.Evidence.ValidTrials, metrics.Evidence.CollectionErrorCount, metrics.Evidence.CollectionErrorRate*100, metrics.Evidence.LiveWithModelTrace, metrics.Evidence.LiveWithoutModelTrace, metrics.Evidence.RequestedFaultCases, configuredFaults(results), triggeredFaults(results)))

	out.WriteString("## Mode-scoped headline metrics\n\n")
	out.WriteString("| mode | trials | completed | task success | recovery success | LLM acceptance | Guard rejection | memory hit | latency p50/p95 ms | note |\n|---|---:|---:|---|---|---|---|---|---|---|\n")
	for _, key := range []string{"all", ModeLive, ModeReplay, ModeOffline} {
		headline, ok := metrics.Headline[key]
		if !ok {
			continue
		}
		note := ""
		if headline.DescriptiveOnly {
			note = "descriptive only"
		}
		out.WriteString(fmt.Sprintf("| `%s` | %d | %d | %s | %s | %s | %s | %s | %s | %s |\n", key, headline.Trials, headline.Completed, rateText(headline.TaskSuccessRate, headline.TaskSuccessNumerator, headline.TaskSuccessDenominator), rateText(headline.RecoverySuccessRate, headline.RecoverySuccessNumerator, headline.RecoverySuccessDenominator), rateText(headline.LLMProposalAcceptanceRate, headline.LLMProposalNumerator, headline.LLMProposalDenominator), rateText(headline.GuardRejectionRate, headline.GuardRejectionNumerator, headline.GuardRejectionDenominator), rateText(headline.MemoryRetrievalHitRate, headline.MemoryRetrievalHitNumerator, headline.MemoryRetrievalHitDenominator), latencyText(headline.LatencyP50MS, headline.LatencyP95MS), note))
	}
	additionalHeadlines := make([]string, 0)
	for key := range metrics.Headline {
		if strings.Contains(key, "/") {
			additionalHeadlines = append(additionalHeadlines, key)
		}
	}
	sort.Strings(additionalHeadlines)
	for _, key := range additionalHeadlines {
		headline := metrics.Headline[key]
		note := "environment slice"
		if headline.DescriptiveOnly {
			note += "; descriptive only"
		}
		out.WriteString(fmt.Sprintf("| `%s` | %d | %d | %s | %s | %s | %s | %s | %s | %s |\n", key, headline.Trials, headline.Completed, rateText(headline.TaskSuccessRate, headline.TaskSuccessNumerator, headline.TaskSuccessDenominator), rateText(headline.RecoverySuccessRate, headline.RecoverySuccessNumerator, headline.RecoverySuccessDenominator), rateText(headline.LLMProposalAcceptanceRate, headline.LLMProposalNumerator, headline.LLMProposalDenominator), rateText(headline.GuardRejectionRate, headline.GuardRejectionNumerator, headline.GuardRejectionDenominator), rateText(headline.MemoryRetrievalHitRate, headline.MemoryRetrievalHitNumerator, headline.MemoryRetrievalHitDenominator), latencyText(headline.LatencyP50MS, headline.LatencyP95MS), note))
	}

	out.WriteString("\n## RuntimeGuard and parser/context stages\n\n")
	out.WriteString(fmt.Sprintf("- `LLMTransportFailureRate`: %s\n- `LLMParseFailureRate`: %s\n- `ContextValidationRejectionRate`: %s\n- `RuntimeGuardRejectionRate`: %s\n\n", rateText(metrics.GuardStages.TransportFailureRate, metrics.GuardStages.TransportFailureNumerator, metrics.GuardStages.TransportFailureDenominator), rateText(metrics.GuardStages.ParseFailureRate, metrics.GuardStages.ParseFailureNumerator, metrics.GuardStages.ParseFailureDenominator), rateText(metrics.GuardStages.ContextValidationRejectionRate, metrics.GuardStages.ContextValidationNumerator, metrics.GuardStages.ContextValidationDenominator), rateText(metrics.GuardStages.RuntimeGuardRejectionRate, metrics.GuardStages.RuntimeGuardRejectionNumerator, metrics.GuardStages.RuntimeGuardRejectionDenominator)))
	out.WriteString(fmt.Sprintf("- `llm_decision_attempts`: `%d`; transport failures: `%d`; parse failures: `%d`; context-evidence rejections: `%d`; context-memory rejections: `%d`; RuntimeGuard rejections: `%d`; accepted: `%d`; fallback: `%d`\n- `llm_parsed_proposal_acceptance_rate`: `%.2f%%` (%d/%d); `llm_end_to_end_decision_success_rate`: `%.2f%%` (%d/%d)\n\n", metrics.LLMDecisionAttempts, metrics.LLMTransportFailures, metrics.LLMParseFailures, metrics.ContextEvidenceRejections, metrics.ContextMemoryRejections, metrics.RuntimeGuardRejections, metrics.AcceptedDecisions, metrics.FallbackDecisions, metrics.LLMParsedProposalAcceptanceRate*100, metrics.AcceptedDecisions, metrics.LLMProposals, metrics.LLMEndToEndDecisionSuccessRate*100, metrics.AcceptedDecisions, metrics.LLMDecisionAttempts))
	out.WriteString("Parse/schema and context failures are not counted as RuntimeGuard rejections. A rejected audit record is not itself a side effect.\n\n")

	out.WriteString("## Reliability and side effects\n\n")
	out.WriteString(fmt.Sprintf("- `unsafe_side_effect_count`: `%d` (only rejected decisions with a protected side-effect counter increase)\n- `duplicate_settlement_count`: `%d`\n- `business_payment_resubmission_count`: `%d`\n- `payment_exact_redelivery_count`: `%d`\n- `MemoryCitedActions`: `%d`; `MemoryCitedActionsInSuccessfulEpisodes`: `%d` - descriptive correlation, not causal credit assignment\n\n", metrics.UnsafeSideEffectCount, metrics.DuplicateSettlementCount, metrics.BusinessPaymentResubmissionCount, metrics.PaymentExactRedeliveryCount, metrics.MemoryCitedActions, metrics.MemoryCitedActionsInSuccessfulEpisodes))
	out.WriteString("### pass^k\n\n")
	out.WriteString("`pass^k` means the first k independent **live** trials for a task all passed the required deterministic graders; replay/offline rows never enter this denominator. It is not `pass@k`.\n\n")
	renderPassK(&out, metrics.Reliability)

	out.WriteString("## Fault coverage\n\n")
	out.WriteString("| failure | requested | configured | triggered | task recovered | business recovery | operational retry |\n|---|---:|---:|---:|---:|---:|---:|\n")
	keys := make([]string, 0, len(metrics.FaultCoverage))
	for key := range metrics.FaultCoverage {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := metrics.FaultCoverage[key]
		out.WriteString(fmt.Sprintf("| `%s` | %d | %d | %d | %d | %d | %d |\n", key, value.Requested, value.Configured, value.Triggered, value.TaskRecovered, value.BusinessRecoveryEntered, value.OperationalRetryCases))
	}
	out.WriteString(fmt.Sprintf("\n`fault_recovery_success_rate` = %d/%d = %.2f%% and is based on `EffectiveApplied && Grade.Passed`; `business_recovery_success_rate` = %d/%d = %.2f%% and is based on explicit RECOVERING/business recovery. Operational retries are reported separately. A fault enters the recovery denominator only when `Triggered=true` and `InjectionCount>0`. Payment faults require a Kitex-aware controller; crash/restart requires a process supervisor.\n\n", metrics.FaultTaskRecovered, metrics.FaultTriggered, metrics.FaultRecoverySuccessRate*100, metrics.RecoverySuccesses, metrics.RecoveryEpisodes, metrics.BusinessRecoverySuccessRate*100))

	out.WriteString("## Experiment matrix (all rows are qualified by the mode table above)\n\n")
	renderCells(&out, "Memory on/off", metrics.ExperimentMatrix.MemoryMode)
	renderCells(&out, "LLM vs RuleRecoveryProvider", metrics.ExperimentMatrix.RecoveryProvider)
	renderCells(&out, "Failure injection rate", metrics.ExperimentMatrix.FailureRate)
	renderCells(&out, "Happy vs recovery path", metrics.ExperimentMatrix.Path)

	out.WriteString("## Episode evidence\n\n")
	out.WriteString("| case | suite | environment | mode | ingress | task/trial | variant | fault | triggered | state | execution_status | episode_id | payment_tx | artifact | validation | duration ms |\n|---|---|---|---|---|---|---|---|---:|---|---|---|---|---|---|---:|\n")
	for _, result := range results {
		state, executionStatus, episodeID, tx, artifact, validation := "", "", result.EpisodeID, "", "", ""
		if result.Trace.Episode != nil {
			state = string(result.Trace.Episode.State)
			episodeID = result.Trace.Episode.EpisodeID
		}
		if result.Trace.Execution != nil {
			executionStatus = string(result.Trace.Execution.Status)
		}
		for _, intent := range result.Trace.PaymentIntents {
			if intent != nil && (intent.TxID != "" || intent.TxHash != "") {
				tx = firstNonEmpty(intent.TxID, intent.TxHash)
			}
		}
		if result.Trace.Artifact != nil {
			artifact = result.Trace.Artifact.DeliveryID
		}
		if result.Trace.Validation != nil {
			validation = result.Trace.Validation.ReasonCode
		}
		variant := firstNonEmpty(result.Trace.RuntimeVariant.RecoveryProvider, "unknown") + "/" + firstNonEmpty(result.Trace.RuntimeVariant.MemoryMode, "unknown") + "/" + firstNonEmpty(result.Trace.RuntimeVariant.ModelRef, "unknown")
		taskTrial := result.TaskID
		if result.TrialIndex > 0 {
			taskTrial = fmt.Sprintf("%s#%d", taskTrial, result.TrialIndex)
		}
		out.WriteString(fmt.Sprintf("| `%s` | `%s` | `%s` | `%s` | `%s` | `%s` | `%s` | `%s` | %t | `%s` | `%s` | `%s` | `%s` | `%s` | `%s` | %.2f |\n", result.CaseID, firstNonEmpty(result.Suite, "unknown"), firstNonEmpty(result.Environment, "unknown"), result.Mode, result.Ingress, taskTrial, variant, result.Failure.Kind, result.Failure.EffectiveApplied(), state, executionStatus, episodeID, tx, artifact, validation, result.DurationMS))
	}

	out.WriteString("\n## Latency semantics\n\n")
	out.WriteString("`episode_latency` = external submit to terminal observation; `llm_latency` = provider request start to response received; `payment_confirmation_latency` = PaymentIntent created to confirmed; `recovery_latency` = first RECOVERING entry to terminal.\n\n")
	out.WriteString("## Reproducibility\n\n")
	out.WriteString("Use the committed dataset seed, task/trial metadata, and immutable request fixture. The external harness writes `episode_results.jsonl`, `grade_results.jsonl`, `metrics.json`, and `report.md`. Artifact bodies are omitted unless `--include-artifact-body` is explicitly used.\n")
	return out.String()
}

func rateText(value *float64, numerator, denominator int) string {
	if value == nil {
		return fmt.Sprintf("unavailable (%d/%d)", numerator, denominator)
	}
	return fmt.Sprintf("%.2f%% (%d/%d)", *value*100, numerator, denominator)
}

func latencyText(p50, p95 *float64) string {
	if p50 == nil || p95 == nil {
		return "unavailable"
	}
	return fmt.Sprintf("%.2f / %.2f", *p50, *p95)
}

func renderPassK(out *strings.Builder, reliability ReliabilityMetrics) {
	for _, item := range []struct {
		name   string
		values map[string]int
	}{{"pass^1", reliability.PassP1}, {"pass^2", reliability.PassP2}, {"pass^4", reliability.PassP4}, {"pass^8", reliability.PassP8}} {
		out.WriteString("- `" + item.name + "`: ")
		keys := make([]string, 0, len(item.values))
		for key := range item.values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, fmt.Sprintf("%s=%d", key, item.values[key]))
		}
		out.WriteString(strings.Join(parts, ", ") + "\n")
	}
	for _, key := range []string{"pass^1", "pass^2", "pass^4", "pass^8"} {
		if aggregate, ok := reliability.Aggregate[key]; ok {
			out.WriteString(fmt.Sprintf("- `%s` aggregate: `%d/%d` eligible task groups, `%.2f%%`\n", key, aggregate.PassingTasks, aggregate.EligibleTasks, aggregate.Rate*100))
		}
	}
	out.WriteString("\n")
}

func renderCells(out *strings.Builder, title string, cells map[string]Cell) {
	out.WriteString("### " + title + "\n\n| cell | episodes | task success | recovery success | p50 ms | p95 ms |\n|---|---:|---:|---:|---:|---:|\n")
	keys := make([]string, 0, len(cells))
	for key := range cells {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := cells[key]
		out.WriteString(fmt.Sprintf("| `%s` | %d | %.2f%% | %.2f%% | %.2f | %.2f |\n", key, value.Episodes, value.TaskSuccessRate*100, value.RecoverySuccessRate*100, value.LatencyP50MS, value.LatencyP95MS))
	}
	out.WriteString("\n")
}

func configuredFaults(results []EpisodeResult) int {
	count := 0
	for _, result := range results {
		if result.Failure.Configured {
			count++
		}
	}
	return count
}
func triggeredFaults(results []EpisodeResult) int {
	count := 0
	for _, result := range results {
		if result.Failure.EffectiveApplied() {
			count++
		}
	}
	return count
}
func renderModes(modes map[string]int) string {
	keys := make([]string, 0, len(modes))
	for key := range modes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s:%d", key, modes[key]))
	}
	return strings.Join(parts, ", ")
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
