// Package entity 定义领域实体
package entity

import (
	"time"

	"github.com/stablepay/payment-service/pkg/constants"
	"github.com/stablepay/payment-service/pkg/errors"
)

// Payment 支付聚合根
// 代表一次完整的支付交易生命周期
type Payment struct {
	// 基础标识
	ID     uint64 `gorm:"primaryKey"`
	TxID   string `gorm:"column:tx_id;type:varchar(64);uniqueIndex;not null"`

	// 参与方
	AgentDID string `gorm:"column:agent_did;type:varchar(128);index;not null"`
	SkillDID string `gorm:"column:skill_did;type:varchar(128);index;not null"`

	// 金额信息
	AmountMinor int64              `gorm:"column:amount;not null"`
	Currency    constants.Currency `gorm:"column:currency;type:tinyint;not null;default:1"`

	// 签名信息
	Signature string `gorm:"column:signature;type:varchar(512);not null"`
	SignTime  int64  `gorm:"column:sign_timestamp;not null"`
	SignNonce string `gorm:"column:sign_nonce;type:varchar(64);uniqueIndex;not null"`

	// 链上信息
	TxHash string `gorm:"column:tx_hash;type:varchar(128);index"`

	// 状态信息（使用 int8 存储）
	Status      int8   `gorm:"column:status;type:tinyint;not null;default:0"`
	RetryCount  int8   `gorm:"column:retry_count;type:tinyint;not null;default:0"`
	ErrorCode   string `gorm:"column:error_code;type:varchar(32)"`
	ErrorMsg    string `gorm:"column:error_message;type:varchar(512)"`

	// 时间戳
	CreatedAt   time.Time  `gorm:"column:created_at;not null"`
	UpdatedAt   time.Time  `gorm:"column:updated_at;not null"`
	ConfirmedAt *time.Time `gorm:"column:confirmed_at"`
	ExpiresAt   time.Time  `gorm:"column:expires_at;not null"`
}

// TableName 指定表名
func (Payment) TableName() string {
	return "payment_transactions"
}

// NewPayment 创建新的支付实体
func NewPayment(txID, agentDID, skillDID string, amountMinor int64, currency constants.Currency,
	signature string, signTime int64, signNonce string, timeoutMinutes int) (*Payment, error) {

	now := time.Now()

	return &Payment{
		TxID:        txID,
		AgentDID:    agentDID,
		SkillDID:    skillDID,
		AmountMinor: amountMinor,
		Currency:    currency,
		Signature:   signature,
		SignTime:    signTime,
		SignNonce:   signNonce,
		Status:      constants.PaymentStatusCreated,
		RetryCount:  0,
		CreatedAt:   now,
		UpdatedAt:   now,
		ExpiresAt:   now.Add(time.Duration(timeoutMinutes) * time.Minute),
	}, nil
}

// TransitionTo 状态转换
// 执行状态转换并验证转换的合法性
func (p *Payment) TransitionTo(newStatus int8) error {
	if !constants.CanTransitionTo(p.Status, newStatus) {
		return errors.Newf(errors.INVALID_PARAMETERS,
			"invalid status transition from %s to %s",
			constants.PaymentStatusToString(p.Status),
			constants.PaymentStatusToString(newStatus))
	}

	oldStatus := p.Status
	p.Status = newStatus
	p.UpdatedAt = time.Now()

	// 记录确认时间
	if newStatus == constants.PaymentStatusConfirmed && p.ConfirmedAt == nil {
		now := time.Now()
		p.ConfirmedAt = &now
	}

	// 可以在这里添加领域事件发布
	_ = oldStatus // 可用于事件记录

	return nil
}

// MarkAsPending 标记为交易中
func (p *Payment) MarkAsPending(txHash string) error {
	p.TxHash = txHash
	return p.TransitionTo(constants.PaymentStatusPending)
}

// MarkAsConfirmed 标记为已确认
func (p *Payment) MarkAsConfirmed() error {
	return p.TransitionTo(constants.PaymentStatusConfirmed)
}

// MarkAsCompleted 标记为已完成
func (p *Payment) MarkAsCompleted() error {
	return p.TransitionTo(constants.PaymentStatusCompleted)
}

// MarkAsFailed 标记为失败
func (p *Payment) MarkAsFailed(errCode, errMsg string) error {
	p.ErrorCode = errCode
	p.ErrorMsg = errMsg
	return p.TransitionTo(constants.PaymentStatusFailed)
}

// MarkAsCancelled 标记为已取消
func (p *Payment) MarkAsCancelled() error {
	return p.TransitionTo(constants.PaymentStatusCancelled)
}

// CanRetry 检查是否可以重试
func (p *Payment) CanRetry(maxRetryCount int) bool {
	if p.Status != constants.PaymentStatusFailed {
		return false
	}
	return int(p.RetryCount) < maxRetryCount
}

// IncrementRetryCount 增加重试计数
func (p *Payment) IncrementRetryCount() {
	p.RetryCount++
	p.UpdatedAt = time.Now()
}

// IsExpired 检查支付是否已超时
func (p *Payment) IsExpired() bool {
	return time.Now().After(p.ExpiresAt)
}

// IsTerminal 检查是否已处于终态
func (p *Payment) IsTerminal() bool {
	return constants.IsTerminalStatus(p.Status)
}

// GetIdempotencyKey 获取幂等性键
// 用于幂等性控制：agent_did + skill_did + nonce
func (p *Payment) GetIdempotencyKey() string {
	return p.AgentDID + ":" + p.SkillDID + ":" + p.SignNonce
}

// GetSignData 获取待签名的数据
// 签名内容格式: agent_did|skill_did|amount|currency|timestamp|nonce
func (p *Payment) GetSignData() string {
	return p.AgentDID + "|" + p.SkillDID + "|" +
		string(rune(p.AmountMinor)) + "|" + string(p.Currency) + "|" +
		string(rune(p.SignTime)) + "|" + p.SignNonce
}

// PaymentIdempotency 幂等性记录实体
type PaymentIdempotency struct {
	ID             uint64    `gorm:"primaryKey"`
	IdempotencyKey string    `gorm:"column:idempotency_key;type:varchar(128);uniqueIndex;not null"`
	TxID           *string   `gorm:"column:tx_id;type:varchar(64)"`
	Status         int8      `gorm:"column:status;type:tinyint;not null;default:0"` // 0=PENDING, 1=COMPLETED, 2=FAILED
	RequestHash    string    `gorm:"column:request_hash;type:varchar(64);not null"`
	ResponseData   string    `gorm:"column:response_data;type:text"`
	ExpiresAt      time.Time `gorm:"column:expires_at;not null;index"`
	CreatedAt      time.Time `gorm:"column:created_at;not null"`
}

// TableName 指定表名
func (PaymentIdempotency) TableName() string {
	return "payment_idempotency_keys"
}

// BlockchainCallback 链上回调记录实体
type BlockchainCallback struct {
	ID           uint64     `gorm:"primaryKey"`
	TxHash       string     `gorm:"column:tx_hash;type:varchar(128);not null"`
	TxID         string     `gorm:"column:tx_id;type:varchar(64);index;not null"`
	CallbackType string     `gorm:"column:callback_type;type:varchar(32);not null"` // confirmation/failure
	CallbackData string     `gorm:"column:callback_data;type:json;not null"`
	Processed    bool       `gorm:"column:processed;type:tinyint;not null;default:0;index"`
	CreatedAt    time.Time  `gorm:"column:created_at;not null"`
	ProcessedAt  *time.Time `gorm:"column:processed_at"`
}

// TableName 指定表名
func (BlockchainCallback) TableName() string {
	return "blockchain_callbacks"
}

// MarkAsProcessed 标记为已处理
func (c *BlockchainCallback) MarkAsProcessed() {
	now := time.Now()
	c.Processed = true
	c.ProcessedAt = &now
}
