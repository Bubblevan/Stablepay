// Package memory contains derived historical context. Memory is advisory
// context only; it is never a source of transactional authority.
package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type MemoryType string

const (
	MemoryMerchantOutcome     MemoryType = "MERCHANT_OUTCOME"
	MemoryCapabilityOutcome   MemoryType = "CAPABILITY_OUTCOME"
	MemoryRecoveryOutcome     MemoryType = "RECOVERY_OUTCOME"
	MemoryRequesterPreference MemoryType = "REQUESTER_PREFERENCE"
)

type MemoryScope string

const (
	ScopeRequester          MemoryScope = "REQUESTER"
	ScopeParentSession      MemoryScope = "PARENT_SESSION"
	ScopeMerchant           MemoryScope = "MERCHANT"
	ScopeMerchantCapability MemoryScope = "MERCHANT_CAPABILITY"
)

type MemoryApplicability string

const (
	ApplicabilityCurrent            MemoryApplicability = "CURRENT"
	ApplicabilityHistoricalVersion  MemoryApplicability = "HISTORICAL_VERSION"
	ApplicabilityHistoricalSnapshot MemoryApplicability = "HISTORICAL_SNAPSHOT"
)

var (
	ErrInvalidMemory       = errors.New("invalid memory record")
	ErrInvalidObservation  = errors.New("invalid memory observation")
	ErrMemoryConflict      = errors.New("memory identity conflicts with an existing record")
	ErrObservationConflict = errors.New("memory observation conflicts with an existing identity")
	ErrMemoryNotFound      = errors.New("memory record not found")
)

// OutcomeFacts is a deliberately small, deterministic fact payload. It has
// no response body, credential, private key, signed transaction, or model
// opinion. Fields are counters or values derived from persisted runtime facts.
type OutcomeFacts struct {
	AttemptCount                           int       `json:"attempt_count,omitempty"`
	FulfilledCount                         int       `json:"fulfilled_count,omitempty"`
	DeliveryValidCount                     int       `json:"delivery_valid_count,omitempty"`
	DeliveryInvalidCount                   int       `json:"delivery_invalid_count,omitempty"`
	PaymentFailedCount                     int       `json:"payment_failed_count,omitempty"`
	MerchantErrorCount                     int       `json:"merchant_error_count,omitempty"`
	SwitchAwayCount                        int       `json:"switch_away_count,omitempty"`
	RecentFailureStreak                    int       `json:"recent_failure_streak,omitempty"`
	RecoveryAttemptCount                   int       `json:"recovery_attempt_count,omitempty"`
	RecoverySuccessCount                   int       `json:"recovery_success_count,omitempty"`
	RetryAfterDeliveryInvalidCount         int       `json:"retry_after_delivery_invalid_count,omitempty"`
	RetryAfterDeliveryInvalidSuccessCount  int       `json:"retry_after_delivery_invalid_success_count,omitempty"`
	SwitchAfterDeliveryInvalidCount        int       `json:"switch_after_delivery_invalid_count,omitempty"`
	SwitchAfterDeliveryInvalidSuccessCount int       `json:"switch_after_delivery_invalid_success_count,omitempty"`
	RediscoverCount                        int       `json:"rediscover_count,omitempty"`
	AskParentCount                         int       `json:"ask_parent_count,omitempty"`
	ParentApprovalCount                    int       `json:"parent_approval_count,omitempty"`
	ParentDenialCount                      int       `json:"parent_denial_count,omitempty"`
	LastOutcome                            string    `json:"last_outcome,omitempty"`
	LastOutcomeAt                          time.Time `json:"last_outcome_at,omitempty"`
	RecoveryAction                         string    `json:"recovery_action,omitempty"`
	RecoveryTriggerReason                  string    `json:"recovery_trigger_reason,omitempty"`
	RecoveryResult                         string    `json:"recovery_result,omitempty"`
	PreferenceKey                          string    `json:"preference_key,omitempty"`
	PreferenceValue                        string    `json:"preference_value,omitempty"`
}

func (f OutcomeFacts) Validate() error {
	if f.AttemptCount < 0 || f.FulfilledCount < 0 || f.DeliveryValidCount < 0 || f.DeliveryInvalidCount < 0 ||
		f.PaymentFailedCount < 0 || f.MerchantErrorCount < 0 || f.SwitchAwayCount < 0 || f.RecentFailureStreak < 0 ||
		f.RecoveryAttemptCount < 0 || f.RecoverySuccessCount < 0 || f.RetryAfterDeliveryInvalidCount < 0 || f.RetryAfterDeliveryInvalidSuccessCount < 0 || f.SwitchAfterDeliveryInvalidCount < 0 || f.SwitchAfterDeliveryInvalidSuccessCount < 0 || f.RediscoverCount < 0 || f.AskParentCount < 0 || f.ParentApprovalCount < 0 || f.ParentDenialCount < 0 {
		return ErrInvalidMemory
	}
	return nil
}

type MemoryRecord struct {
	MemoryID            string       `json:"memory_id"`
	Type                MemoryType   `json:"type"`
	Scope               MemoryScope  `json:"scope"`
	RequesterDID        string       `json:"requester_did,omitempty"`
	ParentSessionID     string       `json:"parent_session_id,omitempty"`
	MerchantDID         string       `json:"merchant_did,omitempty"`
	CapabilityID        string       `json:"capability_id,omitempty"`
	CatalogVersion      string       `json:"catalog_version,omitempty"`
	CatalogSnapshotHash string       `json:"catalog_snapshot_hash,omitempty"`
	CatalogSnapshotRef  string       `json:"catalog_snapshot_ref,omitempty"`
	Summary             string       `json:"summary"`
	StructuredFacts     OutcomeFacts `json:"structured_facts"`
	SourceEpisodeIDs    []string     `json:"source_episode_ids"`
	SourceEventRefs     []string     `json:"source_event_refs"`
	SourceEvidenceRefs  []string     `json:"source_evidence_refs,omitempty"`
	ObservationCount    int          `json:"observation_count"`
	// Confidence is a deterministic observation-support score, not a calibrated
	// probability and never a model confidence or authority signal.
	Confidence      float64    `json:"confidence"`
	FirstObservedAt time.Time  `json:"first_observed_at"`
	LastObservedAt  time.Time  `json:"last_observed_at"`
	ValidFrom       time.Time  `json:"valid_from"`
	ValidUntil      *time.Time `json:"valid_until,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	FactsRef        string     `json:"facts_ref"`
	PayloadHash     string     `json:"payload_hash"`
	// Applicability is query-time metadata and is not part of the persisted
	// canonical payload hash. It marks a catalog-version mismatch as historical.
	Applicability MemoryApplicability `json:"applicability,omitempty"`
}

type MemoryObservation struct {
	ObservationID          string    `json:"observation_id"`
	MemoryID               string    `json:"memory_id"`
	SourceEpisodeID        string    `json:"source_episode_id"`
	SourceEventRef         string    `json:"source_event_ref"`
	SourceEvidenceRefs     []string  `json:"source_evidence_refs,omitempty"`
	ObservationKind        string    `json:"observation_kind"`
	Outcome                string    `json:"outcome,omitempty"`
	ObservedAt             time.Time `json:"observed_at"`
	CatalogVersion         string    `json:"catalog_version,omitempty"`
	CatalogSnapshotHash    string    `json:"catalog_snapshot_hash,omitempty"`
	CatalogSnapshotRef     string    `json:"catalog_snapshot_ref,omitempty"`
	TriggerEventRef        string    `json:"trigger_event_ref,omitempty"`
	RecoveryActionEventRef string    `json:"recovery_action_event_ref,omitempty"`
	TerminalEventRef       string    `json:"terminal_event_ref,omitempty"`
	RelatedEventRefs       []string  `json:"related_event_refs,omitempty"`
	PayloadHash            string    `json:"payload_hash"`
}

type MemoryQuery struct {
	RequesterDID        string
	ParentSessionID     string
	MerchantDID         string
	CapabilityID        string
	CatalogVersion      string
	CatalogSnapshotHash string
	Now                 time.Time
	Limit               int
}

// MemoryRetriever exposes deterministic, scope-filtered retrieval of derived
// historical context. It is advisory data and is never a transactional
// authority for payment, entitlement, candidate membership, or runtime state.
type MemoryRetriever interface {
	Retrieve(context.Context, MemoryQuery) ([]*MemoryRecord, error)
}

type MemoryDetailResolver interface {
	Resolve(context.Context, string, int) ([]*MemoryObservation, error)
}

type MemoryStore interface {
	MemoryRetriever
	SaveObservationAndUpdateAggregate(context.Context, *MemoryRecord, *MemoryObservation, OutcomeFacts) error
	GetMemory(context.Context, string) (*MemoryRecord, error)
	ListMemories(context.Context, MemoryQuery) ([]*MemoryRecord, error)
	SearchMemories(context.Context, MemoryQuery) ([]*MemoryRecord, error)
	Resolve(context.Context, string, int) ([]*MemoryObservation, error)
}

// EpisodeSource is the authoritative read surface used by the deterministic
// post-episode projector. Implementations expose persisted facts, not memory.
type EpisodeSource interface {
	GetMemoryEpisode(context.Context, string) (EpisodeView, error)
	ListMemoryEpisodeEvents(context.Context, string) ([]EventView, error)
	ListMemoryPaymentIntents(context.Context, string) ([]PaymentView, error)
	GetMemoryDeliveryArtifact(context.Context, string) (DeliveryView, error)
	GetMemoryValidationEvidence(context.Context, string) (ValidationView, error)
}

// The view types keep the memory package independent from repository adapter
// packages while preserving the exact authoritative values needed for a
// deterministic projection.
type EpisodeView struct {
	EpisodeID              string
	RequesterDID           string
	ParentSessionID        string
	State                  string
	TerminalReason         string
	SelectedMerchantDID    string
	SelectedCapabilityID   string
	SelectedCatalogVersion string
	SelectedCatalogHash    string
	SelectedCatalogRef     string
	DeliveryRefs           []string
	ValidationRefs         []string
	AttemptedMerchants     []string
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type EventView struct {
	EventID                   string
	Sequence                  uint64
	OccurredAt                time.Time
	Action                    string
	Observation               string
	ObservationCode           string
	FactsRef                  string
	PayloadHash               string
	MerchantDID               string
	CapabilityID              string
	TargetMerchantDID         string
	TargetCatalogVersion      string
	TargetCatalogSnapshotHash string
	TargetCatalogSnapshotRef  string
	StateAfter                string
}

type PaymentView struct {
	IntentID            string
	MerchantDID         string
	CapabilityID        string
	CatalogVersion      string
	CatalogSnapshotHash string
	CatalogSnapshotRef  string
	Status              string
	UpdatedAt           time.Time
}

type DeliveryView struct {
	DeliveryID          string
	EpisodeID           string
	InvocationID        string
	MerchantDID         string
	CapabilityID        string
	CatalogVersion      string
	CatalogSnapshotHash string
	CatalogSnapshotRef  string
	Attempt             int
	ReceivedAt          time.Time
}

type ValidationView struct {
	ValidationID string
	DeliveryID   string
	Valid        bool
	ReasonCode   string
	EvidenceRefs []string
	CreatedAt    time.Time
}

func (r MemoryRecord) Clone() *MemoryRecord {
	r.SourceEpisodeIDs = append([]string(nil), r.SourceEpisodeIDs...)
	r.SourceEventRefs = append([]string(nil), r.SourceEventRefs...)
	r.SourceEvidenceRefs = append([]string(nil), r.SourceEvidenceRefs...)
	if r.ValidUntil != nil {
		value := *r.ValidUntil
		r.ValidUntil = &value
	}
	return &r
}

func (o MemoryObservation) Clone() *MemoryObservation {
	o.SourceEvidenceRefs = append([]string(nil), o.SourceEvidenceRefs...)
	o.RelatedEventRefs = append([]string(nil), o.RelatedEventRefs...)
	return &o
}

func validType(value MemoryType) bool {
	switch value {
	case MemoryMerchantOutcome, MemoryCapabilityOutcome, MemoryRecoveryOutcome, MemoryRequesterPreference:
		return true
	default:
		return false
	}
}

func validScope(value MemoryScope) bool {
	switch value {
	case ScopeRequester, ScopeParentSession, ScopeMerchant, ScopeMerchantCapability:
		return true
	default:
		return false
	}
}

func (r MemoryRecord) Validate() error {
	if strings.TrimSpace(r.MemoryID) == "" || !validType(r.Type) || !validScope(r.Scope) || strings.TrimSpace(r.Summary) == "" || len(r.Summary) > 1024 ||
		r.ObservationCount < 0 || r.Confidence < 0 || r.Confidence > 1 || r.FirstObservedAt.IsZero() || r.LastObservedAt.IsZero() ||
		r.ValidFrom.IsZero() || r.CreatedAt.IsZero() || r.UpdatedAt.IsZero() || r.FactsRef != "memory://"+r.MemoryID || strings.TrimSpace(r.PayloadHash) == "" {
		return ErrInvalidMemory
	}
	if containsSensitiveMaterial(r.Summary) {
		return ErrInvalidMemory
	}
	switch r.Scope {
	case ScopeRequester:
		if strings.TrimSpace(r.RequesterDID) == "" {
			return ErrInvalidMemory
		}
	case ScopeParentSession:
		if strings.TrimSpace(r.ParentSessionID) == "" {
			return ErrInvalidMemory
		}
	case ScopeMerchant:
		if strings.TrimSpace(r.MerchantDID) == "" {
			return ErrInvalidMemory
		}
	case ScopeMerchantCapability:
		if strings.TrimSpace(r.MerchantDID) == "" || strings.TrimSpace(r.CapabilityID) == "" {
			return ErrInvalidMemory
		}
	}
	if r.ValidUntil != nil && !r.ValidUntil.After(r.ValidFrom) {
		return ErrInvalidMemory
	}
	if r.LastObservedAt.Before(r.FirstObservedAt) || r.UpdatedAt.Before(r.CreatedAt) || r.ObservationCount == 0 || len(r.SourceEpisodeIDs) == 0 || len(r.SourceEventRefs) == 0 {
		return ErrInvalidMemory
	}
	if err := r.StructuredFacts.Validate(); err != nil {
		return err
	}
	expected, err := r.PayloadHashFor()
	if err != nil || expected != r.PayloadHash {
		return ErrInvalidMemory
	}
	return nil
}

func containsSensitiveMaterial(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{"private_key", "private key", "signedtx", "signed_tx", "payment-signature", "api_key", "api key", "secret credential"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func (o MemoryObservation) Validate() error {
	if strings.TrimSpace(o.ObservationID) == "" || strings.TrimSpace(o.MemoryID) == "" || strings.TrimSpace(o.SourceEpisodeID) == "" ||
		strings.TrimSpace(o.SourceEventRef) == "" || strings.TrimSpace(o.ObservationKind) == "" || o.ObservedAt.IsZero() || strings.TrimSpace(o.PayloadHash) == "" {
		return ErrInvalidObservation
	}
	expected, err := o.PayloadHashFor()
	if err != nil || expected != o.PayloadHash {
		return ErrInvalidObservation
	}
	return nil
}

func (r MemoryRecord) CanonicalSnapshot() ([]byte, error) {
	copy := r
	copy.PayloadHash = ""
	copy.Applicability = ""
	copy.SourceEpisodeIDs = sortedUnique(copy.SourceEpisodeIDs)
	copy.SourceEventRefs = sortedUnique(copy.SourceEventRefs)
	copy.SourceEvidenceRefs = sortedUnique(copy.SourceEvidenceRefs)
	copy.ValidUntil = cloneTimePtr(copy.ValidUntil)
	return json.Marshal(copy)
}

func (r MemoryRecord) PayloadHashFor() (string, error) {
	snapshot, err := r.CanonicalSnapshot()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(snapshot)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func (r *MemoryRecord) RefreshPayloadHash() error {
	if r == nil {
		return ErrInvalidMemory
	}
	hash, err := r.PayloadHashFor()
	if err != nil {
		return err
	}
	r.PayloadHash = hash
	return nil
}

func (o MemoryObservation) CanonicalSnapshot() ([]byte, error) {
	copy := o
	copy.PayloadHash = ""
	copy.SourceEvidenceRefs = sortedUnique(copy.SourceEvidenceRefs)
	copy.RelatedEventRefs = sortedUnique(copy.RelatedEventRefs)
	return json.Marshal(copy)
}

func (o MemoryObservation) PayloadHashFor() (string, error) {
	snapshot, err := o.CanonicalSnapshot()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(snapshot)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func (o *MemoryObservation) RefreshPayloadHash() error {
	if o == nil {
		return ErrInvalidObservation
	}
	hash, err := o.PayloadHashFor()
	if err != nil {
		return err
	}
	o.PayloadHash = hash
	return nil
}

func IdentityKey(typ MemoryType, scope MemoryScope, requester, parentSession, merchant, capability, catalogVersion string) string {
	return strings.Join([]string{string(typ), string(scope), strings.TrimSpace(requester), strings.TrimSpace(parentSession), strings.TrimSpace(merchant), strings.ToLower(strings.TrimSpace(capability)), strings.TrimSpace(catalogVersion)}, "\x00")
}

func IdentityKeyForSnapshot(typ MemoryType, scope MemoryScope, requester, parentSession, merchant, capability, catalogVersion, catalogSnapshotHash string) string {
	if strings.TrimSpace(catalogSnapshotHash) == "" {
		return IdentityKey(typ, scope, requester, parentSession, merchant, capability, catalogVersion)
	}
	return strings.Join([]string{string(typ), string(scope), strings.TrimSpace(requester), strings.TrimSpace(parentSession), strings.TrimSpace(merchant), strings.ToLower(strings.TrimSpace(capability)), strings.TrimSpace(catalogVersion), strings.TrimSpace(catalogSnapshotHash)}, "\x00")
}

func MemoryIDFor(typ MemoryType, scope MemoryScope, requester, parentSession, merchant, capability, catalogVersion string) string {
	return MemoryIDForSnapshot(typ, scope, requester, parentSession, merchant, capability, catalogVersion, "")
}

func MemoryIDForSnapshot(typ MemoryType, scope MemoryScope, requester, parentSession, merchant, capability, catalogVersion, catalogSnapshotHash string) string {
	digest := sha256.Sum256([]byte(IdentityKeyForSnapshot(typ, scope, requester, parentSession, merchant, capability, catalogVersion, catalogSnapshotHash)))
	return "mem_" + hex.EncodeToString(digest[:])
}

func ObservationIDFor(memoryID, episodeID, kind string) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{memoryID, strings.TrimSpace(episodeID), strings.TrimSpace(kind)}, "\x00")))
	return "obs_" + hex.EncodeToString(digest[:])
}

func Summarize(r MemoryRecord) string {
	label := strings.TrimSpace(r.MerchantDID)
	if label == "" {
		label = strings.TrimSpace(r.RequesterDID)
	}
	if label == "" {
		label = string(r.Scope)
	}
	capability := strings.TrimSpace(r.CapabilityID)
	if capability != "" {
		label += "/" + capability
	}
	f := r.StructuredFacts
	switch r.Type {
	case MemoryRecoveryOutcome:
		return fmt.Sprintf("%s recovery history: attempts=%d, successful=%d, last_action=%s", label, f.RecoveryAttemptCount, f.RecoverySuccessCount, valueOrUnknown(f.RecoveryAction))
	case MemoryRequesterPreference:
		return fmt.Sprintf("requester preference %s=%s (historical, advisory)", valueOrUnknown(f.PreferenceKey), valueOrUnknown(f.PreferenceValue))
	default:
		return fmt.Sprintf("%s outcome history: attempts=%d, valid=%d, invalid=%d, fulfilled=%d, payment_failed=%d, merchant_error=%d, switch_away=%d, last=%s", label, f.AttemptCount, f.DeliveryValidCount, f.DeliveryInvalidCount, f.FulfilledCount, f.PaymentFailedCount, f.MerchantErrorCount, f.SwitchAwayCount, valueOrUnknown(f.LastOutcome))
	}
}

func valueOrUnknown(value string) string {
	if strings.TrimSpace(value) == "" {
		return "UNKNOWN"
	}
	return value
}

func sortedUnique(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
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

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := value.UTC()
	return &copy
}
