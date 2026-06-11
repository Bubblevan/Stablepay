// Copyright 2025 StablePay. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dto

// -------------------- 商品查询参数 --------------------

// ProductListReq 商品列表查询参数
type ProductListReq struct {
	Page     int    `query:"page"`
	Size     int    `query:"size"`
	Category string `query:"category,omitempty"`
	Keyword  string `query:"keyword,omitempty"`
}

// ProductDetailReq 商品详情查询参数
type ProductDetailReq struct {
	ID string `query:"id"` // 商品 ID
}

// -------------------- 购买执行参数 --------------------

// PurchaseExecuteReq 执行购买请求参数
type PurchaseExecuteReq struct {
	AgentDid string `query:"agent_did" validate:"required"`
	// 可选：x402 Payment-Signature header (Base64 编码的 JSON)
	// 由客户端在支付后附带，商户后端将其提交给 Gateway 验证
	PaymentSignature string `header:"Payment-Signature"`
}

// -------------------- 分页默认值 --------------------

const (
	DefaultPage = 1
	DefaultSize = 20
	MaxSize     = 100
)
