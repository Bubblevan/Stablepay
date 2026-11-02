package machinepay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SQLStore struct{ db *gorm.DB }

type orderRow struct {
	ID             string     `gorm:"primaryKey;size:32"`
	IdempotencyKey string     `gorm:"size:128;not null;uniqueIndex:uidx_machinepay_idempotency,priority:2"`
	ResourceID     string     `gorm:"size:32;not null;uniqueIndex:uidx_machinepay_idempotency,priority:1"`
	GoodsName      string     `gorm:"size:32;not null"`
	AmountMinor    int64      `gorm:"not null"`
	PaymentLink    string     `gorm:"size:32;not null;default:''"`
	Status         string     `gorm:"size:32;not null;index"`
	TradeNo        *string    `gorm:"size:80;uniqueIndex:uidx_machinepay_trade"`
	CreatedAt      time.Time  `gorm:"not null;index"`
	ExpiresAt      time.Time  `gorm:"not null;index"`
	PaidAt         *time.Time `gorm:"index"`
}

func (orderRow) TableName() string { return "machine_pay_orders" }

type eventRow struct {
	EventID     string     `gorm:"primaryKey;size:128"`
	Payload     []byte     `gorm:"type:longblob;not null"`
	CreatedAt   time.Time  `gorm:"not null;index"`
	PublishedAt *time.Time `gorm:"index"`
}

func (eventRow) TableName() string { return "machine_pay_outbox" }

type SettledEvent struct {
	EventID     string `json:"event_id"`
	EventType   string `json:"event_type"`
	Schema      int    `json:"schema_version"`
	OrderID     string `json:"order_id"`
	TradeNo     string `json:"trade_no"`
	ResourceID  string `json:"resource_id"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
	OccurredAt  string `json:"occurred_at"`
}

func NewSQLStore(db *gorm.DB, migrate bool) (*SQLStore, error) {
	if db == nil {
		return nil, errors.New("Machine Pay MySQL connection is required")
	}
	if migrate {
		if err := db.AutoMigrate(&orderRow{}, &eventRow{}); err != nil {
			return nil, fmt.Errorf("migrate Machine Pay tables: %w", err)
		}
	} else if !db.Migrator().HasTable(&orderRow{}) || !db.Migrator().HasTable(&eventRow{}) {
		return nil, errors.New("Machine Pay tables are missing; apply payment-service migrations before startup")
	}
	return &SQLStore{db: db}, nil
}

func (s *SQLStore) GetOrCreate(ctx context.Context, key, resource, goods string, amount int64, created, expires time.Time) (Order, error) {
	id, err := newOrderID()
	if err != nil {
		return Order{}, err
	}
	row := orderRow{ID: id, IdempotencyKey: key, ResourceID: resource, GoodsName: goods, AmountMinor: amount,
		Status: "AWAITING_PAYMENT", CreatedAt: created, ExpiresAt: expires}
	err = s.db.WithContext(ctx).Create(&row).Error
	if err == nil {
		return rowToOrder(row), nil
	}
	var existing orderRow
	lookupErr := s.db.WithContext(ctx).Where("resource_id = ? AND idempotency_key = ?", resource, key).First(&existing).Error
	if lookupErr != nil {
		return Order{}, err
	}
	if existing.GoodsName != goods || existing.AmountMinor != amount || existing.ResourceID != resource || existing.ExpiresAt.Before(time.Now()) {
		return Order{}, ErrConflict
	}
	return rowToOrder(existing), nil
}

func (s *SQLStore) SavePaymentLink(ctx context.Context, id, link string) (Order, error) {
	if link == "" || len(link) > 32 {
		return Order{}, ErrConflict
	}
	var row orderRow
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", id).Error; err != nil {
			return err
		}
		if row.Status != "AWAITING_PAYMENT" || row.ExpiresAt.Before(time.Now()) {
			return ErrConflict
		}
		if row.PaymentLink != "" && row.PaymentLink != link {
			return ErrConflict
		}
		row.PaymentLink = link
		return tx.Model(&orderRow{}).Where("id = ? AND status = ?", id, "AWAITING_PAYMENT").Update("payment_link", link).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Order{}, ErrNotFound
	}
	return rowToOrder(row), err
}

func (s *SQLStore) Find(ctx context.Context, id string) (Order, error) {
	var row orderRow
	if err := s.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Order{}, ErrNotFound
		}
		return Order{}, err
	}
	return rowToOrder(row), nil
}

func (s *SQLStore) Settle(ctx context.Context, id string, receipt Receipt) (Order, error) {
	var result orderRow
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row orderRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if receipt.OrderID != row.ID || receipt.ResourceID != row.ResourceID || receipt.AmountMinor != row.AmountMinor || receipt.TradeNo == "" || !receipt.Active || receipt.VerifiedBy == "" {
			return ErrConflict
		}
		if row.Status == "PAID" {
			if row.TradeNo == nil || *row.TradeNo != receipt.TradeNo {
				return ErrConflict
			}
			result = row
			return nil
		}
		if row.Status != "AWAITING_PAYMENT" || row.ExpiresAt.Before(time.Now()) {
			return ErrConflict
		}
		now := time.Now().UTC()
		row.Status, row.TradeNo, row.PaidAt = "PAID", &receipt.TradeNo, &now
		if err := tx.Model(&orderRow{}).Where("id = ? AND status = ?", row.ID, "AWAITING_PAYMENT").Updates(map[string]any{
			"status": row.Status, "trade_no": row.TradeNo, "paid_at": row.PaidAt,
		}).Error; err != nil {
			return err
		}
		event := SettledEvent{EventID: "machinepay-" + row.ID + "-PAID", EventType: "machinepay.payment.succeeded", Schema: 1,
			OrderID: row.ID, TradeNo: receipt.TradeNo, ResourceID: row.ResourceID, AmountMinor: row.AmountMinor, Currency: "CNY", OccurredAt: now.Format(time.RFC3339Nano)}
		payload, err := json.Marshal(event)
		if err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&eventRow{EventID: event.EventID, Payload: payload, CreatedAt: now}).Error; err != nil {
			return err
		}
		result = row
		return nil
	})
	return rowToOrder(result), err
}

func (s *SQLStore) PendingEvents(ctx context.Context, limit int) ([]SettledEvent, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	var rows []eventRow
	if err := s.db.WithContext(ctx).Where("published_at IS NULL").Order("created_at ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]SettledEvent, 0, len(rows))
	for _, row := range rows {
		var event SettledEvent
		if err := json.Unmarshal(row.Payload, &event); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, nil
}

func (s *SQLStore) MarkEventPublished(ctx context.Context, eventID string) error {
	now := time.Now().UTC()
	return s.db.WithContext(ctx).Model(&eventRow{}).Where("event_id = ? AND published_at IS NULL", eventID).Update("published_at", now).Error
}

func rowToOrder(row orderRow) Order {
	order := Order{ID: row.ID, IdempotencyKey: row.IdempotencyKey, ResourceID: row.ResourceID, GoodsName: row.GoodsName,
		AmountMinor: row.AmountMinor, PaymentLink: row.PaymentLink, Status: row.Status, CreatedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt, PaidAt: row.PaidAt}
	if row.TradeNo != nil {
		order.TradeNo = *row.TradeNo
	}
	return order
}
