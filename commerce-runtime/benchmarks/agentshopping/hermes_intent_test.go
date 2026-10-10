package agentshopping

import (
	"strings"
	"testing"
)

func TestHermesMissingProductConstraintProducesClarification(t *testing.T) {
	intent, err := decodeHermesIntent([]byte(`{"category":"coffee","item":"beans","required_format":"","preferred_style":"floral","region":"CN-EAST","max_price_minor":2000,"allow_substitution":false}`))
	if err != nil {
		t.Fatalf("decode partial but valid intent: %v", err)
	}
	intent = normalizeIntentClarification(intent)
	if !intent.ClarificationRequired {
		t.Fatal("missing required format did not request clarification")
	}
	missing := missingIntentClarificationFields(intent)
	if len(missing) != 1 || missing[0] != "required_format" {
		t.Fatalf("missing fields=%v, want only required_format", missing)
	}
	if intent.ClarificationQuestion != "需要哪种规格或形式？" {
		t.Fatalf("clarification question=%q", intent.ClarificationQuestion)
	}
	if !intent.ConfirmationNeeded {
		t.Fatal("a clarified eventual purchase must still require buyer confirmation")
	}
}

func TestHermesClarificationIsStableAndDoesNotInventMissingFields(t *testing.T) {
	intent := Intent{Category: "coffee", RequiredFormat: "whole-bean", PreferredStyle: "floral",
		ClarificationRequired: true, Region: "CN-EAST", MaxPriceMinor: 2000}
	first := normalizeIntentClarification(intent)
	second := normalizeIntentClarification(first)
	if first.ClarificationQuestion != "您想购买哪种商品？" || second.ClarificationQuestion != first.ClarificationQuestion {
		t.Fatalf("clarification question changed across replay: first=%+v second=%+v", first, second)
	}
	if strings.Join(missingIntentClarificationFields(first), ",") != "item" {
		t.Fatalf("missing field set was altered: %v", missingIntentClarificationFields(first))
	}
}
