// Package episode owns the CommerceEpisode aggregate and its explicit state machine.
package episode

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

type State string

const (
	StateAccepted           State = "ACCEPTED"
	StateDiscovering        State = "DISCOVERING"
	StateInvoking           State = "INVOKING"
	StateNegotiating        State = "NEGOTIATING"
	StatePaying             State = "PAYING"
	StateClaiming           State = "CLAIMING"
	StateInvokingDelivery   State = "INVOKING_DELIVERY"
	StateValidatingDelivery State = "VALIDATING_DELIVERY"
	StateRecovering         State = "RECOVERING"
	StateFulfilled          State = "FULFILLED"
	StateFailed             State = "FAILED"
	StateBlocked            State = "BLOCKED"
	StateAborted            State = "ABORTED"
	StateExpired            State = "EXPIRED"
	StateCompensating       State = "COMPENSATING"
	StateDisputed           State = "DISPUTED"

	// StateCreated is kept as an API alias for clients that describe the initial
	// projection as CREATED. The canonical persisted state is ACCEPTED.
	StateCreated = StateAccepted
	// SETTLING was the earlier draft name for PAYING. Persist only PAYING.
	StateSettling = StatePaying
)

var (
	ErrInvalidTransition   = errors.New("invalid episode state transition")
	ErrTerminalEpisode     = errors.New("episode is terminal")
	ErrEpisodeExpired      = errors.New("episode deadline has expired")
	ErrImmutableContract   = errors.New("contract snapshot is immutable")
	ErrInvalidEpisode      = errors.New("invalid commerce episode")
	ErrIdempotencyConflict = errors.New("idempotency key conflicts with an existing transition")
)

type BudgetSnapshot struct {
	Currency         string `json:"currency"`
	BudgetLimitMinor int64  `json:"budget_limit_minor"`
	ReservedAmount   int64  `json:"reserved_amount"`
	SettledAmount    int64  `json:"settled_amount"`
	RefundedAmount   int64  `json:"refunded_amount"`
	ConsumedAmount   int64  `json:"consumed_amount"`
	AvailableBudget  int64  `json:"available_budget"`
	SunkCost         int64  `json:"sunk_cost"`
	RefundReusable   bool   `json:"refund_reusable"`
}

// Recalculate applies the TRD budget semantics. With reusable refunds:
// consumed=max(0, settled-refunded), available=limit-consumed-reserved.
// When RefundReusable is false, refunds remain accounting evidence but do not
// return capacity to available budget; consumed is settled amount.
func (b *BudgetSnapshot) Recalculate() error {
	if b == nil || strings.TrimSpace(b.Currency) == "" || b.BudgetLimitMinor < 0 || b.ReservedAmount < 0 || b.SettledAmount < 0 || b.RefundedAmount < 0 {
		return ErrInvalidEpisode
	}
	consumed := maxInt64(0, b.SettledAmount-b.RefundedAmount)
	if !b.RefundReusable {
		consumed = b.SettledAmount
	}
	if consumed < 0 || consumed > b.BudgetLimitMinor || b.ReservedAmount > b.BudgetLimitMinor-consumed {
		return ErrInvalidEpisode
	}
	b.ConsumedAmount = consumed
	b.AvailableBudget = b.BudgetLimitMinor - consumed - b.ReservedAmount
	b.SunkCost = maxInt64(0, b.SettledAmount-b.RefundedAmount)
	return nil
}

func (b BudgetSnapshot) Validate() error {
	copy := b
	if err := copy.Recalculate(); err != nil {
		return err
	}
	if copy.ConsumedAmount != b.ConsumedAmount || copy.AvailableBudget != b.AvailableBudget || copy.SunkCost != b.SunkCost {
		return ErrInvalidEpisode
	}
	return nil
}

type CommerceEpisode struct {
	EpisodeID               string         `json:"episode_id"`
	RequestID               string         `json:"request_id"`
	SessionID               string         `json:"session_id,omitempty"`
	State                   State          `json:"state"`
	TerminalReason          string         `json:"terminal_reason,omitempty"`
	ContractSnapshotHash    string         `json:"contract_snapshot_hash"`
	ContractSnapshot        []byte         `json:"contract_snapshot"`
	SelectedMerchantDID     string         `json:"selected_merchant_did,omitempty"`
	SelectedCapabilityID    string         `json:"selected_capability_id,omitempty"`
	SelectedWorkflowVersion string         `json:"selected_workflow_version,omitempty"`
	CurrentQuoteHash        string         `json:"current_quote_hash,omitempty"`
	EntitlementRefs         []string       `json:"entitlement_refs,omitempty"`
	DeliveryRefs            []string       `json:"delivery_refs,omitempty"`
	ValidationEvidenceRefs  []string       `json:"validation_evidence_refs,omitempty"`
	AttemptedMerchants      []string       `json:"attempted_merchants,omitempty"`
	ActionCount             int            `json:"action_count"`
	PaymentAttemptCount     int            `json:"payment_attempt_count"`
	DeliveryAttemptCount    int            `json:"delivery_attempt_count"`
	RetryCount              int            `json:"retry_count"`
	MaxTotalAttempts        int            `json:"max_total_attempts"`
	MaxPaymentAttempts      int            `json:"max_payment_attempts"`
	MaxDeliveryAttempts     int            `json:"max_delivery_attempts"`
	Budget                  BudgetSnapshot `json:"budget"`
	DeadlineAt              time.Time      `json:"deadline_at"`
	Version                 uint64         `json:"version"`
	CreatedAt               time.Time      `json:"created_at"`
	UpdatedAt               time.Time      `json:"updated_at"`
}

func New(episodeID string, request contract.AcquireCapabilityRequest, now time.Time) (*CommerceEpisode, error) {
	if strings.TrimSpace(episodeID) == "" {
		return nil, fmt.Errorf("%w: episode_id is required", ErrInvalidEpisode)
	}
	normalized, err := request.NormalizeAt(now)
	if err != nil {
		return nil, err
	}
	snapshot, err := normalized.CanonicalSnapshot()
	if err != nil {
		return nil, err
	}
	hash, err := normalized.SnapshotHash()
	if err != nil {
		return nil, err
	}
	now = now.UTC()
	return &CommerceEpisode{
		EpisodeID:            episodeID,
		RequestID:            normalized.RequestID,
		SessionID:            normalized.ParentSessionID,
		State:                StateAccepted,
		ContractSnapshotHash: hash,
		ContractSnapshot:     append([]byte(nil), snapshot...),
		Budget: BudgetSnapshot{
			Currency:         normalized.Constraints.Currency,
			BudgetLimitMinor: normalized.Constraints.BudgetLimitMinor,
			AvailableBudget:  normalized.Constraints.BudgetLimitMinor,
			RefundReusable:   true,
		},
		MaxTotalAttempts:    normalized.Constraints.MaxTotalAttempts,
		MaxPaymentAttempts:  normalized.Constraints.MaxPaymentAttempts,
		MaxDeliveryAttempts: normalized.Constraints.MaxDeliveryAttempts,
		DeadlineAt:          normalized.Constraints.DeadlineAt,
		Version:             1,
		CreatedAt:           now,
		UpdatedAt:           now,
	}, nil
}

func (e *CommerceEpisode) Validate() error {
	if e == nil || strings.TrimSpace(e.EpisodeID) == "" || strings.TrimSpace(e.RequestID) == "" {
		return ErrInvalidEpisode
	}
	if !KnownState(e.State) || e.Version == 0 || e.ContractSnapshotHash == "" || len(e.ContractSnapshot) == 0 {
		return ErrInvalidEpisode
	}
	digest := sha256.Sum256(e.ContractSnapshot)
	if e.ContractSnapshotHash != "sha256:"+hex.EncodeToString(digest[:]) {
		return ErrInvalidEpisode
	}
	if e.DeadlineAt.IsZero() || e.CreatedAt.IsZero() || e.UpdatedAt.IsZero() {
		return ErrInvalidEpisode
	}
	if err := e.Budget.Validate(); err != nil {
		return ErrInvalidEpisode
	}
	if e.MaxTotalAttempts <= 0 || e.MaxPaymentAttempts <= 0 || e.MaxDeliveryAttempts <= 0 ||
		e.MaxPaymentAttempts > e.MaxTotalAttempts || e.MaxDeliveryAttempts > e.MaxTotalAttempts {
		return ErrInvalidEpisode
	}
	if IsTerminal(e.State) && strings.TrimSpace(e.TerminalReason) == "" {
		return fmt.Errorf("%w: terminal_reason is required for terminal state", ErrInvalidEpisode)
	}
	return nil
}

func (e *CommerceEpisode) ApplyTransition(to State, now time.Time, reason string) error {
	if e == nil {
		return ErrInvalidEpisode
	}
	if IsTerminal(e.State) {
		return ErrTerminalEpisode
	}
	if !CanTransition(e.State, to) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, e.State, to)
	}
	if !now.IsZero() && !now.Before(e.DeadlineAt) && to != StateExpired {
		return ErrEpisodeExpired
	}
	e.State = to
	if IsTerminal(to) {
		e.TerminalReason = strings.TrimSpace(reason)
		if e.TerminalReason == "" {
			e.TerminalReason = strings.ToLower(string(to))
		}
	}
	e.Version++
	e.UpdatedAt = now.UTC()
	return nil
}

// ApplyCommittedState advances the projection for a committed event. A
// same-state event is legal and still increments the aggregate version.
func (e *CommerceEpisode) ApplyCommittedState(to State, now time.Time, reason string) error {
	if e == nil {
		return ErrInvalidEpisode
	}
	if to == e.State {
		if IsTerminal(e.State) {
			return ErrTerminalEpisode
		}
		if !now.IsZero() && !now.Before(e.DeadlineAt) {
			return ErrEpisodeExpired
		}
		e.Version++
		e.UpdatedAt = now.UTC()
		return nil
	}
	return e.ApplyTransition(to, now, reason)
}

func AllowedTransitions(from State) []State {
	transitions := map[State][]State{
		StateAccepted:           {StateDiscovering, StateAborted, StateExpired},
		StateDiscovering:        {StateInvoking, StateFailed, StateBlocked, StateAborted, StateExpired},
		StateInvoking:           {StateNegotiating, StateInvokingDelivery, StateFailed, StateBlocked, StateAborted, StateExpired},
		StateNegotiating:        {StatePaying, StateBlocked, StateFailed, StateAborted, StateExpired},
		StatePaying:             {StateClaiming, StateFailed, StateBlocked, StateAborted, StateExpired},
		StateClaiming:           {StateInvokingDelivery, StateFailed, StateBlocked, StateAborted, StateExpired},
		StateInvokingDelivery:   {StateValidatingDelivery, StateFailed, StateAborted, StateExpired},
		StateValidatingDelivery: {StateFulfilled, StateRecovering, StateFailed, StateExpired},
		StateRecovering:         {StateInvokingDelivery, StateFailed, StateAborted, StateExpired},
	}
	return append([]State(nil), transitions[from]...)
}

func CanTransition(from, to State) bool {
	for _, candidate := range AllowedTransitions(from) {
		if candidate == to {
			return true
		}
	}
	return false
}

func KnownState(state State) bool {
	switch state {
	case StateAccepted, StateDiscovering, StateInvoking, StateNegotiating, StatePaying,
		StateClaiming, StateInvokingDelivery, StateValidatingDelivery, StateRecovering,
		StateFulfilled, StateFailed, StateBlocked, StateAborted, StateExpired,
		StateCompensating, StateDisputed:
		return true
	default:
		return false
	}
}

func IsTerminal(state State) bool {
	switch state {
	case StateFulfilled, StateFailed, StateBlocked, StateAborted, StateExpired, StateDisputed:
		return true
	default:
		return false
	}
}

// StateForAction is the deterministic runtime mapping. A proposal can name an
// action, but cannot choose an arbitrary next state.
func StateForAction(from State, action trace.ActionType, observation trace.ObservationType) (State, bool) {
	switch action {
	case trace.ActionDiscover:
		return StateDiscovering, from == StateAccepted
	case trace.ActionSelectMerchant, trace.ActionInvoke:
		if action == trace.ActionInvoke && from == StateInvokingDelivery {
			return StateValidatingDelivery, true
		}
		return StateInvoking, from == StateDiscovering
	case trace.ActionParse402:
		return StateNegotiating, from == StateInvoking
	case trace.ActionReserveBudget, trace.ActionNegotiateAndPay:
		return StatePaying, from == StateNegotiating
	case trace.ActionCreatePayment:
		return StateClaiming, from == StatePaying
	case trace.ActionPaymentSubmitted, trace.ActionPaymentPending, trace.ActionPaymentStatusQueried:
		return StatePaying, from == StatePaying
	case trace.ActionVerifyEntitlement:
		return StateInvokingDelivery, from == StateClaiming
	case trace.ActionValidateDelivery:
		switch observation {
		case trace.ObservationDeliveryValid:
			return StateFulfilled, from == StateValidatingDelivery
		case trace.ObservationDeliveryInvalid:
			return StateRecovering, from == StateValidatingDelivery
		}
	case trace.ActionRetrySameMerchant:
		return StateInvokingDelivery, from == StateRecovering
	case trace.ActionStop:
		return StateFailed, from == StateRecovering || from == StateDiscovering || from == StateInvoking || from == StateNegotiating
	}
	return "", false
}

func (e *CommerceEpisode) Clone() *CommerceEpisode {
	if e == nil {
		return nil
	}
	copy := *e
	copy.ContractSnapshot = append([]byte(nil), e.ContractSnapshot...)
	copy.EntitlementRefs = append([]string(nil), e.EntitlementRefs...)
	copy.DeliveryRefs = append([]string(nil), e.DeliveryRefs...)
	copy.ValidationEvidenceRefs = append([]string(nil), e.ValidationEvidenceRefs...)
	copy.AttemptedMerchants = append([]string(nil), e.AttemptedMerchants...)
	return &copy
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
