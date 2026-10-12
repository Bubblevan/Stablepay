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

func TestRecoverExplicitRequiredFormatRequiresOneCatalogMatchInExplicitClause(t *testing.T) {
	taxonomy := Taxonomy{
		Formats:       []string{"冷萃包", "挂耳装"},
		FormatAliases: map[string][]string{"挂耳装": {"挂耳咖啡袋"}},
	}
	tests := []struct {
		name   string
		prompt string
		want   string
		ok     bool
	}{
		{
			name:   "explicit canonical value",
			prompt: "请帮我买咖啡，规格必须是冷萃包，风格偏好花果香。",
			want:   "冷萃包",
			ok:     true,
		},
		{
			name:   "explicit alias maps to canonical value",
			prompt: "format must be 挂耳咖啡袋, please check stock",
			want:   "挂耳装",
			ok:     true,
		},
		{
			name:   "mention without explicit format clause is insufficient",
			prompt: "请推荐冷萃包，规格不限。",
			ok:     false,
		},
		{
			name:   "ambiguous alternatives are not inferred",
			prompt: "规格必须是冷萃包或挂耳装。",
			ok:     false,
		},
		{
			name:   "conflicting explicit clauses are not inferred",
			prompt: "规格必须是冷萃包，但格式必须为挂耳装。",
			ok:     false,
		},
		{
			name:   "affirmative and rejected mentions conflict",
			prompt: "规格必须是冷萃包，但我不要冷萃包。",
			ok:     false,
		},
		{
			name:   "rejected alternative does not block a different explicit format",
			prompt: "不要冷萃包；规格必须是挂耳装。",
			want:   "挂耳装",
			ok:     true,
		},
		{
			name:   "negated clause does not match an affirmative marker",
			prompt: "不要冷萃包；规格未知。",
			ok:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := recoverExplicitRequiredFormat(tt.prompt, taxonomy)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("recoverExplicitRequiredFormat() = (%q, %v), want (%q, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestRecoveredExplicitFormatDoesNotRequireOptionalStyle(t *testing.T) {
	taxonomy := Taxonomy{
		Regions:    []string{"CN-SOUTH"},
		Categories: []string{"coffee"},
		Items:      []string{"哥伦比亚"},
		Formats:    []string{"冷萃包"},
		Styles:     []string{"花果香"},
	}
	prompt := "请买哥伦比亚咖啡，规格必须是冷萃包。"
	format, ok := recoverExplicitRequiredFormat(prompt, taxonomy)
	if !ok || format != "冷萃包" {
		t.Fatalf("explicit required format recovery=(%q, %t), want cold-brew bag", format, ok)
	}
	intent := normalizeIntentClarification(Intent{Category: "coffee", Item: "哥伦比亚", RequiredFormat: format,
		Region: "CN-SOUTH", MaxPriceMinor: 800, AllowSubstitution: true, ConfirmationNeeded: true})
	if intent.ClarificationRequired || intent.RequiredFormat != "冷萃包" || intent.PreferredStyle != "" ||
		intent.ClarificationQuestion != "" || !intent.ConfirmationNeeded {
		t.Fatalf("explicit format recovery must produce a usable plan without asking for an unstated soft preference: %+v", intent)
	}
}

func TestReplayChallengeBatchResultsRevalidatesRawOutputWithoutMutatingIt(t *testing.T) {
	taxonomy := Taxonomy{Regions: []string{"CN-EAST"}, Categories: []string{"coffee"},
		Items: []string{"beans"}, Formats: []string{"cold-brew-bag"}, Styles: []string{"floral"}}
	requests := []BatchIntentRequest{
		{TaskID: "explicit-format", Prompt: "Buy beans; format must be cold-brew-bag.",
			DefaultRegion: "CN-EAST", BuyerBudgetMinor: 2000},
		{TaskID: "missing-format", Prompt: "Buy beans in the east, under 20 USDC.",
			DefaultRegion: "CN-EAST", BuyerBudgetMinor: 2000},
	}
	firstRaw := &Intent{Category: "coffee", Item: "beans", PreferredStyle: "floral",
		Region: "CN-EAST", MaxPriceMinor: 1800, AllowSubstitution: true}
	secondRaw := &Intent{Category: "coffee", Item: "beans", PreferredStyle: "floral",
		Region: "CN-EAST", MaxPriceMinor: 1800, AllowSubstitution: true}
	previous := []BatchIntentResult{
		{TaskID: "missing-format", RawIntent: secondRaw, AllowSubstitutionSpecified: true},
		{TaskID: "explicit-format", RawIntent: firstRaw, AllowSubstitutionSpecified: true},
	}

	replayed, err := ReplayChallengeBatchResults(requests, previous, taxonomy)
	if err != nil {
		t.Fatalf("replay stored parse results: %v", err)
	}
	if len(replayed) != 2 || replayed[0].TaskID != "explicit-format" || replayed[1].TaskID != "missing-format" {
		t.Fatalf("replay did not restore request order: %+v", replayed)
	}
	if replayed[0].Error != "" || replayed[0].Intent.RequiredFormat != "cold-brew-bag" ||
		len(replayed[0].CanonicalizedFields) != 1 || replayed[0].CanonicalizedFields[0] != "required_format_from_explicit_request" {
		t.Fatalf("explicit slot was not deterministically recovered: %+v", replayed[0])
	}
	if replayed[0].Intent.searchText != requests[0].Prompt || replayed[1].Intent.searchText != requests[1].Prompt {
		t.Fatalf("replay must restore each frozen user prompt for discovery ranking: first=%q second=%q",
			replayed[0].Intent.searchText, replayed[1].Intent.searchText)
	}
	if replayed[0].RawIntent.RequiredFormat != "" || firstRaw.RequiredFormat != "" {
		t.Fatalf("replay mutated the original model output: %+v", replayed[0].RawIntent)
	}
	if replayed[1].Error != "" || !replayed[1].Intent.ClarificationRequired ||
		replayed[1].Intent.RequiredFormat != "" || replayed[1].Intent.ClarificationQuestion != "需要哪种规格或形式？" {
		t.Fatalf("genuinely missing format did not remain a clarification: %+v", replayed[1])
	}
}
