// Package eval contains the external-client benchmark harness. It imports
// contracts and redacted observation types only; it never imports application
// Service or Runtime internals.
package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/stablepay/commerce-runtime/internal/observability"
)

const (
	ModeOffline = "offline"
	ModeReplay  = "replay"
	ModeLive    = "live"

	IngressHTTP = "http"
	IngressMCP  = "mcp"
	IngressCLI  = "cli"
)

type Scenario struct {
	CaseID                     string                         `json:"case_id"`
	Seed                       int64                          `json:"seed"`
	Ingress                    string                         `json:"ingress"`
	MemoryMode                 string                         `json:"memory_mode"`
	RecoveryProvider           string                         `json:"recovery_provider"`
	Path                       string                         `json:"path"`
	Suite                      string                         `json:"suite,omitempty"`
	Environment                string                         `json:"environment,omitempty"`
	TaskID                     string                         `json:"task_id,omitempty"`
	TrialIndex                 int                            `json:"trial_index,omitempty"`
	ExpectedTerminal           string                         `json:"expected_terminal,omitempty"`
	ExpectedPaymentIntents     *int                           `json:"expected_payment_intents,omitempty"`
	ExpectedSettlementCount    *int                           `json:"expected_settlement_count,omitempty"`
	ExpectedEntitlementTxID    string                         `json:"expected_entitlement_tx_id,omitempty"`
	RequireRecovery            bool                           `json:"require_recovery,omitempty"`
	MustNotCreateSecondPayment bool                           `json:"must_not_create_second_payment,omitempty"`
	ExpectedVariant            observability.RuntimeVariant   `json:"expected_variant,omitempty"`
	Failure                    observability.FailureInjection `json:"failure"`
	Request                    json.RawMessage                `json:"request,omitempty"`
	AutoApprove                bool                           `json:"auto_approve"`
	ReplayResult               *observability.EpisodeResult   `json:"replay_result,omitempty"`
}

func (s Scenario) Validate() error {
	if strings.TrimSpace(s.CaseID) == "" {
		return errors.New("eval case_id is required")
	}
	if s.Seed == 0 {
		return errors.New("eval seed must be non-zero")
	}
	if s.Ingress == "" {
		s.Ingress = IngressHTTP
	}
	if s.Ingress != IngressHTTP && s.Ingress != IngressMCP && s.Ingress != IngressCLI {
		return fmt.Errorf("unsupported eval ingress %q", s.Ingress)
	}
	if s.Failure.RatePercent < 0 || s.Failure.RatePercent > 100 || s.Failure.Repeat < 0 {
		return errors.New("failure rate must be 0..100 and repeat must be non-negative")
	}
	if s.ReplayResult == nil && len(s.Request) == 0 {
		return errors.New("live scenario requires request; replay scenario requires replay_result")
	}
	return nil
}

type FailurePlan struct {
	Kind             string `json:"kind"`
	Component        string `json:"component"`
	Trigger          string `json:"trigger"`
	Repeat           int    `json:"repeat"`
	ExpectedRecovery string `json:"expected_recovery"`
	LiveSupported    bool   `json:"live_supported"`
}

// RequiredFailurePlans is the auditable S11 fault matrix. Payment scenarios
// are replay/controller scenarios unless a Kitex-aware external injector is
// supplied; the harness never labels an unapplied plan as a live fault.
func RequiredFailurePlans() []FailurePlan {
	return []FailurePlan{
		{Kind: "merchant_transient", Component: "merchant", Trigger: "first N HTTP invocations", Repeat: 2, ExpectedRecovery: "retry or switch then terminal", LiveSupported: true},
		{Kind: "merchant_permanent", Component: "merchant", Trigger: "all HTTP invocations", Repeat: 0, ExpectedRecovery: "bounded failure or parent", LiveSupported: true},
		{Kind: "payment_transient", Component: "payment", Trigger: "first N submit/query calls", Repeat: 2, ExpectedRecovery: "reconcile and settle once", LiveSupported: false},
		{Kind: "payment_permanent", Component: "payment", Trigger: "all submit calls", Repeat: 0, ExpectedRecovery: "bounded failure without settlement", LiveSupported: false},
		{Kind: "delivery_invalid", Component: "merchant", Trigger: "delivery response violates validator", Repeat: 1, ExpectedRecovery: "delivery retry or switch", LiveSupported: true},
		{Kind: "verification_mismatch", Component: "verification", Trigger: "proof tx_id differs from PaymentIntent", Repeat: 1, ExpectedRecovery: "unknown/invalid entitlement recovery", LiveSupported: true},
		{Kind: "llm_malformed", Component: "llm", Trigger: "HTTP 200 with malformed decision JSON", Repeat: 1, ExpectedRecovery: "fallback or bounded failure", LiveSupported: true},
		{Kind: "llm_timeout", Component: "llm", Trigger: "request exceeds provider timeout", Repeat: 1, ExpectedRecovery: "fallback or bounded failure", LiveSupported: true},
		{Kind: "crash_restart", Component: "runtime", Trigger: "external supervisor termination after persisted transition", Repeat: 1, ExpectedRecovery: "resume from authoritative state", LiveSupported: false},
	}
}

// InjectAt applies a stable hash sampler to a case index. It is used by a
// controller or replay generator, never as a source of fake success metrics.
func InjectAt(seed int64, caseID, failureKind string, eligibleIndex, ratePercent int) bool {
	if ratePercent <= 0 {
		return false
	}
	if ratePercent >= 100 {
		return true
	}
	key := fmt.Sprintf("%d\x00%s\x00%s\x00%d", seed, caseID, failureKind, eligibleIndex)
	digest := sha256.Sum256([]byte(key))
	value, _ := hex.DecodeString(hex.EncodeToString(digest[:2]))
	if len(value) != 2 {
		return false
	}
	return int(value[0])<<8|int(value[1]) < ratePercent*65536/100
}
