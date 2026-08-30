package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type RewardPayoutResult struct {
	TxID   string
	TxHash string
}

type rewardPayoutRequest struct {
	AgentDid       string `json:"agent_did"`
	WalletAddress  string `json:"wallet_address"`
	AmountMinor    int64  `json:"amount_minor"`
	Currency       string `json:"currency"`
	IdempotencyKey string `json:"idempotency_key"`
	Purpose        string `json:"purpose"`
}

type rewardPayoutResponse struct {
	Code    int                    `json:"code"`
	Message string                 `json:"message"`
	Data    map[string]interface{} `json:"data"`
}

// sendRegistrationReward transfers registration USDC via payment-service or synthetic mode (dev only).
func sendRegistrationReward(ctx context.Context, agentDid, walletAddress string, amountMinor int64, idempotencyKey string) (*RewardPayoutResult, error) {
	mode := strings.ToLower(strings.TrimSpace(envOrDefault("REWARD_PAYOUT_MODE", "payment_service")))
	switch mode {
	case "synthetic":
		if strings.ToLower(envOrDefault("ALLOW_SYNTHETIC_REWARD", "false")) != "true" {
			return nil, fmt.Errorf("synthetic reward disabled; set ALLOW_SYNTHETIC_REWARD=true for local dev")
		}
		txID := "x-reward-synthetic-" + idempotencyKey
		return &RewardPayoutResult{TxID: txID, TxHash: txID}, nil
	case "payment_service":
		return callPaymentServiceReward(ctx, agentDid, walletAddress, amountMinor, idempotencyKey)
	default:
		return nil, fmt.Errorf("unsupported REWARD_PAYOUT_MODE %q", mode)
	}
}

func callPaymentServiceReward(ctx context.Context, agentDid, walletAddress string, amountMinor int64, idempotencyKey string) (*RewardPayoutResult, error) {
	base := strings.TrimRight(envOrDefault("PAYMENT_SERVICE_URL", "http://payment-service:8080"), "/")
	url := base + "/api/v1/internal/rewards/x-registration"

	body, err := json.Marshal(rewardPayoutRequest{
		AgentDid:       agentDid,
		WalletAddress:  walletAddress,
		AmountMinor:    amountMinor,
		Currency:       "USDC",
		IdempotencyKey: idempotencyKey,
		Purpose:        "x_registration_reward",
	})
	if err != nil {
		return nil, err
	}

	timeout := 60 * time.Second
	if v := envOrDefault("REWARD_PAYOUT_TIMEOUT_SEC", "60"); v != "" {
		if sec, parseErr := time.ParseDuration(v + "s"); parseErr == nil {
			timeout = sec
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Idempotency-Key", idempotencyKey)
	if apiKey := envOrDefault("PAYMENT_SERVICE_API_KEY", ""); apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	client := &http.Client{Timeout: timeout}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("payment-service reward request failed: %w", err)
	}
	defer res.Body.Close()

	raw, _ := io.ReadAll(res.Body)
	var envelope rewardPayoutResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("payment-service reward invalid JSON (http %d): %s", res.StatusCode, string(raw))
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 || envelope.Code != 0 {
		msg := envelope.Message
		if msg == "" {
			msg = string(raw)
		}
		return nil, fmt.Errorf("payment-service reward failed (http %d): %s", res.StatusCode, msg)
	}

	txID := stringFromMap(envelope.Data, "tx_id", "transaction_id", "reward_tx_id")
	txHash := stringFromMap(envelope.Data, "tx_hash", "reward_tx", "signature")
	if txID == "" {
		txID = idempotencyKey
	}
	if txHash == "" {
		txHash = txID
	}
	return &RewardPayoutResult{TxID: txID, TxHash: txHash}, nil
}

func stringFromMap(data map[string]interface{}, keys ...string) string {
	if data == nil {
		return ""
	}
	for _, key := range keys {
		if v, ok := data[key]; ok && v != nil {
			s := fmt.Sprintf("%v", v)
			if strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return ""
}
