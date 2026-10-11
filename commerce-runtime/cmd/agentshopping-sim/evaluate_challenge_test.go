package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stablepay/commerce-runtime/benchmarks/agentshopping"
)

func TestHashChallengeEvaluationSourcesResolvesFromRepositoryRoot(t *testing.T) {
	_, sourcePath, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller did not return this test source path")
	}
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(sourcePath), "..", "..", ".."))
	hashes, err := hashChallengeEvaluationSources(repositoryRoot)
	if err != nil {
		t.Fatalf("hash repository evaluation sources before model invocation: %v", err)
	}
	if len(hashes) != len(challengeEvaluationSourceNames) {
		t.Fatalf("expected all evaluation source files to be hash-bound: got %d, want %d", len(hashes), len(challengeEvaluationSourceNames))
	}
	for _, name := range challengeEvaluationSourceNames {
		if !strings.HasPrefix(hashes[name], "sha256:") || len(hashes[name]) != len("sha256:")+64 {
			t.Errorf("missing/invalid scoring source digest for %s: %q", name, hashes[name])
		}
	}
}

func TestChallengeParserRequestUsesDeclaredContextRatherThanGoldIntent(t *testing.T) {
	task := agentshopping.ChallengeTask{TaskID: "context-separation", Prompt: "Buy beans, max 3 USDC, ship to east.",
		Intent:             agentshopping.Intent{Region: "CN-EAST", MaxPriceMinor: 300},
		BuyerDefaultRegion: "CN-WEST", BuyerBudgetMinor: 900}
	request := challengeParserRequest(task)
	if request.DefaultRegion != "CN-WEST" || request.BuyerBudgetMinor != 900 {
		t.Fatalf("parser context leaked gold intent instead of independent buyer context: %+v", request)
	}
}

func TestChallengeOracleIntentResultsUseRuntimeClarificationNormalization(t *testing.T) {
	tests := []struct {
		name     string
		intent   agentshopping.Intent
		question string
	}{
		{
			name: "missing item",
			intent: agentshopping.Intent{Category: "coffee", RequiredFormat: "bag", PreferredStyle: "floral",
				Region: "CN-EAST", MaxPriceMinor: 500, ConfirmationNeeded: true, ClarificationRequired: true},
			question: "您想购买哪种商品？",
		},
		{
			name: "missing format",
			intent: agentshopping.Intent{Category: "coffee", Item: "beans", PreferredStyle: "floral",
				Region: "CN-EAST", MaxPriceMinor: 500, ConfirmationNeeded: true, ClarificationRequired: true},
			question: "需要哪种规格或形式？",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			results := challengeOracleIntentResults([]agentshopping.ChallengeTask{{TaskID: "clarify", Intent: test.intent}})
			if len(results) != 1 || results[0].TaskID != "clarify" ||
				results[0].Intent.ClarificationQuestion != test.question || !results[0].Intent.ClarificationRequired {
				t.Fatalf("oracle fixture must use runtime clarification normalization: %+v", results)
			}
		})
	}
}

func TestChallengeOracleIntentResultsDoNotClarifyForMissingOptionalStyle(t *testing.T) {
	intent := agentshopping.Intent{Category: "coffee", Item: "beans", RequiredFormat: "bag",
		Region: "CN-EAST", MaxPriceMinor: 500, ConfirmationNeeded: true}
	results := challengeOracleIntentResults([]agentshopping.ChallengeTask{{TaskID: "optional-style", Intent: intent}})
	if len(results) != 1 || results[0].Intent.ClarificationRequired || results[0].Intent.ClarificationQuestion != "" {
		t.Fatalf("a complete intent with no soft style preference must remain actionable: %+v", results)
	}
}

func TestLoadChallengeReplayResultsBindsSnapshotAndHashesSourceFiles(t *testing.T) {
	localRunRoot := t.TempDir()
	hashes := agentshopping.ChallengeEvaluationHashes{CatalogSHA256: "sha256:catalog",
		InventorySHA256: "sha256:inventory", TasksSHA256: "sha256:tasks", ManifestSHA256: "sha256:manifest"}
	runID := "source-run-1"
	runDir := filepath.Join(localRunRoot, "agent-shopping", "challenge-evaluations", runID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	parsePath := filepath.Join(runDir, "parse-results.jsonl")
	parseResult := agentshopping.BatchIntentResult{TaskID: "task-1", AllowSubstitutionSpecified: true,
		RawIntent: &agentshopping.Intent{Category: "coffee", Item: "beans"}}
	parseBytes, err := json.Marshal(parseResult)
	if err != nil {
		t.Fatal(err)
	}
	parseBytes = append(parseBytes, '\n')
	if err := os.WriteFile(parsePath, parseBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	sourceManifest := challengeRunManifest{SchemaVersion: "stablepay.agent-shopping-challenge-run.v3",
		RunID: runID, TaskCount: 1, Hashes: hashes, Provider: "openai-codex", Model: "gpt-6-luna",
		PromptTemplateSHA256: "sha256:template", PromptSHA256: "sha256:prompt", ResponseSHA256: "sha256:response",
		UsageStatus: "AVAILABLE", APICallsStatus: "TOKEN_METRIC_UNAVAILABLE",
		Usage: agentshopping.ParseUsage{InputTokens: 10, OutputTokens: 5, HermesCLIRuns: 1}}
	manifestBytes, err := json.Marshal(sourceManifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "manifest.json"), manifestBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	source, loadedManifest, results, err := loadChallengeReplayResults(localRunRoot, parsePath, hashes, 1)
	if err != nil {
		t.Fatalf("load matching source run: %v", err)
	}
	if source.RunID != runID || source.Provider != sourceManifest.Provider || source.Model != sourceManifest.Model ||
		source.InputTokens != 10 || loadedManifest.RunID != runID || len(results) != 1 || results[0].TaskID != "task-1" {
		t.Fatalf("replay source provenance was not retained: source=%+v manifest=%+v results=%+v", source, loadedManifest, results)
	}
	if len(source.ParseResultsSHA256) != len("sha256:")+64 || len(source.ManifestSHA256) != len("sha256:")+64 {
		t.Fatalf("replay source file hashes are missing: %+v", source)
	}

	mismatched := hashes
	mismatched.TasksSHA256 = "sha256:other-tasks"
	if _, _, _, err := loadChallengeReplayResults(localRunRoot, parsePath, mismatched, 1); err == nil {
		t.Fatal("replay accepted a source run with a different task/catalog snapshot")
	}
}

func TestLoadChallengeReplayResultsAllowsOnlyArtifactBoundRelevanceLabelEnrichment(t *testing.T) {
	localRunRoot := t.TempDir()
	runID := "source-label-enrichment"
	runDir := filepath.Join(localRunRoot, "agent-shopping", "challenge-evaluations", runID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	sourceTask := agentshopping.ChallengeTask{TaskID: "task-1", Scenario: "PLAN", Prompt: "buy the exact coffee",
		ExpectedDisposition: agentshopping.ChallengePlan, Intent: agentshopping.Intent{ConfirmationNeeded: true},
		GoldSKUs: []string{"sku-exact"}}
	evaluationTask := sourceTask
	evaluationTask.RelevantSKUs = []string{"sku-exact"}
	if err := writeJSONLines(filepath.Join(runDir, "suite.jsonl"), []agentshopping.ChallengeTask{sourceTask}); err != nil {
		t.Fatal(err)
	}
	parseResult := agentshopping.BatchIntentResult{TaskID: "task-1", Intent: agentshopping.Intent{Category: "coffee", Item: "beans"}}
	if err := writeJSONLines(filepath.Join(runDir, "parse-results.jsonl"), []agentshopping.BatchIntentResult{parseResult}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "trials.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "metrics.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceHashes := agentshopping.ChallengeEvaluationHashes{CatalogSHA256: "sha256:catalog",
		InventorySHA256: "sha256:inventory", TasksSHA256: "sha256:source-tasks", ManifestSHA256: "sha256:manifest"}
	evaluationHashes := sourceHashes
	evaluationHashes.TasksSHA256 = "sha256:enriched-tasks"
	artifacts, err := hashChallengeRunArtifacts(runDir)
	if err != nil {
		t.Fatal(err)
	}
	sourceManifest := challengeRunManifest{SchemaVersion: "stablepay.agent-shopping-challenge-run.v5", RunID: runID,
		TaskCount: 1, Hashes: sourceHashes, ArtifactsSHA256: artifacts, Provider: "openai-codex", Model: "gpt-6-luna"}
	manifestBytes, err := json.Marshal(sourceManifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "manifest.json"), manifestBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	parsePath := filepath.Join(runDir, "parse-results.jsonl")
	source, _, results, err := loadChallengeReplayResultsWithRelevanceEnrichment(localRunRoot, parsePath,
		evaluationHashes, 1, []agentshopping.ChallengeTask{evaluationTask})
	if err != nil {
		t.Fatalf("replay should permit only additive relevant_skus labels: %v", err)
	}
	if !source.RelevanceLabelsAdded || source.SourceTaskSHA256 != sourceHashes.TasksSHA256 ||
		source.EvaluationTaskSHA256 != evaluationHashes.TasksSHA256 || len(results) != 1 {
		t.Fatalf("relevance enrichment lineage was not recorded: source=%+v results=%+v", source, results)
	}
	if _, _, _, err := loadChallengeReplayResults(localRunRoot, parsePath, evaluationHashes, 1); err == nil {
		t.Fatal("default strict replay unexpectedly accepted a changed task hash")
	}
	changedPrompt := evaluationTask
	changedPrompt.Prompt = "a different prompt"
	if _, _, _, err := loadChallengeReplayResultsWithRelevanceEnrichment(localRunRoot, parsePath,
		evaluationHashes, 1, []agentshopping.ChallengeTask{changedPrompt}); err == nil {
		t.Fatal("relevance-label-only replay accepted a prompt change")
	}
	changedGold := evaluationTask
	changedGold.GoldSKUs = []string{"sku-other"}
	if _, _, _, err := loadChallengeReplayResultsWithRelevanceEnrichment(localRunRoot, parsePath,
		evaluationHashes, 1, []agentshopping.ChallengeTask{changedGold}); err == nil {
		t.Fatal("relevance-label-only replay accepted a gold-label change")
	}
}

func TestLoadChallengeReplayResultsVerifiesV4ArtifactHashes(t *testing.T) {
	localRunRoot := t.TempDir()
	hashes := agentshopping.ChallengeEvaluationHashes{CatalogSHA256: "sha256:catalog",
		InventorySHA256: "sha256:inventory", TasksSHA256: "sha256:tasks", ManifestSHA256: "sha256:manifest"}
	runID := "source-run-v4"
	runDir := filepath.Join(localRunRoot, "agent-shopping", "challenge-evaluations", runID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	parseResult := agentshopping.BatchIntentResult{TaskID: "task-1", Intent: agentshopping.Intent{Category: "coffee", Item: "beans"}}
	parseBytes, err := json.Marshal(parseResult)
	if err != nil {
		t.Fatal(err)
	}
	parseBytes = append(parseBytes, '\n')
	artifacts := map[string][]byte{
		"suite.jsonl": []byte("{}\n"), "parse-results.jsonl": parseBytes,
		"trials.jsonl": []byte("{}\n"), "metrics.json": []byte("{}\n"),
	}
	for name, data := range artifacts {
		if err := os.WriteFile(filepath.Join(runDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	artifactHashes, err := hashChallengeRunArtifacts(runDir)
	if err != nil {
		t.Fatalf("hash run artifacts: %v", err)
	}
	manifest := challengeRunManifest{SchemaVersion: "stablepay.agent-shopping-challenge-run.v4",
		RunID: runID, TaskCount: 1, Hashes: hashes, ArtifactsSHA256: artifactHashes,
		Provider: "openai-codex", Model: "gpt-6-luna", PromptTemplateSHA256: "sha256:template",
		PromptSHA256: "sha256:prompt", ResponseSHA256: "sha256:response"}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(runDir, "manifest.json")
	if err := os.WriteFile(manifestPath, manifestBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	parsePath := filepath.Join(runDir, "parse-results.jsonl")
	if _, _, results, err := loadChallengeReplayResults(localRunRoot, parsePath, hashes, 1); err != nil || len(results) != 1 {
		t.Fatalf("valid v4 artifact bundle should replay: results=%+v err=%v", results, err)
	}
	manifest.SchemaVersion = "stablepay.agent-shopping-challenge-run.v5"
	manifestBytes, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, manifestBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, results, err := loadChallengeReplayResults(localRunRoot, parsePath, hashes, 1); err != nil || len(results) != 1 {
		t.Fatalf("valid v5 diagnostic artifact bundle should replay: results=%+v err=%v", results, err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "model-streams.jsonl"), []byte("auxiliary trace\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, results, err := loadChallengeReplayResults(localRunRoot, parsePath, hashes, 1); err != nil || len(results) != 1 {
		t.Fatalf("legacy v5 replay should tolerate an unbound auxiliary model stream: results=%+v err=%v", results, err)
	}
	if err := os.Remove(filepath.Join(runDir, "model-streams.jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "model-responses.jsonl"), []byte("response\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := loadChallengeReplayResults(localRunRoot, parsePath, hashes, 1); err == nil {
		t.Fatal("replay accepted a present raw response artifact not bound by the v4 manifest")
	}
	if err := os.Remove(filepath.Join(runDir, "model-responses.jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parsePath, append(parseBytes, ' '), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := loadChallengeReplayResults(localRunRoot, parsePath, hashes, 1); err == nil {
		t.Fatal("replay accepted parse results changed after the v4 manifest was written")
	}
}

func TestChallengeParserRequestLegacyDraftFallback(t *testing.T) {
	task := agentshopping.ChallengeTask{TaskID: "legacy-draft", Prompt: "generated fixture request",
		Intent: agentshopping.Intent{Region: "CN-EAST", MaxPriceMinor: 500}}
	request := challengeParserRequest(task)
	if request.DefaultRegion != "CN-EAST" || request.BuyerBudgetMinor != 500 {
		t.Fatalf("legacy draft compatibility context changed: %+v", request)
	}
}

func TestChallengeModelRunRequiresReviewBundleOrExplicitDiagnosticOptIn(t *testing.T) {
	cases := []struct {
		name               string
		oracleIntents      bool
		replayParseResults string
		requireHumanReview bool
		allowUnreviewed    bool
		wantError          bool
	}{
		{name: "unreviewed model call is blocked by default", wantError: true},
		{name: "explicit unreviewed diagnostic opt-in", allowUnreviewed: true},
		{name: "recomputable human review path", requireHumanReview: true},
		{name: "oracle evaluation needs no model opt-in", oracleIntents: true},
		{name: "offline replay needs no model opt-in", replayParseResults: "prior/parse-results.jsonl"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := validateChallengeModelRunPermission(testCase.oracleIntents, testCase.replayParseResults,
				testCase.requireHumanReview, testCase.allowUnreviewed)
			if (err != nil) != testCase.wantError {
				t.Fatalf("permission validation error=%v, wantError=%t", err, testCase.wantError)
			}
		})
	}
	encodedManifest, err := json.Marshal(challengeRunManifest{UnreviewedDiagnosticModelRun: true})
	if err != nil {
		t.Fatalf("marshal diagnostic opt-in manifest: %v", err)
	}
	var manifestRoundTrip challengeRunManifest
	if err := json.Unmarshal(encodedManifest, &manifestRoundTrip); err != nil {
		t.Fatalf("unmarshal diagnostic opt-in manifest: %v", err)
	}
	if !manifestRoundTrip.UnreviewedDiagnosticModelRun {
		t.Fatalf("manifest did not preserve unreviewed diagnostic model-run status: %s", encodedManifest)
	}
}

func TestHumanReviewGateRequiresExactly500Tasks(t *testing.T) {
	for _, taskCount := range []int{0, 1, 499, 501} {
		if err := validateHumanHoldoutTaskCount(taskCount, true); err == nil {
			t.Errorf("strict human-review gate accepted %d tasks; want exactly 500", taskCount)
		}
	}
	if err := validateHumanHoldoutTaskCount(500, true); err != nil {
		t.Fatalf("strict human-review gate rejected 500 tasks: %v", err)
	}
	if err := validateHumanHoldoutTaskCount(1, false); err != nil {
		t.Fatalf("non-strict diagnostic task count should remain available: %v", err)
	}
}

func TestHumanReviewGateRequiresFrozenStrataAndDecisionLabels(t *testing.T) {
	var tasks []agentshopping.ChallengeTask
	nextID := 0
	for stratum, quota := range humanHoldoutStrata {
		for i := 0; i < quota.TaskCount; i++ {
			nextID++
			tasks = append(tasks, agentshopping.ChallengeTask{TaskID: fmt.Sprintf("task-%03d", nextID),
				Scenario: stratum, ExpectedDisposition: quota.Disposition})
		}
	}
	if len(tasks) != 500 {
		t.Fatalf("test quota profile should contain 500 tasks, got %d", len(tasks))
	}
	if err := validateHumanHoldoutStrata(tasks, true); err != nil {
		t.Fatalf("frozen human holdout profile rejected: %v", err)
	}

	wrongDisposition := append([]agentshopping.ChallengeTask(nil), tasks...)
	planIndex := -1
	for index, task := range wrongDisposition {
		if task.Scenario == "S01" {
			planIndex = index
			break
		}
	}
	if planIndex < 0 {
		t.Fatal("test quota profile is missing S01")
	}
	wrongDisposition[planIndex].ExpectedDisposition = agentshopping.ChallengeNoPlan
	if err := validateHumanHoldoutStrata(wrongDisposition, true); err == nil {
		t.Fatal("strict holdout accepted a decision label inconsistent with its predeclared stratum")
	}

	wrongQuota := append([]agentshopping.ChallengeTask(nil), tasks...)
	wrongQuota[planIndex].Scenario = "S02"
	if err := validateHumanHoldoutStrata(wrongQuota, true); err == nil {
		t.Fatal("strict holdout accepted a post-hoc stratum-count change")
	}

	unknownStratum := append([]agentshopping.ChallengeTask(nil), tasks...)
	unknownStratum[planIndex].Scenario = "EASY_PLAN"
	if err := validateHumanHoldoutStrata(unknownStratum, true); err == nil {
		t.Fatal("strict holdout accepted an unregistered, answer-revealing stratum")
	}
	if err := validateHumanHoldoutStrata(tasks[:1], false); err != nil {
		t.Fatalf("non-strict diagnostic should not require the official stratum mix: %v", err)
	}
}

func TestRecordedHumanHoldoutProtocolIsBoundToManifest(t *testing.T) {
	protocol, digest, err := humanHoldoutProtocolIdentity()
	if err != nil {
		t.Fatalf("compute frozen protocol identity: %v", err)
	}
	counts := make(map[string]int, len(protocol.Strata))
	for name, stratum := range protocol.Strata {
		counts[name] = stratum.TaskCount
	}
	manifest := challengeRunManifest{SchemaVersion: "stablepay.agent-shopping-challenge-run.v5",
		HumanReviewBundleStatus: "RECOMPUTED_MATCH_IDENTITIES_AND_PROCESS_SELF_ATTESTED",
		TaskCount:               protocol.TaskCount, ScenarioCounts: counts, HoldoutProtocol: &protocol,
		HoldoutProtocolSHA256: digest}
	if err := validateRecordedHumanHoldoutProtocol(manifest); err != nil {
		t.Fatalf("valid frozen protocol should verify: %v", err)
	}

	tamperedHash := manifest
	tamperedHash.HoldoutProtocolSHA256 = "sha256:" + strings.Repeat("0", 64)
	if err := validateRecordedHumanHoldoutProtocol(tamperedHash); err == nil {
		t.Fatal("tampered protocol hash was accepted")
	}

	tamperedCounts := manifest
	tamperedCounts.ScenarioCounts = make(map[string]int, len(counts))
	for name, count := range counts {
		tamperedCounts.ScenarioCounts[name] = count
	}
	tamperedCounts.ScenarioCounts["S01"]--
	if err := validateRecordedHumanHoldoutProtocol(tamperedCounts); err == nil {
		t.Fatal("manifest scenario counts inconsistent with protocol were accepted")
	}

	missingDefinition := manifest
	missingDefinition.HoldoutProtocol = nil
	if err := validateRecordedHumanHoldoutProtocol(missingDefinition); err == nil {
		t.Fatal("strict human-review manifest without protocol definition was accepted")
	}
}
