// Package entity 定义领域实体
// GasSubsidyEntity 表示 Gas 补贴的领域实体，包含业务规则和行为
package entity

import (
	"time"

	"github.com/google/uuid"
)

// SubsidyStatus 补贴状态
type SubsidyStatus string

const (
	SubsidyPending   SubsidyStatus = "pending"
	SubsidyCompleted SubsidyStatus = "completed"
	SubsidyFailed    SubsidyStatus = "failed"
)

// GasSubsidyEntity Gas 补贴领域实体
// 职责：封装 Gas 补贴的业务规则和数据
type GasSubsidyEntity struct {
	ID              uint64
	TxID            string
	OriginalTxHash  string
	GasAmount       uint64
	SubsidyAmount   uint64
	AgentPaidAmount uint64
	SubsidyTxHash   string
	Status          SubsidyStatus
	FeePayer        string
	Network         string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// NewGasSubsidyEntity 创建新的 Gas 补贴实体
// 工厂方法，确保实体创建时的默认值
func NewGasSubsidyEntity(txID, feePayer, network string) *GasSubsidyEntity {
	if txID == "" {
		txID = uuid.New().String()
	}
	
	return &GasSubsidyEntity{
		TxID:            txID,
		Status:          SubsidyPending,
		FeePayer:        feePayer,
		Network:         network,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
}

// CalculateSubsidy 计算补贴金额
// 业务规则：根据实际 Gas 费用和补贴比例计算
// ratio: 1.0 = 100% 补贴, 0.5 = 50% 补贴
func (e *GasSubsidyEntity) CalculateSubsidy(actualGas uint64, ratio float64) {
	e.GasAmount = actualGas
	e.SubsidyAmount = uint64(float64(actualGas) * ratio)
	e.AgentPaidAmount = actualGas - e.SubsidyAmount
	e.UpdatedAt = time.Now()
}

// MarkCompleted 标记为已完成
func (e *GasSubsidyEntity) MarkCompleted() {
	e.Status = SubsidyCompleted
	e.UpdatedAt = time.Now()
}

// MarkFailed 标记为失败
func (e *GasSubsidyEntity) MarkFailed() {
	e.Status = SubsidyFailed
	e.UpdatedAt = time.Now()
}

// IsCompleted 是否已完成
func (e *GasSubsidyEntity) IsCompleted() bool {
	return e.Status == SubsidyCompleted
}

// IsPending 是否待处理
func (e *GasSubsidyEntity) IsPending() bool {
	return e.Status == SubsidyPending
}

// SetTxHash 设置交易哈希
func (e *GasSubsidyEntity) SetTxHash(txHash string) {
	e.OriginalTxHash = txHash
	e.UpdatedAt = time.Now()
}

// GetTotalSubsidyAmount 获取累计补贴金额（用于统计）
func (e *GasSubsidyEntity) GetTotalSubsidyAmount() uint64 {
	if e.IsCompleted() {
		return e.SubsidyAmount
	}
	return 0
}

// UpdateStatus 更新状态（通用方法，用于状态同步）
func (e *GasSubsidyEntity) UpdateStatus(status SubsidyStatus) {
	e.Status = status
	e.UpdatedAt = time.Now()
}

// IsFailed 是否已失败
func (e *GasSubsidyEntity) IsFailed() bool {
	return e.Status == SubsidyFailed
}

// IsValidForFinalization 是否可以最终确定
// 业务规则：只有 pending 状态的交易才能更新为最终状态
func (e *GasSubsidyEntity) IsValidForFinalization() bool {
	return e.Status == SubsidyPending
}
