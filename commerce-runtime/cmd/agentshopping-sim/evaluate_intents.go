package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/stablepay/commerce-runtime/benchmarks/agentshopping"
)

type intentModelRunManifest struct {
	SchemaVersion        string                         "json:\"schema_version\""
	RunID                string                         "json:\"run_id\""
	CreatedAtUTC         string                         "json:\"created_at_utc\""
	Evaluation           string                         "json:\"evaluation\""
	InvocationMode       string                         "json:\"invocation_mode\""
	ToolPolicy           string                         "json:\"tool_policy\""
	Provider             string                         "json:\"provider\""
	Model                string                         "json:\"model\""
	Reasoning            string                         "json:\"reasoning\""
	Temperature          string                         "json:\"temperature\""
	TopP                 string                         "json:\"top_p\""
	MaxTokens            string                         "json:\"max_tokens\""
	PostprocessingPolicy string                         "json:\"postprocessing_policy\""
	UsageStatus          string                         "json:\"usage_status\""
	SuiteStatus          string                         "json:\"suite_status\""
	UsageFile            string                         "json:\"usage_file,omitempty\""
	Usage                agentshopping.ParseUsage       "json:\"usage\""
	PromptTemplateSHA256 string                         "json:\"prompt_template_sha256\""
	SamplingMethod       string                         "json:\"sampling_method\""
	Seed                 int64                          "json:\"seed\""
	TaskCount            int                            "json:\"task_count\""
	ProductCount         int                            "json:\"product_count\""
	InventoryRows        int                            "json:\"inventory_rows\""
	SourceManifest       string                         "json:\"source_manifest\""
	Hashes               agentshopping.EvaluationHashes "json:\"hashes\""
	ScenarioCounts       map[string]int                 "json:\"scenario_counts\""
	RealPaymentCalled    bool                           "json:\"real_payment_called\""
	SimulationDBWritten  bool                           "json:\"simulation_database_written\""
	MetricBoundary       string                         "json:\"metric_boundary\""
}

type intentModelRunReport struct {
	SchemaVersion        string                              "json:\"schema_version\""
	RunID                string                              "json:\"run_id\""
	Evaluation           string                              "json:\"evaluation\""
	Provider             string                              "json:\"provider\""
	Model                string                              "json:\"model\""
	Reasoning            string                              "json:\"reasoning\""
	Temperature          string                              "json:\"temperature\""
	TopP                 string                              "json:\"top_p\""
	MaxTokens            string                              "json:\"max_tokens\""
	PromptTemplateSHA256 string                              "json:\"prompt_template_sha256\""
	Usage                agentshopping.ParseUsage            "json:\"usage\""
	BatchLatencyMillis   int64                               "json:\"batch_latency_ms\""
	Hashes               agentshopping.EvaluationHashes      "json:\"hashes\""
	Metrics              agentshopping.IntentParseEvaluation "json:\"metrics\""
}

func evaluateIntents(args []string) error {
	flags := flag.NewFlagSet("evaluate-intents", flag.ContinueOnError)
	manifestPath := flags.String("manifest", filepath.Join(".local-run", "simulated-merchants", "manifest.json"), "local synthetic Merchant manifest")
	seed := flags.Int64("seed", 20261010, "locked deterministic fixture seed")
	taskCount := flags.Int("tasks", 500, "intent evaluation task count (500 is the locked suite; smaller values are diagnostics)")
	cutoff := flags.Int("cutoff", agentshopping.DefaultRecallCutoff, "downstream candidate recall cutoff")
	bootstrapSamples := flags.Int("bootstrap-samples", 10000, "paired bootstrap resamples")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *taskCount <= 0 || *taskCount > 500 || *cutoff <= 0 || *cutoff > 100 ||
		*bootstrapSamples < 1000 || *bootstrapSamples > 100000 {
		return errors.New("intent evaluation requires 1–500 tasks, cutoff in [1,100], 1,000–100,000 bootstrap samples, and no positional args")
	}
	manifestAbs, err := filepath.Abs(*manifestPath)
	if err != nil {
		return err
	}
	manifestBytes, err := os.ReadFile(manifestAbs)
	if err != nil {
		return fmt.Errorf("read source manifest: %w", err)
	}
	localRunRoot := filepath.Dir(filepath.Dir(manifestAbs))
	expectedOutputRoot := filepath.Join(localRunRoot, "agent-shopping", "evaluations")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	catalog, err := agentshopping.LoadMerchantCatalog(ctx, manifestAbs, nil)
	if err != nil {
		return err
	}
	if catalog.Seed != *seed {
		return fmt.Errorf("source seed %d differs from requested seed %d", catalog.Seed, *seed)
	}
	overlay := make([]agentshopping.OverlayEntry, 0, len(catalog.Inventory))
	for _, row := range catalog.Inventory {
		overlay = append(overlay, agentshopping.OverlayEntry{MerchantID: row.MerchantID, SKUID: row.SKUID,
			Region: row.Region, OnHand: row.OnHand - row.Reserved, Eligible: row.Eligible,
			PriceMinor: row.PriceMinor, Revision: row.Revision})
	}
	lockedTasks, err := agentshopping.GenerateTasks(catalog.Products, overlay, *seed, 500)
	if err != nil {
		return err
	}
	tasks := lockedTasks
	suiteStatus := "DIAGNOSTIC_PARTIAL_NOT_RESUME_EVIDENCE"
	samplingMethod := "STRATIFIED_SCENARIO_SAMPLE_FROM_LOCKED_500"
	if *taskCount == 500 {
		if err := agentshopping.ValidateLockedSuite(catalog, lockedTasks); err != nil {
			return err
		}
		suiteStatus = "LOCKED_COMPLETE"
		samplingMethod = "FULL_LOCKED_500"
	} else {
		tasks, err = selectStratifiedDiagnosticTasks(lockedTasks, *taskCount)
		if err != nil {
			return err
		}
	}
	hashes, err := agentshopping.ComputeEvaluationHashes(catalog, tasks, manifestBytes)
	if err != nil {
		return err
	}

	taxonomy := taxonomyFromCatalog(catalog)
	buyers := agentshopping.BuildSyntheticBuyers(*seed, 2000)
	requests := make([]agentshopping.BatchIntentRequest, 0, len(tasks))
	for index, task := range tasks {
		buyer := buyers[index%len(buyers)]
		if buyer.BudgetMinor < task.Intent.MaxPriceMinor {
			found := false
			for _, candidate := range buyers {
				if candidate.BudgetMinor >= task.Intent.MaxPriceMinor {
					buyer = candidate
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("locked task %s exceeds every synthetic buyer budget", task.TaskID)
			}
		}
		requests = append(requests, agentshopping.BatchIntentRequest{TaskID: task.TaskID, Prompt: task.Prompt,
			DefaultRegion: buyer.HomeRegion, BuyerBudgetMinor: buyer.BudgetMinor})
	}
	usageDir := filepath.Join(localRunRoot, "agent-shopping", "model-usage")
	if err := os.MkdirAll(usageDir, 0o700); err != nil {
		return fmt.Errorf("create ignored local model-usage directory: %w", err)
	}
	previousUsageDir, hadUsageDir := os.LookupEnv("SHOP_SIM_USAGE_DIR")
	if err := os.Setenv("SHOP_SIM_USAGE_DIR", usageDir); err != nil {
		return err
	}
	defer func() {
		if hadUsageDir {
			_ = os.Setenv("SHOP_SIM_USAGE_DIR", previousUsageDir)
		} else {
			_ = os.Unsetenv("SHOP_SIM_USAGE_DIR")
		}
	}()
	parser := agentshopping.NewHermesIntentParser("", "openai-codex", "gpt-6-luna")
	started := time.Now()
	results, usage, parseErr := parser.ParseBatchWithUsage(ctx, requests, taxonomy)
	batchLatency := time.Since(started)
	if parseErr != nil {
		return fmt.Errorf("GPT-6 Luna batch intent evaluation failed (usage_status=%s): %w", usage.UsageStatus, parseErr)
	}
	metrics, trials, err := agentshopping.EvaluateParsedIntentBatch(catalog, tasks, results, *cutoff, *bootstrapSamples, *seed)
	if err != nil {
		return err
	}
	scenarioCounts := make(map[string]int)
	for _, task := range tasks {
		scenarioCounts[task.Scenario]++
	}
	createdAt := time.Now().UTC()
	runID := "shopping-intents-" + createdAt.Format("20060102T150405.000000000Z")
	temperature := "UNREPORTED_BY_HERMES_CLI"
	topP := "UNREPORTED_BY_HERMES_CLI"
	maxTokens := "UNREPORTED_BY_HERMES_CLI"
	manifest := intentModelRunManifest{SchemaVersion: "stablepay.agent-shopping-intent-run.v1", RunID: runID,
		CreatedAtUTC: createdAt.Format(time.RFC3339Nano), Evaluation: "gpt-6-luna-intent-extraction-and-downstream-ranking",
		InvocationMode: fmt.Sprintf("%d-batches-of-at-most-%d-tasks", (len(tasks)+49)/50, 50), Provider: parser.Provider(), Model: parser.Model(),
		ToolPolicy: "Hermes safe-mode with explicit bot_room allowlist; locally verified empty tool definitions",
		Reasoning:  usage.Reasoning, Temperature: temperature, TopP: topP, MaxTokens: maxTokens,
		UsageStatus: usage.UsageStatus, SuiteStatus: suiteStatus, UsageFile: usage.UsageFile, Usage: usage,
		SamplingMethod:       samplingMethod,
		PromptTemplateSHA256: usage.TemplateSHA256, Seed: *seed, TaskCount: len(tasks),
		ProductCount: len(catalog.Products), InventoryRows: len(catalog.Inventory),
		SourceManifest: ".local-run/simulated-merchants/manifest.json", Hashes: hashes,
		ScenarioCounts: scenarioCounts, RealPaymentCalled: false, SimulationDBWritten: false,
		PostprocessingPolicy: "exact item-to-category mapping and explicit format/style alias normalization; raw model and normalized pipeline exact rates reported separately",
		MetricBoundary:       fmt.Sprintf("GPT-6 Luna parses %d synthetic requests in chunks of at most 50 (suite_status=%s, sampling_method=%s). Downstream B1 is evaluated against the isolated deterministic catalog; this is not production API latency, real payment, or Devnet evidence.", len(tasks), suiteStatus, samplingMethod)}
	report := intentModelRunReport{SchemaVersion: "stablepay.agent-shopping-intent-report.v1", RunID: runID,
		Evaluation: manifest.Evaluation, Provider: parser.Provider(), Model: parser.Model(), Reasoning: usage.Reasoning,
		Temperature: temperature, TopP: topP, MaxTokens: maxTokens, PromptTemplateSHA256: usage.TemplateSHA256,
		Usage: usage, BatchLatencyMillis: batchLatency.Milliseconds(), Hashes: hashes, Metrics: metrics}
	if err := os.MkdirAll(expectedOutputRoot, 0o700); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(expectedOutputRoot, ".staging-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := writeJSONLines(filepath.Join(staging, "suite.jsonl"), tasks); err != nil {
		return err
	}
	if err := writeJSONLines(filepath.Join(staging, "intent-trials.jsonl"), trials); err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(staging, "intent-metrics.json"), report); err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(staging, "manifest.json"), manifest); err != nil {
		return err
	}
	if len(usage.RawResponses) > 0 {
		if err := writeJSONLines(filepath.Join(staging, "model-responses.jsonl"), usage.RawResponses); err != nil {
			return fmt.Errorf("write local raw model responses artifact: %w", err)
		}
	} else if usage.RawResponse != "" {
		if err := os.WriteFile(filepath.Join(staging, "model-responses.jsonl"), []byte(usage.RawResponse), 0o600); err != nil {
			return fmt.Errorf("write local raw model response artifact: %w", err)
		}
	}
	if len(usage.RawStreams) > 0 {
		if err := writeJSONLines(filepath.Join(staging, "model-streams.jsonl"), usage.RawStreams); err != nil {
			return fmt.Errorf("write local raw Hermes stream artifact: %w", err)
		}
	}
	finalDir := filepath.Join(expectedOutputRoot, runID)
	if _, err := os.Stat(finalDir); err == nil {
		return fmt.Errorf("evaluation run already exists: %s", finalDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(staging, finalDir); err != nil {
		return fmt.Errorf("publish intent evaluation artifacts: %w", err)
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"run_id": runID, "run_dir": finalDir, "provider": parser.Provider(), "model": parser.Model(),
		"task_count": len(tasks), "suite_status": suiteStatus, "parse_success_rate": metrics.ParseSuccessRate,
		"exact_intent_match_rate":           metrics.ExactIntentMatchRate,
		"raw_model_exact_intent_match_rate": metrics.RawModelExactRate,
		"canonicalizations":                 metrics.Canonicalizations,
		"downstream_valid_plan_rate":        metrics.DownstreamTreatment.ValidPlanRate,
		"usage_status":                      usage.UsageStatus, "input_tokens": usage.InputTokens, "output_tokens": usage.OutputTokens,
		"auxiliary_input_tokens": usage.AuxInputTokens, "auxiliary_output_tokens": usage.AuxOutputTokens,
		"tool_call_rejected_batches": usage.ToolCallRejectedBatches,
		"api_calls_status":           usage.APICallsStatus, "hermes_cli_runs": usage.HermesCLIRuns, "tasks_sha256": hashes.TasksSHA256,
	})
}

func selectStratifiedDiagnosticTasks(tasks []agentshopping.Task, count int) ([]agentshopping.Task, error) {
	if count <= 0 || count >= len(tasks) {
		return nil, fmt.Errorf("stratified diagnostic sample size must be between 1 and %d", len(tasks)-1)
	}
	byScenario := make(map[string][]agentshopping.Task)
	for _, task := range tasks {
		byScenario[task.Scenario] = append(byScenario[task.Scenario], task)
	}
	scenarios := make([]string, 0, len(byScenario))
	for scenario := range byScenario {
		scenarios = append(scenarios, scenario)
	}
	sort.Strings(scenarios)
	type scenarioQuota struct {
		scenario  string
		count     int
		remainder int
	}
	quotas := make([]scenarioQuota, 0, len(scenarios))
	assigned := 0
	for _, scenario := range scenarios {
		product := count * len(byScenario[scenario])
		quotas = append(quotas, scenarioQuota{scenario: scenario, count: product / len(tasks), remainder: product % len(tasks)})
		assigned += product / len(tasks)
	}
	for assigned < count {
		best := -1
		for i := range quotas {
			if quotas[i].count >= len(byScenario[quotas[i].scenario]) {
				continue
			}
			if best == -1 || quotas[i].remainder > quotas[best].remainder ||
				(quotas[i].remainder == quotas[best].remainder && quotas[i].scenario < quotas[best].scenario) {
				best = i
			}
		}
		if best == -1 {
			return nil, errors.New("could not allocate stratified diagnostic sample")
		}
		quotas[best].count++
		quotas[best].remainder = -1
		assigned++
	}

	selected := make([]agentshopping.Task, 0, count)
	for _, q := range quotas {
		bucket := byScenario[q.scenario]
		for i := 0; i < q.count; i++ {
			index := ((2*i + 1) * len(bucket)) / (2 * q.count)
			if index >= len(bucket) {
				index = len(bucket) - 1
			}
			selected = append(selected, bucket[index])
		}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].TaskID < selected[j].TaskID })
	return selected, nil
}

func taxonomyFromCatalog(catalog agentshopping.ImportedCatalog) agentshopping.Taxonomy {
	return agentshopping.TaxonomyFromProducts(catalog.Products)
}
