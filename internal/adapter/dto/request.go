// Copyright 2025 StablePay. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dto

// -------------------- 商品查询参数 --------------------

// ProductListReq represents query params for GET /api/v1/products.
type ProductListReq struct {
	Page     int    `query:"page"`
	Size     int    `query:"size"`
	Category string `query:"category,omitempty"`
	Keyword  string `query:"keyword,omitempty"`
}

// ProductDetailReq represents path/query params for product detail.
type ProductDetailReq struct {
	ID string `path:"id"`
}

// -------------------- 购买执行参数 --------------------

// PurchaseExecuteReq represents HTTP input for GET /api/v1/products/:id/execute.
//
// AgentDID comes from query string for MVP simplicity. PaymentSignature is the
// x402 v2 PAYMENT-SIGNATURE request header sent by the client after payment.
type PurchaseExecuteReq struct {
	AgentDID         string `query:"agent_did" validate:"required"`
	PaymentSignature string `header:"PAYMENT-SIGNATURE"`
}

// -------------------- 分页默认值 --------------------

const (
	DefaultPage = 1
	DefaultSize = 20
	MaxSize     = 100
)
