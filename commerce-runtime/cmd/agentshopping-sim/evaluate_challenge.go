package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/benchmarks/agentshopping"
)

type challengeRunManifest struct {
	SchemaVersion                string                                  `json:"schema_version"`
	RunID                        string                                  `json:"run_id"`
	CreatedAtUTC                 string                                  `json:"created_at_utc"`
	Evaluation                   string                                  `json:"evaluation"`
	SuiteStatus                  string                                  `json:"suite_status"`
	TaskProvenanceStatus         string                                  `json:"task_provenance_status"`
	UnreviewedDiagnosticModelRun bool                                    `json:"unreviewed_diagnostic_model_run"`
	TaskSource                   string                                  `json:"task_source"`
	HumanReviewBundlePath        string                                  `json:"human_review_bundle_path,omitempty"`
	HumanReviewBundleStatus      string                                  `json:"human_review_bundle_status,omitempty"`
	HumanReviewAuditSHA256       string                                  `json:"human_review_audit_sha256,omitempty"`
	HoldoutProtocol              *humanHoldoutProtocolDefinition         `json:"holdout_protocol,omitempty"`
	HoldoutProtocolSHA256        string                                  `json:"holdout_protocol_sha256,omitempty"`
	TaskCount                    int                                     `json:"task_count"`
	RecallCutoff                 int                                     `json:"recall_cutoff"`
	DiscoveryWindow              int                                     `json:"discovery_candidate_window"`
	ScenarioCounts               map[string]int                          `json:"scenario_counts"`
	SourceManifest               string                                  `json:"source_manifest"`
	Hashes                       agentshopping.ChallengeEvaluationHashes `json:"hashes"`
	ArtifactsSHA256              map[string]string                       `json:"artifacts_sha256"`
	EvaluationSourceSHA256       map[string]string                       `json:"evaluation_source_sha256"`
	Provider                     string                                  `json:"provider"`
	Model                        string                                  `json:"model"`
	Reasoning                    string                                  `json:"reasoning"`
	Temperature                  string                                  `json:"temperature"`
	TopP                         string                                  `json:"top_p"`
	MaxTokens                    string                                  `json:"max_tokens"`
	PromptTemplateSHA256         string                                  `json:"prompt_template_sha256"`
	PromptSHA256                 string                                  `json:"prompt_sha256"`
	ResponseSHA256               string                                  `json:"response_sha256"`
	UsageStatus                  string                                  `json:"usage_status"`
	APICallsStatus               string                                  `json:"api_calls_status"`
	Usage                        agentshopping.ParseUsage                `json:"usage"`
	BatchLatencyMillis           int64                                   `json:"batch_latency_ms"`
	RealPaymentCalled            bool                                    `json:"real_payment_called"`
	SimulationDBWritten          bool                                    `json:"simulation_database_written"`
	MetricBoundary               string                                  `json:"metric_boundary"`
	RunMode                      string                                  `json:"run_mode,omitempty"`
	RelevanceLabelReplay         bool                                    `json:"relevance_label_replay,omitempty"`
	ReplaySource                 *challengeReplaySource                  `json:"replay_source,omitempty"`
}

type challengeReplaySource struct {
	RunID                string `json:"run_id"`
	ParseResultsPath     string `json:"parse_results_path"`
	ParseResultsSHA256   string `json:"parse_results_sha256"`
	ManifestPath         string `json:"manifest_path"`
	ManifestSHA256       string `json:"manifest_sha256"`
	Provider             string `json:"provider"`
	Model                string `json:"model"`
	PromptTemplateSHA256 string `json:"prompt_template_sha256"`
	PromptSHA256         string `json:"prompt_sha256"`
	ResponseSHA256       string `json:"response_sha256"`
	UsageStatus          string `json:"usage_status"`
	APICallsStatus       string `json:"api_calls_status"`
	InputTokens          int64  `json:"input_tokens"`
	OutputTokens         int64  `json:"output_tokens"`
	HermesCLIRuns        int64  `json:"hermes_cli_runs"`
	SourceTaskSHA256     string `json:"source_task_sha256,omitempty"`
	EvaluationTaskSHA256 string `json:"evaluation_task_sha256,omitempty"`
	RelevanceLabelsAdded bool   `json:"relevance_labels_added,omitempty"`
}

type humanHoldoutStratum struct {
	TaskCount      int                                `json:"task_count"`
	Disposition    agentshopping.ChallengeDisposition `json:"expected_disposition"`
	PromptingFocus string                             `json:"prompting_focus"`
}

const humanHoldoutProtocolVersion = "stablepay.agent-shopping-human-holdout.v1"
const humanHoldoutTaskCount = 500

type humanHoldoutProtocolDefinition struct {
	Version   string                         `json:"version"`
	TaskCount int                            `json:"task_count"`
	Strata    map[string]humanHoldoutStratum `json:"strata"`
}

// Keep the official holdout mix frozen in source so task selection cannot be
// changed after outcomes are observed. Stratum IDs are intentionally opaque in
// reviewer-facing prompt packets; the author/coordinator retains this mapping.
var humanHoldoutStrata = map[string]humanHoldoutStratum{
	"S01": {TaskCount: 50, Disposition: agentshopping.ChallengePlan, PromptingFocus: "ordinary wording for an exact requested product"},
	"S02": {TaskCount: 50, Disposition: agentshopping.ChallengePlan, PromptingFocus: "conjunction of multiple hard product attributes"},
	"S03": {TaskCount: 50, Disposition: agentshopping.ChallengePlan, PromptingFocus: "aliases and close product or format variants"},
	"S04": {TaskCount: 50, Disposition: agentshopping.ChallengePlan, PromptingFocus: "budget, quantity, and total-cost boundaries"},
	"S05": {TaskCount: 50, Disposition: agentshopping.ChallengePlan, PromptingFocus: "ranking with multiple soft-preference tradeoffs"},
	"S06": {TaskCount: 50, Disposition: agentshopping.ChallengePlan, PromptingFocus: "explicitly accepted substitutes, distinct from exact targets"},
	"S07": {TaskCount: 50, Disposition: agentshopping.ChallengePlan, PromptingFocus: "current stock, region, and price feasibility"},
	"S08": {TaskCount: 25, Disposition: agentshopping.ChallengeNoPlan, PromptingFocus: "no offer satisfies the stated budget"},
	"S09": {TaskCount: 25, Disposition: agentshopping.ChallengeNoPlan, PromptingFocus: "no offer serves the required delivery region"},
	"S10": {TaskCount: 25, Disposition: agentshopping.ChallengeNoPlan, PromptingFocus: "no offer satisfies the hard-attribute conjunction"},
	"S11": {TaskCount: 25, Disposition: agentshopping.ChallengeClarify, PromptingFocus: "missing or under-specified product identity"},
	"S12": {TaskCount: 25, Disposition: agentshopping.ChallengeClarify, PromptingFocus: "missing required attribute or buyer choice"},
	"S13": {TaskCount: 25, Disposition: agentshopping.ChallengeClarify, PromptingFocus: "ambiguous hard constraint requiring a targeted question"},
}

type challengeRunReport struct {
	SchemaVersion        string                                  `json:"schema_version"`
	RunID                string                                  `json:"run_id"`
	Evaluation           string                                  `json:"evaluation"`
	Provider             string                                  `json:"provider"`
	Model                string                                  `json:"model"`
	Reasoning            string                                  `json:"reasoning"`
	PromptTemplateSHA256 string                                  `json:"prompt_template_sha256"`
	Hashes               agentshopping.ChallengeEvaluationHashes `json:"hashes"`
	Metrics              agentshopping.ChallengeEvaluationReport `json:"metrics"`
}

func evaluateChallenge(args []string) error {
	flags := flag.NewFlagSet("evaluate-challenge", flag.ContinueOnError)
	manifestPath := flags.String("manifest", filepath.Join(".local-run", "simulated-merchants", "manifest.json"), "local synthetic Merchant manifest")
	tasksPath := flags.String("tasks", "", "explicit challenge tasks JSONL under the local challenge-suites directory (required)")
	seed := flags.Int64("seed", 20261010, "expected synthetic Merchant catalog seed")
	cutoff := flags.Int("cutoff", agentshopping.DefaultRecallCutoff, "Recall@K cutoff and maximum feasible-offer response count (1–100)")
	oracleIntents := flags.Bool("oracle-intents", false, "use each task's gold intent to isolate B0/B1 ranking; makes no model calls and is never resume evidence")
	replayParseResultsPath := flags.String("replay-parse-results", "", "reprocess a prior run's parse-results.jsonl offline; makes no model calls")
	allowRelevanceLabelReplay := flags.Bool("allow-relevance-label-only-replay", false,
		"allow offline replay when the only task change is adding previously missing relevant_skus labels; all source tasks must be artifact-hash verified")
	requireHumanReview := flags.Bool("require-human-review", false, "require an exactly 500-task recomputable double-review bundle; identities and review-process declarations remain self-attested")
	reviewBundlePath := flags.String("review-bundle", "", "finalized review bundle directory matching --tasks; required with --require-human-review")
	allowUnreviewedDiagnostic := flags.Bool("allow-unreviewed-diagnostic-model-run", false, "explicitly allow a model call on tasks without a recomputable human-review bundle; result is diagnostic only")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *cutoff <= 0 || *cutoff > 100 || strings.TrimSpace(*tasksPath) == "" {
		return errors.New("evaluate-challenge requires an explicit --tasks path, cutoff in [1,100], and no positional arguments")
	}
	if *oracleIntents && strings.TrimSpace(*replayParseResultsPath) != "" {
		return errors.New("--oracle-intents and --replay-parse-results are mutually exclusive")
	}
	if *allowRelevanceLabelReplay && strings.TrimSpace(*replayParseResultsPath) == "" {
		return errors.New("--allow-relevance-label-only-replay requires --replay-parse-results")
	}
	if err := validateChallengeModelRunPermission(*oracleIntents, *replayParseResultsPath,
		*requireHumanReview, *allowUnreviewedDiagnostic); err != nil {
		return err
	}
	if *requireHumanReview && strings.TrimSpace(*reviewBundlePath) == "" {
		return errors.New("--require-human-review requires --review-bundle with prompts, two annotation streams, adjudications, finalized tasks and review audit")
	}
	manifestAbs, err := filepath.Abs(*manifestPath)
	if err != nil {
		return err
	}
	tasksAbs, err := filepath.Abs(*tasksPath)
	if err != nil {
		return err
	}
	reviewBundleAbs := ""
	if strings.TrimSpace(*reviewBundlePath) != "" {
		reviewBundleAbs, err = filepath.Abs(*reviewBundlePath)
		if err != nil {
			return err
		}
	}
	replayParseResultsAbs := ""
	if strings.TrimSpace(*replayParseResultsPath) != "" {
		replayParseResultsAbs, err = filepath.Abs(*replayParseResultsPath)
		if err != nil {
			return err
		}
	}
	unreviewedDiagnosticModelRun := !*oracleIntents && strings.TrimSpace(*replayParseResultsPath) == "" && !*requireHumanReview
	manifestDir := filepath.Dir(manifestAbs)
	if filepath.Base(manifestDir) != "simulated-merchants" || filepath.Base(filepath.Dir(manifestDir)) != ".local-run" {
		return errors.New("source manifest must be under .local-run/simulated-merchants")
	}
	localRunRoot := filepath.Dir(manifestDir)
	challengeSourceRoot := filepath.Join(localRunRoot, "agent-shopping", "challenge-suites")
	relativeTaskPath, err := filepath.Rel(challengeSourceRoot, tasksAbs)
	if err != nil || !pathWithinDirectory(challengeSourceRoot, tasksAbs) || relativeTaskPath == "." || filepath.IsAbs(relativeTaskPath) {
		return errors.New("challenge task file must remain under .local-run/agent-shopping/challenge-suites")
	}
	localRunReal, err := filepath.EvalSymlinks(localRunRoot)
	if err != nil {
		return fmt.Errorf("resolve local-run root: %w", err)
	}
	challengeRootReal, err := filepath.EvalSymlinks(challengeSourceRoot)
	if err != nil || !pathWithinDirectory(localRunReal, challengeRootReal) {
		return errors.New("challenge-suite directory resolves outside the local .local-run root")
	}
	manifestReal, err := filepath.EvalSymlinks(manifestAbs)
	if err != nil || !pathWithinDirectory(localRunReal, manifestReal) {
		return errors.New("source manifest resolves outside the local .local-run root")
	}
	tasksReal, err := filepath.EvalSymlinks(tasksAbs)
	if err != nil || !pathWithinDirectory(challengeRootReal, tasksReal) {
		return errors.New("challenge task file resolves outside the local challenge-suite directory")
	}
	// Validate and hash the scoring implementation before any model batches run.
	// localRunRoot is the .local-run directory; source paths are repository-relative.
	evaluationSourceHashes, err := hashChallengeEvaluationSources(filepath.Dir(localRunRoot))
	if err != nil {
		return fmt.Errorf("hash challenge evaluation sources: %w", err)
	}
	bundleReal := ""
	if *requireHumanReview {
		if !samePath(filepath.Dir(reviewBundleAbs), challengeSourceRoot) || samePath(reviewBundleAbs, challengeSourceRoot) {
			return errors.New("review bundle must be a direct child of .local-run/agent-shopping/challenge-suites")
		}
		bundleInfo, statErr := os.Lstat(reviewBundleAbs)
		if statErr != nil || !bundleInfo.IsDir() || bundleInfo.Mode()&os.ModeSymlink != 0 {
			return errors.New("review bundle must be an existing, non-symlink directory under challenge-suites")
		}
		bundleReal, err = filepath.EvalSymlinks(reviewBundleAbs)
		if err != nil || !samePath(bundleReal, reviewBundleAbs) || !pathWithinDirectory(challengeRootReal, bundleReal) {
			return errors.New("review bundle resolves outside the local challenge-suite directory")
		}
		if !samePath(tasksReal, filepath.Join(bundleReal, "tasks.jsonl")) {
			return errors.New("--tasks must point to tasks.jsonl inside --review-bundle")
		}
	}
	manifestBytes, err := os.ReadFile(manifestAbs)
	if err != nil {
		return fmt.Errorf("read source Merchant manifest: %w", err)
	}
	tasks, err := readChallengeTasks(tasksAbs)
	if err != nil {
		return err
	}
	if err := validateHumanHoldoutTaskCount(len(tasks), *requireHumanReview); err != nil {
		return err
	}
	if err := validateHumanHoldoutStrata(tasks, *requireHumanReview); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()
	catalog, err := agentshopping.LoadMerchantCatalog(ctx, manifestAbs, nil)
	if err != nil {
		return err
	}
	if catalog.Seed != *seed {
		return fmt.Errorf("source seed %d differs from requested seed %d", catalog.Seed, *seed)
	}
	if err := agentshopping.ValidateChallengeTaskSet(catalog, tasks); err != nil {
		return fmt.Errorf("challenge labels do not match the pinned catalog/inventory snapshot: %w", err)
	}
	hashes, err := agentshopping.ComputeChallengeHashes(catalog, tasks, manifestBytes)
	if err != nil {
		return err
	}
	provenanceStatus := agentshopping.ChallengeTaskProvenanceStatus(tasks, hashes)
	reviewAuditDigest := ""
	if *requireHumanReview {
		if err := agentshopping.ValidateChallengeTaskProvenance(tasks, hashes); err != nil {
			return fmt.Errorf("challenge suite failed the required human-review gate: %w", err)
		}
		audit, digest, err := verifyChallengeReviewBundle(bundleReal, tasksReal, catalog, manifestBytes)
		if err != nil {
			return fmt.Errorf("challenge suite failed the recomputable human-review bundle gate: %w", err)
		}
		if audit.Hashes != hashes {
			return errors.New("review bundle catalog, inventory, task, or manifest hashes do not match this evaluation")
		}
		reviewAuditDigest = digest
		provenanceStatus = "HUMAN_REVIEW_BUNDLE_RECOMPUTED_IDENTITIES_AND_PROCESS_SELF_ATTESTED"
	}
	taxonomy := taxonomyFromCatalog(catalog)
	requests := make([]agentshopping.BatchIntentRequest, 0, len(tasks))
	for _, task := range tasks {
		requests = append(requests, challengeParserRequest(task))
	}
	var results []agentshopping.BatchIntentResult
	var usage agentshopping.ParseUsage
	provider, model := "openai-codex", "gpt-6-luna"
	temperature, topP, maxTokens := "UNREPORTED_BY_HERMES_CLI", "UNREPORTED_BY_HERMES_CLI", "UNREPORTED_BY_HERMES_CLI"
	evaluationName := "gpt-6-luna-agent-shopping-b0-b1-challenge"
	runMode := "MODEL_PARSE_AND_EVALUATION"
	if unreviewedDiagnosticModelRun {
		runMode = "UNREVIEWED_DIAGNOSTIC_MODEL_PARSE_AND_EVALUATION"
	}
	var replaySource *challengeReplaySource
	started := time.Now()
	if *oracleIntents {
		results = challengeOracleIntentResults(tasks)
		provider, model = "NOT_USED_ORACLE_INTENT", "NOT_USED_ORACLE_INTENT"
		usage = agentshopping.ParseUsage{Reasoning: "NOT_APPLICABLE", UsageStatus: "NOT_APPLICABLE_ORACLE_INTENTS",
			APICallsStatus: "NOT_APPLICABLE_NO_MODEL_CALLS"}
		temperature, topP, maxTokens = "NOT_APPLICABLE_ORACLE_INTENTS", "NOT_APPLICABLE_ORACLE_INTENTS", "NOT_APPLICABLE_ORACLE_INTENTS"
		evaluationName = "oracle-intent-agent-shopping-b0-b1-ranker-diagnostic"
		runMode = "ORACLE_INTENT_EVALUATION_NO_MODEL_CALLS"
	} else if replayParseResultsAbs != "" {
		var source *challengeReplaySource
		var sourceManifest challengeRunManifest
		var storedResults []agentshopping.BatchIntentResult
		var loadErr error
		if *allowRelevanceLabelReplay {
			source, sourceManifest, storedResults, loadErr = loadChallengeReplayResultsWithRelevanceEnrichment(
				localRunRoot, replayParseResultsAbs, hashes, len(tasks), tasks)
		} else {
			source, sourceManifest, storedResults, loadErr = loadChallengeReplayResults(localRunRoot,
				replayParseResultsAbs, hashes, len(tasks))
		}
		if loadErr != nil {
			return loadErr
		}
		results, err = agentshopping.ReplayChallengeBatchResults(requests, storedResults, taxonomy)
		if err != nil {
			return fmt.Errorf("revalidate stored challenge parse results: %w", err)
		}
		replaySource = source
		provider, model = source.Provider, source.Model
		temperature, topP, maxTokens = sourceManifest.Temperature, sourceManifest.TopP, sourceManifest.MaxTokens
		evaluationName = "offline-replay-" + sourceManifest.Evaluation
		runMode = "OFFLINE_STORED_PARSE_REPLAY_NO_NEW_MODEL_CALLS"
		if *allowRelevanceLabelReplay {
			evaluationName = "offline-relevance-label-replay-" + sourceManifest.Evaluation
			runMode = "OFFLINE_STORED_PARSE_REPLAY_RELEVANCE_LABEL_ENRICHMENT_NO_NEW_MODEL_CALLS"
		}
		usage = agentshopping.ParseUsage{TemplateSHA256: source.PromptTemplateSHA256,
			PromptSHA256: source.PromptSHA256, ResponseSHA256: source.ResponseSHA256,
			Reasoning: "NOT_APPLICABLE_OFFLINE_REPLAY", UsageStatus: "NOT_APPLICABLE_REPLAY_NO_NEW_MODEL_CALLS",
			APICallsStatus: "NOT_APPLICABLE_NO_MODEL_CALLS"}
	} else {
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
		parser := agentshopping.NewHermesIntentParser("", provider, model)
		parsedResults, parsedUsage, parseErr := parser.ParseChallengeBatchWithUsage(ctx, requests, taxonomy)
		if parseErr != nil {
			return fmt.Errorf("GPT-6 Luna challenge parsing failed (usage_status=%s): %w", parsedUsage.UsageStatus, parseErr)
		}
		results, usage = parsedResults, parsedUsage
	}
	batchLatency := time.Since(started)
	metrics, trials, err := agentshopping.EvaluateChallengeTasks(catalog, tasks, results, *cutoff)
	if err != nil {
		return err
	}
	metrics.Hashes = hashes
	metrics.DataOrigin = provenanceStatus
	scenarioCounts := make(map[string]int)
	for _, task := range tasks {
		scenarioCounts[task.Scenario]++
	}
	createdAt := time.Now().UTC()
	runID := "shopping-challenge-" + createdAt.Format("20060102T150405.000000000Z")
	suiteStatus := "DIAGNOSTIC_SUBSET_NOT_RESUME_EVIDENCE"
	if replaySource != nil {
		suiteStatus = "OFFLINE_PARSE_REPLAY_DIAGNOSTIC_NOT_RESUME_EVIDENCE"
		if replaySource.RelevanceLabelsAdded {
			suiteStatus = "OFFLINE_RELEVANCE_ENRICHED_PARSE_REPLAY_DIAGNOSTIC_NOT_RESUME_EVIDENCE"
		}
	} else if *oracleIntents {
		suiteStatus = "ORACLE_INTENT_DIAGNOSTIC_NOT_RESUME_EVIDENCE"
	} else if reviewAuditDigest != "" {
		suiteStatus = "HUMAN_REVIEW_BUNDLE_VERIFIED_NOT_RESUME_EVIDENCE"
	} else if provenanceStatus == agentshopping.ChallengeProvenanceDeclared {
		suiteStatus = "HUMAN_REVIEW_DECLARED_NOT_EXTERNALLY_AUDITED"
	} else if len(tasks) == 500 {
		suiteStatus = "500_TASKS_PROVENANCE_UNVERIFIED"
	}
	metricBoundary := challengeMetricBoundary(*oracleIntents, provenanceStatus)
	if unreviewedDiagnosticModelRun {
		metricBoundary += " This model-backed run was explicitly authorized as an unreviewed diagnostic and is not resume evidence."
	}
	if reviewAuditDigest != "" {
		metricBoundary += " The complete double-review bundle was recomputed against this exact catalog, inventory, task file and source manifest; pseudonymous identity and blind/freeze declarations remain self-attested, not externally authenticated."
	}
	if replaySource != nil {
		metricBoundary = "OFFLINE REPLAY of stored structured model outputs; no model/API calls were made in this run. Source run: " + replaySource.RunID + ". " + challengeMetricBoundary(false, provenanceStatus)
		if replaySource.RelevanceLabelsAdded {
			metricBoundary = "OFFLINE RELEVANCE-LABEL-ENRICHED REPLAY: source suite and model outputs are artifact-hash verified; task prompts, intents, decision labels, gold labels, task order, catalog, inventory, and source manifest are unchanged. Only relevant_skus was added, so prompt/model output was not regenerated. These exact-target labels are algorithmically derived from the same synthetic catalog, not independently reviewed. No model/API calls were made. Source run: " + replaySource.RunID + ". " + challengeMetricBoundary(false, provenanceStatus)
		}
	}
	manifest := challengeRunManifest{SchemaVersion: "stablepay.agent-shopping-challenge-run.v5", RunID: runID,
		CreatedAtUTC: createdAt.Format(time.RFC3339Nano), Evaluation: evaluationName,
		SuiteStatus: suiteStatus, TaskProvenanceStatus: provenanceStatus,
		UnreviewedDiagnosticModelRun: unreviewedDiagnosticModelRun,
		TaskSource:                   filepath.ToSlash(filepath.Join(".local-run", "agent-shopping", "challenge-suites", relativeTaskPath)),
		HumanReviewBundlePath: func() string {
			if reviewAuditDigest == "" {
				return ""
			}
			return filepath.ToSlash(filepath.Join(".local-run", "agent-shopping", "challenge-suites", filepath.Base(reviewBundleAbs)))
		}(),
		HumanReviewBundleStatus: func() string {
			if reviewAuditDigest == "" {
				return ""
			}
			return "RECOMPUTED_MATCH_IDENTITIES_AND_PROCESS_SELF_ATTESTED"
		}(),
		HumanReviewAuditSHA256: reviewAuditDigest,
		TaskCount:              len(tasks), RecallCutoff: *cutoff, DiscoveryWindow: metrics.DiscoveryCandidateWindow, ScenarioCounts: scenarioCounts,
		SourceManifest: filepath.ToSlash(filepath.Join(".local-run", "simulated-merchants", filepath.Base(manifestAbs))), Hashes: hashes,
		EvaluationSourceSHA256: evaluationSourceHashes,
		Provider:               provider, Model: model, Reasoning: usage.Reasoning,
		Temperature: temperature, TopP: topP, MaxTokens: maxTokens,
		PromptTemplateSHA256: usage.TemplateSHA256, PromptSHA256: usage.PromptSHA256, ResponseSHA256: usage.ResponseSHA256,
		UsageStatus: usage.UsageStatus, APICallsStatus: usage.APICallsStatus, Usage: usage,
		BatchLatencyMillis: batchLatency.Milliseconds(), RealPaymentCalled: false, SimulationDBWritten: false,
		MetricBoundary: metricBoundary, RunMode: runMode, RelevanceLabelReplay: replaySource != nil && replaySource.RelevanceLabelsAdded,
		ReplaySource: replaySource}
	if *requireHumanReview {
		protocol, protocolHash, protocolErr := humanHoldoutProtocolIdentity()
		if protocolErr != nil {
			return fmt.Errorf("identify frozen human holdout protocol: %w", protocolErr)
		}
		manifest.HoldoutProtocol = &protocol
		manifest.HoldoutProtocolSHA256 = protocolHash
	}
	report := challengeRunReport{SchemaVersion: "stablepay.agent-shopping-challenge-report.v5", RunID: runID,
		Evaluation: manifest.Evaluation, Provider: provider, Model: model, Reasoning: usage.Reasoning,
		PromptTemplateSHA256: usage.TemplateSHA256, Hashes: hashes, Metrics: metrics}
	outputRoot := filepath.Join(localRunRoot, "agent-shopping", "challenge-evaluations")
	if err := os.MkdirAll(outputRoot, 0o700); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(outputRoot, ".staging-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := writeJSONLines(filepath.Join(staging, "suite.jsonl"), tasks); err != nil {
		return err
	}
	if err := writeJSONLines(filepath.Join(staging, "parse-results.jsonl"), results); err != nil {
		return err
	}
	if err := writeJSONLines(filepath.Join(staging, "trials.jsonl"), trials); err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(staging, "metrics.json"), report); err != nil {
		return err
	}
	if len(usage.RawResponses) > 0 {
		if err := writeJSONLines(filepath.Join(staging, "model-responses.jsonl"), usage.RawResponses); err != nil {
			return err
		}
	}
	manifest.ArtifactsSHA256, err = hashChallengeRunArtifacts(staging)
	if err != nil {
		return fmt.Errorf("hash challenge run artifacts: %w", err)
	}
	if err := writeJSONFile(filepath.Join(staging, "manifest.json"), manifest); err != nil {
		return err
	}
	if len(usage.RawStreams) > 0 {
		if err := writeJSONLines(filepath.Join(staging, "model-streams.jsonl"), usage.RawStreams); err != nil {
			return err
		}
	}
	finalDir := filepath.Join(outputRoot, runID)
	if _, err := os.Stat(finalDir); err == nil {
		return fmt.Errorf("challenge evaluation run already exists: %s", finalDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(staging, finalDir); err != nil {
		return fmt.Errorf("publish challenge evaluation artifacts: %w", err)
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"run_id": runID, "run_dir": finalDir, "suite_status": suiteStatus, "run_mode": runMode,
		"unreviewed_diagnostic_model_run": unreviewedDiagnosticModelRun,
		"replay_source_run_id":            replaySourceRunID(replaySource),
		"task_count":                      metrics.TaskCount, "plan_tasks": metrics.PlanTasks,
		"correct_decision_rate_all_tasks": metrics.CorrectDecisionRate,
		"discovery_candidate_window":      metrics.DiscoveryCandidateWindow,
		"feasible_sku_recall_at_k":        metrics.FeasibleSKURecallAtK, "plan_task_hit_at_k": metrics.PlanTaskHitAtK,
		"treatment_feasible_offer_sku_recall_at_k": metrics.TreatmentFeasibleOfferSKURecallAtK,
		"treatment_feasible_offer_task_hit_at_k":   metrics.TreatmentFeasibleOfferTaskHitAtK,
		"target_product_recall_at_k":               metrics.TargetProductRecallAtK, "target_product_task_hit_at_k": metrics.TargetProductTaskHitAtK,
		"baseline_valid_plan_rate": metrics.PlanComparison.BaselineValidPlanRate,
		"valid_plan_rate":          metrics.ValidPlanRate,
		"valid_plan_rate_delta":    metrics.PlanComparison.TreatmentMinusBaselineValidPlanRate,
		"paired_plan_outcomes":     metrics.PlanComparison.PairedOutcomes, "no_plan_tasks": metrics.NoPlanTasks,
		"independent_synthetic_oracle": metrics.IndependentOracle,
		"correct_no_plan_rate":         metrics.CorrectNoPlanRate, "clarification_tasks": metrics.ClarificationTasks,
		"correct_clarification_rate":                    metrics.CorrectClarifyRate,
		"clarification_question_provided_rate":          metrics.ClarificationQuestionRate,
		"clarification_question_missing_field_cue_rate": metrics.ClarificationQuestionCueRate,
		"parse_failures":                                metrics.ParseFailures,
		"usage_status":                                  usage.UsageStatus, "input_tokens": usage.InputTokens, "output_tokens": usage.OutputTokens,
		"api_calls_status": usage.APICallsStatus, "hermes_cli_runs": usage.HermesCLIRuns,
		"tasks_sha256": hashes.TasksSHA256,
	})
}

func validateChallengeModelRunPermission(oracleIntents bool, replayParseResults string,
	requireHumanReview, allowUnreviewedDiagnostic bool) error {
	if oracleIntents || strings.TrimSpace(replayParseResults) != "" || requireHumanReview || allowUnreviewedDiagnostic {
		return nil
	}
	return errors.New("model-backed challenge evaluation requires --require-human-review with a recomputable bundle, or explicit --allow-unreviewed-diagnostic-model-run")
}

func validateHumanHoldoutTaskCount(taskCount int, requireHumanReview bool) error {
	if !requireHumanReview {
		return nil
	}
	if taskCount != humanHoldoutTaskCount {
		return fmt.Errorf("--require-human-review requires exactly %d frozen holdout tasks; got %d", humanHoldoutTaskCount, taskCount)
	}
	return nil
}

func validateHumanHoldoutStrata(tasks []agentshopping.ChallengeTask, requireHumanReview bool) error {
	if !requireHumanReview {
		return nil
	}
	counts := make(map[string]int, len(humanHoldoutStrata))
	for _, task := range tasks {
		stratum := strings.ToUpper(strings.TrimSpace(task.Scenario))
		quota, exists := humanHoldoutStrata[stratum]
		if !exists {
			return fmt.Errorf("strict human holdout contains unknown stratum %q; expected opaque IDs S01–S13", task.Scenario)
		}
		if task.ExpectedDisposition != quota.Disposition {
			return fmt.Errorf("strict human holdout stratum %s requires disposition %s, got %s",
				stratum, quota.Disposition, task.ExpectedDisposition)
		}
		counts[stratum]++
	}
	for stratum, quota := range humanHoldoutStrata {
		if counts[stratum] != quota.TaskCount {
			return fmt.Errorf("strict human holdout stratum %s requires %d tasks, got %d",
				stratum, quota.TaskCount, counts[stratum])
		}
	}
	return nil
}

func loadChallengeReplayResults(localRunRoot, parseResultsPath string, expectedHashes agentshopping.ChallengeEvaluationHashes,
	expectedTaskCount int) (*challengeReplaySource, challengeRunManifest, []agentshopping.BatchIntentResult, error) {
	return loadChallengeReplayResultsInternal(localRunRoot, parseResultsPath, expectedHashes, expectedTaskCount, nil, false)
}

func loadChallengeReplayResultsWithRelevanceEnrichment(localRunRoot, parseResultsPath string,
	expectedHashes agentshopping.ChallengeEvaluationHashes, expectedTaskCount int,
	evaluationTasks []agentshopping.ChallengeTask) (*challengeReplaySource, challengeRunManifest,
	[]agentshopping.BatchIntentResult, error) {
	return loadChallengeReplayResultsInternal(localRunRoot, parseResultsPath, expectedHashes, expectedTaskCount,
		evaluationTasks, true)
}

func loadChallengeReplayResultsInternal(localRunRoot, parseResultsPath string, expectedHashes agentshopping.ChallengeEvaluationHashes,
	expectedTaskCount int, evaluationTasks []agentshopping.ChallengeTask, allowRelevanceEnrichment bool) (
	*challengeReplaySource, challengeRunManifest, []agentshopping.BatchIntentResult, error) {
	localRunRootReal, err := filepath.EvalSymlinks(localRunRoot)
	if err != nil {
		return nil, challengeRunManifest{}, nil, fmt.Errorf("resolve local-run root for replay: %w", err)
	}
	evaluationRoot := filepath.Join(localRunRoot, "agent-shopping", "challenge-evaluations")
	if filepath.Base(parseResultsPath) != "parse-results.jsonl" || !pathWithinDirectory(evaluationRoot, parseResultsPath) {
		return nil, challengeRunManifest{}, nil, errors.New("replay parse results must be parse-results.jsonl under a prior local challenge-evaluation run")
	}
	parseResultsReal, err := filepath.EvalSymlinks(parseResultsPath)
	if err != nil {
		return nil, challengeRunManifest{}, nil, fmt.Errorf("resolve replay parse-results path: %w", err)
	}
	evaluationRootReal, err := filepath.EvalSymlinks(evaluationRoot)
	if err != nil || !pathWithinDirectory(evaluationRootReal, parseResultsReal) {
		return nil, challengeRunManifest{}, nil, errors.New("replay parse-results path resolves outside local challenge-evaluation runs")
	}
	runDir := filepath.Dir(parseResultsReal)
	if filepath.Clean(filepath.Dir(runDir)) != filepath.Clean(evaluationRootReal) {
		return nil, challengeRunManifest{}, nil, errors.New("replay parse-results file must be directly inside a challenge-evaluation run directory")
	}
	manifestPath := filepath.Join(runDir, "manifest.json")
	manifestReal, err := filepath.EvalSymlinks(manifestPath)
	if err != nil || !pathWithinDirectory(runDir, manifestReal) {
		return nil, challengeRunManifest{}, nil, errors.New("replay source manifest is missing or resolves outside its run directory")
	}
	manifestBytes, err := os.ReadFile(manifestReal)
	if err != nil {
		return nil, challengeRunManifest{}, nil, fmt.Errorf("read replay source manifest: %w", err)
	}
	var sourceManifest challengeRunManifest
	if err := json.Unmarshal(manifestBytes, &sourceManifest); err != nil {
		return nil, challengeRunManifest{}, nil, fmt.Errorf("decode replay source manifest: %w", err)
	}
	if sourceManifest.SchemaVersion != "stablepay.agent-shopping-challenge-run.v2" &&
		sourceManifest.SchemaVersion != "stablepay.agent-shopping-challenge-run.v3" &&
		sourceManifest.SchemaVersion != "stablepay.agent-shopping-challenge-run.v4" &&
		sourceManifest.SchemaVersion != "stablepay.agent-shopping-challenge-run.v5" {
		return nil, challengeRunManifest{}, nil, errors.New("unsupported replay source manifest schema")
	}
	if err := verifyChallengeRunArtifactHashes(runDir, sourceManifest); err != nil {
		return nil, challengeRunManifest{}, nil, err
	}
	if sourceManifest.SchemaVersion == "stablepay.agent-shopping-challenge-run.v5" &&
		sourceManifest.HumanReviewBundleStatus != "" {
		if err := validateRecordedHumanHoldoutProtocol(sourceManifest); err != nil {
			return nil, challengeRunManifest{}, nil, fmt.Errorf("validate replay source holdout protocol: %w", err)
		}
	}
	if sourceManifest.RunID != filepath.Base(runDir) || sourceManifest.TaskCount != expectedTaskCount {
		return nil, challengeRunManifest{}, nil, errors.New("replay source run identity or task count does not match the selected inputs")
	}
	relevanceLabelsAdded := false
	if sourceManifest.Hashes != expectedHashes {
		if !allowRelevanceEnrichment || sourceManifest.Hashes.CatalogSHA256 != expectedHashes.CatalogSHA256 ||
			sourceManifest.Hashes.InventorySHA256 != expectedHashes.InventorySHA256 ||
			sourceManifest.Hashes.ManifestSHA256 != expectedHashes.ManifestSHA256 {
			return nil, challengeRunManifest{}, nil, errors.New("replay source task/catalog/inventory hashes do not match the selected inputs")
		}
		if sourceManifest.SchemaVersion != "stablepay.agent-shopping-challenge-run.v4" &&
			sourceManifest.SchemaVersion != "stablepay.agent-shopping-challenge-run.v5" {
			return nil, challengeRunManifest{}, nil, errors.New("relevance-label-only replay requires a v4/v5 source with artifact-hash-bound suite.jsonl")
		}
		suitePath := filepath.Join(runDir, "suite.jsonl")
		suiteInfo, statErr := os.Lstat(suitePath)
		if statErr != nil || !suiteInfo.Mode().IsRegular() || suiteInfo.Mode()&os.ModeSymlink != 0 {
			return nil, challengeRunManifest{}, nil, errors.New("relevance-label-only replay source suite is missing or not a regular file")
		}
		sourceTasks, readErr := readChallengeTasks(suitePath)
		if readErr != nil {
			return nil, challengeRunManifest{}, nil, fmt.Errorf("read artifact-verified replay suite: %w", readErr)
		}
		if len(evaluationTasks) != expectedTaskCount || !challengeTasksEqualExceptRelevantSKUs(sourceTasks, evaluationTasks) {
			return nil, challengeRunManifest{}, nil, errors.New("relevance-label-only replay rejected: task suite differs beyond adding relevant_skus labels")
		}
		relevanceLabelsAdded = true
	}
	if strings.TrimSpace(sourceManifest.Provider) == "" || strings.TrimSpace(sourceManifest.Model) == "" {
		return nil, challengeRunManifest{}, nil, errors.New("replay source manifest is missing provider/model provenance")
	}
	parseBytes, err := os.ReadFile(parseResultsReal)
	if err != nil {
		return nil, challengeRunManifest{}, nil, fmt.Errorf("read replay parse results: %w", err)
	}
	results, err := decodeChallengeBatchResults(parseBytes)
	if err != nil {
		return nil, challengeRunManifest{}, nil, err
	}
	if len(results) != expectedTaskCount {
		return nil, challengeRunManifest{}, nil, fmt.Errorf("replay parse result count %d does not match task count %d", len(results), expectedTaskCount)
	}
	parseDigest := sha256.Sum256(parseBytes)
	manifestDigest := sha256.Sum256(manifestBytes)
	relativeParse, err := filepath.Rel(localRunRootReal, parseResultsReal)
	if err != nil || filepath.IsAbs(relativeParse) || !pathWithinDirectory(localRunRootReal, parseResultsReal) {
		return nil, challengeRunManifest{}, nil, errors.New("replay parse-results path is outside local-run")
	}
	relativeManifest, err := filepath.Rel(localRunRootReal, manifestReal)
	if err != nil || filepath.IsAbs(relativeManifest) || !pathWithinDirectory(localRunRootReal, manifestReal) {
		return nil, challengeRunManifest{}, nil, errors.New("replay source manifest path is outside local-run")
	}
	source := &challengeReplaySource{RunID: sourceManifest.RunID,
		ParseResultsPath: filepath.ToSlash(relativeParse), ParseResultsSHA256: "sha256:" + hex.EncodeToString(parseDigest[:]),
		ManifestPath: filepath.ToSlash(relativeManifest), ManifestSHA256: "sha256:" + hex.EncodeToString(manifestDigest[:]),
		Provider: sourceManifest.Provider, Model: sourceManifest.Model,
		PromptTemplateSHA256: sourceManifest.PromptTemplateSHA256, PromptSHA256: sourceManifest.PromptSHA256,
		ResponseSHA256: sourceManifest.ResponseSHA256, UsageStatus: sourceManifest.UsageStatus,
		APICallsStatus: sourceManifest.APICallsStatus, InputTokens: sourceManifest.Usage.InputTokens,
		OutputTokens: sourceManifest.Usage.OutputTokens, HermesCLIRuns: sourceManifest.Usage.HermesCLIRuns,
		SourceTaskSHA256: sourceManifest.Hashes.TasksSHA256, EvaluationTaskSHA256: expectedHashes.TasksSHA256,
		RelevanceLabelsAdded: relevanceLabelsAdded}
	return source, sourceManifest, results, nil
}

func challengeTasksEqualExceptRelevantSKUs(sourceTasks, evaluationTasks []agentshopping.ChallengeTask) bool {
	if len(sourceTasks) == 0 || len(sourceTasks) != len(evaluationTasks) {
		return false
	}
	sourceComparable := append([]agentshopping.ChallengeTask(nil), sourceTasks...)
	evaluationComparable := append([]agentshopping.ChallengeTask(nil), evaluationTasks...)
	for index := range sourceComparable {
		if len(sourceComparable[index].RelevantSKUs) != 0 {
			return false
		}
		if evaluationComparable[index].ExpectedDisposition == agentshopping.ChallengePlan {
			if len(evaluationComparable[index].RelevantSKUs) == 0 {
				return false
			}
		} else if len(evaluationComparable[index].RelevantSKUs) != 0 {
			return false
		}
		sourceComparable[index].RelevantSKUs = nil
		evaluationComparable[index].RelevantSKUs = nil
	}
	return reflect.DeepEqual(sourceComparable, evaluationComparable)
}

var challengeRunArtifactNames = []string{
	"suite.jsonl", "parse-results.jsonl", "trials.jsonl", "metrics.json",
	"model-responses.jsonl", "model-streams.jsonl",
}

var challengeEvaluationSourceNames = []string{
	"commerce-runtime/benchmarks/agentshopping/challenge_evaluation.go",
	"commerce-runtime/benchmarks/agentshopping/challenge_independent_oracle.go",
	"commerce-runtime/benchmarks/agentshopping/challenge_parser.go",
	"commerce-runtime/benchmarks/agentshopping/evaluation.go",
	"commerce-runtime/benchmarks/agentshopping/hermes_batch.go",
	"commerce-runtime/benchmarks/agentshopping/hermes_intent.go",
	"commerce-runtime/benchmarks/agentshopping/retrieval.go",
	"commerce-runtime/benchmarks/agentshopping/substitution.go",
	"commerce-runtime/benchmarks/agentshopping/attributes.go",
	"commerce-runtime/cmd/agentshopping-sim/evaluate_challenge.go",
}

func hashChallengeEvaluationSources(repositoryRoot string) (map[string]string, error) {
	hashes := make(map[string]string, len(challengeEvaluationSourceNames))
	for _, name := range challengeEvaluationSourceNames {
		path := filepath.Join(repositoryRoot, filepath.FromSlash(name))
		info, err := os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("scoring source %s is not a regular file", name)
		}
		digest, err := hashFileSHA256(path)
		if err != nil {
			return nil, fmt.Errorf("hash %s: %w", name, err)
		}
		hashes[name] = digest
	}
	return hashes, nil
}

func hashChallengeRunArtifacts(runDir string) (map[string]string, error) {
	hashes := make(map[string]string, len(challengeRunArtifactNames))
	for _, name := range challengeRunArtifactNames {
		path := filepath.Join(runDir, name)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			if isRequiredChallengeRunArtifact(name) {
				return nil, fmt.Errorf("required challenge run artifact %s is missing", name)
			}
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("challenge run artifact %s is not a regular file", name)
		}
		digest, err := hashFileSHA256(path)
		if err != nil {
			return nil, fmt.Errorf("hash %s: %w", name, err)
		}
		hashes[name] = digest
	}
	return hashes, nil
}

func isRequiredChallengeRunArtifact(name string) bool {
	switch name {
	case "suite.jsonl", "parse-results.jsonl", "trials.jsonl", "metrics.json":
		return true
	default:
		return false
	}
}

func hashFileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func verifyChallengeRunArtifactHashes(runDir string, manifest challengeRunManifest) error {
	if manifest.SchemaVersion != "stablepay.agent-shopping-challenge-run.v4" &&
		manifest.SchemaVersion != "stablepay.agent-shopping-challenge-run.v5" {
		return nil // v2/v3 manifests predate output artifact hashes.
	}
	if manifest.ArtifactsSHA256 == nil {
		return errors.New("v4 replay source manifest is missing artifact SHA-256 values")
	}
	allowed := make(map[string]bool, len(challengeRunArtifactNames))
	for _, name := range challengeRunArtifactNames {
		allowed[name] = true
	}
	actualHashes, err := hashChallengeRunArtifacts(runDir)
	if err != nil {
		return fmt.Errorf("verify v4 replay artifacts: %w", err)
	}
	for name := range actualHashes {
		if _, bound := manifest.ArtifactsSHA256[name]; bound {
			continue
		}
		// model-streams.jsonl was added after some v5 runs were written. It is
		// an auxiliary trace and is not consumed during parse-result replay;
		// keep legacy runs replayable while still requiring hashes for every
		// artifact that can affect the replay result.
		if name != "model-streams.jsonl" {
			return fmt.Errorf("v4 replay source manifest does not bind present artifact %s", name)
		}
	}
	for name, expected := range manifest.ArtifactsSHA256 {
		if !allowed[name] {
			return fmt.Errorf("v4 replay source manifest lists unsupported artifact %q", name)
		}
		const prefix = "sha256:"
		if !strings.HasPrefix(expected, prefix) || len(expected) != len(prefix)+sha256.Size*2 {
			return fmt.Errorf("v4 replay source manifest has invalid SHA-256 for %s", name)
		}
		if _, err := hex.DecodeString(strings.TrimPrefix(expected, prefix)); err != nil {
			return fmt.Errorf("v4 replay source manifest has invalid SHA-256 for %s: %w", name, err)
		}
		actual, present := actualHashes[name]
		if !present {
			return fmt.Errorf("v4 replay source manifest hashes missing artifact %s", name)
		}
		if actual != expected {
			return fmt.Errorf("v4 replay artifact hash mismatch for %s", name)
		}
	}
	return nil
}

func humanHoldoutProtocolIdentity() (humanHoldoutProtocolDefinition, string, error) {
	definition := humanHoldoutProtocolDefinition{Version: humanHoldoutProtocolVersion, TaskCount: humanHoldoutTaskCount,
		Strata: humanHoldoutStrata}
	encoded, err := json.Marshal(definition)
	if err != nil {
		return humanHoldoutProtocolDefinition{}, "", err
	}
	digest := sha256.Sum256(encoded)
	return definition, "sha256:" + hex.EncodeToString(digest[:]), nil
}

func validateRecordedHumanHoldoutProtocol(manifest challengeRunManifest) error {
	if manifest.HoldoutProtocol == nil {
		return errors.New("recomputed human-review run manifest is missing its frozen protocol definition")
	}
	protocol := manifest.HoldoutProtocol
	if protocol.Version == "" || protocol.TaskCount != humanHoldoutTaskCount || manifest.TaskCount != protocol.TaskCount {
		return errors.New("recorded human holdout protocol version or task count is invalid")
	}
	if len(protocol.Strata) == 0 {
		return errors.New("recorded human holdout protocol has no strata")
	}
	total := 0
	expectedCounts := make(map[string]int, len(protocol.Strata))
	dispositionCounts := make(map[agentshopping.ChallengeDisposition]int, 3)
	for name, stratum := range protocol.Strata {
		if strings.TrimSpace(name) == "" || stratum.TaskCount <= 0 || stratum.PromptingFocus == "" {
			return fmt.Errorf("recorded human holdout protocol has an invalid stratum %q", name)
		}
		switch stratum.Disposition {
		case agentshopping.ChallengePlan, agentshopping.ChallengeNoPlan, agentshopping.ChallengeClarify:
		default:
			return fmt.Errorf("recorded human holdout protocol has an unsupported disposition for %s", name)
		}
		total += stratum.TaskCount
		expectedCounts[name] = stratum.TaskCount
		dispositionCounts[stratum.Disposition] += stratum.TaskCount
	}
	if total != protocol.TaskCount || len(manifest.ScenarioCounts) != len(expectedCounts) ||
		dispositionCounts[agentshopping.ChallengePlan] != 350 ||
		dispositionCounts[agentshopping.ChallengeNoPlan] != 75 ||
		dispositionCounts[agentshopping.ChallengeClarify] != 75 {
		return errors.New("recorded protocol quotas do not cover the task count or manifest scenarios")
	}
	for name, count := range expectedCounts {
		if manifest.ScenarioCounts[name] != count {
			return fmt.Errorf("manifest scenario count for %s does not match the recorded protocol", name)
		}
	}
	encoded, err := json.Marshal(protocol)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(encoded)
	expectedHash := "sha256:" + hex.EncodeToString(digest[:])
	if manifest.HoldoutProtocolSHA256 != expectedHash {
		return errors.New("recorded human holdout protocol does not match its SHA-256")
	}
	return nil
}

func decodeChallengeBatchResults(data []byte) ([]agentshopping.BatchIntentResult, error) {
	results := make([]agentshopping.BatchIntentResult, 0)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		decoder := json.NewDecoder(strings.NewReader(line))
		decoder.DisallowUnknownFields()
		var result agentshopping.BatchIntentResult
		if err := decoder.Decode(&result); err != nil {
			return nil, fmt.Errorf("decode replay parse-result line %d: %w", lineNumber, err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return nil, fmt.Errorf("replay parse-result line %d contains trailing JSON data", lineNumber)
		}
		results = append(results, result)
		if len(results) > 500 {
			return nil, errors.New("replay parse-results file exceeds the 500-task maximum")
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read replay parse results: %w", err)
	}
	if len(results) == 0 {
		return nil, errors.New("replay parse-results file is empty")
	}
	return results, nil
}

func replaySourceRunID(source *challengeReplaySource) string {
	if source == nil {
		return ""
	}
	return source.RunID
}

func challengeParserRequest(task agentshopping.ChallengeTask) agentshopping.BatchIntentRequest {
	defaultRegion := task.BuyerDefaultRegion
	if defaultRegion == "" {
		defaultRegion = task.Intent.Region // compatibility for generated, provenance-unverified drafts
	}
	buyerBudget := task.BuyerBudgetMinor
	if buyerBudget <= 0 {
		buyerBudget = task.Intent.MaxPriceMinor // compatibility for generated, provenance-unverified drafts
	}
	return agentshopping.BatchIntentRequest{TaskID: task.TaskID, Prompt: task.Prompt,
		DefaultRegion: defaultRegion, BuyerBudgetMinor: buyerBudget}
}

func readChallengeTasks(path string) ([]agentshopping.ChallengeTask, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open challenge task file: %w", err)
	}
	defer file.Close()
	var tasks []agentshopping.ChallengeTask
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		decoder := json.NewDecoder(strings.NewReader(line))
		decoder.DisallowUnknownFields()
		var task agentshopping.ChallengeTask
		if err := decoder.Decode(&task); err != nil {
			return nil, fmt.Errorf("decode challenge task line %d: %w", lineNumber, err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return nil, fmt.Errorf("challenge task line %d contains trailing JSON data", lineNumber)
		}
		tasks = append(tasks, task)
		if len(tasks) > 500 {
			return nil, errors.New("challenge task file exceeds the 500-task maximum")
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read challenge task file: %w", err)
	}
	if len(tasks) == 0 {
		return nil, errors.New("challenge task file is empty")
	}
	return tasks, nil
}

func challengeOracleIntentResults(tasks []agentshopping.ChallengeTask) []agentshopping.BatchIntentResult {
	results := make([]agentshopping.BatchIntentResult, len(tasks))
	for index, task := range tasks {
		// The HTTP API fills a safe, field-targeted question after intent
		// parsing. Apply that same deterministic boundary in oracle mode so
		// clarification metrics do not grade an unnormalized gold fixture.
		intent := agentshopping.NormalizeIntentClarification(task.Intent)
		intent = intent.WithSearchText(task.Prompt)
		results[index] = agentshopping.BatchIntentResult{TaskID: task.TaskID, Intent: intent}
	}
	return results
}

func challengeMetricBoundary(oracleIntents bool, provenanceStatus string) string {
	common := "B0 (exact title/tag + budget + price) and B1 (runtime-matched bounded discovery window, then live inventory/price/hard-attribute filtering and preference ranking) are evaluated on the same task set; valid-plan delta is paired by task. Planning uses the first-stage candidate window before filtering; Recall@K scores only its top-K prefix, and the response is limited to K feasible offers. Snapshot-feasible SKU Recall@K and target-product Recall@K are separate from task-hit and valid-plan rate. Target-product Recall@K is unavailable unless explicit relevant_skus labels exist; generated legacy drafts do not inherit this metric from feasible gold labels. Reviewer identity and blind/freeze declarations remain self-attested; a basic provenance declaration alone does not recompute its source packet. This is not production traffic, real payment, fulfillment, or Devnet evidence. Task provenance status: " + provenanceStatus + "."
	if oracleIntents {
		return "ORACLE INTENT DIAGNOSTIC ONLY: each result uses the task's gold intent, makes no model/API calls, and isolates ranking/feasibility as an upper-bound diagnostic; it does not evaluate natural-language parsing or Agent decisions. " + common
	}
	return "GPT-6 Luna parses the supplied challenge requests in batches of at most 50. " + common
}
