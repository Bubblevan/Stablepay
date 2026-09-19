// Package trace defines the structured, redacted language shared by events and
// decision guards. It intentionally contains references and hashes, not secrets.
package trace

import (
	"errors"
	"fmt"
	"strings"
)

type ActionType string

const (
	ActionDiscover                    ActionType = "DISCOVER"
	ActionInvoke                      ActionType = "INVOKE"
	ActionParse402                    ActionType = "PARSE_402"
	ActionReserveBudget               ActionType = "RESERVE_BUDGET"
	ActionCreatePayment               ActionType = "CREATE_PAYMENT"
	ActionVerifyEntitlement           ActionType = "VERIFY_ENTITLEMENT"
	ActionValidateDelivery            ActionType = "VALIDATE_DELIVERY"
	ActionRetrySameMerchant           ActionType = "RETRY_SAME_MERCHANT"
	ActionStop                        ActionType = "STOP"
	ActionSelectMerchant              ActionType = "SELECT_MERCHANT"
	ActionNegotiateAndPay             ActionType = "NEGOTIATE_AND_PAY"
	ActionPaymentSubmitted            ActionType = "PAYMENT_SUBMITTED"
	ActionPaymentPending              ActionType = "PAYMENT_PENDING"
	ActionPaymentStatusQueried        ActionType = "PAYMENT_STATUS_QUERIED"
	ActionPaymentAuthorizationChecked ActionType = "PAYMENT_AUTHORIZATION_CHECKED"
	ActionPaymentConfirmed            ActionType = "PAYMENT_CONFIRMED"
	ActionPaymentFailed               ActionType = "PAYMENT_FAILED"
	ActionPaymentUnknown              ActionType = "PAYMENT_UNKNOWN"
)

type ObservationType string

const (
	ObservationCandidatesFound      ObservationType = "CANDIDATES_FOUND"
	ObservationNoEligibleCandidate  ObservationType = "NO_ELIGIBLE_CANDIDATE"
	ObservationHTTP402              ObservationType = "HTTP_402"
	ObservationMerchantResponse     ObservationType = "MERCHANT_RESPONSE"
	ObservationQuoteValid           ObservationType = "QUOTE_VALID"
	ObservationPolicyDenied         ObservationType = "POLICY_DENIED"
	ObservationPaymentSettled       ObservationType = "PAYMENT_SETTLED"
	ObservationEntitlementValid     ObservationType = "ENTITLEMENT_VALID"
	ObservationDeliveryValid        ObservationType = "DELIVERY_VALID"
	ObservationDeliveryInvalid      ObservationType = "DELIVERY_INVALID"
	ObservationToolError            ObservationType = "TOOL_ERROR"
	ObservationDeadlineExceeded     ObservationType = "DEADLINE_EXCEEDED"
	ObservationPaymentSubmitted     ObservationType = "PAYMENT_SUBMITTED"
	ObservationPaymentPending       ObservationType = "PAYMENT_PENDING"
	ObservationPaymentStatusQueried ObservationType = "PAYMENT_STATUS_QUERIED"
	ObservationAuthorizationAllowed ObservationType = "AUTHORIZATION_ALLOWED"
	ObservationPaymentConfirmed     ObservationType = "PAYMENT_CONFIRMED"
	ObservationPaymentFailed        ObservationType = "PAYMENT_FAILED"
	ObservationPaymentUnknown       ObservationType = "PAYMENT_UNKNOWN"
	ObservationEntitlementInvalid   ObservationType = "ENTITLEMENT_INVALID"
	ObservationEntitlementUnknown   ObservationType = "ENTITLEMENT_UNKNOWN"
)

type Action struct {
	Type           ActionType `json:"type"`
	IdempotencyKey string     `json:"idempotency_key"`
	InputRef       string     `json:"input_ref,omitempty"`
	InputHash      string     `json:"input_hash,omitempty"`
}

type Observation struct {
	Type        ObservationType `json:"type"`
	Code        string          `json:"code,omitempty"`
	FactsRef    string          `json:"facts_ref,omitempty"`
	PayloadHash string          `json:"payload_hash,omitempty"`
}

type Decision struct {
	ProposedAction ActionType `json:"proposed_action"`
	ProposalID     string     `json:"proposal_id"`
	Reason         string     `json:"reason,omitempty"`
	CandidateSetID string     `json:"candidate_set_id,omitempty"`
	Target         *Target    `json:"target,omitempty"`
	EvidenceRefs   []string   `json:"evidence_refs,omitempty"`
}

type Target struct {
	MerchantDID         string `json:"merchant_did,omitempty"`
	CapabilityID        string `json:"capability_id,omitempty"`
	CatalogVersion      string `json:"catalog_version,omitempty"`
	CatalogSnapshotHash string `json:"catalog_snapshot_hash,omitempty"`
	CatalogSnapshotRef  string `json:"catalog_snapshot_ref,omitempty"`
}

type RuntimeCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

type RuntimeVerdict struct {
	Allowed bool           `json:"allowed"`
	Checks  []RuntimeCheck `json:"checks,omitempty"`
	Reason  string         `json:"reason,omitempty"`
}

func (a Action) Validate() error {
	if strings.TrimSpace(string(a.Type)) == "" {
		return errors.New("action type is required")
	}
	if strings.TrimSpace(a.IdempotencyKey) == "" {
		return errors.New("action idempotency key is required")
	}
	return nil
}

func (o Observation) Validate() error {
	if strings.TrimSpace(string(o.Type)) == "" {
		return errors.New("observation type is required")
	}
	return nil
}

func (v RuntimeVerdict) Validate() error {
	if !v.Allowed && strings.TrimSpace(v.Reason) == "" {
		return errors.New("rejected runtime verdict needs a reason")
	}
	return nil
}

func (a ActionType) String() string { return string(a) }

func KnownAction(action ActionType) bool {
	switch action {
	case ActionDiscover, ActionInvoke, ActionParse402, ActionReserveBudget, ActionCreatePayment,
		ActionVerifyEntitlement, ActionValidateDelivery, ActionRetrySameMerchant, ActionStop,
		ActionSelectMerchant, ActionNegotiateAndPay, ActionPaymentSubmitted, ActionPaymentPending,
		ActionPaymentStatusQueried, ActionPaymentAuthorizationChecked, ActionPaymentConfirmed,
		ActionPaymentFailed, ActionPaymentUnknown:
		return true
	default:
		return false
	}
}

func (o ObservationType) String() string { return string(o) }

func Check(name string, passed bool, detail string) RuntimeCheck {
	return RuntimeCheck{Name: name, Passed: passed, Detail: detail}
}

func InvalidObservation(message string) error {
	return fmt.Errorf("invalid observation: %s", message)
}
