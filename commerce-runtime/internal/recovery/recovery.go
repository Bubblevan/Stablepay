// Package recovery contains the durable, runtime-owned recovery facts used by
// S5. These values are facts and commands at the application boundary; a
// decision provider may propose an action, but it cannot author them.
package recovery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type ReasonCode string

const (
	ReasonDeliveryInvalid        ReasonCode = "DELIVERY_INVALID"
	ReasonMerchantAccessRejected ReasonCode = "MERCHANT_ACCESS_REJECTED"
	ReasonMerchantError          ReasonCode = "MERCHANT_ERROR"
	ReasonQuoteExpired           ReasonCode = "QUOTE_EXPIRED"
	ReasonPaymentFailed          ReasonCode = "PAYMENT_FAILED"
	ReasonEntitlementInvalid     ReasonCode = "ENTITLEMENT_INVALID"
	ReasonAttemptLimit           ReasonCode = "ATTEMPT_LIMIT"
	ReasonBudgetInsufficient     ReasonCode = "BUDGET_INSUFFICIENT"
	ReasonCatalogStale           ReasonCode = "CATALOG_STALE"
	ReasonNoRecoveryCandidate    ReasonCode = "NO_RECOVERY_CANDIDATE"
	ReasonRecoveryExhausted      ReasonCode = "RECOVERY_EXHAUSTED"
	ReasonParentDenied           ReasonCode = "PARENT_DENIED"
	ReasonBudgetExhausted        ReasonCode = "BUDGET_EXHAUSTED"
	ReasonDeadlineExceeded       ReasonCode = "DEADLINE_EXCEEDED"
)

func (r ReasonCode) Valid() bool {
	switch r {
	case ReasonDeliveryInvalid, ReasonMerchantAccessRejected, ReasonMerchantError,
		ReasonQuoteExpired, ReasonPaymentFailed, ReasonEntitlementInvalid,
		ReasonAttemptLimit, ReasonBudgetInsufficient, ReasonCatalogStale,
		ReasonNoRecoveryCandidate, ReasonRecoveryExhausted, ReasonParentDenied,
		ReasonBudgetExhausted, ReasonDeadlineExceeded:
		return true
	default:
		return false
	}
}

type RecoveryContext struct {
	RecoveryID           string     `json:"recovery_id"`
	EpisodeID            string     `json:"episode_id"`
	TriggerEventID       string     `json:"trigger_event_id"`
	ReasonCode           ReasonCode `json:"reason_code"`
	CurrentMerchantDID   string     `json:"current_merchant_did"`
	CurrentCapabilityID  string     `json:"current_capability_id"`
	CandidateSetID       string     `json:"candidate_set_id"`
	AttemptedMerchants   []string   `json:"attempted_merchants"`
	PaymentIntentIDs     []string   `json:"payment_intent_ids"`
	SettledMinor         int64      `json:"settled_minor"`
	RefundedMinor        int64      `json:"refunded_minor"`
	ConsumedMinor        int64      `json:"consumed_minor"`
	AvailableMinor       int64      `json:"available_minor"`
	SunkCostMinor        int64      `json:"sunk_cost_minor"`
	DeliveryAttemptCount int        `json:"delivery_attempt_count"`
	RetryCount           int        `json:"retry_count"`
	DeadlineAt           time.Time  `json:"deadline_at"`
	CreatedAt            time.Time  `json:"created_at"`
	FactsRef             string     `json:"facts_ref"`
	PayloadHash          string     `json:"payload_hash"`
}

var (
	ErrInvalidRecoveryContext = errors.New("invalid recovery context")
	ErrInvalidParentRequest   = errors.New("invalid parent approval request")
	ErrInvalidParentDecision  = errors.New("invalid parent decision fact")
	ErrInvalidBudgetAmendment = errors.New("invalid budget amendment")
)

func (c RecoveryContext) Normalize() RecoveryContext {
	c.RecoveryID = strings.TrimSpace(c.RecoveryID)
	c.EpisodeID = strings.TrimSpace(c.EpisodeID)
	c.TriggerEventID = strings.TrimSpace(c.TriggerEventID)
	c.CurrentMerchantDID = strings.TrimSpace(c.CurrentMerchantDID)
	c.CurrentCapabilityID = strings.ToLower(strings.TrimSpace(c.CurrentCapabilityID))
	c.CandidateSetID = strings.TrimSpace(c.CandidateSetID)
	c.FactsRef = strings.TrimSpace(c.FactsRef)
	c.PayloadHash = strings.ToLower(strings.TrimSpace(c.PayloadHash))
	c.AttemptedMerchants = append([]string(nil), c.AttemptedMerchants...)
	c.PaymentIntentIDs = append([]string(nil), c.PaymentIntentIDs...)
	return c
}

func (c RecoveryContext) Validate() error {
	c = c.Normalize()
	if c.RecoveryID == "" || c.EpisodeID == "" || c.TriggerEventID == "" || !c.ReasonCode.Valid() || c.DeadlineAt.IsZero() || c.CreatedAt.IsZero() || c.FactsRef == "" || c.PayloadHash == "" || c.SettledMinor < 0 || c.RefundedMinor < 0 || c.ConsumedMinor < 0 || c.AvailableMinor < 0 || c.SunkCostMinor < 0 || c.DeliveryAttemptCount < 0 || c.RetryCount < 0 {
		return ErrInvalidRecoveryContext
	}
	if len(c.AttemptedMerchants) == 0 {
		return ErrInvalidRecoveryContext
	}
	return nil
}

func (c RecoveryContext) CanonicalSnapshot() ([]byte, error) {
	c = c.Normalize()
	return json.Marshal(struct {
		RecoveryID           string     `json:"recovery_id"`
		EpisodeID            string     `json:"episode_id"`
		TriggerEventID       string     `json:"trigger_event_id"`
		ReasonCode           ReasonCode `json:"reason_code"`
		CurrentMerchantDID   string     `json:"current_merchant_did"`
		CurrentCapabilityID  string     `json:"current_capability_id"`
		CandidateSetID       string     `json:"candidate_set_id"`
		AttemptedMerchants   []string   `json:"attempted_merchants"`
		PaymentIntentIDs     []string   `json:"payment_intent_ids"`
		SettledMinor         int64      `json:"settled_minor"`
		RefundedMinor        int64      `json:"refunded_minor"`
		ConsumedMinor        int64      `json:"consumed_minor"`
		AvailableMinor       int64      `json:"available_minor"`
		SunkCostMinor        int64      `json:"sunk_cost_minor"`
		DeliveryAttemptCount int        `json:"delivery_attempt_count"`
		RetryCount           int        `json:"retry_count"`
		DeadlineAt           time.Time  `json:"deadline_at"`
		CreatedAt            time.Time  `json:"created_at"`
		FactsRef             string     `json:"facts_ref"`
	}{c.RecoveryID, c.EpisodeID, c.TriggerEventID, c.ReasonCode, c.CurrentMerchantDID, c.CurrentCapabilityID, c.CandidateSetID, c.AttemptedMerchants, c.PaymentIntentIDs, c.SettledMinor, c.RefundedMinor, c.ConsumedMinor, c.AvailableMinor, c.SunkCostMinor, c.DeliveryAttemptCount, c.RetryCount, c.DeadlineAt, c.CreatedAt, c.FactsRef})
}

func (c *RecoveryContext) RefreshPayloadHash() error {
	if c == nil {
		return ErrInvalidRecoveryContext
	}
	snapshot, err := c.CanonicalSnapshot()
	if err != nil {
		return err
	}
	digest := sha256.Sum256(snapshot)
	c.PayloadHash = "sha256:" + hex.EncodeToString(digest[:])
	return nil
}

func (c RecoveryContext) Clone() *RecoveryContext { c = c.Normalize(); return &c }

type ApprovalScope string

const (
	AllowSwitch    ApprovalScope = "ALLOW_SWITCH"
	BudgetIncrease ApprovalScope = "BUDGET_INCREASE"
)

type ParentApprovalRequest struct {
	ApprovalID                   string        `json:"approval_id"`
	EpisodeID                    string        `json:"episode_id"`
	RecoveryID                   string        `json:"recovery_id"`
	ReasonCode                   ReasonCode    `json:"reason_code"`
	RequestedAction              string        `json:"requested_action"`
	ApprovalScope                ApprovalScope `json:"approval_scope"`
	CurrentBudgetMinor           int64         `json:"current_budget_minor"`
	ConsumedMinor                int64         `json:"consumed_minor"`
	AvailableMinor               int64         `json:"available_minor"`
	SunkCostMinor                int64         `json:"sunk_cost_minor"`
	CurrentMerchantDID           string        `json:"current_merchant_did"`
	CandidateSetID               string        `json:"candidate_set_id,omitempty"`
	CandidateMerchantDID         string        `json:"candidate_merchant_did,omitempty"`
	CandidateCapabilityID        string        `json:"candidate_capability_id,omitempty"`
	RequestedBudgetIncreaseMinor int64         `json:"requested_budget_increase_minor,omitempty"`
	ExpiresAt                    time.Time     `json:"expires_at"`
	FactsRef                     string        `json:"facts_ref"`
	PayloadHash                  string        `json:"payload_hash"`
	CreatedAt                    time.Time     `json:"created_at"`
}

func (r ParentApprovalRequest) Validate() error {
	if strings.TrimSpace(r.ApprovalID) == "" || strings.TrimSpace(r.EpisodeID) == "" || strings.TrimSpace(r.RecoveryID) == "" || !r.ReasonCode.Valid() || strings.TrimSpace(r.RequestedAction) == "" || (r.ApprovalScope != AllowSwitch && r.ApprovalScope != BudgetIncrease) || r.CurrentBudgetMinor < 0 || r.ConsumedMinor < 0 || r.AvailableMinor < 0 || r.SunkCostMinor < 0 || r.ExpiresAt.IsZero() || r.FactsRef == "" || r.PayloadHash == "" || r.CreatedAt.IsZero() {
		return ErrInvalidParentRequest
	}
	if r.RequestedBudgetIncreaseMinor < 0 {
		return ErrInvalidParentRequest
	}
	return nil
}
func (r ParentApprovalRequest) Clone() *ParentApprovalRequest { c := r; return &c }

type Decision string

const (
	Approve Decision = "APPROVE"
	Deny    Decision = "DENY"
)

type ParentDecisionFact struct {
	ApprovalID  string    `json:"approval_id"`
	EpisodeID   string    `json:"episode_id"`
	Decision    Decision  `json:"decision"`
	ActorRef    string    `json:"actor_ref"`
	OccurredAt  time.Time `json:"occurred_at"`
	FactsRef    string    `json:"facts_ref"`
	PayloadHash string    `json:"payload_hash"`
}

func (d ParentDecisionFact) Validate() error {
	if strings.TrimSpace(d.ApprovalID) == "" || strings.TrimSpace(d.EpisodeID) == "" || (d.Decision != Approve && d.Decision != Deny) || strings.TrimSpace(d.ActorRef) == "" || d.OccurredAt.IsZero() || d.FactsRef == "" || d.PayloadHash == "" {
		return ErrInvalidParentDecision
	}
	return nil
}
func (d ParentDecisionFact) Clone() *ParentDecisionFact { c := d; return &c }

type BudgetAmendment struct {
	AmendmentID   string    `json:"amendment_id"`
	EpisodeID     string    `json:"episode_id"`
	OldLimitMinor int64     `json:"old_limit_minor"`
	NewLimitMinor int64     `json:"new_limit_minor"`
	DeltaMinor    int64     `json:"delta_minor"`
	ApprovedBy    string    `json:"approved_by"`
	ApprovalRef   string    `json:"approval_ref"`
	OccurredAt    time.Time `json:"occurred_at"`
	FactsRef      string    `json:"facts_ref"`
	PayloadHash   string    `json:"payload_hash"`
}

func (b BudgetAmendment) Validate() error {
	if strings.TrimSpace(b.AmendmentID) == "" || strings.TrimSpace(b.EpisodeID) == "" || b.OldLimitMinor < 0 || b.NewLimitMinor < 0 || b.DeltaMinor != b.NewLimitMinor-b.OldLimitMinor || b.NewLimitMinor < b.OldLimitMinor || strings.TrimSpace(b.ApprovedBy) == "" || strings.TrimSpace(b.ApprovalRef) == "" || b.OccurredAt.IsZero() || b.FactsRef == "" || b.PayloadHash == "" {
		return ErrInvalidBudgetAmendment
	}
	return nil
}
func (b BudgetAmendment) Clone() *BudgetAmendment { c := b; return &c }
