package catalog

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/contract"
)

// BuildCandidateSet applies all hard eligibility rules before ranking. The
// returned set contains the exact catalog facts needed by later runtime
// guards, so later catalog mutation cannot change this result.
func BuildCandidateSet(candidateSetID, episodeID, requestID string, query DiscoveryQuery, capabilities []*MerchantCapability, generatedAt, expiresAt time.Time) (*CandidateSet, error) {
	query = query.Normalize()
	if err := query.Validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(candidateSetID) == "" || strings.TrimSpace(episodeID) == "" || strings.TrimSpace(requestID) == "" || generatedAt.IsZero() || expiresAt.IsZero() || !expiresAt.After(generatedAt) || expiresAt.After(query.Deadline) {
		return nil, ErrInvalidCandidateSet
	}
	queryHash, err := query.SnapshotHash()
	if err != nil {
		return nil, err
	}
	result := &CandidateSet{CandidateSetID: strings.TrimSpace(candidateSetID), EpisodeID: strings.TrimSpace(episodeID), RequestID: strings.TrimSpace(requestID), QueryHash: queryHash,
		GeneratedAt: generatedAt.UTC().Truncate(time.Nanosecond), ExpiresAt: expiresAt.UTC().Truncate(time.Nanosecond), FactsRef: "candidate-set://" + strings.TrimSpace(candidateSetID)}
	for _, capability := range capabilities {
		if capability == nil {
			continue
		}
		normalized := capability.Normalize()
		if err := normalized.Validate(); err != nil {
			return nil, fmt.Errorf("%w: catalog entry %s/%s: %v", ErrInvalidCapability, normalized.MerchantDID, normalized.CapabilityID, err)
		}
		facts, rank := evaluate(normalized, query, generatedAt)
		if !facts.Eligible() {
			continue
		}
		hash, err := normalized.SnapshotHash()
		if err != nil {
			return nil, err
		}
		result.Candidates = append(result.Candidates, Candidate{MerchantDID: normalized.MerchantDID, CapabilityID: normalized.CapabilityID, PayeeDID: normalized.PayeeDID,
			CatalogVersion: normalized.CatalogVersion, CatalogSnapshotHash: hash, CatalogSnapshotRef: normalized.SnapshotRef(), Eligibility: facts, RankFeatures: rank})
	}
	*result = result.Normalize()
	payloadHash, err := result.PayloadHashFor()
	if err != nil {
		return nil, err
	}
	result.PayloadHash = payloadHash
	if err := result.Validate(); err != nil {
		return nil, err
	}
	return result, nil
}

func evaluate(capability MerchantCapability, query DiscoveryQuery, now time.Time) (EligibilityFacts, RankFeatures) {
	facts := EligibilityFacts{StatusActive: capability.Status == StatusActive && capability.Availability == AvailabilityAvailable,
		NotExpired:                    !now.Before(capability.ValidFrom) && now.Before(capability.ValidUntil),
		TaskTypeCompatible:            contains(capability.TaskTypes, query.TaskType),
		InputContentTypeCompatible:    contentTypeCompatible(capability.InputContentTypes, query.InputContentType),
		OutputContentTypeCompatible:   contentTypeCompatible(capability.OutputContentTypes, query.ExpectedOutputContentType),
		ProtocolCompatible:            intersection(capability.SupportedProtocolVersions, query.SupportedProtocolVersions),
		CurrencyCompatible:            contains(capability.SupportedCurrencies, query.Currency),
		PriceHintPresent:              capability.PriceHintMinor != nil,
		PriceHintWithinBudget:         capability.PriceHintMinor == nil,
		SemanticConstraintsCompatible: semanticCompatible(capability.SemanticTags, query.SemanticConstraints)}
	if capability.PriceHintMinor != nil {
		facts.PriceHintWithinBudget = capability.PriceHintCurrency == query.Currency && *capability.PriceHintMinor <= query.BudgetLimitMinor
	}
	if !facts.StatusActive {
		facts.Reasons = append(facts.Reasons, "inactive_or_unavailable")
	}
	if !facts.NotExpired {
		facts.Reasons = append(facts.Reasons, "expired_or_not_yet_valid")
	}
	if !facts.TaskTypeCompatible {
		facts.Reasons = append(facts.Reasons, "task_type_mismatch")
	}
	if !facts.InputContentTypeCompatible {
		facts.Reasons = append(facts.Reasons, "input_content_type_mismatch")
	}
	if !facts.OutputContentTypeCompatible {
		facts.Reasons = append(facts.Reasons, "output_content_type_mismatch")
	}
	if !facts.ProtocolCompatible {
		facts.Reasons = append(facts.Reasons, "protocol_mismatch")
	}
	if !facts.CurrencyCompatible {
		facts.Reasons = append(facts.Reasons, "currency_mismatch")
	}
	if !facts.PriceHintWithinBudget {
		facts.Reasons = append(facts.Reasons, "price_hint_over_budget")
	}
	if !facts.SemanticConstraintsCompatible {
		facts.Reasons = append(facts.Reasons, "semantic_constraint_mismatch")
	}
	protocolPreference := 0
	for index, preferred := range query.SupportedProtocolVersions {
		if contains(capability.SupportedProtocolVersions, preferred) {
			protocolPreference = len(query.SupportedProtocolVersions) - index
			break
		}
	}
	outputCompatibility := 0
	if contains(capability.OutputContentTypes, query.ExpectedOutputContentType) {
		outputCompatibility = 2
	} else if contentTypeCompatible(capability.OutputContentTypes, query.ExpectedOutputContentType) {
		outputCompatibility = 1
	}
	var price *int64
	if capability.PriceHintMinor != nil {
		value := *capability.PriceHintMinor
		price = &value
	}
	return facts, RankFeatures{ProtocolPreference: protocolPreference, OutputCompatibility: outputCompatibility, PriceHintKnown: capability.PriceHintMinor != nil, PriceHintMinor: price,
		StableTieBreaker: capability.MerchantDID + "\x00" + capability.CapabilityID + "\x00" + capability.CatalogVersion}
}

func intersection(left, right []string) bool {
	for _, value := range left {
		if contains(right, value) {
			return true
		}
	}
	return false
}

func contentTypeCompatible(supported []string, requested string) bool {
	requested = strings.ToLower(strings.TrimSpace(requested))
	if requested == "*/*" {
		return len(supported) > 0
	}
	for _, value := range supported {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == requested || value == "*/*" {
			return true
		}
		if strings.HasSuffix(value, "/*") && strings.HasPrefix(requested, strings.TrimSuffix(value, "*")) {
			return true
		}
	}
	return false
}

func semanticCompatible(tags []string, constraints []contract.KeyValue) bool {
	if len(constraints) == 0 {
		return true
	}
	for _, constraint := range constraints {
		key := strings.ToLower(strings.TrimSpace(constraint.Key))
		value := strings.ToLower(strings.TrimSpace(constraint.Value))
		matched := false
		for _, tag := range tags {
			tag = strings.ToLower(strings.TrimSpace(tag))
			if tag == key+"="+value || tag == key+":"+value || (value == "" && tag == key) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// RankCandidates is exported for callers that already have an eligible list.
// It uses only stable fields and never reads the wall clock.
func RankCandidates(candidates []Candidate) []Candidate {
	result := append([]Candidate(nil), candidates...)
	sort.SliceStable(result, func(i, j int) bool { return candidateLess(result[i], result[j]) })
	return result
}
