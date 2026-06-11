// Copyright 2025 StablePay. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package client 实现 COLA 架构的 Infrastructure 层外部服务调用。
//
// 职责：
// - 封装对 StablePay API Gateway 的 HTTP 调用
// - 与 Gate way 通信验证支付状态
// - 封装 x402 协议细节
package client

import (
	"context"
	"fmt"
)

// StablePayClient StablePay Gateway 客户端
//
// TODO: 后续步骤接入实际 HTTP 调用
// 使用 Hertz 的 HTTP 客户端或标准库 net/http
type StablePayClient struct {
	baseURL    string
	apiKey     string
	httpClient interface{} // TODO: 后续接入实际的 HTTP 客户端
}

// NewStablePayClient 创建 Gateway 客户端
func NewStablePayClient(baseURL, apiKey string) *StablePayClient {
	return &StablePayClient{
		baseURL: baseURL,
		apiKey:  apiKey,
	}
}

// VerifyPurchaseRequest Gateway 验证请求参数
type VerifyPurchaseRequest struct {
	AgentDid string
	SkillDid string
}

// VerifyPurchaseResponse Gateway 验证响应
type VerifyPurchaseResponse struct {
	Purchased bool   `json:"purchased"`
	TxID      string `json:"tx_id,omitempty"`
	TxHash    string `json:"tx_hash,omitempty"`
}

// VerifyPurchase 向 Gateway 验证购买状态
//
// GET /api/v1/verify?agent_did=<did>&skill_did=<skill_did>
// Header: X-API-Key
func (c *StablePayClient) VerifyPurchase(ctx context.Context, req *VerifyPurchaseRequest) (*VerifyPurchaseResponse, error) {
	// TODO: 后续步骤实现实际的 HTTP 请求
	// 当前返回骨架值
	_ = fmt.Sprintf("%s/api/v1/verify?agent_did=%s&skill_did=%s",
		c.baseURL, req.AgentDid, req.SkillDid)

	return &VerifyPurchaseResponse{
		Purchased: false, // 默认未购买
	}, nil
}

// SettlePayment 向 Gateway 结算支付
//
// POST /api/v1/pay
// TODO: 后续步骤实现
func (c *StablePayClient) SettlePayment(ctx context.Context, paymentData map[string]interface{}) error {
	// TODO: 后续步骤实现
	return nil
}
