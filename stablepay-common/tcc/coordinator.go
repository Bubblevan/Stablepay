package tcc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

// Coordinator TCC协调器，负责执行Try-Confirm-Cancel流程
type Coordinator struct {
	manager TransactionManager
	logger  *logrus.Logger
}

// NewCoordinator 创建新的协调器
func NewCoordinator(manager TransactionManager, logger *logrus.Logger) *Coordinator {
	if logger == nil {
		logger = logrus.New()
	}
	return &Coordinator{
		manager: manager,
		logger:  logger,
	}
}

// ExecuteTransaction 执行TCC事务
func (c *Coordinator) ExecuteTransaction(ctx context.Context, participants []TccParticipant, context map[string]interface{}) error {
	// 开始事务
	transaction, err := c.manager.Begin(ctx, participants, context)
	if err != nil {
		return fmt.Errorf("开始事务失败: %w", err)
	}

	// 执行Try阶段
	if err := c.executeTryPhase(ctx, transaction, participants); err != nil {
		c.logger.WithFields(logrus.Fields{
			"transaction_id": transaction.ID,
			"error":          err,
		}).Error("Try阶段执行失败，开始取消事务")

		// Try阶段失败，执行Cancel
		if cancelErr := c.manager.Cancel(ctx, transaction.ID); cancelErr != nil {
			c.logger.WithFields(logrus.Fields{
				"transaction_id": transaction.ID,
				"cancel_error":   cancelErr,
			}).Error("取消事务失败")
		}
		return err
	}

	// Try阶段成功，执行Confirm阶段
	if err := c.executeConfirmPhase(ctx, transaction, participants); err != nil {
		c.logger.WithFields(logrus.Fields{
			"transaction_id": transaction.ID,
			"error":          err,
		}).Error("Confirm阶段执行失败，开始取消事务")

		// Confirm阶段失败，执行Cancel
		if cancelErr := c.manager.Cancel(ctx, transaction.ID); cancelErr != nil {
			c.logger.WithFields(logrus.Fields{
				"transaction_id": transaction.ID,
				"cancel_error":   cancelErr,
			}).Error("取消事务失败")
		}
		return err
	}

	c.logger.WithFields(logrus.Fields{
		"transaction_id": transaction.ID,
	}).Info("TCC事务执行成功")

	return nil
}

// executeTryPhase 执行Try阶段
func (c *Coordinator) executeTryPhase(ctx context.Context, transaction *Transaction, participants []TccParticipant) error {
	c.logger.WithFields(logrus.Fields{
		"transaction_id": transaction.ID,
		"participants":   len(participants),
	}).Info("开始执行Try阶段")

	// 更新事务状态为尝试中
	transaction.Status = StatusTrying
	transaction.UpdatedAt = time.Now()

	// 这里应该更新存储中的事务状态
	// 由于我们只有接口，这里简化处理

	// 执行每个参与者的Try操作
	for i, participant := range participants {
		c.logger.WithFields(logrus.Fields{
			"transaction_id": transaction.ID,
			"participant":    participant.GetName(),
			"index":          i,
		}).Info("执行参与者Try操作")

		// 解析Context
		var context map[string]interface{}
		json.Unmarshal([]byte(transaction.Context), &context)

		if err := participant.Try(ctx, transaction.ID, context); err != nil {
			c.logger.WithFields(logrus.Fields{
				"transaction_id": transaction.ID,
				"participant":    participant.GetName(),
				"error":          err,
			}).Error("参与者Try操作失败")
			return fmt.Errorf("参与者 %s Try操作失败: %w", participant.GetName(), err)
		}
	}

	c.logger.WithFields(logrus.Fields{
		"transaction_id": transaction.ID,
	}).Info("Try阶段执行完成")

	return nil
}

// executeConfirmPhase 执行Confirm阶段
func (c *Coordinator) executeConfirmPhase(ctx context.Context, transaction *Transaction, participants []TccParticipant) error {
	c.logger.WithFields(logrus.Fields{
		"transaction_id": transaction.ID,
		"participants":   len(participants),
	}).Info("开始执行Confirm阶段")

	// 执行每个参与者的Confirm操作
	for i, participant := range participants {
		c.logger.WithFields(logrus.Fields{
			"transaction_id": transaction.ID,
			"participant":    participant.GetName(),
			"index":          i,
		}).Info("执行参与者Confirm操作")

		// 解析Context
		var context map[string]interface{}
		json.Unmarshal([]byte(transaction.Context), &context)

		if err := participant.Confirm(ctx, transaction.ID, context); err != nil {
			c.logger.WithFields(logrus.Fields{
				"transaction_id": transaction.ID,
				"participant":    participant.GetName(),
				"error":          err,
			}).Error("参与者Confirm操作失败")
			return fmt.Errorf("参与者 %s Confirm操作失败: %w", participant.GetName(), err)
		}
	}

	c.logger.WithFields(logrus.Fields{
		"transaction_id": transaction.ID,
	}).Info("Confirm阶段执行完成")

	return nil
}

// CancelTransaction 取消事务
func (c *Coordinator) CancelTransaction(ctx context.Context, transactionID string) error {
	c.logger.WithFields(logrus.Fields{
		"transaction_id": transactionID,
	}).Info("开始取消事务")

	return c.manager.Cancel(ctx, transactionID)
}

// GetTransaction 获取事务信息
func (c *Coordinator) GetTransaction(ctx context.Context, transactionID string) (*Transaction, error) {
	return c.manager.GetTransaction(ctx, transactionID)
}

// ListTransactions 列出事务
func (c *Coordinator) ListTransactions(ctx context.Context, status TransactionStatus, limit int, offset int) ([]*Transaction, error) {
	return c.manager.ListTransactions(ctx, status, limit, offset)
}
