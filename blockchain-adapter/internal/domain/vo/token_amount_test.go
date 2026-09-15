package vo

import "testing"

func TestBusinessMinorToTokenRaw(t *testing.T) {
	tests := []struct {
		name     string
		currency string
		minor    int64
		want     uint64
	}{
		{name: "USDC", currency: "USDC", minor: 1, want: 10000},
		{name: "USDT lowercase", currency: "usdt", minor: 1234, want: 12340000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BusinessMinorToTokenRaw(tt.currency, tt.minor)
			if err != nil {
				t.Fatalf("BusinessMinorToTokenRaw() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("BusinessMinorToTokenRaw() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBusinessMinorToTokenRawRejectsInvalidInput(t *testing.T) {
	for _, tt := range []struct {
		name     string
		currency string
		minor    int64
	}{
		{name: "zero", currency: "USDC", minor: 0},
		{name: "negative", currency: "USDC", minor: -1},
		{name: "unsupported currency", currency: "BTC", minor: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := BusinessMinorToTokenRaw(tt.currency, tt.minor); err == nil {
				t.Fatal("BusinessMinorToTokenRaw() should reject invalid input")
			}
		})
	}
}
