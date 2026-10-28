package procurementmvp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"time"

	"github.com/stablepay/payment-service/pkg/agentharness"
)

// These are tool invocation records on the existing purchase, not another
// payment state machine. Client tool reports never establish PAID/DELIVERED.
type AgentPayAttempt struct {
	ID           string    `json:"id"`
	Stage        string    `json:"stage"`
	Status       string    `json:"status"`
	LinkDigest   string    `json:"link_digest,omitempty"`
	OutputDigest string    `json:"output_digest,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type AgentPayHandoff struct {
	PurchaseID   string                `json:"purchase_id"`
	QuoteHash    string                `json:"quote_hash"`
	ApprovalRef  string                `json:"approval_ref"`
	PaymentMode  string                `json:"payment_mode"`
	CashierURL   string                `json:"cashier_url"`
	MerchantInfo string                `json:"merchant_info"`
	Amount       string                `json:"amount"`
	Policy       agentharness.Decision `json:"harness_policy"`
}

var toolDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (s *Service) AgentPayHandoff(ctx context.Context, owner, purchaseID string) (AgentPayHandoff, error) {
	p, err := s.Get(owner, purchaseID)
	if err != nil {
		return AgentPayHandoff{}, err
	}
	if p.Status != "AWAITING_PAYMENT" || p.Quote.Mode != "alipay_sandbox" || p.ApprovalRef == "" || p.ApprovedAt.IsZero() {
		return AgentPayHandoff{}, errors.New("Agent Pay需要已确认的支付宝沙箱待付款订单")
	}
	var plan Plan
	err = s.Store.transaction(func(st *state) error {
		var ok bool
		plan, ok = st.Plans[p.Quote.PlanID]
		if !ok || plan.Owner != owner {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return AgentPayHandoff{}, err
	}
	policy := agentharness.Policy{Version: "agent-payment-cny-v1", MaxAmountMinor: plan.ServiceBudgetMinor, AllowedMerchants: []string{"StablePay 自营工具"}}
	result := policy.Evaluate(p.Quote.Product.Merchant, p.Quote.Product.AmountMinor)
	if result.Decision == "DENY" {
		return AgentPayHandoff{}, errors.New("Agent Harness拒绝当前商户或服务费")
	}
	// Human acceptance is the existing authenticated purchase approval. It is
	// neither a DID signature nor the eventual Alipay payment confirmation.
	result.Status, result.Decision = "APPROVED", "ALLOW"
	result.ReasonCodes = append(result.ReasonCodes, "EXACT_QUOTE_BUYER_CONFIRMED")
	checkout, err := s.Checkout(ctx, owner, purchaseID)
	if err != nil {
		return AgentPayHandoff{}, err
	}
	u, err := url.Parse(checkout["cashier_url"])
	if err != nil || checkout["mode"] != "alipay_sandbox" || u.Scheme != "https" || u.Host != "openapi-sandbox.dl.alipaydev.com" || u.User != nil || u.Query().Get("method") != "alipay.trade.page.pay" {
		return AgentPayHandoff{}, errors.New("支付宝收银台环境不匹配")
	}
	amount := fmt.Sprintf("%d.%02d", p.Quote.Product.AmountMinor/100, p.Quote.Product.AmountMinor%100)
	return AgentPayHandoff{PurchaseID: p.ID, QuoteHash: p.Quote.Hash, ApprovalRef: p.ApprovalRef, PaymentMode: p.Quote.Mode, CashierURL: checkout["cashier_url"], MerchantInfo: fmt.Sprintf("%s，%s，人民币%s元（沙箱）", p.Quote.Product.Merchant, p.Quote.Product.Name, amount), Amount: amount, Policy: result}, nil
}

func (s *Service) ClaimAgentPay(owner, purchaseID, stage, linkDigest string) (AgentPayAttempt, error) {
	var result AgentPayAttempt
	if stage != "signal" && stage != "submit" {
		return result, errors.New("未知支付宝工具阶段")
	}
	err := s.Store.transaction(func(st *state) error {
		p, ok := st.Purchases[purchaseID]
		if !ok || p.Owner != owner {
			return ErrNotFound
		}
		if p.Quote.Mode != "alipay_sandbox" || p.Status != "AWAITING_PAYMENT" || p.ApprovalRef == "" || time.Now().After(p.ApprovedAt.Add(15*time.Minute)) {
			return ErrConflict
		}
		signalReady := false
		for _, attempt := range p.AgentPayAttempts {
			if attempt.Stage == stage {
				return errors.New("该工具阶段已有执行记录；结果不明时先查询订单，禁止自动重放")
			}
			if attempt.Stage == "signal" && attempt.Status == "CLIENT_REPORTED_SUCCESS" && attempt.LinkDigest == linkDigest && toolDigestPattern.MatchString(linkDigest) {
				signalReady = true
			}
		}
		if stage == "submit" && !signalReady {
			return errors.New("缺少同一订单支付短链的工具记录")
		}
		result = AgentPayAttempt{ID: id(), Stage: stage, Status: "STARTED", CreatedAt: time.Now().UTC(), LinkDigest: linkDigest}
		p.AgentPayAttempts = append(p.AgentPayAttempts, result)
		p.Events = append(p.Events, Event{Type: "AGENTPAY_TOOL_STARTED", Detail: stage + ":" + result.ID, At: result.CreatedAt})
		st.Purchases[p.ID] = p
		return nil
	})
	return result, err
}

func (s *Service) RecordAgentPay(owner, purchaseID, attemptID, status, outputDigest, linkDigest string) (AgentPayAttempt, error) {
	var result AgentPayAttempt
	if (status != "CLIENT_REPORTED_SUCCESS" && status != "UNKNOWN") || !toolDigestPattern.MatchString(outputDigest) {
		return result, errors.New("支付宝工具记录格式无效")
	}
	err := s.Store.transaction(func(st *state) error {
		p, ok := st.Purchases[purchaseID]
		if !ok || p.Owner != owner {
			return ErrNotFound
		}
		for i, attempt := range p.AgentPayAttempts {
			if attempt.ID != attemptID {
				continue
			}
			if attempt.Status != "STARTED" {
				if attempt.Status == status && attempt.OutputDigest == outputDigest && attempt.LinkDigest == linkDigest {
					result = attempt
					return nil
				}
				return ErrConflict
			}
			if status == "CLIENT_REPORTED_SUCCESS" && !toolDigestPattern.MatchString(linkDigest) {
				return errors.New("缺少支付链接摘要")
			}
			if attempt.Stage == "submit" && attempt.LinkDigest != linkDigest {
				return ErrConflict
			}
			attempt.Status, attempt.OutputDigest, attempt.LinkDigest = status, outputDigest, linkDigest
			p.AgentPayAttempts[i] = attempt
			p.Events = append(p.Events, Event{Type: "AGENTPAY_CLIENT_TOOL_REPORT", Detail: attempt.Stage + ":" + status + "；不是付款凭证", At: time.Now().UTC()})
			st.Purchases[p.ID] = p
			result = attempt
			return nil
		}
		return ErrNotFound
	})
	return result, err
}
