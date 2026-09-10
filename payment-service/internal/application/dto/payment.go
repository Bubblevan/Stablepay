package dto

import (
	"fmt"
	"strings"
	"time"

	"github.com/stablepay/payment-service/pkg/constants"
)

type InitiatePaymentRequest struct {
	IdempotencyKey string `json:"-" header:"X-Idempotency-Key" binding:"required"`
	RequestID      string `json:"-"`
	TraceID        string `json:"-"`
	AgentDID       string `json:"agent_did" binding:"required"`
	SkillDID       string `json:"skill_did" binding:"required"`
	AmountStr      string `json:"amount" binding:"required"`
	Currency       string `json:"currency" binding:"required,oneof=USDC USDT"`
	Signature      string `json:"signature" binding:"required"`
	Timestamp      int64  `json:"timestamp" binding:"required"`
	Nonce          string `json:"nonce" binding:"required"`
	// Partially-signed SPL tx (base64); hot wallet only adds fee-payer signature. Required for agent/OWS flows.
	SignedTxBase64 string `json:"signed_tx_base64"`
	// IntentID is required only when agent_harness.require_intent_for_payment is enabled.
	// It binds a payment to the server-side policy decision made before execution.
	IntentID string `json:"intent_id,omitempty"`
}

// CreatePaymentIntentRequest is the agent's proposed spend. It deliberately has
// no transaction payload: an agent may propose, but it cannot execute until the
// deterministic policy and (where needed) a DID-signed user approval pass.
type CreatePaymentIntentRequest struct {
	AgentDID    string `json:"agent_did" binding:"required"`
	SkillDID    string `json:"skill_did" binding:"required"`
	AmountStr   string `json:"amount" binding:"required"`
	Currency    string `json:"currency" binding:"required,oneof=USDC USDT"`
	Purpose     string `json:"purpose" binding:"required,max=512"`
	ResourceURI string `json:"resource_uri,omitempty,max=1024"`
}

type CreatePaymentIntentResponse struct {
	IntentID        string   `json:"intent_id"`
	Status          string   `json:"status"`
	Decision        string   `json:"decision"`
	ReasonCodes     []string `json:"reason_codes"`
	PolicyVersion   string   `json:"policy_version"`
	ExpiresAt       string   `json:"expires_at"`
	ApprovalPayload string   `json:"approval_payload,omitempty"`
	Trace           []string `json:"trace"`
}

// ApprovePaymentIntentRequest must be signed by the same DID that owns the
// intent. The signature covers the immutable intent fields rather than a free
// form chat reply, so an approval cannot be replayed for another merchant.
type ApprovePaymentIntentRequest struct {
	IntentID  string `uri:"intent_id" binding:"required"`
	AgentDID  string `json:"agent_did" binding:"required"`
	Signature string `json:"signature" binding:"required"`
	Timestamp int64  `json:"timestamp" binding:"required"`
	Nonce     string `json:"nonce" binding:"required"`
}

type ApprovePaymentIntentResponse struct {
	IntentID      string `json:"intent_id"`
	Status        string `json:"status"`
	Decision      string `json:"decision"`
	PolicyVersion string `json:"policy_version"`
	ExpiresAt     string `json:"expires_at"`
}

type InitiatePaymentResponse struct {
	TxID        string `json:"tx_id"`
	TxHash      string `json:"tx_hash,omitempty"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
	ConfirmedAt string `json:"confirmed_at,omitempty"`
}

type GetPaymentStatusRequest struct {
	TxID string `uri:"tx_id" binding:"required"`
}

type GetPaymentStatusResponse struct {
	TxID        string `json:"tx_id"`
	AgentDID    string `json:"agent_did"`
	SkillDID    string `json:"skill_did"`
	Amount      string `json:"amount"`
	Currency    string `json:"currency"`
	Status      string `json:"status"`
	TxHash      string `json:"tx_hash,omitempty"`
	CreatedAt   string `json:"created_at"`
	ConfirmedAt string `json:"confirmed_at,omitempty"`
	FailedAt    string `json:"failed_at,omitempty"`
}

type ListPaymentHistoryRequest struct {
	AgentDID string `query:"agent_did" binding:"required"`
	Status   string `query:"status,omitempty"`
	Page     int    `query:"page" binding:"min=1"`
	PageSize int    `query:"page_size" binding:"min=1,max=100"`
}

type PaymentHistoryItem struct {
	TxID      string `json:"tx_id"`
	SkillDID  string `json:"skill_did"`
	Amount    string `json:"amount"`
	Currency  string `json:"currency"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

type ListPaymentHistoryResponse struct {
	Items    []*PaymentHistoryItem `json:"items"`
	Total    int64                 `json:"total"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"page_size"`
}

type GetPaymentRequirementRequest struct {
	SkillDID  string `query:"skill_did" binding:"required"`
	AgentDID  string `query:"agent_did,omitempty"`
	SkillName string `query:"skill_name,omitempty"`
	Amount    string `query:"amount,omitempty"` // 优先使用 amount
	Price     string `query:"price,omitempty"`  // 向后兼容
	Currency  string `query:"currency,omitempty" binding:"omitempty,oneof=USDC USDT"`
	Message   string `query:"message,omitempty"`
}

type PaymentRequirementResponse struct {
	SkillDID  string `json:"skill_did"`
	SkillName string `json:"skill_name,omitempty"`
	Price     string `json:"price"`
	Currency  string `json:"currency"`
	Message   string `json:"message"`
	Endpoint  string `json:"payment_endpoint"`
}

type AlreadyPurchasedResponse struct {
	Purchased    bool   `json:"purchased"`
	PurchaseTime string `json:"purchase_time,omitempty"`
}

type ShortLinkPayRequest struct {
	SkillDID string  `query:"skill" binding:"required"`
	Price    float64 `query:"price" binding:"required,gt=0"`
}

type ShortLinkVerifyRequest struct {
	SkillDID string `query:"skill" binding:"required"`
	AgentDID string `query:"agent" binding:"required"`
}

type ShortLinkVerifyResponse struct {
	Purchased    bool   `json:"purchased"`
	PurchaseTime string `json:"purchase_time,omitempty"`
}

type MQPaymentEvent struct {
	EventID        string `json:"event_id"`
	EventType      string `json:"event_type"`
	SchemaVersion  int    `json:"schema_version"`
	IdempotencyKey string `json:"idempotency_key"`
	TxID           string `json:"tx_id"`
	AgentDID       string `json:"agent_did"`
	SkillDID       string `json:"skill_did"`
	AmountMinor    int64  `json:"amount_minor"`
	Currency       string `json:"currency"`
	TxHash         string `json:"tx_hash,omitempty"`
	Status         string `json:"status"`
	OccurredAt     string `json:"occurred_at"`
	ConfirmedAt    string `json:"confirmed_at,omitempty"`
	Timestamp      int64  `json:"timestamp,omitempty"` // legacy alias; consumers use occurred_at first
	RequestID      string `json:"request_id,omitempty"`
	TraceID        string `json:"trace_id,omitempty"`
	ErrorCode      string `json:"error_code,omitempty"`
	ErrorMsg       string `json:"error_msg,omitempty"`

	// 扩展字段(可选,reward 场景使用)
	RewardPurpose string `json:"reward_purpose,omitempty"` // 如 "x_registration_reward"
	FromWallet    string `json:"from_wallet,omitempty"`    // 奖励场景下记录 treasury 钱包
	ToWallet      string `json:"to_wallet,omitempty"`      // 奖励场景下记录用户钱包
}

// Validate checks the canonical payment event envelope before it is published.
// The JSON field names are mirrored by verification-service and the IDL examples.
func (e *MQPaymentEvent) Validate() error {
	if e == nil {
		return fmt.Errorf("payment event is nil")
	}
	if e.EventID == "" || e.EventType == "" || e.SchemaVersion != 1 || e.IdempotencyKey == "" {
		return fmt.Errorf("payment event envelope is incomplete")
	}
	if e.EventType != "payment.success" && e.EventType != "payment.failed" {
		return fmt.Errorf("unsupported payment event type: %s", e.EventType)
	}
	if e.TxID == "" || e.AgentDID == "" || e.SkillDID == "" || e.AmountMinor <= 0 {
		return fmt.Errorf("payment event business identity is incomplete")
	}
	if currency := strings.ToUpper(strings.TrimSpace(e.Currency)); currency != "USDC" && currency != "USDT" {
		return fmt.Errorf("unsupported payment event currency: %s", e.Currency)
	}
	if _, err := time.Parse(time.RFC3339, e.OccurredAt); err != nil {
		return fmt.Errorf("invalid payment event occurred_at: %w", err)
	}
	return nil
}

// XRegistrationRewardRequest verification-service -> payment-service 的内部奖励请求。
// 由 X 验证流程触发,amount 默认 1 USDC,treasury -> 用户钱包,无 payer 签名。
type XRegistrationRewardRequest struct {
	IdempotencyKey string `json:"-" header:"X-Idempotency-Key" binding:"required"`
	AgentDID       string `json:"agent_did" binding:"required"`
	WalletAddress  string `json:"wallet_address" binding:"required"`
	TweetID        string `json:"tweet_id" binding:"required"`
	XHandle        string `json:"x_handle" binding:"required"`
	AmountStr      string `json:"amount" binding:"required"`
	Currency       string `json:"currency" binding:"required,oneof=USDC"`
	Reason         string `json:"reason,omitempty"` // 默认 "x_registration_reward"
}

// XRegistrationRewardResponse payment-service -> verification-service
type XRegistrationRewardResponse struct {
	TxID            string `json:"tx_id"`
	TxHash          string `json:"tx_hash,omitempty"`
	Status          string `json:"status"`
	Amount          string `json:"amount"`
	Currency        string `json:"currency"`
	AgentDID        string `json:"agent_did"`
	RecipientWallet string `json:"recipient_wallet"`
	FromWallet      string `json:"from_wallet"`
	CreatedAt       string `json:"created_at"`
	ConfirmedAt     string `json:"confirmed_at,omitempty"`
	IdempotencyKey  string `json:"idempotency_key"`
	AlreadyPaid     bool   `json:"already_paid"`
}

func ToMQEventTag(status int8) string {
	switch status {
	case constants.PaymentStatusConfirmed, constants.PaymentStatusCompleted:
		return constants.MQTagPaymentSucceeded
	case constants.PaymentStatusFailed, constants.PaymentStatusCancelled:
		return constants.MQTagPaymentFailed
	default:
		return ""
	}
}
