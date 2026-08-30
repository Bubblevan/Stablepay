package tcc

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

// DefaultTransactionManager TCC事务管理器的默认实现
type DefaultTransactionManager struct {
	storage Storage
	logger  *logrus.Logger
}

// NewTransactionManager 创建新的事务管理器
func NewTransactionManager(storage Storage, logger *logrus.Logger) *DefaultTransactionManager {
	if logger == nil {
		logger = logrus.New()
	}
	return &DefaultTransactionManager{
		storage: storage,
		logger:  logger,
	}
}

// Begin 开始一个新的TCC事务
func (tm *DefaultTransactionManager) Begin(ctx context.Context, participants []TccParticipant, context map[string]interface{}) (*Transaction, error) {
	if len(participants) == 0 {
		return nil, fmt.Errorf("至少需要一个参与者")
	}

	// 创建事务，默认超时时间为30分钟
	timeout := 30 * time.Minute
	transaction := NewTransaction(participants, context, timeout)

	// 保存事务到存储（包括关联的参与者）
	if err := tm.storage.SaveTransaction(ctx, transaction); err != nil {
		return nil, fmt.Errorf("保存事务失败: %w", err)
	}

	tm.logger.WithFields(logrus.Fields{
		"transaction_id": transaction.ID,
		"participants":   len(participants),
	}).Info("TCC事务已开始")

	return transaction, nil
}

// Commit 提交TCC事务
func (tm *DefaultTransactionManager) Commit(ctx context.Context, transactionID string) error {
	// 获取事务
	transaction, err := tm.storage.GetTransaction(ctx, transactionID)
	if err != nil {
		return fmt.Errorf("获取事务失败: %w", err)
	}

	if transaction.Status != StatusTrying {
		return fmt.Errorf("事务状态不正确，当前状态: %s", transaction.Status)
	}

	// 更新事务状态为确认中
	transaction.Status = StatusConfirming
	transaction.UpdatedAt = time.Now()
	if err := tm.storage.UpdateTransaction(ctx, transaction); err != nil {
		return fmt.Errorf("更新事务状态失败: %w", err)
	}

	// 获取参与者
	participants, err := tm.storage.GetParticipantsByTransactionID(ctx, transactionID)
	if err != nil {
		return fmt.Errorf("获取参与者失败: %w", err)
	}

	// 执行确认操作
	confirmedCount := 0
	for _, participant := range participants {
		// 这里应该调用实际的参与者确认方法
		// 由于我们只有接口，这里模拟确认过程
		participant.Status = StatusConfirmed
		participant.UpdatedAt = time.Now()

		if err := tm.storage.UpdateParticipant(ctx, participant); err != nil {
			tm.logger.WithFields(logrus.Fields{
				"transaction_id": transactionID,
				"participant_id": participant.ID,
				"error":          err,
			}).Error("更新参与者状态失败")
			continue
		}
		confirmedCount++
	}

	// 更新事务状态
	if confirmedCount == len(participants) {
		transaction.Status = StatusConfirmed
	} else {
		transaction.Status = StatusFailed
	}
	transaction.UpdatedAt = time.Now()

	if err := tm.storage.UpdateTransaction(ctx, transaction); err != nil {
		return fmt.Errorf("更新事务最终状态失败: %w", err)
	}

	tm.logger.WithFields(logrus.Fields{
		"transaction_id":     transactionID,
		"confirmed_count":    confirmedCount,
		"total_participants": len(participants),
		"final_status":       transaction.Status,
	}).Info("TCC事务提交完成")

	return nil
}

// Cancel 取消TCC事务
func (tm *DefaultTransactionManager) Cancel(ctx context.Context, transactionID string) error {
	// 获取事务
	transaction, err := tm.storage.GetTransaction(ctx, transactionID)
	if err != nil {
		return fmt.Errorf("获取事务失败: %w", err)
	}

	// 更新事务状态为取消中
	transaction.Status = StatusCancelling
	transaction.UpdatedAt = time.Now()
	if err := tm.storage.UpdateTransaction(ctx, transaction); err != nil {
		return fmt.Errorf("更新事务状态失败: %w", err)
	}

	// 获取参与者
	participants, err := tm.storage.GetParticipantsByTransactionID(ctx, transactionID)
	if err != nil {
		return fmt.Errorf("获取参与者失败: %w", err)
	}

	// 执行取消操作
	cancelledCount := 0
	for _, participant := range participants {
		// 这里应该调用实际的参与者取消方法
		// 由于我们只有接口，这里模拟取消过程
		participant.Status = StatusCancelled
		participant.UpdatedAt = time.Now()

		if err := tm.storage.UpdateParticipant(ctx, participant); err != nil {
			tm.logger.WithFields(logrus.Fields{
				"transaction_id": transactionID,
				"participant_id": participant.ID,
				"error":          err,
			}).Error("更新参与者状态失败")
			continue
		}
		cancelledCount++
	}

	// 更新事务状态
	if cancelledCount == len(participants) {
		transaction.Status = StatusCancelled
	} else {
		transaction.Status = StatusFailed
	}
	transaction.UpdatedAt = time.Now()

	if err := tm.storage.UpdateTransaction(ctx, transaction); err != nil {
		return fmt.Errorf("更新事务最终状态失败: %w", err)
	}

	tm.logger.WithFields(logrus.Fields{
		"transaction_id":     transactionID,
		"cancelled_count":    cancelledCount,
		"total_participants": len(participants),
		"final_status":       transaction.Status,
	}).Info("TCC事务取消完成")

	return nil
}

// GetTransaction 根据ID获取事务
func (tm *DefaultTransactionManager) GetTransaction(ctx context.Context, transactionID string) (*Transaction, error) {
	return tm.storage.GetTransaction(ctx, transactionID)
}

// ListTransactions 列出事务
func (tm *DefaultTransactionManager) ListTransactions(ctx context.Context, status TransactionStatus, limit int, offset int) ([]*Transaction, error) {
	return tm.storage.ListTransactions(ctx, status, limit, offset)
}
