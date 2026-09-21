package eval

import (
	"testing"
)

func TestInjectAtIsDeterministicAndBounded(t *testing.T) {
	first := InjectAt(42, "case-a", "merchant_transient", 3, 20)
	for index := 0; index < 20; index++ {
		if InjectAt(42, "case-a", "merchant_transient", 3, 20) != first {
			t.Fatal("sampler is not deterministic")
		}
	}
	if InjectAt(42, "case-a", "merchant_transient", 3, 0) || !InjectAt(42, "case-a", "merchant_transient", 3, 100) {
		t.Fatal("rate bounds are incorrect")
	}
}

func TestRequiredFailurePlansCoverS11Matrix(t *testing.T) {
	plans := RequiredFailurePlans()
	seen := map[string]bool{}
	for _, plan := range plans {
		seen[plan.Kind] = true
	}
	for _, required := range []string{"merchant_transient", "merchant_permanent", "payment_transient", "payment_permanent", "delivery_invalid", "verification_mismatch", "llm_malformed", "llm_timeout", "crash_restart"} {
		if !seen[required] {
			t.Fatalf("missing failure plan %s", required)
		}
	}
}

func TestRequiredAdversarialFixturesCoverProposalBoundaries(t *testing.T) {
	fixtures := RequiredAdversarialFixtures()
	seen := map[string]bool{}
	for _, fixture := range fixtures {
		if fixture.ExpectedSideEffects != 0 {
			t.Fatalf("adversarial fixture must start side-effect-free: %#v", fixture)
		}
		seen[fixture.Kind] = true
	}
	for _, required := range []string{"llm_malformed", "unknown_action", "fake_evidence", "fake_memory_ref", "hallucinated_merchant", "stale_proposal"} {
		if !seen[required] {
			t.Fatalf("missing adversarial fixture %s", required)
		}
	}
}
