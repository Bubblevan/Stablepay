package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/benchmarks/agentshopping"
)

type challengeDraftManifest struct {
	SchemaVersion    string                                  `json:"schema_version"`
	Status           string                                  `json:"status"`
	TaskProvenance   string                                  `json:"task_provenance"`
	ReviewGate       string                                  `json:"review_gate"`
	CreatedAtUTC     string                                  `json:"created_at_utc"`
	Seed             int64                                   `json:"seed"`
	TaskSeed         int64                                   `json:"task_seed"`
	TaskCount        int                                     `json:"task_count"`
	PromptStyle      string                                  `json:"prompt_style"`
	ScenarioCounts   map[string]int                          `json:"scenario_counts"`
	DecisionCounts   map[string]int                          `json:"decision_counts"`
	GoldSetSizeMin   int                                     `json:"gold_set_size_min"`
	GoldSetSizeMax   int                                     `json:"gold_set_size_max"`
	GoldSetSizeMean  float64                                 `json:"gold_set_size_mean"`
	SourceManifest   string                                  `json:"source_manifest"`
	Hashes           agentshopping.ChallengeEvaluationHashes `json:"hashes"`
	PublicDatasetUse string                                  `json:"public_dataset_use"`
	MetricBoundary   string                                  `json:"metric_boundary"`
}

func generateChallengeDraft(args []string) error {
	flags := flag.NewFlagSet("generate-challenge-draft", flag.ContinueOnError)
	manifestPath := flags.String("manifest", filepath.Join(".local-run", "simulated-merchants", "manifest.json"), "local synthetic Merchant manifest")
	seed := flags.Int64("seed", 20261010, "expected synthetic Merchant catalog seed")
	taskSeed := flags.Int64("task-seed", 20261010, "independent challenge task generation seed")
	count := flags.Int("tasks", 500, "draft task count (1–500)")
	mixedDecisions := flags.Bool("mixed-decisions", false, "include generated PLAN, NO_PLAN, and CLARIFY tasks (diagnostic only)")
	naturalLanguageV2 := flags.Bool("natural-language-v2", false, "use varied user-style prompts while preserving structured intents and labels")
	outputDir := flags.String("out-dir", filepath.Join(".local-run", "agent-shopping", "challenge-suites", "draft-auto-v1"), "new output directory under .local-run/agent-shopping/challenge-suites")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *count <= 0 || *count > 500 {
		return errors.New("generate-challenge-draft requires 1–500 tasks and no positional arguments")
	}
	manifestAbs, err := filepath.Abs(*manifestPath)
	if err != nil {
		return err
	}
	manifestDir := filepath.Dir(manifestAbs)
	if filepath.Base(manifestDir) != "simulated-merchants" || filepath.Base(filepath.Dir(manifestDir)) != ".local-run" {
		return errors.New("source manifest must be under .local-run/simulated-merchants")
	}
	localRunRoot := filepath.Dir(manifestDir)
	challengeRoot := filepath.Join(localRunRoot, "agent-shopping", "challenge-suites")
	outputAbs, err := filepath.Abs(*outputDir)
	if err != nil {
		return err
	}
	if filepath.Clean(filepath.Dir(outputAbs)) != filepath.Clean(challengeRoot) || filepath.Clean(outputAbs) == filepath.Clean(challengeRoot) {
		return errors.New("draft output must be a new direct child directory under .local-run/agent-shopping/challenge-suites")
	}
	localRunReal, err := filepath.EvalSymlinks(localRunRoot)
	if err != nil {
		return fmt.Errorf("resolve local-run root: %w", err)
	}
	manifestReal, err := filepath.EvalSymlinks(manifestAbs)
	if err != nil || !pathWithinDirectory(localRunReal, manifestReal) {
		return errors.New("source manifest resolves outside the local .local-run root")
	}
	manifestBytes, err := os.ReadFile(manifestAbs)
	if err != nil {
		return fmt.Errorf("read source Merchant manifest: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	catalog, err := agentshopping.LoadMerchantCatalog(ctx, manifestAbs, nil)
	if err != nil {
		return err
	}
	if catalog.Seed != *seed {
		return fmt.Errorf("source seed %d differs from requested seed %d", catalog.Seed, *seed)
	}
	var tasks []agentshopping.ChallengeTask
	if *mixedDecisions {
		tasks, err = agentshopping.GenerateChallengeDecisionDraft(catalog, *taskSeed, *count)
	} else {
		tasks, err = agentshopping.GenerateChallengePlanDraft(catalog, *taskSeed, *count)
	}
	if err != nil {
		return err
	}
	promptStyle := "catalog-seeded-template-v1"
	if *naturalLanguageV2 {
		if err := agentshopping.NaturalizeChallengeTaskPrompts(tasks, *taskSeed); err != nil {
			return fmt.Errorf("naturalize challenge prompts: %w", err)
		}
		promptStyle = "varied-user-language-v2; intents-and-labels-unchanged"
	}
	hashes, err := agentshopping.ComputeChallengeHashes(catalog, tasks, manifestBytes)
	if err != nil {
		return err
	}
	scenarioCounts := make(map[string]int)
	decisionCounts := make(map[string]int)
	minGold, maxGold, goldTotal := int(^uint(0)>>1), 0, 0
	for _, task := range tasks {
		scenarioCounts[task.Scenario]++
		decisionCounts[string(task.ExpectedDisposition)]++
		size := len(task.GoldSKUs)
		if size < minGold {
			minGold = size
		}
		if size > maxGold {
			maxGold = size
		}
		goldTotal += size
	}
	manifest := challengeDraftManifest{SchemaVersion: "stablepay.agent-shopping-challenge-draft.v1",
		Status: "AUTO_GENERATED_UNREVIEWED_DRAFT", TaskProvenance: "GENERATED_FROM_LOCAL_SYNTHETIC_MERCHANT_SNAPSHOT",
		ReviewGate:   "INDEPENDENT_PROMPT_AND_GOLD_LABEL_REVIEW_REQUIRED_BEFORE_HOLDOUT_OR_RESUME_CLAIM",
		CreatedAtUTC: time.Now().UTC().Format(time.RFC3339Nano), Seed: *seed, TaskSeed: *taskSeed,
		TaskCount: len(tasks), PromptStyle: promptStyle,
		ScenarioCounts: scenarioCounts, DecisionCounts: decisionCounts, GoldSetSizeMin: minGold, GoldSetSizeMax: maxGold,
		GoldSetSizeMean: float64(goldTotal) / float64(len(tasks)),
		SourceManifest:  filepath.ToSlash(filepath.Join(".local-run", "simulated-merchants", filepath.Base(manifestAbs))),
		Hashes:          hashes, PublicDatasetUse: "NONE; public benchmarks informed rubric only; no public prompts/products/labels imported",
		MetricBoundary: "Synthetic diagnostic draft only. Not an independent human holdout, production commerce data, live inventory, payment, fulfillment, or Devnet evidence."}
	if *mixedDecisions {
		manifest.MetricBoundary += " Disposition mix is algorithmically generated from the same synthetic snapshot and is not an independently authored decision benchmark."
	}
	if err := os.MkdirAll(challengeRoot, 0o700); err != nil {
		return fmt.Errorf("create local challenge-suite directory: %w", err)
	}
	challengeRootReal, err := filepath.EvalSymlinks(challengeRoot)
	if err != nil || !pathWithinDirectory(localRunReal, challengeRootReal) {
		return errors.New("challenge-suite directory resolves outside the local .local-run root")
	}
	staging, err := os.MkdirTemp(challengeRoot, ".draft-staging-")
	if err != nil {
		return fmt.Errorf("create local draft staging directory: %w", err)
	}
	defer os.RemoveAll(staging)
	if err := writeJSONLines(filepath.Join(staging, "tasks.jsonl"), tasks); err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(staging, "manifest.json"), manifest); err != nil {
		return err
	}
	if _, err := os.Stat(outputAbs); err == nil {
		return fmt.Errorf("draft output directory already exists; refusing to overwrite: %s", outputAbs)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(staging, outputAbs); err != nil {
		return fmt.Errorf("publish local challenge draft: %w", err)
	}
	digest := sha256.Sum256(manifestBytes)
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"output_dir": outputAbs, "tasks_file": filepath.Join(outputAbs, "tasks.jsonl"), "task_count": len(tasks),
		"task_seed": *taskSeed, "prompt_style": promptStyle,
		"status": manifest.Status, "review_gate": manifest.ReviewGate, "decision_counts": decisionCounts,
		"tasks_sha256": hashes.TasksSHA256, "catalog_sha256": hashes.CatalogSHA256,
		"inventory_sha256": hashes.InventorySHA256, "source_manifest_sha256": "sha256:" + hex.EncodeToString(digest[:]),
		"gold_set_size_min": minGold, "gold_set_size_max": maxGold, "gold_set_size_mean": manifest.GoldSetSizeMean,
	})
}

func pathWithinDirectory(root, target string) bool {
	rootAbs, rootErr := filepath.Abs(root)
	targetAbs, targetErr := filepath.Abs(target)
	if rootErr != nil || targetErr != nil {
		return false
	}
	relative, err := filepath.Rel(filepath.Clean(rootAbs), filepath.Clean(targetAbs))
	if err != nil || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return false
	}
	return true
}
