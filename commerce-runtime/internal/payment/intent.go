// Package payment contains runtime-owned payment intent facts. A PaymentIntent
// is permission to attempt an economic action, not a payment-service record.
package payment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type IntentStatus string

const (
	IntentCreated    IntentStatus = "CREATED"
	IntentAuthorized IntentStatus = "AUTHORIZED"
	IntentSubmitting IntentStatus = "SUBMITTING"
	IntentPending    IntentStatus = "PENDING"
	IntentConfirmed  IntentStatus = "CONFIRMED"
	IntentFailed     IntentStatus = "FAILED"
	IntentUnknown    IntentStatus = "UNKNOWN"
	IntentExpired    IntentStatus = "EXPIRED"
)

var (
	ErrInvalidIntent       = errors.New("invalid payment intent")
	ErrIntentConflict      = errors.New("payment intent conflicts with an existing economic identity")
	ErrIntentStateConflict = errors.New("payment intent state changed concurrently")
	ErrIntentExpired       = errors.New("payment intent has expired")
)

type PaymentIntent struct {
	IntentID           string       `json:"intent_id"`
	EpisodeID          string       `json:"episode_id"`
	MerchantDID        string       `json:"merchant_did"`
	CapabilityID       string       `json:"capability_id"`
	PayeeDID           string       `json:"payee_did"`
	QuoteHash          string       `json:"quote_hash"`
	AmountMinor        int64        `json:"amount_minor"`
	Currency           string       `json:"currency"`
	RequesterDID       string       `json:"requester_did"`
	EpisodeVersion     uint64       `json:"episode_version"`
	BudgetReservation  int64        `json:"budget_reservation"`
	IdempotencyKey     string       `json:"idempotency_key"`
	EconomicKey        string       `json:"economic_key"`
	ExpiresAt          time.Time    `json:"expires_at"`
	Status             IntentStatus `json:"status"`
	TxID               string       `json:"tx_id,omitempty"`
	TxHash             string       `json:"tx_hash,omitempty"`
	AuthorizationRef   string       `json:"authorization_ref,omitempty"`
	CredentialRef      string       `json:"credential_ref,omitempty"`
	RequestFingerprint string       `json:"request_fingerprint"`
	FailureCode        string       `json:"failure_code,omitempty"`
	CreatedAt          time.Time    `json:"created_at"`
	UpdatedAt          time.Time    `json:"updated_at"`
}

type economicIdentity struct {
	EpisodeID    string `json:"episode_id"`
	MerchantDID  string `json:"merchant_did"`
	CapabilityID string `json:"capability_id"`
	PayeeDID     string `json:"payee_did"`
	QuoteHash    string `json:"quote_hash"`
	AmountMinor  int64  `json:"amount_minor"`
	Currency     string `json:"currency"`
}

func EconomicIdentityKey(episodeID, merchantDID, capabilityID, payeeDID, quoteHash string, amountMinor int64, currency string) string {
	identity := economicIdentity{EpisodeID: episodeID, MerchantDID: merchantDID, CapabilityID: capabilityID, PayeeDID: payeeDID, QuoteHash: quoteHash, AmountMinor: amountMinor, Currency: strings.ToUpper(strings.TrimSpace(currency))}
	encoded, _ := json.Marshal(identity)
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func (i PaymentIntent) Validate() error {
	if strings.TrimSpace(i.IntentID) == "" || strings.TrimSpace(i.EpisodeID) == "" || strings.TrimSpace(i.MerchantDID) == "" || strings.TrimSpace(i.CapabilityID) == "" || strings.TrimSpace(i.PayeeDID) == "" || strings.TrimSpace(i.QuoteHash) == "" || strings.TrimSpace(i.RequesterDID) == "" {
		return ErrInvalidIntent
	}
	if i.AmountMinor <= 0 || i.BudgetReservation != i.AmountMinor || strings.TrimSpace(i.Currency) == "" || i.EpisodeVersion == 0 {
		return ErrInvalidIntent
	}
	if strings.TrimSpace(i.IdempotencyKey) == "" || strings.TrimSpace(i.EconomicKey) == "" || strings.TrimSpace(i.CredentialRef) == "" || strings.TrimSpace(i.RequestFingerprint) == "" || i.ExpiresAt.IsZero() || i.CreatedAt.IsZero() || i.UpdatedAt.IsZero() || i.UpdatedAt.Before(i.CreatedAt) || !i.ExpiresAt.After(i.CreatedAt) {
		return ErrInvalidIntent
	}
	if i.EconomicKey != EconomicIdentityKey(i.EpisodeID, i.MerchantDID, i.CapabilityID, i.PayeeDID, i.QuoteHash, i.AmountMinor, i.Currency) {
		return ErrInvalidIntent
	}
	if i.RequestFingerprint != RequestFingerprint(i) {
		return ErrInvalidIntent
	}
	if !KnownStatus(i.Status) {
		return ErrInvalidIntent
	}
	return nil
}

// RequestFingerprint is the stable command identity sent to the payment plane.
// CredentialRef is a durable opaque reference, never the credential secret. A
// retry must resolve the same reference and therefore produce the same signed
// command bytes for the payment-service idempotency hash.
func RequestFingerprint(i PaymentIntent) string {
	identity := struct {
		IntentID       string `json:"intent_id"`
		EpisodeID      string `json:"episode_id"`
		MerchantDID    string `json:"merchant_did"`
		CapabilityID   string `json:"capability_id"`
		PayeeDID       string `json:"payee_did"`
		QuoteHash      string `json:"quote_hash"`
		AmountMinor    int64  `json:"amount_minor"`
		Currency       string `json:"currency"`
		RequesterDID   string `json:"requester_did"`
		IdempotencyKey string `json:"idempotency_key"`
		CredentialRef  string `json:"credential_ref"`
	}{i.IntentID, i.EpisodeID, i.MerchantDID, i.CapabilityID, i.PayeeDID, i.QuoteHash, i.AmountMinor, strings.ToUpper(strings.TrimSpace(i.Currency)), i.RequesterDID, i.IdempotencyKey, i.CredentialRef}
	encoded, _ := json.Marshal(identity)
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func KnownStatus(value IntentStatus) bool {
	switch value {
	case IntentCreated, IntentAuthorized, IntentSubmitting, IntentPending, IntentConfirmed, IntentFailed, IntentUnknown, IntentExpired:
		return true
	default:
		return false
	}
}

func (i PaymentIntent) IsTerminal() bool {
	return i.Status == IntentConfirmed || i.Status == IntentFailed || i.Status == IntentExpired
}

func (i PaymentIntent) CanSubmitAt(now time.Time) error {
	if err := i.Validate(); err != nil {
		return err
	}
	if !now.Before(i.ExpiresAt) {
		return ErrIntentExpired
	}
	if i.Status != IntentCreated && i.Status != IntentAuthorized {
		return ErrIntentStateConflict
	}
	return nil
}
