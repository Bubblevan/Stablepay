package observability

import "testing"

func TestRuntimeVariantMatchesPartialExpectationAndRejectsMismatch(t *testing.T) {
	actual := RuntimeVariant{RuntimeVersion: "runtime-1", MemoryMode: "on", RecoveryProvider: "llm", LLMProvider: "deepseek", ModelRef: "deepseek-chat"}.WithHash()
	if !actual.Matches(RuntimeVariant{MemoryMode: "on", RecoveryProvider: "llm"}) {
		t.Fatal("partial runtime variant expectation should match actual runtime")
	}
	if actual.Matches(RuntimeVariant{MemoryMode: "off"}) {
		t.Fatal("memory variant mismatch was not rejected")
	}
	if actual.Matches(RuntimeVariant{ConfigHash: "sha256:not-actual"}) {
		t.Fatal("config hash mismatch was not rejected")
	}
}
