package test

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/ut"
	"stablepay/api-gateway/internal/app"
	"stablepay/api-gateway/internal/domain"
	"stablepay/api-gateway/internal/infrastructure/config"
	"stablepay/api-gateway/internal/infrastructure/observability"
)

func newTestConfig() *config.AppConfig {
	return &config.AppConfig{
		Server: config.Server{Address: ":18080", ReadTimeoutMS: 1000, WriteTimeoutMS: 1000, GracefulStopSecs: 3},
		Security: config.Security{
			AllowedAPIKeys:        []string{"k1"},
			AllowedTimestampSkewS: 300,
			RequireNonce:          true,
			CanonicalVersion:      "v0.1",
		},
		Routes: []domain.RoutePolicy{
			{Name: "payment.pay", Method: "POST", Path: "/api/v1/pay", Auth: domain.AuthDID, RateLimits: domain.RateLimitRule{IPPerMinute: 100, DIDPerMinute: 50, RoutePerMinute: 10}},
			{Name: "verification.verify", Method: "GET", Path: "/api/v1/verify", Auth: domain.AuthAPI, RateLimits: domain.RateLimitRule{IPPerMinute: 100, RoutePerMinute: 100}},
			{Name: "shortcut.pay", Method: "GET", Path: "/pay", Auth: domain.AuthNone, RateLimits: domain.RateLimitRule{IPPerMinute: 100, RoutePerMinute: 30}},
		},
	}
}

func TestShortcutPay(t *testing.T) {
	instance, err := app.New(newTestConfig(), observability.NewTestLogger())
	if err != nil {
		t.Fatalf("bootstrap error: %v", err)
	}
	req := ut.PerformRequest(instance.Engine.Engine, "GET", "/pay?skill=did:solana:dev1&price=5.00", nil)
	if req.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", req.Code)
	}
}

func TestVerifyNeedsAPIKey(t *testing.T) {
	instance, err := app.New(newTestConfig(), observability.NewTestLogger())
	if err != nil {
		t.Fatalf("bootstrap error: %v", err)
	}
	req := ut.PerformRequest(instance.Engine.Engine, "GET", "/api/v1/verify?agent_did=a&skill_did=b", nil)
	if req.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", req.Code)
	}
}

func TestPayRequiresSignatureFields(t *testing.T) {
	instance, err := app.New(newTestConfig(), observability.NewTestLogger())
	if err != nil {
		t.Fatalf("bootstrap error: %v", err)
	}
	body := bytes.NewBufferString(`{"agent_did":"did:solana:a","skill_did":"did:solana:b","amount":"5.00","currency":"USDC"}`)
	req := ut.PerformRequest(instance.Engine.Engine, "POST", "/api/v1/pay", &ut.Body{Body: body, Len: body.Len()})
	if req.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", req.Code)
	}
}
