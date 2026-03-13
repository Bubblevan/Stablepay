// Package repository 数据仓库实现
package repository

import (
	"context"
	"time"

	"github.com/stablepay/payment-service/internal/domain/entity"
	"github.com/stablepay/payment-service/pkg/errors"
	"gorm.io/gorm"
)

// PaymentRepositoryImpl 支付记录仓库实现
type PaymentRepositoryImpl struct {
	db *gorm.DB
}

// NewPaymentRepository 创建仓库
func NewPaymentRepository(db *gorm.DB) *PaymentRepositoryImpl {
	return &PaymentRepositoryImpl{db: db}
}

// Create 创建支付记录
func (r *PaymentRepositoryImpl) Create(ctx context.Context, payment *entity.Payment) error {
	if err := r.db.WithContext(ctx).Create(payment).Error; err != nil {
		return errors.Wrap(errors.DATABASE_CONNECTION_ERROR, err, "failed to create payment")
	}
	return nil
}

// GetByTxID 根据交易ID查询
func (r *PaymentRepositoryImpl) GetByTxID(ctx context.Context, txID string) (*entity.Payment, error) {
	var payment entity.Payment
	if err := r.db.WithContext(ctx).Where("tx_id = ?", txID).First(&payment).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errors.New(errors.RESOURCE_NOT_FOUND, "payment not found")
		}
		return nil, errors.Wrap(errors.DATABASE_CONNECTION_ERROR, err, "failed to get payment")
	}
	return &payment, nil
}

// GetByTxHash 根据链上交易哈希查询
func (r *PaymentRepositoryImpl) GetByTxHash(ctx context.Context, txHash string) (*entity.Payment, error) {
	var payment entity.Payment
	if err := r.db.WithContext(ctx).Where("tx_hash = ?", txHash).First(&payment).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errors.New(errors.RESOURCE_NOT_FOUND, "payment not found")
		}
		return nil, errors.Wrap(errors.DATABASE_CONNECTION_ERROR, err, "failed to get payment")
	}
	return &payment, nil
}

// Update 更新支付记录
func (r *PaymentRepositoryImpl) Update(ctx context.Context, payment *entity.Payment) error {
	if err := r.db.WithContext(ctx).Save(payment).Error; err != nil {
		return errors.Wrap(errors.DATABASE_CONNECTION_ERROR, err, "failed to update payment")
	}
	return nil
}

// UpdateStatus 更新支付状态
func (r *PaymentRepositoryImpl) UpdateStatus(ctx context.Context, txID string, status int8) error {
	if err := r.db.WithContext(ctx).
		Model(&entity.Payment{}).
		Where("tx_id = ?", txID).
		Update("status", status).
		Update("updated_at", time.Now()).
		Error; err != nil {
		return errors.Wrap(errors.DATABASE_CONNECTION_ERROR, err, "failed to update payment status")
	}
	return nil
}

// ListByAgentDID 查询Agent的支付记录
func (r *PaymentRepositoryImpl) ListByAgentDID(ctx context.Context, agentDID string, status *int8, offset, limit int) ([]*entity.Payment, int64, error) {
	var payments []*entity.Payment
	var total int64

	query := r.db.WithContext(ctx).Model(&entity.Payment{}).Where("agent_did = ?", agentDID)
	if status != nil {
		query = query.Where("status = ?", *status)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, errors.Wrap(errors.DATABASE_CONNECTION_ERROR, err, "failed to count payments")
	}

	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&payments).Error; err != nil {
		return nil, 0, errors.Wrap(errors.DATABASE_CONNECTION_ERROR, err, "failed to list payments")
	}

	return payments, total, nil
}

// ListBySkillDID 查询Skill的支付记录
func (r *PaymentRepositoryImpl) ListBySkillDID(ctx context.Context, skillDID string, status *int8, offset, limit int) ([]*entity.Payment, int64, error) {
	var payments []*entity.Payment
	var total int64

	query := r.db.WithContext(ctx).Model(&entity.Payment{}).Where("skill_did = ?", skillDID)
	if status != nil {
		query = query.Where("status = ?", *status)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, errors.Wrap(errors.DATABASE_CONNECTION_ERROR, err, "failed to count payments")
	}

	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&payments).Error; err != nil {
		return nil, 0, errors.Wrap(errors.DATABASE_CONNECTION_ERROR, err, "failed to list payments")
	}

	return payments, total, nil
}

// ListPending 查询待处理的支付记录（用于定时任务）
func (r *PaymentRepositoryImpl) ListPending(ctx context.Context, beforeTime time.Time, limit int) ([]*entity.Payment, error) {
	var payments []*entity.Payment
	if err := r.db.WithContext(ctx).
		Where("status = ?", 1). // PENDING
		Where("created_at <= ?", beforeTime).
		Limit(limit).
		Find(&payments).Error; err != nil {
		return nil, errors.Wrap(errors.DATABASE_CONNECTION_ERROR, err, "failed to list pending payments")
	}
	return payments, nil
}

// ListFailedForRetry 查询可重试的失败记录
func (r *PaymentRepositoryImpl) ListFailedForRetry(ctx context.Context, maxRetryCount int, limit int) ([]*entity.Payment, error) {
	var payments []*entity.Payment
	if err := r.db.WithContext(ctx).
		Where("status = ?", 4). // FAILED
		Where("retry_count < ?", maxRetryCount).
		Limit(limit).
		Find(&payments).Error; err != nil {
		return nil, errors.Wrap(errors.DATABASE_CONNECTION_ERROR, err, "failed to list failed payments")
	}
	return payments, nil
}

// CountByAgentAndSkill 统计Agent对Skill的支付次数
func (r *PaymentRepositoryImpl) CountByAgentAndSkill(ctx context.Context, agentDID, skillDID string) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).
		Model(&entity.Payment{}).
		Where("agent_did = ?", agentDID).
		Where("skill_did = ?", skillDID).
		Where("status IN ?", []int{2, 3}). // CONFIRMED 或 COMPLETED
		Count(&count).Error; err != nil {
		return 0, errors.Wrap(errors.DATABASE_CONNECTION_ERROR, err, "failed to count payments")
	}
	return count, nil
}

// PaymentIdempotencyRepositoryImpl 幂等性记录仓库实现
type PaymentIdempotencyRepositoryImpl struct {
	db *gorm.DB
}

// NewPaymentIdempotencyRepository 创建仓库
func NewPaymentIdempotencyRepository(db *gorm.DB) *PaymentIdempotencyRepositoryImpl {
	return &PaymentIdempotencyRepositoryImpl{db: db}
}

// Get 根据幂等性键查询
func (r *PaymentIdempotencyRepositoryImpl) Get(ctx context.Context, idempotencyKey string) (*entity.PaymentIdempotency, error) {
	var record entity.PaymentIdempotency
	if err := r.db.WithContext(ctx).Where("idempotency_key = ?", idempotencyKey).First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, errors.Wrap(errors.DATABASE_CONNECTION_ERROR, err, "failed to get idempotency record")
	}
	return &record, nil
}

// Create 创建幂等性记录
func (r *PaymentIdempotencyRepositoryImpl) Create(ctx context.Context, record *entity.PaymentIdempotency) error {
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		return errors.Wrap(errors.DATABASE_CONNECTION_ERROR, err, "failed to create idempotency record")
	}
	return nil
}

// Update 更新幂等性记录
func (r *PaymentIdempotencyRepositoryImpl) Update(ctx context.Context, record *entity.PaymentIdempotency) error {
	if err := r.db.WithContext(ctx).Save(record).Error; err != nil {
		return errors.Wrap(errors.DATABASE_CONNECTION_ERROR, err, "failed to update idempotency record")
	}
	return nil
}

// DeleteExpired 删除过期的幂等性记录
func (r *PaymentIdempotencyRepositoryImpl) DeleteExpired(ctx context.Context, beforeTime time.Time) error {
	if err := r.db.WithContext(ctx).
		Where("expires_at < ?", beforeTime).
		Delete(&entity.PaymentIdempotency{}).Error; err != nil {
		return errors.Wrap(errors.DATABASE_CONNECTION_ERROR, err, "failed to delete expired records")
	}
	return nil
}

// BlockchainCallbackRepositoryImpl 链上回调记录仓库实现
type BlockchainCallbackRepositoryImpl struct {
	db *gorm.DB
}

// NewBlockchainCallbackRepository 创建仓库
func NewBlockchainCallbackRepository(db *gorm.DB) *BlockchainCallbackRepositoryImpl {
	return &BlockchainCallbackRepositoryImpl{db: db}
}

// Create 创建回调记录
func (r *BlockchainCallbackRepositoryImpl) Create(ctx context.Context, callback *entity.BlockchainCallback) error {
	if err := r.db.WithContext(ctx).Create(callback).Error; err != nil {
		return errors.Wrap(errors.DATABASE_CONNECTION_ERROR, err, "failed to create callback record")
	}
	return nil
}

// GetByTxHashAndType 根据交易哈希和类型查询
func (r *BlockchainCallbackRepositoryImpl) GetByTxHashAndType(ctx context.Context, txHash, callbackType string) (*entity.BlockchainCallback, error) {
	var record entity.BlockchainCallback
	if err := r.db.WithContext(ctx).
		Where("tx_hash = ?", txHash).
		Where("callback_type = ?", callbackType).
		First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, errors.Wrap(errors.DATABASE_CONNECTION_ERROR, err, "failed to get callback record")
	}
	return &record, nil
}

// MarkAsProcessed 标记为已处理
func (r *BlockchainCallbackRepositoryImpl) MarkAsProcessed(ctx context.Context, id uint64) error {
	now := time.Now()
	if err := r.db.WithContext(ctx).
		Model(&entity.BlockchainCallback{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"processed":    true,
			"processed_at": &now,
		}).Error; err != nil {
		return errors.Wrap(errors.DATABASE_CONNECTION_ERROR, err, "failed to mark callback as processed")
	}
	return nil
}

// ListUnprocessed 查询未处理的回调记录
func (r *BlockchainCallbackRepositoryImpl) ListUnprocessed(ctx context.Context, limit int) ([]*entity.BlockchainCallback, error) {
	var records []*entity.BlockchainCallback
	if err := r.db.WithContext(ctx).
		Where("processed = ?", false).
		Limit(limit).
		Find(&records).Error; err != nil {
		return nil, errors.Wrap(errors.DATABASE_CONNECTION_ERROR, err, "failed to list unprocessed callbacks")
	}
	return records, nil
}
