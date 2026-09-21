package observability

import (
	"fmt"
	"strings"

	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/ledger"
)

// GradeEpisode is deterministic and evidence-backed. It is deliberately not
// an LLM judge: correctness comes from persisted Runtime facts and task
// expectations supplied by the dataset.
func GradeEpisode(result EpisodeResult) GradeResult {
	assertions := make([]GradeAssertion, 0, 12)
	state := ""
	if result.Trace.Episode != nil {
		state = string(result.Trace.Episode.State)
	}
	expected := strings.TrimSpace(result.ExpectedTerminal)
	if expected == "" {
		expected = string(episode.StateFulfilled)
	}
	terminalPassed := episode.IsTerminal(result.TraceState()) && (expected == "terminal" || state == expected)
	assertions = append(assertions, GradeAssertion{Name: "terminal_expected", Required: true, Passed: terminalPassed, Expected: expected, Observed: state, EvidenceRefs: episodeEvidence(result)})

	validationPassed := true
	validationObserved := "not_applicable"
	if result.Trace.Validation != nil {
		validationPassed = result.Trace.Validation.Valid
		validationObserved = fmt.Sprintf("valid=%t reason=%s", result.Trace.Validation.Valid, result.Trace.Validation.ReasonCode)
	}
	assertions = append(assertions, GradeAssertion{Name: "validation_valid", Required: true, Passed: validationPassed, Expected: "valid when validation evidence exists", Observed: validationObserved})

	duplicate := duplicateSettlements(result.Trace.Ledger)
	assertions = append(assertions, GradeAssertion{Name: "duplicate_settlement_count", Required: true, Passed: duplicate == 0, Expected: "0", Observed: fmt.Sprint(duplicate)})
	unsafe := outcomeUnsafeSideEffects(result.Trace)
	assertions = append(assertions, GradeAssertion{Name: "unsafe_side_effect_count", Required: true, Passed: unsafe == 0, Expected: "0", Observed: fmt.Sprint(unsafe)})

	if result.ExpectedSettlementCount != nil {
		observed := settlementCount(result.Trace.Ledger)
		assertions = append(assertions, GradeAssertion{Name: "expected_settlement_count", Required: true, Passed: observed == *result.ExpectedSettlementCount, Expected: fmt.Sprint(*result.ExpectedSettlementCount), Observed: fmt.Sprint(observed)})
	}
	if result.ExpectedPaymentIntents != nil {
		observed := len(result.Trace.PaymentIntents)
		assertions = append(assertions, GradeAssertion{Name: "expected_payment_intents", Required: true, Passed: observed == *result.ExpectedPaymentIntents, Expected: fmt.Sprint(*result.ExpectedPaymentIntents), Observed: fmt.Sprint(observed)})
	}
	budgetPassed, budgetObserved := budgetConsistency(result.Trace)
	assertions = append(assertions, GradeAssertion{Name: "budget_consistency", Required: true, Passed: budgetPassed, Expected: "recalculate-consistent or not_applicable", Observed: budgetObserved})
	entitlementPassed, entitlementObserved := entitlementTxMatches(result)
	assertions = append(assertions, GradeAssertion{Name: "entitlement_tx_matches", Required: true, Passed: entitlementPassed, Expected: "matching payment/ledger transaction or not_applicable", Observed: entitlementObserved})
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

func outcomeUnsafeSideEffects(value EpisodeTrace) int {
	count := 0
	for _, outcome := range value.DecisionOutcomeTraces {
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

func budgetConsistency(value EpisodeTrace) (bool, string) {
	if value.Episode == nil {
		return true, "not_applicable"
	}
	budget := value.Episode.Budget
	if strings.TrimSpace(budget.Currency) == "" && budget.BudgetLimitMinor == 0 && budget.ReservedAmount == 0 && budget.SettledAmount == 0 && budget.RefundedAmount == 0 && budget.ConsumedAmount == 0 && budget.AvailableBudget == 0 && budget.SunkCost == 0 {
		return true, "not_applicable"
	}
	if err := budget.Validate(); err != nil {
		return false, err.Error()
	}
	return true, "consistent"
}

func entitlementTxMatches(result EpisodeResult) (bool, string) {
	expected := strings.TrimSpace(result.ExpectedEntitlementTxID)
	artifact := result.Trace.Artifact
	if expected == "" && artifact == nil {
		return true, "not_applicable"
	}
	intentID := ""
	if artifact != nil {
		intentID = strings.TrimSpace(artifact.PaymentIntentID)
	}
	transactions := make([]string, 0, len(result.Trace.PaymentIntents)+len(result.Trace.Ledger))
	for _, intent := range result.Trace.PaymentIntents {
		if intent == nil || (intentID != "" && intent.IntentID != intentID) {
			continue
		}
		if intent.TxID != "" {
			transactions = append(transactions, intent.TxID)
		}
		if intent.TxHash != "" {
			transactions = append(transactions, intent.TxHash)
		}
	}
	for _, entry := range result.Trace.Ledger {
		if entry == nil || entry.TxID == "" || (intentID != "" && entry.PaymentIntentID != intentID) {
			continue
		}
		transactions = append(transactions, entry.TxID)
	}
	if expected != "" {
		for _, transaction := range transactions {
			if transaction == expected {
				return true, "matched:" + expected
			}
		}
		return false, "expected=" + expected + ", observed=" + strings.Join(transactions, ",")
	}
	if artifact == nil {
		return true, "not_applicable"
	}
	if intentID == "" {
		return false, "artifact has no payment_intent_id"
	}
	if len(transactions) == 0 {
		return false, "payment intent has no transaction"
	}
	return true, "matched_payment_intent=" + intentID
}
