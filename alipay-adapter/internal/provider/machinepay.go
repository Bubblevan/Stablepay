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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	machinePayPrecreateMethod = "alipay.aipay.agent.payment.precreate"
	machinePayVerifyMethod    = "alipay.aipay.agent.payment.verify"
	maxMachineProofBytes      = 16 << 10
)

// MachinePayConfig uses an AI Pay app/service identity. These credentials are
// separate from the ordinary website-payment configuration when the console
// issues a dedicated sandbox app.
type MachinePayConfig struct {
	AppID      string
	ServiceID  string
	GatewayURL string
}

// MachinePay handles Alipay's A2M metered-payment protocol. It intentionally
// stays separate from the x402 challenge/signature format: both use HTTP 402,
// but Machine Pay returns amtPaymentLink and consumes Payment-Proof.
type MachinePay struct {
	cfg     MachinePayConfig
	private *rsa.PrivateKey
	public  *rsa.PublicKey
	client  *http.Client
}

type MachinePayOrder struct {
	OrderID     string
	ResourceID  string
	GoodsName   string
	AmountMinor int64
	PayBefore   time.Time
}

type MachinePayProof struct {
	PaymentProof  string `json:"payment_proof"`
	TradeNo       string `json:"trade_no"`
	ClientSession string `json:"client_session,omitempty"`
}

type MachinePayReceipt struct {
	OrderID     string
	TradeNo     string
	ResourceID  string
	AmountMinor int64
	Active      bool
	VerifiedBy  string
}

func NewMachinePay(cfg MachinePayConfig, privatePEM, publicPEM []byte) (*MachinePay, error) {
	cfg.AppID = strings.TrimSpace(cfg.AppID)
	cfg.ServiceID = strings.TrimSpace(cfg.ServiceID)
	if cfg.AppID == "" || len(cfg.AppID) > 32 || cfg.ServiceID == "" || len(cfg.ServiceID) > 32 {
		return nil, errors.New("Machine Pay app_id and service_id are required")
	}
	if cfg.GatewayURL == "" {
		cfg.GatewayURL = sandboxGateway
	}
	u, err := url.Parse(cfg.GatewayURL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/gateway.do" ||
		(u.Host != "openapi-sandbox.dl.alipaydev.com" && u.Host != "openapi.alipay.com") {
		return nil, errors.New("Machine Pay gateway must be an official Alipay HTTPS gateway")
	}
	private, err := parsePrivateKey(privatePEM)
	if err != nil {
		return nil, err
	}
	public, err := parsePublicKey(publicPEM)
	if err != nil {
		return nil, err
	}
	return &MachinePay{cfg: cfg, private: private, public: public, client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func parsePrivateKey(raw []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(raw)
	der := []byte(nil)
	if block != nil {
		der = block.Bytes
	} else {
		decoded, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(raw)), ""))
		if err != nil {
			return nil, errors.New("invalid Machine Pay application private key")
		}
		der = decoded
	}
	var private *rsa.PrivateKey
	if key, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		private, _ = key.(*rsa.PrivateKey)
	} else {
		private, _ = x509.ParsePKCS1PrivateKey(der)
	}
	if private == nil || private.N.BitLen() < 2048 {
		return nil, errors.New("Machine Pay RSA2 requires an RSA application key of at least 2048 bits")
	}
	return private, nil
}

func parsePublicKey(raw []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(raw)
	der := []byte(nil)
	if block != nil {
		der = block.Bytes
	} else {
		decoded, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(raw)), ""))
		if err != nil {
			return nil, errors.New("invalid Machine Pay Alipay public key")
		}
		der = decoded
	}
	key, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, errors.New("Machine Pay Alipay public key must use PKIX PEM")
	}
	public, _ := key.(*rsa.PublicKey)
	if public == nil {
		return nil, errors.New("Machine Pay Alipay public key must be RSA")
	}
	return public, nil
}

// Precreate creates one Alipay metered-use order. The caller must keep the
// order and idempotency record durably before returning PaymentLink in a 402.
func (m *MachinePay) Precreate(ctx context.Context, order MachinePayOrder) (string, error) {
	if m == nil || m.client == nil {
		return "", errors.New("Machine Pay provider is not configured")
	}
	if err := validateMachinePayOrder(order); err != nil {
		return "", err
	}
	if order.PayBefore.IsZero() || !order.PayBefore.After(time.Now()) || order.PayBefore.After(time.Now().Add(12*time.Hour)) {
		return "", errors.New("Machine Pay pay_before must be within the next 12 hours")
	}
	response, err := m.execute(ctx, machinePayPrecreateMethod, map[string]string{
		"out_trade_no": order.OrderID,
		"service_id":   m.cfg.ServiceID,
		"goods_name":   order.GoodsName,
		"amount":       yuan(order.AmountMinor),
		"pay_before":   order.PayBefore.Format(time.RFC3339),
		"currency":     "CNY",
		"memo":         order.ResourceID,
	})
	if err != nil {
		return "", err
	}
	var result struct {
		Code        string `json:"code"`
		Msg         string `json:"msg"`
		SubMsg      string `json:"sub_msg"`
		PaymentLink string `json:"amt_pre_pay_id"`
	}
	if err := json.Unmarshal(response, &result); err != nil || result.Code != "10000" || result.PaymentLink == "" || len(result.PaymentLink) > 32 {
		return "", fmt.Errorf("Alipay Machine Pay precreate rejected or returned an invalid payment link: %s", responseMessage(result.Msg, result.SubMsg))
	}
	return result.PaymentLink, nil
}

// Verify parses the opaque Alipay Payment-Proof and verifies it server-side.
// The returned receipt is only safe to deliver against after every expected
// order/resource/amount binding has been checked by the caller.
func (m *MachinePay) Verify(ctx context.Context, header string, expected MachinePayOrder) (MachinePayReceipt, error) {
	if m == nil || m.client == nil {
		return MachinePayReceipt{}, errors.New("Machine Pay provider is not configured")
	}
	if err := validateMachinePayOrder(expected); err != nil {
		return MachinePayReceipt{}, err
	}
	receipt, err := m.VerifyProof(ctx, header)
	if err != nil {
		return MachinePayReceipt{}, err
	}
	if receipt.OrderID != expected.OrderID || receipt.ResourceID != expected.ResourceID || receipt.AmountMinor != expected.AmountMinor {
		return MachinePayReceipt{}, errors.New("Alipay Machine Pay receipt does not match the pending resource order")
	}
	return receipt, nil
}

// VerifyProof validates the buyer-supplied proof and the signed Alipay result.
// Payment Service must still bind the returned receipt to its durable order.
func (m *MachinePay) VerifyProof(ctx context.Context, header string) (MachinePayReceipt, error) {
	if m == nil || m.client == nil {
		return MachinePayReceipt{}, errors.New("Machine Pay provider is not configured")
	}
	proof, err := ParseMachinePayProof(header)
	if err != nil {
		return MachinePayReceipt{}, err
	}
	response, err := m.execute(ctx, machinePayVerifyMethod, map[string]string{
		"trade_no":       proof.TradeNo,
		"payment_proof":  proof.PaymentProof,
		"client_session": proof.ClientSession,
	})
	if err != nil {
		return MachinePayReceipt{}, err
	}
	var result struct {
		Code       string `json:"code"`
		Msg        string `json:"msg"`
		SubMsg     string `json:"sub_msg"`
		Amount     string `json:"amount"`
		OutTradeNo string `json:"out_trade_no"`
		TradeNo    string `json:"trade_no"`
		ResourceID string `json:"resource_id"`
		Active     bool   `json:"active"`
	}
	if err := json.Unmarshal(response, &result); err != nil || result.Code != "10000" {
		return MachinePayReceipt{}, fmt.Errorf("Alipay Machine Pay proof verification failed: %s", responseMessage(result.Msg, result.SubMsg))
	}
	amountMinor, err := parseYuan(result.Amount)
	if err != nil || result.OutTradeNo == "" || result.TradeNo != proof.TradeNo || result.ResourceID == "" || amountMinor <= 0 {
		return MachinePayReceipt{}, errors.New("Alipay Machine Pay returned an invalid verified receipt")
	}
	return MachinePayReceipt{OrderID: result.OutTradeNo, TradeNo: result.TradeNo, ResourceID: result.ResourceID, AmountMinor: amountMinor, Active: result.Active, VerifiedBy: "alipay-machinepay-proof"}, nil
}

func ParseMachinePayProof(header string) (MachinePayProof, error) {
	encoded := strings.TrimSpace(header)
	if encoded == "" || len(encoded) > maxMachineProofBytes*2 {
		return MachinePayProof{}, errors.New("Payment-Proof header is missing or too large")
	}
	var raw []byte
	var err error
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		raw, err = enc.DecodeString(encoded)
		if err == nil {
			break
		}
	}
	if err != nil || len(raw) == 0 || len(raw) > maxMachineProofBytes {
		return MachinePayProof{}, errors.New("Payment-Proof must be bounded base64 JSON")
	}
	var envelope struct {
		Protocol struct {
			PaymentProof string `json:"payment_proof"`
			TradeNo      string `json:"trade_no"`
		} `json:"protocol"`
		Method struct {
			ClientSession string `json:"client_session"`
		} `json:"method"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || len(envelope.Protocol.PaymentProof) == 0 || len(envelope.Protocol.PaymentProof) > 64 || len(envelope.Protocol.TradeNo) == 0 || len(envelope.Protocol.TradeNo) > 32 || len(envelope.Method.ClientSession) > 1024 {
		return MachinePayProof{}, errors.New("Payment-Proof has invalid protocol fields")
	}
	return MachinePayProof{PaymentProof: envelope.Protocol.PaymentProof, TradeNo: envelope.Protocol.TradeNo, ClientSession: envelope.Method.ClientSession}, nil
}

func validateMachinePayOrder(order MachinePayOrder) error {
	if strings.TrimSpace(order.OrderID) == "" || len(order.OrderID) > 32 || strings.ContainsAny(order.OrderID, "&=/") ||
		strings.TrimSpace(order.ResourceID) == "" || len([]rune(order.ResourceID)) > 32 ||
		strings.TrimSpace(order.GoodsName) == "" || len([]rune(order.GoodsName)) > 32 ||
		order.AmountMinor < 1 || order.AmountMinor > 5000 {
		return errors.New("Machine Pay requires a bounded order, resource, name, and CNY amount from 0.01 to 50.00")
	}
	return nil
}

func (m *MachinePay) execute(ctx context.Context, method string, business any) (json.RawMessage, error) {
	rawBusiness, err := json.Marshal(business)
	if err != nil {
		return nil, err
	}
	params := map[string]string{"app_id": m.cfg.AppID, "method": method, "format": "JSON", "charset": "utf-8", "sign_type": "RSA2",
		"timestamp": time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05"), "version": "1.0", "biz_content": string(rawBusiness)}
	digest := sha256.Sum256([]byte(canonical(params, false)))
	signature, err := rsa.SignPKCS1v15(rand.Reader, m.private, crypto.SHA256, digest[:])
	if err != nil {
		return nil, err
	}
	values := url.Values{}
	for key, value := range params {
		values.Set(key, value)
	}
	values.Set("sign", base64.StdEncoding.EncodeToString(signature))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.cfg.GatewayURL, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := m.client.Do(req)
	if err != nil {
		return nil, errors.New("Alipay Machine Pay gateway unavailable; order state remains unresolved")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil || response.StatusCode != http.StatusOK {
		return nil, errors.New("Alipay Machine Pay gateway returned an invalid response")
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(body, &envelope) != nil {
		return nil, errors.New("Alipay Machine Pay gateway returned malformed JSON")
	}
	responseKey := strings.ReplaceAll(method, ".", "_") + "_response"
	payload, signatureRaw := envelope[responseKey], envelope["sign"]
	var responseSignature string
	if len(payload) == 0 || json.Unmarshal(signatureRaw, &responseSignature) != nil || responseSignature == "" {
		return nil, errors.New("Alipay Machine Pay response is missing its signature")
	}
	decoded, err := base64.StdEncoding.DecodeString(responseSignature)
	if err != nil {
		return nil, errors.New("Alipay Machine Pay response signature is malformed")
	}
	responseDigest := sha256.Sum256(payload)
	if rsa.VerifyPKCS1v15(m.public, crypto.SHA256, responseDigest[:], decoded) != nil {
		return nil, errors.New("Alipay Machine Pay response signature verification failed")
	}
	return payload, nil
}

func responseMessage(message, detail string) string {
	if strings.TrimSpace(detail) != "" {
		return strings.TrimSpace(detail)
	}
	if strings.TrimSpace(message) != "" {
		return strings.TrimSpace(message)
	}
	return "Alipay returned an invalid response"
}
