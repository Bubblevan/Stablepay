package clients

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"stablepay/api-gateway/internal/application"
)

type MockDIDClient struct{}

func NewMockDIDClient() *MockDIDClient { return &MockDIDClient{} }

func (m *MockDIDClient) CreateDID(_ context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	return map[string]interface{}{
		"did":            "did:solana:" + uuid.NewString(),
		"public_key":     "mock_public_key",
		"wallet_address": "mock_wallet_address",
		"user_type":      req["user_type"],
		"created_at":     time.Now().UTC().Format(time.RFC3339),
	}, 200, 0, nil
}

func (m *MockDIDClient) RegisterDID(_ context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	publicKey := fmt.Sprintf("%v", req["public_key"])
	if publicKey == "" {
		publicKey = "mock_public_key"
	}
	walletAddress := fmt.Sprintf("%v", req["wallet_address"])
	if walletAddress == "" {
		walletAddress = publicKey
	}
	return map[string]interface{}{
		"did":            "did:solana:" + publicKey,
		"public_key":     publicKey,
		"wallet_address": walletAddress,
		"wallet_id":      req["wallet_id"],
		"status":         "active",
		"created_at":     time.Now().UTC().Format(time.RFC3339),
	}, 200, 0, nil
}

func (m *MockDIDClient) VerifyDID(_ context.Context, _ map[string]interface{}) (map[string]interface{}, int, int, error) {
	return map[string]interface{}{"valid": true}, 200, 0, nil
}

func (m *MockDIDClient) GetDID(_ context.Context, did string) (map[string]interface{}, int, int, error) {
	return map[string]interface{}{
		"did":            did,
		"public_key":     "mock_public_key",
		"wallet_address": "mock_wallet_address",
	}, 200, 0, nil
}

func (m *MockDIDClient) VerifySignature(_ context.Context, req map[string]interface{}) (bool, error) {
	return req["signature"] != "", nil
}

type MockPaymentClient struct{}

func NewMockPaymentClient() *MockPaymentClient { return &MockPaymentClient{} }

func (m *MockPaymentClient) Pay(_ context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	return map[string]interface{}{
		"tx_id":        "pay_" + uuid.NewString(),
		"tx_hash":      "mock_tx_hash",
		"status":       "confirmed",
		"confirmed_at": time.Now().UTC().Format(time.RFC3339),
		"agent_did":    req["agent_did"],
		"skill_did":    req["skill_did"],
	}, 200, 0, nil
}

func (m *MockPaymentClient) GetPaymentRequirement(_ context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	message := "Payment required to access this skill"
	if raw, ok := req["message"]; ok && raw != nil {
		if str := fmt.Sprintf("%v", raw); str != "" {
			message = str
		}
	}
	price := "1.00"
	if raw, ok := req["price"]; ok && raw != nil {
		if str := fmt.Sprintf("%v", raw); str != "" {
			price = str
		}
	}
	currency := "USDC"
	if raw, ok := req["currency"]; ok && raw != nil {
		if str := fmt.Sprintf("%v", raw); str != "" {
			currency = str
		}
	}
	return map[string]interface{}{
		"skill_did":        req["skill_did"],
		"skill_name":       req["skill_name"],
		"price":            price,
		"currency":         currency,
		"message":          message,
		"payment_endpoint": "/api/v1/pay",
	}, 402, 402, nil
}

func (m *MockPaymentClient) GetPayment(_ context.Context, txID string) (map[string]interface{}, int, int, error) {
	return map[string]interface{}{
		"tx_id":        txID,
		"tx_hash":      "mock_tx_hash",
		"status":       "confirmed",
		"confirmed_at": time.Now().UTC().Format(time.RFC3339),
	}, 200, 0, nil
}

func (m *MockPaymentClient) GetPaymentHistory(_ context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	return map[string]interface{}{
		"items": []map[string]interface{}{
			{
				"tx_id":      "pay_" + uuid.NewString(),
				"skill_did":  req["skill_did"],
				"amount":     "5.00",
				"currency":   "USDC",
				"status":     "confirmed",
				"created_at": time.Now().UTC().Format(time.RFC3339),
			},
		},
		"total": 1,
	}, 200, 0, nil
}

type MockVerificationClient struct{}

func NewMockVerificationClient() *MockVerificationClient { return &MockVerificationClient{} }

func (m *MockVerificationClient) Verify(_ context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	return map[string]interface{}{
		"purchased":     true,
		"purchase_time": time.Now().UTC().Format(time.RFC3339),
		"tx_id":         "pay_" + uuid.NewString(),
		"amount":        "5.00",
		"currency":      "USDC",
		"agent_did":     req["agent_did"],
		"skill_did":     req["skill_did"],
	}, 200, 0, nil
}

func (m *MockVerificationClient) BatchVerify(_ context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	return map[string]interface{}{
		"items": []map[string]interface{}{
			{"skill_did": "did:solana:skill1", "purchased": true},
			{"skill_did": "did:solana:skill2", "purchased": false},
		},
		"agent_did": req["agent_did"],
	}, 200, 0, nil
}

func (m *MockVerificationClient) GetProof(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	data, httpStatus, code, err := m.Verify(ctx, req)
	if err != nil {
		return nil, httpStatus, code, err
	}
	data["tx_hash"] = "mock_tx_hash"
	data["proof_version"] = "v0.1"
	return data, 200, 0, nil
}

type MockQueryClient struct{}

func NewMockQueryClient() *MockQueryClient { return &MockQueryClient{} }

func (m *MockQueryClient) GetBalance(_ context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	return map[string]interface{}{
		"balance":       "25.50",
		"currency":      "USDC",
		"monthly_spent": "24.50",
		"monthly_limit": "50.00",
		"agent_did":     req["agent_did"],
	}, 200, 0, nil
}

func (m *MockQueryClient) GetTransactions(_ context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	return map[string]interface{}{
		"items": []map[string]interface{}{
			{"tx_id": "pay_" + uuid.NewString(), "type": req["type"], "amount": "5.00", "currency": "USDC"},
		},
		"total": 1,
	}, 200, 0, nil
}

func (m *MockQueryClient) GetRevenue(_ context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	return map[string]interface{}{
		"skill_did":      req["skill_did"],
		"total_revenue":  "120.00",
		"total_sales":    24,
		"currency":       "USDC",
		"sales_trend":    []map[string]interface{}{{"date": "2026-02-13", "amount": "15.00"}},
		"report_version": "v0.1",
	}, 200, 0, nil
}

func (m *MockQueryClient) GetSales(_ context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	return map[string]interface{}{
		"skill_did": req["skill_did"],
		"items": []map[string]interface{}{
			{
				"tx_id":        "sale_" + uuid.NewString(),
				"agent_did":    "did:solana:mock-agent",
				"skill_did":    req["skill_did"],
				"amount_minor": 1000000,
				"currency":     "USDC",
				"created_at":   time.Now().UTC().Format(time.RFC3339),
			},
		},
		"total": 1,
	}, 200, 0, nil
}

var (
	_ application.DIDServiceClient          = (*MockDIDClient)(nil)
	_ application.PaymentServiceClient      = (*MockPaymentClient)(nil)
	_ application.VerificationServiceClient = (*MockVerificationClient)(nil)
	_ application.QueryServiceClient        = (*MockQueryClient)(nil)
)
