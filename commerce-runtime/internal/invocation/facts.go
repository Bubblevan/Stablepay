// Package invocation contains the persisted, redacted facts produced by the
// merchant invocation and delivery-validation loop.
package invocation

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

const MaxStoredPayloadBytes = 1 << 20

var (
	ErrInvalidFact        = errors.New("invalid commerce invocation fact")
	ErrFactConflict       = errors.New("commerce invocation fact conflicts with an existing identity")
	ErrInvocationInFlight = errors.New("merchant invocation is already in flight")
)

type Phase string

const (
	PhaseInitial  Phase = "INITIAL"
	PhaseDelivery Phase = "DELIVERY"
)

// MerchantInvocation is an immutable operation identity plus the redacted
// result of one HTTP attempt. Secrets and complete credentials are never part
// of this fact.
type MerchantInvocation struct {
	InvocationID        string            `json:"invocation_id"`
	EpisodeID           string            `json:"episode_id"`
	MerchantDID         string            `json:"merchant_did"`
	CapabilityID        string            `json:"capability_id"`
	CatalogVersion      string            `json:"catalog_version"`
	CatalogSnapshotHash string            `json:"catalog_snapshot_hash"`
	Phase               Phase             `json:"phase"`
	Attempt             int               `json:"attempt"`
	RequestHash         string            `json:"request_hash"`
	ResponseStatus      int               `json:"response_status"`
	ResponseContentType string            `json:"response_content_type,omitempty"`
	ResponsePayloadHash string            `json:"response_payload_hash,omitempty"`
	ResponseRef         string            `json:"response_ref,omitempty"`
	SelectedHeaders     map[string]string `json:"selected_headers,omitempty"`
	ResponseBody        []byte            `json:"response_body,omitempty"`
	PaymentRequiredRef  string            `json:"payment_required_ref,omitempty"`
	EntitlementRef      string            `json:"entitlement_ref,omitempty"`
	StartedAt           time.Time         `json:"started_at"`
	CompletedAt         time.Time         `json:"completed_at"`
	TraceID             string            `json:"trace_id,omitempty"`
	IdempotencyKey      string            `json:"idempotency_key"`
}

func (i MerchantInvocation) Validate() error {
	if strings.TrimSpace(i.InvocationID) == "" || strings.TrimSpace(i.EpisodeID) == "" ||
		strings.TrimSpace(i.MerchantDID) == "" || strings.TrimSpace(i.CapabilityID) == "" ||
		strings.TrimSpace(i.CatalogVersion) == "" || strings.TrimSpace(i.CatalogSnapshotHash) == "" ||
		strings.TrimSpace(i.RequestHash) == "" || strings.TrimSpace(i.IdempotencyKey) == "" ||
		i.Attempt <= 0 || i.StartedAt.IsZero() || (!i.CompletedAt.IsZero() && i.CompletedAt.Before(i.StartedAt)) {
		return ErrInvalidFact
	}
	if i.Phase != PhaseInitial && i.Phase != PhaseDelivery {
		return ErrInvalidFact
	}
	if i.ResponseStatus < 0 || i.ResponseStatus > 599 || (i.ResponseStatus != 0 && i.ResponseStatus < 100) || len(i.ResponseBody) > MaxStoredPayloadBytes {
		return ErrInvalidFact
	}
	if i.ResponseStatus > 0 && strings.TrimSpace(i.ResponsePayloadHash) == "" {
		return ErrInvalidFact
	}
	return nil
}

func (i MerchantInvocation) Clone() *MerchantInvocation {
	copy := i
	copy.ResponseBody = append([]byte(nil), i.ResponseBody...)
	if i.SelectedHeaders != nil {
		copy.SelectedHeaders = make(map[string]string, len(i.SelectedHeaders))
		for key, value := range i.SelectedHeaders {
			copy.SelectedHeaders[key] = value
		}
	}
	return &copy
}

type PaymentRequirementFact struct {
	PaymentRequirementID string    `json:"payment_requirement_id"`
	EpisodeID            string    `json:"episode_id"`
	InvocationID         string    `json:"invocation_id"`
	MerchantDID          string    `json:"merchant_did"`
	CapabilityID         string    `json:"capability_id"`
	CatalogVersion       string    `json:"catalog_version"`
	CatalogSnapshotHash  string    `json:"catalog_snapshot_hash"`
	ProtocolVersion      string    `json:"protocol_version"`
	Scheme               string    `json:"scheme"`
	Network              string    `json:"network"`
	Asset                string    `json:"asset"`
	AtomicAmount         int64     `json:"atomic_amount"`
	AtomicDecimals       int       `json:"atomic_decimals"`
	BusinessAmountMinor  int64     `json:"business_amount_minor"`
	Currency             string    `json:"currency"`
	PayTo                string    `json:"pay_to"`
	PayeeDID             string    `json:"payee_did"`
	ResourceURL          string    `json:"resource_url"`
	ProductID            string    `json:"product_id,omitempty"`
	SkillDID             string    `json:"skill_did,omitempty"`
	MaxTimeoutSeconds    int       `json:"max_timeout_seconds"`
	ObservedAt           time.Time `json:"observed_at"`
	ExpiresAt            time.Time `json:"expires_at"`
	RawPayloadHash       string    `json:"raw_payload_hash"`
	CanonicalQuoteHash   string    `json:"canonical_quote_hash"`
	FactsRef             string    `json:"facts_ref"`
}

func (p PaymentRequirementFact) Validate() error {
	if strings.TrimSpace(p.PaymentRequirementID) == "" || strings.TrimSpace(p.EpisodeID) == "" || strings.TrimSpace(p.InvocationID) == "" ||
		strings.TrimSpace(p.MerchantDID) == "" || strings.TrimSpace(p.CapabilityID) == "" || strings.TrimSpace(p.CatalogVersion) == "" ||
		strings.TrimSpace(p.CatalogSnapshotHash) == "" || strings.TrimSpace(p.ProtocolVersion) == "" || strings.TrimSpace(p.Scheme) == "" ||
		strings.TrimSpace(p.Network) == "" || strings.TrimSpace(p.Asset) == "" || p.AtomicAmount <= 0 || p.AtomicDecimals <= 0 || p.BusinessAmountMinor <= 0 || strings.TrimSpace(p.Currency) == "" ||
		strings.TrimSpace(p.PayTo) == "" || strings.TrimSpace(p.PayeeDID) == "" || strings.TrimSpace(p.ResourceURL) == "" ||
		p.MaxTimeoutSeconds <= 0 || p.ObservedAt.IsZero() || !p.ExpiresAt.After(p.ObservedAt) || strings.TrimSpace(p.RawPayloadHash) == "" ||
		strings.TrimSpace(p.CanonicalQuoteHash) == "" || strings.TrimSpace(p.FactsRef) == "" {
		return ErrInvalidFact
	}
	return nil
}

func (p PaymentRequirementFact) Clone() *PaymentRequirementFact { copy := p; return &copy }

type DeliveryArtifact struct {
	DeliveryID      string    `json:"delivery_id"`
	EpisodeID       string    `json:"episode_id"`
	InvocationID    string    `json:"invocation_id"`
	MerchantDID     string    `json:"merchant_did"`
	CapabilityID    string    `json:"capability_id"`
	ContentType     string    `json:"content_type"`
	PayloadRef      string    `json:"payload_ref"`
	PayloadHash     string    `json:"payload_hash"`
	Body            []byte    `json:"body,omitempty"`
	PaymentIntentID string    `json:"payment_intent_id,omitempty"`
	EntitlementRef  string    `json:"entitlement_ref,omitempty"`
	Attempt         int       `json:"attempt"`
	HTTPStatus      int       `json:"http_status"`
	ReceivedAt      time.Time `json:"received_at"`
}

func (d DeliveryArtifact) Validate() error {
	if strings.TrimSpace(d.DeliveryID) == "" || strings.TrimSpace(d.EpisodeID) == "" || strings.TrimSpace(d.InvocationID) == "" ||
		strings.TrimSpace(d.MerchantDID) == "" || strings.TrimSpace(d.CapabilityID) == "" || strings.TrimSpace(d.PayloadRef) == "" ||
		strings.TrimSpace(d.PayloadHash) == "" || d.Attempt <= 0 || d.HTTPStatus < 100 || d.HTTPStatus > 599 || d.ReceivedAt.IsZero() ||
		len(d.Body) > MaxStoredPayloadBytes {
		return ErrInvalidFact
	}
	return nil
}

func (d DeliveryArtifact) Clone() *DeliveryArtifact {
	copy := d
	copy.Body = append([]byte(nil), d.Body...)
	return &copy
}

type ValidationEvidence struct {
	ValidationID     string    `json:"validation_id"`
	EpisodeID        string    `json:"episode_id"`
	DeliveryID       string    `json:"delivery_id"`
	ValidatorName    string    `json:"validator_name"`
	ValidatorVersion string    `json:"validator_version"`
	Valid            bool      `json:"valid"`
	ReasonCode       string    `json:"reason_code"`
	EvidenceRefs     []string  `json:"evidence_refs,omitempty"`
	PayloadHash      string    `json:"payload_hash"`
	CreatedAt        time.Time `json:"created_at"`
}

func (v ValidationEvidence) Validate() error {
	if strings.TrimSpace(v.ValidationID) == "" || strings.TrimSpace(v.EpisodeID) == "" || strings.TrimSpace(v.DeliveryID) == "" ||
		strings.TrimSpace(v.ValidatorName) == "" || strings.TrimSpace(v.ValidatorVersion) == "" || strings.TrimSpace(v.ReasonCode) == "" ||
		strings.TrimSpace(v.PayloadHash) == "" || v.CreatedAt.IsZero() {
		return ErrInvalidFact
	}
	return nil
}

func (v ValidationEvidence) Clone() *ValidationEvidence {
	copy := v
	copy.EvidenceRefs = append([]string(nil), v.EvidenceRefs...)
	return &copy
}

func PayloadHash(body []byte) string {
	digest := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(digest[:])
}
