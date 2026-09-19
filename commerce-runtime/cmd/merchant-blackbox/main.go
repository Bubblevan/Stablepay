package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/stablepay/commerce-runtime/internal/adapters"
	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/invocation"
	"github.com/stablepay/commerce-runtime/internal/x402"
)

func main() {
	endpoint := os.Getenv("MERCHANT_BLACKBOX_ENDPOINT")
	if strings.TrimSpace(endpoint) == "" {
		endpoint = "http://127.0.0.1:8787/api/v1/products/ai-agent-job-2025/execute"
	}
	adapter := adapters.NewHTTPMerchantAdapter(nil)
	ref := catalog.EndpointRef{Endpoint: endpoint, Method: "GET"}
	initialRequest := adapters.MerchantInvokeRequest{
		EpisodeID: "blackbox-episode", RequesterDID: "did:stablepay:blackbox", MerchantDID: "did:merchant:blackbox",
		CapabilityID: "ai-agent-job-2025", CatalogVersion: "v1", CatalogSnapshotHash: "sha256:blackbox-catalog",
		CatalogSnapshotRef: "catalog://blackbox/v1", Attempt: 1, Phase: invocation.PhaseInitial, TraceID: "blackbox-initial",
		IdempotencyKey: "blackbox:initial", Endpoint: ref,
	}
	initial, err := adapter.Invoke(context.Background(), initialRequest)
	if err != nil {
		fail("initial merchant request", err)
	}
	if initial.HTTPStatus != 402 {
		fail("initial merchant request", fmt.Errorf("expected HTTP 402, got %d", initial.HTTPStatus))
	}
	parsed, err := x402.ParseRequired(initial.Headers, initial.Body)
	if err != nil {
		fail("parse actual merchant x402 challenge", err)
	}
	if parsed.AtomicAmount != 2000000 || parsed.BusinessAmountMinor != 200 || !strings.EqualFold(parsed.Currency, "USDC") {
		fail("actual merchant amount regression", fmt.Errorf("atomic=%d decimals=%d business=%d currency=%s", parsed.AtomicAmount, parsed.AtomicDecimals, parsed.BusinessAmountMinor, parsed.Currency))
	}

	paidRequest := initialRequest
	paidRequest.Attempt = 1
	paidRequest.Phase = invocation.PhaseDelivery
	paidRequest.TraceID = "blackbox-delivery"
	paidRequest.IdempotencyKey = "blackbox:delivery:1"
	paidRequest.EntitlementRef = "blackbox-entitlement"
	paidRequest.PaymentIntentID = "blackbox-intent"
	paidRequest.PaymentSignature = "blackbox-payment"
	paid, err := adapter.Invoke(context.Background(), paidRequest)
	if err != nil {
		fail("paid merchant request", err)
	}
	if paid.HTTPStatus != 200 {
		fail("paid merchant request", fmt.Errorf("expected HTTP 200, got %d", paid.HTTPStatus))
	}
	replayed, err := adapter.Invoke(context.Background(), paidRequest)
	if err != nil {
		fail("replayed paid merchant request", err)
	}
	var paidJSON, replayedJSON any
	if err := json.Unmarshal(paid.Body, &paidJSON); err != nil {
		fail("decode paid merchant response", err)
	}
	if err := json.Unmarshal(replayed.Body, &replayedJSON); err != nil {
		fail("decode replayed merchant response", err)
	}
	if replayed.HTTPStatus != 200 || !reflect.DeepEqual(paidJSON, replayedJSON) {
		fail("replayed paid merchant request", fmt.Errorf("result was not semantically stable"))
	}
	fmt.Println("merchant black-box contract passed: HTTP 402 -> atomic 2000000/business 200 -> paid 200/idempotent replay")
}

func fail(stage string, err error) {
	fmt.Fprintf(os.Stderr, "merchant black-box contract failed at %s: %v\n", stage, err)
	os.Exit(1)
}
