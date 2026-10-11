// Command s11-live-local-suite runs the S11 external-client benchmark with a
// fresh live-local Runtime and fault controller for every expanded trial.
// It exercises only public HTTP/MCP/CLI ingress through eval.Client.
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/eval"
	"github.com/stablepay/commerce-runtime/internal/livelocal"
	"github.com/stablepay/commerce-runtime/internal/observability"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "s11-live-local-suite: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	set := flag.NewFlagSet("s11-live-local-suite", flag.ContinueOnError)
	dataset := set.String("dataset", "", "scenario JSONL dataset")
	outDir := set.String("out-dir", "s11-live-local-suite", "output directory")
	runtimeCLI := set.String("runtime-cli", "", "stablepay-runtime executable for CLI ingress")
	trials := set.Int("trials", 1, "fresh Runtime trials per task")
	poll := set.Duration("poll", 50*time.Millisecond, "status poll interval")
	timeout := set.Duration("timeout", 45*time.Second, "per episode timeout")
	if err := set.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*dataset) == "" {
		return errors.New("--dataset is required")
	}
	datasetBytes, err := os.ReadFile(*dataset)
	if err != nil {
		return err
	}
	datasetDigest := sha256.Sum256(datasetBytes)
	scenarios, err := readScenarios(*dataset)
	if err != nil {
		return err
	}
	taskCount := uniqueTaskCount(scenarios)
	scenarios, err = eval.ExpandTrials(scenarios, *trials)
	if err != nil {
		return err
	}
	results := make([]observability.EpisodeResult, 0, len(scenarios))
	for index, scenario := range scenarios {
		if err := scenario.Validate(); err != nil {
			return fmt.Errorf("case %s: %w", scenario.CaseID, err)
		}
		if strings.TrimSpace(scenario.TrialIsolationID) == "" {
			taskID := scenario.TaskID
			if taskID == "" {
				taskID = scenario.CaseID
			}
			scenario.TrialIsolationID = fmt.Sprintf("live-local-runtime:%s:%s:%d:%d", scenario.CaseID, taskID, scenario.TrialIndex, scenario.Seed)
		}
		root, err := livelocal.New(context.Background(), livelocal.Config{MemoryMode: defaultValue(scenario.MemoryMode, "on"), LLMMode: llmMode(scenario.RecoveryProvider)})
		if err != nil {
			return fmt.Errorf("case %s runtime: %w", scenario.CaseID, err)
		}
		server := httptest.NewServer(root.Handler)
		client := eval.Client{BaseURL: server.URL, InjectorURL: server.URL, RuntimeCLI: *runtimeCLI, PollInterval: *poll, PollTimeout: *timeout}
		if index == 0 {
			if health, ready, checkErr := client.Check(context.Background()); checkErr != nil || health != 200 || ready != 200 {
				server.Close()
				root.Close()
				return fmt.Errorf("public health/readiness check failed: health=%d ready=%d err=%v", health, ready, checkErr)
			}
		}
		result, runErr := client.Run(context.Background(), []eval.Scenario{scenario})
		server.Close()
		root.Close()
		if runErr != nil {
			return fmt.Errorf("case %s: %w", scenario.CaseID, runErr)
		}
		if len(result) != 1 {
			return fmt.Errorf("case %s returned %d results", scenario.CaseID, len(result))
		}
		results = append(results, result[0])
	}
	if err := writeOutputs(*outDir, results); err != nil {
		return err
	}
	manifest := suiteManifest{
		SchemaVersion:          "agentpay-benchmark.v1",
		Benchmark:              "S11 isolated live-local Agent commerce suite",
		GeneratedAtUTC:         time.Now().UTC(),
		GoVersion:              buildGoVersion(),
		SourceRevision:         sourceRevision(),
		SourceModified:         sourceModifiedAtBuild(),
		DatasetFile:            filepath.Base(*dataset),
		DatasetSHA256:          fmt.Sprintf("sha256:%x", datasetDigest),
		DistinctTaskProfiles:   taskCount,
		IndependentTrialsEach:  *trials,
		ExpandedEpisodeCount:   len(scenarios),
		RuntimePerTrial:        "fresh livelocal Runtime and isolated in-memory repository",
		MerchantPaymentAdapter: "deterministic local mocks; no Payment Service or chain client",
		ProviderBoundary:       "local deterministic decision provider only; no external LLM request",
		IngressBoundary:        "public HTTP/MCP/CLI client boundary over loopback httptest server",
		ExternalNetworkCalls:   false,
		PollInterval:           poll.String(),
		EpisodeTimeout:         timeout.String(),
	}
	encodedManifest, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*outDir, "manifest.json"), append(encodedManifest, '\n'), 0600); err != nil {
		return err
	}
	fmt.Printf("S11 isolated live-local outputs: %s\n", *outDir)
	return nil
}

type suiteManifest struct {
	SchemaVersion          string    `json:"schema_version"`
	Benchmark              string    `json:"benchmark"`
	GeneratedAtUTC         time.Time `json:"generated_at_utc"`
	GoVersion              string    `json:"go_version,omitempty"`
	SourceRevision         string    `json:"source_revision,omitempty"`
	SourceModified         string    `json:"source_modified,omitempty"`
	DatasetFile            string    `json:"dataset_file"`
	DatasetSHA256          string    `json:"dataset_sha256"`
	DistinctTaskProfiles   int       `json:"distinct_task_profiles"`
	IndependentTrialsEach  int       `json:"independent_trials_each"`
	ExpandedEpisodeCount   int       `json:"expanded_episode_count"`
	RuntimePerTrial        string    `json:"runtime_per_trial"`
	MerchantPaymentAdapter string    `json:"merchant_payment_adapter"`
	ProviderBoundary       string    `json:"provider_boundary"`
	IngressBoundary        string    `json:"ingress_boundary"`
	ExternalNetworkCalls   bool      `json:"external_network_calls"`
	PollInterval           string    `json:"poll_interval"`
	EpisodeTimeout         string    `json:"episode_timeout"`
}

func uniqueTaskCount(scenarios []eval.Scenario) int {
	tasks := make(map[string]struct{}, len(scenarios))
	for _, scenario := range scenarios {
		taskID := strings.TrimSpace(scenario.TaskID)
		if taskID == "" {
			taskID = scenario.CaseID
		}
		tasks[taskID] = struct{}{}
	}
	return len(tasks)
}

func buildSetting(key string) string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == key {
			return setting.Value
		}
	}
	return ""
}

func sourceModifiedAtBuild() string {
	// The PowerShell runner captures git status before the benchmark output
	// directory is created. Prefer that source-only snapshot to Go's VCS flag,
	// which can report modified for a clean linked worktree on some hosts.
	if value := strings.ToLower(strings.TrimSpace(os.Getenv("STABLEPAY_SOURCE_MODIFIED"))); value == "true" || value == "false" {
		return value
	}
	return buildSetting("vcs.modified")
}

func sourceRevision() string {
	if value := strings.TrimSpace(os.Getenv("STABLEPAY_SOURCE_REVISION")); value != "" {
		return value
	}
	return buildSetting("vcs.revision")
}

func buildGoVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return info.GoVersion
}

func llmMode(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "llm":
		return "local"
	case "baseline_stop":
		return "stop"
	default:
		return "rule"
	}
}

func defaultValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func readScenarios(path string) ([]eval.Scenario, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var result []eval.Scenario
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 8<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var value eval.Scenario
		if err := json.Unmarshal([]byte(line), &value); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, scanner.Err()
}

func writeOutputs(outDir string, results []observability.EpisodeResult) error {
	if err := os.MkdirAll(outDir, 0700); err != nil {
		return err
	}
	writeJSONL := func(name string, values []any) error {
		file, err := os.Create(filepath.Join(outDir, name))
		if err != nil {
			return err
		}
		defer file.Close()
		encoder := json.NewEncoder(file)
		for _, value := range values {
			if err := encoder.Encode(value); err != nil {
				return err
			}
		}
		return nil
	}
	resultValues := make([]any, len(results))
	gradeValues := make([]any, len(results))
	for index, result := range results {
		resultValues[index] = result
		grade := result.Grade
		if len(grade.Assertions) == 0 {
			grade = observability.GradeEpisode(result)
		}
		gradeValues[index] = map[string]any{"case_id": result.CaseID, "task_id": result.TaskID, "trial_index": result.TrialIndex, "trial_isolation_id": result.TrialIsolationID, "grade": grade}
	}
	if err := writeJSONL("episode_results.jsonl", resultValues); err != nil {
		return err
	}
	if err := writeJSONL("grade_results.jsonl", gradeValues); err != nil {
		return err
	}
	metrics := observability.Compute(results, time.Now().UTC())
	encoded, err := json.MarshalIndent(metrics, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "metrics.json"), append(encoded, '\n'), 0600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "report.md"), []byte(observability.RenderMarkdown(metrics, results)), 0600)
}
