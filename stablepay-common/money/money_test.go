package money

import (
	"math/big"
	"testing"

	"github.com/shopspring/decimal"
)

func TestCurrencyKindAndPrecision(t *testing.T) {
	tests := []struct {
		name      string
		currency  Currency
		wantKind  CurrencyKind
		wantPrec  int
		wantValid bool
	}{
		{"USD", USD, CurrencyKindFiat, 2, true},
		{"JPY", JPY, CurrencyKindFiat, 0, true},
		{"KWD", KWD, CurrencyKindFiat, 3, true},
		{"USDT", USDT, CurrencyKindStablecoin, 2, true},
		{"USDC", USDC, CurrencyKindStablecoin, 2, true},
		{"InvalidEmpty", Currency{}, CurrencyKindUnknown, 0, false},
		{"InvalidKind", Currency{Code: "USD", Precision: 2, Kind: CurrencyKindUnknown}, CurrencyKindUnknown, 2, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.currency.Kind != tt.wantKind {
				t.Fatalf("Kind=%v, want=%v", tt.currency.Kind, tt.wantKind)
			}
			if tt.currency.GetPrecision() != tt.wantPrec {
				t.Fatalf("Precision=%v, want=%v", tt.currency.GetPrecision(), tt.wantPrec)
			}
			if tt.currency.IsValid() != tt.wantValid {
				t.Fatalf("IsValid=%v, want=%v", tt.currency.IsValid(), tt.wantValid)
			}
		})
	}
}

func TestNewMoneyFromString_Stablecoin2dp(t *testing.T) {
	m, err := NewMoneyFromString("123", USDT) // 1.23
	if err != nil {
		t.Fatalf("NewMoneyFromString err=%v", err)
	}
	if got := m.GetString(); got != "123" {
		t.Fatalf("GetString=%s, want=123", got)
	}
	if got := m.FormatDecimalAmount(); got != "1.23" {
		t.Fatalf("FormatDecimalAmount=%s, want=1.23", got)
	}
}

func TestKWD_3dp_Format(t *testing.T) {
	m, err := NewMoneyFromString("1234", KWD) // 1.234
	if err != nil {
		t.Fatalf("NewMoneyFromString err=%v", err)
	}
	if got := m.FormatDecimalAmount(); got != "1.234" {
		t.Fatalf("FormatDecimalAmount=%s, want=1.234", got)
	}
}

func TestNewMoneyFromDecimalString_DefaultHalfEven(t *testing.T) {
	// USD 2位：1.005 -> 1.00（100.5 cents，Half-Even -> 100）
	m1, err := NewMoneyFromDecimalString("1.005", USD)
	if err != nil {
		t.Fatalf("NewMoneyFromDecimalString err=%v", err)
	}
	if got := m1.GetString(); got != "100" {
		t.Fatalf("minor=%s, want=100", got)
	}
	if got := m1.FormatDecimalAmount(); got != "1.00" {
		t.Fatalf("decimal=%s, want=1.00", got)
	}

	// 1.015 -> 1.02（101.5 cents，Half-Even -> 102）
	m2, err := NewMoneyFromDecimalString("1.015", USD)
	if err != nil {
		t.Fatalf("NewMoneyFromDecimalString err=%v", err)
	}
	if got := m2.GetString(); got != "102" {
		t.Fatalf("minor=%s, want=102", got)
	}
	if got := m2.FormatDecimalAmount(); got != "1.02" {
		t.Fatalf("decimal=%s, want=1.02", got)
	}
}

func TestBigInt_MinorString_RoundTrip(t *testing.T) {
	huge := new(big.Int)
	huge.SetString("123456789012345678901234567890", 10)
	m, err := NewMoneyFromBigInt(huge, USD)
	if err != nil {
		t.Fatalf("NewMoneyFromBigInt err=%v", err)
	}
	if got := m.GetString(); got != huge.String() {
		t.Fatalf("GetString=%s, want=%s", got, huge.String())
	}
}

func TestNewMoneyFromDecimal_RoundBank(t *testing.T) {
	// 直接测试 NewMoneyFromDecimal 的舍入模式（银行家舍入）
	amount := decimal.NewFromFloat(1.005)
	m, err := NewMoneyFromDecimal(amount, USD)
	if err != nil {
		t.Fatalf("NewMoneyFromDecimal err=%v", err)
	}
	if got := m.GetString(); got != "100" {
		t.Fatalf("minor=%s, want=100", got)
	}
}


