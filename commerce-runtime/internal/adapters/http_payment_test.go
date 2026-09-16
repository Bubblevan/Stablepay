package adapters

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/payment"
)

func adapterIntent() payment.PaymentIntent {
	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	return payment.PaymentIntent{
		IntentID: "pi-1", EpisodeID: "episode-1", MerchantDID: "did:merchant:1", CapabilityID: "capability-1",
		QuoteHash: "sha256:quote", AmountMinor: 300, Currency: "USDC", RequesterDID: "did:agent:1",
		EpisodeVersion: 5, BudgetReservation: 300, IdempotencyKey: "pay-1", EconomicKey: payment.EconomicIdentityKey("episode-1", "did:merchant:1", "capability-1", "sha256:quote", 300, "USDC"),
		ExpiresAt: now.Add(time.Hour), Status: payment.IntentAuthorized, CreatedAt: now, UpdatedAt: now,
	}
}

func adapterAuthorization(intent payment.PaymentIntent) AuthorizationResult {
	return AuthorizationResult{Allowed: true, RequesterDID: intent.RequesterDID, MerchantDID: intent.MerchantDID,
		CapabilityID: intent.CapabilityID, QuoteHash: intent.QuoteHash, AmountMinor: intent.AmountMinor,
		Currency: intent.Currency, AuthorizationRef: "auth-1"}
}

func TestHTTPPaymentAdapterUsesIntentSnapshotAndMapsConfirmed(t *testing.T) {
	intent := adapterIntent()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/v1/pay" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("X-Idempotency-Key") != intent.IdempotencyKey {
			t.Fatalf("missing idempotency key: %q", request.Header.Get("X-Idempotency-Key"))
		}
		var body struct {
			AgentDID string `json:"agent_did"`
			SkillDID string `json:"skill_did"`
			Amount   string `json:"amount"`
			Currency string `json:"currency"`
			IntentID string `json:"intent_id"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.AgentDID != intent.RequesterDID || body.SkillDID != intent.CapabilityID || body.Amount != "3.00" || body.Currency != intent.Currency || body.IntentID != intent.IntentID {
			t.Fatalf("adapter sent untrusted or incorrectly formatted fields: %#v", body)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"data":{"tx_id":"tx-1","tx_hash":"hash-1","status":"confirmed","amount_minor":300,"currency":"USDC"}}`))
	}))
	defer server.Close()

	adapter := &HTTPPaymentAdapter{BaseURL: server.URL, Credentials: CredentialFunc(func(context.Context, payment.PaymentIntent) (PaymentCredentials, error) {
		return PaymentCredentials{Signature: "sig", Timestamp: "ts", Nonce: "nonce"}, nil
	})}
	outcome, err := adapter.Submit(context.Background(), PaymentSubmitRequest{Intent: intent, Authorization: adapterAuthorization(intent), TraceID: "trace-1"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != payment.OutcomeConfirmed || outcome.TxID != "tx-1" || outcome.AmountMinor != 300 || outcome.Currency != "USDC" {
		t.Fatalf("unexpected outcome: %#v", outcome)
	}
}

func TestHTTPPaymentAdapterMapsPendingFailedAndUnknown(t *testing.T) {
	intent := adapterIntent()
	statuses := []string{"pending", "failed", "unexpected"}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		index := 0
		if strings.HasSuffix(request.Header.Get("X-Trace-ID"), "-failed") {
			index = 1
		} else if strings.HasSuffix(request.Header.Get("X-Trace-ID"), "-unknown") {
			index = 2
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"tx_id":"tx-1","status":"` + statuses[index] + `","amount_minor":300,"currency":"USDC"}`))
	}))
	defer server.Close()
	adapter := &HTTPPaymentAdapter{BaseURL: server.URL, Credentials: CredentialFunc(func(context.Context, payment.PaymentIntent) (PaymentCredentials, error) {
		return PaymentCredentials{}, nil
	})}
	for _, test := range []struct {
		trace  string
		status payment.OutcomeStatus
	}{
		{trace: "trace-pending", status: payment.OutcomePending},
		{trace: "trace-failed", status: payment.OutcomeFailed},
		{trace: "trace-unknown", status: payment.OutcomeUnknown},
	} {
		outcome, err := adapter.Submit(context.Background(), PaymentSubmitRequest{Intent: intent, Authorization: adapterAuthorization(intent), TraceID: test.trace})
		if test.status == payment.OutcomeUnknown {
			if err == nil || outcome.Status != payment.OutcomeUnknown {
				t.Fatalf("expected unknown adapter response, outcome=%#v err=%v", outcome, err)
			}
			continue
		}
		if err != nil || outcome.Status != test.status {
			t.Fatalf("expected %s, outcome=%#v err=%v", test.status, outcome, err)
		}
	}
}

func TestHTTPPaymentAdapterQueryUsesTransactionIdentity(t *testing.T) {
	intent := adapterIntent()
	intent.Status = payment.IntentUnknown
	intent.TxID = "tx-1"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/v1/pay/tx-1" {
			t.Fatalf("unexpected query: %s %s", request.Method, request.URL.Path)
		}
		_, _ = writer.Write([]byte(`{"status":"completed","tx_id":"tx-1","amount_minor":300,"currency":"USDC"}`))
	}))
	defer server.Close()
	adapter := &HTTPPaymentAdapter{BaseURL: server.URL}
	outcome, err := adapter.Query(context.Background(), PaymentQuery{Intent: intent})
	if err != nil || outcome.Status != payment.OutcomeConfirmed {
		t.Fatalf("expected confirmed query, outcome=%#v err=%v", outcome, err)
	}
	intent.TxID = ""
	outcome, err = adapter.Query(context.Background(), PaymentQuery{Intent: intent})
	if err == nil || outcome.Status != payment.OutcomeUnknown {
		t.Fatalf("expected no-blind-resubmit unknown, outcome=%#v err=%v", outcome, err)
	}
}
