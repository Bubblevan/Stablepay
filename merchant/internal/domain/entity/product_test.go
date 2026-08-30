package entity

import "testing"

func TestProductIsPurchasable(t *testing.T) {
	product := NewProductBuilder().
		WithSKUID("test-sku").
		WithTitle("Test Product").
		WithPrice("1.50", CurrencyUSDC).
		WithStatus(ProductStatusActive).
		WithSellerAddress("seller111").
		MustBuild()

	if !product.IsPurchasable() {
		t.Fatal("expected product to be purchasable")
	}
	if got := product.GetPriceMinorUnits(); got != "1500000" {
		t.Fatalf("got %s, want 1500000", got)
	}
}

func TestProductDraftIsNotPurchasable(t *testing.T) {
	product := NewProductBuilder().
		WithSKUID("test-sku").
		WithTitle("Test Product").
		WithPrice("1.50", CurrencyUSDC).
		WithStatus(ProductStatusDraft).
		WithSellerAddress("seller111").
		MustBuild()

	if product.IsPurchasable() {
		t.Fatal("expected draft product not to be purchasable")
	}
}

func TestProductValidateBasicRejectsMissingFields(t *testing.T) {
	_, err := NewProductBuilder().WithSKUID("").WithTitle("").Build()
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestProductActivateDeactivate(t *testing.T) {
	p := NewProductBuilder().
		WithSKUID("x").
		WithTitle("X").
		WithPrice("1.00", CurrencyUSDC).
		MustBuild()

	if p.Status != ProductStatusDraft {
		t.Fatalf("default should be draft, got %s", p.Status)
	}

	p.Activate()
	if p.Status != ProductStatusActive {
		t.Fatalf("expected active, got %s", p.Status)
	}

	p.Deactivate()
	if p.Status != ProductStatusInactive {
		t.Fatalf("expected inactive, got %s", p.Status)
	}
}
