package fiat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/stablepay/payment-service/internal/application/dto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SQLStore keeps each order and its idempotency keys as indexed MySQL rows.
// A small lock row serializes the existing Store.Update callback API while the
// transactional outbox is committed with the same order transition.
type SQLStore struct{ db *gorm.DB }

type fiatOrderRow struct {
	ID             string    `gorm:"primaryKey;size:40"`
	RequestID      string    `gorm:"size:128;not null;uniqueIndex:uidx_fiat_order_request"`
	IdempotencyKey string    `gorm:"size:128;not null;uniqueIndex:uidx_fiat_order_idempotency"`
	ProductID      string    `gorm:"size:128;not null"`
	Subject        string    `gorm:"size:180;not null"`
	QuoteHash      string    `gorm:"size:128;not null"`
	ApprovalRef    string    `gorm:"size:128;not null"`
	AgentDID       string    `gorm:"size:255;not null"`
	SkillDID       string    `gorm:"size:255;not null"`
	AmountMinor    int64     `gorm:"not null"`
	Currency       string    `gorm:"size:8;not null"`
	Mode           string    `gorm:"size:32;not null"`
	Status         string    `gorm:"size:32;not null;index"`
	TradeNo        *string   `gorm:"size:80;uniqueIndex:uidx_fiat_order_trade"`
	VerifiedBy     string    `gorm:"size:40"`
	CreatedAt      time.Time `gorm:"not null;index"`
	PaidAt         *time.Time
	HarnessPolicy  string `gorm:"size:64"`
}

func (fiatOrderRow) TableName() string { return "fiat_payment_orders" }

type fiatStoreLock struct {
	ID uint `gorm:"primaryKey"`
}

func (fiatStoreLock) TableName() string { return "fiat_payment_store_locks" }

type fiatOutboxRow struct {
	EventID     string     `gorm:"primaryKey;size:128"`
	Payload     []byte     `gorm:"type:longblob;not null"`
	CreatedAt   time.Time  `gorm:"not null;index"`
	PublishedAt *time.Time `gorm:"index"`
}

func (fiatOutboxRow) TableName() string { return "fiat_payment_outbox" }

// Legacy JSON snapshots are read once for an in-place migration; new writes
// never touch this table.
type paymentSnapshot struct {
	ID      uint   `gorm:"primaryKey"`
	Payload []byte `gorm:"type:longblob;not null"`
}

func (paymentSnapshot) TableName() string { return "fiat_payment_snapshots" }

func NewSQLStore(db *gorm.DB, migrate bool) (*SQLStore, error) {
	if db == nil {
		return nil, errors.New("payment MySQL connection is required")
	}
	if migrate {
		if err := db.AutoMigrate(&fiatOrderRow{}, &fiatStoreLock{}, &fiatOutboxRow{}); err != nil {
			return nil, err
		}
	} else if !db.Migrator().HasTable(&fiatOrderRow{}) || !db.Migrator().HasTable(&fiatStoreLock{}) || !db.Migrator().HasTable(&fiatOutboxRow{}) {
		return nil, errors.New("fiat payment tables are missing; apply payment-service migrations before startup")
	}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&fiatStoreLock{ID: 1}).Error; err != nil {
		return nil, err
	}
	s := &SQLStore{db: db}
	if err := s.importLegacySnapshot(); err != nil {
		return nil, fmt.Errorf("import legacy fiat snapshot: %w", err)
	}
	return s, nil
}

func (s *SQLStore) importLegacySnapshot() error {
	if !s.db.Migrator().HasTable(&paymentSnapshot{}) {
		return nil
	}
	var snapshot paymentSnapshot
	if err := s.db.First(&snapshot, 1).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	var orders map[string]Order
	if err := json.Unmarshal(snapshot.Payload, &orders); err != nil {
		return err
	}
	for _, order := range orders {
		if order.ID == "" {
			continue
		}
		if order.IdempotencyKey == "" {
			order.IdempotencyKey = "legacy:" + order.RequestID
		}
		row := orderToRow(order)
		if err := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLStore) Update(change func(map[string]Order) error) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var lock fiatStoreLock
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lock, 1).Error; err != nil {
			return err
		}
		var rows []fiatOrderRow
		if err := tx.Find(&rows).Error; err != nil {
			return err
		}
		before, next := make(map[string]Order, len(rows)), make(map[string]Order, len(rows))
		for _, row := range rows {
			order := rowToOrder(row)
			before[order.ID] = order
			next[order.ID] = order
		}
		if err := change(next); err != nil {
			return err
		}
		for id, order := range next {
			prior, exists := before[id]
			if exists && reflect.DeepEqual(prior, order) {
				continue
			}
			row := orderToRow(order)
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"request_id", "idempotency_key", "product_id", "subject", "quote_hash", "approval_ref", "agent_did", "skill_did", "amount_minor", "currency", "mode", "status", "trade_no", "verified_by", "created_at", "paid_at", "harness_policy"})}).Create(&row).Error; err != nil {
				return err
			}
			if isTerminal(order.Status) && (!exists || prior.Status != order.Status) {
				if err := s.insertOutbox(tx, order); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s *SQLStore) insertOutbox(tx *gorm.DB, order Order) error {
	if order.AgentDID == "" || order.SkillDID == "" || order.AmountMinor <= 0 || order.IdempotencyKey == "" {
		return nil
	}
	eventType, status := "payment.success", "COMPLETED"
	if order.Status == "CLOSED" {
		eventType, status = "payment.failed", "CANCELLED"
	}
	now := time.Now().UTC()
	event := dto.MQPaymentEvent{EventID: "fiat-" + order.ID + "-" + status, EventType: eventType, SchemaVersion: 1,
		IdempotencyKey: order.IdempotencyKey, TxID: order.ID, AgentDID: order.AgentDID, SkillDID: order.SkillDID,
		AmountMinor: order.AmountMinor, Currency: "CNY", Status: status, OccurredAt: now.Format(time.RFC3339Nano), ConfirmedAt: "", RequestID: order.RequestID}
	if order.PaidAt != nil && order.Status == "PAID" {
		event.OccurredAt = order.PaidAt.UTC().Format(time.RFC3339Nano)
		event.ConfirmedAt = event.OccurredAt
	}
	payload, err := json.Marshal(&event)
	if err != nil {
		return err
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&fiatOutboxRow{EventID: event.EventID, Payload: payload, CreatedAt: now}).Error
}

func (s *SQLStore) PendingEvents(ctx context.Context, limit int) ([]dto.MQPaymentEvent, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	var rows []fiatOutboxRow
	if err := s.db.WithContext(ctx).Where("published_at IS NULL").Order("created_at ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	events := make([]dto.MQPaymentEvent, 0, len(rows))
	for _, row := range rows {
		var event dto.MQPaymentEvent
		if err := json.Unmarshal(row.Payload, &event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}
func (s *SQLStore) MarkEventPublished(ctx context.Context, eventID string) error {
	now := time.Now().UTC()
	return s.db.WithContext(ctx).Model(&fiatOutboxRow{}).Where("event_id = ? AND published_at IS NULL", eventID).Update("published_at", now).Error
}

func orderToRow(order Order) fiatOrderRow {
	var tradeNo *string
	if order.TradeNo != "" {
		value := order.TradeNo
		tradeNo = &value
	}
	return fiatOrderRow{ID: order.ID, RequestID: order.RequestID, IdempotencyKey: order.IdempotencyKey, ProductID: order.ProductID, Subject: order.Subject,
		QuoteHash: order.QuoteHash, ApprovalRef: order.ApprovalRef, AgentDID: order.AgentDID, SkillDID: order.SkillDID, AmountMinor: order.AmountMinor,
		Currency: order.Currency, Mode: order.Mode, Status: order.Status, TradeNo: tradeNo, VerifiedBy: order.VerifiedBy, CreatedAt: order.CreatedAt,
		PaidAt: order.PaidAt, HarnessPolicy: order.HarnessPolicy}
}
func rowToOrder(row fiatOrderRow) Order {
	order := Order{ID: row.ID, RequestID: row.RequestID, IdempotencyKey: row.IdempotencyKey, ProductID: row.ProductID, Subject: row.Subject,
		QuoteHash: row.QuoteHash, ApprovalRef: row.ApprovalRef, AgentDID: row.AgentDID, SkillDID: row.SkillDID, AmountMinor: row.AmountMinor,
		Currency: row.Currency, Mode: row.Mode, Status: row.Status, VerifiedBy: row.VerifiedBy, CreatedAt: row.CreatedAt, PaidAt: row.PaidAt, HarnessPolicy: row.HarnessPolicy}
	if row.TradeNo != nil {
		order.TradeNo = *row.TradeNo
	}
	return order
}
func isTerminal(status string) bool { return status == "PAID" || status == "CLOSED" }
