package ledger

import (
	"errors"
	"testing"
	"time"
)

func ledgerEntry(sequence uint64, entryType EntryType, amount int64, key string) *LedgerEntry {
	entry := &LedgerEntry{
		EntryID: "entry-" + key, EpisodeID: "episode-1", Sequence: sequence, Type: entryType,
		Currency: "USDC", AmountMinor: amount, PaymentIntentID: "intent-1", IdempotencyKey: key,
		OccurredAt: time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC), TraceID: "trace-1", ReferenceHash: HashReference(key),
	}
	if entryType == EntryRefundConfirmed {
		entry.RefundID = "refund-" + key
	}
	return entry
}

func TestBuildProjectionReserveSettleReleaseRefund(t *testing.T) {
	entries := []*LedgerEntry{
		ledgerEntry(1, EntryBudgetReserved, 300, "reserve"),
		ledgerEntry(2, EntryPaymentSettled, 300, "settled"),
		ledgerEntry(3, EntryBudgetReleased, 300, "release"),
		ledgerEntry(4, EntryRefundConfirmed, 100, "refund"),
	}
	projection, err := BuildProjection("USDC", 1000, true, entries)
	if err != nil {
		t.Fatal(err)
	}
	if projection.ReservedAmount != 0 || projection.SettledAmount != 300 || projection.RefundedAmount != 100 || projection.ConsumedAmount != 200 || projection.AvailableBudget != 800 || projection.SunkCost != 200 {
		t.Fatalf("unexpected projection: %#v", projection)
	}
}

func TestProjectionRejectsInsufficientReserveAndOverRefund(t *testing.T) {
	if _, err := BuildProjection("USDC", 100, true, []*LedgerEntry{ledgerEntry(1, EntryBudgetReserved, 101, "too-much")}); !errors.Is(err, ErrInsufficientBudget) {
		t.Fatalf("expected insufficient budget, got %v", err)
	}
	entries := []*LedgerEntry{ledgerEntry(1, EntryPaymentSettled, 100, "settled")}
	refund := ledgerEntry(2, EntryRefundConfirmed, 101, "refund")
	if _, err := BuildProjection("USDC", 200, true, append(entries, refund)); !errors.Is(err, ErrRefundInvariant) {
		t.Fatalf("expected refund invariant, got %v", err)
	}
}

func TestProjectionDuplicateEntryIsIdempotentButChangedBodyConflicts(t *testing.T) {
	entry := ledgerEntry(1, EntryBudgetReserved, 40, "reserve")
	projection, err := BuildProjection("USDC", 100, true, []*LedgerEntry{entry, entry})
	if err != nil {
		t.Fatal(err)
	}
	if projection.ReservedAmount != 40 || projection.AvailableBudget != 60 {
		t.Fatalf("duplicate entry changed projection: %#v", projection)
	}
	changed := *entry
	changed.AmountMinor = 41
	if _, err := BuildProjection("USDC", 100, true, []*LedgerEntry{entry, &changed}); !errors.Is(err, ErrDuplicateEntry) {
		t.Fatalf("expected duplicate entry conflict, got %v", err)
	}
	changed = *entry
	changed.EntryID = "entry-other"
	changed.AmountMinor = 41
	if _, err := BuildProjection("USDC", 100, true, []*LedgerEntry{entry, &changed}); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}
}

func TestNonReusableRefundKeepsCapacityConsumed(t *testing.T) {
	entries := []*LedgerEntry{ledgerEntry(1, EntryPaymentSettled, 70, "settled"), ledgerEntry(2, EntryRefundConfirmed, 20, "refund")}
	projection, err := BuildProjection("USDC", 100, false, entries)
	if err != nil {
		t.Fatal(err)
	}
	if projection.ConsumedAmount != 70 || projection.AvailableBudget != 30 || projection.SunkCost != 50 {
		t.Fatalf("unexpected non-reusable refund projection: %#v", projection)
	}
}
