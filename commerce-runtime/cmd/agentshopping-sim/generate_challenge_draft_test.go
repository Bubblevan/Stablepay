package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stablepay/commerce-runtime/benchmarks/agentshopping"
)

func TestEvaluateChallengeRequiresExplicitSuitePath(t *testing.T) {
	err := evaluateChallenge(nil)
	if err == nil || !strings.Contains(err.Error(), "explicit --tasks path") {
		t.Fatalf("evaluation must refuse an implicit/missing suite path before model work, got %v", err)
	}
}

func TestEvaluateChallengeRequiresReviewBundleBeforeAnyEvaluationWork(t *testing.T) {
	err := evaluateChallenge([]string{"--tasks", filepath.Join(t.TempDir(), "tasks.jsonl"), "--require-human-review"})
	if err == nil || !strings.Contains(err.Error(), "requires --review-bundle") {
		t.Fatalf("required human review must reject a missing bundle before loading data or calling a model, got %v", err)
	}
}

func TestOracleChallengeResultsUseGoldIntentsWithoutModelInvocation(t *testing.T) {
	tasks := []agentshopping.ChallengeTask{{TaskID: "oracle-1", Intent: agentshopping.Intent{Item: "beans"}}}
	results := challengeOracleIntentResults(tasks)
	if len(results) != 1 || results[0].TaskID != tasks[0].TaskID || results[0].Intent.Item != "beans" || results[0].Error != "" {
		t.Fatalf("oracle results should be a one-to-one gold-intent projection: %+v", results)
	}
	boundary := challengeMetricBoundary(true, agentshopping.ChallengeProvenanceUnverified)
	if !strings.Contains(boundary, "makes no model/API calls") || !strings.Contains(boundary, "upper-bound diagnostic") {
		t.Fatalf("oracle run artifact must disclose its non-model diagnostic boundary: %s", boundary)
	}
}

func TestPathWithinDirectoryChecksPathComponents(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".local-run", "agent-shopping", "challenge-suites")
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "root", path: root, want: true},
		{name: "nested draft", path: filepath.Join(root, "draft-v1", "tasks.jsonl"), want: true},
		{name: "prefix sibling", path: root + "-outside", want: false},
		{name: "parent traversal", path: filepath.Join(root, "..", "secrets.env"), want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := pathWithinDirectory(root, test.path); got != test.want {
				t.Fatalf("pathWithinDirectory(%q, %q) = %t, want %t", root, test.path, got, test.want)
			}
		})
	}
}
