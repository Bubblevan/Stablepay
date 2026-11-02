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
	"time"
)

func machineFixture(t *testing.T) (*MachinePay, *rsa.PrivateKey, *rsa.PrivateKey) {
	t.Helper()
	appKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	alipayKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(appKey)
	if err != nil {
		t.Fatal(err)
	}
	public, err := x509.MarshalPKIXPublicKey(&alipayKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	machine, err := NewMachinePay(MachinePayConfig{AppID: "ai-pay-app", ServiceID: "API123456", GatewayURL: sandboxGateway},
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public}))
	if err != nil {
		t.Fatal(err)
	}
	return machine, appKey, alipayKey
}

func signedMachineResponse(t *testing.T, key *rsa.PrivateKey, method string, payload map[string]any) io.ReadCloser {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	name := strings.ReplaceAll(method, ".", "_") + "_response"
	envelope, err := json.Marshal(map[string]any{name: json.RawMessage(raw), "sign": sign(t, key, raw)})
	if err != nil {
		t.Fatal(err)
	}
	return io.NopCloser(strings.NewReader(string(envelope)))
}

func verifyMachineRequestSignature(t *testing.T, values url.Values, key *rsa.PrivateKey) {
	t.Helper()
	params := make(map[string]string, len(values))
	for name := range values {
		if values.Get(name) != "" {
			params[name] = values.Get(name)
		}
	}
	signature, err := base64.StdEncoding.DecodeString(params["sign"])
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(canonical(params, false)))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], signature); err != nil {
		t.Fatalf("request signature did not verify: %v", err)
	}
}

func TestMachinePayPrecreateAndVerifyProof(t *testing.T) {
	machine, appKey, alipayKey := machineFixture(t)
	privateDER, err := x509.MarshalPKCS8PrivateKey(appKey)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&alipayKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parsePrivateKey([]byte(base64.StdEncoding.EncodeToString(privateDER))); err != nil {
		t.Fatalf("parse console-style base64 application key: %v", err)
	}
	if _, err := parsePublicKey([]byte(base64.StdEncoding.EncodeToString(publicDER))); err != nil {
		t.Fatalf("parse console-style base64 Alipay key: %v", err)
	}
	proofHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"protocol":{"payment_proof":"proof-1","trade_no":"ALI-1"},"method":{"client_session":"agent-session"}}`))
	machine.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != sandboxGateway || req.Method != http.MethodPost {
			t.Fatalf("unexpected gateway request: %s %s", req.Method, req.URL)
		}
		if err := req.ParseForm(); err != nil {
			t.Fatal(err)
		}
		verifyMachineRequestSignature(t, req.PostForm, appKey)
		var response io.ReadCloser
		switch req.PostForm.Get("method") {
		case machinePayPrecreateMethod:
			var biz map[string]string
			if err := json.Unmarshal([]byte(req.PostForm.Get("biz_content")), &biz); err != nil {
				t.Fatal(err)
			}
			if biz["service_id"] != "API123456" || biz["out_trade_no"] != "MP-1" || biz["amount"] != "0.01" || biz["currency"] != "CNY" || biz["memo"] != "/reports/latest" {
				t.Fatalf("unexpected Machine Pay precreate payload: %+v", biz)
			}
			response = signedMachineResponse(t, alipayKey, machinePayPrecreateMethod, map[string]any{"code": "10000", "msg": "Success", "amt_pre_pay_id": "amt_pre_pay_1"})
		case machinePayVerifyMethod:
			var biz map[string]string
			if err := json.Unmarshal([]byte(req.PostForm.Get("biz_content")), &biz); err != nil {
				t.Fatal(err)
			}
			if biz["trade_no"] != "ALI-1" || biz["payment_proof"] != "proof-1" || biz["client_session"] != "agent-session" {
				t.Fatalf("unexpected Machine Pay proof payload: %+v", biz)
			}
			response = signedMachineResponse(t, alipayKey, machinePayVerifyMethod, map[string]any{"code": "10000", "msg": "Success", "amount": "0.01", "out_trade_no": "MP-1", "trade_no": "ALI-1", "resource_id": "/reports/latest", "active": true})
		default:
			t.Fatalf("unexpected Alipay method %q", req.PostForm.Get("method"))
		}
		return &http.Response{StatusCode: http.StatusOK, Body: response, Header: make(http.Header)}, nil
	})}

	order := MachinePayOrder{OrderID: "MP-1", ResourceID: "/reports/latest", GoodsName: "Latest report", AmountMinor: 1, PayBefore: time.Now().Add(10 * time.Minute)}
	link, err := machine.Precreate(context.Background(), order)
	if err != nil || link != "amt_pre_pay_1" {
		t.Fatalf("link=%q err=%v", link, err)
	}
	receipt, err := machine.Verify(context.Background(), proofHeader, order)
	if err != nil || !receipt.Active || receipt.OrderID != order.OrderID || receipt.TradeNo != "ALI-1" || receipt.AmountMinor != 1 || receipt.ResourceID != order.ResourceID {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
}

func TestMachinePayRejectsUnboundOrInvalidProofs(t *testing.T) {
	machine, _, alipayKey := machineFixture(t)
	proofHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"protocol":{"payment_proof":"proof-1","trade_no":"ALI-1"},"method":{}}`))
	machine.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		response := signedMachineResponse(t, alipayKey, machinePayVerifyMethod, map[string]any{"code": "10000", "msg": "Success", "amount": "0.02", "out_trade_no": "MP-OTHER", "trade_no": "ALI-1", "resource_id": "/other", "active": true})
		return &http.Response{StatusCode: http.StatusOK, Body: response, Header: make(http.Header)}, nil
	})}
	order := MachinePayOrder{OrderID: "MP-1", ResourceID: "/reports/latest", GoodsName: "Latest report", AmountMinor: 1}
	if _, err := machine.Verify(context.Background(), proofHeader, order); err == nil {
		t.Fatal("unbound Machine Pay receipt was accepted")
	}
	if _, err := ParseMachinePayProof("not-base64"); err == nil {
		t.Fatal("malformed Payment-Proof was accepted")
	}
	if _, err := ParseMachinePayProof(base64.StdEncoding.EncodeToString([]byte(`{"protocol":{"payment_proof":"p"}}`))); err == nil {
		t.Fatal("Payment-Proof without trade_no was accepted")
	}
}

func TestMachinePayOrderBounds(t *testing.T) {
	bad := MachinePayOrder{OrderID: "x", ResourceID: "/r", GoodsName: "service", AmountMinor: 5001, PayBefore: time.Now().Add(time.Minute)}
	if err := validateMachinePayOrder(bad); err == nil {
		t.Fatal("amount above Alipay's documented ¥50 limit was accepted")
	}
	bad.AmountMinor = 1
	bad.OrderID = strings.Repeat("x", 33)
	if err := validateMachinePayOrder(bad); err == nil {
		t.Fatal("order ID above Alipay's documented 32-character limit was accepted")
	}
}
