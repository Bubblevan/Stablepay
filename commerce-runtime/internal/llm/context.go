package llm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/decision"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/evidence"
	"github.com/stablepay/commerce-runtime/internal/invocation"
	"github.com/stablepay/commerce-runtime/internal/memory"
	"github.com/stablepay/commerce-runtime/internal/recovery"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

const (
	MaxContextCandidates    = 32
	MaxContextEvidence      = 16
	MaxContextMemories      = 16
	MaxMemoryCandidates     = 8
	MaxMemoriesPerCandidate = 2
	MaxContextActions       = 16
)

var (
	ErrInvalidDecisionContext = errors.New("invalid LLM decision context")
	ErrContextTooLarge        = errors.New("LLM decision context exceeds bounds")
)

type EpisodeSnapshot struct {
	EpisodeID            string        `json:"episode_id"`
	RequestID            string        `json:"request_id"`
	State                episode.State `json:"state"`
	DeadlineAt           time.Time     `json:"deadline_at"`
	Version              uint64        `json:"version"`
	ActionCount          int           `json:"action_count"`
	MaxTotalAttempts     int           `json:"max_total_attempts"`
	PaymentAttemptCount  int           `json:"payment_attempt_count"`
	MaxPaymentAttempts   int           `json:"max_payment_attempts"`
	DeliveryAttemptCount int           `json:"delivery_attempt_count"`
	MaxDeliveryAttempts  int           `json:"max_delivery_attempts"`
	RetryCount           int           `json:"retry_count"`
	AttemptedMerchants   []string      `json:"attempted_merchants,omitempty"`
}

type MerchantSnapshot struct {
	MerchantDID         string `json:"merchant_did"`
	CapabilityID        string `json:"capability_id"`
	CatalogVersion      string `json:"catalog_version"`
	CatalogSnapshotHash string `json:"catalog_snapshot_hash"`
	CatalogSnapshotRef  string `json:"catalog_snapshot_ref"`
}

type CandidateSummary struct {
	MerchantDID         string `json:"merchant_did"`
	CapabilityID        string `json:"capability_id"`
	PayeeDID            string `json:"payee_did"`
	CatalogVersion      string `json:"catalog_version"`
	CatalogSnapshotHash string `json:"catalog_snapshot_hash"`
	CatalogSnapshotRef  string `json:"catalog_snapshot_ref"`
	Eligible            bool   `json:"eligible"`
}

type CandidateSetSummary struct {
	CandidateSetID string             `json:"candidate_set_id"`
	Generation     int                `json:"generation"`
	FactsRef       string             `json:"facts_ref"`
	PayloadHash    string             `json:"payload_hash"`
	ExpiresAt      time.Time          `json:"expires_at"`
	Candidates     []CandidateSummary `json:"candidates,omitempty"`
}

type RecoverySummary struct {
	RecoveryID          string   `json:"recovery_id"`
	EpisodeID           string   `json:"episode_id"`
	ReasonCode          string   `json:"reason_code"`
	CurrentMerchantDID  string   `json:"current_merchant_did"`
	CurrentCapabilityID string   `json:"current_capability_id"`
	CandidateSetID      string   `json:"candidate_set_id"`
	AttemptedMerchants  []string `json:"attempted_merchants"`
	PaymentIntentIDs    []string `json:"payment_intent_ids"`
	FactsRef            string   `json:"facts_ref"`
	PayloadHash         string   `json:"payload_hash"`
}

type BudgetSummary struct {
	Currency       string `json:"currency"`
	LimitMinor     int64  `json:"limit_minor"`
	ReservedMinor  int64  `json:"reserved_minor"`
	SettledMinor   int64  `json:"settled_minor"`
	ConsumedMinor  int64  `json:"consumed_minor"`
	AvailableMinor int64  `json:"available_minor"`
	SunkCostMinor  int64  `json:"sunk_cost_minor"`
}

type ValidationSummary struct {
	ValidationID string    `json:"validation_id"`
	DeliveryID   string    `json:"delivery_id"`
	Valid        bool      `json:"valid"`
	ReasonCode   string    `json:"reason_code"`
	EvidenceRefs []string  `json:"evidence_refs,omitempty"`
	PayloadHash  string    `json:"payload_hash"`
	CreatedAt    time.Time `json:"created_at"`
}

type CandidateMemorySummary struct {
	MerchantDID         string                `json:"merchant_did"`
	CapabilityID        string                `json:"capability_id"`
	CatalogVersion      string                `json:"catalog_version"`
	CatalogSnapshotHash string                `json:"catalog_snapshot_hash"`
	Memories            []memory.MemoryRecord `json:"memories,omitempty"`
}

// DecisionContext is the bounded structured input sent to an LLM. It is
// intentionally a projection, never a dump of the event log or database.
type DecisionContext struct {
	Episode              EpisodeSnapshot           `json:"episode"`
	CurrentEventSequence uint64                    `json:"current_event_sequence"`
	AllowedActions       []trace.ActionType        `json:"allowed_actions"`
	SelectedMerchant     *MerchantSnapshot         `json:"selected_merchant,omitempty"`
	CandidateSet         CandidateSetSummary       `json:"candidate_set"`
	Recovery             *RecoverySummary          `json:"recovery,omitempty"`
	Budget               BudgetSummary             `json:"budget"`
	LatestValidation     *ValidationSummary        `json:"latest_validation,omitempty"`
	RetrievedEvidence    []evidence.EvidenceRecord `json:"retrieved_evidence,omitempty"`
	RetrievedMemories    []memory.MemoryRecord     `json:"retrieved_memories,omitempty"`
	CandidateMemories    []CandidateMemorySummary  `json:"candidate_memories,omitempty"`
}

type ContextInput struct {
	Episode           *episode.CommerceEpisode
	AllowedActions    []trace.ActionType
	CandidateSet      *catalog.CandidateSet
	Recovery          *recovery.RecoveryContext
	LatestValidation  *invocation.ValidationEvidence
	RetrievedEvidence []evidence.EvidenceRecord
	RetrievedMemories []memory.MemoryRecord
	CandidateMemories []CandidateMemorySummary
}

// ContextBuilder is deliberately pure: callers provide only the bounded
// runtime projections and already-resolved evidence records.
type ContextBuilder struct{}

func (ContextBuilder) Build(input ContextInput) (DecisionContext, error) {
	return buildDecisionContext(input)
}

func BuildDecisionContext(input ContextInput) (DecisionContext, error) {
	return (ContextBuilder{}).Build(input)
}

func buildDecisionContext(input ContextInput) (DecisionContext, error) {
	if input.Episode == nil {
		return DecisionContext{}, ErrInvalidDecisionContext
	}
	e := input.Episode
	ctx := DecisionContext{
		Episode: EpisodeSnapshot{
			EpisodeID: e.EpisodeID, RequestID: e.RequestID, State: e.State, DeadlineAt: e.DeadlineAt.UTC(), Version: e.Version,
			ActionCount: e.ActionCount, MaxTotalAttempts: e.MaxTotalAttempts, PaymentAttemptCount: e.PaymentAttemptCount,
			MaxPaymentAttempts: e.MaxPaymentAttempts, DeliveryAttemptCount: e.DeliveryAttemptCount,
			MaxDeliveryAttempts: e.MaxDeliveryAttempts, RetryCount: e.RetryCount, AttemptedMerchants: append([]string(nil), e.AttemptedMerchants...),
		},
		CurrentEventSequence: eventSequence(e.Version),
		AllowedActions:       normalizeActions(input.AllowedActions),
		Budget:               BudgetSummary{Currency: e.Budget.Currency, LimitMinor: e.Budget.BudgetLimitMinor, ReservedMinor: e.Budget.ReservedAmount, SettledMinor: e.Budget.SettledAmount, ConsumedMinor: e.Budget.ConsumedAmount, AvailableMinor: e.Budget.AvailableBudget, SunkCostMinor: e.Budget.SunkCost},
	}
	if e.SelectedMerchantDID != "" || e.SelectedCapabilityID != "" {
		ctx.SelectedMerchant = &MerchantSnapshot{MerchantDID: e.SelectedMerchantDID, CapabilityID: e.SelectedCapabilityID, CatalogVersion: e.SelectedCatalogVersion, CatalogSnapshotHash: e.SelectedCatalogSnapshotHash, CatalogSnapshotRef: e.SelectedCatalogSnapshotRef}
	}
	if input.CandidateSet != nil {
		set := input.CandidateSet.Normalize()
		ctx.CandidateSet = CandidateSetSummary{CandidateSetID: set.CandidateSetID, Generation: set.Generation, FactsRef: set.FactsRef, PayloadHash: set.PayloadHash, ExpiresAt: set.ExpiresAt.UTC()}
		for _, candidate := range set.Candidates {
			if len(ctx.CandidateSet.Candidates) >= MaxContextCandidates {
				break
			}
			ctx.CandidateSet.Candidates = append(ctx.CandidateSet.Candidates, CandidateSummary{MerchantDID: candidate.MerchantDID, CapabilityID: candidate.CapabilityID, PayeeDID: candidate.PayeeDID, CatalogVersion: candidate.CatalogVersion, CatalogSnapshotHash: candidate.CatalogSnapshotHash, CatalogSnapshotRef: candidate.CatalogSnapshotRef, Eligible: candidate.Eligibility.Eligible()})
		}
	}
	if input.Recovery != nil {
		r := input.Recovery.Normalize()
		ctx.Recovery = &RecoverySummary{RecoveryID: r.RecoveryID, EpisodeID: r.EpisodeID, ReasonCode: string(r.ReasonCode), CurrentMerchantDID: r.CurrentMerchantDID, CurrentCapabilityID: r.CurrentCapabilityID, CandidateSetID: r.CandidateSetID, AttemptedMerchants: append([]string(nil), r.AttemptedMerchants...), PaymentIntentIDs: append([]string(nil), r.PaymentIntentIDs...), FactsRef: r.FactsRef, PayloadHash: r.PayloadHash}
	}
	if input.LatestValidation != nil {
		v := input.LatestValidation.Clone()
		ctx.LatestValidation = &ValidationSummary{ValidationID: v.ValidationID, DeliveryID: v.DeliveryID, Valid: v.Valid, ReasonCode: v.ReasonCode, EvidenceRefs: append([]string(nil), v.EvidenceRefs...), PayloadHash: v.PayloadHash, CreatedAt: v.CreatedAt.UTC()}
	}
	for i, record := range input.RetrievedEvidence {
		if i >= MaxContextEvidence {
			break
		}
		if err := record.Validate(); err != nil {
			return DecisionContext{}, err
		}
		ctx.RetrievedEvidence = append(ctx.RetrievedEvidence, *record.Clone())
	}
	for i, record := range input.RetrievedMemories {
		if i >= MaxContextMemories {
			break
		}
		if err := record.Validate(); err != nil {
			return DecisionContext{}, err
		}
		copy := record.Clone()
		if copy.Applicability == "" {
			copy.Applicability = memory.ApplicabilityCurrent
		}
		ctx.RetrievedMemories = append(ctx.RetrievedMemories, *copy)
	}
	totalMemories := len(ctx.RetrievedMemories)
	for candidateIndex, candidate := range input.CandidateMemories {
		if candidateIndex >= MaxMemoryCandidates {
			break
		}
		if strings.TrimSpace(candidate.MerchantDID) == "" || strings.TrimSpace(candidate.CapabilityID) == "" {
			return DecisionContext{}, ErrInvalidDecisionContext
		}
		summary := CandidateMemorySummary{MerchantDID: candidate.MerchantDID, CapabilityID: candidate.CapabilityID, CatalogVersion: candidate.CatalogVersion, CatalogSnapshotHash: candidate.CatalogSnapshotHash}
		for memoryIndex, record := range candidate.Memories {
			if memoryIndex >= MaxMemoriesPerCandidate || totalMemories >= MaxContextMemories {
				break
			}
			if err := record.Validate(); err != nil {
				return DecisionContext{}, err
			}
			copy := record.Clone()
			if copy.Applicability == "" {
				copy.Applicability = memory.ApplicabilityCurrent
			}
			summary.Memories = append(summary.Memories, *copy)
			totalMemories++
		}
		if len(summary.Memories) > 0 {
			ctx.CandidateMemories = append(ctx.CandidateMemories, summary)
		}
	}
	if err := ctx.Validate(); err != nil {
		return DecisionContext{}, err
	}
	return ctx, nil
}

func eventSequence(version uint64) uint64 {
	if version == 0 {
		return 0
	}
	return version - 1
}

func normalizeActions(actions []trace.ActionType) []trace.ActionType {
	seen := make(map[trace.ActionType]struct{}, len(actions))
	result := make([]trace.ActionType, 0, len(actions))
	for _, action := range actions {
		if _, ok := seen[action]; ok {
			continue
		}
		seen[action] = struct{}{}
		result = append(result, action)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func (c DecisionContext) Validate() error {
	totalMemories := len(c.RetrievedMemories)
	if len(c.CandidateMemories) > MaxMemoryCandidates {
		return ErrContextTooLarge
	}
	for _, candidate := range c.CandidateMemories {
		if strings.TrimSpace(candidate.MerchantDID) == "" || strings.TrimSpace(candidate.CapabilityID) == "" || len(candidate.Memories) > MaxMemoriesPerCandidate {
			return ErrInvalidDecisionContext
		}
		totalMemories += len(candidate.Memories)
	}
	if strings.TrimSpace(c.Episode.EpisodeID) == "" || c.Episode.DeadlineAt.IsZero() || c.Episode.Version == 0 || len(c.AllowedActions) == 0 || len(c.AllowedActions) > MaxContextActions || len(c.CandidateSet.Candidates) > MaxContextCandidates || len(c.RetrievedEvidence) > MaxContextEvidence || totalMemories > MaxContextMemories {
		return ErrInvalidDecisionContext
	}
	if c.CurrentEventSequence != eventSequence(c.Episode.Version) {
		return ErrInvalidDecisionContext
	}
	for _, action := range c.AllowedActions {
		if !decision.ProviderAllowedAction(action) && action != trace.ActionSelectMerchant {
			return ErrInvalidDecisionContext
		}
	}
	for _, record := range c.RetrievedEvidence {
		if err := record.Validate(); err != nil {
			return err
		}
	}
	for _, record := range c.RetrievedMemories {
		if err := record.Validate(); err != nil {
			return err
		}
	}
	for _, candidate := range c.CandidateMemories {
		for _, record := range candidate.Memories {
			if err := record.Validate(); err != nil {
				return err
			}
		}
	}
	return nil
}

// CanonicalSnapshot excludes no runtime facts. Retrieved document bodies are
// represented by their verified payload/chunk hashes because those hashes bind
// the exact text included in the prompt.
func (c DecisionContext) CanonicalSnapshot() ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	copyContext := c
	copyContext.RetrievedEvidence = make([]evidence.EvidenceRecord, 0, len(c.RetrievedEvidence))
	for _, record := range c.RetrievedEvidence {
		copyContext.RetrievedEvidence = append(copyContext.RetrievedEvidence, *record.Clone())
	}
	copyContext.RetrievedMemories = make([]memory.MemoryRecord, 0, len(c.RetrievedMemories))
	for _, record := range c.RetrievedMemories {
		copyContext.RetrievedMemories = append(copyContext.RetrievedMemories, *record.Clone())
	}
	return json.Marshal(copyContext)
}

func (c DecisionContext) Hash() (string, error) {
	snapshot, err := c.CanonicalSnapshot()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(snapshot)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
