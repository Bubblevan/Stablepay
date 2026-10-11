package composition

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/application"
	"github.com/stablepay/commerce-runtime/internal/x402"
)

func TestHTTPBuyerQuoteValidatorAcceptsOnlyExactCurrentChallenge(t *testing.T) {
	const requesterDID = "did:solana:11111111111111111111111111111111"
	var amount = "250000"
	var status = http.StatusPaymentRequired
	var redirects = false
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Query().Get("agent_did") != requesterDID {
			t.Errorf("unexpected Merchant validation request: method=%s query=%v", r.Method, r.URL.Query())
		}
		if redirects {
			http.Redirect(w, r, "/redirect-target", http.StatusFound)
			return
		}
		if status != http.StatusPaymentRequired {
			w.WriteHeader(status)
			return
		}
		body := buyerQuoteChallenge(server.URL+"/api/v1/products/sku-1/execute", amount)
		// Include a valid older v1 header with different amount. Runtime
		// validation must prefer canonical v2, independent of header order.
		legacy, _ := json.Marshal(map[string]any{"x402Version": 1, "accepts": []any{map[string]any{
			"scheme": "exact", "network": "devnet", "maxAmountRequired": "350000", "asset": "USDC",
			"payTo": "11111111111111111111111111111111", "resource": server.URL + "/api/v1/products/sku-1/execute",
			"maxTimeoutSeconds": 300, "extra": map[string]any{"currency": "USDC", "productId": "sku-1"},
		}}})
		w.Header().Add("PAYMENT-REQUIRED", base64.StdEncoding.EncodeToString(legacy))
		w.Header().Add("PAYMENT-REQUIRED", base64.StdEncoding.EncodeToString(body))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	resourceURL := server.URL + "/api/v1/products/sku-1/execute"
	challenge := buyerQuoteChallenge(resourceURL, amount)
	parsed, err := x402.DecodeBodyRequirement(challenge)
	if err != nil {
		t.Fatal(err)
	}
	offer := application.BuyerOffer{RequesterDID: requesterDID, QuoteHash: parsed.CanonicalHash,
		ResourceURL: resourceURL, ProductID: "sku-1", AmountMinor: 25, Currency: "USDC", Network: "devnet", Asset: "USDC",
		ExpiresAt: time.Now().Add(time.Minute)}
	validator := NewHTTPBuyerQuoteValidator(server.Client())
	if err := validator.ValidateBuyerOffer(context.Background(), offer); err != nil {
		t.Fatalf("unchanged quote rejected: %v", err)
	}

	amount = "350000"
	if err := validator.ValidateBuyerOffer(context.Background(), offer); !errors.Is(err, ErrLiveBuyerQuoteChanged) {
		t.Fatalf("changed price was not rejected as stale: %v", err)
	}

	amount = "250000"
	status = http.StatusNotFound
	if err := validator.ValidateBuyerOffer(context.Background(), offer); !errors.Is(err, ErrLiveBuyerQuoteUnavailable) {
		t.Fatalf("unavailable product did not fail closed: %v", err)
	}

	status = http.StatusPaymentRequired
	redirects = true
	if err := validator.ValidateBuyerOffer(context.Background(), offer); !errors.Is(err, ErrLiveBuyerQuoteUnavailable) {
		t.Fatalf("redirect was followed or accepted: %v", err)
	}
}

func buyerQuoteChallenge(resourceURL, amount string) []byte {
	challenge := struct {
		X402Version int `json:"x402Version"`
		Resource    struct {
			URL string `json:"url"`
		} `json:"resource"`
		Accepts []struct {
			Scheme            string         `json:"scheme"`
			Network           string         `json:"network"`
			Amount            string         `json:"amount"`
			Asset             string         `json:"asset"`
			PayTo             string         `json:"payTo"`
			MaxTimeoutSeconds int            `json:"maxTimeoutSeconds"`
			Extra             map[string]any `json:"extra"`
		} `json:"accepts"`
	}{X402Version: 2}
	challenge.Resource.URL = resourceURL
	challenge.Accepts = []struct {
		Scheme            string         `json:"scheme"`
		Network           string         `json:"network"`
		Amount            string         `json:"amount"`
		Asset             string         `json:"asset"`
		PayTo             string         `json:"payTo"`
		MaxTimeoutSeconds int            `json:"maxTimeoutSeconds"`
		Extra             map[string]any `json:"extra"`
	}{{Scheme: "exact", Network: "devnet", Amount: amount, Asset: "USDC",
		PayTo: "11111111111111111111111111111111", MaxTimeoutSeconds: 300,
		Extra: map[string]any{"currency": "USDC", "productId": "sku-1"}}}
	body, err := json.Marshal(challenge)
	if err != nil {
		panic(err)
	}
	return body
}

func TestHTTPBuyerQuoteValidatorRejectsOversizedChallenge(t *testing.T) {
	const requesterDID = "did:solana:11111111111111111111111111111111"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(strings.Repeat("x", 100)))
	}))
	defer server.Close()
	validator := NewHTTPBuyerQuoteValidator(server.Client())
	validator.BodyLimit = 16
	err := validator.ValidateBuyerOffer(context.Background(), application.BuyerOffer{RequesterDID: requesterDID,
		ResourceURL: server.URL + "/execute", QuoteHash: "sha256:expected"})
	if !errors.Is(err, ErrLiveBuyerQuoteUnavailable) {
		t.Fatalf("oversized challenge did not fail closed: %v", err)
	}
}
