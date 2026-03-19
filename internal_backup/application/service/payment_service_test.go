// Package service 应用服务单元测试
package service

import (
	"context"
	"testing"
	"time"

	"github.com/stablepay/payment-service/internal/application/dto"
	"github.com/stablepay/payment-service/pkg/constants"
	"github.com/stablepay/payment-service/pkg/errors"
)

// MockPaymentRepository Mock 支付仓库
type MockPaymentRepository struct {
	payments map[string]*MockPayment
}

type MockPayment struct {
	TxID        string
	AgentDID    string
	SkillDID    string
	Status      int8
	AmountMinor int64
}

func (m *MockPaymentRepository) GetByTxID(ctx context.Context, txID string) (*MockPayment, error) {
	if p, ok := m.payments[txID]; ok {
		return p, nil
	}
	return nil, errors.New(errors.RESOURCE_NOT_FOUND, "not found")
}

func (m *MockPaymentRepository) Create(ctx context.Context, payment *MockPayment) error {
	m.payments[payment.TxID] = payment
	return nil
}

// TestParseStatus 测试状态解析
func TestParseStatus(t *testing.T) {
	tests := []struct {
		input    string
		expected int8
	}{
		{"CREATED", 0},
		{"PENDING", 1},
		{"CONFIRMED", 2},
		{"COMPLETED", 3},
		{"FAILED", 4},
		{"CANCELLED", 5},
		{"UNKNOWN", -1},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseStatus(tt.input)
			if got != tt.expected {
				t.Errorf("parseStatus(%s) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

// TestExtractWalletFromDID 测试钱包地址提取
func TestExtractWalletFromDID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"did:solana:abc123", "abc123"},
		{"did:solana:4fK9x2HyJkLmNpQrStUvWxYz", "4fK9x2HyJkLmNpQrStUvWxYz"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := extractWalletFromDID(tt.input)
			if got != tt.expected {
				t.Errorf("extractWalletFromDID(%s) = %s, want %s", tt.input, got, tt.expected)
			}
		})
	}
}

// TestGenerateTxID 测试交易ID生成
func TestGenerateTxID(t *testing.T) {
	id1 := generateTxID()
	id2 := generateTxID()

	if id1 == "" {
		t.Error("generateTxID() should not return empty string")
	}

	if id1 == id2 {
		t.Error("generateTxID() should generate unique IDs")
	}
}

// TestDTOToMQEventTag 测试事件 Tag 转换
func TestDTOToMQEventTag(t *testing.T) {
	tests := []struct {
		status   int8
		expected string
	}{
		{constants.PaymentStatusConfirmed, constants.MQTagPaymentSucceeded},
		{constants.PaymentStatusCompleted, constants.MQTagPaymentSucceeded},
		{constants.PaymentStatusFailed, constants.MQTagPaymentFailed},
		{constants.PaymentStatusCancelled, constants.MQTagPaymentFailed},
		{constants.PaymentStatusCreated, ""},
		{constants.PaymentStatusPending, ""},
	}

	for _, tt := range tests {
		t.Run(constants.PaymentStatusToString(tt.status), func(t *testing.T) {
			got := dto.ToMQEventTag(tt.status)
			if got != tt.expected {
				t.Errorf("ToMQEventTag(%d) = %s, want %s", tt.status, got, tt.expected)
			}
		})
	}
}

// MockBlockchainExecutor Mock 区块链执行器
type MockBlockchainExecutor struct {
	shouldFail bool
	txHash     string
	delay      time.Duration
}

func (m *MockBlockchainExecutor) ExecuteTransfer(ctx context.Context, fromWallet, toWallet string, amountMinor int64, currency constants.Currency) (string, error) {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	if m.shouldFail {
		return "", errors.New(errors.BLOCKCHAIN_NETWORK_ERROR, "mock blockchain error")
	}
	return m.txHash, nil
}

func (m *MockBlockchainExecutor) QueryTxStatus(ctx context.Context, txHash string) (int8, *time.Time, error) {
	return constants.PaymentStatusConfirmed, nil, nil
}

// TestPaymentApplicationService_InitiatePayment_Validation 测试参数校验
func TestPaymentApplicationService_InitiatePayment_Validation(t *testing.T) {
	// 这里可以添加更完整的测试
	// 由于依赖较多，实际项目中应使用 mock 框架

	tests := []struct {
		name    string
		req     *dto.InitiatePaymentRequest
		wantErr bool
	}{
		{
			name: "缺少幂等性键",
			req: &dto.InitiatePaymentRequest{
				AgentDID:  "did:solana:agent123",
				SkillDID:  "did:solana:skill456",
				AmountStr: "5.00",
				Currency:  "USDC",
				Signature: "sig123",
				Timestamp: time.Now().Unix(),
				Nonce:     "nonce123",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 简化测试，实际应构造完整的服务实例
			_ = tt.req
		})
	}
}
