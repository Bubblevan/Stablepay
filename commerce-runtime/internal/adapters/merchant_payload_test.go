package adapters

import (
	"context"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/invocation"
)

func TestHTTPMerchantAdapterMaterializesWorkflowPayloadAtMerchantBoundary(t *testing.T) {
	payload := []byte("hello")
	payloadHash := invocation.PayloadHash(payload)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		if request.Method != http.MethodPost || string(body) != string(payload) || request.Header.Get("Content-Type") != "text/plain" || request.Header.Get("X-StablePay-Input-Content-Type") != "text/plain" {
			t.Errorf("merchant did not receive materialized payload: method=%s body=%q content_type=%q input_content_type=%q", request.Method, body, request.Header.Get("Content-Type"), request.Header.Get("X-StablePay-Input-Content-Type"))
		}
		if request.Header.Get("X-StablePay-Input-Payload-Hash") != payloadHash {
			t.Errorf("merchant received wrong payload hash header: got=%q want=%q", request.Header.Get("X-StablePay-Input-Payload-Hash"), payloadHash)
		}
		writer.Header().Set("Content-Type", "text/plain")
		_, _ = writer.Write([]byte("HELLO"))
	}))
	defer server.Close()

	rawHash, err := hex.DecodeString(strings.TrimPrefix(payloadHash, "sha256:"))
	if err != nil {
		t.Fatal(err)
	}
	request := MerchantInvokeRequest{EpisodeID: "episode-payload", RequesterDID: "did:agent:payload", MerchantDID: "did:merchant:payload", CapabilityID: "transform", CatalogVersion: "v1", CatalogSnapshotHash: "sha256:catalog", CatalogSnapshotRef: "catalog://payload", InputRef: "workflow-artifact://wr:payload/source", InputHash: strings.TrimPrefix(payloadHash, "sha256:"), InputPayload: payload, InputContentType: "text/plain", InputPayloadHash: rawHash, Attempt: 1, Phase: invocation.PhaseDelivery, TraceID: "trace-payload", IdempotencyKey: "invoke-payload", Endpoint: catalog.EndpointRef{Endpoint: server.URL, Method: http.MethodPost}}
	result, err := NewHTTPMerchantAdapter(server.Client()).Invoke(context.Background(), request)
	if err != nil || result.HTTPStatus != http.StatusOK || string(result.Body) != "HELLO" {
		t.Fatalf("HTTP merchant payload invocation failed: result=%#v err=%v", result, err)
	}
}
