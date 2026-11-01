package provider

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
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func fixture(t *testing.T) (*Alipay, *rsa.PrivateKey) {
	t.Helper()
	appKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	providerKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	public, _ := x509.MarshalPKIXPublicKey(&providerKey.PublicKey)
	appPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: mustPKCS8(t, appKey)})
	a, err := NewAlipay(Config{AppID: "sandbox-app", SellerID: "sandbox-seller", NotifyURL: "https://merchant.example/notify", ReturnURL: "https://merchant.example/return"}, appPEM, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public}))
	if err != nil {
		t.Fatal(err)
	}
	return a, providerKey
}
func mustPKCS8(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()
	raw, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func sign(t *testing.T, key *rsa.PrivateKey, raw []byte) string {
	t.Helper()
	digest := sha256.Sum256(raw)
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(sig)
}
func TestCheckoutAndNotificationSignature(t *testing.T) {
	a, providerKey := fixture(t)
	link, err := a.Checkout(Order{ID: "SP123", Subject: "预算审核", AmountMinor: 3})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(link)
	if u.Host != "openapi-sandbox.dl.alipaydev.com" || u.Query().Get("method") != "alipay.trade.page.pay" || u.Query().Get("sign") == "" {
		t.Fatal("invalid Alipay sandbox cashier URL")
	}
	params := map[string]string{"app_id": "sandbox-app", "seller_id": "sandbox-seller", "sign_type": "RSA2", "out_trade_no": "SP123", "trade_no": "ALI-123", "total_amount": "0.03", "trade_status": "TRADE_SUCCESS"}
	params["sign"] = sign(t, providerKey, []byte(canonical(params, true)))
	receipt, err := a.VerifyNotification(params)
	if err != nil || receipt.AmountMinor != 3 || receipt.Status != "PAID" || receipt.VerifiedBy != "alipay-rsa2-notify" {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	params["total_amount"] = "0.04"
	if _, err := a.VerifyNotification(params); err == nil {
		t.Fatal("tampered notification accepted")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }
func TestQueryRequiresSignedAlipayResponse(t *testing.T) {
	a, providerKey := fixture(t)
	payload := []byte(`{"code":"10000","out_trade_no":"SP123","trade_no":"ALI-123","total_amount":"0.03","trade_status":"TRADE_SUCCESS","seller_id":"sandbox-seller"}`)
	signature := sign(t, providerKey, payload)
	a.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		_ = req.ParseForm()
		if req.PostForm.Get("method") != "alipay.trade.query" {
			t.Fatal("wrong API method")
		}
		body, _ := json.Marshal(map[string]any{"alipay_trade_query_response": json.RawMessage(payload), "sign": signature})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})}
	receipt, err := a.Query(context.Background(), Order{ID: "SP123"})
	if err != nil || receipt.Status != "PAID" || receipt.VerifiedBy != "alipay-rsa2-query" {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	signature = ""
	if _, err := a.Query(context.Background(), Order{ID: "SP123"}); err == nil {
		t.Fatal("unsigned Alipay query response accepted")
	}
}

func TestAmountPrecision(t *testing.T) {
	for _, value := range []string{"0.001", "NaN", "-1.00", "1."} {
		if _, err := parseYuan(value); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
	if cents, err := parseYuan("0.29"); err != nil || cents != 29 {
		t.Fatalf("got %d %v", cents, err)
	}
}
