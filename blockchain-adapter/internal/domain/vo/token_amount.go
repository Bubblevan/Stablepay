package vo

import (
	"fmt"
	"strings"
)

// BusinessAmountDecimals is the precision used by StablePay's business money
// contract for USDC and USDT.
const BusinessAmountDecimals = 2

// TokenAmountDecimals is the precision used by the Solana SPL token mints.
const TokenAmountDecimals = 6

const businessToTokenMultiplier uint64 = 10000

// BusinessMinorToTokenRaw converts a business amount_minor into the raw SPL
// token units required by a Solana transfer instruction. The conversion stays
// at the blockchain boundary so payment signatures, records, and events keep
// the business amount (for example, 0.01 USDC == 1 business minor unit).
func BusinessMinorToTokenRaw(currency string, amountMinor int64) (uint64, error) {
	if amountMinor <= 0 {
		return 0, fmt.Errorf("amount_minor must be greater than 0")
	}

	switch strings.ToUpper(strings.TrimSpace(currency)) {
	case "USDC", "USDT":
	default:
		return 0, fmt.Errorf("unsupported currency: %s", currency)
	}

	amount := uint64(amountMinor)
	if amount > ^uint64(0)/businessToTokenMultiplier {
		return 0, fmt.Errorf("amount_minor overflows token raw units: %d", amountMinor)
	}
	return amount * businessToTokenMultiplier, nil
}
