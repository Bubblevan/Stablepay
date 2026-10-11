package agentshopping

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const httpApprovalTestAPIKey = "simulation-api-key-that-is-long-enough-32"
const httpApprovalTestBuyerKey = "separate-buyer-approval-key-is-long-enough"

func TestQuoteConfirmationRequiresIndependentBuyerCredential(t *testing.T) {
	api, err := NewHTTPAPI(&MySQLStore{tenant: "isolated-test"}, catalogHTTPTestParser{}, httpApprovalTestAPIKey, httpApprovalTestBuyerKey)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api)
	defer server.Close()

	for _, test := range []struct {
		name       string
		buyerKey   string
		wantStatus int
		wantCode   string
	}{
		{name: "agent has only simulation key", wantStatus: http.StatusUnauthorized, wantCode: "BUYER_APPROVAL_REQUIRED"},
		{name: "wrong buyer key", buyerKey: "attacker-supplied-value", wantStatus: http.StatusUnauthorized, wantCode: "BUYER_APPROVAL_REQUIRED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/quotes/quote-1/confirm", strings.NewReader(`{}`))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("X-Simulation-API-Key", httpApprovalTestAPIKey)
			if test.buyerKey != "" {
				request.Header.Set("X-Simulation-Buyer-Approval-Key", test.buyerKey)
			}
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != test.wantStatus || !strings.Contains(string(body), test.wantCode) {
				t.Fatalf("response=%d %q, want %d with code %s", response.StatusCode, body, test.wantStatus, test.wantCode)
			}
		})
	}

	called := false
	approvalGate := api.buyerApprovalAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodPost, "/v1/quotes/quote-1/confirm", nil)
	request.Header.Set("X-Simulation-Buyer-Approval-Key", httpApprovalTestBuyerKey)
	response := httptest.NewRecorder()
	approvalGate.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || !called {
		t.Fatalf("valid buyer credential did not pass approval gate: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestQuoteConfirmationFailsClosedWithoutBuyerApprovalConfiguration(t *testing.T) {
	api, err := NewHTTPAPI(&MySQLStore{tenant: "isolated-test"}, catalogHTTPTestParser{}, httpApprovalTestAPIKey)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/quotes/quote-1/confirm", strings.NewReader(`{}`))
	request.Header.Set("X-Simulation-API-Key", httpApprovalTestAPIKey)
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "BUYER_APPROVAL_NOT_CONFIGURED") {
		t.Fatalf("unconfigured approval path should fail closed: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestBuyerConfirmationIDIsNotDerivableFromQuoteIdempotencyKey(t *testing.T) {
	first, err := newBuyerConfirmationID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := newBuyerConfirmationID()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !strings.HasPrefix(first, "confirmation_") || len(first) != len("confirmation_")+64 {
		t.Fatalf("confirmation IDs should be unique opaque random values: first=%q second=%q", first, second)
	}
}
