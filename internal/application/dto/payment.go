package dto

import "github.com/stablepay/payment-service/pkg/constants"

type InitiatePaymentRequest struct {
	IdempotencyKey string `json:"-" header:"X-Idempotency-Key" binding:"required"`
	AgentDID       string `json:"agent_did" binding:"required"`
	SkillDID       string `json:"skill_did" binding:"required"`
	AmountStr      string `json:"amount" binding:"required"`
	Currency       string `json:"currency" binding:"required,oneof=USDC USDT"`
	Signature      string `json:"signature" binding:"required"`
	Timestamp      int64  `json:"timestamp" binding:"required"`
	Nonce          string `json:"nonce" binding:"required"`
	// Partially-signed SPL tx (base64); hot wallet only adds fee-payer signature. Required for agent/OWS flows.
	SignedTxBase64 string `json:"signed_tx_base64"`
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
	TxID        string `json:"tx_id"`
	AgentDID    string `json:"agent_did"`
	SkillDID    string `json:"skill_did"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
	TxHash      string `json:"tx_hash,omitempty"`
	Status      string `json:"status"`
	Timestamp   int64  `json:"timestamp"`
	ErrorCode   string `json:"error_code,omitempty"`
	ErrorMsg    string `json:"error_msg,omitempty"`
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
