package agentshopping

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const simulatedFulfillmentProvider = "simulation-only"
const defaultMockFulfillmentMaxAttempts = 3

type MockFulfillmentFaultPlan struct {
	TransientFailures int
	PermanentFailure  bool
	MaxAttempts       int
}

func normalizeMockFulfillmentFaultPlan(plan MockFulfillmentFaultPlan) (MockFulfillmentFaultPlan, error) {
	if plan.MaxAttempts == 0 {
		plan.MaxAttempts = defaultMockFulfillmentMaxAttempts
	}
	if plan.MaxAttempts < 1 || plan.MaxAttempts > 10 || plan.TransientFailures < 0 ||
		plan.TransientFailures > plan.MaxAttempts || (plan.PermanentFailure && plan.TransientFailures > 0) {
		return MockFulfillmentFaultPlan{}, ErrInvalidSimulationData
	}
	return plan, nil
}

func mockFulfillmentAttemptOutcome(plan MockFulfillmentFaultPlan, attempt int) string {
	if plan.PermanentFailure {
		return FulfillmentAttemptPermanentFailure
	}
	if attempt <= plan.TransientFailures {
		if attempt >= plan.MaxAttempts {
			return FulfillmentAttemptPermanentFailure
		}
		return FulfillmentAttemptRetryableFailure
	}
	return FulfillmentAttemptDelivered
}

func (s *MySQLStore) fulfillmentFaultPlan(taskID string) (MockFulfillmentFaultPlan, error) {
	if plan, ok := s.mockFulfillmentFaults[taskID]; ok {
		return normalizeMockFulfillmentFaultPlan(plan)
	}
	if simulatedFulfillmentFailure(taskID) {
		return MockFulfillmentFaultPlan{PermanentFailure: true, MaxAttempts: 1}, nil
	}
	return MockFulfillmentFaultPlan{MaxAttempts: defaultMockFulfillmentMaxAttempts}, nil
}

// FulfillOrder runs the benchmark-only merchant fulfillment adapter for an
// already-paid order. Retryable provider attempts are durably recorded so the
// same idempotency key can resume after an API restart. Permanent failure writes
// a separate mock refund effect and restores inventory in the same transaction.
// No production Merchant, payment provider, or settlement ledger is called.
func (s *MySQLStore) FulfillOrder(ctx context.Context, orderID, idempotencyKey string) (FulfillmentResult, error) {
	orderID = strings.TrimSpace(orderID)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if orderID == "" || idempotencyKey == "" || len(idempotencyKey) > 128 {
		return FulfillmentResult{}, ErrInvalidSimulationData
	}

	var result FulfillmentResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var order OrderRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND order_id = ?", s.tenant, orderID).
			First(&order).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}

		var final MockFulfillmentRecord
		if err := tx.Where("tenant_id = ? AND order_id = ?", s.tenant, orderID).First(&final).Error; err == nil {
			if final.IdempotencyKey != idempotencyKey {
				return ErrIdempotencyConflict
			}
			var loadErr error
			result, loadErr = loadFulfillmentResult(tx, order, final)
			return loadErr
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		var previousAttempt MockFulfillmentAttemptRecord
		hasPreviousAttempt := false
		if err := tx.Where("tenant_id = ? AND order_id = ?", s.tenant, orderID).
			Order("attempt_number DESC").First(&previousAttempt).Error; err == nil {
			hasPreviousAttempt = true
			if previousAttempt.IdempotencyKey != idempotencyKey {
				return ErrIdempotencyConflict
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if order.Status != OrderPaid {
			return ErrOrderNotReady
		}

		var payment MockPaymentRecord
		if err := tx.Where("tenant_id = ? AND order_id = ?", s.tenant, orderID).First(&payment).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrOrderNotReady
			}
			return err
		}
		if payment.Status != PaymentSettled {
			return ErrOrderNotReady
		}

		var reservation InventoryReservationRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND order_id = ?", s.tenant, orderID).
			First(&reservation).Error; err != nil {
			return err
		}
		if reservation.Status != "CONSUMED" || reservation.Quantity != 1 {
			return ErrOrderNotReady
		}

		attemptNumber := 1
		var plan MockFulfillmentFaultPlan
		if hasPreviousAttempt {
			attemptNumber = previousAttempt.AttemptNumber + 1
			plan = MockFulfillmentFaultPlan{TransientFailures: previousAttempt.PlannedTransientFailures,
				PermanentFailure: previousAttempt.PermanentFailure, MaxAttempts: previousAttempt.MaxAttempts}
			if !s.now().UTC().Before(previousAttempt.NextAttemptAt) {
				// Resume the fault plan captured in the previous durable attempt;
				// a process-local test configuration is not authoritative after restart.
			} else {
				pending, loadErr := loadPendingFulfillmentResult(order, payment, previousAttempt, s.now().UTC())
				result = pending
				return loadErr
			}
		} else {
			var err error
			plan, err = s.fulfillmentFaultPlan(order.TaskID)
			if err != nil {
				return err
			}
		}
		if attemptNumber > plan.MaxAttempts {
			return fmt.Errorf("fulfillment retry exceeded configured attempt bound: %w", ErrInvalidSimulationData)
		}

		now := s.now().UTC()
		outcome := mockFulfillmentAttemptOutcome(plan, attemptNumber)
		attempt := MockFulfillmentAttemptRecord{AttemptID: StableID("mock-fulfillment-attempt", s.tenant, orderID,
			fmt.Sprint(attemptNumber)), TenantID: s.tenant, OrderID: orderID, AttemptNumber: attemptNumber,
			IdempotencyKey: idempotencyKey, Status: outcome, PlannedTransientFailures: plan.TransientFailures,
			PermanentFailure: plan.PermanentFailure, MaxAttempts: plan.MaxAttempts, Provider: simulatedFulfillmentProvider,
			NextAttemptAt: now, CreatedAt: now, DataOrigin: syntheticOrigin}
		if outcome == FulfillmentAttemptRetryableFailure {
			attempt.FailureCode = "TRANSIENT_DELIVERY_FAILURE"
			attempt.NextAttemptAt = now.Add(mockFulfillmentRetryDelay(attemptNumber))
			if err := tx.Create(&attempt).Error; err != nil {
				return err
			}
			if err := auditTx(tx, s.tenant, order.TaskID, attempt.AttemptID, "MOCK_FULFILLMENT_RETRY_SCHEDULED", now); err != nil {
				return err
			}
			result = pendingFulfillmentResult(order, payment, attempt, now)
			return nil
		}

		fulfillment := MockFulfillmentRecord{FulfillmentID: StableID("mock-fulfillment", s.tenant, orderID),
			TenantID: s.tenant, OrderID: orderID, IdempotencyKey: idempotencyKey,
			Provider: simulatedFulfillmentProvider, CreatedAt: now, CompletedAt: now,
			AttemptCount: attemptNumber, MaxAttempts: plan.MaxAttempts, DataOrigin: syntheticOrigin}
		if outcome == FulfillmentAttemptPermanentFailure {
			fulfillment.Status = FulfillmentCompensated
			fulfillment.FailureCode = "SIMULATED_DELIVERY_FAILURE"
			if !plan.PermanentFailure && attemptNumber >= plan.MaxAttempts && attemptNumber <= plan.TransientFailures {
				fulfillment.FailureCode = "TRANSIENT_RETRY_EXHAUSTED"
			}
			attempt.FailureCode = fulfillment.FailureCode
			if err := compensateMockFulfillment(tx, s.tenant, &order, payment, &reservation, now); err != nil {
				return err
			}
			if err := auditTx(tx, s.tenant, order.TaskID, fulfillment.FulfillmentID, "MOCK_FULFILLMENT_FAILED", now); err != nil {
				return err
			}
		} else if outcome == FulfillmentAttemptDelivered {
			fulfillment.Status = FulfillmentDelivered
			fulfillment.DeliveryReference = StableID("mock-delivery", s.tenant, orderID)
			order.Status = OrderFulfilled
			order.CompletedAt = now
			if err := auditTx(tx, s.tenant, order.TaskID, fulfillment.FulfillmentID, "MOCK_FULFILLMENT_DELIVERED", now); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("unknown simulated fulfillment outcome %q: %w", outcome, ErrInvalidSimulationData)
		}
		if err := tx.Create(&attempt).Error; err != nil {
			return err
		}
		if err := tx.Create(&fulfillment).Error; err != nil {
			return err
		}
		if err := tx.Save(&order).Error; err != nil {
			return err
		}
		if err := updateTaskStatusTx(tx, s.tenant, order.TaskID, order.Status); err != nil {
			return err
		}
		if err := auditTx(tx, s.tenant, order.TaskID, order.OrderID, order.Status, now); err != nil {
			return err
		}
		loaded, loadErr := loadFulfillmentResult(tx, order, fulfillment)
		result = loaded
		return loadErr
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return FulfillmentResult{}, ErrIdempotencyConflict
	}
	return result, err
}

func (s *MySQLStore) GetFulfillment(ctx context.Context, orderID string) (FulfillmentResult, error) {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return FulfillmentResult{}, ErrInvalidSimulationData
	}
	var order OrderRecord
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND order_id = ?", s.tenant, orderID).First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return FulfillmentResult{}, ErrNotFound
		}
		return FulfillmentResult{}, err
	}
	var fulfillment MockFulfillmentRecord
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND order_id = ?", s.tenant, orderID).First(&fulfillment).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			var attempt MockFulfillmentAttemptRecord
			if attemptErr := s.db.WithContext(ctx).Where("tenant_id = ? AND order_id = ?", s.tenant, orderID).
				Order("attempt_number DESC").First(&attempt).Error; attemptErr != nil {
				if errors.Is(attemptErr, gorm.ErrRecordNotFound) {
					return FulfillmentResult{}, ErrNotFound
				}
				return FulfillmentResult{}, attemptErr
			}
			var payment MockPaymentRecord
			if err := s.db.WithContext(ctx).Where("tenant_id = ? AND order_id = ?", s.tenant, orderID).First(&payment).Error; err != nil {
				return FulfillmentResult{}, err
			}
			return loadPendingFulfillmentResult(order, payment, attempt, s.now().UTC())
		}
		return FulfillmentResult{}, err
	}
	return loadFulfillmentResult(s.db.WithContext(ctx), order, fulfillment)
}

func mockFulfillmentRetryDelay(attemptNumber int) time.Duration {
	if attemptNumber <= 1 {
		return 5 * time.Millisecond
	}
	delay := 5 * time.Millisecond * time.Duration(1<<min(attemptNumber-1, 7))
	if delay > time.Second {
		return time.Second
	}
	return delay
}

func pendingFulfillmentResult(order OrderRecord, payment MockPaymentRecord, attempt MockFulfillmentAttemptRecord, now time.Time) FulfillmentResult {
	return FulfillmentResult{Order: orderFromRecord(order), Payment: paymentFromRecord(payment),
		Fulfillment: Fulfillment{ID: StableID("mock-fulfillment", attempt.TenantID, attempt.OrderID),
			TenantID: attempt.TenantID, OrderID: attempt.OrderID, IdempotencyKey: attempt.IdempotencyKey,
			Status: FulfillmentRetryPending, Provider: attempt.Provider, FailureCode: attempt.FailureCode,
			AttemptCount: attempt.AttemptNumber, MaxAttempts: attempt.MaxAttempts,
			Retryable: true, RetryAfterMillis: retryAfterMillis(attempt.NextAttemptAt, now),
			SimulationOnly: true, CreatedAt: attempt.CreatedAt}}
}

func retryAfterMillis(nextAttemptAt, now time.Time) int64 {
	remaining := nextAttemptAt.Sub(now)
	if remaining <= 0 {
		return 0
	}
	return int64((remaining + time.Millisecond - 1) / time.Millisecond)
}

func loadPendingFulfillmentResult(order OrderRecord, payment MockPaymentRecord,
	attempt MockFulfillmentAttemptRecord, now time.Time) (FulfillmentResult, error) {
	if attempt.Status != FulfillmentAttemptRetryableFailure {
		return FulfillmentResult{}, fmt.Errorf("no pending fulfillment retry for order %s: %w", order.OrderID, ErrNotFound)
	}
	return pendingFulfillmentResult(order, payment, attempt, now), nil
}

func loadFulfillmentResult(db *gorm.DB, order OrderRecord, fulfillment MockFulfillmentRecord) (FulfillmentResult, error) {
	var payment MockPaymentRecord
	if err := db.Where("tenant_id = ? AND order_id = ?", order.TenantID, order.OrderID).First(&payment).Error; err != nil {
		return FulfillmentResult{}, err
	}
	result := FulfillmentResult{Order: orderFromRecord(order), Payment: paymentFromRecord(payment),
		Fulfillment: fulfillmentFromRecord(fulfillment)}
	var refund MockRefundRecord
	if err := db.Where("tenant_id = ? AND order_id = ?", order.TenantID, order.OrderID).First(&refund).Error; err == nil {
		converted := refundFromRecord(refund)
		result.Refund = &converted
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return FulfillmentResult{}, err
	}
	return result, nil
}

func compensateMockFulfillment(tx *gorm.DB, tenant string, order *OrderRecord, payment MockPaymentRecord,
	reservation *InventoryReservationRecord, now time.Time) error {
	var inventory InventoryRecord
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND merchant_id = ? AND sku_id = ? AND region = ?",
		tenant, reservation.MerchantID, reservation.SKUID, reservation.Region).First(&inventory).Error; err != nil {
		return err
	}
	if reservation.Quantity <= 0 || inventory.Reserved < 0 || inventory.OnHand < 0 ||
		inventory.OnHand < inventory.Reserved-reservation.Quantity {
		return fmt.Errorf("inventory state cannot be safely restored for order %s: %w", order.OrderID, ErrInvalidSimulationData)
	}
	inventory.OnHand += reservation.Quantity
	inventory.Revision++
	inventory.UpdatedAt = now
	if err := tx.Save(&inventory).Error; err != nil {
		return err
	}
	reservation.Status = "RETURNED"
	reservation.CompletedAt = now
	if err := tx.Save(reservation).Error; err != nil {
		return err
	}
	refund := MockRefundRecord{RefundID: StableID("mock-refund", tenant, payment.PaymentID), TenantID: tenant,
		OrderID: order.OrderID, PaymentID: payment.PaymentID, AmountMinor: payment.AmountMinor, Currency: payment.Currency,
		Provider: simulatedFulfillmentProvider, Status: RefundCompleted,
		IdempotencyKey:  StableID("mock-refund-idempotency", tenant, payment.PaymentID),
		RefundReference: StableID("mock-refund-reference", tenant, payment.PaymentID), CreatedAt: now, DataOrigin: syntheticOrigin}
	if err := tx.Create(&refund).Error; err != nil {
		return err
	}
	order.Status = OrderRefunded
	order.CompletedAt = now
	if err := auditTx(tx, tenant, order.TaskID, refund.RefundID, "MOCK_REFUND_ISSUED", now); err != nil {
		return err
	}
	return auditTx(tx, tenant, order.TaskID, reservation.ReservationID, "INVENTORY_RESTOCKED", now)
}

func simulatedFulfillmentFailure(taskID string) bool {
	digest := sha256.Sum256([]byte("stablepay-mock-fulfillment|" + taskID))
	return binary.BigEndian.Uint16(digest[:2])%100 < 10
}

func orderFromRecord(record OrderRecord) Order {
	return Order{ID: record.OrderID, TenantID: record.TenantID, TaskID: record.TaskID, BuyerID: record.BuyerID,
		QuoteID: record.QuoteID, ConfirmationID: record.ConfirmationID, IdempotencyKey: record.IdempotencyKey,
		AmountMinor: record.AmountMinor, Currency: record.Currency, Status: record.Status,
		CreatedAt: record.CreatedAt, CompletedAt: record.CompletedAt}
}

func fulfillmentFromRecord(record MockFulfillmentRecord) Fulfillment {
	return Fulfillment{ID: record.FulfillmentID, TenantID: record.TenantID, OrderID: record.OrderID,
		IdempotencyKey: record.IdempotencyKey, Status: record.Status, Provider: record.Provider,
		DeliveryReference: record.DeliveryReference, FailureCode: record.FailureCode,
		AttemptCount: record.AttemptCount, MaxAttempts: record.MaxAttempts, SimulationOnly: true,
		CreatedAt: record.CreatedAt, CompletedAt: &record.CompletedAt}
}

func refundFromRecord(record MockRefundRecord) MockRefund {
	return MockRefund{ID: record.RefundID, TenantID: record.TenantID, OrderID: record.OrderID,
		PaymentID: record.PaymentID, AmountMinor: record.AmountMinor, Currency: record.Currency,
		Provider: record.Provider, Status: record.Status, IdempotencyKey: record.IdempotencyKey,
		RefundReference: record.RefundReference, SimulationOnly: true, CreatedAt: record.CreatedAt}
}
