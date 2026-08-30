package tcc

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// GormStorage 基于GORM的存储实现
type GormStorage struct {
	db *gorm.DB
}

// NewGormStorage 创建新的GORM存储
func NewGormStorage(db *gorm.DB) *GormStorage {
	// 自动迁移表结构
	db.AutoMigrate(&Transaction{}, &Participant{})
	return &GormStorage{db: db}
}

// SaveTransaction 保存事务
func (s *GormStorage) SaveTransaction(ctx context.Context, transaction *Transaction) error {
	// 保存事务
	if err := s.db.WithContext(ctx).Create(transaction).Error; err != nil {
		return fmt.Errorf("保存事务失败: %w", err)
	}

	return nil
}

// UpdateTransaction 更新事务
func (s *GormStorage) UpdateTransaction(ctx context.Context, transaction *Transaction) error {
	// 更新事务
	if err := s.db.WithContext(ctx).Model(&Transaction{}).
		Where("id = ?", transaction.ID).
		Updates(map[string]interface{}{
			"status":     transaction.Status,
			"updated_at": transaction.UpdatedAt,
			"expires_at": transaction.ExpiresAt,
			"context":    transaction.Context,
		}).Error; err != nil {
		return fmt.Errorf("更新事务失败: %w", err)
	}

	return nil
}

// GetTransaction 根据ID获取事务
func (s *GormStorage) GetTransaction(ctx context.Context, transactionID string) (*Transaction, error) {
	var transaction Transaction
	if err := s.db.WithContext(ctx).Preload("Participants").
		Where("id = ?", transactionID).First(&transaction).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("事务不存在: %s", transactionID)
		}
		return nil, fmt.Errorf("获取事务失败: %w", err)
	}

	// Context已经是字符串，无需反序列化

	return &transaction, nil
}

// ListTransactions 列出事务
func (s *GormStorage) ListTransactions(ctx context.Context, status TransactionStatus, limit int, offset int) ([]*Transaction, error) {
	var transactions []*Transaction
	query := s.db.WithContext(ctx).Preload("Participants")

	if status != "" {
		query = query.Where("status = ?", status)
	}

	if err := query.Limit(limit).Offset(offset).
		Order("created_at DESC").Find(&transactions).Error; err != nil {
		return nil, fmt.Errorf("列出事务失败: %w", err)
	}

	// Context已经是字符串，无需反序列化

	return transactions, nil
}

// SaveParticipant 保存参与者
func (s *GormStorage) SaveParticipant(ctx context.Context, participant *Participant) error {
	if err := s.db.WithContext(ctx).Create(participant).Error; err != nil {
		return fmt.Errorf("保存参与者失败: %w", err)
	}

	return nil
}

// UpdateParticipant 更新参与者
func (s *GormStorage) UpdateParticipant(ctx context.Context, participant *Participant) error {
	// 更新参与者
	if err := s.db.WithContext(ctx).Model(&Participant{}).
		Where("id = ?", participant.ID).
		Updates(map[string]interface{}{
			"status":     participant.Status,
			"updated_at": participant.UpdatedAt,
			"context":    participant.Context,
			"error":      participant.Error,
		}).Error; err != nil {
		return fmt.Errorf("更新参与者失败: %w", err)
	}

	return nil
}

// GetParticipantsByTransactionID 获取事务的参与者
func (s *GormStorage) GetParticipantsByTransactionID(ctx context.Context, transactionID string) ([]*Participant, error) {
	var participants []*Participant
	if err := s.db.WithContext(ctx).
		Where("transaction_id = ?", transactionID).
		Find(&participants).Error; err != nil {
		return nil, fmt.Errorf("获取参与者失败: %w", err)
	}

	// Context已经是字符串，无需反序列化

	return participants, nil
}
