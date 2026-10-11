package agentshopping

import (
	"fmt"
	"strconv"
	"strings"
)

const maxChallengeAttributeValues = 64

// CategoryAttributeValues exposes only bounded, observed categorical values
// from the pinned synthetic catalog. High-cardinality numeric fields are
// omitted so the parser cannot invent arbitrary operators or ranges.
func CategoryAttributeValues(products []Product) map[string]map[string][]string {
	values := make(map[string]map[string]map[string]bool)
	for _, product := range products {
		if product.Status != "active" {
			continue
		}
		if values[product.Category] == nil {
			values[product.Category] = make(map[string]map[string]bool)
		}
		for key, raw := range product.Attributes {
			if key == "data_origin" || key == "synthetic_notice" || key == "substitution_group" || key == "substitution_policy" {
				continue
			}
			items := attributeValueStrings(raw)
			if len(items) == 0 {
				continue
			}
			if values[product.Category][key] == nil {
				values[product.Category][key] = make(map[string]bool)
			}
			for _, item := range items {
				values[product.Category][key][item] = true
			}
		}
	}
	result := make(map[string]map[string][]string, len(values))
	for category, byKey := range values {
		result[category] = make(map[string][]string)
		for key, set := range byKey {
			if len(set) > maxChallengeAttributeValues {
				continue
			}
			result[category][key] = sortedStringSet(set)
		}
	}
	return result
}

func requiredAttributesAllowed(category string, required map[string]string, taxonomy Taxonomy) bool {
	if len(required) == 0 {
		return true
	}
	allowed := taxonomy.CategoryAttributeValues[category]
	if len(allowed) == 0 {
		return false
	}
	for key, value := range required {
		values, ok := allowed[key]
		if !ok || strings.TrimSpace(value) == "" || !allowedValue(values, value) {
			return false
		}
	}
	return true
}

func requiredAttributesMatch(productAttributes map[string]any, required map[string]string) bool {
	for key, expected := range required {
		found := false
		for _, actual := range attributeValueStrings(productAttributes[key]) {
			if strings.EqualFold(strings.TrimSpace(actual), strings.TrimSpace(expected)) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func sameRequiredAttributes(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func attributeValueStrings(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return []string{typed}
	case []string:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if strings.TrimSpace(item) != "" {
				result = append(result, item)
			}
		}
		return result
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			result = append(result, attributeValueStrings(item)...)
		}
		return result
	case float64:
		return []string{strconv.FormatFloat(typed, 'f', -1, 64)}
	case float32:
		return []string{strconv.FormatFloat(float64(typed), 'f', -1, 32)}
	case int:
		return []string{strconv.Itoa(typed)}
	case int64:
		return []string{strconv.FormatInt(typed, 10)}
	case int32:
		return []string{strconv.FormatInt(int64(typed), 10)}
	case bool:
		return []string{strconv.FormatBool(typed)}
	default:
		return []string{fmt.Sprint(typed)}
	}
}
