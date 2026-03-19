// Package repository 定义领域层仓库接口
package repository

import (
	"context"
	"time"

	"github.com/stablepay/payment-service/internal/domain/entity"
)

// PaymentRepository 支付记录仓库接口
type PaymentRepository interface {
	// Create 创建支付记录
	Create(ctx context.Context, payment *entity.Payment) error

	// GetByTxID 根据交易ID查询
	GetByTxID(ctx context.Context, txID string) (*entity.Payment, error)

	// GetByTxHash 根据链上交易哈希查询
	GetByTxHash(ctx context.Context, txHash string) (*entity.Payment, error)

	// Update 更新支付记录
	Update(ctx context.Context, payment *entity.Payment) error

	// UpdateStatus 更新支付状态
	UpdateStatus(ctx context.Context, txID string, status int8) error

	// ListByAgentDID 查询Agent的支付记录
	ListByAgentDID(ctx context.Context, agentDID string, status *int8, offset, limit int) ([]*entity.Payment, int64, error)

	// ListBySkillDID 查询Skill的支付记录
	ListBySkillDID(ctx context.Context, skillDID string, status *int8, offset, limit int) ([]*entity.Payment, int64, error)

	// ListPending 查询待处理的支付记录（用于定时任务）
	ListPending(ctx context.Context, beforeTime time.Time, limit int) ([]*entity.Payment, error)

	// ListFailedForRetry 查询可重试的失败记录
	ListFailedForRetry(ctx context.Context, maxRetryCount int, limit int) ([]*entity.Payment, error)

	// CountByAgentAndSkill 统计Agent对Skill的支付次数
	CountByAgentAndSkill(ctx context.Context, agentDID, skillDID string) (int64, error)
}

// PaymentIdempotencyRepository 幂等性记录仓库接口
type PaymentIdempotencyRepository interface {
	// Get 根据幂等性键查询
	Get(ctx context.Context, idempotencyKey string) (*entity.PaymentIdempotency, error)

	// Create 创建幂等性记录
	Create(ctx context.Context, record *entity.PaymentIdempotency) error

	// Update 更新幂等性记录
	Update(ctx context.Context, record *entity.PaymentIdempotency) error

	// DeleteExpired 删除过期的幂等性记录
	DeleteExpired(ctx context.Context, beforeTime time.Time) error
}

// BlockchainCallbackRepository 链上回调记录仓库接口
type BlockchainCallbackRepository interface {
	// Create 创建回调记录
	Create(ctx context.Context, callback *entity.BlockchainCallback) error

	// GetByTxHashAndType 根据交易哈希和类型查询
	GetByTxHashAndType(ctx context.Context, txHash, callbackType string) (*entity.BlockchainCallback, error)

	// MarkAsProcessed 标记为已处理
	MarkAsProcessed(ctx context.Context, id uint64) error

	// ListUnprocessed 查询未处理的回调记录
	ListUnprocessed(ctx context.Context, limit int) ([]*entity.BlockchainCallback, error)
}
