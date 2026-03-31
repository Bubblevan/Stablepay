package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"stablepay/api-gateway/internal/application"
	"stablepay/api-gateway/internal/domain"
)

type capturePaymentClient struct {
	lastReq map[string]interface{}
}

func (c *capturePaymentClient) Pay(_ context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	clone := make(map[string]interface{}, len(req))
	for key, value := range req {
		clone[key] = value
	}
	c.lastReq = clone
	return map[string]interface{}{
		"agent_did":   req["agent_did"],
		"signature":   req["signature"],
		"timestamp":   req["timestamp"],
		"nonce":       req["nonce"],
		"gateway_did": req["did"],
	}, http.StatusOK, 0, nil
}

func (c *capturePaymentClient) GetPaymentRequirement(_ context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	clone := make(map[string]interface{}, len(req))
	for key, value := range req {
		clone[key] = value
	}
	c.lastReq = clone
	return map[string]interface{}{
		"skill_did":        req["skill_did"],
		"skill_name":       req["skill_name"],
		"price":            req["price"],
		"currency":         req["currency"],
		"message":          req["message"],
		"payment_endpoint": "/api/v1/pay",
	}, http.StatusPaymentRequired, 402, nil
}

func (c *capturePaymentClient) GetPayment(context.Context, string) (map[string]interface{}, int, int, error) {
	return map[string]interface{}{}, http.StatusOK, 0, nil
}

func (c *capturePaymentClient) GetPaymentHistory(context.Context, map[string]interface{}) (map[string]interface{}, int, int, error) {
	return map[string]interface{}{}, http.StatusOK, 0, nil
}

type noopDIDClient struct{}

func (n *noopDIDClient) CreateDID(context.Context, map[string]interface{}) (map[string]interface{}, int, int, error) {
	return map[string]interface{}{}, http.StatusOK, 0, nil
}

func (n *noopDIDClient) VerifyDID(context.Context, map[string]interface{}) (map[string]interface{}, int, int, error) {
	return map[string]interface{}{}, http.StatusOK, 0, nil
}

func (n *noopDIDClient) GetDID(context.Context, string) (map[string]interface{}, int, int, error) {
	return map[string]interface{}{}, http.StatusOK, 0, nil
}

func (n *noopDIDClient) VerifySignature(context.Context, map[string]interface{}) (bool, error) {
	return true, nil
}

type noopVerificationClient struct{}

func (n *noopVerificationClient) Verify(context.Context, map[string]interface{}) (map[string]interface{}, int, int, error) {
	return map[string]interface{}{}, http.StatusOK, 0, nil
}

func (n *noopVerificationClient) BatchVerify(context.Context, map[string]interface{}) (map[string]interface{}, int, int, error) {
	return map[string]interface{}{}, http.StatusOK, 0, nil
}

func (n *noopVerificationClient) GetProof(context.Context, map[string]interface{}) (map[string]interface{}, int, int, error) {
	return map[string]interface{}{}, http.StatusOK, 0, nil
}

type captureVerificationClient struct {
	lastReq map[string]interface{}
}

func (c *captureVerificationClient) Verify(_ context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	clone := make(map[string]interface{}, len(req))
	for key, value := range req {
		clone[key] = value
	}
	c.lastReq = clone
	return map[string]interface{}{
		"agent_did": req["agent_did"],
		"skill_did": req["skill_did"],
	}, http.StatusOK, 0, nil
}

func (c *captureVerificationClient) BatchVerify(context.Context, map[string]interface{}) (map[string]interface{}, int, int, error) {
	return map[string]interface{}{}, http.StatusOK, 0, nil
}

func (c *captureVerificationClient) GetProof(context.Context, map[string]interface{}) (map[string]interface{}, int, int, error) {
	return map[string]interface{}{}, http.StatusOK, 0, nil
}

type noopQueryClient struct{}

func (n *noopQueryClient) GetBalance(context.Context, map[string]interface{}) (map[string]interface{}, int, int, error) {
	return map[string]interface{}{}, http.StatusOK, 0, nil
}

func (n *noopQueryClient) GetTransactions(context.Context, map[string]interface{}) (map[string]interface{}, int, int, error) {
	return map[string]interface{}{}, http.StatusOK, 0, nil
}

func (n *noopQueryClient) GetRevenue(context.Context, map[string]interface{}) (map[string]interface{}, int, int, error) {
	return map[string]interface{}{}, http.StatusOK, 0, nil
}

func TestProxyPreservesPaymentBodySignatureFields(t *testing.T) {
	paymentClient := &capturePaymentClient{}
	svc := application.NewService(&noopDIDClient{}, paymentClient, &noopVerificationClient{}, &noopQueryClient{})
	handler := NewHandler(svc)

	h := server.Default()
	h.POST(
		"/api/v1/pay",
		func(ctx context.Context, c *app.RequestContext) {
			c.Set(domain.CtxDID, "did:solana:gateway-header")
			c.Set(domain.CtxSignature, "gateway-signature")
			c.Set(domain.CtxTimestamp, "2026-03-30T14:00:00Z")
			c.Set(domain.CtxNonce, "gateway-nonce")
			c.Next(ctx)
		},
		handler.Proxy("payment.pay"),
	)

	body := strings.NewReader(`{"agent_did":"did:solana:agent","skill_did":"did:solana:skill","amount":"1.00","currency":"USDC","signature":"payment-signature","timestamp":1700000000,"nonce":"payment-nonce"}`)
	resp := ut.PerformRequest(
		h.Engine,
		http.MethodPost,
		"/api/v1/pay",
		&ut.Body{Body: body, Len: body.Len()},
		ut.Header{Key: "Content-Type", Value: "application/json"},
		ut.Header{Key: "X-Idempotency-Key", Value: "idem-1"},
	)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.Code, resp.Body.String())
	}

	if got := paymentClient.lastReq["signature"]; got != "payment-signature" {
		t.Fatalf("expected payment signature to be preserved, got %#v", got)
	}
	if got := paymentClient.lastReq["nonce"]; got != "payment-nonce" {
		t.Fatalf("expected payment nonce to be preserved, got %#v", got)
	}
	if got := paymentClient.lastReq["timestamp"]; got != float64(1700000000) {
		t.Fatalf("expected payment timestamp to be preserved, got %#v", got)
	}
	if got := paymentClient.lastReq["did"]; got != "did:solana:gateway-header" {
		t.Fatalf("expected gateway did to be injected separately, got %#v", got)
	}
	if got := paymentClient.lastReq["idempotency_key"]; got != "idem-1" {
		t.Fatalf("expected idempotency key to be forwarded, got %#v", got)
	}
}

func TestProxyPassesThroughPaymentRequirement(t *testing.T) {
	paymentClient := &capturePaymentClient{}
	svc := application.NewService(&noopDIDClient{}, paymentClient, &noopVerificationClient{}, &noopQueryClient{})
	handler := NewHandler(svc)

	h := server.Default()
	h.GET("/api/v1/pay/require", handler.Proxy("payment.require"))

	resp := ut.PerformRequest(
		h.Engine,
		http.MethodGet,
		"/api/v1/pay/require?skill_did=did%3Asolana%3Askill&skill_name=ShowMeTheMoney&price=1.00&currency=USDC&message=Pay%20to%20unlock",
		nil,
	)

	if resp.Code != http.StatusPaymentRequired {
		t.Fatalf("expected 402, got %d body=%s", resp.Code, resp.Body.String())
	}

	var envelope struct {
		Code int `json:"code"`
		Data struct {
			SkillDID string `json:"skill_did"`
			Price    string `json:"price"`
			Currency string `json:"currency"`
			Message  string `json:"message"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if envelope.Code != 402 {
		t.Fatalf("expected 402 code, got %+v", envelope)
	}
	if envelope.Data.SkillDID != "did:solana:skill" {
		t.Fatalf("expected skill did in payload, got %+v", envelope.Data)
	}
	if envelope.Data.Message != "Pay to unlock" {
		t.Fatalf("expected message to survive 402 passthrough, got %+v", envelope.Data)
	}
	if got := paymentClient.lastReq["price"]; got != "1.00" {
		t.Fatalf("expected price query to reach payment client, got %#v", got)
	}
}

func TestSetIfAbsentKeepsExistingValue(t *testing.T) {
	req := map[string]interface{}{"signature": "payment-signature"}
	setIfAbsent(req, "signature", "gateway-signature")
	if req["signature"] != "payment-signature" {
		t.Fatalf("expected existing value to be preserved, got %#v", req["signature"])
	}
}

func TestSetIfAbsentInjectsMissingValue(t *testing.T) {
	req := map[string]interface{}{}
	setIfAbsent(req, "did", "did:solana:test")
	if req["did"] != "did:solana:test" {
		t.Fatalf("expected missing value to be injected, got %#v", req["did"])
	}
}

func TestProxyResponseIsEnvelope(t *testing.T) {
	paymentClient := &capturePaymentClient{}
	svc := application.NewService(&noopDIDClient{}, paymentClient, &noopVerificationClient{}, &noopQueryClient{})
	handler := NewHandler(svc)

	h := server.Default()
	h.POST("/api/v1/pay", handler.Proxy("payment.pay"))

	body := strings.NewReader(`{"agent_did":"did:solana:agent","skill_did":"did:solana:skill","amount":"1.00","currency":"USDC","signature":"sig","timestamp":1700000000,"nonce":"nonce"}`)
	resp := ut.PerformRequest(
		h.Engine,
		http.MethodPost,
		"/api/v1/pay",
		&ut.Body{Body: body, Len: body.Len()},
		ut.Header{Key: "Content-Type", Value: "application/json"},
		ut.Header{Key: "X-Idempotency-Key", Value: "idem-1"},
	)

	var envelope struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if envelope.Code != 0 {
		t.Fatalf("expected success envelope, got %+v", envelope)
	}
}

func TestProxyDecodesQueryStringValues(t *testing.T) {
	verificationClient := &captureVerificationClient{}
	svc := application.NewService(&noopDIDClient{}, &capturePaymentClient{}, verificationClient, &noopQueryClient{})
	handler := NewHandler(svc)

	h := server.Default()
	h.GET("/api/v1/verify", handler.Proxy("verification.verify"))

	resp := ut.PerformRequest(
		h.Engine,
		http.MethodGet,
		"/api/v1/verify?agent_did=did%3Asolana%3Aagent&skill_did=did%3Asolana%3Askill",
		nil,
	)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if got := verificationClient.lastReq["agent_did"]; got != "did:solana:agent" {
		t.Fatalf("expected decoded agent did, got %#v", got)
	}
	if got := verificationClient.lastReq["skill_did"]; got != "did:solana:skill" {
		t.Fatalf("expected decoded skill did, got %#v", got)
	}
}
