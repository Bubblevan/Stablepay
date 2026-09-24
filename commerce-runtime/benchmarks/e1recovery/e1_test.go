package e1recovery

import "testing"

func TestAggregateMarksUnavailableUsageWithoutPublishingNumbers(t *testing.T) {
	metrics := aggregate([]DecisionResult{{
		Provider:         "deepseek",
		TokenUsageStatus: "TOKEN_METRIC_UNAVAILABLE",
	}})

	if metrics.InputTokens.Status != "TOKEN_METRIC_UNAVAILABLE" || metrics.OutputTokens.Status != "TOKEN_METRIC_UNAVAILABLE" {
		t.Fatalf("token status = %q/%q, want TOKEN_METRIC_UNAVAILABLE", metrics.InputTokens.Status, metrics.OutputTokens.Status)
	}
	if metrics.InputTokens.Total != nil || metrics.InputTokens.Mean != nil || metrics.OutputTokens.Total != nil || metrics.OutputTokens.Mean != nil {
		t.Fatal("unavailable token usage must not publish numeric totals or means")
	}
	if metrics.AverageTokensPerRecovery != nil || metrics.EstimatedAPICostUSD != nil {
		t.Fatal("unavailable token usage must not publish an average or cost")
	}
}

func TestAggregatePublishesUsageOnlyWhenEveryTrialHasIt(t *testing.T) {
	metrics := aggregate([]DecisionResult{
		{TokenUsageStatus: "AVAILABLE", InputTokens: 12, OutputTokens: 4},
		{TokenUsageStatus: "TOKEN_METRIC_UNAVAILABLE", InputTokens: 0, OutputTokens: 0},
	})

	if metrics.InputTokens.Status != "TOKEN_METRIC_UNAVAILABLE" || metrics.OutputTokens.Status != "TOKEN_METRIC_UNAVAILABLE" {
		t.Fatalf("partial usage status = %q/%q, want TOKEN_METRIC_UNAVAILABLE", metrics.InputTokens.Status, metrics.OutputTokens.Status)
	}
	if metrics.InputTokens.Total != nil || metrics.OutputTokens.Total != nil || metrics.AverageTokensPerRecovery != nil {
		t.Fatal("partial usage must not be reported as a complete aggregate")
	}
}
