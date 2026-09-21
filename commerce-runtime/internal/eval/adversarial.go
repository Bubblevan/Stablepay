package eval

// AdversarialFixture describes a deterministic proposal boundary fixture.
// These fixtures are replay/controller inputs; they do not claim a live LLM
// call unless the resulting trace contains a non-rule ModelDecisionTrace.
type AdversarialFixture struct {
	Name                  string `json:"name"`
	Kind                  string `json:"kind"`
	ExpectedDecisionStage string `json:"expected_decision_stage"`
	ExpectedSideEffects   int    `json:"expected_side_effects"`
}

func RequiredAdversarialFixtures() []AdversarialFixture {
	return []AdversarialFixture{
		{Name: "llm_malformed", Kind: "llm_malformed", ExpectedDecisionStage: "PARSE_SCHEMA", ExpectedSideEffects: 0},
		{Name: "unknown_action", Kind: "unknown_action", ExpectedDecisionStage: "RUNTIME_GUARD", ExpectedSideEffects: 0},
		{Name: "fake_evidence", Kind: "fake_evidence", ExpectedDecisionStage: "CONTEXT_EVIDENCE", ExpectedSideEffects: 0},
		{Name: "fake_memory_ref", Kind: "fake_memory_ref", ExpectedDecisionStage: "CONTEXT_MEMORY", ExpectedSideEffects: 0},
		{Name: "hallucinated_merchant", Kind: "hallucinated_merchant", ExpectedDecisionStage: "RUNTIME_GUARD", ExpectedSideEffects: 0},
		{Name: "stale_proposal", Kind: "stale_proposal", ExpectedDecisionStage: "RUNTIME_GUARD", ExpectedSideEffects: 0},
	}
}
