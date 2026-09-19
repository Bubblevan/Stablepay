package adapters

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/invocation"
	"github.com/stablepay/commerce-runtime/internal/x402"
)

func TestHTTPMerchantAdapterUsesExisting402AndPaidContract(t *testing.T) {
	const resourcePath = "/api/v1/products/transcription-1/execute"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != resourcePath || request.URL.Query().Get("agent_did") != "did:stablepay:agent" {
			t.Fatalf("unexpected merchant request: %s", request.URL.String())
		}
		if request.Header.Get("PAYMENT-SIGNATURE") == "" {
			challenge := map[string]any{"x402Version": 2, "resource": map[string]any{"url": "http://merchant.example" + resourcePath}, "accepts": []any{map[string]any{"scheme": "exact", "network": "devnet", "amount": "300", "asset": "USDC", "payTo": "payee-1", "maxTimeoutSeconds": 300, "extra": map[string]any{"currency": "USDC", "skillDid": "did:payee:1"}}}}
			body, _ := json.Marshal(challenge)
			writer.Header().Set("PAYMENT-REQUIRED", base64.StdEncoding.EncodeToString(body))
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusPaymentRequired)
			_, _ = writer.Write(body)
			return
		}
		writer.Header().Set("Content-Type", "text/plain")
		_, _ = writer.Write([]byte("paid transcript"))
	}))
	defer server.Close()

	adapter := NewHTTPMerchantAdapter(server.Client())
	adapter.Timeout = time.Second
	initialRequest := MerchantInvokeRequest{EpisodeID: "episode-1", RequesterDID: "did:stablepay:agent", MerchantDID: "did:merchant:1", CapabilityID: "transcription", CatalogVersion: "v1", CatalogSnapshotHash: "sha256:catalog", CatalogSnapshotRef: "catalog://did:merchant:1/transcription/v1", Attempt: 1, Phase: invocation.PhaseInitial, TraceID: "trace-1", IdempotencyKey: "invoke-1", Endpoint: structEndpoint(server.URL + resourcePath)}
	initial, err := adapter.Invoke(context.Background(), initialRequest)
	if err != nil || initial.HTTPStatus != http.StatusPaymentRequired || initial.Headers["PAYMENT-REQUIRED"] == "" {
		t.Fatalf("expected real HTTP 402 contract: %#v err=%v", initial, err)
	}
	parsed, err := x402.ParseRequired(initial.Headers, initial.Body)
	if err != nil || parsed.AmountMinor != 300 || parsed.ProtocolVersion != "x402-v2" {
		t.Fatalf("parser did not consume merchant 402: %#v err=%v", parsed, err)
	}
	paidRequest := initialRequest
	paidRequest.Phase = invocation.PhaseDelivery
	paidRequest.Attempt = 1
	paidRequest.TraceID = "trace-2"
	paidRequest.IdempotencyKey = "invoke-2"
	paidRequest.PaymentSignature = "opaque-test-signature"
	paid, err := adapter.Invoke(context.Background(), paidRequest)
	if err != nil || paid.HTTPStatus != http.StatusOK || string(paid.Body) != "paid transcript" {
		t.Fatalf("expected paid merchant delivery: %#v err=%v", paid, err)
	}
	artifact := invocation.DeliveryArtifact{DeliveryID: "delivery-1", EpisodeID: "episode-1", InvocationID: "invoke-2", MerchantDID: paidRequest.MerchantDID, CapabilityID: paidRequest.CapabilityID, ContentType: paid.ContentType, PayloadRef: paid.PayloadRef, PayloadHash: paid.PayloadHash, Body: paid.Body, Attempt: 1, HTTPStatus: paid.HTTPStatus, ReceivedAt: paid.OccurredAt}
	if err := artifact.Validate(); err != nil || !strings.Contains(string(artifact.Body), "transcript") {
		t.Fatalf("paid response did not form a delivery artifact: %#v err=%v", artifact, err)
	}
}

func structEndpoint(value string) catalog.EndpointRef {
	return catalog.EndpointRef{Endpoint: value, Method: http.MethodGet}
}
