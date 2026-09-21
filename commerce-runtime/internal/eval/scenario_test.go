package eval

import (
	"testing"
)

func TestInjectAtIsDeterministicAndBounded(t *testing.T) {
	first := InjectAt(42, "case-a", 3, 20)
	for index := 0; index < 20; index++ {
		if InjectAt(42, "case-a", 3, 20) != first {
			t.Fatal("sampler is not deterministic")
		}
	}
	if InjectAt(42, "case-a", 3, 0) || !InjectAt(42, "case-a", 3, 100) {
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
