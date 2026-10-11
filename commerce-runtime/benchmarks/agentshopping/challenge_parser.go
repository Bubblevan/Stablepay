package agentshopping

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ReplayChallengeBatchResults revalidates stored structured model outputs
// against the current deterministic normalization and challenge rules. It
// makes no model calls and preserves each original RawIntent for provenance.
func ReplayChallengeBatchResults(requests []BatchIntentRequest, previous []BatchIntentResult,
	taxonomy Taxonomy) ([]BatchIntentResult, error) {
	if len(requests) == 0 || len(requests) > 500 || len(previous) != len(requests) {
		return nil, ErrInvalidSimulationData
	}
	requestByID := make(map[string]BatchIntentRequest, len(requests))
	for _, request := range requests {
		if strings.TrimSpace(request.TaskID) == "" || strings.TrimSpace(request.Prompt) == "" ||
			request.BuyerBudgetMinor <= 0 || !allowedValue(taxonomy.Regions, request.DefaultRegion) {
			return nil, ErrInvalidSimulationData
		}
		if _, exists := requestByID[request.TaskID]; exists {
			return nil, fmt.Errorf("replay requests contain duplicate task %q", request.TaskID)
		}
		requestByID[request.TaskID] = request
	}
	previousByID := make(map[string]BatchIntentResult, len(previous))
	for _, result := range previous {
		if _, ok := requestByID[result.TaskID]; !ok {
			return nil, fmt.Errorf("stored parse results contain unknown task %q", result.TaskID)
		}
		if _, exists := previousByID[result.TaskID]; exists {
			return nil, fmt.Errorf("stored parse results contain duplicate task %q", result.TaskID)
		}
		previousByID[result.TaskID] = result
	}

	replayed := make([]BatchIntentResult, 0, len(requests))
	for _, request := range requests {
		result, ok := previousByID[request.TaskID]
		if !ok {
			return nil, fmt.Errorf("stored parse results are missing task %q", request.TaskID)
		}
		result.Intent = Intent{}
		result.CanonicalizedFields = nil
		if result.RawIntent == nil {
			if result.Error == "" {
				result.Error = "MISSING_RAW_INTENT"
			}
			replayed = append(replayed, result)
			continue
		}
		raw := *result.RawIntent
		raw.Category = strings.TrimSpace(raw.Category)
		raw.Item = strings.TrimSpace(raw.Item)
		raw.RequiredFormat = strings.TrimSpace(raw.RequiredFormat)
		raw.PreferredStyle = strings.TrimSpace(raw.PreferredStyle)
		raw.Region = strings.TrimSpace(raw.Region)
		result.Error = "INVALID_INTENT"

		var allowSubstitution *bool
		if result.AllowSubstitutionSpecified {
			value := raw.AllowSubstitution
			allowSubstitution = &value
		}
		row := hermesBatchIntentResponse{TaskID: result.TaskID, Category: raw.Category, Item: raw.Item,
			RequiredFormat: raw.RequiredFormat, PreferredStyle: raw.PreferredStyle,
			RequiredAttributes: raw.RequiredAttributes, Region: raw.Region, MaxPriceMinor: raw.MaxPriceMinor,
			AllowSubstitution: allowSubstitution}
		explicitFormatRecovered := false
		if row.RequiredFormat == "" {
			if explicitFormat, ok := recoverExplicitRequiredFormat(request.Prompt, taxonomy); ok {
				row.RequiredFormat = explicitFormat
				explicitFormatRecovered = true
			}
		}
		intent, canonicalized, validationErr := validateParsedBatchIntent(row, request.DefaultRegion,
			request.BuyerBudgetMinor, taxonomy)
		if validationErr == nil {
			result.Intent = intent
			if explicitFormatRecovered {
				canonicalized = append(canonicalized, "required_format_from_explicit_request")
			}
			result.CanonicalizedFields = canonicalized
			result.Error = ""
			replayed = append(replayed, result)
			continue
		}
		if result.AllowSubstitutionSpecified {
			partial, partialFields, partialErr := normalizePartialChallengeIntent(raw, request, taxonomy)
			if partialErr == nil {
				result.Intent = partial
				result.CanonicalizedFields = partialFields
				result.Error = ""
			}
		}
		if result.Error == "" {
			result.Intent.searchText = request.Prompt
		}
		replayed = append(replayed, result)
	}
	return replayed, nil
}

// ParseChallengeBatchWithUsage preserves the historical complete-intent
// parser contract while allowing the challenge evaluator to grade explicit
// missing-field clarifications. Only a parse result rejected as INVALID_INTENT
// may be reinterpreted, and only when its raw output is otherwise valid and
// explicitly includes allow_substitution.
func (p HermesIntentParser) ParseChallengeBatchWithUsage(ctx context.Context, requests []BatchIntentRequest,
	taxonomy Taxonomy) ([]BatchIntentResult, ParseUsage, error) {
	results, usage, err := p.ParseBatchWithUsage(ctx, requests, taxonomy)
	if err != nil {
		return results, usage, err
	}
	requestByID := make(map[string]BatchIntentRequest, len(requests))
	for _, request := range requests {
		requestByID[request.TaskID] = request
	}
	for index := range results {
		result := &results[index]
		if result.Error != "INVALID_INTENT" || result.RawIntent == nil || !result.AllowSubstitutionSpecified {
			continue
		}
		request, ok := requestByID[result.TaskID]
		if !ok {
			return results, usage, fmt.Errorf("challenge parser returned unknown task %q", result.TaskID)
		}
		intent, canonicalized, normalizeErr := normalizePartialChallengeIntent(*result.RawIntent, request, taxonomy)
		if normalizeErr != nil {
			continue
		}
		result.Intent = intent
		result.CanonicalizedFields = canonicalized
		result.Error = ""
	}
	for index := range results {
		if results[index].Error != "" {
			continue
		}
		if request, ok := requestByID[results[index].TaskID]; ok {
			results[index].Intent.searchText = request.Prompt
		}
	}
	return results, usage, nil
}

func normalizePartialChallengeIntent(raw Intent, request BatchIntentRequest, taxonomy Taxonomy) (Intent, []string, error) {
	if strings.TrimSpace(request.TaskID) == "" || request.BuyerBudgetMinor <= 0 ||
		!allowedValue(taxonomy.Regions, request.DefaultRegion) {
		return Intent{}, nil, ErrInvalidSimulationData
	}
	intent := raw
	intent.Category = strings.TrimSpace(intent.Category)
	intent.Item = strings.TrimSpace(intent.Item)
	intent.RequiredFormat = strings.TrimSpace(intent.RequiredFormat)
	intent.PreferredStyle = strings.TrimSpace(intent.PreferredStyle)
	intent.Region = strings.TrimSpace(intent.Region)
	if intent.Region == "" {
		intent.Region = request.DefaultRegion
	}
	if region, ok := mapRegion(intent.Region); ok {
		intent.Region = region
	}
	if intent.MaxPriceMinor == 0 {
		intent.MaxPriceMinor = request.BuyerBudgetMinor
	}
	if intent.MaxPriceMinor <= 0 || intent.MaxPriceMinor > request.BuyerBudgetMinor ||
		!allowedValue(taxonomy.Regions, intent.Region) {
		return Intent{}, nil, errors.New("partial intent has an invalid region or budget")
	}
	intent, canonicalized := canonicalizeIntentFromTaxonomy(intent, taxonomy)
	if intent.RequiredFormat == "" {
		if explicitFormat, ok := recoverExplicitRequiredFormat(request.Prompt, taxonomy); ok {
			intent.RequiredFormat = explicitFormat
			canonicalized = append(canonicalized, "required_format_from_explicit_request")
		}
	}
	for _, field := range []struct {
		name  string
		value string
		set   []string
	}{
		{"category", intent.Category, taxonomy.Categories},
		{"item", intent.Item, taxonomy.Items},
		{"required_format", intent.RequiredFormat, taxonomy.Formats},
		{"preferred_style", intent.PreferredStyle, taxonomy.Styles},
	} {
		if field.value != "" && !allowedValue(field.set, field.value) {
			return Intent{}, nil, fmt.Errorf("partial intent contains unsupported %s", field.name)
		}
	}
	if !requiredAttributesAllowed(intent.Category, intent.RequiredAttributes, taxonomy) {
		return Intent{}, nil, errors.New("partial intent contains unsupported hard product attributes")
	}
	if len(missingIntentClarificationFields(intent)) == 0 {
		return Intent{}, nil, errors.New("partial intent is complete and should not be converted to clarification")
	}
	intent = normalizeIntentClarification(intent)
	intent.ConfirmationNeeded = true
	return intent, canonicalized, nil
}
