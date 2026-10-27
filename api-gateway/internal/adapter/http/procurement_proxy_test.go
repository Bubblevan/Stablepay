package http

import (
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

func TestProcurementProxyPreservesConfirmationBoundary(t *testing.T) {
	upstream := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.URL.Path != "/api/mvp/purchases" || r.Header.Get("Cookie") != "stablepay_mvp=session" || r.Header.Get("Origin") != "https://app.example" || r.Header.Get("Idempotency-Key") != "approval-key" || r.Header.Get("X-Requested-With") != "StablePay" {
			t.Error("browser confirmation context was not preserved")
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("untrusted client authorization forwarded")
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"accepted":true}` {
			t.Error("confirmation body altered")
		}
		w.Header().Add("Set-Cookie", "stablepay_mvp=next; HttpOnly; SameSite=Strict")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(202)
		_, _ = w.Write([]byte(`{"status":"AWAITING_PAYMENT"}`))
	}))
	defer upstream.Close()
	h := server.Default()
	if err := RegisterProcurementProxy(h, upstream.URL, ""); err != nil {
		t.Fatal(err)
	}
	body := strings.NewReader(`{"accepted":true}`)
	response := ut.PerformRequest(h.Engine, "POST", "/api/mvp/purchases", &ut.Body{Body: body, Len: body.Len()}, ut.Header{Key: "Cookie", Value: "stablepay_mvp=session"}, ut.Header{Key: "Origin", Value: "https://app.example"}, ut.Header{Key: "Idempotency-Key", Value: "approval-key"}, ut.Header{Key: "X-Requested-With", Value: "StablePay"}, ut.Header{Key: "Authorization", Value: "Bearer client-fake"})
	if response.Code != 202 || !strings.Contains(response.Body.String(), "AWAITING_PAYMENT") {
		t.Fatal(response.Code, response.Body.String())
	}
	if !strings.Contains(string(response.Result().Header.Peek("Set-Cookie")), "HttpOnly") {
		t.Fatal("session cookie attributes were dropped")
	}
	private := ut.PerformRequest(h.Engine, "POST", "/v1/fiat/orders", nil)
	if private.Code != 404 {
		t.Fatal("private payment routes exposed")
	}
}

func TestNotifyProxyOnlyTargetsProviderNotification(t *testing.T) {
	upstream := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.URL.Path != "/v1/fiat/alipay/notify" {
			t.Error("notification routed to wrong boundary")
		}
		_, _ = w.Write([]byte("success"))
	}))
	defer upstream.Close()
	h := server.Default()
	if err := RegisterProcurementProxy(h, "", upstream.URL); err != nil {
		t.Fatal(err)
	}
	response := ut.PerformRequest(h.Engine, "POST", "/api/payments/alipay/notify", nil)
	if response.Code != 200 || response.Body.String() != "success" {
		t.Fatal(response.Code, response.Body.String())
	}
	if err := RegisterProcurementProxy(server.Default(), "https://example.com/untrusted/path", ""); err == nil {
		t.Fatal("upstream path accepted")
	}
}
