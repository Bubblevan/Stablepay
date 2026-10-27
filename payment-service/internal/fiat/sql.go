package fiat

import (
	"encoding/json"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SQLStore reuses payment-service's existing MySQL connection. The checkpoint
// lock is intentionally coarse for this MVP; it provides transactional dedupe
// across callbacks and multiple service instances without changing USDC tables.
type SQLStore struct{ db *gorm.DB }
type paymentSnapshot struct {
	ID      uint   `gorm:"primaryKey"`
	Payload []byte `gorm:"type:longblob;not null"`
}

func (paymentSnapshot) TableName() string { return "fiat_payment_snapshots" }
func NewSQLStore(db *gorm.DB, migrate bool) (*SQLStore, error) {
	if migrate {
		if err := db.AutoMigrate(&paymentSnapshot{}); err != nil {
			return nil, err
		}
	}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&paymentSnapshot{ID: 1, Payload: []byte("{}")}).Error; err != nil {
		return nil, err
	}
	return &SQLStore{db: db}, nil
}
func (s *SQLStore) Update(change func(map[string]Order) error) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var row paymentSnapshot
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, 1).Error; err != nil {
			return err
		}
		var orders map[string]Order
		if err := json.Unmarshal(row.Payload, &orders); err != nil {
			return err
		}
		if orders == nil {
			return errors.New("invalid fiat SQL snapshot")
		}
		if err := change(orders); err != nil {
			return err
		}
		raw, err := json.Marshal(orders)
		if err != nil {
			return err
		}
		if string(raw) == string(row.Payload) {
			return nil
		}
		return tx.Model(&row).Update("payload", raw).Error
	})
}
