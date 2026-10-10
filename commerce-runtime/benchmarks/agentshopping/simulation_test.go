package agentshopping

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func testSimulator(t *testing.T, tenants ...string) *Simulator {
	t.Helper()
	rows := make([]Inventory, 0, len(tenants))
	for _, tenant := range tenants {
		rows = append(rows, Inventory{
			TenantID: tenant, MerchantID: "merchant-1", SKUID: "sku-1", Region: "CN-EAST",
			OnHand: 3, Eligible: true, PriceMinor: 1000, Revision: 1,
		})
	}
	sim, err := NewSimulator(rows)
	if err != nil {
		t.Fatalf("NewSimulator: %v", err)
	}
	return sim
}

func issueTestQuote(t *testing.T, sim *Simulator, tenant string, max int64) Quote {
	t.Helper()
	quote, err := sim.IssueQuote(QuoteRequest{
		TenantID: tenant, TaskID: "task-1", MerchantID: "merchant-1", SKUID: "sku-1",
		Region: "CN-EAST", MaxMinor: max, Currency: "USDC",
	})
	if err != nil {
		t.Fatalf("IssueQuote: %v", err)
	}
	return quote
}

func TestRejectedConfirmationHasNoOrderOrPayment(t *testing.T) {
	sim := testSimulator(t, "tenant-a")
	quote := issueTestQuote(t, sim, "tenant-a", 1000)
	confirmation, err := sim.Confirm("tenant-a", quote.ID, false, quote.AmountMinor)
	if err != nil {
		t.Fatalf("Confirm rejection: %v", err)
	}
	if _, err := sim.Checkout("tenant-a", confirmation.ID, "idem-rejected"); !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("Checkout rejected confirmation error = %v, want ErrConfirmationRequired", err)
	}
	_, _, _, orders, payments, _ := sim.Snapshot()
	if len(orders) != 0 || len(payments) != 0 {
		t.Fatalf("rejected consent created side effects: orders=%d payments=%d", len(orders), len(payments))
	}
}

func TestPriceChangeInvalidatesOldQuoteAndRequiresFreshConsent(t *testing.T) {
	sim := testSimulator(t, "tenant-a")
	oldQuote := issueTestQuote(t, sim, "tenant-a", 1000)
	newPrice := int64(1200)
	if err := sim.ChangeInventory("tenant-a", "merchant-1", "sku-1", "CN-EAST", &newPrice, nil); err != nil {
		t.Fatalf("ChangeInventory: %v", err)
	}
	if _, err := sim.Confirm("tenant-a", oldQuote.ID, true, oldQuote.AmountMinor); !errors.Is(err, ErrStaleQuote) {
		t.Fatalf("Confirm stale quote error = %v, want ErrStaleQuote", err)
	}
	newQuote := issueTestQuote(t, sim, "tenant-a", 1200)
	if newQuote.ID == oldQuote.ID || newQuote.Version <= oldQuote.Version || oldQuote.AmountMinor != 1000 || newQuote.AmountMinor != 1200 {
		t.Fatalf("quote version/immutability failed: old=%+v new=%+v", oldQuote, newQuote)
	}
	confirmation, err := sim.Confirm("tenant-a", newQuote.ID, true, newQuote.AmountMinor)
	if err != nil {
		t.Fatalf("Confirm fresh quote: %v", err)
	}
	result, err := sim.Checkout("tenant-a", confirmation.ID, "idem-price-change")
	if err != nil || result.Order.Status != OrderPaid || result.Payment.Status != PaymentSettled {
		t.Fatalf("fresh consent checkout = (%+v, %v)", result, err)
	}
	_, quotes, _, orders, payments, _ := sim.Snapshot()
	if len(quotes) != 2 || len(orders) != 1 || len(payments) != 1 {
		t.Fatalf("unexpected history sizes: quotes=%d orders=%d payments=%d", len(quotes), len(orders), len(payments))
	}
}

func TestStockChangeAfterConfirmationBlocksPayment(t *testing.T) {
	sim := testSimulator(t, "tenant-a")
	quote := issueTestQuote(t, sim, "tenant-a", 1000)
	confirmation, err := sim.Confirm("tenant-a", quote.ID, true, quote.AmountMinor)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	zero := 0
	if err := sim.ChangeInventory("tenant-a", "merchant-1", "sku-1", "CN-EAST", nil, &zero); err != nil {
		t.Fatalf("ChangeInventory: %v", err)
	}
	if _, err := sim.Checkout("tenant-a", confirmation.ID, "idem-stock-loss"); !errors.Is(err, ErrStaleQuote) {
		t.Fatalf("Checkout after stock loss error = %v, want ErrStaleQuote", err)
	}
	_, _, _, orders, payments, _ := sim.Snapshot()
	if len(orders) != 0 || len(payments) != 0 {
		t.Fatalf("stale checkout created side effects: orders=%d payments=%d", len(orders), len(payments))
	}
}

func TestDeclinedMockPaymentReleasesReservationAndIsIdempotent(t *testing.T) {
	sim := testSimulator(t, "tenant-a")
	if err := sim.SetMockPaymentOutcome(PaymentDeclined); err != nil {
		t.Fatalf("SetMockPaymentOutcome: %v", err)
	}
	quote := issueTestQuote(t, sim, "tenant-a", 1000)
	confirmation, err := sim.Confirm("tenant-a", quote.ID, true, quote.AmountMinor)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	first, err := sim.Checkout("tenant-a", confirmation.ID, "idem-decline")
	if err != nil || first.Order.Status != OrderPaymentError || first.Payment.Status != PaymentDeclined {
		t.Fatalf("declined checkout = (%+v, %v)", first, err)
	}
	second, err := sim.Checkout("tenant-a", confirmation.ID, "idem-decline")
	if err != nil || second.Order.ID != first.Order.ID || second.Payment.ID != first.Payment.ID {
		t.Fatalf("idempotent retry = (%+v, %v), first=(%+v)", second, err, first)
	}
	inventory, _, _, orders, payments, _ := sim.Snapshot()
	if len(inventory) != 1 || inventory[0].OnHand != 3 || inventory[0].Reserved != 0 || len(orders) != 1 || len(payments) != 1 {
		t.Fatalf("decline did not release stock exactly once: inventory=%+v orders=%d payments=%d", inventory, len(orders), len(payments))
	}
}

func TestTenantBoundaryAndIdempotencyConflict(t *testing.T) {
	sim := testSimulator(t, "tenant-a", "tenant-b")
	quote := issueTestQuote(t, sim, "tenant-a", 1000)
	if _, err := sim.Confirm("tenant-b", quote.ID, true, quote.AmountMinor); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant confirmation error = %v, want ErrNotFound", err)
	}
	confirmation, err := sim.Confirm("tenant-a", quote.ID, true, quote.AmountMinor)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if _, err := sim.Checkout("tenant-a", confirmation.ID, "shared-idem"); err != nil {
		t.Fatalf("first Checkout: %v", err)
	}
	otherQuote := issueTestQuote(t, sim, "tenant-b", 1000)
	otherConfirmation, err := sim.Confirm("tenant-b", otherQuote.ID, true, otherQuote.AmountMinor)
	if err != nil {
		t.Fatalf("tenant-b Confirm: %v", err)
	}
	if _, err := sim.Checkout("tenant-a", otherConfirmation.ID, "shared-idem"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant checkout error = %v, want ErrNotFound", err)
	}
	// Same-tenant reuse of a key for another confirmation is rejected.
	otherAQuote := issueTestQuote(t, sim, "tenant-a", 1000)
	otherAConfirmation, err := sim.Confirm("tenant-a", otherAQuote.ID, true, otherAQuote.AmountMinor)
	if err != nil {
		t.Fatalf("second same-tenant Confirm: %v", err)
	}
	if _, err := sim.Checkout("tenant-a", otherAConfirmation.ID, "shared-idem"); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("idempotency conflict error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestConcurrentCheckoutCreatesOneOrderAndPayment(t *testing.T) {
	sim := testSimulator(t, "tenant-a")
	quote := issueTestQuote(t, sim, "tenant-a", 1000)
	confirmation, err := sim.Confirm("tenant-a", quote.ID, true, quote.AmountMinor)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	const workers = 24
	results := make(chan CheckoutResult, workers)
	errorsSeen := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := sim.Checkout("tenant-a", confirmation.ID, "idem-concurrent")
			if err != nil {
				errorsSeen <- err
				return
			}
			results <- result
		}()
	}
	wg.Wait()
	close(results)
	close(errorsSeen)
	for err := range errorsSeen {
		t.Errorf("concurrent Checkout: %v", err)
	}
	var orderID, paymentID string
	for result := range results {
		if orderID == "" {
			orderID, paymentID = result.Order.ID, result.Payment.ID
		}
		if result.Order.ID != orderID || result.Payment.ID != paymentID {
			t.Errorf("idempotent requests returned different records: %+v", result)
		}
	}
	inventory, _, _, orders, payments, _ := sim.Snapshot()
	if len(orders) != 1 || len(payments) != 1 || inventory[0].OnHand != 2 || inventory[0].Reserved != 0 {
		t.Fatalf("concurrent checkout duplicated effects: inventory=%+v orders=%d payments=%d", inventory, len(orders), len(payments))
	}
	if orderID == "" || paymentID == "" {
		t.Fatal(fmt.Sprintf("no successful results received: order=%q payment=%q", orderID, paymentID))
	}
}
