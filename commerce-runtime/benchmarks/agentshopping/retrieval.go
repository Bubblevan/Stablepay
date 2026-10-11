package agentshopping

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

const DefaultDiscoveryCandidateWindow = 100
const defaultDiscoveryTitleCoverageWeight = 2.0
const defaultDiscoveryTitlePhraseWeight = 1.0

type catalogDiscoveryKey struct {
	category string
	format   string
}

type catalogDiscoveryPartition struct {
	products  []Product
	documents map[string]catalogDiscoveryDocument
}

type catalogDiscoveryDocument struct {
	allTokens     []string
	titleTokens   []string
	titleSequence []string
	titleBigrams  map[discoveryTextBigram]struct{}
}

type scoredDiscoveryProduct struct {
	product            Product
	matchType          string
	hardAttributesOkay bool
	bm25Score          float64
	textScore          int
	style              int
}

type discoveryTextBigram [2]string

// merchantCatalogDiscoveryIndex narrows each request to the category and
// required-format partition and caches immutable tokenized documents for that
// snapshot. Query-dependent BM25 statistics are still computed per request.
type merchantCatalogDiscoveryIndex map[catalogDiscoveryKey]catalogDiscoveryPartition

func newMerchantCatalogDiscoveryIndex(products []Product) merchantCatalogDiscoveryIndex {
	index := make(merchantCatalogDiscoveryIndex)
	for _, product := range products {
		key := catalogDiscoveryKey{category: product.Category, format: product.Format}
		partition := index[key]
		if partition.documents == nil {
			partition.documents = make(map[string]catalogDiscoveryDocument)
		}
		partition.products = append(partition.products, product)
		titleSequence := tokenizeDiscoveryText(product.Title)
		titleTokens := uniqueTerms(titleSequence)
		sort.Strings(titleTokens)
		partition.documents[catalogProductKey(product.MerchantID, product.SKUID)] = catalogDiscoveryDocument{
			allTokens: productDiscoveryTokens(product), titleTokens: titleTokens, titleSequence: titleSequence,
			titleBigrams: discoveryTitleBigramSet(titleSequence),
		}
		index[key] = partition
	}
	return index
}

func (index merchantCatalogDiscoveryIndex) productsForIntent(intent Intent) []Product {
	return index.partitionForIntent(intent).products
}

func (index merchantCatalogDiscoveryIndex) documentsForIntent(intent Intent) map[string]catalogDiscoveryDocument {
	return index.partitionForIntent(intent).documents
}

func (index merchantCatalogDiscoveryIndex) partitionForIntent(intent Intent) catalogDiscoveryPartition {
	return index[catalogDiscoveryKey{category: intent.Category, format: intent.RequiredFormat}]
}

// rankDiscoveryProducts models the first-stage Merchant catalog search. It
// routes by category and required item/format, then ranks full hard-attribute
// matches ahead of other same-item listings without removing the latter.
// Inventory and live price overlays are still applied after the bounded
// retrieval window, so discovery coverage remains separate from offer
// feasibility.
func rankDiscoveryProducts(products []Product, intent Intent, limit int) []Product {
	return rankDiscoveryProductsWithWeights(products, intent, limit, defaultDiscoveryTitleCoverageWeight, defaultDiscoveryTitlePhraseWeight)
}

// rankDiscoveryProductsWithTitleCoverageWeight is also used by the ESCI
// development benchmark to compare a bounded title-coverage reranking signal
// against the BM25 order without title-phrase scoring. A zero weight disables
// title coverage.
func rankDiscoveryProductsWithTitleCoverageWeight(products []Product, intent Intent, limit int, titleCoverageWeight float64) []Product {
	return rankDiscoveryProductsWithWeights(products, intent, limit, titleCoverageWeight, 0)
}

func rankDiscoveryProductsWithWeights(products []Product, intent Intent, limit int, titleCoverageWeight, titlePhraseWeight float64) []Product {
	if limit <= 0 || limit > 100 {
		limit = DefaultRecallCutoff
	}
	candidates := make([]scoredDiscoveryProduct, 0, 64)
	for _, product := range products {
		matchType := discoveryMatchType(product, intent)
		if matchType == "" {
			continue
		}
		candidates = append(candidates, scoredDiscoveryProduct{product: product, matchType: matchType,
			hardAttributesOkay: requiredAttributesMatch(product.Attributes, intent.RequiredAttributes),
			textScore:          discoveryTextScore(product, intent), style: stylePreferenceScore(product, intent.PreferredStyle)})
	}
	if strings.TrimSpace(intent.searchText) != "" {
		applyDiscoveryBM25(candidates, intent)
		applyDiscoveryTitleCoverageBonus(candidates, intent, titleCoverageWeight)
		applyDiscoveryTitlePhraseBonus(candidates, intent, titlePhraseWeight)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].matchType != candidates[j].matchType {
			return matchTypePriority(candidates[i].matchType) > matchTypePriority(candidates[j].matchType)
		}
		if candidates[i].hardAttributesOkay != candidates[j].hardAttributesOkay {
			return candidates[i].hardAttributesOkay
		}
		if candidates[i].bm25Score != candidates[j].bm25Score {
			return candidates[i].bm25Score > candidates[j].bm25Score
		}
		if candidates[i].textScore != candidates[j].textScore {
			return candidates[i].textScore > candidates[j].textScore
		}
		if candidates[i].style != candidates[j].style {
			return candidates[i].style > candidates[j].style
		}
		return candidates[i].product.SKUID < candidates[j].product.SKUID
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	result := make([]Product, len(candidates))
	for i, candidate := range candidates {
		result[i] = candidate.product
	}
	return result
}

func applyDiscoveryTitlePhraseBonus(candidates []scoredDiscoveryProduct, intent Intent, weight float64) {
	if weight <= 0 || len(candidates) == 0 {
		return
	}
	queryTokens := tokenizeDiscoveryText(intent.searchText)
	queryBigrams := uniqueDiscoveryTextBigrams(queryTokens)
	if len(queryBigrams) == 0 {
		return
	}
	for index := range candidates {
		product := candidates[index].product
		document := intent.discoveryDocs[catalogProductKey(product.MerchantID, product.SKUID)]
		titleBigrams := document.titleBigrams
		if titleBigrams == nil {
			titleSequence := document.titleSequence
			if titleSequence == nil {
				titleSequence = tokenizeDiscoveryText(product.Title)
			}
			titleBigrams = discoveryTitleBigramSet(titleSequence)
		}
		candidates[index].bm25Score += weight * discoveryTitleBigramCoverageForSet(queryBigrams, titleBigrams)
	}
}

func discoveryTitleBigramCoverage(queryTokens, titleTokens []string) float64 {
	return discoveryTitleBigramCoverageForSet(uniqueDiscoveryTextBigrams(queryTokens), discoveryTitleBigramSet(titleTokens))
}

func uniqueDiscoveryTextBigrams(tokens []string) []discoveryTextBigram {
	if len(tokens) < 2 {
		return nil
	}
	seen := make(map[discoveryTextBigram]struct{}, len(tokens)-1)
	bigrams := make([]discoveryTextBigram, 0, len(tokens)-1)
	for index := 0; index+1 < len(tokens); index++ {
		bigram := discoveryTextBigram{tokens[index], tokens[index+1]}
		if _, exists := seen[bigram]; exists {
			continue
		}
		seen[bigram] = struct{}{}
		bigrams = append(bigrams, bigram)
	}
	return bigrams
}

func discoveryTitleBigramSet(tokens []string) map[discoveryTextBigram]struct{} {
	if len(tokens) < 2 {
		return nil
	}
	bigrams := make(map[discoveryTextBigram]struct{}, len(tokens)-1)
	for index := 0; index+1 < len(tokens); index++ {
		bigrams[discoveryTextBigram{tokens[index], tokens[index+1]}] = struct{}{}
	}
	return bigrams
}

func discoveryTitleBigramCoverageForSet(queryBigrams []discoveryTextBigram, titleBigrams map[discoveryTextBigram]struct{}) float64 {
	if len(queryBigrams) == 0 || len(titleBigrams) == 0 {
		return 0
	}
	matched := 0
	for _, queryBigram := range queryBigrams {
		if _, exists := titleBigrams[queryBigram]; exists {
			matched++
		}
	}
	return float64(matched) / float64(len(queryBigrams))
}

func applyDiscoveryTitleCoverageBonus(candidates []scoredDiscoveryProduct, intent Intent, weight float64) {
	if weight <= 0 || len(candidates) == 0 {
		return
	}
	queryTerms := uniqueTerms(tokenizeDiscoveryText(intent.searchText))
	if len(queryTerms) == 0 {
		return
	}
	sort.Strings(queryTerms)
	for index := range candidates {
		product := candidates[index].product
		titleTokens := intent.discoveryDocs[catalogProductKey(product.MerchantID, product.SKUID)].titleTokens
		if titleTokens == nil {
			titleTokens = uniqueTerms(tokenizeDiscoveryText(product.Title))
			sort.Strings(titleTokens)
		}
		matched := 0
		for _, term := range titleTokens {
			queryIndex := sort.SearchStrings(queryTerms, term)
			if queryIndex < len(queryTerms) && queryTerms[queryIndex] == term {
				matched++
			}
		}
		candidates[index].bm25Score += weight * float64(matched) / float64(len(queryTerms))
	}
}

// applyDiscoveryBM25 uses the original buyer wording and structured intent
// against the candidate product's searchable catalog facts. It only reranks
// candidates that already passed category/item/format and exact/substitute
// eligibility checks; match type, hard constraints, live inventory and price
// retain their existing authority.
func applyDiscoveryBM25(candidates []scoredDiscoveryProduct, intent Intent) {
	queryParts := []string{intent.searchText, intent.Category, intent.Item, intent.RequiredFormat,
		intent.PreferredStyle, intent.Region}
	attributeKeys := make([]string, 0, len(intent.RequiredAttributes))
	for key := range intent.RequiredAttributes {
		attributeKeys = append(attributeKeys, key)
	}
	sort.Strings(attributeKeys)
	for _, key := range attributeKeys {
		queryParts = append(queryParts, key, intent.RequiredAttributes[key])
	}
	queryTerms := uniqueTerms(tokenizeDiscoveryText(strings.Join(queryParts, " ")))
	if len(queryTerms) == 0 || len(candidates) == 0 {
		return
	}

	queryTermIndex := make(map[string]int, len(queryTerms))
	for index, term := range queryTerms {
		queryTermIndex[term] = index
	}
	frequenciesByDocument := make([][]int, len(candidates))
	lengths := make([]int, len(candidates))
	documentFrequency := make([]int, len(queryTerms))
	totalLength := 0
	for index, candidate := range candidates {
		product := candidate.product
		document := intent.discoveryDocs[catalogProductKey(product.MerchantID, product.SKUID)].allTokens
		if document == nil {
			document = productDiscoveryTokens(product)
		}
		lengths[index] = len(document)
		totalLength += lengths[index]
		frequencies := make([]int, len(queryTerms))
		for _, term := range document {
			termIndex, relevant := queryTermIndex[term]
			if !relevant {
				continue
			}
			if frequencies[termIndex] == 0 {
				documentFrequency[termIndex]++
			}
			frequencies[termIndex]++
		}
		frequenciesByDocument[index] = frequencies
	}
	averageLength := float64(totalLength) / float64(len(candidates))
	if averageLength < 1 {
		averageLength = 1
	}
	const k1, b = 1.2, 0.75
	count := float64(len(candidates))
	inverseFrequencies := make([]float64, len(queryTerms))
	for termIndex := range queryTerms {
		df := float64(documentFrequency[termIndex])
		inverseFrequencies[termIndex] = math.Log(1 + (count-df+0.5)/(df+0.5))
	}
	for index, frequencies := range frequenciesByDocument {
		lengthNorm := k1 * (1 - b + b*float64(lengths[index])/averageLength)
		for termIndex, frequency := range frequencies {
			if frequency == 0 {
				continue
			}
			tf := float64(frequency)
			candidates[index].bm25Score += inverseFrequencies[termIndex] * tf * (k1 + 1) / (tf + lengthNorm)
		}
	}
}

func productDiscoveryTokens(product Product) []string {
	parts := []string{product.Title, product.Description, product.Category, product.Item,
		product.Format, product.Style, product.Author}
	parts = append(parts, product.Tags...)
	keys := make([]string, 0, len(product.Attributes))
	for key := range product.Attributes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, key)
		parts = append(parts, attributeValueStrings(product.Attributes[key])...)
	}
	return tokenizeDiscoveryText(strings.Join(parts, " "))
}

func tokenizeDiscoveryText(value string) []string {
	var tokens []string
	var word strings.Builder
	flushWord := func() {
		if word.Len() == 0 {
			return
		}
		tokens = append(tokens, strings.ToLower(word.String()))
		word.Reset()
	}
	runes := []rune(strings.ToLower(value))
	for index := 0; index < len(runes); {
		current := runes[index]
		if unicode.Is(unicode.Han, current) {
			flushWord()
			end := index + 1
			for end < len(runes) && unicode.Is(unicode.Han, runes[end]) {
				end++
			}
			if end-index == 1 {
				tokens = append(tokens, string(current))
			} else {
				for offset := index; offset+1 < end; offset++ {
					tokens = append(tokens, string(runes[offset:offset+2]))
				}
			}
			index = end
			continue
		}
		if unicode.IsLetter(current) || unicode.IsDigit(current) {
			word.WriteRune(current)
		} else {
			flushWord()
		}
		index++
	}
	flushWord()
	return tokens
}

func uniqueTerms(terms []string) []string {
	seen := make(map[string]struct{}, len(terms))
	result := make([]string, 0, len(terms))
	for _, term := range terms {
		if _, exists := seen[term]; exists {
			continue
		}
		seen[term] = struct{}{}
		result = append(result, term)
	}
	return result
}

func discoveryMatchType(product Product, intent Intent) string {
	if product.Status != "active" || product.Currency != "USDC" || product.Category != intent.Category ||
		product.Format != intent.RequiredFormat {
		return ""
	}
	if product.Item == intent.Item {
		return MatchTypeExact
	}
	if !intent.AllowSubstitution || curatedSubstitutionGroup(intent.Category, intent.Item, intent.RequiredFormat) == "" ||
		productSubstitutionGroup(product) != curatedSubstitutionGroup(intent.Category, intent.Item, intent.RequiredFormat) {
		return ""
	}
	return MatchTypeSubstitute
}

func discoveryTextScore(product Product, intent Intent) int {
	title := strings.ToLower(product.Title)
	tags := strings.ToLower(strings.Join(product.Tags, " "))
	score := 0
	for _, term := range []struct {
		value  string
		weight int
	}{{intent.Item, 4}, {intent.RequiredFormat, 3}, {intent.PreferredStyle, 1}} {
		if term.value == "" {
			continue
		}
		needle := strings.ToLower(term.value)
		if strings.Contains(title, needle) {
			score += term.weight
		} else if strings.Contains(tags, needle) {
			score += term.weight / 2
		}
	}
	return score
}
