package mysql

import (
	"context"
	"time"

	"github.com/stablepay/verification-service/internal/domain/entity"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type PurchaseModel struct {
	ID          uint      `gorm:"primaryKey"`
	EventID     *string   `gorm:"column:event_id;type:varchar(128);uniqueIndex"`
	AgentDID    string    `gorm:"column:agent_did;type:varchar(128);uniqueIndex:uk_agent_skill"`
	SkillDID    string    `gorm:"column:skill_did;type:varchar(128);uniqueIndex:uk_agent_skill"`
	TxID        string    `gorm:"column:tx_id;type:varchar(128);index"`
	AmountMinor int64     `gorm:"column:amount_minor"`
	Currency    int32     `gorm:"column:currency"`
	TxHash      string    `gorm:"column:tx_hash;type:varchar(128)"`
	CreatedAt   time.Time `gorm:"column:created_at"`
}

func (PurchaseModel) TableName() string { return "purchase_records" }

type PurchaseRepository struct{ db *gorm.DB }

func Open(dsn string) (*gorm.DB, error) { return gorm.Open(mysql.Open(dsn), &gorm.Config{}) }

func NewPurchaseRepository(db *gorm.DB) *PurchaseRepository { return &PurchaseRepository{db: db} }

func AutoMigrate(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).AutoMigrate(&PurchaseModel{})
}

func (r *PurchaseRepository) Find(ctx context.Context, agentDID, skillDID string) (*entity.PurchaseRecord, error) {
	var row PurchaseModel
	if err := r.db.WithContext(ctx).Where("agent_did = ? AND skill_did = ?", agentDID, skillDID).First(&row).Error; err != nil {
		return nil, err
	}
	return &entity.PurchaseRecord{
		EventID:     valueOrEmpty(row.EventID),
		AgentDID:    row.AgentDID,
		SkillDID:    row.SkillDID,
		TxID:        row.TxID,
		AmountMinor: row.AmountMinor,
		Currency:    row.Currency,
		TxHash:      row.TxHash,
		CreatedAt:   row.CreatedAt,
	}, nil
}

func (r *PurchaseRepository) FindByEventID(ctx context.Context, eventID string) (*entity.PurchaseRecord, error) {
	if eventID == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var row PurchaseModel
	if err := r.db.WithContext(ctx).Where("event_id = ?", eventID).First(&row).Error; err != nil {
		return nil, err
	}
	return &entity.PurchaseRecord{
		EventID: valueOrEmpty(row.EventID), AgentDID: row.AgentDID, SkillDID: row.SkillDID,
		TxID: row.TxID, AmountMinor: row.AmountMinor, Currency: row.Currency,
		TxHash: row.TxHash, CreatedAt: row.CreatedAt,
	}, nil
}

func (r *PurchaseRepository) Create(ctx context.Context, record *entity.PurchaseRecord) error {
	eventID := record.EventID
	return r.db.WithContext(ctx).Create(&PurchaseModel{
		EventID:     stringPtrOrNil(eventID),
		AgentDID:    record.AgentDID,
		SkillDID:    record.SkillDID,
		TxID:        record.TxID,
		AmountMinor: record.AmountMinor,
		Currency:    record.Currency,
		TxHash:      record.TxHash,
		CreatedAt:   record.CreatedAt,
	}).Error
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func stringPtrOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
