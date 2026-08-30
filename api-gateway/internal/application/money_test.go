package application

import "testing"

func TestNormalizeBusinessAmount(t *testing.T) {
	req := map[string]interface{}{
		"amount":   "5.00",
		"currency": "USDC",
	}

	if err := normalizeBusinessAmount(req); err != nil {
		t.Fatalf("normalize amount failed: %v", err)
	}

	if req["amount"] != "5.00" {
		t.Fatalf("unexpected amount: %v", req["amount"])
	}
	if req["amount_minor"] != "500" {
		t.Fatalf("unexpected amount_minor: %v", req["amount_minor"])
	}
}

func TestNormalizeBusinessAmountRejectsUnsupportedCurrency(t *testing.T) {
	req := map[string]interface{}{
		"amount":   "5.00",
		"currency": "BTC",
	}

	if err := normalizeBusinessAmount(req); err == nil {
		t.Fatal("expected unsupported currency error")
	}
}
