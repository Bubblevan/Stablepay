// Package ledger contains append-only money facts and their deterministic
// budget projection. CommerceEpisode.Budget is only a cached projection; the
// entries in this package are the auditable financial facts.
package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/episode"
)

type EntryType string

const (
	EntryBudgetReserved       EntryType = "BUDGET_RESERVED"
	EntryBudgetReleased       EntryType = "BUDGET_RELEASED"
	EntryPaymentSettled       EntryType = "PAYMENT_SETTLED"
	EntryRefundConfirmed      EntryType = "REFUND_CONFIRMED"
	EntryCompensationRecorded EntryType = "COMPENSATION_RECORDED"
)

var (
	ErrInvalidEntry        = errors.New("invalid ledger entry")
	ErrDuplicateEntry      = errors.New("duplicate ledger entry")
	ErrIdempotencyConflict = errors.New("ledger idempotency key conflicts with an existing entry")
	ErrInsufficientBudget  = errors.New("insufficient available budget")
	ErrRefundInvariant     = errors.New("refund exceeds refundable settlement")
	ErrProjectionInvariant = errors.New("invalid budget projection")
)

// LedgerEntry is append-only. A refund is a new fact; it never edits or
// removes the settlement entry that made the payment economically real.
type LedgerEntry struct {
	EntryID         string    `json:"entry_id"`
	EpisodeID       string    `json:"episode_id"`
	Sequence        uint64    `json:"sequence"`
	Type            EntryType `json:"type"`
	Currency        string    `json:"currency"`
	AmountMinor     int64     `json:"amount_minor"`
	PaymentIntentID string    `json:"payment_intent_id,omitempty"`
	TxID            string    `json:"tx_id,omitempty"`
	RefundID        string    `json:"refund_id,omitempty"`
	IdempotencyKey  string    `json:"idempotency_key"`
	OccurredAt      time.Time `json:"occurred_at"`
	TraceID         string    `json:"trace_id,omitempty"`
	ReferenceHash   string    `json:"reference_hash"`
	MetadataHash    string    `json:"metadata_hash,omitempty"`
}

func (e LedgerEntry) Validate() error {
	if strings.TrimSpace(e.EntryID) == "" || strings.TrimSpace(e.EpisodeID) == "" || e.Sequence == 0 {
		return ErrInvalidEntry
	}
	if !KnownEntryType(e.Type) || strings.TrimSpace(e.Currency) == "" || e.AmountMinor <= 0 {
		return ErrInvalidEntry
	}
	if strings.TrimSpace(e.IdempotencyKey) == "" || e.OccurredAt.IsZero() || !strings.HasPrefix(e.ReferenceHash, "sha256:") {
		return ErrInvalidEntry
	}
	if e.MetadataHash != "" && !strings.HasPrefix(e.MetadataHash, "sha256:") {
		return ErrInvalidEntry
	}
	switch e.Type {
	case EntryBudgetReserved, EntryBudgetReleased, EntryPaymentSettled, EntryRefundConfirmed:
		if strings.TrimSpace(e.PaymentIntentID) == "" {
			return ErrInvalidEntry
		}
	}
	if e.Type == EntryRefundConfirmed && strings.TrimSpace(e.RefundID) == "" {
		return ErrInvalidEntry
	}
	return nil
}

func KnownEntryType(value EntryType) bool {
	switch value {
	case EntryBudgetReserved, EntryBudgetReleased, EntryPaymentSettled, EntryRefundConfirmed, EntryCompensationRecorded:
		return true
	default:
		return false
	}
}

func HashReference(value string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return "sha256:" + hex.EncodeToString(digest[:])
}

// BudgetProjection is computed from the append-only ledger. RefundReusable is
// explicit: true follows the TRD reusable-refund formula; false keeps refunds
// as audit facts but does not return that capacity to available budget.
type BudgetProjection struct {
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

func NewProjection(currency string, limit int64, refundReusable bool) BudgetProjection {
	projection := BudgetProjection{Currency: strings.ToUpper(strings.TrimSpace(currency)), BudgetLimitMinor: limit, RefundReusable: refundReusable}
	_ = projection.recalculate()
	return projection
}

// BuildProjection applies each unique entry once. Repeating the exact same
// entry is a replay and has no additional economic effect; reusing an entry ID
// or idempotency key with a different body is a conflict.
func BuildProjection(currency string, limit int64, refundReusable bool, entries []*LedgerEntry) (BudgetProjection, error) {
	projection := NewProjection(currency, limit, refundReusable)
	if projection.BudgetLimitMinor < 0 || projection.Currency == "" {
		return BudgetProjection{}, ErrProjectionInvariant
	}
	seenIDs := make(map[string]LedgerEntry)
	seenKeys := make(map[string]LedgerEntry)
	for _, entry := range entries {
		if entry == nil {
			return BudgetProjection{}, ErrInvalidEntry
		}
		if err := entry.Validate(); err != nil {
			return BudgetProjection{}, err
		}
		if existing, ok := seenIDs[entry.EntryID]; ok {
			if existing != *entry {
				return BudgetProjection{}, ErrDuplicateEntry
			}
			continue
		}
		if existing, ok := seenKeys[entry.IdempotencyKey]; ok {
			if existing != *entry {
				return BudgetProjection{}, ErrIdempotencyConflict
			}
			continue
		}
		seenIDs[entry.EntryID] = *entry
		seenKeys[entry.IdempotencyKey] = *entry
		if err := projection.Apply(*entry); err != nil {
			return BudgetProjection{}, err
		}
	}
	return projection, nil
}

func (p *BudgetProjection) Apply(entry LedgerEntry) error {
	if p == nil {
		return ErrProjectionInvariant
	}
	if err := entry.Validate(); err != nil {
		return err
	}
	if strings.ToUpper(entry.Currency) != p.Currency {
		return ErrProjectionInvariant
	}
	switch entry.Type {
	case EntryBudgetReserved:
		if p.AvailableBudget < entry.AmountMinor {
			return ErrInsufficientBudget
		}
		p.ReservedAmount += entry.AmountMinor
	case EntryBudgetReleased:
		if entry.AmountMinor > p.ReservedAmount {
			return ErrProjectionInvariant
		}
		p.ReservedAmount -= entry.AmountMinor
	case EntryPaymentSettled:
		p.SettledAmount += entry.AmountMinor
	case EntryRefundConfirmed:
		p.RefundedAmount += entry.AmountMinor
		if p.RefundedAmount > p.SettledAmount {
			return ErrRefundInvariant
		}
	case EntryCompensationRecorded:
		// Compensation is retained as an auditable fact. S2 does not invent a
		// second availability formula for it; sunk cost remains net settlement.
	}
	return p.recalculate()
}

func (p *BudgetProjection) recalculate() error {
	if p == nil || p.BudgetLimitMinor < 0 || p.ReservedAmount < 0 || p.SettledAmount < 0 || p.RefundedAmount < 0 || p.RefundedAmount > p.SettledAmount {
		return ErrProjectionInvariant
	}
	p.ConsumedAmount = maxInt64(0, p.SettledAmount-p.RefundedAmount)
	if !p.RefundReusable {
		p.ConsumedAmount = p.SettledAmount
	}
	if p.ConsumedAmount > p.BudgetLimitMinor || p.ReservedAmount > p.BudgetLimitMinor-p.ConsumedAmount {
		return ErrInsufficientBudget
	}
	p.AvailableBudget = p.BudgetLimitMinor - p.ConsumedAmount - p.ReservedAmount
	p.SunkCost = maxInt64(0, p.SettledAmount-p.RefundedAmount)
	return nil
}

func (p BudgetProjection) Validate() error {
	copy := p
	if err := copy.recalculate(); err != nil {
		return err
	}
	if copy.ConsumedAmount != p.ConsumedAmount || copy.AvailableBudget != p.AvailableBudget || copy.SunkCost != p.SunkCost {
		return ErrProjectionInvariant
	}
	return nil
}

func (p BudgetProjection) ToEpisodeBudget() episode.BudgetSnapshot {
	return episode.BudgetSnapshot{
		Currency: p.Currency, BudgetLimitMinor: p.BudgetLimitMinor, ReservedAmount: p.ReservedAmount,
		SettledAmount: p.SettledAmount, RefundedAmount: p.RefundedAmount, ConsumedAmount: p.ConsumedAmount,
		AvailableBudget: p.AvailableBudget, SunkCost: p.SunkCost, RefundReusable: p.RefundReusable,
	}
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
