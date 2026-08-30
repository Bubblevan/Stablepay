// Package vo 定义值对象
package vo

import (
	"time"
)

// TxStatusVO 交易状态值对象
type TxStatusVO struct {
	TxHash      string
	Status      string // pending/confirmed/failed
	Slot        uint64
	BlockTime   *time.Time
	Fee         uint64
	Err         interface{}
	ExplorerURL string
}

// NewTxStatusVO 创建交易状态值对象
func NewTxStatusVO(txHash, status string) TxStatusVO {
	return TxStatusVO{
		TxHash: txHash,
		Status: status,
	}
}

// WithConfirmation 添加确认信息
func (t TxStatusVO) WithConfirmation(slot uint64, blockTime *time.Time, fee uint64) TxStatusVO {
	t.Slot = slot
	t.BlockTime = blockTime
	t.Fee = fee
	return t
}

// WithError 添加错误信息
func (t TxStatusVO) WithError(err interface{}) TxStatusVO {
	t.Err = err
	return t
}

// IsConfirmed 是否已确认
func (t TxStatusVO) IsConfirmed() bool {
	return t.Status == "confirmed"
}

// IsFailed 是否失败
func (t TxStatusVO) IsFailed() bool {
	return t.Status == "failed"
}

// IsPending 是否待处理
func (t TxStatusVO) IsPending() bool {
	return t.Status == "pending"
}
