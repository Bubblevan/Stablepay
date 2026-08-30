package persistence

import (
	"context"
	"time"

	"github.com/stablepay/query-service/internal/domain/entity"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TransactionModel struct {
	ID          uint   `gorm:"primaryKey"`
	TxID        string `gorm:"column:tx_id;uniqueIndex;type:varchar(191)"`
	SourceTxID  string `gorm:"column:source_tx_id;index;type:varchar(191)"`
	AgentDID    string `gorm:"column:agent_did;index"`
	SkillDID    string `gorm:"column:skill_did;index"`
	AmountMinor int64  `gorm:"column:amount_minor"`
	Currency    int32  `gorm:"column:currency"`
	TxType      int32  `gorm:"column:tx_type"`
	CreatedAt   string `gorm:"column:created_at;type:varchar(191)"`
}

func (TransactionModel) TableName() string { return "transaction_records" }

type Repository struct{ db *gorm.DB }

func Open(dsn string) (*gorm.DB, error) { return gorm.Open(mysql.Open(dsn), &gorm.Config{}) }

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) AutoMigrate(ctx context.Context) error {
	return r.db.WithContext(ctx).AutoMigrate(&TransactionModel{})
}

func (r *Repository) SumPurchaseByAgent(ctx context.Context, agentDID string) (int64, error) {
	var total int64
	err := r.db.WithContext(ctx).Model(&TransactionModel{}).
		Where("agent_did = ? AND tx_type = ?", agentDID, int32(entity.TransactionPurchase)).
		Select("COALESCE(SUM(amount_minor), 0)").Scan(&total).Error
	return total, err
}

func (r *Repository) List(ctx context.Context, query entity.TransactionQuery) ([]entity.Transaction, int64, error) {
	db := r.db.WithContext(ctx).Model(&TransactionModel{})
	if query.Type == entity.TransactionPurchase {
		db = db.Where("agent_did = ? AND tx_type = ?", query.DID, int32(query.Type))
	} else {
		db = db.Where("skill_did = ? AND tx_type = ?", query.DID, int32(query.Type))
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []TransactionModel
	if err := db.Order("created_at DESC").Limit(query.Limit).Offset(query.Offset).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	items := make([]entity.Transaction, 0, len(rows))
	for _, row := range rows {
		items = append(items, toEntity(row))
	}
	return items, total, nil
}

func (r *Repository) RevenueSummary(ctx context.Context, skillDID string) (entity.RevenueSummary, error) {
	db := r.db.WithContext(ctx).Model(&TransactionModel{}).Where("skill_did = ? AND tx_type = ?", skillDID, int32(entity.TransactionRevenue))
	var result entity.RevenueSummary
	if err := db.Count(&result.TotalSales).Error; err != nil {
		return result, err
	}
	if err := db.Select("COALESCE(SUM(amount_minor), 0)").Scan(&result.TotalRevenueMinor).Error; err != nil {
		return result, err
	}
	var trends []struct {
		Date   string
		Amount int64
	}
	if err := db.Select("SUBSTR(created_at, 1, 10) AS date, SUM(amount_minor) AS amount").Group("SUBSTR(created_at, 1, 10)").Order("date ASC").Scan(&trends).Error; err != nil {
		return result, err
	}
	result.Trend = make([]entity.RevenueTrend, 0, len(trends))
	for _, trend := range trends {
		result.Trend = append(result.Trend, entity.RevenueTrend{Date: trend.Date, AmountMinor: trend.Amount})
	}
	return result, nil
}

func (r *Repository) ListSales(ctx context.Context, skillDID string, limit, offset int) ([]entity.Sale, int64, error) {
	items, total, err := r.List(ctx, entity.TransactionQuery{DID: skillDID, Type: entity.TransactionRevenue, Limit: limit, Offset: offset})
	if err != nil {
		return nil, 0, err
	}
	sales := make([]entity.Sale, 0, len(items))
	for _, item := range items {
		sales = append(sales, entity.Sale{Transaction: item})
	}
	return sales, total, nil
}

func (r *Repository) Upsert(ctx context.Context, transaction entity.Transaction) error {
	storageID := transaction.TxID + "#purchase"
	if transaction.Type == entity.TransactionRevenue {
		storageID = transaction.TxID + "#revenue"
	}
	row := TransactionModel{TxID: storageID, SourceTxID: transaction.SourceTxID, AgentDID: transaction.AgentDID, SkillDID: transaction.SkillDID, AmountMinor: transaction.AmountMinor, Currency: transaction.Currency, TxType: int32(transaction.Type), CreatedAt: transaction.CreatedAt.UTC().Format(time.RFC3339)}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "tx_id"}}, DoUpdates: clause.AssignmentColumns([]string{"source_tx_id", "agent_did", "skill_did", "amount_minor", "currency", "tx_type", "created_at"})}).Create(&row).Error
}

func toEntity(row TransactionModel) entity.Transaction {
	createdAt, _ := time.Parse(time.RFC3339, row.CreatedAt)
	return entity.Transaction{TxID: row.TxID, SourceTxID: row.SourceTxID, AgentDID: row.AgentDID, SkillDID: row.SkillDID, AmountMinor: row.AmountMinor, Currency: row.Currency, Type: entity.TransactionType(row.TxType), CreatedAt: createdAt}
}
