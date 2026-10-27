package fiat

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func requestFixture() CreateRequest {
	return CreateRequest{RequestID: "purchase-1", ProductID: "review", Subject: "采购预审服务", QuoteHash: "quote-hash", ApprovalRef: "approval-1", AmountMinor: 3, Currency: "CNY"}
}
func localService(t *testing.T) (*Service, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "orders.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	return NewService(store, LocalProvider{}), path
}
func TestConcurrentIdempotencyAndRestart(t *testing.T) {
	s, path := localService(t)
	var wg sync.WaitGroup
	ids := make(chan string, 20)
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			order, err := s.Create(requestFixture())
			if err != nil {
				t.Error(err)
				return
			}
			ids <- order.ID
		}()
	}
	wg.Wait()
	close(ids)
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatal("duplicate economic order")
		}
	}
	paid, err := s.Simulate(first)
	if err != nil || paid.VerifiedBy != "local-simulation" {
		t.Fatalf("%+v %v", paid, err)
	}
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewService(store, LocalProvider{})
	again, err := restarted.Create(requestFixture())
	if err != nil || again.ID != first || again.Status != "PAID" {
		t.Fatalf("restart replay failed: %+v %v", again, err)
	}
	changed := requestFixture()
	changed.AmountMinor = 4
	if _, err := s.Create(changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("amount rebinding accepted: %v", err)
	}
}
func TestReceiptCannotRebindAmountOrTrade(t *testing.T) {
	s, _ := localService(t)
	first, _ := s.Create(requestFixture())
	bad := Receipt{OrderID: first.ID, AmountMinor: 4, TradeNo: "T1", Status: "PAID", VerifiedBy: "test-signed"}
	if _, err := s.applyReceipt(bad); !errors.Is(err, ErrConflict) {
		t.Fatal("wrong amount accepted")
	}
	bad.AmountMinor = 3
	if _, err := s.applyReceipt(bad); err != nil {
		t.Fatal(err)
	}
	secondRequest := requestFixture()
	secondRequest.RequestID = "purchase-2"
	second, _ := s.Create(secondRequest)
	bad.OrderID = second.ID
	if _, err := s.applyReceipt(bad); !errors.Is(err, ErrConflict) {
		t.Fatal("trade reused across orders")
	}
	bad.OrderID = first.ID
	bad.Status = "CLOSED"
	result, err := s.applyReceipt(bad)
	if err != nil || result.Status != "PAID" {
		t.Fatal("late callback regressed success")
	}
}
func alipayFixture(t *testing.T) (*Alipay, *rsa.PrivateKey) {
	t.Helper()
	merchant, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	public, _ := x509.MarshalPKIXPublicKey(&provider.PublicKey)
	adapter, err := NewAlipay(AlipayConfig{AppID: "test-app", SellerID: "test-seller", NotifyURL: "https://example.com/notify", ReturnURL: "https://example.com/payathon"}, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(merchant)}), pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public}))
	if err != nil {
		t.Fatal(err)
	}
	return adapter, provider
}
func testSign(t *testing.T, key *rsa.PrivateKey, content []byte) string {
	t.Helper()
	hash := sha256.Sum256(content)
	raw, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}
func TestRSA2NotificationIdentityAndTampering(t *testing.T) {
	adapter, key := alipayFixture(t)
	params := map[string]string{"app_id": "test-app", "seller_id": "test-seller", "sign_type": "RSA2", "out_trade_no": "SPtest", "trade_no": "A123", "total_amount": "0.03", "trade_status": "TRADE_SUCCESS", "subject": "采购 + 核验"}
	params["sign"] = testSign(t, key, []byte(canonical(params, true)))
	receipt, err := adapter.VerifyNotification(params)
	if err != nil || receipt.AmountMinor != 3 || receipt.Status != "PAID" {
		t.Fatalf("%+v %v", receipt, err)
	}
	params["total_amount"] = "0.01"
	if _, err := adapter.VerifyNotification(params); err == nil {
		t.Fatal("forged amount accepted")
	}
	params["total_amount"] = "0.03"
	params["seller_id"] = "other"
	params["sign"] = testSign(t, key, []byte(canonical(params, true)))
	if _, err := adapter.VerifyNotification(params); err == nil {
		t.Fatal("wrong seller accepted even with valid signature")
	}
	store, _ := NewFileStore(filepath.Join(t.TempDir(), "alipay.json"))
	service := NewService(store, adapter)
	order, _ := service.Create(requestFixture())
	if _, err := service.Simulate(order.ID); err == nil {
		t.Fatal("simulator enabled on Alipay")
	}
	link, err := adapter.Checkout(order)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(link)
	if parsed.Host != "openapi-sandbox.dl.alipaydev.com" || parsed.Query().Get("method") != "alipay.trade.page.pay" || parsed.Query().Get("sign") == "" {
		t.Fatal("invalid checkout contract")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }
func TestSignedQueryAndUnsignedResponse(t *testing.T) {
	a, key := alipayFixture(t)
	raw := []byte(`{"code":"10000","out_trade_no":"SPtest","trade_no":"T1","total_amount":"0.03","trade_status":"TRADE_SUCCESS"}`)
	signature := testSign(t, key, raw)
	a.Client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		_ = req.ParseForm()
		if req.PostForm.Get("method") != "alipay.trade.query" {
			t.Fatal("wrong query method")
		}
		envelope := map[string]any{"alipay_trade_query_response": json.RawMessage(raw), "sign": signature}
		body, _ := json.Marshal(envelope)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})}
	result, err := a.Query(context.Background(), Order{ID: "SPtest"})
	if err != nil || result.VerifiedBy != "alipay-rsa2-query" {
		t.Fatalf("%+v %v", result, err)
	}
	signature = ""
	if _, err := a.Query(context.Background(), Order{ID: "SPtest"}); err == nil {
		t.Fatal("unsigned query confirmed payment")
	}
}
func TestHTTPRequiresServiceTokenAndRejectsLocalNotify(t *testing.T) {
	s, _ := localService(t)
	handler := Handler(s, "this-is-a-test-internal-token")
	req := httptest.NewRequest("POST", "/v1/fiat/orders", strings.NewReader(`{}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != 401 {
		t.Fatal(response.Code)
	}
	req = httptest.NewRequest("POST", "/v1/fiat/alipay/notify", strings.NewReader("app_id=x&app_id=y"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != 400 {
		t.Fatal(response.Code)
	}
}
func TestYuanUsesExactMinorUnits(t *testing.T) {
	for _, raw := range []string{"0.001", "NaN", "1e2", "-1.00", "1."} {
		if _, err := ParseYuan(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if cents, err := ParseYuan("0.29"); err != nil || cents != 29 {
		t.Fatal(cents, err)
	}
}

func TestSignedNotificationDurablyConfirmsMatchingOrder(t *testing.T) {
	adapter, key := alipayFixture(t)
	store, err := NewFileStore(filepath.Join(t.TempDir(), "orders.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(store, adapter)
	order, err := s.Create(requestFixture())
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]string{"app_id": "test-app", "seller_id": "test-seller", "sign_type": "RSA2", "out_trade_no": order.ID, "trade_no": "SANDBOX_T1", "total_amount": "0.03", "trade_status": "TRADE_SUCCESS", "subject": "预算 + 采购%检查"}
	params["sign"] = testSign(t, key, []byte(canonical(params, true)))
	values := url.Values{}
	for name, value := range params {
		values.Set(name, value)
	}
	handler := Handler(s, "test-internal-token-123456")
	for range 2 {
		req := httptest.NewRequest("POST", "/v1/fiat/alipay/notify", strings.NewReader(values.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != 200 || response.Body.String() != "success" {
			t.Fatalf("signed notification rejected: %d %s", response.Code, response.Body.String())
		}
	}
	confirmed, err := s.Get(order.ID)
	if err != nil || confirmed.Status != "PAID" || confirmed.VerifiedBy != "alipay-rsa2-notify" || confirmed.PaidAt == nil {
		t.Fatalf("not durably confirmed: %+v %v", confirmed, err)
	}
	values.Set("total_amount", "0.04")
	req := httptest.NewRequest("POST", "/v1/fiat/alipay/notify", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != 400 {
		t.Fatal("tampered form accepted")
	}
}
