package agentshopping

const (
	MatchTypeExact      = "EXACT"
	MatchTypeSubstitute = "SUBSTITUTE"

	coffeeSingleOriginGroup = "coffee-single-origin-same-format-v1"
)

// curatedSubstitutionGroup is benchmark-only, reviewed synthetic taxonomy.
// It deliberately covers one narrow equivalence family: different single-
// origin coffee items in the same required format. Other categories do not
// get fuzzy same-category substitutions.
func curatedSubstitutionGroup(category, item, format string) string {
	if category != "coffee" || format != "单品豆" {
		return ""
	}
	switch item {
	case "耶加雪菲", "曼特宁", "哥伦比亚", "危地马拉", "云南日晒", "巴西黄波旁":
		return coffeeSingleOriginGroup
	default:
		return ""
	}
}

func productSubstitutionGroup(product Product) string {
	curated := curatedSubstitutionGroup(product.Category, product.Item, product.Format)
	if curated == "" || (product.SubstitutionGroup != "" && product.SubstitutionGroup != curated) {
		return ""
	}
	return curated
}

func candidateMatchType(product Product, intent Intent) string {
	if !requiredAttributesMatch(product.Attributes, intent.RequiredAttributes) {
		return ""
	}
	if product.Category == intent.Category && product.Item == intent.Item && product.Format == intent.RequiredFormat {
		return MatchTypeExact
	}
	if !intent.AllowSubstitution || product.Category != intent.Category || product.Format != intent.RequiredFormat {
		return ""
	}
	requestedGroup := curatedSubstitutionGroup(intent.Category, intent.Item, intent.RequiredFormat)
	if requestedGroup == "" || productSubstitutionGroup(product) != requestedGroup {
		return ""
	}
	return MatchTypeSubstitute
}

func substitutionNotice(requestedItem, productItem string) string {
	return "替代商品：请求的“" + requestedItem + "”将由“" + productItem + "”替代；价格和库存已重新核验，仍需买家明确确认。"
}

func applyBenchmarkSubstitutionMetadata(product *Product) {
	if product == nil {
		return
	}
	product.SubstitutionGroup = curatedSubstitutionGroup(product.Category, product.Item, product.Format)
}

func matchTypePriority(matchType string) int {
	if matchType == MatchTypeExact {
		return 2
	}
	if matchType == MatchTypeSubstitute {
		return 1
	}
	return 0
}
