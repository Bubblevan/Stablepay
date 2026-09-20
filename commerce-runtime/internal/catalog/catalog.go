// Package catalog contains the trusted merchant capability facts used by
// structured discovery. Catalog records are facts and references; they are
// never prompt text and never contain executable behavior.
package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/contract"
)

const (
	StatusActive     CapabilityStatus = "ACTIVE"
	StatusInactive   CapabilityStatus = "INACTIVE"
	StatusDeprecated CapabilityStatus = "DEPRECATED"

	AvailabilityAvailable   Availability = "AVAILABLE"
	AvailabilityUnavailable Availability = "UNAVAILABLE"
	AvailabilityUnknown     Availability = "UNKNOWN"

	CapabilityStatusActive      = StatusActive
	CapabilityStatusInactive    = StatusInactive
	CapabilityStatusDeprecated  = StatusDeprecated
	CapabilityAvailabilityReady = AvailabilityAvailable
)

type CapabilityStatus string
type Availability string

var (
	ErrInvalidCapability      = errors.New("invalid merchant capability")
	ErrInvalidCapabilityID    = errors.New("capability_id must be a stable non-DID identifier")
	ErrInvalidEndpoint        = errors.New("invalid capability endpoint reference")
	ErrInvalidCatalogVersion  = errors.New("catalog_version is required")
	ErrInvalidCatalogValidity = errors.New("catalog validity interval is invalid")
	ErrInvalidPriceHint       = errors.New("invalid price hint")
	ErrInvalidSnapshotHash    = errors.New("invalid catalog snapshot hash")
	ErrInvalidDiscoveryQuery  = errors.New("invalid discovery query")
	ErrInvalidCandidateSet    = errors.New("invalid candidate set")
	ErrCandidateSetExpired    = errors.New("candidate set has expired")
	ErrCatalogSnapshotExpired = errors.New("catalog snapshot has expired")
	// ErrCandidateExpired is kept as a domain-level alias for callers that
	// describe the frozen catalog fact as a candidate.
	ErrCandidateExpired     = ErrCatalogSnapshotExpired
	ErrCandidateNotEligible = errors.New("candidate is not structurally eligible")
)

var capabilityIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,127}$`)
var digestPattern = regexp.MustCompile(`^(?:sha256:)?[a-fA-F0-9]{64}$`)

// EndpointRef is deliberately structured. Ref is an inert registry/object
// reference; Endpoint is an optional URL that a later runtime stage may use.
// S3 stores the reference but does not invoke it.
type EndpointRef struct {
	Endpoint string `json:"endpoint,omitempty"`
	Ref      string `json:"ref,omitempty"`
	Method   string `json:"method,omitempty"`
}

func (e EndpointRef) Normalize() EndpointRef {
	e.Endpoint = strings.TrimSpace(e.Endpoint)
	e.Ref = strings.TrimSpace(e.Ref)
	e.Method = strings.ToUpper(strings.TrimSpace(e.Method))
	return e
}

func (e EndpointRef) Validate(required bool) error {
	e = e.Normalize()
	if e.Endpoint == "" && e.Ref == "" {
		if required {
			return ErrInvalidEndpoint
		}
		return nil
	}
	if e.Endpoint != "" {
		u, err := url.Parse(e.Endpoint)
		if err != nil || u.Scheme == "" || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return ErrInvalidEndpoint
		}
	}
	if e.Method != "" {
		switch e.Method {
		case "GET", "POST", "PUT", "PATCH", "DELETE":
		default:
			return ErrInvalidEndpoint
		}
	}
	return nil
}

// MerchantCapability is one immutable version of a merchant's capability.
// PayeeDID is intentionally a separate field from MerchantDID and
// CapabilityID: the former is the payment recipient, not the capability key.
type MerchantCapability struct {
	MerchantDID               string           `json:"merchant_did"`
	CapabilityID              string           `json:"capability_id"`
	PayeeDID                  string           `json:"payee_did"`
	Name                      string           `json:"name"`
	Description               string           `json:"description"`
	TaskTypes                 []string         `json:"task_types"`
	SemanticTags              []string         `json:"semantic_tags"`
	InvokeEndpoint            EndpointRef      `json:"invoke_endpoint"`
	QuoteEndpoint             EndpointRef      `json:"quote_endpoint,omitempty"`
	InputSchemaRef            string           `json:"input_schema_ref,omitempty"`
	OutputSchemaRef           string           `json:"output_schema_ref,omitempty"`
	InputContentTypes         []string         `json:"input_content_types"`
	OutputContentTypes        []string         `json:"output_content_types"`
	SupportedProtocolVersions []string         `json:"supported_protocol_versions"`
	SupportedCurrencies       []string         `json:"supported_currencies"`
	PricingModel              string           `json:"pricing_model"`
	PriceHintMinor            *int64           `json:"price_hint_minor,omitempty"`
	PriceHintCurrency         string           `json:"price_hint_currency,omitempty"`
	Status                    CapabilityStatus `json:"status"`
	Availability              Availability     `json:"availability"`
	CatalogVersion            string           `json:"catalog_version"`
	Source                    string           `json:"source"`
	SourceRef                 string           `json:"source_ref,omitempty"`
	SourceHash                string           `json:"source_hash,omitempty"`
	ValidFrom                 time.Time        `json:"valid_from"`
	ValidUntil                time.Time        `json:"valid_until"`
	CreatedAt                 time.Time        `json:"created_at"`
	UpdatedAt                 time.Time        `json:"updated_at"`
}

func (c MerchantCapability) Normalize() MerchantCapability {
	c.MerchantDID = strings.TrimSpace(c.MerchantDID)
	c.CapabilityID = strings.ToLower(strings.TrimSpace(c.CapabilityID))
	c.PayeeDID = strings.TrimSpace(c.PayeeDID)
	c.Name = strings.TrimSpace(c.Name)
	c.Description = strings.TrimSpace(c.Description)
	c.TaskTypes = normalizeStrings(c.TaskTypes, true)
	c.SemanticTags = normalizeStrings(c.SemanticTags, true)
	c.InvokeEndpoint = c.InvokeEndpoint.Normalize()
	c.QuoteEndpoint = c.QuoteEndpoint.Normalize()
	c.InputSchemaRef = strings.TrimSpace(c.InputSchemaRef)
	c.OutputSchemaRef = strings.TrimSpace(c.OutputSchemaRef)
	c.InputContentTypes = normalizeStrings(c.InputContentTypes, true)
	c.OutputContentTypes = normalizeStrings(c.OutputContentTypes, true)
	c.SupportedProtocolVersions = normalizeStrings(c.SupportedProtocolVersions, true)
	c.SupportedCurrencies = normalizeStrings(c.SupportedCurrencies, false)
	c.PricingModel = strings.ToLower(strings.TrimSpace(c.PricingModel))
	c.PriceHintCurrency = strings.ToUpper(strings.TrimSpace(c.PriceHintCurrency))
	c.Status = CapabilityStatus(strings.ToUpper(strings.TrimSpace(string(c.Status))))
	c.Availability = Availability(strings.ToUpper(strings.TrimSpace(string(c.Availability))))
	c.CatalogVersion = strings.TrimSpace(c.CatalogVersion)
	c.Source = strings.TrimSpace(c.Source)
	c.SourceRef = strings.TrimSpace(c.SourceRef)
	c.SourceHash = strings.ToLower(strings.TrimSpace(c.SourceHash))
	// The durable MySQL schema stores catalog timestamps at millisecond
	// precision and rounds values on write. Canonical snapshots must use the
	// same rounding to remain stable after persistence and reload.
	c.ValidFrom = c.ValidFrom.UTC().Round(time.Millisecond)
	c.ValidUntil = c.ValidUntil.UTC().Round(time.Millisecond)
	c.CreatedAt = c.CreatedAt.UTC().Round(time.Millisecond)
	c.UpdatedAt = c.UpdatedAt.UTC().Round(time.Millisecond)
	if c.PriceHintMinor != nil {
		value := *c.PriceHintMinor
		c.PriceHintMinor = &value
	}
	return c
}

func normalizeStrings(values []string, lower bool) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if lower {
			value = strings.ToLower(value)
		}
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func (c MerchantCapability) Validate() error {
	c = c.Normalize()
	if c.MerchantDID == "" || c.PayeeDID == "" || c.Name == "" || c.Description == "" || c.Source == "" {
		return fmt.Errorf("%w: merchant/payee/name/description/source are required", ErrInvalidCapability)
	}
	if c.CapabilityID == "" || strings.HasPrefix(c.CapabilityID, "did:") || !capabilityIDPattern.MatchString(c.CapabilityID) {
		return ErrInvalidCapabilityID
	}
	if len(c.TaskTypes) == 0 || len(c.InputContentTypes) == 0 || len(c.OutputContentTypes) == 0 || len(c.SupportedProtocolVersions) == 0 || len(c.SupportedCurrencies) == 0 {
		return fmt.Errorf("%w: structured task/content/protocol/currency fields are required", ErrInvalidCapability)
	}
	if err := c.InvokeEndpoint.Validate(true); err != nil {
		return fmt.Errorf("%w: invoke endpoint: %v", ErrInvalidCapability, err)
	}
	if err := c.QuoteEndpoint.Validate(false); err != nil {
		return fmt.Errorf("%w: quote endpoint: %v", ErrInvalidCapability, err)
	}
	if c.InputSchemaRef == "" || c.OutputSchemaRef == "" {
		return fmt.Errorf("%w: input/output schema refs are required", ErrInvalidCapability)
	}
	if c.PriceHintMinor != nil {
		if *c.PriceHintMinor < 0 || c.PriceHintCurrency == "" {
			return ErrInvalidPriceHint
		}
		if !contains(c.SupportedCurrencies, c.PriceHintCurrency) {
			return fmt.Errorf("%w: hint currency is not supported", ErrInvalidPriceHint)
		}
	}
	if c.Status != StatusActive && c.Status != StatusInactive && c.Status != StatusDeprecated {
		return fmt.Errorf("%w: unknown status", ErrInvalidCapability)
	}
	if c.Availability != AvailabilityAvailable && c.Availability != AvailabilityUnavailable && c.Availability != AvailabilityUnknown {
		return fmt.Errorf("%w: unknown availability", ErrInvalidCapability)
	}
	if c.CatalogVersion == "" {
		return ErrInvalidCatalogVersion
	}
	if c.ValidFrom.IsZero() || c.ValidUntil.IsZero() || !c.ValidUntil.After(c.ValidFrom) {
		return ErrInvalidCatalogValidity
	}
	if c.CreatedAt.IsZero() || c.UpdatedAt.IsZero() || c.UpdatedAt.Before(c.CreatedAt) {
		return fmt.Errorf("%w: created/updated timestamps are invalid", ErrInvalidCapability)
	}
	if c.SourceHash != "" && !digestPattern.MatchString(c.SourceHash) {
		return ErrInvalidSnapshotHash
	}
	return nil
}

type canonicalCapability struct {
	MerchantDID               string           `json:"merchant_did"`
	CapabilityID              string           `json:"capability_id"`
	PayeeDID                  string           `json:"payee_did"`
	Name                      string           `json:"name"`
	Description               string           `json:"description"`
	TaskTypes                 []string         `json:"task_types"`
	SemanticTags              []string         `json:"semantic_tags"`
	InvokeEndpoint            EndpointRef      `json:"invoke_endpoint"`
	QuoteEndpoint             EndpointRef      `json:"quote_endpoint,omitempty"`
	InputSchemaRef            string           `json:"input_schema_ref"`
	OutputSchemaRef           string           `json:"output_schema_ref"`
	InputContentTypes         []string         `json:"input_content_types"`
	OutputContentTypes        []string         `json:"output_content_types"`
	SupportedProtocolVersions []string         `json:"supported_protocol_versions"`
	SupportedCurrencies       []string         `json:"supported_currencies"`
	PricingModel              string           `json:"pricing_model"`
	PriceHintMinor            *int64           `json:"price_hint_minor,omitempty"`
	PriceHintCurrency         string           `json:"price_hint_currency,omitempty"`
	Status                    CapabilityStatus `json:"status"`
	Availability              Availability     `json:"availability"`
	CatalogVersion            string           `json:"catalog_version"`
	Source                    string           `json:"source"`
	SourceRef                 string           `json:"source_ref,omitempty"`
	SourceHash                string           `json:"source_hash,omitempty"`
	ValidFrom                 time.Time        `json:"valid_from"`
	ValidUntil                time.Time        `json:"valid_until"`
	CreatedAt                 time.Time        `json:"created_at"`
	UpdatedAt                 time.Time        `json:"updated_at"`
}

func (c MerchantCapability) canonical() canonicalCapability {
	c = c.Normalize()
	return canonicalCapability{MerchantDID: c.MerchantDID, CapabilityID: c.CapabilityID, PayeeDID: c.PayeeDID, Name: c.Name, Description: c.Description,
		TaskTypes: c.TaskTypes, SemanticTags: c.SemanticTags, InvokeEndpoint: c.InvokeEndpoint, QuoteEndpoint: c.QuoteEndpoint,
		InputSchemaRef: c.InputSchemaRef, OutputSchemaRef: c.OutputSchemaRef, InputContentTypes: c.InputContentTypes, OutputContentTypes: c.OutputContentTypes,
		SupportedProtocolVersions: c.SupportedProtocolVersions, SupportedCurrencies: c.SupportedCurrencies, PricingModel: c.PricingModel,
		PriceHintMinor: c.PriceHintMinor, PriceHintCurrency: c.PriceHintCurrency, Status: c.Status, Availability: c.Availability,
		CatalogVersion: c.CatalogVersion, Source: c.Source, SourceRef: c.SourceRef, SourceHash: c.SourceHash, ValidFrom: c.ValidFrom,
		ValidUntil: c.ValidUntil, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
}

func (c MerchantCapability) CanonicalSnapshot() ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(c.canonical())
}

func (c MerchantCapability) SnapshotHash() (string, error) {
	snapshot, err := c.CanonicalSnapshot()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(snapshot)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func (c MerchantCapability) SnapshotRef() string {
	c = c.Normalize()
	return "catalog://" + c.MerchantDID + "/" + c.CapabilityID + "/" + c.CatalogVersion
}

func (c MerchantCapability) Clone() *MerchantCapability {
	c = c.Normalize()
	return &c
}

func (c MerchantCapability) IsActiveAt(now time.Time) bool {
	c = c.Normalize()
	return c.Status == StatusActive && c.Availability == AvailabilityAvailable && !now.Before(c.ValidFrom) && now.Before(c.ValidUntil)
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// CompareVersions provides a stable ordering for current-pointer selection.
// Numeric v-prefixed versions sort numerically; all other versions sort by
// their canonical string. Equal versions compare equal.
func CompareVersions(left, right string) int {
	left = strings.TrimSpace(strings.ToLower(left))
	right = strings.TrimSpace(strings.ToLower(right))
	if left == right {
		return 0
	}
	parse := func(value string) (uint64, bool) {
		value = strings.TrimPrefix(value, "v")
		if value == "" {
			return 0, false
		}
		n, err := strconv.ParseUint(value, 10, 64)
		return n, err == nil
	}
	ln, lok := parse(left)
	rn, rok := parse(right)
	if lok && rok {
		if ln < rn {
			return -1
		}
		return 1
	}
	if left < right {
		return -1
	}
	return 1
}

// CatalogSnapshotRef identifies the exact fact used by a candidate set.
type CatalogSnapshotRef struct {
	MerchantDID         string `json:"merchant_did"`
	CapabilityID        string `json:"capability_id"`
	CatalogVersion      string `json:"catalog_version"`
	CatalogSnapshotHash string `json:"catalog_snapshot_hash"`
}

type EligibilityFacts struct {
	StatusActive                  bool     `json:"status_active"`
	NotExpired                    bool     `json:"not_expired"`
	TaskTypeCompatible            bool     `json:"task_type_compatible"`
	InputContentTypeCompatible    bool     `json:"input_content_type_compatible"`
	OutputContentTypeCompatible   bool     `json:"output_content_type_compatible"`
	ProtocolCompatible            bool     `json:"protocol_compatible"`
	CurrencyCompatible            bool     `json:"currency_compatible"`
	PriceHintPresent              bool     `json:"price_hint_present"`
	PriceHintWithinBudget         bool     `json:"price_hint_within_budget"`
	SemanticConstraintsCompatible bool     `json:"semantic_constraints_compatible"`
	Reasons                       []string `json:"reasons,omitempty"`
}

func (f EligibilityFacts) Eligible() bool {
	return f.StatusActive && f.NotExpired && f.TaskTypeCompatible && f.InputContentTypeCompatible && f.OutputContentTypeCompatible && f.ProtocolCompatible && f.CurrencyCompatible && f.PriceHintWithinBudget && f.SemanticConstraintsCompatible
}

type RankFeatures struct {
	ProtocolPreference  int    `json:"protocol_preference"`
	OutputCompatibility int    `json:"output_compatibility"`
	PriceHintKnown      bool   `json:"price_hint_known"`
	PriceHintMinor      *int64 `json:"price_hint_minor,omitempty"`
	StableTieBreaker    string `json:"stable_tie_breaker"`
}

type Candidate struct {
	MerchantDID         string           `json:"merchant_did"`
	CapabilityID        string           `json:"capability_id"`
	PayeeDID            string           `json:"payee_did"`
	CatalogVersion      string           `json:"catalog_version"`
	CatalogSnapshotHash string           `json:"catalog_snapshot_hash"`
	CatalogSnapshotRef  string           `json:"catalog_snapshot_ref"`
	CatalogValidFrom    time.Time        `json:"catalog_valid_from"`
	CatalogValidUntil   time.Time        `json:"catalog_valid_until"`
	Eligibility         EligibilityFacts `json:"eligibility"`
	RankFeatures        RankFeatures     `json:"rank_features"`
}

func (c Candidate) SnapshotRef() CatalogSnapshotRef {
	return CatalogSnapshotRef{MerchantDID: c.MerchantDID, CapabilityID: c.CapabilityID, CatalogVersion: c.CatalogVersion, CatalogSnapshotHash: c.CatalogSnapshotHash}
}

type CandidateSet struct {
	CandidateSetID      string               `json:"candidate_set_id"`
	EpisodeID           string               `json:"episode_id"`
	RequestID           string               `json:"request_id"`
	Generation          int                  `json:"generation"`
	QueryHash           string               `json:"query_hash"`
	CatalogSnapshotRefs []CatalogSnapshotRef `json:"catalog_snapshot_refs"`
	Candidates          []Candidate          `json:"candidates"`
	GeneratedAt         time.Time            `json:"generated_at"`
	ExpiresAt           time.Time            `json:"expires_at"`
	FactsRef            string               `json:"facts_ref"`
	PayloadHash         string               `json:"payload_hash"`
}

func (s CandidateSet) Normalize() CandidateSet {
	s.CandidateSetID = strings.TrimSpace(s.CandidateSetID)
	s.EpisodeID = strings.TrimSpace(s.EpisodeID)
	s.RequestID = strings.TrimSpace(s.RequestID)
	if s.Generation < 0 {
		s.Generation = 0
	}
	s.QueryHash = strings.ToLower(strings.TrimSpace(s.QueryHash))
	// MySQL persists these facts at millisecond precision and rounds values on
	// write. Canonical catalog facts use that durable rounding so payload hashes
	// survive a DB roundtrip.
	s.GeneratedAt = s.GeneratedAt.UTC().Round(time.Millisecond)
	s.ExpiresAt = s.ExpiresAt.UTC().Round(time.Millisecond)
	s.FactsRef = strings.TrimSpace(s.FactsRef)
	s.PayloadHash = strings.ToLower(strings.TrimSpace(s.PayloadHash))
	s.Candidates = append([]Candidate(nil), s.Candidates...)
	for i := range s.Candidates {
		s.Candidates[i].MerchantDID = strings.TrimSpace(s.Candidates[i].MerchantDID)
		s.Candidates[i].CapabilityID = strings.ToLower(strings.TrimSpace(s.Candidates[i].CapabilityID))
		s.Candidates[i].PayeeDID = strings.TrimSpace(s.Candidates[i].PayeeDID)
		s.Candidates[i].CatalogVersion = strings.TrimSpace(s.Candidates[i].CatalogVersion)
		s.Candidates[i].CatalogSnapshotHash = strings.ToLower(strings.TrimSpace(s.Candidates[i].CatalogSnapshotHash))
		s.Candidates[i].CatalogSnapshotRef = strings.TrimSpace(s.Candidates[i].CatalogSnapshotRef)
		s.Candidates[i].CatalogValidFrom = s.Candidates[i].CatalogValidFrom.UTC().Round(time.Millisecond)
		s.Candidates[i].CatalogValidUntil = s.Candidates[i].CatalogValidUntil.UTC().Round(time.Millisecond)
		s.Candidates[i].Eligibility.Reasons = append([]string(nil), s.Candidates[i].Eligibility.Reasons...)
		if s.Candidates[i].RankFeatures.PriceHintMinor != nil {
			value := *s.Candidates[i].RankFeatures.PriceHintMinor
			s.Candidates[i].RankFeatures.PriceHintMinor = &value
		}
	}
	sort.Slice(s.Candidates, func(i, j int) bool { return candidateLess(s.Candidates[i], s.Candidates[j]) })
	s.CatalogSnapshotRefs = make([]CatalogSnapshotRef, 0, len(s.Candidates))
	for _, candidate := range s.Candidates {
		s.CatalogSnapshotRefs = append(s.CatalogSnapshotRefs, candidate.SnapshotRef())
	}
	return s
}

func candidateLess(left, right Candidate) bool {
	if left.RankFeatures.ProtocolPreference != right.RankFeatures.ProtocolPreference {
		return left.RankFeatures.ProtocolPreference > right.RankFeatures.ProtocolPreference
	}
	if left.RankFeatures.OutputCompatibility != right.RankFeatures.OutputCompatibility {
		return left.RankFeatures.OutputCompatibility > right.RankFeatures.OutputCompatibility
	}
	if left.RankFeatures.PriceHintKnown != right.RankFeatures.PriceHintKnown {
		return left.RankFeatures.PriceHintKnown
	}
	if left.RankFeatures.PriceHintKnown && right.RankFeatures.PriceHintKnown {
		if left.RankFeatures.PriceHintMinor == nil != (right.RankFeatures.PriceHintMinor == nil) {
			return left.RankFeatures.PriceHintMinor != nil
		}
		if left.RankFeatures.PriceHintMinor != nil && *left.RankFeatures.PriceHintMinor != *right.RankFeatures.PriceHintMinor {
			return *left.RankFeatures.PriceHintMinor < *right.RankFeatures.PriceHintMinor
		}
	}
	if left.MerchantDID != right.MerchantDID {
		return left.MerchantDID < right.MerchantDID
	}
	if left.CapabilityID != right.CapabilityID {
		return left.CapabilityID < right.CapabilityID
	}
	if left.CatalogVersion != right.CatalogVersion {
		return CompareVersions(left.CatalogVersion, right.CatalogVersion) > 0
	}
	return left.CatalogSnapshotHash < right.CatalogSnapshotHash
}

func (s CandidateSet) Validate() error {
	s = s.Normalize()
	if s.CandidateSetID == "" || s.EpisodeID == "" || s.RequestID == "" || s.Generation < 0 || s.QueryHash == "" || s.GeneratedAt.IsZero() || s.ExpiresAt.IsZero() || !s.ExpiresAt.After(s.GeneratedAt) || s.FactsRef == "" || s.PayloadHash == "" {
		return ErrInvalidCandidateSet
	}
	seen := make(map[string]struct{}, len(s.Candidates))
	for _, candidate := range s.Candidates {
		if candidate.MerchantDID == "" || candidate.CapabilityID == "" || candidate.PayeeDID == "" || candidate.CatalogVersion == "" || !digestPattern.MatchString(candidate.CatalogSnapshotHash) || candidate.CatalogSnapshotRef == "" || candidate.CatalogValidFrom.IsZero() || candidate.CatalogValidUntil.IsZero() || !candidate.CatalogValidUntil.After(candidate.CatalogValidFrom) || !candidate.Eligibility.Eligible() {
			return ErrInvalidCandidateSet
		}
		key := candidate.MerchantDID + "\x00" + candidate.CapabilityID
		if _, ok := seen[key]; ok {
			return ErrInvalidCandidateSet
		}
		seen[key] = struct{}{}
		if candidate.RankFeatures.PriceHintKnown && candidate.RankFeatures.PriceHintMinor == nil {
			return ErrInvalidCandidateSet
		}
	}
	canonical, err := json.Marshal(s.canonical())
	if err != nil {
		return ErrInvalidCandidateSet
	}
	digest := sha256.Sum256(canonical)
	if s.PayloadHash != "sha256:"+hex.EncodeToString(digest[:]) {
		return ErrInvalidCandidateSet
	}
	return nil
}

func (s CandidateSet) ValidateAt(now time.Time) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if now.IsZero() || !now.Before(s.ExpiresAt) {
		return ErrCandidateSetExpired
	}
	if now.Before(s.GeneratedAt) {
		return ErrInvalidCandidateSet
	}
	return nil
}

type canonicalCandidateSet struct {
	CandidateSetID      string               `json:"candidate_set_id"`
	EpisodeID           string               `json:"episode_id"`
	RequestID           string               `json:"request_id"`
	Generation          int                  `json:"generation"`
	QueryHash           string               `json:"query_hash"`
	CatalogSnapshotRefs []CatalogSnapshotRef `json:"catalog_snapshot_refs"`
	Candidates          []Candidate          `json:"candidates"`
	GeneratedAt         time.Time            `json:"generated_at"`
	ExpiresAt           time.Time            `json:"expires_at"`
	FactsRef            string               `json:"facts_ref"`
}

func (s CandidateSet) canonical() canonicalCandidateSet {
	s = s.Normalize()
	return canonicalCandidateSet{CandidateSetID: s.CandidateSetID, EpisodeID: s.EpisodeID, RequestID: s.RequestID, Generation: s.Generation, QueryHash: s.QueryHash,
		CatalogSnapshotRefs: s.CatalogSnapshotRefs, Candidates: s.Candidates, GeneratedAt: s.GeneratedAt, ExpiresAt: s.ExpiresAt, FactsRef: s.FactsRef}
}

// CanonicalSnapshot intentionally excludes PayloadHash, which is derived from
// this payload. This avoids a self-referential hash while retaining every
// authoritative candidate fact.
func (s CandidateSet) CanonicalSnapshot() ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(s.canonical())
}

func (s CandidateSet) SnapshotHash() (string, error) {
	snapshot, err := s.CanonicalSnapshot()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(snapshot)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

// PayloadHashFor computes the derived payload hash while a CandidateSet is
// being assembled, before PayloadHash itself has been populated.
func (s CandidateSet) PayloadHashFor() (string, error) {
	s = s.Normalize()
	snapshot, err := json.Marshal(s.canonical())
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(snapshot)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func (s CandidateSet) Clone() *CandidateSet {
	s = s.Normalize()
	return &s
}

func (s CandidateSet) FindCandidate(merchantDID, capabilityID string) (Candidate, bool) {
	merchantDID = strings.TrimSpace(merchantDID)
	capabilityID = strings.ToLower(strings.TrimSpace(capabilityID))
	for _, candidate := range s.Candidates {
		if candidate.MerchantDID == merchantDID && candidate.CapabilityID == capabilityID {
			return candidate, true
		}
	}
	return Candidate{}, false
}

// DiscoveryQuery is the structured projection of an AcquireCapabilityRequest.
// It contains no free-form instruction that can override hard eligibility.
type DiscoveryQuery struct {
	TaskType                  string              `json:"task_type"`
	InputContentType          string              `json:"input_content_type"`
	ExpectedOutputContentType string              `json:"expected_output_content_type"`
	SupportedProtocolVersions []string            `json:"supported_protocol_versions"`
	Currency                  string              `json:"currency"`
	BudgetLimitMinor          int64               `json:"budget_limit_minor"`
	Deadline                  time.Time           `json:"deadline"`
	SemanticConstraints       []contract.KeyValue `json:"semantic_constraints,omitempty"`
}

func FromAcquireCapabilityRequest(request contract.AcquireCapabilityRequest) (DiscoveryQuery, error) {
	normalized, err := request.Normalize()
	if err != nil {
		return DiscoveryQuery{}, err
	}
	expectedOutputContentType := normalized.ExpectedOutput.ContentType
	if expectedOutputContentType == "" {
		expectedOutputContentType = "*/*"
	}
	semanticConstraints := append([]contract.KeyValue(nil), normalized.AcquisitionGoal.SemanticConstraints...)
	semanticConstraints = append(semanticConstraints, normalized.ExpectedOutput.SemanticConstraints...)
	return DiscoveryQuery{TaskType: normalized.AcquisitionGoal.TaskType, InputContentType: normalized.Input.ContentType,
		ExpectedOutputContentType: expectedOutputContentType, SupportedProtocolVersions: append([]string(nil), normalized.Constraints.SupportedProtocolVersions...),
		Currency: normalized.Constraints.Currency, BudgetLimitMinor: normalized.Constraints.BudgetLimitMinor, Deadline: normalized.Constraints.DeadlineAt,
		SemanticConstraints: semanticConstraints}, nil
}

func (q DiscoveryQuery) Normalize() DiscoveryQuery {
	q.TaskType = strings.ToLower(strings.TrimSpace(q.TaskType))
	q.InputContentType = strings.ToLower(strings.TrimSpace(q.InputContentType))
	q.ExpectedOutputContentType = strings.ToLower(strings.TrimSpace(q.ExpectedOutputContentType))
	q.SupportedProtocolVersions = normalizeStrings(q.SupportedProtocolVersions, true)
	q.Currency = strings.ToUpper(strings.TrimSpace(q.Currency))
	q.Deadline = q.Deadline.UTC().Truncate(time.Nanosecond)
	q.SemanticConstraints = normalizeKeyValues(q.SemanticConstraints)
	return q
}

func (q DiscoveryQuery) Validate() error {
	q = q.Normalize()
	if q.TaskType == "" || q.InputContentType == "" || q.ExpectedOutputContentType == "" || len(q.SupportedProtocolVersions) == 0 || q.Currency == "" || q.BudgetLimitMinor < 0 || q.Deadline.IsZero() {
		return ErrInvalidDiscoveryQuery
	}
	return nil
}

func (q DiscoveryQuery) CanonicalSnapshot() ([]byte, error) {
	q = q.Normalize()
	if err := q.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(q)
}

func (q DiscoveryQuery) SnapshotHash() (string, error) {
	snapshot, err := q.CanonicalSnapshot()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(snapshot)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func normalizeKeyValues(values []contract.KeyValue) []contract.KeyValue {
	result := make([]contract.KeyValue, 0, len(values))
	for _, value := range values {
		result = append(result, contract.KeyValue{Key: strings.ToLower(strings.TrimSpace(value.Key)), Value: strings.ToLower(strings.TrimSpace(value.Value))})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Key == result[j].Key {
			return result[i].Value < result[j].Value
		}
		return result[i].Key < result[j].Key
	})
	return result
}
