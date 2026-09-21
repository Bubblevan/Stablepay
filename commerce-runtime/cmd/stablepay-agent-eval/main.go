// Command stablepay-agent-eval runs the S11 external-client benchmark and
// exports only redacted Runtime observability facts.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/eval"
	"github.com/stablepay/commerce-runtime/internal/observability"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "run":
		err = run(os.Args[2:])
	case "report":
		err = report(os.Args[2:])
	case "export":
		err = export(os.Args[2:])
	case "check":
		err = check(os.Args[2:])
	case "plans":
		err = plans()
	default:
		usage()
		err = errors.New("unknown command")
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "stablepay-agent-eval: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	set := flag.NewFlagSet("run", flag.ContinueOnError)
	dataset := set.String("dataset", "", "scenario JSONL dataset")
	outDir := set.String("out-dir", "s11-results", "output directory")
	server := set.String("server", envOr("COMMERCE_RUNTIME_URL", "http://127.0.0.1:8090"), "Runtime server URL")
	token := set.String("token", os.Getenv("COMMERCE_RUNTIME_API_TOKEN"), "Runtime API token")
	injector := set.String("injector", "", "external fault-controller URL")
	runtimeCLI := set.String("runtime-cli", "", "stablepay-runtime executable for true CLI ingress")
	trials := set.Int("trials", 1, "independent trials per task")
	poll := set.Duration("poll", 500*time.Millisecond, "status poll interval")
	timeout := set.Duration("timeout", 5*time.Minute, "per episode poll timeout")
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
	scenarios, err = expandTrials(scenarios, *trials)
	if err != nil {
		return err
	}
	client := eval.Client{BaseURL: *server, Token: *token, InjectorURL: *injector, RuntimeCLI: *runtimeCLI, PollInterval: *poll, PollTimeout: *timeout}
	results, err := client.Run(context.Background(), scenarios)
	if err != nil {
		return err
	}
	return writeOutputs(*outDir, results)
}

func report(args []string) error {
	set := flag.NewFlagSet("report", flag.ContinueOnError)
	input := set.String("input", "", "episode_results.jsonl")
	outDir := set.String("out-dir", "s11-results", "output directory")
	if err := set.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*input) == "" {
		return errors.New("--input is required")
	}
	results, err := readResults(*input)
	if err != nil {
		return err
	}
	return writeOutputs(*outDir, results)
}

func export(args []string) error {
	set := flag.NewFlagSet("export", flag.ContinueOnError)
	server := set.String("server", envOr("COMMERCE_RUNTIME_URL", "http://127.0.0.1:8090"), "Runtime server URL")
	token := set.String("token", os.Getenv("COMMERCE_RUNTIME_API_TOKEN"), "Runtime API token")
	episodeID := set.String("episode-id", "", "episode id")
	out := set.String("out", "trace.json", "trace JSON output")
	includeBody := set.Bool("include-artifact-body", false, "explicitly include artifact body")
	if err := set.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*episodeID) == "" {
		return errors.New("--episode-id is required")
	}
	path := "/v1/episodes/" + *episodeID + "/observability"
	if *includeBody {
		path += "?include_artifact_body=true"
	}
	request, err := http.NewRequest(http.MethodGet, strings.TrimRight(*server, "/")+path, nil)
	if err != nil {
		return err
	}
	if strings.TrimSpace(*token) != "" {
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(*token))
	}
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("observability returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var traceValue observability.EpisodeTrace
	if err := json.Unmarshal(body, &traceValue); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(traceValue, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, append(encoded, '\n'), 0600); err != nil {
		return err
	}
	fmt.Printf("trace exported: %s\n", *out)
	return nil
}

func check(args []string) error {
	set := flag.NewFlagSet("check", flag.ContinueOnError)
	server := set.String("server", envOr("COMMERCE_RUNTIME_URL", "http://127.0.0.1:8090"), "Runtime server URL")
	token := set.String("token", os.Getenv("COMMERCE_RUNTIME_API_TOKEN"), "Runtime API token")
	if err := set.Parse(args); err != nil {
		return err
	}
	healthStatus, readinessStatus, err := (eval.Client{BaseURL: *server, Token: *token}).Check(context.Background())
	if err != nil {
		return err
	}
	fmt.Printf("healthz=%d readyz=%d\n", healthStatus, readinessStatus)
	if healthStatus < 200 || healthStatus >= 300 || readinessStatus < 200 || readinessStatus >= 300 {
		return fmt.Errorf("runtime is not healthy and ready")
	}
	return nil
}

func plans() error {
	encoded, err := json.MarshalIndent(eval.RequiredFailurePlans(), "", "  ")
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(encoded, '\n'))
	return err
}

func writeOutputs(outDir string, results []observability.EpisodeResult) error {
	if err := os.MkdirAll(outDir, 0700); err != nil {
		return err
	}
	jsonl, err := os.Create(filepath.Join(outDir, "episode_results.jsonl"))
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(jsonl)
	for _, result := range results {
		if err := encoder.Encode(result); err != nil {
			_ = jsonl.Close()
			return err
		}
	}
	if err := jsonl.Close(); err != nil {
		return err
	}
	grades, err := os.Create(filepath.Join(outDir, "grade_results.jsonl"))
	if err != nil {
		return err
	}
	gradeEncoder := json.NewEncoder(grades)
	for _, result := range results {
		grade := result.Grade
		if len(grade.Assertions) == 0 {
			grade = observability.GradeEpisode(result)
		}
		if err := gradeEncoder.Encode(map[string]any{"case_id": result.CaseID, "task_id": result.TaskID, "trial_index": result.TrialIndex, "grade": grade}); err != nil {
			_ = grades.Close()
			return err
		}
	}
	if err := grades.Close(); err != nil {
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
	if err := os.WriteFile(filepath.Join(outDir, "report.md"), []byte(observability.RenderMarkdown(metrics, results)), 0600); err != nil {
		return err
	}
	fmt.Printf("S11 outputs: %s\n", outDir)
	return nil
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
			return nil, fmt.Errorf("decode scenario: %w", err)
		}
		result = append(result, value)
	}
	return result, scanner.Err()
}

func readResults(path string) ([]observability.EpisodeResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var result []observability.EpisodeResult
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 8<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var value observability.EpisodeResult
		if err := json.Unmarshal([]byte(line), &value); err != nil {
			return nil, fmt.Errorf("decode episode result: %w", err)
		}
		result = append(result, value)
	}
	return result, scanner.Err()
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: stablepay-agent-eval {run|report|export|check|plans} [flags]")
}

func expandTrials(scenarios []eval.Scenario, trials int) ([]eval.Scenario, error) {
	if trials <= 0 {
		return nil, errors.New("--trials must be positive")
	}
	if trials == 1 {
		return scenarios, nil
	}
	result := make([]eval.Scenario, 0, len(scenarios)*trials)
	for _, scenario := range scenarios {
		taskID := scenario.TaskID
		if taskID == "" {
			taskID = scenario.CaseID
		}
		for trial := 0; trial < trials; trial++ {
			copy := scenario
			copy.TaskID = taskID
			copy.TrialIndex = trial + 1
			copy.CaseID = fmt.Sprintf("%s#trial-%d", scenario.CaseID, trial+1)
			copy.Seed = scenario.Seed + int64((trial+1)*1000003)
			if len(copy.Request) > 0 {
				var request map[string]any
				if err := json.Unmarshal(copy.Request, &request); err != nil {
					return nil, fmt.Errorf("trial request %s: %w", copy.CaseID, err)
				}
				request["request_id"] = fmt.Sprintf("%s-trial-%d", requestIDFromScenario(copy.Request), trial+1)
				encoded, err := json.Marshal(request)
				if err != nil {
					return nil, err
				}
				copy.Request = encoded
			}
			result = append(result, copy)
		}
	}
	return result, nil
}

func requestIDFromScenario(raw json.RawMessage) string {
	var value struct {
		RequestID string `json:"request_id"`
	}
	_ = json.Unmarshal(raw, &value)
	if strings.TrimSpace(value.RequestID) == "" {
		return "s11-task"
	}
	return value.RequestID
}
