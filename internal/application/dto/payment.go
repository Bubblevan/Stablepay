// Package dto 定义应用层数据传输对象
package dto

import (
	"github.com/stablepay/payment-service/pkg/constants"
)

// InitiatePaymentRequest 发起支付请求
// 对应 REST API: POST /api/v1/pay
type InitiatePaymentRequest struct {
	// 幂等性键（从Header X-Idempotency-Key获取）
	IdempotencyKey string `json:"-" header:"X-Idempotency-Key" binding:"required"`

	// 参与方
	AgentDID string `json:"agent_did" binding:"required"`
	SkillDID string `json:"skill_did" binding:"required"`

	// 金额（字符串格式，如 "5.00"）
	AmountStr string `json:"amount" binding:"required"`
	Currency  string `json:"currency" binding:"required,oneof=USDC USDT"`

	// 签名信息
	Signature string `json:"signature" binding:"required"`
	Timestamp int64  `json:"timestamp" binding:"required"`
	Nonce     string `json:"nonce" binding:"required"`
}

// InitiatePaymentResponse 发起支付响应
type InitiatePaymentResponse struct {
	TxID        string `json:"tx_id"`
	TxHash      string `json:"tx_hash,omitempty"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
	ConfirmedAt string `json:"confirmed_at,omitempty"`
}

// GetPaymentStatusRequest 查询支付状态请求
// 对应 REST API: GET /api/v1/pay/{tx_id}
type GetPaymentStatusRequest struct {
	TxID string `uri:"tx_id" binding:"required"`
}

// GetPaymentStatusResponse 查询支付状态响应
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

// ListPaymentHistoryRequest 查询支付历史请求
// 对应 REST API: GET /api/v1/pay/history
type ListPaymentHistoryRequest struct {
	AgentDID string `query:"agent_did" binding:"required"`
	Status   string `query:"status,omitempty"` // 可选状态过滤
	Page     int    `query:"page" binding:"min=1"`
	PageSize int    `query:"page_size" binding:"min=1,max=100"`
}

// PaymentHistoryItem 支付历史项
type PaymentHistoryItem struct {
	TxID      string `json:"tx_id"`
	SkillDID  string `json:"skill_did"`
	Amount    string `json:"amount"`
	Currency  string `json:"currency"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

// ListPaymentHistoryResponse 查询支付历史响应
type ListPaymentHistoryResponse struct {
	Items    []*PaymentHistoryItem `json:"items"`
	Total    int64                 `json:"total"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"page_size"`
}

// GetPaymentRequirementRequest 获取支付要求请求
// 对应 REST API: GET /api/v1/pay/require
type GetPaymentRequirementRequest struct {
	SkillDID string `query:"skill_did" binding:"required"`
	AgentDID string `query:"agent_did,omitempty"` // 可选，用于检查是否已购买
}

// PaymentRequirementResponse 支付要求响应（HTTP 402）
type PaymentRequirementResponse struct {
	SkillDID  string `json:"skill_did"`
	SkillName string `json:"skill_name,omitempty"`
	Price     string `json:"price"`
	Currency  string `json:"currency"`
	Endpoint  string `json:"payment_endpoint"`
}

// AlreadyPurchasedResponse 已购买响应
type AlreadyPurchasedResponse struct {
	Purchased    bool   `json:"purchased"`
	PurchaseTime string `json:"purchase_time,omitempty"`
}

// ShortLinkPayRequest 短链支付请求
// 对应 REST API: GET /pay?skill=...&price=...
type ShortLinkPayRequest struct {
	SkillDID string  `query:"skill" binding:"required"`
	Price    float64 `query:"price" binding:"required,gt=0"`
}

// ShortLinkVerifyRequest 短链验证请求
// 对应 REST API: GET /verify?skill=...&agent=...
type ShortLinkVerifyRequest struct {
	SkillDID string `query:"skill" binding:"required"`
	AgentDID string `query:"agent" binding:"required"`
}

// ShortLinkVerifyResponse 短链验证响应
type ShortLinkVerifyResponse struct {
	Purchased    bool   `json:"purchased"`
	PurchaseTime string `json:"purchase_time,omitempty"`
}

// MQPaymentEvent MQ支付事件
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

// ToMQEventTag 转换状态为MQ Tag
func ToMQEventTag(status constants.PaymentStatus) string {
	switch status {
	case constants.PaymentStatusConfirmed, constants.PaymentStatusCompleted:
		return constants.MQTagPaymentSucceeded
	case constants.PaymentStatusFailed, constants.PaymentStatusCancelled:
		return constants.MQTagPaymentFailed
	default:
		return ""
	}
}
