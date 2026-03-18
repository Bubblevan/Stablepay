// Package persist 定义持久化对象（PO）
// 职责：数据库表结构映射，与领域实体分离
package persist

// GasSubsidyPO Gas 补贴持久化对象（数据库表映射）
type GasSubsidyPO struct {
	ID              uint64 `gorm:"primaryKey;autoIncrement"`
	TxID            string `gorm:"size:64;index;not null"`
	OriginalTxHash  string `gorm:"size:88;index;not null"`
	GasAmount       uint64 `gorm:"not null"`
	SubsidyAmount   uint64 `gorm:"not null"`
	AgentPaidAmount uint64 `gorm:"not null"`
	SubsidyTxHash   string `gorm:"size:88"`
	Status          string `gorm:"size:20;index;not null"`
	FeePayer        string `gorm:"size:44;not null"`
	Network         string `gorm:"size:20;not null"`
	CreatedAt       int64  `gorm:"index"`
	UpdatedAt       int64
}

// TableName 表名
func (GasSubsidyPO) TableName() string {
	return "gas_subsidies"
}
