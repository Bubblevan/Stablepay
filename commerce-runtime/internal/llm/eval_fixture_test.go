package llm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stablepay/commerce-runtime/internal/decision"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

func TestCheckedInDecisionEvalFixtureCoversS6Cases(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "s6", "decision_eval.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		CaseID         string `json:"case_id"`
		ExpectedAction string `json:"expected_action"`
		Class          string `json:"class"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 9 {
		t.Fatalf("fixture cases=%d", len(cases))
	}
	seen := make(map[string]bool)
	for _, value := range cases {
		seen[value.CaseID] = true
		if value.ExpectedAction == "PAYMENT_CONFIRMED" {
			if decision.ProviderAllowedAction(trace.ActionType(value.ExpectedAction)) {
				t.Fatal("fixture accidentally allows a runtime payment action")
			}
			continue
		}
		if !decision.ProviderAllowedAction(trace.ActionType(value.ExpectedAction)) && trace.ActionType(value.ExpectedAction) != trace.ActionSelectMerchant {
			t.Fatalf("fixture action not represented by S6 authority: %#v", value)
		}
	}
	for _, required := range []string{"select-merchant", "retry-same-merchant", "switch-merchant", "rediscover", "ask-parent", "stop", "malicious-retrieval", "hallucinated-target", "budget-insufficient"} {
		if !seen[required] {
			t.Fatalf("missing fixture case %s", required)
		}
	}
}
