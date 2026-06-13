// Copyright 2025 StablePay. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package entity

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

const (
	// CurrencyUSDC is the first supported settlement currency for StablePay MVP.
	CurrencyUSDC = "USDC"

	// USDCDecimals is the SPL USDC decimal precision used for minor units.
	USDCDecimals = 6
)

var (
	// ErrInvalidAmount indicates that a decimal amount string cannot be converted safely.
	ErrInvalidAmount = errors.New("invalid decimal amount")

	// ErrUnsupportedCurrency indicates that a product uses a currency unsupported by this merchant server.
	ErrUnsupportedCurrency = errors.New("unsupported currency")
)

// Money is a domain value object for token amounts.
//
// The API may display prices as decimal strings like "2.00", while x402
// PaymentRequirements.amount must use atomic token units like "2000000" for
// 6-decimal USDC. Money keeps this conversion deterministic and avoids floats.
type Money struct {
	AmountMinor string
	Currency    string
	Decimals    int
}

// NewMoneyFromDecimal converts a human-readable decimal string into minor units.
func NewMoneyFromDecimal(amount, currency string) (Money, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	decimals, err := CurrencyDecimals(currency)
	if err != nil {
		return Money{}, err
	}

	minor, err := DecimalToMinorUnits(amount, decimals)
	if err != nil {
		return Money{}, err
	}

	return Money{
		AmountMinor: minor,
		Currency:    currency,
		Decimals:    decimals,
	}, nil
}

// CurrencyDecimals returns token decimal precision for supported currencies.
func CurrencyDecimals(currency string) (int, error) {
	switch strings.ToUpper(strings.TrimSpace(currency)) {
	case CurrencyUSDC:
		return USDCDecimals, nil
	default:
		return 0, fmt.Errorf("%w: %s", ErrUnsupportedCurrency, currency)
	}
}

// DecimalToMinorUnits converts a non-negative decimal string to atomic units.
//
// Examples with decimals=6:
//   - "2"        -> "2000000"
//   - "2.00"     -> "2000000"
//   - "0.000001" -> "1"
//
// This function intentionally does not use float64, because float rounding is a
// bad fit for payment amounts.
func DecimalToMinorUnits(amount string, decimals int) (string, error) {
	amount = strings.TrimSpace(amount)
	if amount == "" || decimals < 0 {
		return "", fmt.Errorf("%w: empty amount or invalid decimals", ErrInvalidAmount)
	}
	if strings.HasPrefix(amount, "-") || strings.HasPrefix(amount, "+") {
		return "", fmt.Errorf("%w: signed amount is not allowed: %s", ErrInvalidAmount, amount)
	}

	parts := strings.Split(amount, ".")
	if len(parts) > 2 {
		return "", fmt.Errorf("%w: too many decimal points: %s", ErrInvalidAmount, amount)
	}

	whole := parts[0]
	frac := ""
	if len(parts) == 2 {
		frac = parts[1]
	}
	if whole == "" {
		whole = "0"
	}
	if whole == "" && frac == "" {
		return "", fmt.Errorf("%w: empty amount", ErrInvalidAmount)
	}

	for _, r := range whole + frac {
		if !unicode.IsDigit(r) {
			return "", fmt.Errorf("%w: contains non-digit character: %s", ErrInvalidAmount, amount)
		}
	}
	if len(frac) > decimals {
		return "", fmt.Errorf("%w: fractional precision exceeds %d decimals: %s", ErrInvalidAmount, decimals, amount)
	}

	frac = frac + strings.Repeat("0", decimals-len(frac))
	minor := strings.TrimLeft(whole+frac, "0")
	if minor == "" {
		minor = "0"
	}
	return minor, nil
}
