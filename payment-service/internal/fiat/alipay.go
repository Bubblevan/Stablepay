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
	"net/url"
	"sort"
	"strings"
	"time"
)

type AlipayConfig struct{ AppID, SellerID, PrivateKeyFile, PublicKeyFile, NotifyURL, ReturnURL string }
type Alipay struct {
	cfg     AlipayConfig
	private *rsa.PrivateKey
	public  *rsa.PublicKey
	Client  *http.Client
}

const sandboxGateway = "https://openapi-sandbox.dl.alipaydev.com/gateway.do"

// This adapter intentionally supports only the computer-website sandbox.
// Mobile alipay.trade.agent.pay has a different contract and no sandbox.
func NewAlipay(cfg AlipayConfig, privatePEM, publicPEM []byte) (*Alipay, error) {
	if cfg.AppID == "" || cfg.SellerID == "" {
		return nil, errors.New("Alipay app_id and seller_id are required")
	}
	for _, raw := range []string{cfg.NotifyURL, cfg.ReturnURL} {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			return nil, errors.New("Alipay callback and return URLs must use HTTPS")
		}
	}
	block, _ := pem.Decode(privatePEM)
	if block == nil {
		return nil, errors.New("invalid application private key PEM")
	}
	var private *rsa.PrivateKey
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		private, _ = key.(*rsa.PrivateKey)
	} else {
		private, _ = x509.ParsePKCS1PrivateKey(block.Bytes)
	}
	if private == nil || private.N.BitLen() < 2048 {
		return nil, errors.New("RSA2 requires an RSA application key of at least 2048 bits")
	}
	block, _ = pem.Decode(publicPEM)
	if block == nil {
		return nil, errors.New("invalid Alipay public key PEM")
	}
	var public *rsa.PublicKey
	if key, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		public, _ = key.(*rsa.PublicKey)
	}
	if public == nil {
		return nil, errors.New("Alipay RSA public key must use PKIX PEM")
	}
	return &Alipay{cfg: cfg, private: private, public: public, Client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (*Alipay) Mode() string { return "alipay_sandbox" }

func canonical(params map[string]string, notification bool) string {
	keys := []string{}
	for key, value := range params {
		if key != "sign" && value != "" && (!notification || key != "sign_type") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, key+"="+params[key])
	}
	return strings.Join(pairs, "&")
}
func (a *Alipay) signed(method string, biz any) (url.Values, error) {
	raw, err := json.Marshal(biz)
	if err != nil {
		return nil, err
	}
	params := map[string]string{"app_id": a.cfg.AppID, "method": method, "format": "JSON", "charset": "utf-8", "sign_type": "RSA2", "timestamp": time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05"), "version": "1.0", "biz_content": string(raw)}
	if method == "alipay.trade.page.pay" {
		params["notify_url"], params["return_url"] = a.cfg.NotifyURL, a.cfg.ReturnURL
	}
	digest := sha256.Sum256([]byte(canonical(params, false)))
	signature, err := rsa.SignPKCS1v15(rand.Reader, a.private, crypto.SHA256, digest[:])
	if err != nil {
		return nil, err
	}
	params["sign"] = base64.StdEncoding.EncodeToString(signature)
	values := url.Values{}
	for key, value := range params {
		values.Set(key, value)
	}
	return values, nil
}
func (a *Alipay) Checkout(order Order) (string, error) {
	params, err := a.signed("alipay.trade.page.pay", map[string]string{"out_trade_no": order.ID, "total_amount": Yuan(order.AmountMinor), "subject": order.Subject, "product_code": "FAST_INSTANT_TRADE_PAY", "time_expire": time.Now().Add(15 * time.Minute).In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05")})
	if err != nil {
		return "", err
	}
	return sandboxGateway + "?" + params.Encode(), nil
}
func (a *Alipay) verify(content []byte, signature string) error {
	decoded, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return errors.New("invalid Alipay signature")
	}
	digest := sha256.Sum256(content)
	if rsa.VerifyPKCS1v15(a.public, crypto.SHA256, digest[:], decoded) != nil {
		return errors.New("Alipay signature verification failed")
	}
	return nil
}
func (a *Alipay) VerifyNotification(params map[string]string) (Receipt, error) {
	if params["sign_type"] != "RSA2" || params["app_id"] != a.cfg.AppID || params["seller_id"] != a.cfg.SellerID {
		return Receipt{}, errors.New("Alipay identity does not match this payment service")
	}
	if err := a.verify([]byte(canonical(params, true)), params["sign"]); err != nil {
		return Receipt{}, err
	}
	return receiptFrom(params["out_trade_no"], params["trade_no"], params["total_amount"], params["trade_status"], "alipay-rsa2-notify")
}
func (a *Alipay) Query(ctx context.Context, order Order) (Receipt, error) {
	params, err := a.signed("alipay.trade.query", map[string]string{"out_trade_no": order.ID})
	if err != nil {
		return Receipt{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sandboxGateway, strings.NewReader(params.Encode()))
	if err != nil {
		return Receipt{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := a.Client.Do(req)
	if err != nil {
		return Receipt{}, errors.New("Alipay query unavailable; payment remains unconfirmed")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil || response.StatusCode != http.StatusOK {
		return Receipt{}, errors.New("Alipay query failed")
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(raw, &envelope) != nil {
		return Receipt{}, errors.New("invalid Alipay query response")
	}
	payload := envelope["alipay_trade_query_response"]
	var signature string
	_ = json.Unmarshal(envelope["sign"], &signature)
	if len(payload) == 0 || a.verify(payload, signature) != nil {
		return Receipt{}, errors.New("Alipay query response signature is invalid")
	}
	var result struct {
		Code        string `json:"code"`
		OutTradeNo  string `json:"out_trade_no"`
		TradeNo     string `json:"trade_no"`
		TotalAmount string `json:"total_amount"`
		TradeStatus string `json:"trade_status"`
		SellerID    string `json:"seller_id"`
	}
	if json.Unmarshal(payload, &result) != nil || result.Code != "10000" {
		return Receipt{}, errors.New("Alipay has not confirmed this order")
	}
	if result.SellerID != "" && result.SellerID != a.cfg.SellerID {
		return Receipt{}, errors.New("Alipay seller mismatch")
	}
	if result.OutTradeNo != order.ID {
		return Receipt{}, ErrConflict
	}
	return receiptFrom(result.OutTradeNo, result.TradeNo, result.TotalAmount, result.TradeStatus, "alipay-rsa2-query")
}
func receiptFrom(orderID, tradeNo, amount, status, source string) (Receipt, error) {
	cents, err := ParseYuan(amount)
	if err != nil || orderID == "" {
		return Receipt{}, errors.New("invalid Alipay order amount")
	}
	mapped := "AWAITING_PAYMENT"
	switch status {
	case "TRADE_SUCCESS", "TRADE_FINISHED":
		mapped = "PAID"
	case "TRADE_CLOSED":
		mapped = "CLOSED"
	case "WAIT_BUYER_PAY":
	default:
		return Receipt{}, errors.New("unknown Alipay trade status")
	}
	return Receipt{OrderID: orderID, TradeNo: tradeNo, AmountMinor: cents, Status: mapped, VerifiedBy: source}, nil
}
