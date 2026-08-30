// Copyright 2025 StablePay. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package client implements Infrastructure-layer external service clients.
//
// StablePayClient is a technical adapter. It translates the Application-layer
// PaymentVerifier port into concrete HTTP calls to the StablePay Gateway.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	appPort "github.com/stablepay/merchant-server/internal/application/port"
	domainSvc "github.com/stablepay/merchant-server/internal/domain/service"
)

const defaultHTTPTimeout = 10 * time.Second

// StablePayClient calls StablePay Gateway HTTP APIs.
type StablePayClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewStablePayClient creates a Gateway client with a default timeout.
func NewStablePayClient(baseURL, apiKey string) *StablePayClient {
	return NewStablePayClientWithHTTPClient(baseURL, apiKey, &http.Client{Timeout: defaultHTTPTimeout})
}

// NewStablePayClientWithHTTPClient creates a Gateway client with an injected HTTP client.
// Tests can pass httptest-backed clients here without changing Application code.
func NewStablePayClientWithHTTPClient(baseURL, apiKey string, httpClient *http.Client) *StablePayClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &StablePayClient{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:     strings.TrimSpace(apiKey),
		httpClient: httpClient,
	}
}

// VerifyPurchase verifies purchase state through StablePay Gateway.
//
// Current Gateway contract used by the existing StablePay services:
//
//	GET /api/v1/verify/proof?agent_did=...&skill_did=...
//	X-API-Key: <api key>
func (c *StablePayClient) VerifyPurchase(ctx context.Context, req appPort.VerifyPurchaseRequest) (*appPort.VerifyPurchaseResult, error) {
	agentDID := strings.TrimSpace(req.AgentDID)
	skillDID := strings.TrimSpace(req.SkillDID)
	if agentDID == "" {
		return nil, fmt.Errorf("stablepay verify purchase: agent_did is required")
	}
	if skillDID == "" {
		return nil, fmt.Errorf("stablepay verify purchase: skill_did is required")
	}
	if c.baseURL == "" {
		return nil, fmt.Errorf("stablepay verify purchase: gateway base url is required")
	}

	endpoint, err := url.Parse(c.baseURL + "/api/v1/verify/proof")
	if err != nil {
		return nil, fmt.Errorf("stablepay verify purchase: parse endpoint: %w", err)
	}
	q := endpoint.Query()
	q.Set("agent_did", agentDID)
	q.Set("skill_did", skillDID)
	endpoint.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("stablepay verify purchase: create request: %w", err)
	}
	if c.apiKey != "" {
		httpReq.Header.Set("X-API-Key", c.apiKey)
	}
	if sig := strings.TrimSpace(req.PaymentSignature); sig != "" {
		httpReq.Header.Set(domainSvc.X402PaymentSignatureHeader, sig)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("stablepay verify purchase: call gateway: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB safety limit.
	if err != nil {
		return nil, fmt.Errorf("stablepay verify purchase: read response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return &appPort.VerifyPurchaseResult{Purchased: false}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("stablepay verify purchase: gateway status=%d body=%s", resp.StatusCode, trimForError(body))
	}

	result, err := parseVerifyProofResponse(body)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func parseVerifyProofResponse(body []byte) (*appPort.VerifyPurchaseResult, error) {
	var envelope struct {
		Code      int             `json:"code"`
		Message   string          `json:"message"`
		Data      json.RawMessage `json:"data"`
		RequestID string          `json:"request_id"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("stablepay verify purchase: decode response envelope: %w", err)
	}
	if envelope.Code != 0 {
		return nil, fmt.Errorf("stablepay verify purchase: gateway code=%d message=%s", envelope.Code, envelope.Message)
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return &appPort.VerifyPurchaseResult{Purchased: false}, nil
	}

	var data struct {
		Purchased bool   `json:"purchased"`
		TxID      string `json:"tx_id"`
		TxHash    string `json:"tx_hash"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		return nil, fmt.Errorf("stablepay verify purchase: decode response data: %w", err)
	}

	proof := map[string]any{}
	if err := json.Unmarshal(envelope.Data, &proof); err != nil {
		return nil, fmt.Errorf("stablepay verify purchase: decode proof map: %w", err)
	}

	return &appPort.VerifyPurchaseResult{
		Purchased: data.Purchased,
		TxID:      data.TxID,
		TxHash:    data.TxHash,
		Proof:     proof,
	}, nil
}

func trimForError(body []byte) string {
	const max = 512
	text := strings.TrimSpace(string(body))
	if len(text) <= max {
		return text
	}
	return text[:max] + "..."
}

// SettlePayment is intentionally not used by the current merchant resource flow.
// Payment submission is handled by OpenClaw through stablepay_pay_via_gateway.
func (c *StablePayClient) SettlePayment(ctx context.Context, paymentData map[string]interface{}) error {
	_ = ctx
	_ = paymentData
	return nil
}
