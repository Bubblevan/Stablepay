package payment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// PaymentTransportTrace records operation identity and timing only. It never
// stores signatures, signed transactions, credentials, or provider payloads.
type PaymentTransportTrace struct {
	TraceID             string    `json:"trace_id"`
	EpisodeID           string    `json:"episode_id"`
	PaymentIntentID     string    `json:"payment_intent_id"`
	Kind                string    `json:"kind"`
	IdempotencyKey      string    `json:"idempotency_key"`
	RequestFingerprint  string    `json:"request_fingerprint"`
	StartedAt           time.Time `json:"started_at"`
	FinishedAt          time.Time `json:"finished_at"`
	ResultStatus        string    `json:"result_status"`
	PayloadHash         string    `json:"payload_hash"`
}

var ErrInvalidPaymentTransportTrace = errors.New("invalid payment transport trace")

func (t PaymentTransportTrace) CanonicalSnapshot() ([]byte, error) {
	copy := t
	copy.PayloadHash = ""
	return json.Marshal(copy)
}

func (t PaymentTransportTrace) PayloadHashFor() (string, error) {
	snapshot, err := t.CanonicalSnapshot()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(snapshot)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func (t *PaymentTransportTrace) RefreshPayloadHash() error {
	if t == nil {
		return ErrInvalidPaymentTransportTrace
	}
	hash, err := t.PayloadHashFor()
	if err != nil {
		return err
	}
	t.PayloadHash = hash
	return nil
}

func (t PaymentTransportTrace) Validate() error {
	if strings.TrimSpace(t.TraceID) == "" || strings.TrimSpace(t.EpisodeID) == "" || strings.TrimSpace(t.PaymentIntentID) == "" || strings.TrimSpace(t.IdempotencyKey) == "" || strings.TrimSpace(t.RequestFingerprint) == "" || t.StartedAt.IsZero() || t.FinishedAt.IsZero() || t.FinishedAt.Before(t.StartedAt) || strings.TrimSpace(t.ResultStatus) == "" || strings.TrimSpace(t.PayloadHash) == "" {
		return ErrInvalidPaymentTransportTrace
	}
	switch t.Kind {
	case "INITIAL_SUBMIT", "EXACT_REDELIVERY", "STATUS_QUERY":
	default:
		return ErrInvalidPaymentTransportTrace
	}
	expected, err := t.PayloadHashFor()
	if err != nil || expected != t.PayloadHash {
		return ErrInvalidPaymentTransportTrace
	}
	return nil
}

func (t PaymentTransportTrace) Clone() *PaymentTransportTrace {
	copy := t
	return &copy
}
