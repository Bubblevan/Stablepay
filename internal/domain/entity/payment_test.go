// Package entity 领域实体单元测试
package entity

import (
	"testing"
	"time"

	"github.com/stablepay/payment-service/pkg/constants"
)

func TestPayment_TransitionTo(t *testing.T) {
	payment, _ := NewPayment(
		"test-tx-id",
		"did:solana:agent123",
		"did:solana:skill456",
		5000000,
		constants.CurrencyUSDC,
		"signature123",
		1234567890,
		"nonce123",
		5,
	)

	tests := []struct {
		name      string
		newStatus constants.PaymentStatus
		wantErr   bool
	}{
		{
			name:      "CREATED -> PENDING",
			newStatus: constants.PaymentStatusPending,
			wantErr:   false,
		},
		{
			name:      "PENDING -> CONFIRMED",
			newStatus: constants.PaymentStatusConfirmed,
			wantErr:   false,
		},
		{
			name:      "CONFIRMED -> COMPLETED",
			newStatus: constants.PaymentStatusCompleted,
			wantErr:   false,
		},
		{
			name:      "COMPLETED -> FAILED (非法)",
			newStatus: constants.PaymentStatusFailed,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 重置为初始状态
			payment.Status = constants.PaymentStatusCreated

			// 按路径转换到合适的状态
			switch tt.name {
			case "PENDING -> CONFIRMED":
				_ = payment.TransitionTo(constants.PaymentStatusPending)
			case "CONFIRMED -> COMPLETED":
				_ = payment.TransitionTo(constants.PaymentStatusPending)
				_ = payment.TransitionTo(constants.PaymentStatusConfirmed)
			case "COMPLETED -> FAILED (非法)":
				_ = payment.TransitionTo(constants.PaymentStatusPending)
				_ = payment.TransitionTo(constants.PaymentStatusConfirmed)
				_ = payment.TransitionTo(constants.PaymentStatusCompleted)
			}

			err := payment.TransitionTo(tt.newStatus)
			if (err != nil) != tt.wantErr {
				t.Errorf("TransitionTo() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPayment_IsExpired(t *testing.T) {
	// 创建一个已经过期的支付
	expiredPayment, _ := NewPayment(
		"test-tx-id",
		"did:solana:agent123",
		"did:solana:skill456",
		5000000,
		constants.CurrencyUSDC,
		"signature123",
		1234567890,
		"nonce123",
		-1, // 已过期
	)
	expiredPayment.ExpiresAt = time.Now().Add(-1 * time.Hour)

	// 创建一个未过期的支付
	validPayment, _ := NewPayment(
		"test-tx-id-2",
		"did:solana:agent123",
		"did:solana:skill456",
		5000000,
		constants.CurrencyUSDC,
		"signature123",
		1234567890,
		"nonce123",
		60, // 60分钟后过期
	)

	if !expiredPayment.IsExpired() {
		t.Error("IsExpired() should return true for expired payment")
	}

	if validPayment.IsExpired() {
		t.Error("IsExpired() should return false for valid payment")
	}
}

func TestPayment_CanRetry(t *testing.T) {
	payment, _ := NewPayment(
		"test-tx-id",
		"did:solana:agent123",
		"did:solana:skill456",
		5000000,
		constants.CurrencyUSDC,
		"signature123",
		1234567890,
		"nonce123",
		5,
	)

	// 非失败状态不能重试
	if payment.CanRetry(3) {
		t.Error("CanRetry() should return false for non-failed payment")
	}

	// 设置为失败状态
	_ = payment.TransitionTo(constants.PaymentStatusFailed)

	// 未超过最大重试次数可以重试
	if !payment.CanRetry(3) {
		t.Error("CanRetry() should return true when retry count < max")
	}

	// 增加重试次数
	payment.RetryCount = 3
	if payment.CanRetry(3) {
		t.Error("CanRetry() should return false when retry count >= max")
	}
}

func TestPayment_GetIdempotencyKey(t *testing.T) {
	payment, _ := NewPayment(
		"test-tx-id",
		"did:solana:agent123",
		"did:solana:skill456",
		5000000,
		constants.CurrencyUSDC,
		"signature123",
		1234567890,
		"nonce123",
		5,
	)

	expectedKey := "did:solana:agent123:did:solana:skill456:nonce123"
	if key := payment.GetIdempotencyKey(); key != expectedKey {
		t.Errorf("GetIdempotencyKey() = %v, want %v", key, expectedKey)
	}
}
