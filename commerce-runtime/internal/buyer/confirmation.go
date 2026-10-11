// Package buyer contains durable, transaction-specific buyer authorization
// facts. It is deliberately distinct from Runtime Parent Approval, which is a
// risk/budget policy gate rather than acceptance of a particular purchase.
package buyer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var ErrInvalidConfirmation = errors.New("invalid buyer confirmation")

type Confirmation struct {
	ConfirmationID string    `json:"confirmation_id"`
	EpisodeID      string    `json:"episode_id"`
	RequestID      string    `json:"request_id"`
	RequesterDID   string    `json:"requester_did"`
	MerchantDID    string    `json:"merchant_did"`
	CapabilityID   string    `json:"capability_id"`
	ProductID      string    `json:"product_id"`
	OfferRef       string    `json:"offer_ref"`
	QuoteHash      string    `json:"quote_hash"`
	AmountMinor    int64     `json:"amount_minor"`
	Currency       string    `json:"currency"`
	Accepted       bool      `json:"accepted"`
	IdempotencyKey string    `json:"idempotency_key"`
	PayloadHash    string    `json:"payload_hash"`
	Signature      string    `json:"signature"`
	ExpiresAt      time.Time `json:"expires_at"`
	CreatedAt      time.Time `json:"created_at"`
}

func (c Confirmation) Validate() error {
	if strings.TrimSpace(c.ConfirmationID) == "" || strings.TrimSpace(c.EpisodeID) == "" ||
		strings.TrimSpace(c.RequestID) == "" || strings.TrimSpace(c.RequesterDID) == "" ||
		strings.TrimSpace(c.MerchantDID) == "" || strings.TrimSpace(c.CapabilityID) == "" ||
		strings.TrimSpace(c.ProductID) == "" || strings.TrimSpace(c.OfferRef) == "" || strings.TrimSpace(c.QuoteHash) == "" ||
		c.AmountMinor <= 0 || strings.TrimSpace(c.Currency) == "" ||
		strings.TrimSpace(c.IdempotencyKey) == "" || strings.TrimSpace(c.PayloadHash) == "" ||
		strings.TrimSpace(c.Signature) == "" || c.ExpiresAt.IsZero() || c.CreatedAt.IsZero() ||
		!c.ExpiresAt.After(c.CreatedAt) {
		return ErrInvalidConfirmation
	}
	return nil
}

// ApprovalMessage is the canonical, domain-separated payload wallets sign.
// Field order is fixed by the struct and amounts remain integer minor units.
func ApprovalMessage(c Confirmation) ([]byte, error) {
	payload := struct {
		Domain       string    `json:"domain"`
		EpisodeID    string    `json:"episode_id"`
		RequestID    string    `json:"request_id"`
		RequesterDID string    `json:"requester_did"`
		MerchantDID  string    `json:"merchant_did"`
		CapabilityID string    `json:"capability_id"`
		ProductID    string    `json:"product_id"`
		OfferRef     string    `json:"offer_ref"`
		QuoteHash    string    `json:"quote_hash"`
		AmountMinor  int64     `json:"amount_minor"`
		Currency     string    `json:"currency"`
		Accepted     bool      `json:"accepted"`
		ExpiresAt    time.Time `json:"expires_at"`
	}{"SP-COMMERCE-001/BUYER-CONFIRMATION/V1", c.EpisodeID, c.RequestID, c.RequesterDID,
		c.MerchantDID, c.CapabilityID, c.ProductID, c.OfferRef, c.QuoteHash, c.AmountMinor,
		strings.ToUpper(strings.TrimSpace(c.Currency)), c.Accepted, c.ExpiresAt.UTC()}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return append([]byte("StablePay Buyer Confirmation V1\n"), encoded...), nil
}

func HashPayload(message []byte) string {
	digest := sha256.Sum256(message)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func (c Confirmation) Clone() *Confirmation {
	copy := c
	return &copy
}

// SameIdempotentRequest compares the signed purchase intent rather than
// server-generated confirmation identity/timestamps. A retry may race before
// it can observe the first commit, so those server fields are expected to
// differ while the buyer-authorized payload remains identical.
func SameIdempotentRequest(left, right *Confirmation) bool {
	if left == nil || right == nil {
		return false
	}
	return left.EpisodeID == right.EpisodeID && left.RequestID == right.RequestID &&
		left.RequesterDID == right.RequesterDID && left.MerchantDID == right.MerchantDID &&
		left.CapabilityID == right.CapabilityID && left.ProductID == right.ProductID &&
		left.OfferRef == right.OfferRef && left.QuoteHash == right.QuoteHash &&
		left.AmountMinor == right.AmountMinor && strings.EqualFold(left.Currency, right.Currency) &&
		left.Accepted == right.Accepted && left.IdempotencyKey == right.IdempotencyKey &&
		left.PayloadHash == right.PayloadHash && left.ExpiresAt.UTC().Equal(right.ExpiresAt.UTC())
}
