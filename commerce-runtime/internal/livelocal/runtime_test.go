package livelocal

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/adapters"
	"github.com/stablepay/commerce-runtime/internal/eval"
	"github.com/stablepay/commerce-runtime/internal/invocation"
	"github.com/stablepay/commerce-runtime/internal/ledger"
	"github.com/stablepay/commerce-runtime/internal/observability"
	"github.com/stablepay/commerce-runtime/internal/payment"
)

func TestFaultControllerRateZeroDoesNotModifyMerchant(t *testing.T) {
	fault := &faultController{CaseID: "rate-zero", Seed: 41, Kind: "merchant_transient", RatePercent: 0, Repeat: 1, Configured: true}
	merchant := &localMerchant{calls: map[string]int{}, fault: fault}
	result, err := merchant.Invoke(context.Background(), adapters.MerchantInvokeRequest{EpisodeID: "episode-1", InputRef: "local://merchant-transient", Phase: invocation.PhaseInitial, Attempt: 1})
	if err != nil || result.HTTPStatus != 402 || fault.InjectionCount != 0 {
		t.Fatalf("rate=0 modified merchant: err=%v status=%d injections=%d", err, result.HTTPStatus, fault.InjectionCount)
	}
}

func TestFaultControllerRateHundredIsSourceOfTruth(t *testing.T) {
	fault := &faultController{CaseID: "rate-full", Seed: 41, Kind: "merchant_transient", RatePercent: 100, Repeat: 1, Configured: true}
	merchant := &localMerchant{calls: map[string]int{}, fault: fault}
	_, err := merchant.Invoke(context.Background(), adapters.MerchantInvokeRequest{EpisodeID: "episode-1", InputRef: "local://merchant-transient", Phase: invocation.PhaseInitial, Attempt: 1})
	if err == nil || fault.InjectionCount != 1 || !fault.status().Configured {
		t.Fatalf("rate=100 did not authorize actual merchant fault: err=%v status=%#v", err, fault.status())
	}
}

func TestLocalPaymentResponseLossIsConfirmedByStatusQuery(t *testing.T) {
	fault := &faultController{CaseID: "payment-response-loss", Seed: 43, Kind: "payment_transient", RatePercent: 100, Repeat: 1, Configured: true}
	adapter := &localPayment{fault: fault, committed: map[string]payment.PaymentOutcome{}}
	intent := payment.PaymentIntent{IntentID: "pi-response-loss", AmountMinor: 1000, Currency: "USDC"}

	beforeSubmit, err := adapter.Query(context.Background(), adapters.PaymentQuery{Intent: intent})
	if err != nil || beforeSubmit.Status != payment.OutcomeUnknown {
		t.Fatalf("unsubmitted mock transaction must not appear confirmed: outcome=%#v err=%v", beforeSubmit, err)
	}

	submitted, err := adapter.Submit(context.Background(), adapters.PaymentSubmitRequest{Intent: intent})
	if !errors.Is(err, errLocalPaymentResponseLost) || submitted.Status != payment.OutcomeUnknown || submitted.IntentID != intent.IntentID {
		t.Fatalf("expected committed payment with lost submit response, got outcome=%#v err=%v", submitted, err)
	}
	if fault.status().InjectionCount != 1 {
		t.Fatalf("expected exactly one injected submit response loss, got %#v", fault.status())
	}

	queried, err := adapter.Query(context.Background(), adapters.PaymentQuery{Intent: intent})
	if err != nil || queried.Status != payment.OutcomeConfirmed || queried.IntentID != intent.IntentID || queried.TxID != "local-tx:"+intent.IntentID || queried.AmountMinor != intent.AmountMinor || queried.Currency != intent.Currency {
		t.Fatalf("status query did not reconcile the same committed payment: outcome=%#v err=%v", queried, err)
	}
}

func TestPairedDeliveryInvalidStopBaselineAndRuleRecovery(t *testing.T) {
	for _, test := range []struct {
		name, llmMode, recoveryProvider, observedTerminal string
		gradePassed                                       bool
	}{
		{name: "stop baseline", llmMode: "stop", recoveryProvider: "baseline_stop", observedTerminal: "FAILED", gradePassed: false},
		{name: "rule recovery", llmMode: "rule", recoveryProvider: "rule", observedTerminal: "FULFILLED", gradePassed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, err := New(context.Background(), Config{MemoryMode: "off", LLMMode: test.llmMode})
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			server := httptest.NewServer(root.Handler)
			defer server.Close()

			paymentIntents, settlements := 1, 1
			request, err := json.Marshal(map[string]any{
				"request_id":        "paired-delivery-invalid-" + test.llmMode,
				"parent_session_id": "paired-delivery-invalid-v1",
				"requester_did":     "did:agent:agentpay-benchmark",
				"acquisition_goal":  map[string]any{"task_type": "agentpay-query", "description": "Complete a local query after an invalid first delivery", "semantic_constraints": []any{map[string]string{"key": "region", "value": "local"}}},
				"input":             map[string]string{"uri": "local://agentpay/paired-delivery-invalid", "content_type": "text/plain"},
				"constraints":       map[string]any{"budget_limit_minor": 1000000, "currency": "USDC", "deadline_at": time.Now().UTC().Add(2 * time.Minute).Format(time.RFC3339), "supported_protocol_versions": []string{"x402-v1"}, "max_total_attempts": 20, "max_payment_attempts": 3, "max_delivery_attempts": 3, "allow_cross_merchant_switch": false, "require_parent_confirmation_above_minor": 0},
				"expected_output":   map[string]string{"schema": "text", "content_type": "text/plain"},
				"validator":         map[string]string{"kind": "builtin", "name": "non-empty", "version": "v1"},
			})
			if err != nil {
				t.Fatal(err)
			}
			scenario := eval.Scenario{
				CaseID: "paired-delivery-invalid-" + test.llmMode, Seed: 6110, Ingress: eval.IngressMCP,
				MemoryMode: "off", RecoveryProvider: test.recoveryProvider, Path: "recovery", Suite: "agentpay_paired_recovery_v1",
				Environment: "live_local", TaskID: "delivery-invalid", TrialIndex: 1,
				ExpectedTerminal: "FULFILLED", ExpectedMerchantDID: "did:merchant:agentpay-lowcost", ExpectedCapabilityID: "agentpay-lowcost",
				ExpectedPaymentIntents: &paymentIntents, ExpectedSettlementCount: &settlements,
				RequirePayment: true, RequireRecovery: true, MustNotCreateSecondPayment: true,
				Failure: observability.FailureInjection{Kind: "delivery_invalid", RatePercent: 100, Repeat: 1}, Request: request,
			}
			client := eval.Client{BaseURL: server.URL, InjectorURL: server.URL, PollInterval: 5 * time.Millisecond, PollTimeout: 3 * time.Second}
			results, err := client.Run(context.Background(), []eval.Scenario{scenario})
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 {
				t.Fatalf("got %d results, want one", len(results))
			}
			result := results[0]
			if result.CollectionError != "" {
				t.Fatalf("unexpected collection error: %s", result.CollectionError)
			}
			if got := string(result.Trace.Episode.State); got != test.observedTerminal {
				t.Fatalf("terminal state = %q, want %q", got, test.observedTerminal)
			}
			if result.Grade.Passed != test.gradePassed {
				t.Fatalf("task grader passed=%t, want %t: %+v", result.Grade.Passed, test.gradePassed, result.Grade.Assertions)
			}
			if result.Trace.RuntimeVariant.RecoveryProvider != test.recoveryProvider {
				t.Fatalf("observed recovery provider = %q, want %q", result.Trace.RuntimeVariant.RecoveryProvider, test.recoveryProvider)
			}
			if len(result.Trace.PaymentIntents) != 1 {
				t.Fatalf("PaymentIntent count = %d, want 1", len(result.Trace.PaymentIntents))
			}
			settled := 0
			for _, entry := range result.Trace.Ledger {
				if entry.Type == ledger.EntryPaymentSettled {
					settled++
				}
			}
			if settled != 1 {
				t.Fatalf("settlement count = %d, want 1", settled)
			}
			if !result.Failure.Triggered || result.Failure.InjectionCount != 1 {
				t.Fatalf("fault was not observed exactly once: %+v", result.Failure)
			}
		})
	}
}
