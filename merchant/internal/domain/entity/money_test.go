package entity

import "testing"

func TestDecimalToMinorUnits(t *testing.T) {
	tests := []struct {
		name   string
		amount string
		want   string
	}{
		{name: "whole", amount: "2", want: "2000000"},
		{name: "two decimals", amount: "2.00", want: "2000000"},
		{name: "one minor unit", amount: "0.000001", want: "1"},
		{name: "zero", amount: "0", want: "0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecimalToMinorUnits(tt.amount, USDCDecimals)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestDecimalToMinorUnitsRejectsTooManyDecimals(t *testing.T) {
	_, err := DecimalToMinorUnits("0.0000001", USDCDecimals)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDecimalToMinorUnitsRejectsSigned(t *testing.T) {
	_, err := DecimalToMinorUnits("-1.50", USDCDecimals)
	if err == nil {
		t.Fatal("expected error for negative amount")
	}
	_, err = DecimalToMinorUnits("+1.50", USDCDecimals)
	if err == nil {
		t.Fatal("expected error for positive sign")
	}
}

func TestDecimalToMinorUnitsRejectsNonDigit(t *testing.T) {
	_, err := DecimalToMinorUnits("1.5a", USDCDecimals)
	if err == nil {
		t.Fatal("expected error for non-digit")
	}
}
