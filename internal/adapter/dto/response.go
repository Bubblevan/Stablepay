// Copyright 2025 StablePay. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package dto (Data Transfer Object) 属于 COLA 架构的 Adapter 层。
//
// DTO 负责：
// - 定义 API 请求/响应的数据结构（与领域实体解耦）
// - 实现序列化/反序列化
// - 参数校验
//
// 各层之间的数据传递规范：
// Adapter(Handler) <-> DTO <-> AppService <-> DomainEntity
package dto

import (
	"time"
)

// -------------------- 基础响应 --------------------

// BaseResponse 所有 API 响应的基础包装
type BaseResponse struct {
	OK        bool   `json:"ok"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

// Success 构造成功响应
func Success(msg string) BaseResponse {
	return BaseResponse{
		OK:        true,
		Message:   msg,
		Timestamp: time.Now().Format(time.RFC3339),
	}
}

// Error 构造失败响应
func Error(code, msg string) BaseResponse {
	return BaseResponse{
		OK:        false,
		Code:      code,
		Message:   msg,
		Timestamp: time.Now().Format(time.RFC3339),
	}
}

// -------------------- 健康检查 --------------------

// HealthResp 健康检查响应
type HealthResp struct {
	BaseResponse
	Service    string `json:"service"`
	Version    string `json:"version,omitempty"`
	DBReady    bool   `json:"db_ready"`
}

// -------------------- 数据分页 --------------------

// Pagination 分页信息
type Pagination struct {
	Total int64 `json:"total"`
	Page  int   `json:"page"`
	Size  int   `json:"size"`
}

// -------------------- 商品相关 DTO --------------------

// ProductItem 商品列表项（对外暴露的视图）
// 不包含敏感的内部字段如 proof_secret
type ProductItem struct {
	ID          string   `json:"id"`
	SKUID       string   `json:"sku_id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Price       string   `json:"price"`
	Currency    string   `json:"currency"`
	Author      string   `json:"author,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Status      string   `json:"status"`
	CreatedAt   string   `json:"created_at,omitempty"`
}

// ProductListResp 商品列表响应
type ProductListResp struct {
	BaseResponse
	Products   []ProductItem `json:"products"`
	Pagination *Pagination   `json:"pagination,omitempty"`
}

// ProductDetailResp 商品详情响应
type ProductDetailResp struct {
	BaseResponse
	Product *ProductItem `json:"product"`
}

// -------------------- x402 支付相关 DTO --------------------

// X402Accept x402 标准中接受的支付方式
type X402Accept struct {
	Scheme            string `json:"scheme"`
	Network           string `json:"network"`
	MaxAmountRequired string `json:"maxAmountRequired"`
	PayTo             string `json:"payTo"`
	Asset             string `json:"asset"`
	Description       string `json:"description"`
	Resource          string `json:"resource"`
	MaxTimeoutSeconds int    `json:"maxTimeoutSeconds"`
	Extra             struct {
		FacilitatorURL string `json:"facilitatorUrl"`
		Currency       string `json:"currency"`
		ProductID      string `json:"productId"`
		SkillDid       string `json:"skillDid"`
	} `json:"extra"`
}

// PaymentRequiredResp 402 Payment Required 响应 (x402 标准)
type PaymentRequiredResp struct {
	X402Version int           `json:"x402Version"`
	Accepts     []X402Accept  `json:"accepts"`
	Error       string        `json:"error"`
}

// PurchaseExecuteResp 执行购买成功响应
type PurchaseExecuteResp struct {
	BaseResponse
	Product   string      `json:"product"`
	Access    interface{} `json:"access,omitempty"`
	Content   interface{} `json:"content,omitempty"`
}
