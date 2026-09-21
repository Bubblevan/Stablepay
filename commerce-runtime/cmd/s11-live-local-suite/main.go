// Command s11-live-local-suite runs the S11 external-client benchmark with a
// fresh live-local Runtime and fault controller for every expanded trial.
// It exercises only public HTTP/MCP/CLI ingress through eval.Client.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	scenarios, err := readScenarios(*dataset)
	if err != nil {
		return err
	}
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
			scenario.TrialIsolationID = fmt.Sprintf("live-local-runtime:%s:%d:%d", taskID, scenario.TrialIndex, scenario.Seed)
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
	fmt.Printf("S11 isolated live-local outputs: %s\n", *outDir)
	return nil
}

func llmMode(provider string) string {
	if strings.EqualFold(strings.TrimSpace(provider), "llm") {
		return "local"
	}
	return "rule"
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
