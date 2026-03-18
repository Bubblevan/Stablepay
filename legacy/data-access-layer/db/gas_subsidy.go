// Package db 提供 Gas 补贴记录的数据访问层
// 从 demo1/internal/solana/gas_subsidy.go 重构为 MySQL/GORM 实现
package db

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// GasSubsidyStatus 补贴状态
type GasSubsidyStatus string

const (
	// SubsidyPending 待处理
	SubsidyPending GasSubsidyStatus = "pending"
	// SubsidyCompleted 已完成
	SubsidyCompleted GasSubsidyStatus = "completed"
	// SubsidyFailed 失败
	SubsidyFailed GasSubsidyStatus = "failed"
)

// GasSubsidyRecord Gas 费补贴记录模型
// 对应数据库表: gas_subsidies
// 从 demo1 的 JSON 存储迁移到 MySQL
type GasSubsidyRecord struct {
	ID              uint64           `gorm:"primaryKey;autoIncrement" json:"id"`
	TxID            string           `gorm:"size:64;index;not null" json:"tx_id"`              // 关联的支付交易 ID
	OriginalTxHash  string           `gorm:"size:88;index;not null" json:"original_tx_hash"`   // 原交易哈希 (Base58)
	GasAmount       uint64           `gorm:"not null" json:"gas_amount"`                       // 总 Gas 费金额 (lamports)
	SubsidyAmount   uint64           `gorm:"not null" json:"subsidy_amount"`                   // 补贴金额
	AgentPaidAmount uint64           `gorm:"not null" json:"agent_paid_amount"`                // Agent 承担金额
	SubsidyTxHash   string           `gorm:"size:88" json:"subsidy_tx_hash"`                   // 补贴交易哈希
	Status          GasSubsidyStatus `gorm:"size:20;index;not null" json:"status"`             // 状态
	FeePayer        string           `gorm:"size:44;not null" json:"fee_payer"`                // FeePayer 地址
	Network         string           `gorm:"size:20;not null" json:"network"`                  // 网络类型
	CreatedAt       time.Time        `gorm:"index" json:"created_at"`                          // 创建时间
	UpdatedAt       time.Time        `json:"updated_at"`                                       // 更新时间
}

// TableName 指定表名
func (GasSubsidyRecord) TableName() string {
	return "gas_subsidies"
}

// SubsidyStats 补贴统计信息
type SubsidyStats struct {
	TotalTransactions int64  `json:"total_transactions"`
	CompletedCount    int64  `json:"completed_count"`
	FailedCount       int64  `json:"failed_count"`
	PendingCount      int64  `json:"pending_count"`
	TotalSubsidy      uint64 `json:"total_subsidy"`    // lamports
	TotalAgentPaid    uint64 `json:"total_agent_paid"` // lamports
}

// GasSubsidyDAL Gas 补贴数据访问层
type GasSubsidyDAL struct {
	db *gorm.DB
}

// NewGasSubsidyDAL 创建 DAL 实例
func NewGasSubsidyDAL(db *gorm.DB) *GasSubsidyDAL {
	return &GasSubsidyDAL{db: db}
}

// Create 创建新的补贴记录
func (dal *GasSubsidyDAL) Create(record *GasSubsidyRecord) error {
	if record.Status == "" {
		record.Status = SubsidyPending
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now()
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = time.Now()
	}

	if err := dal.db.Create(record).Error; err != nil {
		return fmt.Errorf("failed to create gas subsidy record: %w", err)
	}
	return nil
}

// GetByTxHash 通过交易哈希查询记录
func (dal *GasSubsidyDAL) GetByTxHash(txHash string) (*GasSubsidyRecord, error) {
	var record GasSubsidyRecord
	if err := dal.db.Where("original_tx_hash = ?", txHash).First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get gas subsidy record: %w", err)
	}
	return &record, nil
}

// GetByID 通过 ID 查询记录
func (dal *GasSubsidyDAL) GetByID(id uint64) (*GasSubsidyRecord, error) {
	var record GasSubsidyRecord
	if err := dal.db.First(&record, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get gas subsidy record: %w", err)
	}
	return &record, nil
}

// UpdateStatus 更新记录状态
func (dal *GasSubsidyDAL) UpdateStatus(id uint64, status GasSubsidyStatus, gasAmount uint64) error {
	updates := map[string]interface{}{
		"status":       status,
		"updated_at":   time.Now(),
		"gas_amount":   gasAmount,
	}

	if err := dal.db.Model(&GasSubsidyRecord{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update gas subsidy status: %w", err)
	}
	return nil
}

// UpdateSubsidyDetails 更新补贴详情
func (dal *GasSubsidyDAL) UpdateSubsidyDetails(id uint64, gasAmount, subsidyAmount, agentPaidAmount uint64, status GasSubsidyStatus) error {
	updates := map[string]interface{}{
		"gas_amount":        gasAmount,
		"subsidy_amount":    subsidyAmount,
		"agent_paid_amount": agentPaidAmount,
		"status":            status,
		"updated_at":        time.Now(),
	}

	if err := dal.db.Model(&GasSubsidyRecord{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update subsidy details: %w", err)
	}
	return nil
}

// List 查询记录列表（支持分页）
func (dal *GasSubsidyDAL) List(limit, offset int) ([]*GasSubsidyRecord, error) {
	var records []*GasSubsidyRecord
	if err := dal.db.Order("created_at DESC").Limit(limit).Offset(offset).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("failed to list gas subsidy records: %w", err)
	}
	return records, nil
}

// ListByStatus 按状态查询记录
func (dal *GasSubsidyDAL) ListByStatus(status GasSubsidyStatus, limit, offset int) ([]*GasSubsidyRecord, error) {
	var records []*GasSubsidyRecord
	if err := dal.db.Where("status = ?", status).Order("created_at DESC").Limit(limit).Offset(offset).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("failed to list gas subsidy records by status: %w", err)
	}
	return records, nil
}

// ListByNetwork 按网络查询记录
func (dal *GasSubsidyDAL) ListByNetwork(network string, limit, offset int) ([]*GasSubsidyRecord, error) {
	var records []*GasSubsidyRecord
	if err := dal.db.Where("network = ?", network).Order("created_at DESC").Limit(limit).Offset(offset).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("failed to list gas subsidy records by network: %w", err)
	}
	return records, nil
}

// GetStats 获取补贴统计信息
func (dal *GasSubsidyDAL) GetStats() (*SubsidyStats, error) {
	var stats SubsidyStats

	// 总交易数
	if err := dal.db.Model(&GasSubsidyRecord{}).Count(&stats.TotalTransactions).Error; err != nil {
		return nil, fmt.Errorf("failed to get total count: %w", err)
	}

	// 各状态数量
	if err := dal.db.Model(&GasSubsidyRecord{}).Where("status = ?", SubsidyCompleted).Count(&stats.CompletedCount).Error; err != nil {
		return nil, fmt.Errorf("failed to get completed count: %w", err)
	}

	if err := dal.db.Model(&GasSubsidyRecord{}).Where("status = ?", SubsidyFailed).Count(&stats.FailedCount).Error; err != nil {
		return nil, fmt.Errorf("failed to get failed count: %w", err)
	}

	if err := dal.db.Model(&GasSubsidyRecord{}).Where("status = ?", SubsidyPending).Count(&stats.PendingCount).Error; err != nil {
		return nil, fmt.Errorf("failed to get pending count: %w", err)
	}

	// 总补贴金额
	if err := dal.db.Model(&GasSubsidyRecord{}).Select("COALESCE(SUM(subsidy_amount), 0)").Scan(&stats.TotalSubsidy).Error; err != nil {
		return nil, fmt.Errorf("failed to get total subsidy: %w", err)
	}

	// Agent 总支付金额
	if err := dal.db.Model(&GasSubsidyRecord{}).Select("COALESCE(SUM(agent_paid_amount), 0)").Scan(&stats.TotalAgentPaid).Error; err != nil {
		return nil, fmt.Errorf("failed to get total agent paid: %w", err)
	}

	return &stats, nil
}

// GetStatsByNetwork 按网络获取统计
func (dal *GasSubsidyDAL) GetStatsByNetwork(network string) (*SubsidyStats, error) {
	var stats SubsidyStats

	if err := dal.db.Model(&GasSubsidyRecord{}).Where("network = ?", network).Count(&stats.TotalTransactions).Error; err != nil {
		return nil, fmt.Errorf("failed to get stats: %w", err)
	}

	if err := dal.db.Model(&GasSubsidyRecord{}).Where("network = ? AND status = ?", network, SubsidyCompleted).Count(&stats.CompletedCount).Error; err != nil {
		return nil, fmt.Errorf("failed to get stats: %w", err)
	}

	if err := dal.db.Model(&GasSubsidyRecord{}).Where("network = ?", network).Select("COALESCE(SUM(subsidy_amount), 0)").Scan(&stats.TotalSubsidy).Error; err != nil {
		return nil, fmt.Errorf("failed to get stats: %w", err)
	}

	return &stats, nil
}

// CreateSubsidyRecord 便捷的记录创建方法
// 用于业务层快速创建补贴记录
func (dal *GasSubsidyDAL) CreateSubsidyRecord(txID, txHash, feePayer, network string, estimatedFee uint64) (*GasSubsidyRecord, error) {
	record := &GasSubsidyRecord{
		TxID:            txID,
		OriginalTxHash:  txHash,
		GasAmount:       estimatedFee,
		SubsidyAmount:   estimatedFee, // 默认全额补贴
		AgentPaidAmount: 0,
		Status:          SubsidyPending,
		FeePayer:        feePayer,
		Network:         network,
	}

	if err := dal.Create(record); err != nil {
		return nil, err
	}

	return record, nil
}

// UpdateSubsidyStatusByTxHash 通过交易哈希更新状态
func (dal *GasSubsidyDAL) UpdateSubsidyStatusByTxHash(txHash string, status GasSubsidyStatus, gasAmount uint64) error {
	updates := map[string]interface{}{
		"status":     status,
		"gas_amount": gasAmount,
		"updated_at": time.Now(),
	}

	if err := dal.db.Model(&GasSubsidyRecord{}).Where("original_tx_hash = ?", txHash).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update subsidy status: %w", err)
	}
	return nil
}

// FinalizeSubsidy 完成补贴记录（交易确认后调用）
func (dal *GasSubsidyDAL) FinalizeSubsidy(id uint64, actualGas uint64, subsidyRatio float64) error {
	// 计算分摊金额
	subsidyAmount := uint64(float64(actualGas) * subsidyRatio)
	agentPaidAmount := actualGas - subsidyAmount

	updates := map[string]interface{}{
		"gas_amount":        actualGas,
		"subsidy_amount":    subsidyAmount,
		"agent_paid_amount": agentPaidAmount,
		"status":            SubsidyCompleted,
		"updated_at":        time.Now(),
	}

	if err := dal.db.Model(&GasSubsidyRecord{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to finalize subsidy: %w", err)
	}
	return nil
}
