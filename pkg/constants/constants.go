// Package constants 定义 Payment Service 的常量
package constants

import "time"

const (
	// 服务名
	ServiceName = "payment-service"

	// 默认分页参数
	DefaultPageSize = 20
	MaxPageSize     = 100

	// HTTP 头
	HeaderIdempotencyKey = "X-Idempotency-Key"
	HeaderRequestID      = "X-Request-ID"
	HeaderTraceID        = "X-Trace-ID"
	HeaderSignature      = "X-Signature"
	HeaderAgentDID       = "X-Agent-DID"

	// USDC/USDT 精度（小数位数）
	USDCDecimals = 6
	USDTDecimals = 6

	// 时间格式
	TimeFormatISO8601 = "2006-01-02T15:04:05Z"

	// 缓存前缀
	CachePrefixNonce       = "payment:nonce:"
	CachePrefixIdempotency = "payment:idempotency:"
	CachePrefixRateLimit   = "payment:ratelimit:"
	CachePrefixTxStatus    = "payment:tx:status:"

	// 缓存过期时间
	NonceCacheTTL       = 10 * time.Minute
	IdempotencyCacheTTL = 30 * time.Minute
	TxStatusCacheTTL    = 5 * time.Minute
)

// Currency 币种
type Currency string

const (
	CurrencyUSDC Currency = "USDC"
	CurrencyUSDT Currency = "USDT"
)

// IsValid 检查币种是否有效
func (c Currency) IsValid() bool {
	switch c {
	case CurrencyUSDC, CurrencyUSDT:
		return true
	}
	return false
}

// Decimals 获取币种精度
func (c Currency) Decimals() int {
	switch c {
	case CurrencyUSDC, CurrencyUSDT:
		return USDCDecimals
	}
	return USDCDecimals
}

// PaymentStatus 支付状态
type PaymentStatus int8

const (
	PaymentStatusCreated   PaymentStatus = 0
	PaymentStatusPending   PaymentStatus = 1
	PaymentStatusConfirmed PaymentStatus = 2
	PaymentStatusCompleted PaymentStatus = 3
	PaymentStatusFailed    PaymentStatus = 4
	PaymentStatusCancelled PaymentStatus = 5
)

// String 返回状态字符串
func (s PaymentStatus) String() string {
	switch s {
	case PaymentStatusCreated:
		return "CREATED"
	case PaymentStatusPending:
		return "PENDING"
	case PaymentStatusConfirmed:
		return "CONFIRMED"
	case PaymentStatusCompleted:
		return "COMPLETED"
	case PaymentStatusFailed:
		return "FAILED"
	case PaymentStatusCancelled:
		return "CANCELLED"
	}
	return "UNKNOWN"
}

// IsTerminal 是否为终态
func (s PaymentStatus) IsTerminal() bool {
	return s == PaymentStatusCompleted || s == PaymentStatusCancelled
}

// CanTransitionTo 检查状态是否可以转换到目标状态
func (s PaymentStatus) CanTransitionTo(target PaymentStatus) bool {
	// 终态不能再转换
	if s.IsTerminal() {
		return false
	}

	// 定义允许的状态转换
	switch s {
	case PaymentStatusCreated:
		return target == PaymentStatusPending || target == PaymentStatusFailed
	case PaymentStatusPending:
		return target == PaymentStatusConfirmed || target == PaymentStatusFailed
	case PaymentStatusConfirmed:
		return target == PaymentStatusCompleted || target == PaymentStatusFailed
	case PaymentStatusFailed:
		return target == PaymentStatusPending || target == PaymentStatusCancelled
	}
	return false
}

// MQ Topic 和 Tag 定义
const (
	MQTopicPaymentEvents = "payment_events"

	MQTagPaymentSucceeded = "payment_succeeded"
	MQTagPaymentFailed    = "payment_failed"
)
