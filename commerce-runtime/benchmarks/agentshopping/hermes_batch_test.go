package agentshopping

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestDecodeHermesIntentBatchStrictJSON(t *testing.T) {
	output := []byte("[{\"task_id\":\"shop-0001\",\"category\":\"coffee\",\"item\":\"beans\",\"required_format\":\"bag\",\"preferred_style\":\"floral\",\"region\":\"CN-EAST\",\"max_price_minor\":1200,\"allow_substitution\":true}]")
	rows, err := decodeHermesIntentBatch(output)
	if err != nil {
		t.Fatalf("decode batch: %v", err)
	}
	if len(rows) != 1 || rows[0].TaskID != "shop-0001" || rows[0].AllowSubstitution == nil || !*rows[0].AllowSubstitution {
		t.Fatalf("unexpected decoded rows: %+v", rows)
	}
	if _, err := decodeHermesIntentBatch([]byte("[{\"task_id\":\"x\",\"unexpected\":true}]")); err == nil {
		t.Fatal("unknown response fields must fail closed")
	}
	if _, err := decodeHermesIntentBatch([]byte("[] trailing")); err == nil {
		t.Fatal("trailing output must fail closed")
	}
}

func TestDecodeHermesIntentBatchExtractsOneArrayFromPreamble(t *testing.T) {
	output := []byte("A short preamble before the requested output.\n" +
		`[{"task_id":"shop-0002","category":"coffee","item":"beans","required_format":"bag","preferred_style":"","region":"CN-EAST","max_price_minor":1200,"allow_substitution":true}]`)
	rows, err := decodeHermesIntentBatch(output)
	if err != nil {
		t.Fatalf("decode array after preamble: %v", err)
	}
	if len(rows) != 1 || rows[0].TaskID != "shop-0002" {
		t.Fatalf("unexpected decoded rows: %+v", rows)
	}
	if _, err := decodeHermesIntentBatch([]byte("no JSON array here")); !errors.Is(err, errMalformedHermesIntentJSON) {
		t.Fatalf("malformed model output should be classified for per-batch accounting: %v", err)
	}
}

func TestDecodeHermesStreamJSONReadsTerminalTextAndExactTokens(t *testing.T) {
	response := `[{"task_id":"shop-0001"}]`
	output := []byte("{\"type\":\"system\",\"subtype\":\"init\"}\n" +
		"{\"type\":\"text\",\"text\":\"partial\"}\n" +
		"{\"type\":\"result\",\"exit_code\":0,\"text\":" + strconv.Quote(response) +
		",\"tokens\":{\"input\":321,\"output\":17,\"total\":338}}\n")
	text, usage, exitCode, err := decodeHermesStreamJSON(output)
	if err != nil {
		t.Fatalf("decode stream-json: %v", err)
	}
	if string(text) != response || exitCode != 0 {
		t.Fatalf("unexpected result: text=%q exit=%d", text, exitCode)
	}
	if usage.UsageStatus != "AVAILABLE" || usage.InputTokens != 321 || usage.OutputTokens != 17 {
		t.Fatalf("actual token counts not preserved: %+v", usage)
	}
	if usage.APICallsStatus != "TOKEN_METRIC_UNAVAILABLE" || usage.APICalls != 0 {
		t.Fatalf("unreported API call count must not be estimated: %+v", usage)
	}
}

func TestDecodeHermesStreamJSONRejectsToolsAndMissingTerminal(t *testing.T) {
	toolResult := []byte("{\"type\":\"tool_use\"}\n{\"type\":\"result\",\"exit_code\":0,\"text\":\"[]\",\"tokens\":{\"input\":10,\"output\":1}}\n")
	_, usage, _, err := decodeHermesStreamJSON(toolResult)
	if !errors.Is(err, ErrHermesToolCallAttempted) || usage.InputTokens != 10 {
		t.Fatalf("tool call should fail closed but retain actual terminal usage: usage=%+v err=%v", usage, err)
	}
	if _, _, _, err := decodeHermesStreamJSON([]byte("{\"type\":\"system\"}\n")); err == nil {
		t.Fatal("stream without terminal result must fail")
	}
}

func TestValidateParsedBatchIntentEnforcesTaxonomyAndBuyerBudget(t *testing.T) {
	allow := true
	row := hermesBatchIntentResponse{TaskID: "shop-0001", Category: "coffee", Item: "beans", RequiredFormat: "bag",
		PreferredStyle: "floral", Region: "华南", MaxPriceMinor: 1200, AllowSubstitution: &allow}
	taxonomy := Taxonomy{Regions: []string{"CN-EAST", "CN-SOUTH"}, Categories: []string{"coffee"},
		Items: []string{"beans"}, Formats: []string{"bag"}, Styles: []string{"floral"}}
	intent, _, err := validateParsedBatchIntent(row, "CN-EAST", 15000, taxonomy)
	if err != nil {
		t.Fatalf("valid parsed intent rejected: %v", err)
	}
	if intent.Region != "CN-SOUTH" || intent.MaxPriceMinor != 1200 || !intent.AllowSubstitution || !intent.ConfirmationNeeded {
		t.Fatalf("unexpected parsed intent: %+v", intent)
	}
	row.MaxPriceMinor = 15001
	if _, _, err := validateParsedBatchIntent(row, "CN-EAST", 15000, taxonomy); err == nil {
		t.Fatal("budget above the buyer cap must be rejected")
	}
}

func TestValidateParsedBatchIntentAllowsUnstatedOptionalStyle(t *testing.T) {
	allow := true
	row := hermesBatchIntentResponse{TaskID: "shop-no-style", Category: "coffee", Item: "beans", RequiredFormat: "bag",
		Region: "CN-EAST", MaxPriceMinor: 1200, AllowSubstitution: &allow}
	taxonomy := Taxonomy{Regions: []string{"CN-EAST"}, Categories: []string{"coffee"},
		Items: []string{"beans"}, Formats: []string{"bag"}, Styles: []string{"floral"}}
	intent, _, err := validateParsedBatchIntent(row, "CN-EAST", 1500, taxonomy)
	if err != nil {
		t.Fatalf("an unstated optional style must not invalidate an otherwise complete intent: %v", err)
	}
	if intent.PreferredStyle != "" || intent.ClarificationRequired {
		t.Fatalf("unstated optional style was invented or treated as blocking: %+v", intent)
	}
}

func TestValidateParsedBatchIntentChecksHardAttributesAgainstCategoryWhitelist(t *testing.T) {
	allow := false
	row := hermesBatchIntentResponse{TaskID: "attr-1", Category: "coffee", Item: "beans", RequiredFormat: "bag",
		PreferredStyle: "floral", RequiredAttributes: map[string]string{"roast_level": "light"},
		Region: "CN-EAST", MaxPriceMinor: 1200, AllowSubstitution: &allow}
	taxonomy := Taxonomy{Regions: []string{"CN-EAST"}, Categories: []string{"coffee"}, Items: []string{"beans"},
		Formats: []string{"bag"}, Styles: []string{"floral"},
		CategoryAttributeValues: map[string]map[string][]string{"coffee": {"roast_level": {"light", "medium"}}}}
	if _, _, err := validateParsedBatchIntent(row, "CN-EAST", 1500, taxonomy); err != nil {
		t.Fatalf("allowed hard attribute rejected: %v", err)
	}
	row.RequiredAttributes["roast_level"] = "extra-dark"
	if _, _, err := validateParsedBatchIntent(row, "CN-EAST", 1500, taxonomy); err == nil {
		t.Fatal("non-canonical hard attribute must fail closed")
	}
	row.RequiredAttributes = map[string]string{"unknown_key": "light"}
	if _, _, err := validateParsedBatchIntent(row, "CN-EAST", 1500, taxonomy); err == nil {
		t.Fatal("unknown attribute key must fail closed")
	}
}

func TestNormalizePartialChallengeIntentRequiresRealMissingFieldsAndKnownValues(t *testing.T) {
	taxonomy := Taxonomy{Regions: []string{"CN-EAST"}, Categories: []string{"coffee"},
		Items: []string{"beans"}, Formats: []string{"bag"}, Styles: []string{"floral"}}
	request := BatchIntentRequest{TaskID: "ambiguous-1", DefaultRegion: "CN-EAST", BuyerBudgetMinor: 2000}
	partial := Intent{Category: "coffee", RequiredFormat: "bag", PreferredStyle: "floral", MaxPriceMinor: 0}
	intent, canonicalized, err := normalizePartialChallengeIntent(partial, request, taxonomy)
	if err != nil {
		t.Fatalf("valid partial clarification rejected: %v", err)
	}
	if !intent.ClarificationRequired || intent.Item != "" || intent.Region != "CN-EAST" || intent.MaxPriceMinor != 2000 ||
		!intent.ConfirmationNeeded || strings.TrimSpace(intent.ClarificationQuestion) == "" || len(canonicalized) != 0 {
		t.Fatalf("partial clarification was not normalized safely: %+v canonicalized=%v", intent, canonicalized)
	}

	partial.Item = "unknown-product"
	if _, _, err := normalizePartialChallengeIntent(partial, request, taxonomy); err == nil {
		t.Fatal("unknown non-empty field must not be converted into a clarification")
	}
	partial.Item = "beans"
	if _, _, err := normalizePartialChallengeIntent(partial, request, taxonomy); err == nil {
		t.Fatal("a complete intent must not be relabeled as a clarification")
	}
}

func TestValidateParsedBatchIntentNormalizesOnlyExplicitCatalogAliases(t *testing.T) {
	allow := true
	row := hermesBatchIntentResponse{TaskID: "shop-0111", Category: "books", Item: "安全编码训练",
		RequiredFormat: "录播课", PreferredStyle: "新手友好", Region: "CN-SOUTH",
		MaxPriceMinor: 3390, AllowSubstitution: &allow}
	taxonomy := Taxonomy{Regions: []string{"CN-SOUTH"}, Categories: []string{"books", "education"},
		Items: []string{"安全编码训练"}, Formats: []string{"视频课"}, Styles: []string{"入门精选"},
		ItemCategories: map[string][]string{"安全编码训练": {"education"}},
		FormatAliases:  map[string][]string{"视频课": {"录播课"}},
		StyleAliases:   map[string][]string{"入门精选": {"新手友好"}}}

	raw := rawBatchIntent(row)
	intent, canonicalized, err := validateParsedBatchIntent(row, "CN-SOUTH", 5000, taxonomy)
	if err != nil {
		t.Fatalf("validate explicit catalog aliases: %v", err)
	}
	if raw.Category != "books" || raw.RequiredFormat != "录播课" || raw.PreferredStyle != "新手友好" {
		t.Fatalf("raw model output was mutated: %+v", raw)
	}
	if intent.Category != "education" || intent.RequiredFormat != "视频课" || intent.PreferredStyle != "入门精选" {
		t.Fatalf("canonical pipeline intent = %+v", intent)
	}
	want := []string{"category_from_item_taxonomy", "required_format_alias", "preferred_style_alias"}
	if !reflect.DeepEqual(canonicalized, want) {
		t.Fatalf("canonicalized fields = %#v, want %#v", canonicalized, want)
	}
}

func TestCanonicalizeExplicitAliasRejectsAmbiguousMappings(t *testing.T) {
	aliases := map[string][]string{"format-a": {"same-alias"}, "format-b": {"same-alias"}}
	if got, ok := canonicalizeExplicitAlias("same-alias", []string{"format-a", "format-b"}, aliases); ok || got != "same-alias" {
		t.Fatalf("ambiguous alias canonicalized to %q (ok=%v)", got, ok)
	}
}
