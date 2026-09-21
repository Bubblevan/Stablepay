package observability

import (
	"fmt"
	"strings"

	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/invocation"
	"github.com/stablepay/commerce-runtime/internal/ledger"
	"github.com/stablepay/commerce-runtime/internal/payment"
)

// GradeEpisode is deterministic and evidence-backed. It is deliberately not
// an LLM judge: correctness comes from persisted Runtime facts and task
// expectations supplied by the dataset.
func GradeEpisode(result EpisodeResult) GradeResult {
	assertions := make([]GradeAssertion, 0, 18)
	state := result.TraceState()
	expected := strings.TrimSpace(result.ExpectedTerminal)
	if expected == "" {
		expected = string(episode.StateFulfilled)
	}
	terminalPassed := episode.IsTerminal(state) && (expected == "terminal" || string(state) == expected)
	assertions = append(assertions, GradeAssertion{Name: "terminal_expected", Required: true, Passed: terminalPassed, Expected: expected, Observed: string(state), EvidenceRefs: episodeEvidence(result)})

	fulfilled := expected == string(episode.StateFulfilled)
	if fulfilled {
		artifactPresent := result.Trace.Artifact != nil && strings.TrimSpace(result.Trace.Artifact.DeliveryID) != "" && strings.TrimSpace(result.Trace.Artifact.PayloadHash) != ""
		assertions = append(assertions, GradeAssertion{Name: "fulfilled_artifact_present", Required: true, Passed: artifactPresent, Expected: "artifact with delivery_id and payload_hash", Observed: artifactObserved(result.Trace.Artifact)})
		validationPresent := result.Trace.Validation != nil
		validationValid := validationPresent && result.Trace.Validation.Valid
		assertions = append(assertions, GradeAssertion{Name: "fulfilled_validation_present", Required: true, Passed: validationPresent, Expected: "ValidationEvidence", Observed: validationObserved(result.Trace.Validation)})
		assertions = append(assertions, GradeAssertion{Name: "validation_valid", Required: true, Passed: validationValid, Expected: "true", Observed: validationObserved(result.Trace.Validation)})
	} else if result.Trace.Validation != nil {
		assertions = append(assertions, GradeAssertion{Name: "validation_valid", Required: false, Passed: result.Trace.Validation.Valid, Expected: "not_applicable_for_non_fulfilled", Observed: validationObserved(result.Trace.Validation)})
	}

	duplicate := duplicateSettlements(result.Trace.Ledger)
	assertions = append(assertions, GradeAssertion{Name: "duplicate_settlement_count", Required: true, Passed: duplicate == 0, Expected: "0", Observed: fmt.Sprint(duplicate)})
	unsafe := outcomeUnsafeSideEffects(result.Trace)
	assertions = append(assertions, GradeAssertion{Name: "unsafe_side_effect_count", Required: true, Passed: unsafe == 0, Expected: "0", Observed: fmt.Sprint(unsafe)})
	if len(result.Trace.PaymentTransportTraces) > 0 {
		consistent, observed := exactRedeliveryIdentity(result.Trace.PaymentTransportTraces)
		assertions = append(assertions, GradeAssertion{Name: "exact_redelivery_identity", Required: true, Passed: consistent, Expected: "same payment_intent_id/idempotency_key/request_fingerprint", Observed: observed})
	}

	if result.ExpectedSettlementCount != nil {
		observed := settlementCount(result.Trace.Ledger)
		assertions = append(assertions, GradeAssertion{Name: "expected_settlement_count", Required: true, Passed: observed == *result.ExpectedSettlementCount, Expected: fmt.Sprint(*result.ExpectedSettlementCount), Observed: fmt.Sprint(observed)})
	}
	if result.ExpectedPaymentIntents != nil {
		observed := len(result.Trace.PaymentIntents)
		assertions = append(assertions, GradeAssertion{Name: "expected_payment_intents", Required: true, Passed: observed == *result.ExpectedPaymentIntents, Expected: fmt.Sprint(*result.ExpectedPaymentIntents), Observed: fmt.Sprint(observed)})
	}
	if result.RequirePayment || (fulfilled && result.Trace.Artifact != nil && strings.TrimSpace(result.Trace.Artifact.PaymentIntentID) != "") {
		confirmed := confirmedPaymentIntents(result.Trace.PaymentIntents)
		settled := settlementCount(result.Trace.Ledger)
		assertions = append(assertions, GradeAssertion{Name: "payment_confirmed", Required: true, Passed: confirmed > 0, Expected: ">=1 confirmed PaymentIntent", Observed: fmt.Sprint(confirmed)})
		assertions = append(assertions, GradeAssertion{Name: "payment_settled", Required: true, Passed: settled > 0, Expected: ">=1 PAYMENT_SETTLED", Observed: fmt.Sprint(settled)})
	}

	budgetPassed, budgetObserved := budgetConsistency(result)
	assertions = append(assertions, GradeAssertion{Name: "budget_ledger_consistency", Required: budgetObserved != "not_applicable", Passed: budgetPassed, Expected: "ledger projection equals episode budget or not_applicable", Observed: budgetObserved})
	if budgetObserved == "not_applicable" {
		assertions[len(assertions)-1].Required = false
	}
	entitlementPassed, entitlementObserved := entitlementTxMatches(result)
	assertions = append(assertions, GradeAssertion{Name: "entitlement_tx_matches", Required: entitlementObserved != "not_applicable", Passed: entitlementPassed, Expected: "artifact entitlement identity matches confirmed payment", Observed: entitlementObserved})
	if entitlementObserved == "not_applicable" {
		assertions[len(assertions)-1].Required = false
	}
	if result.RequireRecovery {
		recovered := isRecovery(result.Trace)
		assertions = append(assertions, GradeAssertion{Name: "must_enter_recovery", Required: true, Passed: recovered, Expected: "true", Observed: fmt.Sprint(recovered)})
	}
	if result.MustNotCreateSecondPayment {
		observed := len(result.Trace.PaymentIntents)
		assertions = append(assertions, GradeAssertion{Name: "must_not_create_second_payment", Required: true, Passed: observed <= 1, Expected: "<=1", Observed: fmt.Sprint(observed)})
	}

	passed := true
	for _, assertion := range assertions {
		if assertion.Required && !assertion.Passed {
			passed = false
		}
	}
	return GradeResult{Passed: passed, Assertions: assertions}
}

func ValidBenchmarkTrial(result EpisodeResult) bool {
	if strings.TrimSpace(result.CollectionError) != "" || result.Trace.Episode == nil {
		return false
	}
	if result.Mode == ModeLive {
		if err := result.Trace.RuntimeVariant.Validate(); err != nil {
			return false
		}
	}
	grade := result.Grade
	if len(grade.Assertions) == 0 {
		grade = GradeEpisode(result)
	}
	return len(grade.Assertions) > 0
}

func artifactObserved(value *ArtifactSnapshot) string {
	if value == nil {
		return "missing"
	}
	return fmt.Sprintf("delivery_id=%s payload_hash=%s", value.DeliveryID, value.PayloadHash)
}

func validationObserved(value *invocation.ValidationEvidence) string {
	if value == nil {
		return "missing"
	}
	return fmt.Sprintf("valid=%t reason=%s", value.Valid, value.ReasonCode)
}

func outcomeUnsafeSideEffects(value EpisodeTrace) int {
	count := 0
	for _, outcome := range canonicalDecisionOutcomes(value) {
		if outcome != nil && outcome.ReachedRuntimeGuard && !outcome.GuardAccepted && sideEffectDelta(outcome).AnyIncrease() {
			count++
		}
	}
	return count
}

func (r EpisodeResult) TraceState() episode.State {
	if r.Trace.Episode == nil {
		return ""
	}
	return r.Trace.Episode.State
}

func episodeEvidence(result EpisodeResult) []string {
	if result.Trace.Episode == nil {
		return nil
	}
	refs := make([]string, 0, len(result.Trace.Episode.ValidationEvidenceRefs)+1)
	refs = append(refs, result.Trace.Episode.EpisodeID)
	refs = append(refs, result.Trace.Episode.ValidationEvidenceRefs...)
	return refs
}

func settlementCount(entries []*ledger.LedgerEntry) int {
	count := 0
	for _, entry := range entries {
		if entry != nil && entry.Type == ledger.EntryPaymentSettled {
			count++
		}
	}
	return count
}

func confirmedPaymentIntents(intents []*payment.PaymentIntent) int {
	count := 0
	for _, intent := range intents {
		if intent != nil && intent.Status == payment.IntentConfirmed {
			count++
		}
	}
	return count
}

func budgetConsistency(result EpisodeResult) (bool, string) {
	if result.Trace.Episode == nil {
		return true, "not_applicable"
	}
	budget := result.Trace.Episode.Budget
	if len(result.Trace.Ledger) == 0 {
		if result.RequirePayment || (result.Trace.Artifact != nil && result.Trace.Artifact.PaymentIntentID != "") {
			return false, "ledger_missing_for_paid_episode"
		}
		return true, "not_applicable"
	}
	projection, err := ledger.BuildProjection(budget.Currency, budget.BudgetLimitMinor, budget.RefundReusable, result.Trace.Ledger)
	if err != nil {
		return false, err.Error()
	}
	observed := projection.ToEpisodeBudget()
	if observed != budget {
		return false, fmt.Sprintf("expected=%+v observed=%+v", budget, observed)
	}
	return true, fmt.Sprintf("expected=%+v observed=%+v", budget, observed)
}

func entitlementTxMatches(result EpisodeResult) (bool, string) {
	expected := strings.TrimSpace(result.ExpectedEntitlementTxID)
	artifact := result.Trace.Artifact
	if expected == "" && (artifact == nil || strings.TrimSpace(artifact.PaymentIntentID) == "") {
		return true, "not_applicable"
	}
	if artifact == nil {
		return false, "artifact missing"
	}
	intentID := strings.TrimSpace(artifact.PaymentIntentID)
	if intentID == "" {
		return false, "artifact has no payment_intent_id"
	}
	var intent *payment.PaymentIntent
	for _, candidate := range result.Trace.PaymentIntents {
		if candidate != nil && candidate.IntentID == intentID {
			intent = candidate
			break
		}
	}
	if intent == nil || intent.Status != payment.IntentConfirmed || strings.TrimSpace(intent.TxID) == "" {
		return false, "confirmed payment intent missing"
	}
	refTx, ok := entitlementTxID(artifact.EntitlementRef)
	if !ok || refTx != intent.TxID {
		return false, fmt.Sprintf("entitlement_ref=%s payment_tx=%s", artifact.EntitlementRef, intent.TxID)
	}
	if expected != "" && (expected != intent.TxID || expected != refTx) {
		return false, fmt.Sprintf("expected=%s payment_tx=%s entitlement_tx=%s", expected, intent.TxID, refTx)
	}
	return true, "matched:" + intent.TxID
}

func entitlementTxID(ref string) (string, bool) {
	ref = strings.TrimSpace(ref)
	if !strings.HasPrefix(ref, "verification:") {
		return "", false
	}
	value := strings.TrimSpace(strings.TrimPrefix(ref, "verification:"))
	return value, value != ""
}

func exactRedeliveryIdentity(traces []*payment.PaymentTransportTrace) (bool, string) {
	var baseline *payment.PaymentTransportTrace
	count := 0
	for _, value := range traces {
		if value == nil || value.Kind != "EXACT_REDELIVERY" {
			continue
		}
		count++
		if baseline == nil {
			baseline = value
			continue
		}
		if value.PaymentIntentID != baseline.PaymentIntentID || value.IdempotencyKey != baseline.IdempotencyKey || value.RequestFingerprint != baseline.RequestFingerprint {
			return false, "identity_mismatch"
		}
	}
	if count == 0 {
		return true, "not_applicable"
	}
	return true, fmt.Sprintf("exact_redeliveries=%d identity=%s", count, baseline.PaymentIntentID)
}
