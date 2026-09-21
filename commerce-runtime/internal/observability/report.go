package observability

import (
	"fmt"
	"sort"
	"strings"
)

func RenderMarkdown(metrics Metrics, results []EpisodeResult) string {
	var out strings.Builder
	out.WriteString("# StablePay S11 Agent Eval / Observability / Reliability Benchmark\n\n")
	out.WriteString("- Schema: `" + metrics.SchemaVersion + "`\n")
	out.WriteString("- Generated: `" + metrics.GeneratedAt.UTC().Format("2006-01-02T15:04:05Z07:00") + "`\n")
	out.WriteString("- Sensitive fields omitted: `true` (private keys, API keys, DSNs, prompts and raw LLM response bodies are not exported)\n\n")
	out.WriteString("## Evidence boundary\n\n")
	out.WriteString("This report is computed from externally collected Runtime observability snapshots. `offline`, `replay`, and `live` are counted separately. A live row without a `ModelDecisionTrace` is not described as having passed through DeepSeek; a real happy path may be `CLI -> Runtime -> Merchant -> Payment -> Devnet -> Verification -> FULFILLED` without an LLM recovery call.\n\n")
	out.WriteString(fmt.Sprintf("Modes: `%s`; live rows with model trace: `%d`; live rows without model trace: `%d`; requested fault cases: `%d`; applied fault cases: `%d`.\n\n", renderModes(metrics.Evidence.Modes), metrics.Evidence.LiveWithModelTrace, metrics.Evidence.LiveWithoutModelTrace, metrics.Evidence.RequestedFaultCases, metrics.Evidence.AppliedFaultCases))

	out.WriteString("## Headline metrics\n\n")
	out.WriteString("| metric | value |\n|---|---:|\n")
	rows := [][2]string{
		{"task_success_rate", fmt.Sprintf("%.2f%% (%d/%d)", metrics.TaskSuccessRate*100, metrics.SuccessfulEpisodes, metrics.TotalEpisodes)},
		{"recovery_success_rate", fmt.Sprintf("%.2f%% (%d/%d)", metrics.RecoverySuccessRate*100, metrics.RecoverySuccesses, metrics.RecoveryEpisodes)},
		{"llm_proposal_acceptance_rate", fmt.Sprintf("%.2f%% (%d/%d)", metrics.LLMProposalAcceptanceRate*100, metrics.LLMAccepted, metrics.LLMProposals)},
		{"guard_rejection_rate", fmt.Sprintf("%.2f%% (%d/%d)", metrics.GuardRejectionRate*100, metrics.GuardRejections, metrics.GuardDecisions)},
		{"unsafe_side_effect_count", fmt.Sprint(metrics.UnsafeSideEffectCount)},
		{"memory_retrieval_hit_rate", fmt.Sprintf("%.2f%% (%d/%d)", metrics.MemoryRetrievalHitRate*100, metrics.MemoryRetrievalHits, metrics.MemoryRetrievalAttempts)},
		{"memory_citation_rate", fmt.Sprintf("%.2f%% (%d/%d)", metrics.MemoryCitationRate*100, metrics.MemoryCitationCount, metrics.MemoryRetrievalHits)},
		{"memory_guided_action_success", fmt.Sprintf("%d/%d", metrics.MemoryGuidedActionSuccess, metrics.MemoryGuidedActions)},
		{"duplicate_settlement_count", fmt.Sprint(metrics.DuplicateSettlementCount)},
		{"payment_retry_count", fmt.Sprint(metrics.PaymentRetryCount)},
		{"delivery_retry_count", fmt.Sprint(metrics.DeliveryRetryCount)},
		{"episode_latency_p50/p95_ms", fmt.Sprintf("%.2f / %.2f", metrics.EpisodeLatencyP50MS, metrics.EpisodeLatencyP95MS)},
		{"llm_latency_p50/p95_ms", fmt.Sprintf("%.2f / %.2f", metrics.LLMLatencyP50MS, metrics.LLMLatencyP95MS)},
		{"payment_confirmation_latency_p50/p95_ms", fmt.Sprintf("%.2f / %.2f", metrics.PaymentConfirmationLatencyP50MS, metrics.PaymentConfirmationLatencyP95MS)},
		{"recovery_latency_p50/p95_ms", fmt.Sprintf("%.2f / %.2f", metrics.RecoveryLatencyP50MS, metrics.RecoveryLatencyP95MS)},
	}
	for _, row := range rows {
		out.WriteString("| `" + row[0] + "` | " + row[1] + " |\n")
	}

	out.WriteString("\n## Experiment matrix\n\n")
	renderCells(&out, "Memory on/off", metrics.ExperimentMatrix.MemoryMode)
	renderCells(&out, "LLM vs RuleRecoveryProvider", metrics.ExperimentMatrix.RecoveryProvider)
	renderCells(&out, "Failure injection rate", metrics.ExperimentMatrix.FailureRate)
	renderCells(&out, "Happy vs recovery path", metrics.ExperimentMatrix.Path)

	out.WriteString("## Failure-injection recovery steps\n\n")
	out.WriteString("| failure | samples | applied | recovered | p50 steps | p95 steps | mean steps |\n|---|---:|---:|---:|---:|---:|---:|\n")
	keys := make([]string, 0, len(metrics.FailureInjectionRecoverySteps))
	for key := range metrics.FailureInjectionRecoverySteps {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := metrics.FailureInjectionRecoverySteps[key]
		out.WriteString(fmt.Sprintf("| `%s` | %d | %d | %d | %.2f | %.2f | %.2f |\n", key, value.Samples, value.AppliedCount, value.Recovered, value.P50, value.P95, value.Mean))
	}

	out.WriteString("\n## Episode evidence\n\n")
	out.WriteString("| case | mode | ingress | memory | provider | fault | applied | state | episode_id | payment_tx | artifact | validation | duration ms |\n|---|---|---|---|---|---|---:|---|---|---|---|---|---:|\n")
	for _, result := range results {
		state, episodeID, tx, artifact, validation := "", result.EpisodeID, "", "", ""
		if result.Trace.Episode != nil {
			state = string(result.Trace.Episode.State)
			episodeID = result.Trace.Episode.EpisodeID
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
		out.WriteString(fmt.Sprintf("| `%s` | `%s` | `%s` | `%s` | `%s` | `%s` | %t | `%s` | `%s` | `%s` | `%s` | `%s` | %.2f |\n", result.CaseID, result.Mode, result.Ingress, result.MemoryMode, result.RecoveryProvider, result.Failure.Kind, result.Failure.Applied, state, episodeID, tx, artifact, validation, result.DurationMS))
	}

	out.WriteString("\n## Reproducibility\n\n")
	out.WriteString("Use the committed dataset seed and scenario metadata with `stablepay-agent-eval run`. The harness submits only through the public Runtime HTTP/MCP boundary, polls the public observability endpoint, and writes `episode_results.jsonl`, `metrics.json`, and `report.md`. Faults require an external injector/controller; absent an injector, the report records `applied=false` rather than fabricating a fault result.\n")
	return out.String()
}

func renderCells(out *strings.Builder, title string, cells map[string]Cell) {
	out.WriteString("### " + title + "\n\n")
	out.WriteString("| cell | episodes | task success | recovery success | p50 ms | p95 ms |\n|---|---:|---:|---:|---:|---:|\n")
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
