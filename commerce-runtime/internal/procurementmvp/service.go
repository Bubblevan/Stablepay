// Package procurementmvp composes procurement guard, buyer confirmation and
// payment-service's CNY HTTP boundary. It does not sign or verify Alipay itself.
package procurementmvp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/procurement"
)

var ErrNotFound = errors.New("记录不存在")
var ErrConflict = errors.New("报价已过期、状态已改变或请求与之前的确认冲突，请重新获取报价")

type Product struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
	Merchant    string `json:"merchant"`
}

var products = []Product{
	{ID: "budget-check-v1", Name: "活动预算核算", Description: "核算逐项金额、预算总额与差额，交付可下载的预算核算报告。", AmountMinor: 1, Currency: "CNY", Merchant: "StablePay 自营工具"},
	{ID: "procurement-review-v1", Name: "采购清单联合预审", Description: "核对预算、规格、审批记录与票据条件，交付带事实引用的预审报告。", AmountMinor: 3, Currency: "CNY", Merchant: "StablePay 自营工具"},
}

func Products() []Product { return append([]Product(nil), products...) }
func productByID(id string) (Product, error) {
	for _, p := range products {
		if p.ID == id {
			return p, nil
		}
	}
	return Product{}, ErrNotFound
}
func id() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func digest(value any) string {
	raw, _ := json.Marshal(value)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

type Plan struct {
	ID                 string            `json:"id"`
	Owner              string            `json:"owner_hash"`
	Prompt             string            `json:"prompt"`
	Input              procurement.Input `json:"input"`
	ServiceBudgetMinor int64             `json:"service_budget_minor"`
	RecommendedProduct string            `json:"recommended_product"`
	PlannerMode        string            `json:"planner_mode"`
	Explanation        string            `json:"explanation"`
	CreatedAt          time.Time         `json:"created_at"`
}
type Quote struct {
	ID        string    `json:"id"`
	Owner     string    `json:"owner_hash"`
	PlanID    string    `json:"plan_id"`
	Product   Product   `json:"product"`
	Mode      string    `json:"mode"`
	Hash      string    `json:"hash"`
	ExpiresAt time.Time `json:"expires_at"`
}
type Event struct {
	Type   string    `json:"type"`
	Detail string    `json:"detail"`
	At     time.Time `json:"at"`
}
type Delivery struct {
	Report      procurement.Report `json:"report"`
	ContentHash string             `json:"content_hash"`
	DeliveredAt time.Time          `json:"delivered_at"`
}
type Purchase struct {
	ID               string            `json:"id"`
	Owner            string            `json:"owner_hash"`
	IdempotencyKey   string            `json:"idempotency_key"`
	Quote            Quote             `json:"quote"`
	ApprovalRef      string            `json:"approval_ref"`
	ApprovedAt       time.Time         `json:"approved_at"`
	PaymentID        string            `json:"payment_id,omitempty"`
	Status           string            `json:"status"`
	TradeNo          string            `json:"trade_no,omitempty"`
	VerifiedBy       string            `json:"verified_by,omitempty"`
	Delivery         *Delivery         `json:"delivery,omitempty"`
	Events           []Event           `json:"events"`
	AgentPayAttempts []AgentPayAttempt `json:"agentpay_attempts,omitempty"`
}
type PaymentOrder struct {
	ID          string `json:"id"`
	RequestID   string `json:"request_id"`
	ProductID   string `json:"product_id"`
	QuoteHash   string `json:"quote_hash"`
	ApprovalRef string `json:"approval_ref"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
	Mode        string `json:"mode"`
	Status      string `json:"status"`
	TradeNo     string `json:"trade_no"`
	VerifiedBy  string `json:"verified_by"`
}
type PaymentClient struct {
	BaseURL, Token string
	Client         *http.Client
}

func (c PaymentClient) call(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		raw, _ := json.Marshal(input)
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	response, err := client.Do(req)
	if err != nil {
		return errors.New("支付服务暂不可用；当前订单状态可稍后重查")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("支付服务拒绝请求（HTTP %d），请检查支付环境与订单状态", response.StatusCode)
	}
	if output == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(output)
}

type Service struct {
	Store   *Store
	Payment PaymentClient
	Planner IntentPlanner
}

func (s *Service) PaymentMode(ctx context.Context) (string, error) {
	var result struct {
		Mode string `json:"mode"`
	}
	err := s.Payment.call(ctx, "GET", "/v1/fiat/status", nil, &result)
	if err != nil {
		return "", err
	}
	if result.Mode != "local" && result.Mode != "alipay_sandbox" {
		return "", errors.New("支付环境无效")
	}
	return result.Mode, nil
}
func (s *Service) CreatePlan(ctx context.Context, owner, prompt, serviceBudget string, input procurement.Input) (Plan, error) {
	if len(prompt) > 4000 {
		return Plan{}, errors.New("任务描述过长")
	}
	if err := input.Validate(); err != nil {
		return Plan{}, err
	}
	cap, err := procurement.ParseMoney(serviceBudget)
	if err != nil || cap <= 0 || cap > 10000 {
		return Plan{}, errors.New("服务费预算需为 0.01–100 元")
	}
	recommendation, mode, explanation := s.Planner.Recommend(ctx, prompt)
	plan := Plan{ID: id(), Owner: owner, Prompt: prompt, Input: input, ServiceBudgetMinor: cap, RecommendedProduct: recommendation, PlannerMode: mode, Explanation: explanation, CreatedAt: time.Now().UTC()}
	err = s.Store.transaction(func(state *state) error {
		if len(state.Plans) >= 1000 {
			return ErrCapacity
		}
		state.Plans[plan.ID] = plan
		return nil
	})
	return plan, err
}
func (s *Service) CreateQuote(ctx context.Context, owner, planID, productID string) (Quote, error) {
	product, err := productByID(productID)
	if err != nil {
		return Quote{}, err
	}
	mode, err := s.PaymentMode(ctx)
	if err != nil {
		return Quote{}, err
	}
	var quote Quote
	err = s.Store.transaction(func(state *state) error {
		if len(state.Quotes) >= 2000 {
			return ErrCapacity
		}
		plan, ok := state.Plans[planID]
		if !ok || plan.Owner != owner {
			return ErrNotFound
		}
		if product.AmountMinor > plan.ServiceBudgetMinor {
			return errors.New("服务价格超过本次授权的服务费预算")
		}
		quote = Quote{ID: id(), Owner: owner, PlanID: planID, Product: product, Mode: mode, ExpiresAt: time.Now().UTC().Add(5 * time.Minute)}
		quote.Hash = digest(quote)
		state.Quotes[quote.ID] = quote
		return nil
	})
	return quote, err
}
func (s *Service) Approve(ctx context.Context, owner, quoteID, quoteHash, key string) (Purchase, error) {
	if len(key) < 16 || len(key) > 128 {
		return Purchase{}, errors.New("缺少有效幂等键")
	}
	mode, err := s.PaymentMode(ctx)
	if err != nil {
		return Purchase{}, err
	}
	var purchase Purchase
	err = s.Store.transaction(func(state *state) error {
		for _, old := range state.Purchases {
			if old.Owner == owner && old.IdempotencyKey == key {
				if old.Quote.ID != quoteID || old.Quote.Hash != quoteHash {
					return ErrConflict
				}
				purchase = old
				return nil
			}
		}
		quote, ok := state.Quotes[quoteID]
		if !ok || quote.Owner != owner {
			return ErrNotFound
		}
		if quote.Hash != quoteHash || !quote.ExpiresAt.After(time.Now()) || quote.Mode != mode {
			return ErrConflict
		}
		product, err := productByID(quote.Product.ID)
		if err != nil || product.AmountMinor != quote.Product.AmountMinor {
			return ErrConflict
		}
		for _, old := range state.Purchases {
			if old.Owner == owner && old.Quote.ID == quote.ID {
				purchase = old
				return nil
			}
		}
		now := time.Now().UTC()
		if len(state.Purchases) >= 500 {
			return ErrCapacity
		}
		purchase = Purchase{ID: id(), Owner: owner, IdempotencyKey: key, Quote: quote, ApprovalRef: "approval_" + id(), ApprovedAt: now, Status: "AWAITING_PAYMENT", Events: []Event{{Type: "BUYER_APPROVED", Detail: "用户确认了商品、商户、CNY 金额及当前报价", At: now}}}
		state.Purchases[purchase.ID] = purchase
		return nil
	})
	if err != nil {
		return Purchase{}, err
	}
	return s.ensurePayment(ctx, purchase)
}
func (s *Service) Get(owner, purchaseID string) (Purchase, error) {
	var purchase Purchase
	err := s.Store.transaction(func(state *state) error {
		var ok bool
		purchase, ok = state.Purchases[purchaseID]
		if !ok || purchase.Owner != owner {
			return ErrNotFound
		}
		return nil
	})
	return purchase, err
}
func (s *Service) List(owner string) ([]Purchase, error) {
	result := []Purchase{}
	err := s.Store.transaction(func(state *state) error {
		for _, p := range state.Purchases {
			if p.Owner == owner {
				result = append(result, p)
			}
		}
		return nil
	})
	sort.Slice(result, func(i, j int) bool { return result[i].ApprovedAt.After(result[j].ApprovedAt) })
	if len(result) > 20 {
		result = result[:20]
	}
	return result, err
}
func (s *Service) ensurePayment(ctx context.Context, purchase Purchase) (Purchase, error) {
	currentMode, modeErr := s.PaymentMode(ctx)
	if modeErr != nil {
		return purchase, modeErr
	}
	if currentMode != purchase.Quote.Mode {
		return purchase, ErrConflict
	}
	if purchase.PaymentID != "" {
		return purchase, nil
	}
	var order PaymentOrder
	err := s.Payment.call(ctx, "POST", "/v1/fiat/orders", map[string]any{"request_id": purchase.ID, "product_id": purchase.Quote.Product.ID, "subject": purchase.Quote.Product.Name, "quote_hash": purchase.Quote.Hash, "approval_ref": purchase.ApprovalRef, "amount_minor": purchase.Quote.Product.AmountMinor, "currency": "CNY"}, &order)
	if err != nil {
		return purchase, err
	}
	if !matches(purchase, order) {
		return purchase, ErrConflict
	}
	err = s.Store.transaction(func(state *state) error {
		current := state.Purchases[purchase.ID]
		if current.PaymentID == "" {
			current.PaymentID = order.ID
			current.Events = append(current.Events, Event{Type: "PAYMENT_CREATED", Detail: "payment-service 已固定订单与金额", At: time.Now().UTC()})
			state.Purchases[current.ID] = current
		}
		purchase = state.Purchases[purchase.ID]
		return nil
	})
	return purchase, err
}
func matches(p Purchase, o PaymentOrder) bool {
	return o.ID != "" && o.RequestID == p.ID && o.ProductID == p.Quote.Product.ID && o.AmountMinor == p.Quote.Product.AmountMinor && o.Currency == "CNY" && o.Mode == p.Quote.Mode && o.QuoteHash == p.Quote.Hash && o.ApprovalRef == p.ApprovalRef
}
func (s *Service) Checkout(ctx context.Context, owner, purchaseID string) (map[string]string, error) {
	p, err := s.Get(owner, purchaseID)
	if err != nil {
		return nil, err
	}
	p, err = s.ensurePayment(ctx, p)
	if err != nil {
		return nil, err
	}
	if p.Status != "AWAITING_PAYMENT" {
		return nil, ErrConflict
	}
	var result map[string]string
	err = s.Payment.call(ctx, "POST", "/v1/fiat/orders/"+p.PaymentID+"/checkout", nil, &result)
	return result, err
}
func (s *Service) Reconcile(ctx context.Context, owner, purchaseID string, simulate bool) (Purchase, error) {
	p, err := s.Get(owner, purchaseID)
	if err != nil {
		return Purchase{}, err
	}
	p, err = s.ensurePayment(ctx, p)
	if err != nil {
		return p, err
	}
	action := "reconcile"
	if simulate {
		if p.Quote.Mode != "local" {
			return p, errors.New("当前环境不允许本地模拟结算")
		}
		action = "simulate"
	}
	var order PaymentOrder
	err = s.Payment.call(ctx, "POST", "/v1/fiat/orders/"+p.PaymentID+"/"+action, nil, &order)
	if err != nil {
		return p, err
	}
	if !matches(p, order) {
		return p, ErrConflict
	}
	err = s.Store.transaction(func(state *state) error {
		current := state.Purchases[p.ID]
		if current.Delivery != nil {
			p = current
			return nil
		}
		if order.Status == "CLOSED" {
			current.Status = "CLOSED"
			state.Purchases[p.ID] = current
			p = current
			return nil
		}
		if order.Status != "PAID" {
			p = current
			return nil
		}
		if order.TradeNo == "" || order.VerifiedBy == "" {
			return ErrConflict
		}
		plan, ok := state.Plans[current.Quote.PlanID]
		if !ok || plan.Owner != owner {
			return ErrNotFound
		}
		report, err := procurement.Audit(plan.Input, current.Quote.Product.ID == "budget-check-v1")
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		current.TradeNo = order.TradeNo
		current.VerifiedBy = order.VerifiedBy
		current.Status = "DELIVERED"
		current.Delivery = &Delivery{Report: report, ContentHash: digest(report), DeliveredAt: now}
		current.Events = append(current.Events, Event{Type: "PAYMENT_VERIFIED", Detail: order.VerifiedBy, At: now}, Event{Type: "DELIVERED", Detail: "采购报告已生成并记录内容摘要", At: now})
		state.Purchases[p.ID] = current
		p = current
		return nil
	})
	return p, err
}
