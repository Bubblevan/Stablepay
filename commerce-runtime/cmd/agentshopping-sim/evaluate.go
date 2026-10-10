package main

import (
	"bufio"
	"context"
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

type evaluationRunManifest struct {
	SchemaVersion       string                         `json:"schema_version"`
	RunID               string                         `json:"run_id"`
	CreatedAtUTC        string                         `json:"created_at_utc"`
	Evaluation          string                         `json:"evaluation"`
	Provider            string                         `json:"provider"`
	Model               string                         `json:"model"`
	Seed                int64                          `json:"seed"`
	TaskCount           int                            `json:"task_count"`
	ProductCount        int                            `json:"product_count"`
	InventoryRows       int                            `json:"inventory_rows"`
	SourceManifest      string                         `json:"source_manifest"`
	Hashes              agentshopping.EvaluationHashes `json:"hashes"`
	ScenarioCounts      map[string]int                 `json:"scenario_counts"`
	RealPaymentCalled   bool                           `json:"real_payment_called"`
	SimulationDBWritten bool                           `json:"simulation_database_written"`
	LLMCalled           bool                           `json:"llm_called"`
	MetricBoundary      string                         `json:"metric_boundary"`
}

func evaluate(args []string) error {
	flags := flag.NewFlagSet("evaluate", flag.ContinueOnError)
	manifestPath := flags.String("manifest", filepath.Join(".local-run", "simulated-merchants", "manifest.json"), "local synthetic Merchant manifest")
	outputRootFlag := flags.String("output-root", "", "must remain under .local-run/agent-shopping/evaluations")
	seed := flags.Int64("seed", 20261010, "locked deterministic fixture seed")
	taskCount := flags.Int("tasks", 500, "locked suite task count; only 500 is accepted")
	cutoff := flags.Int("cutoff", agentshopping.DefaultRecallCutoff, "candidate recall cutoff")
	bootstrapSamples := flags.Int("bootstrap-samples", 10000, "paired bootstrap resamples")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *taskCount != 500 || *cutoff <= 0 || *cutoff > 100 || *bootstrapSamples < 1000 || *bootstrapSamples > 100000 {
		return errors.New("evaluation requires exactly 500 tasks, cutoff in [1,100], 1,000–100,000 bootstrap samples, and no positional args")
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
	outputRoot := expectedOutputRoot
	if strings.TrimSpace(*outputRootFlag) != "" {
		outputRoot, err = filepath.Abs(*outputRootFlag)
		if err != nil {
			return err
		}
	}
	if !samePath(outputRoot, expectedOutputRoot) {
		return fmt.Errorf("evaluation output is restricted to %s", expectedOutputRoot)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
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
	tasks, err := agentshopping.GenerateTasks(catalog.Products, overlay, *seed, *taskCount)
	if err != nil {
		return err
	}
	if err := agentshopping.ValidateLockedSuite(catalog, tasks); err != nil {
		return err
	}
	hashes, err := agentshopping.ComputeEvaluationHashes(catalog, tasks, manifestBytes)
	if err != nil {
		return err
	}
	report, trials, err := agentshopping.EvaluateStructuredTasks(catalog, tasks, *cutoff, *bootstrapSamples, *seed)
	if err != nil {
		return err
	}
	report.Hashes = hashes

	createdAt := time.Now().UTC()
	runID := "shopping-" + createdAt.Format("20060102T150405.000000000Z")
	scenarioCounts := make(map[string]int)
	for _, task := range tasks {
		scenarioCounts[task.Scenario]++
	}
	manifest := evaluationRunManifest{SchemaVersion: "stablepay.agent-shopping-run.v1", RunID: runID,
		CreatedAtUTC: createdAt.Format(time.RFC3339Nano), Evaluation: report.Evaluation,
		Provider: "not_used", Model: "not_used", Seed: *seed, TaskCount: len(tasks),
		ProductCount: len(catalog.Products), InventoryRows: len(catalog.Inventory),
		SourceManifest: ".local-run/simulated-merchants/manifest.json", Hashes: hashes,
		ScenarioCounts: scenarioCounts, RealPaymentCalled: false, SimulationDBWritten: false, LLMCalled: false,
		MetricBoundary: "Structured gold-intent ranking only; excludes intent-parser quality, persistent API latency, payment and Devnet."}

	if err := os.MkdirAll(outputRoot, 0o700); err != nil {
		return fmt.Errorf("create local evaluation output root: %w", err)
	}
	staging, err := os.MkdirTemp(outputRoot, ".staging-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := writeJSONLines(filepath.Join(staging, "suite.jsonl"), tasks); err != nil {
		return err
	}
	if err := writeJSONLines(filepath.Join(staging, "trials.jsonl"), trials); err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(staging, "metrics.json"), report); err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(staging, "manifest.json"), manifest); err != nil {
		return err
	}
	finalDir := filepath.Join(outputRoot, runID)
	if _, err := os.Stat(finalDir); err == nil {
		return fmt.Errorf("evaluation run already exists: %s", finalDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(staging, finalDir); err != nil {
		return fmt.Errorf("publish completed evaluation artifacts: %w", err)
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"run_id": runID, "run_dir": finalDir, "manifest_sha256": hashes.ManifestSHA256,
		"catalog_sha256": hashes.CatalogSHA256, "overlay_sha256": hashes.OverlaySHA256,
		"tasks_sha256": hashes.TasksSHA256, "task_count": len(tasks), "provider": "not_used",
		"baseline_recall_at_20":     report.Baseline.MacroRecallAtK,
		"treatment_recall_at_20":    report.Treatment.MacroRecallAtK,
		"baseline_valid_plan_rate":  report.Baseline.ValidPlanRate,
		"treatment_valid_plan_rate": report.Treatment.ValidPlanRate,
	})
}

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && strings.EqualFold(filepath.Clean(leftAbs), filepath.Clean(rightAbs))
}

func writeJSONFile(path string, value any) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(value)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func writeJSONLines[T any](path string, values []T) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	writer := bufio.NewWriter(file)
	for _, value := range values {
		data, err := json.Marshal(value)
		if err != nil {
			_ = file.Close()
			return err
		}
		if _, err := writer.Write(data); err != nil {
			_ = file.Close()
			return err
		}
		if err := writer.WriteByte('\n'); err != nil {
			_ = file.Close()
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return nil
}
