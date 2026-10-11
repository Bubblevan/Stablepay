package eval

import (
	"encoding/json"
	"testing"
)

func TestExpandTrialsSingleTrialAssignsPairingMetadata(t *testing.T) {
	input := []Scenario{{CaseID: "case-a", Seed: 1, Request: json.RawMessage(`{"request_id":"request-a"}`), TrialIsolationID: "stale"}}
	got, err := ExpandTrials(input, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].TaskID != "case-a" || got[0].TrialIndex != 1 || got[0].TrialIsolationID != "" {
		t.Fatalf("single-trial metadata = %#v", got)
	}
	if input[0].TrialIndex != 0 || input[0].TaskID != "" || input[0].TrialIsolationID != "stale" {
		t.Fatalf("ExpandTrials mutated input: %#v", input[0])
	}
}
