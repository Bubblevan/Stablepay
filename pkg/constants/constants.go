// Package constants 定义 Payment Service 的常量
package constants

import (
	"time"

	"github.com/stablepay/payment-service/pkg/common"
)

// 从 common 包导入的类型
type (
	// Currency 币种类型
	Currency = common.Currency
	// PaymentStatus 支付状态类型（内部扩展）
	PaymentStatus = common.PaymentStatus
)

// 币种常量
const (
	CurrencyUSDC Currency = 1
	CurrencyUSDT Currency = 2
)

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
	HeaderInternalAPIKey = "X-Internal-Api-Key"
	HeaderSourceService  = "X-Source-Service"

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

	// 签名有效期（分钟）
	SignatureTTLMinutes = 10
	NonceCacheMinutes   = 10
)

// MQ Topic 和 Tag 定义
const (
	MQTopicPaymentEvents = "payment_events"

	MQTagPaymentSucceeded = "payment_succeeded"
	MQTagPaymentFailed    = "payment_failed"
	MQTagRewardGranted    = "reward_granted"
)

// 奖励相关常量
const (
	// SkillDidXRegistrationReward X 注册奖励写入 Payment.SkillDID 的哨兵值。
	// 真实场景下没有 skill_did 概念,沿用 varchar(128) 字段记录此标记以便筛选。
	SkillDidXRegistrationReward = "did:solana:reward:x-registration"

	// SignatureInternalReward 内部奖励写入 Payment.Signature 的哨兵值。
	// 没有真实签名,仅作占位让 NOT NULL 约束通过。
	SignatureInternalReward = "INTERNAL_TREASURY_REWARD"

	// RewardPurposeXRegistration X 注册奖励的 reason / event_type 值。
	RewardPurposeXRegistration = "x_registration_reward"
)

// MaxAmount 最大支付金额（1000 USDC）
const MaxAmount = "1000.00"

// 支付状态扩展（本地定义，补充 common 包中没有的状态）
const (
	PaymentStatusCreated   int8 = 0
	PaymentStatusPending   int8 = 1
	PaymentStatusConfirmed int8 = 2
	PaymentStatusCompleted int8 = 3
	PaymentStatusFailed    int8 = 4
	PaymentStatusCancelled int8 = 5
)

// PaymentStatusEx 扩展支付状态结构
type PaymentStatusEx struct {
	Value int8
}

// String 返回状态字符串
func PaymentStatusToString(s int8) string {
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
func IsTerminalStatus(s int8) bool {
	return s == PaymentStatusCompleted || s == PaymentStatusCancelled
}

// CanTransitionTo 检查状态是否可以转换到目标状态
func CanTransitionTo(current, target int8) bool {
	// 终态不能再转换
	if IsTerminalStatus(current) {
		return false
	}

	// 定义允许的状态转换
	switch current {
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
