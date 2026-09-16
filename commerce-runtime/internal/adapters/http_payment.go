package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/stablepay/commerce-runtime/internal/payment"
)

var ErrPaymentAdapterResponse = errors.New("invalid payment adapter response")

type PaymentCredentials struct {
	// Reference is an opaque durable reference. Secrets and private keys never
	// enter the EpisodeEvent, LedgerEntry or PaymentIntent payload.
	Reference      string
	Signature      string
	Timestamp      string
	Nonce          string
	SignedTxBase64 string
}

type CredentialProvider interface {
	Credentials(context.Context, payment.PaymentIntent) (PaymentCredentials, error)
}

type CredentialFunc func(context.Context, payment.PaymentIntent) (PaymentCredentials, error)

func (f CredentialFunc) Credentials(ctx context.Context, intent payment.PaymentIntent) (PaymentCredentials, error) {
	return f(ctx, intent)
}

// HTTPGatewayPaymentAdapter is retained for explicit gateway integrations.
// Production runtime wiring uses KitexPaymentAdapter for payment-service RPC.
type HTTPGatewayPaymentAdapter struct {
	BaseURL     string
	Client      *http.Client
	Credentials CredentialProvider
}

func (a *HTTPGatewayPaymentAdapter) Submit(ctx context.Context, request PaymentSubmitRequest) (payment.PaymentOutcome, error) {
	if err := request.Intent.Validate(); err != nil {
		return unknownOutcome(request.Intent, err), err
	}
	if err := request.Authorization.Validate(); err != nil {
		return unknownOutcome(request.Intent, err), err
	}
	if err := a.validateBinding(request.Intent, request.Authorization); err != nil {
		return unknownOutcome(request.Intent, err), err
	}
	if request.PayeeDID != request.Intent.PayeeDID {
		err := errors.New("payment request payee does not match intent snapshot")
		return unknownOutcome(request.Intent, err), err
	}
	if request.RequestFingerprint == "" || request.RequestFingerprint != request.Intent.RequestFingerprint {
		err := errors.New("payment request fingerprint does not match intent")
		return unknownOutcome(request.Intent, err), err
	}
	if a == nil || strings.TrimSpace(a.BaseURL) == "" || a.Credentials == nil {
		err := errors.New("payment adapter base URL and credentials are required")
		return unknownOutcome(request.Intent, err), err
	}
	credentials, err := a.Credentials.Credentials(ctx, request.Intent)
	if err != nil {
		return unknownOutcome(request.Intent, err), err
	}
	if request.Intent.CredentialRef != "" && credentials.Reference != request.Intent.CredentialRef {
		err := errors.New("credential reference changed for payment retry")
		return unknownOutcome(request.Intent, err), err
	}
	body := struct {
		AgentDID       string `json:"agent_did"`
		SkillDID       string `json:"skill_did"`
		Amount         string `json:"amount"`
		Currency       string `json:"currency"`
		Signature      string `json:"signature"`
		Timestamp      string `json:"timestamp"`
		Nonce          string `json:"nonce"`
		SignedTxBase64 string `json:"signed_tx_base64,omitempty"`
		IntentID       string `json:"intent_id"`
	}{
		AgentDID: request.Intent.RequesterDID, SkillDID: request.Intent.PayeeDID,
		Amount: minorToMajor(request.Intent.AmountMinor), Currency: strings.ToUpper(request.Intent.Currency),
		Signature: credentials.Signature, Timestamp: credentials.Timestamp, Nonce: credentials.Nonce,
		SignedTxBase64: credentials.SignedTxBase64, IntentID: request.Intent.IntentID,
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return unknownOutcome(request.Intent, err), err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.BaseURL, "/")+"/api/v1/pay", bytes.NewReader(encoded))
	if err != nil {
		return unknownOutcome(request.Intent, err), err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("X-Idempotency-Key", request.Intent.IdempotencyKey)
	if request.TraceID != "" {
		httpRequest.Header.Set("X-Trace-ID", request.TraceID)
	}
	client := a.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return unknownOutcome(request.Intent, err), err
	}
	defer response.Body.Close()
	outcome, decodeErr := decodePaymentResponse(response.Body, request.Intent)
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		if decodeErr == nil && outcome.Status != payment.OutcomeUnknown {
			return outcome, fmt.Errorf("payment service returned HTTP %d", response.StatusCode)
		}
		return unknownOutcome(request.Intent, fmt.Errorf("payment service returned HTTP %d", response.StatusCode)), fmt.Errorf("payment service returned HTTP %d", response.StatusCode)
	}
	if decodeErr != nil {
		return unknownOutcome(request.Intent, decodeErr), decodeErr
	}
	return outcome, nil
}

func (a *HTTPGatewayPaymentAdapter) Query(ctx context.Context, request PaymentQuery) (payment.PaymentOutcome, error) {
	if err := request.Intent.Validate(); err != nil {
		return unknownOutcome(request.Intent, err), err
	}
	if request.PayeeDID != request.Intent.PayeeDID {
		err := errors.New("payment query payee does not match intent snapshot")
		return unknownOutcome(request.Intent, err), err
	}
	if request.Intent.TxID == "" {
		err := errors.New("payment status query requires a tx_id; no blind resubmit is allowed")
		return unknownOutcome(request.Intent, err), err
	}
	if a == nil || strings.TrimSpace(a.BaseURL) == "" {
		err := errors.New("payment adapter base URL is required")
		return unknownOutcome(request.Intent, err), err
	}
	path := strings.TrimRight(a.BaseURL, "/") + "/api/v1/pay/" + url.PathEscape(request.Intent.TxID)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return unknownOutcome(request.Intent, err), err
	}
	if request.TraceID != "" {
		httpRequest.Header.Set("X-Trace-ID", request.TraceID)
	}
	client := a.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return unknownOutcome(request.Intent, err), err
	}
	defer response.Body.Close()
	outcome, decodeErr := decodePaymentResponse(response.Body, request.Intent)
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return unknownOutcome(request.Intent, fmt.Errorf("payment service returned HTTP %d", response.StatusCode)), fmt.Errorf("payment service returned HTTP %d", response.StatusCode)
	}
	if decodeErr != nil {
		return unknownOutcome(request.Intent, decodeErr), decodeErr
	}
	return outcome, nil
}

func (a *HTTPGatewayPaymentAdapter) validateBinding(intent payment.PaymentIntent, authorization AuthorizationResult) error {
	if authorization.RequesterDID != intent.RequesterDID || authorization.MerchantDID != intent.MerchantDID || authorization.CapabilityID != intent.CapabilityID || authorization.PayeeDID != intent.PayeeDID || authorization.QuoteHash != intent.QuoteHash || authorization.AmountMinor != intent.AmountMinor || strings.ToUpper(authorization.Currency) != strings.ToUpper(intent.Currency) {
		return errors.New("payment authorization does not match intent snapshot")
	}
	return nil
}

type paymentResponse struct {
	TxID        string `json:"tx_id"`
	TxHash      string `json:"tx_hash"`
	Status      string `json:"status"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
	ConfirmedAt string `json:"confirmed_at"`
	FailedAt    string `json:"failed_at"`
	Reason      string `json:"reason"`
}

type paymentEnvelope struct {
	Data paymentResponse `json:"data"`
}

func decodePaymentResponse(reader io.Reader, intent payment.PaymentIntent) (payment.PaymentOutcome, error) {
	var raw struct {
		Data        *paymentResponse `json:"data"`
		TxID        string           `json:"tx_id"`
		TxHash      string           `json:"tx_hash"`
		Status      string           `json:"status"`
		AmountMinor int64            `json:"amount_minor"`
		Currency    string           `json:"currency"`
		ConfirmedAt string           `json:"confirmed_at"`
		FailedAt    string           `json:"failed_at"`
		Reason      string           `json:"reason"`
	}
	if err := json.NewDecoder(reader).Decode(&raw); err != nil {
		return unknownOutcome(intent, err), err
	}
	data := paymentResponse{TxID: raw.TxID, TxHash: raw.TxHash, Status: raw.Status, AmountMinor: raw.AmountMinor, Currency: raw.Currency, ConfirmedAt: raw.ConfirmedAt, FailedAt: raw.FailedAt, Reason: raw.Reason}
	if raw.Data != nil {
		data = *raw.Data
	}
	status, err := mapStatus(data.Status)
	if err != nil {
		return unknownOutcome(intent, err), err
	}
	amount := data.AmountMinor
	if amount == 0 {
		amount = intent.AmountMinor
	}
	currency := data.Currency
	if currency == "" {
		currency = intent.Currency
	}
	return payment.PaymentOutcome{Status: status, IntentID: intent.IntentID, TxID: data.TxID, TxHash: data.TxHash, AmountMinor: amount, Currency: strings.ToUpper(currency), Reason: data.Reason, OccurredAt: data.ConfirmedAt}, nil
}

func mapStatus(value string) (payment.OutcomeStatus, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "PENDING", "CREATED":
		return payment.OutcomePending, nil
	case "CONFIRMED", "COMPLETED":
		return payment.OutcomeConfirmed, nil
	case "FAILED", "CANCELLED":
		return payment.OutcomeFailed, nil
	default:
		return payment.OutcomeUnknown, ErrPaymentAdapterResponse
	}
}

func minorToMajor(amount int64) string {
	return strconv.FormatInt(amount/100, 10) + "." + fmt.Sprintf("%02d", amount%100)
}

func unknownOutcome(intent payment.PaymentIntent, err error) payment.PaymentOutcome {
	return payment.PaymentOutcome{Status: payment.OutcomeUnknown, IntentID: intent.IntentID, TxID: intent.TxID, TxHash: intent.TxHash, AmountMinor: intent.AmountMinor, Currency: intent.Currency, Reason: err.Error()}
}
