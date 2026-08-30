// Package entity 定义领域实体
// TransactionEntity 表示区块链交易的领域实体
package entity

import (
	"time"
)

// TxStatus 交易状态
type TxStatus string

const (
	TxPending   TxStatus = "pending"
	TxConfirmed TxStatus = "confirmed"
	TxFailed    TxStatus = "failed"
)

// TransactionEntity 交易领域实体
// 职责：封装区块链交易的核心数据和状态
type TransactionEntity struct {
	TxHash      string
	Status      TxStatus
	Slot        uint64
	BlockTime   *time.Time
	Fee         uint64
	Err         interface{}
	ExplorerURL string
}

// NewTransactionEntity 创建交易实体
func NewTransactionEntity(txHash string) *TransactionEntity {
	return &TransactionEntity{
		TxHash: txHash,
		Status: TxPending,
	}
}

// MarkConfirmed 标记为已确认
func (e *TransactionEntity) MarkConfirmed(slot uint64, blockTime *time.Time, fee uint64) {
	e.Status = TxConfirmed
	e.Slot = slot
	e.BlockTime = blockTime
	e.Fee = fee
}

// MarkFailed 标记为失败
func (e *TransactionEntity) MarkFailed(err interface{}) {
	e.Status = TxFailed
	e.Err = err
}

// IsConfirmed 是否已确认
func (e *TransactionEntity) IsConfirmed() bool {
	return e.Status == TxConfirmed
}

// IsFailed 是否失败
func (e *TransactionEntity) IsFailed() bool {
	return e.Status == TxFailed
}

// GetConfirmationTime 获取确认时间（如果有）
func (e *TransactionEntity) GetConfirmationTime() *time.Time {
	return e.BlockTime
}
