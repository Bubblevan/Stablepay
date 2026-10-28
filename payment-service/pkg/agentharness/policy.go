// Package agentharness contains the deterministic policy shared by the existing
// USDC/USDT AgentPaymentHarness and the CNY payment boundary. Amounts have already
// been converted to the currency's minor units by the caller; this package never
// converts between token precision and CNY fen, verifies identity, or pays.
package agentharness

import "strings"

type Policy struct {
	Version             string
	AutoApproveMaxMinor int64
	MaxAmountMinor      int64
	AllowedMerchants    []string
}

type Decision struct {
	Status        string   `json:"status"`
	Decision      string   `json:"decision"`
	ReasonCodes   []string `json:"reason_codes"`
	PolicyVersion string   `json:"policy_version"`
}

func (p Policy) Evaluate(merchant string, amountMinor int64) Decision {
	result := Decision{PolicyVersion: p.Version}
	deny := func(reason string) Decision {
		result.Status, result.Decision, result.ReasonCodes = "REJECTED", "DENY", []string{reason}
		return result
	}
	if strings.TrimSpace(merchant) == "" || amountMinor <= 0 {
		return deny("INVALID_PAYMENT_INTENT")
	}
	if p.MaxAmountMinor > 0 && amountMinor > p.MaxAmountMinor {
		return deny("AMOUNT_EXCEEDS_AGENT_POLICY")
	}
	if len(p.AllowedMerchants) > 0 {
		allowed, configured := false, false
		for _, candidate := range p.AllowedMerchants {
			candidate = strings.TrimSpace(candidate)
			if candidate != "" {
				configured = true
				allowed = allowed || merchant == candidate
			}
		}
		if configured && !allowed {
			return deny("MERCHANT_NOT_ALLOWLISTED")
		}
	}
	if p.AutoApproveMaxMinor > 0 && amountMinor <= p.AutoApproveMaxMinor {
		result.Status, result.Decision, result.ReasonCodes = "APPROVED", "ALLOW", []string{"WITHIN_AUTO_APPROVAL_LIMIT"}
	} else {
		result.Status, result.Decision, result.ReasonCodes = "PENDING_USER_CONFIRMATION", "REQUIRE_USER_CONFIRMATION", []string{"ABOVE_AUTO_APPROVAL_LIMIT"}
	}
	return result
}
