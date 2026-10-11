package agentshopping

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestGenerateChallengePlanDraftAddsSelectiveSnapshotBackedAttributes(t *testing.T) {
	products := []Product{
		{SKUID: "sim-m01-a", MerchantID: "sim-m01", Category: "coffee", Item: "耶加雪菲", Format: "单品豆",
			Style: "花果香", Currency: "USDC", PriceMinor: 500, Status: "active",
			Attributes: map[string]any{"roast_level": "light", "origin": "Ethiopia / Yirgacheffe"}},
		{SKUID: "sim-m01-b", MerchantID: "sim-m01", Category: "coffee", Item: "耶加雪菲", Format: "单品豆",
			Style: "花果香", Currency: "USDC", PriceMinor: 600, Status: "active",
			Attributes: map[string]any{"roast_level": "dark", "origin": "Colombia / Huila"}},
	}
	catalog := ImportedCatalog{Seed: 19, Products: products}
	for _, product := range products {
		for _, region := range Regions {
			catalog.Inventory = append(catalog.Inventory, Inventory{TenantID: SimulationTenantID, MerchantID: product.MerchantID,
				SKUID: product.SKUID, Region: region, OnHand: 2, Eligible: true, PriceMinor: product.PriceMinor, Revision: 1})
		}
	}

	first, err := GenerateChallengePlanDraft(catalog, 19, 1)
	if err != nil {
		t.Fatalf("GenerateChallengePlanDraft: %v", err)
	}
	second, err := GenerateChallengePlanDraft(catalog, 19, 1)
	if err != nil {
		t.Fatalf("repeat GenerateChallengePlanDraft: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("same seed/snapshot must produce the same draft: first=%+v second=%+v", first, second)
	}
	if len(first) != 1 || first[0].ExpectedDisposition != ChallengePlan || len(first[0].Intent.RequiredAttributes) == 0 || len(first[0].GoldSKUs) != 1 {
		t.Fatalf("draft is not selective and feasible: %+v", first)
	}
	if !strings.Contains(first[0].Prompt, "商品详情需要同时符合这些条件") {
		t.Fatalf("draft omitted the prompt or gold set: %+v", first[0])
	}
	if !strings.HasSuffix(first[0].Scenario, "+HARD_ATTRIBUTES") {
		t.Fatalf("selective attributes were not classified separately: %s", first[0].Scenario)
	}
	var target Product
	for _, product := range products {
		if product.SKUID == first[0].GoldSKUs[0] {
			target = product
		}
	}
	if target.SKUID == "" || candidateMatchType(target, first[0].Intent) == "" {
		t.Fatalf("gold SKU does not satisfy the generated hard attribute constraints: task=%+v product=%+v", first[0], target)
	}
	if len(first[0].RelevantSKUs) == 0 || !containsString(first[0].RelevantSKUs, target.SKUID) {
		t.Fatalf("exact target is missing from distinct relevance labels: task=%+v", first[0])
	}

	single := ImportedCatalog{Seed: 20, Products: []Product{products[0]}}
	for _, region := range Regions {
		single.Inventory = append(single.Inventory, Inventory{TenantID: SimulationTenantID, MerchantID: products[0].MerchantID,
			SKUID: products[0].SKUID, Region: region, OnHand: 1, Eligible: true, PriceMinor: products[0].PriceMinor, Revision: 1})
	}
	nonSelective, err := GenerateChallengePlanDraft(single, 20, 1)
	if err != nil {
		t.Fatalf("GenerateChallengePlanDraft with a singleton feasible candidate: %v", err)
	}
	if !strings.HasSuffix(nonSelective[0].Scenario, "+NONSELECTIVE_ATTRIBUTES") {
		t.Fatalf("singleton candidate was misclassified as selective: %s", nonSelective[0].Scenario)
	}
}

func TestExactChallengeRelevantLabelsIgnoreFeasibilityAndExcludeSubstitutes(t *testing.T) {
	products := []Product{
		{SKUID: "exact-light", Category: "coffee", Item: "耶加雪菲", Format: "单品豆", Currency: "USDC", Status: "active",
			Attributes: map[string]any{"roast_level": "light", "region": "Ethiopia"}},
		{SKUID: "exact-light-out-of-stock", Category: "coffee", Item: "耶加雪菲", Format: "单品豆", Currency: "USDC", Status: "active",
			Attributes: map[string]any{"roast_level": "light", "region": "Ethiopia"}},
		{SKUID: "exact-dark", Category: "coffee", Item: "耶加雪菲", Format: "单品豆", Currency: "USDC", Status: "active",
			Attributes: map[string]any{"roast_level": "dark", "region": "Ethiopia"}},
		{SKUID: "substitute", Category: "coffee", Item: "曼特宁", Format: "单品豆", Currency: "USDC", Status: "active",
			Attributes: map[string]any{"roast_level": "light", "region": "Indonesia"}},
		{SKUID: "inactive", Category: "coffee", Item: "耶加雪菲", Format: "单品豆", Currency: "USDC", Status: "inactive",
			Attributes: map[string]any{"roast_level": "light", "region": "Ethiopia"}},
		{SKUID: "wrong-currency", Category: "coffee", Item: "耶加雪菲", Format: "单品豆", Currency: "USD", Status: "active",
			Attributes: map[string]any{"roast_level": "light", "region": "Ethiopia"}},
	}
	intent := Intent{Category: "coffee", Item: "耶加雪菲", RequiredFormat: "单品豆", AllowSubstitution: true,
		RequiredAttributes: map[string]string{"roast_level": "light"}}
	got := exactChallengeRelevantSKUs(products, intent)
	want := []string{"exact-light", "exact-light-out-of-stock"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("exact relevance must be independent of stock, region, and price while excluding substitutes/inactive/currency mismatches: got=%v want=%v", got, want)
	}
}

func TestChallengeClarificationPatternCycleBalances75Tasks(t *testing.T) {
	base := ChallengeTask{TaskID: "challenge-seed", Scenario: "STANDARD", Prompt: "seed",
		Intent: Intent{Category: "coffee", Item: "ethiopia beans", RequiredFormat: "whole-bean", PreferredStyle: "floral",
			Region: "CN-EAST", MaxPriceMinor: 1000}}
	counts := make(map[string]int, len(challengeClarificationPatterns))
	for ordinal := 0; ordinal < 75; ordinal++ {
		task := makeChallengeClarificationTask(base, ordinal)
		pattern := strings.TrimPrefix(task.Scenario, "CLARIFY+MISSING_")
		counts[pattern]++
		if len(task.RequiredMissingFields) == 0 || len(task.RequiredMissingFields) > 2 {
			t.Fatalf("clarification pattern should omit one or two intent fields: %+v", task)
		}
		if !task.Intent.ClarificationRequired {
			t.Fatalf("clarification scenario must retain the clarification guard: %+v", task)
		}
		missingFields := make(map[string]bool, len(task.RequiredMissingFields))
		for _, field := range task.RequiredMissingFields {
			missingFields[field] = true
		}
		for _, field := range []string{"item", "required_format"} {
			if missingFields[field] == (challengeIntentField(task.Intent, field) != "") {
				t.Fatalf("clarification scenario/intent disagree on field %s: %+v", field, task)
			}
		}
	}
	if len(counts) != 3 {
		t.Fatalf("75 clarifications should cover all three decision-critical missing-field patterns: %v", counts)
	}
	for _, pattern := range challengeClarificationPatterns {
		key := strings.ToUpper(strings.Join(pattern, "_"))
		if counts[key] != 25 {
			t.Fatalf("75 clarifications should distribute 25 tasks to each pattern: %v", counts)
		}
	}
}

func TestGenerateChallengeDecisionDraftAddsNearMissNoPlanAndClarificationTasks(t *testing.T) {
	catalog := mixedDecisionFixtureCatalog()
	if _, err := GenerateChallengeDecisionDraft(catalog, 73, 6); err == nil {
		t.Fatal("mixed-decision draft must reject a count too small to represent all dispositions")
	}
	first, err := GenerateChallengeDecisionDraft(catalog, 73, 40)
	if err != nil {
		t.Fatalf("GenerateChallengeDecisionDraft: %v", err)
	}
	second, err := GenerateChallengeDecisionDraft(catalog, 73, 40)
	if err != nil {
		t.Fatalf("repeat GenerateChallengeDecisionDraft: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("same seed and snapshot must produce a deterministic mixed-decision draft")
	}
	counts := map[ChallengeDisposition]int{}
	noPlanScenarioCounts := map[string]int{}
	clarificationPatternCounts := map[string]int{}
	optionalStylePlanCount := 0
	baseDraft, err := GenerateChallengePlanDraft(catalog, 73, 40)
	if err != nil {
		t.Fatalf("GenerateChallengePlanDraft for clarification provenance: %v", err)
	}
	baseIntentByID := make(map[string]Intent, len(baseDraft))
	for _, task := range baseDraft {
		baseIntentByID[strings.TrimPrefix(task.TaskID, "challenge-")] = task.Intent
	}
	offers := challengeOffers(catalog)
	for _, task := range first {
		counts[task.ExpectedDisposition]++
		if strings.HasSuffix(task.Scenario, "+STYLE_UNSPECIFIED") {
			optionalStylePlanCount++
			if task.ExpectedDisposition != ChallengePlan || task.Intent.PreferredStyle != "" || strings.Contains(task.Prompt, "风格偏好") {
				t.Errorf("an omitted soft style preference should remain a plan task and not leak into its prompt: %+v", task)
			}
			sourceStyle := baseIntentByID[strings.TrimPrefix(task.TaskID, "challenge-")].PreferredStyle
			if sourceStyle != "" && strings.Contains(task.Prompt, sourceStyle) {
				t.Errorf("omitted source style %q still appears in prompt: %s", sourceStyle, task.Prompt)
			}
			for _, value := range task.Intent.RequiredAttributes {
				if strings.EqualFold(strings.TrimSpace(value), sourceStyle) {
					t.Errorf("omitted style leaked back as a hard attribute: %+v", task.Intent.RequiredAttributes)
				}
			}
		}
		if task.ExpectedDisposition == ChallengeNoPlan {
			noPlanScenarioCounts[task.Scenario]++
			for _, cue := range []string{"无方案", "没有可行商品", "不要提高预算", "不要下单", "若没有商品"} {
				if strings.Contains(task.Prompt, cue) {
					t.Errorf("NO_PLAN prompt leaks its expected disposition via %q: %s", cue, task.Prompt)
				}
			}
		}
		if task.ExpectedDisposition == ChallengeClarify {
			for _, cue := range []string{"只追问", "仍缺少", "必要信息", "不要自行猜测", "不要下单"} {
				if strings.Contains(task.Prompt, cue) {
					t.Errorf("CLARIFY prompt leaks its expected disposition via %q: %s", cue, task.Prompt)
				}
			}
		}
	}
	if len(first) != 40 || counts[ChallengePlan] != 28 || counts[ChallengeNoPlan] != 6 || counts[ChallengeClarify] != 6 {
		t.Fatalf("unexpected mixed-decision composition: tasks=%d counts=%v", len(first), counts)
	}
	if optionalStylePlanCount != counts[ChallengePlan]/5 {
		t.Fatalf("style-unspecified controls=%d, want one fifth of plan tasks (%d)", optionalStylePlanCount, counts[ChallengePlan]/5)
	}
	for _, scenario := range []string{"NO_PLAN+PRICE_BOUNDARY", "NO_PLAN+REGION_UNSERVED", "NO_PLAN+ATTRIBUTE_INTERSECTION_EMPTY"} {
		if noPlanScenarioCounts[scenario] != 2 {
			t.Fatalf("six diagnostic NO_PLAN tasks should be balanced across three mechanisms; %s=%d all=%v", scenario, noPlanScenarioCounts[scenario], noPlanScenarioCounts)
		}
	}
	for _, task := range first {
		switch task.ExpectedDisposition {
		case ChallengeNoPlan:
			if len(task.GoldSKUs) != 0 || len(feasibleChallengeProducts(catalog.Products, offers, task.Intent)) != 0 {
				t.Fatalf("no-plan task must have no feasible gold: %+v", task)
			}
			if strings.Contains(task.Scenario, "ATTRIBUTE_INTERSECTION_EMPTY") {
				if !strings.Contains(task.Prompt, "商品详情需要同时符合这些条件") {
					t.Errorf("attribute-conflict no-plan prompt must express constraints without revealing the answer: %s", task.Prompt)
				}
				if len(task.Intent.RequiredAttributes) != 2 {
					t.Fatalf("attribute-conflict no-plan task must require two attributes: %+v", task)
				}
				base := task.Intent
				base.RequiredAttributes = nil
				if len(feasibleChallengeProducts(catalog.Products, offers, base)) == 0 {
					t.Fatalf("attribute-conflict task must have base candidates: %+v", task)
				}
				for key, value := range task.Intent.RequiredAttributes {
					if len(feasibleChallengeProducts(catalog.Products, offers, withRequiredAttribute(base, key, value))) == 0 {
						t.Fatalf("near-miss value %s=%s must be feasible on its own: %+v", key, value, task)
					}
				}
			} else if strings.Contains(task.Scenario, "PRICE_BOUNDARY") {
				base := task.Intent
				base.RequiredAttributes = nil
				base.MaxPriceMinor = 100000
				var minimum int64 = math.MaxInt64
				for _, candidate := range catalog.Products {
					if candidate.Currency != "USDC" || candidateMatchType(candidate, base) == "" {
						continue
					}
					offer, ok := offers[inventoryKey(SimulationTenantID, candidate.MerchantID, candidate.SKUID, base.Region)]
					if ok && offer.Eligible && offer.OnHand-offer.Reserved > 0 && offer.PriceMinor < minimum {
						minimum = offer.PriceMinor
					}
				}
				if minimum == math.MaxInt64 || task.Intent.MaxPriceMinor >= minimum || minimum-task.Intent.MaxPriceMinor < minimum*5/100 {
					t.Fatalf("price-boundary no-plan must use a nontrivial budget gap below the least feasible offer: min=%d task=%+v", minimum, task)
				}
			} else if strings.Contains(task.Scenario, "REGION_UNSERVED") {
				if task.Intent.Region != "CN-WEST" || strings.Contains(task.Prompt, "华东地区") || !strings.Contains(task.Prompt, "西部地区") {
					t.Fatalf("unserved-region no-plan must update both intent and natural-language region: %+v", task)
				}
				for _, candidate := range catalog.Products {
					if candidate.Currency != "USDC" || candidateMatchType(candidate, task.Intent) == "" {
						continue
					}
					offer, ok := offers[inventoryKey(SimulationTenantID, candidate.MerchantID, candidate.SKUID, task.Intent.Region)]
					if ok && offer.Eligible && offer.OnHand-offer.Reserved > 0 {
						t.Fatalf("region-unserved task has a deliverable matching offer: task=%+v offer=%+v", task, offer)
					}
				}
			} else {
				t.Fatalf("unknown no-plan scenario: %+v", task)
			}
		case ChallengeClarify:
			if !task.Intent.ClarificationRequired || len(task.GoldSKUs) != 0 || len(task.RequiredMissingFields) < 1 || len(task.RequiredMissingFields) > 2 {
				t.Fatalf("clarification task must preserve missing fields without gold SKUs: %+v", task)
			}
			if strings.Contains(task.Prompt, "只追问") || strings.Contains(task.Prompt, "仍缺少") {
				t.Errorf("clarification prompt must not name the expected action: %s", task.Prompt)
			}
			if len(task.RequiredMissingFields) == 1 && task.RequiredMissingFields[0] == "preferred_style" {
				t.Errorf("an omitted soft preference alone must not block plan generation: %+v", task)
			}
			pattern := strings.TrimPrefix(task.Scenario, "CLARIFY+MISSING_")
			clarificationPatternCounts[pattern]++
			baseIntent, ok := baseIntentByID[strings.TrimPrefix(task.TaskID, "challenge-")]
			if !ok {
				t.Fatalf("clarification task has no corresponding complete source intent: %+v", task)
			}
			for _, field := range []string{"item", "required_format", "preferred_style"} {
				value := challengeIntentField(task.Intent, field)
				originalValue := challengeIntentField(baseIntent, field)
				if value != "" && !strings.Contains(task.Prompt, value) {
					t.Fatalf("clarification prompt omitted known %s data: value=%q prompt=%q task=%+v", field, value, task.Prompt, task)
				}
				if value == "" && strings.Contains(task.Prompt, originalValue) {
					t.Fatalf("clarification prompt leaked omitted %s data: value=%q prompt=%q task=%+v", field, originalValue, task.Prompt, task)
				}
			}
		}
	}
	if len(clarificationPatternCounts) != len(challengeClarificationPatterns) {
		t.Fatalf("clarification tasks should cover all decision-critical missing-field combinations, got %v", clarificationPatternCounts)
	}
	for _, pattern := range challengeClarificationPatterns {
		key := strings.ToUpper(strings.Join(pattern, "_"))
		want := 2 // six clarification rows cycle through three required-field patterns
		if clarificationPatternCounts[key] != want {
			t.Fatalf("small deterministic fixture clarification pattern counts=%v want %s=%d", clarificationPatternCounts, key, want)
		}
	}
	if err := ValidateChallengeTaskSet(catalog, first); err != nil {
		t.Fatalf("mixed-decision draft must pass snapshot validation: %v", err)
	}
	baseTasks, err := GenerateChallengePlanDraft(catalog, 73, 20)
	if err != nil {
		t.Fatalf("GenerateChallengePlanDraft for boundary regression: %v", err)
	}
	for _, baseTask := range baseTasks {
		if !strings.Contains(baseTask.Scenario, "BUDGET_BOUNDARY") {
			continue
		}
		nearMiss, err := makeChallengeNoPlanTask(baseTask, catalog.Products, offers, TaxonomyFromProducts(catalog.Products), challengeNoPlanAttributeIntersection)
		if err != nil {
			t.Fatalf("make near-miss from budget-boundary task: %v", err)
		}
		if nearMiss.Scenario != "NO_PLAN+ATTRIBUTE_INTERSECTION_EMPTY" || nearMiss.Intent.MaxPriceMinor <= baseTask.Intent.MaxPriceMinor {
			t.Fatalf("near-miss should raise the boundary budget and make the disjoint attributes decisive: base=%+v near=%+v", baseTask, nearMiss)
		}
		break
	}
}

func TestNaturalizeChallengeTaskPromptsIsDeterministicAndPreservesLabels(t *testing.T) {
	catalog := mixedDecisionFixtureCatalog()
	generated, err := GenerateChallengeDecisionDraft(catalog, 73, 40)
	if err != nil {
		t.Fatalf("GenerateChallengeDecisionDraft: %v", err)
	}
	before := append([]ChallengeTask(nil), generated...)
	first := append([]ChallengeTask(nil), generated...)
	second := append([]ChallengeTask(nil), generated...)
	if err := NaturalizeChallengeTaskPrompts(first, 73); err != nil {
		t.Fatalf("NaturalizeChallengeTaskPrompts: %v", err)
	}
	if err := NaturalizeChallengeTaskPrompts(second, 73); err != nil {
		t.Fatalf("repeat NaturalizeChallengeTaskPrompts: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("same task seed must produce identical natural-language prompts")
	}
	shapes := make(map[string]bool)
	for index, task := range first {
		if task.TaskID != before[index].TaskID || !reflect.DeepEqual(task.Intent, before[index].Intent) ||
			!reflect.DeepEqual(task.GoldSKUs, before[index].GoldSKUs) ||
			!reflect.DeepEqual(task.RelevantSKUs, before[index].RelevantSKUs) ||
			task.ExpectedDisposition != before[index].ExpectedDisposition {
			t.Fatalf("naturalization changed a label or structured intent: before=%+v after=%+v", before[index], task)
		}
		if task.Prompt == "" || strings.Contains(task.Prompt, "%!") || !strings.Contains(task.Prompt, FormatMinorUnits(task.Intent.MaxPriceMinor)) {
			t.Fatalf("naturalized prompt is malformed or omits its budget: %q", task.Prompt)
		}
		shape := task.Prompt
		for _, value := range []string{task.Intent.Item, task.Intent.RequiredFormat, task.Intent.Region,
			task.Intent.PreferredStyle, FormatMinorUnits(task.Intent.MaxPriceMinor)} {
			if value != "" {
				shape = strings.ReplaceAll(shape, value, "<FIELD>")
			}
		}
		shapes[shape] = true
	}
	if len(shapes) < 8 {
		t.Fatalf("naturalized suite should express several distinct request forms, got %d shapes", len(shapes))
	}
	if err := ValidateChallengeTaskSet(catalog, first); err != nil {
		t.Fatalf("naturalization must preserve snapshot-valid labels: %v", err)
	}
}

func TestGenerateChallengeDecisionDraftRedistributesUnsupportedScenarioQuota(t *testing.T) {
	catalog := mixedDecisionFixtureCatalog()
	offers := challengeOffers(catalog)
	for _, product := range catalog.Products {
		key := inventoryKey(SimulationTenantID, product.MerchantID, product.SKUID, "CN-WEST")
		if _, exists := offers[key]; exists {
			continue
		}
		catalog.Inventory = append(catalog.Inventory, Inventory{TenantID: SimulationTenantID, MerchantID: product.MerchantID,
			SKUID: product.SKUID, Region: "CN-WEST", OnHand: 3, Eligible: true, PriceMinor: product.PriceMinor, Revision: 1})
		offers[key] = catalog.Inventory[len(catalog.Inventory)-1]
	}
	tasks, err := GenerateChallengeDecisionDraft(catalog, 73, 40)
	if err != nil {
		t.Fatalf("generator should redistribute an infeasible scenario quota across feasible mechanisms: %v", err)
	}
	noPlanCount := 0
	for _, task := range tasks {
		if task.ExpectedDisposition != ChallengeNoPlan {
			continue
		}
		noPlanCount++
		if task.Scenario == "NO_PLAN+"+challengeNoPlanRegionUnserved {
			t.Fatalf("fully served snapshot must not fabricate an unserved-region example: %+v", task)
		}
	}
	if noPlanCount != 6 {
		t.Fatalf("unsupported region quota should be redistributed while preserving total NO_PLAN count: got %d", noPlanCount)
	}
}

func mixedDecisionFixtureCatalog() ImportedCatalog {
	catalog := ImportedCatalog{Seed: 73}
	for itemIndex := 0; itemIndex < 30; itemIndex++ {
		for variant := 0; variant < 2; variant++ {
			roast, origin, style := "light", "ethiopia", "floral"
			if variant == 1 {
				roast, origin, style = "dark", "colombia", "nutty"
			}
			product := Product{SKUID: fmt.Sprintf("sim-m01-%02d-%d", itemIndex, variant), MerchantID: "sim-m01",
				Category: "coffee", Item: fmt.Sprintf("豆类-%02d", itemIndex), Format: "whole-bean", Style: style,
				Title: fmt.Sprintf("商品-%02d · whole-bean · %s", itemIndex, style), Tags: []string{"coffee"},
				Currency: "USDC", PriceMinor: 1000 + int64(itemIndex*10+variant), Status: "active",
				Attributes: map[string]any{"roast_level": roast, "origin": origin}}
			catalog.Products = append(catalog.Products, product)
			for _, region := range Regions {
				if region == "CN-WEST" && itemIndex%2 != 0 {
					continue
				}
				catalog.Inventory = append(catalog.Inventory, Inventory{TenantID: SimulationTenantID, MerchantID: product.MerchantID,
					SKUID: product.SKUID, Region: region, OnHand: 3, Eligible: true, PriceMinor: product.PriceMinor, Revision: 1})
			}
		}
	}
	return catalog
}

func challengeOffers(catalog ImportedCatalog) map[string]Inventory {
	offers := make(map[string]Inventory, len(catalog.Inventory))
	for _, inventory := range catalog.Inventory {
		offers[inventoryKey(SimulationTenantID, inventory.MerchantID, inventory.SKUID, inventory.Region)] = inventory
	}
	return offers
}
