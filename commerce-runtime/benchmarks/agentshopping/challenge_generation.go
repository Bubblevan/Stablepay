package agentshopping

import (
	"crypto/sha256"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// GenerateChallengePlanDraft derives a reproducible, attribute-constrained
// diagnostic suite from the pinned synthetic catalog. It is intentionally a
// draft generator: generated prompts and labels require independent review
// before the suite can be called a holdout or used for résumé claims.
func GenerateChallengePlanDraft(catalog ImportedCatalog, seed int64, count int) ([]ChallengeTask, error) {
	if count <= 0 || count > 500 || len(catalog.Products) == 0 || len(catalog.Inventory) == 0 {
		return nil, fmt.Errorf("challenge draft requires 1–500 tasks and a non-empty catalog: %w", ErrInvalidEvaluationSuite)
	}
	overlay := make([]OverlayEntry, 0, len(catalog.Inventory))
	for _, inventory := range catalog.Inventory {
		overlay = append(overlay, OverlayEntry{MerchantID: inventory.MerchantID, SKUID: inventory.SKUID,
			Region: inventory.Region, OnHand: inventory.OnHand - inventory.Reserved, Eligible: inventory.Eligible,
			PriceMinor: inventory.PriceMinor, Revision: inventory.Revision})
	}
	baseTasks, err := GenerateTasks(catalog.Products, overlay, seed, count)
	if err != nil {
		return nil, fmt.Errorf("generate feasible base tasks: %w", err)
	}
	productBySKU := make(map[string]Product, len(catalog.Products))
	for _, product := range catalog.Products {
		productBySKU[product.SKUID] = product
	}
	offers := make(map[string]Inventory, len(catalog.Inventory))
	for _, inventory := range catalog.Inventory {
		offers[inventoryKey(SimulationTenantID, inventory.MerchantID, inventory.SKUID, inventory.Region)] = inventory
	}
	taxonomy := TaxonomyFromProducts(catalog.Products)
	tasks := make([]ChallengeTask, 0, len(baseTasks))
	for _, base := range baseTasks {
		target, ok := productBySKU[base.UniqueTarget]
		if !ok {
			return nil, fmt.Errorf("base task %s references missing target %s: %w", base.TaskID, base.UniqueTarget, ErrInvalidEvaluationSuite)
		}
		intent := base.Intent
		constraints, selective, err := chooseChallengeAttributeConstraints(target, catalog.Products, offers, intent, taxonomy, seed, base.TaskID)
		if err != nil {
			return nil, fmt.Errorf("choose hard attributes for %s: %w", base.TaskID, err)
		}
		intent.RequiredAttributes = constraints
		gold := feasibleChallengeProducts(catalog.Products, offers, intent)
		goldSKUs := make([]string, 0, len(gold))
		for _, product := range gold {
			goldSKUs = append(goldSKUs, product.SKUID)
		}
		relevantSKUs := exactChallengeRelevantSKUs(catalog.Products, intent)
		if len(goldSKUs) == 0 || !containsString(goldSKUs, target.SKUID) {
			return nil, fmt.Errorf("hard attributes removed the known feasible target for %s: %w", base.TaskID, ErrInvalidEvaluationSuite)
		}
		if len(relevantSKUs) == 0 || !containsString(relevantSKUs, target.SKUID) {
			return nil, fmt.Errorf("hard attributes removed the known exact target from relevance labels for %s: %w", base.TaskID, ErrInvalidEvaluationSuite)
		}
		scenario := base.Scenario + "+HARD_ATTRIBUTES"
		if !selective {
			scenario = base.Scenario + "+NONSELECTIVE_ATTRIBUTES"
		}
		tasks = append(tasks, ChallengeTask{TaskID: "challenge-" + base.TaskID, Scenario: scenario,
			Prompt:              base.Prompt + challengeAttributePromptClause(constraints),
			ExpectedDisposition: ChallengePlan, Intent: intent, GoldSKUs: goldSKUs, RelevantSKUs: relevantSKUs})
	}
	if err := ValidateChallengeTaskSet(catalog, tasks); err != nil {
		return nil, fmt.Errorf("generated challenge draft failed snapshot validation: %w", err)
	}
	return tasks, nil
}

// exactChallengeRelevantSKUs labels every active USDC catalog listing that
// matches the exact requested item, format, and hard attributes. It purposely
// does not consult inventory, region, or price: those define feasibility, not
// whether a listing is the product the buyer asked for.
func exactChallengeRelevantSKUs(products []Product, intent Intent) []string {
	relevant := make([]string, 0)
	for _, product := range products {
		if product.Status == "active" && product.Currency == "USDC" &&
			candidateMatchType(product, intent) == MatchTypeExact {
			relevant = append(relevant, product.SKUID)
		}
	}
	sort.Strings(relevant)
	return relevant
}

// GenerateChallengeDecisionDraft creates a deterministic diagnostic suite
// with PLAN, NO_PLAN, and CLARIFY dispositions. Its labels are still derived
// from the local synthetic snapshot and require independent review.
func GenerateChallengeDecisionDraft(catalog ImportedCatalog, seed int64, count int) ([]ChallengeTask, error) {
	if count < 7 {
		return nil, fmt.Errorf("mixed-decision draft requires at least 7 tasks: %w", ErrInvalidEvaluationSuite)
	}
	planDraft, err := GenerateChallengePlanDraft(catalog, seed, count)
	if err != nil {
		return nil, err
	}
	noPlanCount := count * 15 / 100
	clarifyCount := count * 15 / 100
	type selection struct {
		index int
		hash  [32]byte
	}
	order := make([]selection, len(planDraft))
	for index, task := range planDraft {
		order[index] = selection{index: index, hash: sha256.Sum256([]byte(fmt.Sprintf("%d|%s|mixed-disposition-v1", seed, task.TaskID)))}
	}
	sort.Slice(order, func(i, j int) bool { return strings.Compare(string(order[i].hash[:]), string(order[j].hash[:])) < 0 })
	offers := make(map[string]Inventory, len(catalog.Inventory))
	for _, inventory := range catalog.Inventory {
		offers[inventoryKey(SimulationTenantID, inventory.MerchantID, inventory.SKUID, inventory.Region)] = inventory
	}
	taxonomy := TaxonomyFromProducts(catalog.Products)
	noPlanScenariosByIndex := make(map[int]string, noPlanCount)
	scenarioBase, scenarioRemainder := noPlanCount/len(challengeNoPlanScenarios), noPlanCount%len(challengeNoPlanScenarios)
	remainingNoPlan := noPlanCount
	for scenarioIndex, scenario := range challengeNoPlanScenarios {
		target := scenarioBase
		if scenarioIndex < scenarioRemainder {
			target++
		}
		assigned := 0
		for _, candidate := range order {
			if assigned == target {
				break
			}
			if _, alreadySelected := noPlanScenariosByIndex[candidate.index]; alreadySelected {
				continue
			}
			if !supportsChallengeNoPlanScenario(planDraft[candidate.index], catalog.Products, offers, taxonomy, scenario) {
				continue
			}
			noPlanScenariosByIndex[candidate.index] = scenario
			assigned++
		}
		remainingNoPlan -= assigned
	}
	// Keep the mechanisms balanced where the pinned catalog supports them,
	// then redistribute any unfillable quota across the remaining feasible
	// mechanisms. Do not fabricate inventory gaps just to meet a scenario quota.
	for _, scenario := range challengeNoPlanScenarios {
		if remainingNoPlan == 0 {
			break
		}
		for _, candidate := range order {
			if remainingNoPlan == 0 {
				break
			}
			if _, alreadySelected := noPlanScenariosByIndex[candidate.index]; alreadySelected ||
				!supportsChallengeNoPlanScenario(planDraft[candidate.index], catalog.Products, offers, taxonomy, scenario) {
				continue
			}
			noPlanScenariosByIndex[candidate.index] = scenario
			remainingNoPlan--
		}
	}
	if remainingNoPlan != 0 {
		return nil, fmt.Errorf("synthetic snapshot supports only %d NO_PLAN tasks across feasible mechanisms; requested %d",
			noPlanCount-remainingNoPlan, noPlanCount)
	}
	clarifyIndexes := make(map[int]bool, clarifyCount)
	for _, candidate := range order {
		if len(clarifyIndexes) == clarifyCount {
			break
		}
		if _, isNoPlan := noPlanScenariosByIndex[candidate.index]; !isNoPlan {
			clarifyIndexes[candidate.index] = true
		}
	}
	clarificationOrdinal := 0
	for index := range planDraft {
		if scenario, isNoPlan := noPlanScenariosByIndex[index]; isNoPlan {
			planDraft[index], err = makeChallengeNoPlanTask(planDraft[index], catalog.Products, offers, taxonomy,
				scenario)
		} else if clarifyIndexes[index] {
			planDraft[index] = makeChallengeClarificationTask(planDraft[index], clarificationOrdinal)
			clarificationOrdinal++
		}
		if err != nil {
			return nil, fmt.Errorf("convert draft task %s to mixed decision: %w", planDraft[index].TaskID, err)
		}
	}
	if err := addOptionalStylePlanCases(catalog, planDraft, seed); err != nil {
		return nil, fmt.Errorf("add optional-style plan controls: %w", err)
	}
	if err := ValidateChallengeTaskSet(catalog, planDraft); err != nil {
		return nil, fmt.Errorf("generated mixed-decision draft failed snapshot validation: %w", err)
	}
	return planDraft, nil
}

const (
	challengeNoPlanPriceBoundary         = "PRICE_BOUNDARY"
	challengeNoPlanRegionUnserved        = "REGION_UNSERVED"
	challengeNoPlanAttributeIntersection = "ATTRIBUTE_INTERSECTION_EMPTY"
)

var challengeNoPlanScenarios = []string{
	challengeNoPlanPriceBoundary,
	challengeNoPlanRegionUnserved,
	challengeNoPlanAttributeIntersection,
}

func supportsChallengeNoPlanScenario(task ChallengeTask, products []Product, offers map[string]Inventory, taxonomy Taxonomy, kind string) bool {
	intent := task.Intent
	intent.RequiredAttributes = nil
	switch kind {
	case challengeNoPlanPriceBoundary:
		minimum := minimumEligibleChallengePrice(products, offers, intent)
		return minimum > 1 && minimum < math.MaxInt64
	case challengeNoPlanRegionUnserved:
		_, _, ok := findUnservedChallengeRegion(products, offers, intent)
		return ok
	case challengeNoPlanAttributeIntersection:
		intent.MaxPriceMinor = maximumEligibleChallengePrice(products, offers, intent, "")
		if intent.MaxPriceMinor <= 0 {
			return false
		}
		_, ok := findUnsatisfiableAttributePair(products, offers, intent, taxonomy)
		return ok
	default:
		return false
	}
}

func makeChallengeNoPlanTask(task ChallengeTask, products []Product, offers map[string]Inventory, taxonomy Taxonomy, kind string) (ChallengeTask, error) {
	intent := task.Intent
	intent.RequiredAttributes = nil
	task.RelevantSKUs = nil
	prompt := stripGeneratedAttributeClause(task.Prompt)
	oldBudget := FormatMinorUnits(intent.MaxPriceMinor)
	switch kind {
	case challengeNoPlanPriceBoundary:
		minimum := minimumEligibleChallengePrice(products, offers, intent)
		if minimum <= 1 || minimum == math.MaxInt64 {
			return ChallengeTask{}, fmt.Errorf("cannot construct a safe no-plan price boundary for %s", task.TaskID)
		}
		digest := sha256.Sum256([]byte("challenge-no-plan-price-gap-v1|" + task.TaskID))
		gapPercent := int64(5 + int(digest[0])%26)
		gap := minimum/100*gapPercent + minimum%100*gapPercent/100
		if gap < 1 {
			gap = 1
		}
		intent.MaxPriceMinor = minimum - gap
		if intent.MaxPriceMinor <= 0 {
			return ChallengeTask{}, fmt.Errorf("price gap leaves no positive budget for %s", task.TaskID)
		}
		var replaced bool
		prompt, replaced = replaceGeneratedPromptBudget(prompt, oldBudget, FormatMinorUnits(intent.MaxPriceMinor))
		if !replaced {
			return ChallengeTask{}, fmt.Errorf("generated prompt %s has no budget to tighten", task.TaskID)
		}
	case challengeNoPlanRegionUnserved:
		region, maximumPrice, ok := findUnservedChallengeRegion(products, offers, intent)
		if !ok {
			return ChallengeTask{}, fmt.Errorf("catalog has no valid unserved region for %s", task.TaskID)
		}
		oldRegion := intent.Region
		intent.Region = region
		intent.MaxPriceMinor = maximumPrice
		var replacedBudget, replacedRegion bool
		prompt, replacedBudget = replaceGeneratedPromptBudget(prompt, oldBudget, FormatMinorUnits(intent.MaxPriceMinor))
		prompt, replacedRegion = replaceGeneratedPromptRegion(prompt, oldRegion, region)
		if !replacedBudget || !replacedRegion {
			return ChallengeTask{}, fmt.Errorf("generated prompt %s cannot express unserved region %s", task.TaskID, region)
		}
	case challengeNoPlanAttributeIntersection:
		maximumPrice := maximumEligibleChallengePrice(products, offers, intent, "")
		if maximumPrice <= 0 {
			return ChallengeTask{}, fmt.Errorf("no eligible offers to construct attribute conflict for %s", task.TaskID)
		}
		intent.MaxPriceMinor = maximumPrice
		var replaced bool
		prompt, replaced = replaceGeneratedPromptBudget(prompt, oldBudget, FormatMinorUnits(intent.MaxPriceMinor))
		if !replaced {
			return ChallengeTask{}, fmt.Errorf("generated prompt %s has no budget to widen for attribute diagnosis", task.TaskID)
		}
		constraints, ok := findUnsatisfiableAttributePair(products, offers, intent, taxonomy)
		if !ok {
			return ChallengeTask{}, fmt.Errorf("catalog has no unsatisfiable hard-attribute pair for %s", task.TaskID)
		}
		intent.RequiredAttributes = constraints
		prompt += challengeAttributePromptClause(constraints)
	default:
		return ChallengeTask{}, fmt.Errorf("unsupported no-plan scenario %q", kind)
	}
	task.Prompt = prompt
	task.Intent = intent
	task.ExpectedDisposition = ChallengeNoPlan
	task.GoldSKUs = nil
	task.RequiredMissingFields = nil
	task.Scenario = "NO_PLAN+" + kind
	return task, nil
}

func minimumEligibleChallengePrice(products []Product, offers map[string]Inventory, intent Intent) int64 {
	minimum := int64(math.MaxInt64)
	for _, product := range products {
		if product.Currency != "USDC" || candidateMatchType(product, intent) == "" {
			continue
		}
		offer, ok := offers[inventoryKey(SimulationTenantID, product.MerchantID, product.SKUID, intent.Region)]
		if ok && offer.Eligible && offer.OnHand-offer.Reserved > 0 && offer.PriceMinor < minimum {
			minimum = offer.PriceMinor
		}
	}
	return minimum
}

func findUnservedChallengeRegion(products []Product, offers map[string]Inventory, intent Intent) (string, int64, bool) {
	maximumPrice := maximumEligibleChallengePrice(products, offers, intent, "")
	if maximumPrice <= 0 {
		return "", 0, false
	}
	for _, region := range Regions {
		served := false
		for _, product := range products {
			if product.Currency != "USDC" || candidateMatchType(product, intent) == "" {
				continue
			}
			offer, ok := offers[inventoryKey(SimulationTenantID, product.MerchantID, product.SKUID, region)]
			if ok && offer.Eligible && offer.OnHand-offer.Reserved > 0 {
				served = true
				break
			}
		}
		if !served {
			return region, maximumPrice, true
		}
	}
	return "", 0, false
}

func maximumEligibleChallengePrice(products []Product, offers map[string]Inventory, intent Intent, region string) int64 {
	maximum := int64(0)
	regions := Regions
	if region != "" {
		regions = []string{region}
	}
	for _, product := range products {
		if product.Currency != "USDC" || candidateMatchType(product, intent) == "" {
			continue
		}
		for _, candidateRegion := range regions {
			offer, ok := offers[inventoryKey(SimulationTenantID, product.MerchantID, product.SKUID, candidateRegion)]
			if ok && offer.Eligible && offer.OnHand-offer.Reserved > 0 && offer.PriceMinor > maximum {
				maximum = offer.PriceMinor
			}
		}
	}
	return maximum
}

func replaceGeneratedPromptRegion(prompt, oldRegion, newRegion string) (string, bool) {
	regionNames := map[string]string{"CN-EAST": "华东", "CN-NORTH": "华北", "CN-SOUTH": "华南", "CN-WEST": "西部"}
	oldName, oldOK := regionNames[oldRegion]
	newName, newOK := regionNames[newRegion]
	if !oldOK || !newOK {
		return prompt, false
	}
	oldPhrase := oldName + "地区"
	if !strings.Contains(prompt, oldPhrase) {
		return prompt, false
	}
	return strings.Replace(prompt, oldPhrase, newName+"地区", 1), true
}

var challengeClarificationPatterns = [][]string{
	{"item"},
	{"required_format"},
	{"item", "required_format"},
}

var challengeClarificationOpeners = []string{
	"我准备购买一件%s类商品",
	"想请你帮我挑一件%s类商品",
	"请按我的条件推荐一件%s类商品",
	"我最近想选购一件%s类商品",
	"帮我看看有没有合适的%s类商品",
	"我需要买一件%s类商品",
}

func makeChallengeClarificationTask(task ChallengeTask, ordinal int) ChallengeTask {
	intent := task.Intent
	task.RelevantSKUs = nil
	pattern := challengeClarificationPatterns[ordinal%len(challengeClarificationPatterns)]
	missing := make(map[string]bool, len(pattern))
	for _, field := range pattern {
		missing[field] = true
	}
	if missing["item"] {
		intent.Item = ""
	}
	if missing["required_format"] {
		intent.RequiredFormat = ""
	}
	intent.RequiredAttributes = nil
	intent.AllowSubstitution = false
	intent.ClarificationRequired = true
	intent.ClarificationQuestion = ""
	intent.ConfirmationNeeded = true
	regionNames := map[string]string{"CN-EAST": "华东", "CN-NORTH": "华北", "CN-SOUTH": "华南", "CN-WEST": "西部"}
	categoryNames := map[string]string{"audio": "音频内容", "books": "书籍", "cloud-tools": "云端工具", "coffee": "咖啡",
		"design-assets": "设计素材", "developer-tools": "开发工具", "education": "课程",
		"game-collectibles": "数字收藏品", "stock-media": "媒体素材", "templates": "模板", "travel-guides": "旅行资料",
		"wellness-guides": "健康指南"}
	categoryName := categoryNames[intent.Category]
	if categoryName == "" {
		categoryName = intent.Category
	}
	regionName := regionNames[intent.Region]
	if regionName == "" {
		regionName = intent.Region
	}
	known := make([]string, 0, 3)
	if intent.Item != "" {
		known = append(known, fmt.Sprintf("商品关键词是“%s”", intent.Item))
	}
	if intent.RequiredFormat != "" {
		known = append(known, fmt.Sprintf("规格要求“%s”", intent.RequiredFormat))
	}
	if intent.PreferredStyle != "" {
		known = append(known, fmt.Sprintf("风格偏好“%s”", intent.PreferredStyle))
	}
	opener := challengeClarificationOpeners[ordinal%len(challengeClarificationOpeners)]
	task.Prompt = fmt.Sprintf("%s。%s。送到%s，预算不超过%s USDC。你觉得有哪些商品值得考虑？",
		fmt.Sprintf(opener, categoryName), strings.Join(known, "；"), regionName, FormatMinorUnits(intent.MaxPriceMinor))
	task.Intent = intent
	task.ExpectedDisposition = ChallengeClarify
	task.GoldSKUs = nil
	task.RequiredMissingFields = append([]string(nil), pattern...)
	task.Scenario = "CLARIFY+MISSING_" + strings.ToUpper(strings.Join(pattern, "_"))
	return task
}

func addOptionalStylePlanCases(catalog ImportedCatalog, tasks []ChallengeTask, seed int64) error {
	type selection struct {
		index int
		hash  [32]byte
	}
	plans := make([]selection, 0, len(tasks))
	for index, task := range tasks {
		if task.ExpectedDisposition != ChallengePlan {
			continue
		}
		plans = append(plans, selection{index: index,
			hash: sha256.Sum256([]byte(fmt.Sprintf("%d|%s|optional-style-plan-v1", seed, task.TaskID)))})
	}
	sort.Slice(plans, func(i, j int) bool { return strings.Compare(string(plans[i].hash[:]), string(plans[j].hash[:])) < 0 })
	optionalStyleCount := len(plans) / 5
	if optionalStyleCount == 0 && len(plans) > 0 {
		optionalStyleCount = 1
	}
	eligiblePlans := make([]selection, 0, len(plans))
	for _, plan := range plans {
		if styleOmissionCandidate(tasks[plan.index]) {
			eligiblePlans = append(eligiblePlans, plan)
		}
	}
	if len(eligiblePlans) < optionalStyleCount {
		return fmt.Errorf("only %d plan prompts can omit style without lexical leakage; need %d", len(eligiblePlans), optionalStyleCount)
	}
	offers := make(map[string]Inventory, len(catalog.Inventory))
	for _, inventory := range catalog.Inventory {
		offers[inventoryKey(SimulationTenantID, inventory.MerchantID, inventory.SKUID, inventory.Region)] = inventory
	}
	for _, selected := range eligiblePlans[:optionalStyleCount] {
		task := &tasks[selected.index]
		style := strings.TrimSpace(task.Intent.PreferredStyle)
		if style == "" {
			return fmt.Errorf("plan task %s already lacks a preferred style", task.TaskID)
		}
		prompt, ok := removeGeneratedPreferredStyle(stripGeneratedAttributeClause(task.Prompt))
		if !ok {
			return fmt.Errorf("generated prompt %s does not expose a removable style-preference clause", task.TaskID)
		}
		attributes := make(map[string]string, len(task.Intent.RequiredAttributes))
		for key, value := range task.Intent.RequiredAttributes {
			// Synthetic tasting-note values include the product's style. Do not
			// accidentally turn an omitted soft preference into a hard attribute.
			if strings.EqualFold(strings.TrimSpace(value), style) {
				continue
			}
			attributes[key] = value
		}
		intent := task.Intent
		intent.PreferredStyle = ""
		intent.RequiredAttributes = attributes
		baseIntent := intent
		baseIntent.RequiredAttributes = nil
		baseCandidates := feasibleChallengeProducts(catalog.Products, offers, baseIntent)
		candidates := feasibleChallengeProducts(catalog.Products, offers, intent)
		if len(candidates) == 0 {
			return fmt.Errorf("style-optional plan task %s has no feasible offers", task.TaskID)
		}
		selective := len(candidates) < len(baseCandidates)
		task.Scenario = strings.TrimSuffix(task.Scenario, "+HARD_ATTRIBUTES")
		task.Scenario = strings.TrimSuffix(task.Scenario, "+NONSELECTIVE_ATTRIBUTES")
		if selective {
			task.Scenario += "+HARD_ATTRIBUTES"
		} else {
			task.Scenario += "+NONSELECTIVE_ATTRIBUTES"
		}
		task.Scenario += "+STYLE_UNSPECIFIED"
		task.Intent = intent
		task.Prompt = prompt
		if len(attributes) > 0 {
			task.Prompt += challengeAttributePromptClause(attributes)
		}
		if strings.Contains(task.Prompt, style) {
			return fmt.Errorf("style-optional plan task %s leaks the omitted style in its prompt", task.TaskID)
		}
		task.GoldSKUs = task.GoldSKUs[:0]
		for _, candidate := range candidates {
			task.GoldSKUs = append(task.GoldSKUs, candidate.SKUID)
		}
		// Removing a style-derived hard attribute changes the exact-target
		// relevance set too. Recompute it from the final intent rather than
		// carrying labels derived from the pre-omission constraints.
		task.RelevantSKUs = exactChallengeRelevantSKUs(catalog.Products, task.Intent)
	}
	return nil
}

func styleOmissionCandidate(task ChallengeTask) bool {
	style := strings.TrimSpace(task.Intent.PreferredStyle)
	if style == "" {
		return false
	}
	prompt, ok := removeGeneratedPreferredStyle(stripGeneratedAttributeClause(task.Prompt))
	if !ok || strings.Contains(prompt, style) {
		return false
	}
	attributes := make(map[string]string, len(task.Intent.RequiredAttributes))
	for key, value := range task.Intent.RequiredAttributes {
		if strings.EqualFold(strings.TrimSpace(value), style) {
			continue
		}
		attributes[key] = value
	}
	if len(attributes) > 0 {
		prompt += challengeAttributePromptClause(attributes)
	}
	return !strings.Contains(prompt, style)
}

func removeGeneratedPreferredStyle(prompt string) (string, bool) {
	const stylePrefix = "，风格偏好"
	const budgetMarker = "，单件预算不超过"
	start := strings.Index(prompt, stylePrefix)
	if start < 0 {
		return prompt, false
	}
	budgetOffset := strings.Index(prompt[start+len(stylePrefix):], budgetMarker)
	if budgetOffset < 0 {
		return prompt, false
	}
	end := start + len(stylePrefix) + budgetOffset
	return strings.TrimSpace(prompt[:start] + prompt[end:]), true
}

func findUnsatisfiableAttributePair(products []Product, offers map[string]Inventory, intent Intent, taxonomy Taxonomy) (map[string]string, bool) {
	intent.RequiredAttributes = nil
	candidates := feasibleChallengeProducts(products, offers, intent)
	if len(candidates) == 0 {
		return nil, false
	}
	type observedValue struct {
		key      string
		value    string
		products []int
	}
	byKey := taxonomy.CategoryAttributeValues[intent.Category]
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	options := make([]observedValue, 0)
	for _, key := range keys {
		for _, value := range byKey[key] {
			option := observedValue{key: key, value: value}
			for index, product := range candidates {
				if requiredAttributesMatch(product.Attributes, map[string]string{key: value}) {
					option.products = append(option.products, index)
				}
			}
			if len(option.products) > 0 {
				options = append(options, option)
			}
		}
	}
	for i, left := range options {
		for _, right := range options[i+1:] {
			if left.key == right.key {
				continue
			}
			leftIndex, rightIndex, intersects := 0, 0, false
			for leftIndex < len(left.products) && rightIndex < len(right.products) {
				switch {
				case left.products[leftIndex] == right.products[rightIndex]:
					intersects = true
					leftIndex = len(left.products)
				case left.products[leftIndex] < right.products[rightIndex]:
					leftIndex++
				default:
					rightIndex++
				}
			}
			if !intersects {
				return map[string]string{left.key: left.value, right.key: right.value}, true
			}
		}
	}
	return nil, false
}

func stripGeneratedAttributeClause(prompt string) string {
	const marker = " 商品详情需要同时符合这些条件："
	if prefix, _, found := strings.Cut(prompt, marker); found {
		return strings.TrimSpace(prefix)
	}
	return strings.TrimSpace(prompt)
}

func replaceGeneratedPromptBudget(prompt, oldBudget, newBudget string) (string, bool) {
	if oldBudget == newBudget {
		return prompt, true
	}
	if !strings.Contains(prompt, oldBudget) {
		return prompt, false
	}
	return strings.Replace(prompt, oldBudget, newBudget, 1), true
}

func formatChallengeAttributes(attributes map[string]string) string {
	keys := make([]string, 0, len(attributes))
	for key := range attributes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+" = "+strconv.Quote(attributes[key]))
	}
	return strings.Join(parts, "；")
}

func challengeAttributePromptClause(attributes map[string]string) string {
	return " 商品详情需要同时符合这些条件：" + formatChallengeAttributes(attributes) + "。"
}

type challengeAttributeConstraint struct {
	key   string
	value string
}

func chooseChallengeAttributeConstraints(target Product, products []Product, offers map[string]Inventory,
	intent Intent, taxonomy Taxonomy, seed int64, taskID string) (map[string]string, bool, error) {
	base := feasibleChallengeProducts(products, offers, intent)
	if len(base) == 0 {
		return nil, false, fmt.Errorf("base intent has no feasible products")
	}
	allowedByKey := taxonomy.CategoryAttributeValues[target.Category]
	keys := make([]string, 0, len(allowedByKey))
	for key := range allowedByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	options := make([]challengeAttributeConstraint, 0)
	for _, key := range keys {
		for _, value := range attributeValueStrings(target.Attributes[key]) {
			if !allowedValue(allowedByKey[key], value) {
				continue
			}
			filtered := feasibleChallengeProducts(products, offers, withRequiredAttribute(intent, key, value))
			if len(filtered) > 0 {
				options = append(options, challengeAttributeConstraint{key: key, value: value})
			}
		}
	}
	if len(options) == 0 {
		return nil, false, fmt.Errorf("target %s has no allowlisted attributes", target.SKUID)
	}
	seedHash := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|challenge-attribute-v1", seed, taskID)))
	desired := 2 + int(seedHash[0]%7) // Prefer a small but not necessarily singleton gold set.
	bestScore := math.MaxInt
	var best map[string]string
	bestSelective := false
	consider := func(candidate map[string]string) {
		filtered := feasibleChallengeProducts(products, offers, withRequiredAttributes(intent, candidate))
		if len(filtered) == 0 {
			return
		}
		selective := len(filtered) < len(base)
		score := (len(filtered)-desired)*(len(filtered)-desired)*4 + (2-len(candidate))*5
		if !selective {
			score += 1000000
		}
		if score < bestScore {
			bestScore = score
			best = candidate
			bestSelective = selective
		}
	}
	for i, first := range options {
		consider(map[string]string{first.key: first.value})
		for _, second := range options[i+1:] {
			if first.key == second.key {
				continue
			}
			consider(map[string]string{first.key: first.value, second.key: second.value})
		}
	}
	if len(best) == 0 {
		return nil, false, fmt.Errorf("no allowlisted attribute combination retains a feasible target")
	}
	return best, bestSelective, nil
}

func feasibleChallengeProducts(products []Product, offers map[string]Inventory, intent Intent) []Product {
	result := make([]Product, 0, 16)
	for _, product := range products {
		if product.Currency != "USDC" || candidateMatchType(product, intent) == "" {
			continue
		}
		offer, ok := offers[inventoryKey(SimulationTenantID, product.MerchantID, product.SKUID, intent.Region)]
		if !ok || !offer.Eligible || offer.OnHand-offer.Reserved < 1 || offer.PriceMinor > intent.MaxPriceMinor {
			continue
		}
		result = append(result, product)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].SKUID < result[j].SKUID })
	return result
}

func withRequiredAttribute(intent Intent, key, value string) Intent {
	return withRequiredAttributes(intent, map[string]string{key: value})
}

func withRequiredAttributes(intent Intent, attributes map[string]string) Intent {
	intent.RequiredAttributes = attributes
	return intent
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
