package adapters

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/invocation"
)

var (
	ErrMerchantAdapterNotConfigured = errors.New("merchant adapter is not configured")
	ErrMerchantResponseTooLarge     = errors.New("merchant response exceeds the bounded payload limit")
	ErrMerchantEndpointUnavailable  = errors.New("merchant endpoint is not an HTTP URL")
)

const (
	DefaultMerchantTimeout   = 20 * time.Second
	DefaultMerchantBodyLimit = invocation.MaxStoredPayloadBytes
)

type MerchantInvokeRequest struct {
	EpisodeID           string
	RequesterDID        string
	MerchantDID         string
	CapabilityID        string
	CatalogVersion      string
	CatalogSnapshotHash string
	CatalogSnapshotRef  string
	InputRef            string
	InputHash           string
	EntitlementRef      string
	PaymentIntentID     string
	PaymentSignature    string
	Attempt             int
	Phase               invocation.Phase
	TraceID             string
	IdempotencyKey      string
	Endpoint            catalog.EndpointRef
}

type MerchantInvokeResult struct {
	HTTPStatus  int
	ContentType string
	Headers     map[string]string
	PayloadRef  string
	PayloadHash string
	Body        []byte
	OccurredAt  time.Time
}

func (r MerchantInvokeRequest) Validate() error {
	if strings.TrimSpace(r.EpisodeID) == "" || strings.TrimSpace(r.RequesterDID) == "" || strings.TrimSpace(r.MerchantDID) == "" ||
		strings.TrimSpace(r.CapabilityID) == "" || strings.TrimSpace(r.CatalogVersion) == "" || strings.TrimSpace(r.CatalogSnapshotHash) == "" || strings.TrimSpace(r.CatalogSnapshotRef) == "" ||
		strings.TrimSpace(r.RequesterDID) == "" || r.Attempt <= 0 || strings.TrimSpace(r.TraceID) == "" || strings.TrimSpace(r.IdempotencyKey) == "" ||
		(r.Phase != invocation.PhaseInitial && r.Phase != invocation.PhaseDelivery) {
		return errors.New("invalid merchant invoke request")
	}
	return nil
}

// RequestHash binds an invocation operation without persisting a payment
// signature or any other credential-bearing header.
func RequestHash(r MerchantInvokeRequest) (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	canonical := struct {
		EpisodeID           string              `json:"episode_id"`
		RequesterDID        string              `json:"requester_did"`
		MerchantDID         string              `json:"merchant_did"`
		CapabilityID        string              `json:"capability_id"`
		CatalogVersion      string              `json:"catalog_version"`
		CatalogSnapshotHash string              `json:"catalog_snapshot_hash"`
		CatalogSnapshotRef  string              `json:"catalog_snapshot_ref"`
		InputRef            string              `json:"input_ref,omitempty"`
		InputHash           string              `json:"input_hash,omitempty"`
		EntitlementRef      string              `json:"entitlement_ref,omitempty"`
		PaymentIntentID     string              `json:"payment_intent_id,omitempty"`
		Attempt             int                 `json:"attempt"`
		Phase               invocation.Phase    `json:"phase"`
		Endpoint            catalog.EndpointRef `json:"endpoint"`
		IdempotencyKey      string              `json:"idempotency_key"`
	}{r.EpisodeID, r.RequesterDID, r.MerchantDID, r.CapabilityID, r.CatalogVersion, r.CatalogSnapshotHash, r.CatalogSnapshotRef, r.InputRef, r.InputHash,
		r.EntitlementRef, r.PaymentIntentID, r.Attempt, r.Phase, r.Endpoint.Normalize(), r.IdempotencyKey}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

type MerchantAdapter interface {
	Invoke(context.Context, MerchantInvokeRequest) (MerchantInvokeResult, error)
}

// HTTPMerchantAdapter is the S4 boundary to the existing merchant HTTP
// contract. It does not import merchant application/domain packages.
type HTTPMerchantAdapter struct {
	Client    *http.Client
	Timeout   time.Duration
	BodyLimit int64
	UserAgent string
}

func NewHTTPMerchantAdapter(client *http.Client) *HTTPMerchantAdapter {
	if client == nil {
		client = &http.Client{}
	}
	return &HTTPMerchantAdapter{Client: client, Timeout: DefaultMerchantTimeout, BodyLimit: DefaultMerchantBodyLimit, UserAgent: "stablepay-commerce-runtime/1"}
}

func (a *HTTPMerchantAdapter) Invoke(ctx context.Context, request MerchantInvokeRequest) (MerchantInvokeResult, error) {
	if err := request.Validate(); err != nil {
		return MerchantInvokeResult{}, err
	}
	endpoint := request.Endpoint.Normalize()
	if endpoint.Endpoint == "" {
		return MerchantInvokeResult{}, ErrMerchantEndpointUnavailable
	}
	u, err := url.Parse(endpoint.Endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return MerchantInvokeResult{}, ErrMerchantEndpointUnavailable
	}
	query := u.Query()
	if query.Get("agent_did") == "" {
		query.Set("agent_did", request.RequesterDID)
	}
	u.RawQuery = query.Encode()
	method := endpoint.Method
	if method == "" {
		method = http.MethodGet
	}
	client := a.Client
	if client == nil {
		client = &http.Client{}
	}
	callCtx := ctx
	if callCtx == nil {
		callCtx = context.Background()
	}
	if a.Timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(callCtx, a.Timeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(callCtx, method, u.String(), nil)
	if err != nil {
		return MerchantInvokeResult{}, err
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	if a.UserAgent != "" {
		req.Header.Set("User-Agent", a.UserAgent)
	}
	if request.PaymentSignature != "" {
		req.Header.Set("PAYMENT-SIGNATURE", request.PaymentSignature)
	}
	// Merchant side effects are keyed by the same durable operation identity
	// that the runtime persists. Send both spellings used by deployed merchant
	// implementations so a retry after a runtime crash cannot mint a second
	// delivery.
	req.Header.Set("X-StablePay-Invocation-Idempotency", request.IdempotencyKey)
	req.Header.Set("Idempotency-Key", request.IdempotencyKey)
	resp, err := client.Do(req)
	if err != nil {
		return MerchantInvokeResult{}, err
	}
	defer resp.Body.Close()
	limit := a.BodyLimit
	if limit <= 0 {
		limit = DefaultMerchantBodyLimit
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return MerchantInvokeResult{}, err
	}
	if int64(len(body)) > limit {
		return MerchantInvokeResult{}, ErrMerchantResponseTooLarge
	}
	result := MerchantInvokeResult{HTTPStatus: resp.StatusCode, ContentType: resp.Header.Get("Content-Type"), Body: body, OccurredAt: time.Now().UTC()}
	result.PayloadHash = invocation.PayloadHash(body)
	result.PayloadRef = "merchant-response://" + strings.TrimPrefix(result.PayloadHash, "sha256:")
	result.Headers = selectedProtocolHeaders(resp.Header)
	return result, nil
}

func selectedProtocolHeaders(headers http.Header) map[string]string {
	result := make(map[string]string)
	// HTTP header names are case-insensitive. The deployed Merchant currently
	// emits both v2 PAYMENT-REQUIRED and legacy Payment-Required spellings, so
	// Header.Get cannot safely select the v2 value. Prefer the value whose
	// decoded payload explicitly declares x402Version=2.
	if value := preferredPaymentRequired(headers); value != "" {
		result["PAYMENT-REQUIRED"] = value
	}
	for _, name := range []string{"PAYMENT-RESPONSE", "Content-Type", "Accept-Payment"} {
		if value := strings.TrimSpace(headers.Get(name)); value != "" {
			result[name] = value
		}
	}
	return result
}

func preferredPaymentRequired(headers http.Header) string {
	values := headers.Values("PAYMENT-REQUIRED")
	fallback := ""
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if fallback == "" {
			fallback = value
		}
		decoded, ok := decodePaymentRequiredHeader(value)
		if !ok {
			continue
		}
		var top struct {
			X402Version int `json:"x402Version"`
		}
		if json.Unmarshal(decoded, &top) == nil && top.X402Version == 2 {
			return value
		}
	}
	return fallback
}

func decodePaymentRequiredHeader(value string) ([]byte, bool) {
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if decoded, err := encoding.DecodeString(value); err == nil {
			return decoded, true
		}
	}
	if json.Valid([]byte(value)) {
		return []byte(value), true
	}
	return nil, false
}

func (r MerchantInvokeResult) Validate() error {
	if r.HTTPStatus < 100 || r.HTTPStatus > 599 || len(r.Body) > invocation.MaxStoredPayloadBytes || strings.TrimSpace(r.PayloadHash) == "" || strings.TrimSpace(r.PayloadRef) == "" || r.OccurredAt.IsZero() {
		return fmt.Errorf("invalid merchant invoke result")
	}
	return nil
}
