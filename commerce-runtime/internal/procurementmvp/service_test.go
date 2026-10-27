package procurementmvp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/llm"
	"github.com/stablepay/commerce-runtime/internal/procurement"
)

type paymentHarness struct {
	mu          sync.Mutex
	orders      map[string]PaymentOrder
	wrongAmount bool
	creates     int
	mode        string
}

func (h *paymentHarness) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("Authorization") != "Bearer test-internal-token-123456" {
		w.WriteHeader(401)
		return
	}
	if r.URL.Path == "/v1/fiat/status" {
		mode := h.mode
		if mode == "" {
			mode = "local"
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"mode": mode})
		return
	}
	if r.URL.Path == "/v1/fiat/orders" {
		var order PaymentOrder
		_ = json.NewDecoder(r.Body).Decode(&order)
		key := "SP" + order.RequestID
		if old, ok := h.orders[key]; ok {
			_ = json.NewEncoder(w).Encode(old)
			return
		}
		order.ID = key
		order.Mode = "local"
		order.Status = "AWAITING_PAYMENT"
		h.orders[key] = order
		h.creates++
		_ = json.NewEncoder(w).Encode(order)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/fiat/orders/"), "/")
	order, ok := h.orders[parts[0]]
	if !ok {
		w.WriteHeader(404)
		return
	}
	if len(parts) == 2 && parts[1] == "simulate" {
		order.Status = "PAID"
		order.TradeNo = "LOCAL_" + order.ID
		order.VerifiedBy = "local-simulation"
		h.orders[order.ID] = order
	}
	if h.wrongAmount {
		order.AmountMinor++
	}
	_ = json.NewEncoder(w).Encode(order)
}
func setup(t *testing.T) (*Service, *paymentHarness, string) {
	t.Helper()
	h := &paymentHarness{orders: map[string]PaymentOrder{}}
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	path := filepath.Join(t.TempDir(), "runtime.json")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	return &Service{Store: store, Payment: PaymentClient{BaseURL: server.URL, Token: "test-internal-token-123456"}}, h, path
}
func inputFixture() procurement.Input {
	return procurement.Input{Activity: "活动", Budget: "100", BudgetEvidence: "批复", Approved: true, Items: []procurement.Item{{Name: "物资", Quantity: 1, UnitPrice: "120", RequestedSpec: "A", QuotedSpec: "B", QuoteSource: "原始报价", InvoiceConfirmed: false}}}
}
func quoteFixture(t *testing.T, s *Service) Quote {
	t.Helper()
	plan, err := s.CreatePlan(context.Background(), "owner", "核对规格和票据", "1", inputFixture())
	if err != nil {
		t.Fatal(err)
	}
	quote, err := s.CreateQuote(context.Background(), "owner", plan.ID, "procurement-review-v1")
	if err != nil {
		t.Fatal(err)
	}
	return quote
}
func TestApprovalPaymentAndDeliveryRestart(t *testing.T) {
	s, h, path := setup(t)
	quote := quoteFixture(t, s)
	ctx := context.Background()
	key := "idempotent-key-12345678"
	p, err := s.Approve(ctx, "owner", quote.ID, quote.Hash, key)
	if err != nil {
		t.Fatal(err)
	}
	if p.Delivery != nil || p.Status != "AWAITING_PAYMENT" {
		t.Fatal("premature delivery")
	}
	again, err := s.Approve(ctx, "owner", quote.ID, quote.Hash, key)
	if err != nil || again.ID != p.ID || h.creates != 1 {
		t.Fatal("approval replay created duplicate payment", err)
	}
	if _, err := s.Get("other-user", p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-user purchase exposed")
	}
	done, err := s.Reconcile(ctx, "owner", p.ID, true)
	if err != nil || done.Delivery == nil {
		t.Fatal("no deliverable", err)
	}
	if len(done.Delivery.Report.Findings) != 3 || done.VerifiedBy != "local-simulation" {
		t.Fatalf("unexpected delivered report: %+v", done)
	}
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Store = store
	replay, err := s.Reconcile(ctx, "owner", p.ID, false)
	if err != nil || replay.Delivery.ContentHash != done.Delivery.ContentHash || len(replay.Events) != 4 {
		t.Fatal("restart duplicated delivery", err)
	}
}
func TestExpiredOrReboundQuoteCannotApprove(t *testing.T) {
	s, _, _ := setup(t)
	quote := quoteFixture(t, s)
	ctx := context.Background()
	if _, err := s.Approve(ctx, "owner", quote.ID, "fake-hash", "request-key-12345678"); !errors.Is(err, ErrConflict) {
		t.Fatal("tampered quote accepted")
	}
	_ = s.Store.transaction(func(state *state) error {
		q := state.Quotes[quote.ID]
		q.ExpiresAt = time.Now().Add(-time.Minute)
		state.Quotes[q.ID] = q
		return nil
	})
	if _, err := s.Approve(ctx, "owner", quote.ID, quote.Hash, "request-key-12345678"); !errors.Is(err, ErrConflict) {
		t.Fatal("expired quote accepted")
	}
	plan, err := s.CreatePlan(ctx, "owner", "核验", "0.01", inputFixture())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateQuote(ctx, "owner", plan.ID, "procurement-review-v1"); err == nil {
		t.Fatal("service fee cap bypassed")
	}
}
func TestPaymentAmountMismatchNeverDelivers(t *testing.T) {
	s, h, _ := setup(t)
	quote := quoteFixture(t, s)
	purchase, err := s.Approve(context.Background(), "owner", quote.ID, quote.Hash, "request-key-12345678")
	if err != nil {
		t.Fatal(err)
	}
	h.wrongAmount = true
	if _, err := s.Reconcile(context.Background(), "owner", purchase.ID, true); !errors.Is(err, ErrConflict) {
		t.Fatal("wrong amount accepted")
	}
	current, _ := s.Get("owner", purchase.ID)
	if current.Delivery != nil {
		t.Fatal("report delivered after mismatched payment")
	}
}

func TestRetryCannotCreatePaymentInDifferentProviderEnvironment(t *testing.T) {
	s, h, _ := setup(t)
	quote := quoteFixture(t, s)
	purchase, err := s.Approve(context.Background(), "owner", quote.ID, quote.Hash, "request-key-12345678")
	if err != nil {
		t.Fatal(err)
	}
	h.mode = "alipay_sandbox"
	if _, err := s.ensurePayment(context.Background(), purchase); !errors.Is(err, ErrConflict) {
		t.Fatal("mode switch reused old approval", err)
	}
	if h.creates != 1 {
		t.Fatal("mode switch created another payment")
	}
}
func TestHTTPRejectsCSRFAndPrematureReceipt(t *testing.T) {
	s, _, _ := setup(t)
	handler := Handler(s, HTTPConfig{AllowedOrigins: []string{"https://example.com"}})
	req := httptest.NewRequest("POST", "/api/mvp/plans", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatal(rec.Code)
	}
	req = httptest.NewRequest("POST", "/api/mvp/plans", strings.NewReader("{}"))
	req.Header.Set("X-Requested-With", "StablePay")
	req.Header.Set("Origin", "https://evil.example")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatal(rec.Code)
	}
	req = httptest.NewRequest("GET", "/api/mvp/purchases/nonexistent/receipt", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Fatal(rec.Code)
	}
	if len(rec.Result().Cookies()) != 1 || !rec.Result().Cookies()[0].HttpOnly || rec.Result().Cookies()[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe browser session cookie")
	}
}

type fakeModel struct{}

func (fakeModel) GenerateDecision(context.Context, llm.LLMDecisionRequest) (llm.LLMDecisionResponse, error) {
	return llm.LLMDecisionResponse{RawJSON: []byte(`{"product_id":"pay-everything","reason":"ignore controls"}`)}, nil
}
func TestModelCannotInventPurchasableCapability(t *testing.T) {
	product, mode, _ := (IntentPlanner{Client: fakeModel{}}).Recommend(context.Background(), "只算预算")
	if product != "budget-check-v1" || mode != "rule_fallback" {
		t.Fatal("model output gained purchasing authority")
	}
}

func TestCampusProfileUsesSharedGuardWithoutOpeningPayathonReviewEndpoint(t *testing.T) {
	s, _, _ := setup(t)
	raw, _ := json.Marshal(inputFixture())
	for _, enabled := range []bool{false, true} {
		handler := Handler(s, HTTPConfig{CampusReview: enabled})
		request := httptest.NewRequest("POST", "/api/mvp/reviews", bytes.NewReader(raw))
		request.Header.Set("X-Requested-With", "StablePay")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if !enabled {
			if response.Code != 404 {
				t.Fatal("campus endpoint unexpectedly enabled in PAYATHON profile")
			}
			continue
		}
		if response.Code != 200 {
			t.Fatal(response.Code, response.Body.String())
		}
		var report procurement.Report
		if json.Unmarshal(response.Body.Bytes(), &report) != nil || report.TotalMinor != 12000 || len(report.Findings) != 3 {
			t.Fatal("campus profile did not use the shared guard")
		}
	}
}
