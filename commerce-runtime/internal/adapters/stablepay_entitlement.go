package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// StablePayVerificationEntitlement reads the persisted purchase proof through
// the real API Gateway. The proof is accepted only when its tx_id is exactly
// the runtime PaymentIntent.TxID; a generic purchased=true response is not
// sufficient evidence for this episode.
type StablePayVerificationEntitlement struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

func NewStablePayVerificationEntitlement(baseURL, apiKey string) *StablePayVerificationEntitlement {
	return &StablePayVerificationEntitlement{BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), APIKey: strings.TrimSpace(apiKey), HTTPClient: &http.Client{Timeout: 15 * time.Second}}
}

func (a *StablePayVerificationEntitlement) Verify(ctx context.Context, request EntitlementQuery) (EntitlementResult, error) {
	if a == nil || strings.TrimSpace(a.BaseURL) == "" {
		return EntitlementResult{}, fmt.Errorf("StablePay verification Gateway URL is required")
	}
	if strings.TrimSpace(request.RequesterDID) == "" || strings.TrimSpace(request.PayeeDID) == "" || strings.TrimSpace(request.TxID) == "" {
		return EntitlementResult{}, fmt.Errorf("verification entitlement requires requester, payee and tx_id")
	}
	endpoint, err := url.Parse(a.BaseURL + "/api/v1/verify/proof")
	if err != nil {
		return EntitlementResult{}, fmt.Errorf("parse verification endpoint: %w", err)
	}
	query := endpoint.Query()
	query.Set("agent_did", request.RequesterDID)
	query.Set("skill_did", request.PayeeDID)
	endpoint.RawQuery = query.Encode()
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return EntitlementResult{}, err
	}
	if a.APIKey != "" {
		httpRequest.Header.Set("X-API-Key", a.APIKey)
	}
	client := a.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return EntitlementResult{}, fmt.Errorf("call verification plane: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return EntitlementResult{}, err
	}
	if response.StatusCode == http.StatusNotFound {
		return EntitlementResult{Status: EntitlementUnknown, Reason: "verification proof not found yet"}, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return EntitlementResult{}, fmt.Errorf("verification plane status=%d body=%s", response.StatusCode, trimEntitlementError(body))
	}
	var envelope struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return EntitlementResult{}, fmt.Errorf("decode verification envelope: %w", err)
	}
	if envelope.Code != 0 {
		return EntitlementResult{}, fmt.Errorf("verification plane code=%d message=%s", envelope.Code, envelope.Message)
	}
	var proof struct {
		Purchased bool   `json:"purchased"`
		TxID      string `json:"tx_id"`
		TxHash    string `json:"tx_hash"`
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return EntitlementResult{Status: EntitlementUnknown, Reason: "verification proof is not available yet"}, nil
	}
	if err := json.Unmarshal(envelope.Data, &proof); err != nil {
		return EntitlementResult{}, fmt.Errorf("decode verification proof: %w", err)
	}
	if !proof.Purchased {
		return EntitlementResult{Status: EntitlementUnknown, Reason: "verification plane reports purchase not found"}, nil
	}
	if strings.TrimSpace(proof.TxID) == "" || proof.TxID != request.TxID {
		return EntitlementResult{Status: EntitlementInvalid, Reason: "verification proof tx_id does not match PaymentIntent"}, nil
	}
	return EntitlementResult{
		Status: EntitlementValid, PaymentIntentID: request.IntentID, TxID: proof.TxID, TxHash: proof.TxHash,
		Reference: "verification:" + proof.TxID, EvidenceRef: "verification://proof/" + proof.TxID,
	}, nil
}

func trimEntitlementError(body []byte) string {
	value := strings.TrimSpace(string(body))
	if len(value) > 512 {
		return value[:512] + "..."
	}
	return value
}
