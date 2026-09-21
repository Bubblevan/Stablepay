package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/stablepay/commerce-runtime/internal/eval"
	"github.com/stablepay/commerce-runtime/internal/observability"
)

func TestDeliveryInvalidSkipsChallengeAndInjectsExactlyOnce(t *testing.T) {
	p := &proxy{profile: observability.FailureInjection{Configured: true, Kind: "delivery_invalid", RatePercent: 100, Repeat: 1}, caseID: "delivery-case", seed: 7}
	request := httptest.NewRequest("GET", "http://proxy.test/merchant", nil)
	if got := p.nextFailure(request); got != "" {
		t.Fatalf("challenge must pass through, got %q", got)
	}
	if got := p.nextFailure(request); got != "delivery_invalid" {
		t.Fatalf("paid delivery must be corrupted exactly once, got %q", got)
	}
	if got := p.nextFailure(request); got != "" {
		t.Fatalf("repeat=1 injected more than once: %q", got)
	}
	if p.profile.RequestCount != 3 || p.profile.EligibleCount != 1 || p.profile.InjectionCount != 1 || !p.profile.Triggered {
		t.Fatalf("unexpected injection counters: %#v", p.profile)
	}

	response := httptest.NewRecorder()
	p.status(response)
	var status map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status["injection_count"] != float64(1) || status["eligible_count"] != float64(1) || status["request_count"] != float64(3) {
		t.Fatalf("status did not expose actual counters: %#v", status)
	}
}

func TestConfiguredSamplerMissDoesNotInject(t *testing.T) {
	caseID := ""
	for index := 0; index < 100; index++ {
		candidate := fmt.Sprintf("sampler-case-%d", index)
		if !eval.InjectAt(41, candidate, "merchant_transient", 1, 10) {
			caseID = candidate
			break
		}
	}
	if caseID == "" {
		t.Fatal("could not find deterministic 10% sampler miss")
	}
	p := &proxy{profile: observability.FailureInjection{Configured: true, Kind: "merchant_transient", RatePercent: 10, Repeat: 1}, caseID: caseID, seed: 41}
	if got := p.nextFailure(httptest.NewRequest("GET", "http://proxy.test/merchant", nil)); got != "" {
		t.Fatalf("configured sampler miss injected a fault: %q", got)
	}
	if p.profile.InjectionCount != 0 || p.profile.Triggered {
		t.Fatalf("sampler miss changed injection state: %#v", p.profile)
	}
}
