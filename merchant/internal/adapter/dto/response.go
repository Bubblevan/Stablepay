// Copyright 2025 StablePay. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package dto (Data Transfer Object) belongs to the COLA-style Adapter layer.
//
// DTOs define the HTTP API contract. They are intentionally separate from
// Domain entities and Application read models so that transport shape changes do
// not leak backward into business rules.
package dto

import "time"

// -------------------- 基础响应 --------------------

// BaseResponse is the common API response envelope used by non-x402 responses.
type BaseResponse struct {
	OK        bool   `json:"ok"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

// Success builds a successful response envelope.
func Success(msg string) BaseResponse {
	return BaseResponse{
		OK:        true,
		Message:   msg,
		Timestamp: time.Now().Format(time.RFC3339),
	}
}

// Error builds a failed response envelope.
func Error(code, msg string) BaseResponse {
	return BaseResponse{
		OK:        false,
		Code:      code,
		Message:   msg,
		Timestamp: time.Now().Format(time.RFC3339),
	}
}

// -------------------- 健康检查 --------------------

// HealthResp is the health-check response.
type HealthResp struct {
	BaseResponse
	Service string `json:"service"`
	Version string `json:"version,omitempty"`
	DBReady bool   `json:"db_ready"`
}

// -------------------- 数据分页 --------------------

// Pagination describes list pagination.
type Pagination struct {
	Total int64 `json:"total"`
	Page  int   `json:"page"`
	Size  int   `json:"size"`
}

// -------------------- 商品相关 DTO --------------------

// ProductItem is the public product view returned by HTTP APIs.
type ProductItem struct {
	ID          string   `json:"id"`
	SKUID       string   `json:"sku_id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	ImageURL    string   `json:"image_url,omitempty"`
	Price       string   `json:"price"`
	Currency    string   `json:"currency"`
	Author      string   `json:"author,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Status      string   `json:"status"`
	CreatedAt   string   `json:"created_at,omitempty"`
}

// ProductListResp is returned by GET /api/v1/products.
type ProductListResp struct {
	BaseResponse
	Products   []ProductItem `json:"products"`
	Pagination *Pagination   `json:"pagination,omitempty"`
}

// ProductDetailResp is returned by GET /api/v1/products/:id.
type ProductDetailResp struct {
	BaseResponse
	Product *ProductItem `json:"product"`
}

// -------------------- x402 v2 支付相关 DTO --------------------

// X402ResourceInfo is the transport view of the protected resource.
type X402ResourceInfo struct {
	URL         string   `json:"url"`
	Description string   `json:"description,omitempty"`
	MimeType    string   `json:"mimeType,omitempty"`
	ServiceName string   `json:"serviceName,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	IconURL     string   `json:"iconUrl,omitempty"`
}

// X402PaymentRequirement is one acceptable x402 v2 payment option.
type X402PaymentRequirement struct {
	Scheme            string         `json:"scheme"`
	Network           string         `json:"network"`
	Amount            string         `json:"amount"`
	Asset             string         `json:"asset"`
	PayTo             string         `json:"payTo"`
	MaxTimeoutSeconds int            `json:"maxTimeoutSeconds"`
	Extra             map[string]any `json:"extra,omitempty"`
}

// PaymentRequiredResp is the HTTP 402 body (x402 v2 format).
type PaymentRequiredResp struct {
	X402Version int                      `json:"x402Version"`
	Error       string                   `json:"error,omitempty"`
	Resource    X402ResourceInfo         `json:"resource"`
	Accepts     []X402PaymentRequirement `json:"accepts"`
	Extensions  map[string]any           `json:"extensions,omitempty"`
}

// -------------------- x402 v1 向后兼容 DTO --------------------

// X402AcceptV1 is the x402 v1 accept item format.
// Kept for backward compatibility with older OpenClaw plugin versions.
type X402AcceptV1 struct {
	Scheme            string         `json:"scheme"`
	Network           string         `json:"network"`
	MaxAmountRequired string         `json:"maxAmountRequired"`
	PayTo             string         `json:"payTo"`
	Asset             string         `json:"asset"`
	Description       string         `json:"description"`
	Resource          string         `json:"resource"`
	MaxTimeoutSeconds int            `json:"maxTimeoutSeconds"`
	Extra             map[string]any `json:"extra"`
}

// PaymentRequiredRespV1 is the x402 v1 402 body format.
// Included alongside the v2 response for backward compatibility.
type PaymentRequiredRespV1 struct {
	X402Version int              `json:"x402Version"`
	Error       string           `json:"error"`
	Accepts     []X402AcceptV1   `json:"accepts"`
}

// -------------------- 购买执行结果 --------------------

// PurchaseExecuteResp is returned when a paid resource is unlocked.
type PurchaseExecuteResp struct {
	BaseResponse
	Product       *ProductItem   `json:"product,omitempty"`
	MerchantProof any            `json:"merchant_proof,omitempty"`
	GatewayProof  map[string]any `json:"gateway_proof,omitempty"`
	TxID          string         `json:"tx_id,omitempty"`
	TxHash        string         `json:"tx_hash,omitempty"`
	Content       map[string]any `json:"content,omitempty"`
}
