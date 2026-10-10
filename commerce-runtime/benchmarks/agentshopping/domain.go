// Package agentshopping contains benchmark-only shopping domain types and
// deterministic fixture construction. It is deliberately separate from the
// production Merchant and payment service packages.
package agentshopping

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
)

var Regions = []string{"CN-EAST", "CN-NORTH", "CN-SOUTH", "CN-WEST"}

var formatAliases = map[string][]string{
	"挂耳装": {"单杯挂耳小包", "挂耳咖啡袋"}, "电子版": {"电子书文件", "数字版"},
	"视频课": {"视频课程", "录播课"}, "Figma 文件": {"Figma 设计文件", "可编辑 Figma 源文件"},
	"入门额度": {"开发入门调用额度", "基础 API 额度"}, "WAV 套装": {"WAV 音频包", "无损 WAV 合集"},
}

var styleAliases = map[string][]string{
	"花果香": {"明显花香和果香", "花香果香调"}, "入门精选": {"新手友好", "适合初学者"},
	"项目实战": {"通过项目动手实践", "实战导向"}, "极简风": {"简洁极简视觉", "干净克制"},
	"轻爵士": {"轻松爵士氛围", "柔和爵士"}, "自然光": {"自然采光质感", "自然光影"},
}

// IntentAliasHints returns copies of the benchmark's explicit synthetic alias dictionary.
func IntentAliasHints() (map[string][]string, map[string][]string) {
	clone := func(source map[string][]string) map[string][]string {
		result := make(map[string][]string, len(source))
		for canonical, aliases := range source {
			result[canonical] = append([]string(nil), aliases...)
		}
		return result
	}
	return clone(formatAliases), clone(styleAliases)
}

type Merchant struct {
	ID           string `json:"merchant_id"`
	Name         string `json:"name"`
	Category     string `json:"category"`
	Port         int    `json:"port"`
	ProductCount int    `json:"product_count"`
	ProductsURL  string `json:"products_url"`
}

type Product struct {
	ID          string         `json:"id"`
	SKUID       string         `json:"sku_id"`
	MerchantID  string         `json:"merchant_id"`
	Category    string         `json:"category"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	ImageURL    string         `json:"image_url,omitempty"`
	Price       string         `json:"price"`
	PriceMinor  int64          `json:"price_minor"`
	Currency    string         `json:"currency"`
	Author      string         `json:"author,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	Attributes  map[string]any `json:"attributes,omitempty"`
	Status      string         `json:"status"`
	Item        string         `json:"item"`
	Format      string         `json:"format"`
	Style       string         `json:"style"`
}

type OverlayEntry struct {
	MerchantID string `json:"merchant_id"`
	SKUID      string `json:"sku_id"`
	Region     string `json:"region"`
	OnHand     int    `json:"on_hand"`
	Eligible   bool   `json:"eligible"`
	PriceMinor int64  `json:"price_minor"`
	Revision   int64  `json:"revision"`
}

type Intent struct {
	Category              string `json:"category"`
	Item                  string `json:"item"`
	RequiredFormat        string `json:"required_format"`
	PreferredStyle        string `json:"preferred_style"`
	Region                string `json:"region"`
	MaxPriceMinor         int64  `json:"max_price_minor"`
	AllowSubstitution     bool   `json:"allow_substitution"`
	ConfirmationNeeded    bool   `json:"confirmation_required"`
	ClarificationRequired bool   `json:"clarification_required,omitempty"`
	ClarificationQuestion string `json:"clarification_question,omitempty"`
}

type Task struct {
	TaskID           string   `json:"task_id"`
	SourceMerchantID string   `json:"source_merchant_id"`
	Scenario         string   `json:"scenario"`
	Prompt           string   `json:"prompt"`
	Intent           Intent   `json:"gold_intent"`
	GoldSKUs         []string `json:"gold_skus"`
	Mutation         string   `json:"pre_confirmation_change"`
	UniqueTarget     string   `json:"unique_gold_target"`
}

type ParsedIntent struct {
	TaskID               string `json:"task_id"`
	Category             string `json:"category"`
	Item                 string `json:"item"`
	RequiredFormat       string `json:"required_format"`
	PreferredStyle       string `json:"preferred_style"`
	Region               string `json:"region"`
	MaxPriceMinor        int64  `json:"max_price_minor"`
	AllowSubstitution    bool   `json:"allow_substitution"`
	ConfirmationRequired bool   `json:"confirmation_required"`
}

func OverlayKey(sku, region string) string { return sku + "\x00" + region }

func catalogProductKey(merchantID, skuID string) string { return merchantID + "\x00" + skuID }

func ParseProductTitle(title string) (item, format, style string, err error) {
	parts := strings.Split(title, " · ")
	if len(parts) < 4 {
		return "", "", "", fmt.Errorf("unexpected synthetic product title %q", title)
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2]), nil
}

// TaxonomyFromProducts builds the deterministic intent-parser vocabulary from
// the same product snapshot used for candidate discovery.
func TaxonomyFromProducts(products []Product) Taxonomy {
	categories, items, formats, styles := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	itemCategories := map[string]map[string]bool{}
	for _, product := range products {
		if product.Status != "active" {
			continue
		}
		categories[product.Category] = true
		items[product.Item] = true
		formats[product.Format] = true
		styles[product.Style] = true
		if itemCategories[product.Item] == nil {
			itemCategories[product.Item] = map[string]bool{}
		}
		itemCategories[product.Item][product.Category] = true
	}

	categoryByItem := make(map[string][]string, len(itemCategories))
	for item, values := range itemCategories {
		categoryByItem[item] = sortedStringSet(values)
	}
	formatAliases, styleAliases := IntentAliasHints()
	return Taxonomy{
		Regions: append([]string(nil), Regions...), Categories: sortedStringSet(categories),
		Items: sortedStringSet(items), Formats: sortedStringSet(formats), Styles: sortedStringSet(styles),
		ItemCategories: categoryByItem, FormatAliases: formatAliases, StyleAliases: styleAliases,
	}
}

func sortedStringSet(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func ParseMinorUnits(value string) (int64, error) {
	parts := strings.Split(strings.TrimSpace(value), ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, fmt.Errorf("invalid decimal money %q", value)
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole < 0 {
		return 0, fmt.Errorf("invalid decimal money %q", value)
	}
	fraction := "00"
	if len(parts) == 2 {
		fraction = parts[1]
		if len(fraction) == 1 {
			fraction += "0"
		}
		if len(fraction) != 2 {
			return 0, fmt.Errorf("money must have at most two decimal places: %q", value)
		}
	}
	cents, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil || cents < 0 {
		return 0, fmt.Errorf("invalid decimal money %q", value)
	}
	return whole*100 + cents, nil
}

func FormatMinorUnits(value int64) string {
	return fmt.Sprintf("%d.%02d", value/100, value%100)
}

func MakeOverlay(products []Product, seed int64) []OverlayEntry {
	entries := make([]OverlayEntry, 0, len(products)*len(Regions))
	for _, product := range products {
		for regionIndex, region := range Regions {
			digest := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%s", seed, product.SKUID, region)))
			// The fixture models stock availability (80%) and delivery coverage
			// (91%) independently, yielding a meaningful price-only baseline while
			// keeping the state assignment deterministic and auditable.
			inStock := int(digest[0])%100 < 80
			eligible := int(digest[1])%100 < 91
			qty := 0
			if inStock {
				qty = 1 + int(digest[2])%5
			}
			base := product.PriceMinor
			entries = append(entries, OverlayEntry{
				MerchantID: product.MerchantID,
				SKUID:      product.SKUID,
				Region:     region,
				OnHand:     qty,
				Eligible:   eligible,
				PriceMinor: base + int64(regionIndex*37),
				Revision:   1,
			})
		}
	}
	return entries
}

type productGroup struct {
	merchantID string
	category   string
	item       string
	format     string
	style      string
	products   []Product
}

func groupKey(merchantID, category, item, format, style string) string {
	return strings.Join([]string{merchantID, category, item, format, style}, "\x00")
}

func GenerateTasks(products []Product, overlay []OverlayEntry, seed int64, count int) ([]Task, error) {
	if count <= 0 {
		return nil, fmt.Errorf("task count must be positive")
	}
	overlayByKey := make(map[string]OverlayEntry, len(overlay))
	for _, state := range overlay {
		overlayByKey[OverlayKey(state.SKUID, state.Region)] = state
	}
	groupsByMerchant := make(map[string][]*productGroup)
	groupsByKey := make(map[string]*productGroup)
	for _, product := range products {
		key := groupKey(product.MerchantID, product.Category, product.Item, product.Format, product.Style)
		group := groupsByKey[key]
		if group == nil {
			group = &productGroup{merchantID: product.MerchantID, category: product.Category, item: product.Item, format: product.Format, style: product.Style}
			groupsByKey[key] = group
			groupsByMerchant[product.MerchantID] = append(groupsByMerchant[product.MerchantID], group)
		}
		group.products = append(group.products, product)
	}

	merchantIDs := make([]string, 0, len(groupsByMerchant))
	for id := range groupsByMerchant {
		merchantIDs = append(merchantIDs, id)
	}
	sort.Strings(merchantIDs)
	rng := rand.New(rand.NewSource(seed))
	for _, id := range merchantIDs {
		groups := groupsByMerchant[id]
		rng.Shuffle(len(groups), func(i, j int) { groups[i], groups[j] = groups[j], groups[i] })
	}

	regionsChinese := map[string]string{"CN-EAST": "华东", "CN-NORTH": "华北", "CN-SOUTH": "华南", "CN-WEST": "西部"}
	categoryNeed := make(map[string]int)
	for i, id := range merchantIDs {
		_ = i
		// Twelve balanced merchant profiles: the first eight contribute 42
		// tasks, the remaining four contribute 41.
		if len(merchantIDs) == 12 && i < 8 {
			categoryNeed[id] = 42
		} else if len(merchantIDs) == 12 {
			categoryNeed[id] = 41
		} else {
			categoryNeed[id] = (count + len(merchantIDs) - 1) / len(merchantIDs)
		}
	}

	tasks := make([]Task, 0, count)
	usedTargets := make(map[string]bool)
	usedGoldSets := make(map[string]bool)
	for merchantIndex, merchantID := range merchantIDs {
		needed := categoryNeed[merchantID]
		for groupIndex, group := range groupsByMerchant[merchantID] {
			if needed == 0 || len(tasks) >= count {
				break
			}
			regionOffset := int(seed+int64(merchantIndex*31+groupIndex*17)) % len(Regions)
			if regionOffset < 0 {
				regionOffset = -regionOffset
			}
			chosenRegion := ""
			gold := make([]Product, 0, len(group.products))
			minPrice := int64(0)
			for offset := 0; offset < len(Regions); offset++ {
				region := Regions[(regionOffset+offset)%len(Regions)]
				eligible := make([]Product, 0, len(group.products))
				for _, product := range group.products {
					state := overlayByKey[OverlayKey(product.SKUID, region)]
					if state.Eligible && state.OnHand > 0 {
						eligible = append(eligible, product)
					}
				}
				if len(eligible) == 0 {
					continue
				}
				chosenRegion = region
				gold = eligible
				minPrice = int64(^uint64(0) >> 1)
				for _, product := range eligible {
					price := overlayByKey[OverlayKey(product.SKUID, region)].PriceMinor
					if price < minPrice {
						minPrice = price
					}
				}
				break
			}
			if chosenRegion == "" {
				continue
			}
			budget := minPrice + maxInt64(100, minPrice/5)
			taskOrdinal := len(tasks) + 1
			scenario := "STANDARD"
			mutation := "NONE"
			switch taskOrdinal % 10 {
			case 0:
				scenario, mutation = "PRICE_CHANGE", "PRICE_INCREASE"
			case 5:
				scenario, mutation = "STOCK_CHANGE", "STOCK_LOSS"
			case 1:
				scenario = "FORMAT_ALIAS"
			case 2:
				scenario = "STYLE_ALIAS"
			case 3:
				scenario = "BUDGET_BOUNDARY"
				budget = minPrice
			case 4:
				scenario = "SOFT_PREFERENCE"
			}
			goldSKUs := make([]string, 0, len(gold))
			for _, product := range products {
				if product.Category != group.category || product.Item != group.item || product.Format != group.format || product.Style != group.style || product.Currency != "USDC" {
					continue
				}
				state, ok := overlayByKey[OverlayKey(product.SKUID, chosenRegion)]
				if ok && state.Eligible && state.OnHand > 0 && state.PriceMinor <= budget {
					goldSKUs = append(goldSKUs, product.SKUID)
				}
			}
			sort.Strings(goldSKUs)
			if len(goldSKUs) == 0 {
				continue
			}
			target := ""
			for _, candidate := range goldSKUs {
				if !usedTargets[candidate] {
					target = candidate
					break
				}
			}
			goldSetKey := strings.Join(goldSKUs, "\x00")
			if target == "" || usedTargets[target] || usedGoldSets[goldSetKey] {
				continue
			}
			usedTargets[target] = true
			usedGoldSets[goldSetKey] = true
			formatText := group.format
			if aliases := formatAliases[group.format]; len(aliases) > 0 && scenario == "FORMAT_ALIAS" {
				formatText = aliases[taskOrdinal%len(aliases)]
			}
			styleText := group.style
			if aliases := styleAliases[group.style]; len(aliases) > 0 && scenario == "STYLE_ALIAS" {
				styleText = aliases[taskOrdinal%len(aliases)]
			}
			budgetText := FormatMinorUnits(budget)
			prompt := fmt.Sprintf("请帮我在%s地区买一件%s，规格必须是%s，风格偏好%s，单件预算不超过%s USDC。只考虑当前有库存且能配送到该地区的商品；给我看清楚具体报价，必须等我明确确认后才能付款。允许同规格替代；若报价或库存变化，先重新报价并再次询问我。",
				regionsChinese[chosenRegion], group.item, formatText, styleText, budgetText)
			intent := Intent{Category: group.category, Item: group.item, RequiredFormat: group.format,
				PreferredStyle: group.style, Region: chosenRegion, MaxPriceMinor: budget,
				AllowSubstitution: true, ConfirmationNeeded: true}
			tasks = append(tasks, Task{TaskID: fmt.Sprintf("shop-%04d", len(tasks)+1), SourceMerchantID: merchantID,
				Scenario: scenario, Prompt: prompt, Intent: intent, GoldSKUs: goldSKUs,
				Mutation: mutation, UniqueTarget: target})
			needed--
		}
	}
	if len(tasks) != count {
		return nil, fmt.Errorf("generated %d/%d feasible unique tasks from %d products; not enough viable profiles", len(tasks), count, len(products))
	}
	return tasks, nil
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func StableID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:16])
}

func Matches(product Product, intent Intent) bool {
	return product.Category == intent.Category && product.Item == intent.Item &&
		product.Format == intent.RequiredFormat
}

func RankCandidates(products []Product, intent Intent, state map[string]OverlayEntry, stockAware bool, limit int) []Product {
	candidates := make([]Product, 0, 32)
	for _, product := range products {
		if !Matches(product, intent) || product.Currency != "USDC" {
			continue
		}
		offer, ok := state[OverlayKey(product.SKUID, intent.Region)]
		if !ok || offer.PriceMinor > intent.MaxPriceMinor {
			continue
		}
		if stockAware && (!offer.Eligible || offer.OnHand <= 0) {
			continue
		}
		candidates = append(candidates, product)
	}
	sort.Slice(candidates, func(i, j int) bool {
		left, right := state[OverlayKey(candidates[i].SKUID, intent.Region)], state[OverlayKey(candidates[j].SKUID, intent.Region)]
		leftPreferred := candidates[i].Style == intent.PreferredStyle
		rightPreferred := candidates[j].Style == intent.PreferredStyle
		if leftPreferred != rightPreferred {
			return leftPreferred
		}
		if left.PriceMinor != right.PriceMinor {
			return left.PriceMinor < right.PriceMinor
		}
		return candidates[i].SKUID < candidates[j].SKUID
	})
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates
}
