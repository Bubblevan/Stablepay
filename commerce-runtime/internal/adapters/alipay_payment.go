package adapters

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/payment"
)

// AlipayPaymentAdapter plugs CNY into the existing Runtime PaymentAdapter.
// The Runtime still owns exact-offer buyer confirmation, budget reservation,
// RuntimeGuard, finance transitions, recovery and ledger. payment-service owns
// RSA2 and settlement. All order operations go through Hertz Gateway and then
// Payment Service Kitex; no private HTTP listener exists on payment-service.
type AlipayPaymentAdapter struct {
	BaseURL, Token, MerchantDID, PayeeDID, MerchantName string
	Client                                              *http.Client
}

type alipayOrder struct {
	ID          string `json:"id"`
	RequestID   string `json:"request_id"`
	ProductID   string `json:"product_id"`
	QuoteHash   string `json:"quote_hash"`
	ApprovalRef string `json:"approval_ref"`
	AgentDID    string `json:"agent_did"`
	SkillDID    string `json:"skill_did"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
	Mode        string `json:"mode"`
	Status      string `json:"status"`
	TradeNo     string `json:"trade_no"`
	VerifiedBy  string `json:"verified_by"`
	PaidAt      string `json:"paid_at"`
}

func (a *AlipayPaymentAdapter) validate(intent payment.PaymentIntent, payee string) error {
	if err := intent.Validate(); err != nil {
		return err
	}
	if a == nil || len(a.Token) < 24 || a.MerchantDID == "" || a.PayeeDID == "" || a.MerchantName == "" {
		return errors.New("Alipay adapter is not configured")
	}
	if intent.Currency != "CNY" || intent.MerchantDID != a.MerchantDID || intent.PayeeDID != a.PayeeDID || payee != intent.PayeeDID || intent.PaymentRequirementID == "" {
		return errors.New("CNY intent does not match configured merchant and confirmed offer")
	}
	return nil
}

func (a *AlipayPaymentAdapter) call(ctx context.Context, method, path string, body, output any) error {
	u, err := url.Parse(a.BaseURL)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1" || u.Hostname() == "stablepay-api-gateway"))) {
		return errors.New("Alipay flow requires a local or HTTPS Hertz API Gateway URL")
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(a.BaseURL, "/")+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", a.Token)
	var nonceBytes [16]byte
	if _, err := rand.Read(nonceBytes[:]); err != nil {
		return err
	}
	req.Header.Set("X-StablePay-Nonce", hex.EncodeToString(nonceBytes[:]))
	req.Header.Set("X-StablePay-Timestamp", strconv.FormatInt(time.Now().UTC().UnixMilli(), 10))
	if fields, ok := body.(map[string]any); ok {
		if idempotencyKey, ok := fields["request_id"].(string); ok && idempotencyKey != "" {
			req.Header.Set("X-Idempotency-Key", idempotencyKey)
		}
	}
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if a.Client != nil {
		client = *a.Client
		if client.Timeout <= 0 {
			client.Timeout = 15 * time.Second
		}
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	response, err := client.Do(req)
	if err != nil {
		return errors.New("Alipay payment-service unavailable; reconcile before retry")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("Hertz API Gateway HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	var envelope struct {
		Code    *int            `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) == nil && len(envelope.Data) > 0 {
		if envelope.Code != nil && *envelope.Code != 0 {
			return fmt.Errorf("Hertz API Gateway: %s", envelope.Message)
		}
		return json.Unmarshal(envelope.Data, output)
	}
	return json.Unmarshal(raw, output)
}

func runtimeApproval(intent payment.PaymentIntent) string {
	return "runtime-buyer:" + intent.PaymentRequirementID
}
func runtimeRequest(intent payment.PaymentIntent) string { return "runtime:" + intent.IntentID }

func (a *AlipayPaymentAdapter) outcome(intent payment.PaymentIntent, o alipayOrder) (payment.PaymentOutcome, error) {
	if o.ID == "" || (intent.TxID != "" && intent.TxID != o.ID) || o.RequestID != runtimeRequest(intent) || o.ProductID != intent.CapabilityID || o.QuoteHash != intent.QuoteHash || o.ApprovalRef != runtimeApproval(intent) || o.AgentDID != intent.RequesterDID || o.SkillDID != canonicalPaymentSkillDID(intent.PayeeDID) || o.AmountMinor != intent.AmountMinor || o.Currency != "CNY" || o.Mode != "alipay_sandbox" {
		err := errors.New("Alipay order does not match immutable intent")
		return unknownOutcome(intent, err), err
	}
	result := payment.PaymentOutcome{IntentID: intent.IntentID, TxID: o.ID, AmountMinor: o.AmountMinor, Currency: o.Currency, Status: payment.OutcomePending}
	switch o.Status {
	case "AWAITING_PAYMENT":
	case "CLOSED":
		result.Status = payment.OutcomeFailed
		result.Reason = "ALIPAY_ORDER_CLOSED"
	case "PAID":
		if o.TradeNo == "" || (o.VerifiedBy != "alipay-rsa2-notify" && o.VerifiedBy != "alipay-rsa2-query") {
			err := errors.New("Alipay order lacks verified settlement evidence")
			return unknownOutcome(intent, err), err
		}
		result.Status, result.OccurredAt, result.Reason = payment.OutcomeConfirmed, o.PaidAt, o.VerifiedBy
	default:
		err := errors.New("unknown Alipay order state")
		return unknownOutcome(intent, err), err
	}
	return result, nil
}

func (a *AlipayPaymentAdapter) Submit(ctx context.Context, request PaymentSubmitRequest) (payment.PaymentOutcome, error) {
	i := request.Intent
	if err := a.validate(i, request.PayeeDID); err != nil {
		return unknownOutcome(i, err), err
	}
	if err := request.Authorization.Validate(); err != nil {
		return unknownOutcome(i, err), err
	}
	if !request.Authorization.Allowed || validatePaymentBinding(i, request.Authorization) != nil || request.RequestFingerprint != i.RequestFingerprint {
		err := errors.New("Alipay authorization binding mismatch")
		return unknownOutcome(i, err), err
	}
	var response struct {
		Order alipayOrder `json:"order"`
	}
	err := a.call(ctx, "POST", "/api/v1/fiat/orders", map[string]any{"request_id": runtimeRequest(i), "product_id": i.CapabilityID, "subject": "StablePay 数字服务", "quote_hash": i.QuoteHash, "approval_ref": runtimeApproval(i), "agent_did": i.RequesterDID, "skill_did": canonicalPaymentSkillDID(i.PayeeDID), "amount_minor": i.AmountMinor, "currency": "CNY"}, &response)
	if err != nil {
		return unknownOutcome(i, err), err
	}
	return a.outcome(i, response.Order)
}

func (a *AlipayPaymentAdapter) Query(ctx context.Context, request PaymentQuery) (payment.PaymentOutcome, error) {
	i := request.Intent
	if err := a.validate(i, request.PayeeDID); err != nil {
		return unknownOutcome(i, err), err
	}
	var response struct {
		Order alipayOrder `json:"order"`
	}
	err := a.call(ctx, "GET", "/api/v1/fiat/requests/"+url.PathEscape(runtimeRequest(i)), nil, &response)
	if err != nil {
		return unknownOutcome(i, err), err
	}
	order := response.Order
	if _, err = a.outcome(i, order); err != nil {
		return unknownOutcome(i, err), err
	}
	if order.Status == "AWAITING_PAYMENT" {
		var reconciled struct {
			Order alipayOrder `json:"order"`
		}
		if err = a.call(ctx, "POST", "/api/v1/fiat/orders/"+url.PathEscape(order.ID)+"/reconcile", nil, &reconciled); err != nil {
			return unknownOutcome(i, err), err
		}
		order = reconciled.Order
	}
	return a.outcome(i, order)
}

func (a *AlipayPaymentAdapter) Handoff(ctx context.Context, request PaymentQuery) (PaymentHandoff, error) {
	i := request.Intent
	if err := a.validate(i, request.PayeeDID); err != nil {
		return PaymentHandoff{}, err
	}
	var response struct {
		Order alipayOrder `json:"order"`
	}
	if err := a.call(ctx, "GET", "/api/v1/fiat/requests/"+url.PathEscape(runtimeRequest(i)), nil, &response); err != nil {
		return PaymentHandoff{}, err
	}
	order := response.Order
	if _, err := a.outcome(i, order); err != nil {
		return PaymentHandoff{}, err
	}
	if order.Status != "AWAITING_PAYMENT" {
		return PaymentHandoff{}, errors.New("Alipay order is not awaiting payment")
	}
	var checkout struct {
		Mode       string `json:"mode"`
		CashierURL string `json:"cashier_url"`
	}
	if err := a.call(ctx, "POST", "/api/v1/fiat/orders/"+url.PathEscape(order.ID)+"/checkout", nil, &checkout); err != nil {
		return PaymentHandoff{}, err
	}
	u, err := url.Parse(checkout.CashierURL)
	if err != nil || checkout.Mode != "alipay_sandbox" || u.Scheme != "https" || u.Host != "openapi-sandbox.dl.alipaydev.com" || u.Path != "/gateway.do" || u.User != nil || u.Query().Get("method") != "alipay.trade.page.pay" {
		return PaymentHandoff{}, errors.New("unexpected Alipay sandbox cashier")
	}
	var terms struct {
		OrderID string `json:"out_trade_no"`
		Amount  string `json:"total_amount"`
	}
	if json.Unmarshal([]byte(u.Query().Get("biz_content")), &terms) != nil || terms.OrderID != order.ID || terms.Amount != minorToMajor(i.AmountMinor) {
		return PaymentHandoff{}, errors.New("cashier terms differ from confirmed order")
	}
	return PaymentHandoff{IntentID: i.IntentID, OrderID: order.ID, Mode: checkout.Mode, CashierURL: checkout.CashierURL, MerchantInfo: fmt.Sprintf("%s，%s，人民币%s元（沙箱）", a.MerchantName, i.CapabilityID, minorToMajor(i.AmountMinor)), Amount: minorToMajor(i.AmountMinor)}, nil
}

// CurrencyPaymentAdapter selects an executor, while the existing Harness and
// Runtime state machine remain authoritative for every currency.
type CurrencyPaymentAdapter struct {
	Default interface {
		PaymentAdapter
		PaymentStatusAdapter
	}
	CNY                *AlipayPaymentAdapter
	DefaultEntitlement EntitlementAdapter
}

func (a *CurrencyPaymentAdapter) Submit(ctx context.Context, r PaymentSubmitRequest) (payment.PaymentOutcome, error) {
	if strings.EqualFold(strings.TrimSpace(r.Intent.Currency), "CNY") {
		return a.CNY.Submit(ctx, r)
	}
	return a.Default.Submit(ctx, r)
}
func (a *CurrencyPaymentAdapter) Query(ctx context.Context, r PaymentQuery) (payment.PaymentOutcome, error) {
	if strings.EqualFold(strings.TrimSpace(r.Intent.Currency), "CNY") {
		return a.CNY.Query(ctx, r)
	}
	return a.Default.Query(ctx, r)
}
func (a *CurrencyPaymentAdapter) Handoff(ctx context.Context, r PaymentQuery) (PaymentHandoff, error) {
	if r.Intent.Currency != "CNY" {
		return PaymentHandoff{}, errors.New("interactive handoff is only configured for CNY")
	}
	return a.CNY.Handoff(ctx, r)
}

func (a *CurrencyPaymentAdapter) Verify(ctx context.Context, q EntitlementQuery) (EntitlementResult, error) {
	if q.Currency != "CNY" {
		return a.DefaultEntitlement.Verify(ctx, q)
	}
	unknown := EntitlementResult{Status: EntitlementUnknown}
	if a.CNY == nil || q.MerchantDID != a.CNY.MerchantDID || q.PayeeDID != a.CNY.PayeeDID || q.PaymentRequirementID == "" || q.AmountMinor <= 0 {
		return unknown, errors.New("Alipay entitlement binding mismatch")
	}
	var response struct {
		Order alipayOrder `json:"order"`
	}
	if err := a.CNY.call(ctx, "GET", "/api/v1/fiat/requests/"+url.PathEscape("runtime:"+q.IntentID), nil, &response); err != nil {
		return unknown, err
	}
	o := response.Order
	if o.ID == "" || (q.TxID != "" && q.TxID != o.ID) || o.RequestID != "runtime:"+q.IntentID || o.ProductID != q.CapabilityID || o.QuoteHash != q.QuoteHash || o.ApprovalRef != "runtime-buyer:"+q.PaymentRequirementID || o.AgentDID != q.RequesterDID || o.SkillDID != canonicalPaymentSkillDID(q.PayeeDID) || o.AmountMinor != q.AmountMinor || o.Currency != "CNY" || o.Mode != "alipay_sandbox" {
		return unknown, errors.New("Alipay entitlement order mismatch")
	}
	if o.Status != "PAID" || o.TradeNo == "" || (o.VerifiedBy != "alipay-rsa2-notify" && o.VerifiedBy != "alipay-rsa2-query") {
		return unknown, nil
	}
	if a.DefaultEntitlement == nil {
		return unknown, errors.New("Verification Service entitlement adapter is required for CNY")
	}
	proof, err := a.DefaultEntitlement.Verify(ctx, q)
	if err != nil {
		return unknown, err
	}
	if proof.Status != EntitlementValid || proof.TxID != o.ID {
		return proof, nil
	}
	proof.PaymentIntentID = q.IntentID
	proof.EvidenceRef = "verification:alipay-rsa2:" + o.ID
	return proof, nil
}

var _ PaymentAdapter = (*AlipayPaymentAdapter)(nil)
var _ PaymentStatusAdapter = (*AlipayPaymentAdapter)(nil)
