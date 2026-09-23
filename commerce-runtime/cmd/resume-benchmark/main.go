package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/stablepay/commerce-runtime/benchmarks/e1recovery"
	"github.com/stablepay/commerce-runtime/benchmarks/e2guard"
)

func main() {
	benchmark := flag.String("benchmark", "e2", "benchmark to run: e1, e2, or all")
	repoRoot := flag.String("repo-root", "", "repository root")
	outputRoot := flag.String("output-root", "", "evidence root; defaults to .local-run/resume-benchmark")
	seed := flag.Int64("seed", 42, "frozen dataset seed")
	deepseekTrials := flag.Int("deepseek-trials", 3, "real DeepSeek trials per scenario")
	runDeepSeek := flag.Bool("run-deepseek", false, "run the real configured provider; never uses a local fake")
	flag.Parse()
	root := *repoRoot
	if root == "" {
		root, _ = os.Getwd()
	}
	if *outputRoot == "" {
		*outputRoot = filepath.Join(root, ".local-run", "resume-benchmark")
	}
	ctx := context.Background()
	switch *benchmark {
	case "e2":
		metrics, err := e2guard.Run(ctx, e2guard.RunOptions{OutputDir: filepath.Join(*outputRoot, "e2-guard"), RepoRoot: root, Seed: *seed})
		if err != nil {
			fatal(err)
		}
		fmt.Printf("E2 %s: unsafe rejection %.2f%%, valid acceptance %.2f%%\n", metrics.Status, metrics.UnsafeProposalRejection.Rate*100, metrics.ValidProposalAcceptance.Rate*100)
	case "e1":
		metrics, err := e1recovery.Run(ctx, e1recovery.RunOptions{OutputDir: filepath.Join(*outputRoot, "e1-recovery"), RepoRoot: root, Seed: *seed, DeepSeekTrials: *deepseekTrials, RunDeepSeek: *runDeepSeek, LLMProvider: env("LLM_PROVIDER", "deepseek"), LLMBaseURL: os.Getenv("LLM_BASE_URL"), LLMAPIKey: os.Getenv("LLM_API_KEY"), LLMModel: firstNonEmpty(os.Getenv("LLM_MODEL"), os.Getenv("LLM_MODEL_ID"))})
		if err != nil {
			fatal(err)
		}
		fmt.Printf("E1 %s: Rule recovery success %.2f%%, DeepSeek %s\n", metrics.Status, metrics.Rule.RecoverySuccess.Rate*100, metrics.DeepSeek.Status)
	case "all":
		e2Metrics, err := e2guard.Run(ctx, e2guard.RunOptions{OutputDir: filepath.Join(*outputRoot, "e2-guard"), RepoRoot: root, Seed: *seed})
		if err != nil {
			fatal(err)
		}
		fmt.Printf("E2 %s: unsafe rejection %.2f%%, valid acceptance %.2f%%\n", e2Metrics.Status, e2Metrics.UnsafeProposalRejection.Rate*100, e2Metrics.ValidProposalAcceptance.Rate*100)
		if e2Metrics.Status == "BLOCKER" {
			fmt.Fprintln(os.Stderr, "STOP: E2 exposed a false accept or side-effect escape; E1 was not run")
			os.Exit(2)
		}
		e1Metrics, err := e1recovery.Run(ctx, e1recovery.RunOptions{OutputDir: filepath.Join(*outputRoot, "e1-recovery"), RepoRoot: root, Seed: *seed, DeepSeekTrials: *deepseekTrials, RunDeepSeek: *runDeepSeek, LLMProvider: env("LLM_PROVIDER", "deepseek"), LLMBaseURL: os.Getenv("LLM_BASE_URL"), LLMAPIKey: os.Getenv("LLM_API_KEY"), LLMModel: firstNonEmpty(os.Getenv("LLM_MODEL"), os.Getenv("LLM_MODEL_ID"))})
		if err != nil {
			fatal(err)
		}
		fmt.Printf("E1 %s: Rule recovery success %.2f%%, DeepSeek %s\n", e1Metrics.Status, e1Metrics.Rule.RecoverySuccess.Rate*100, e1Metrics.DeepSeek.Status)
		fmt.Println("E3/E4 are external load/chaos runs; use scripts/benchmark-e3-load.ps1 and scripts/benchmark-e4-chaos.ps1 before claiming B0 complete.")
	default:
		fatal(fmt.Errorf("unknown benchmark %q", *benchmark))
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
