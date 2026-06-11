// Copyright 2025 StablePay. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package entity 实现 COLA 架构的 Domain 层核心实体。
//
// Entity 是领域模型的核心，包含：
// - 业务属性
// - 业务行为（方法）
// - 业务不变量（约束）
//
// Entity 不依赖任何框架或基础设施，是纯 Go 结构体。
package entity

import "time"

// ProductStatus 商品状态
type ProductStatus string

const (
	ProductStatusActive   ProductStatus = "active"    // 上架
	ProductStatusInactive ProductStatus = "inactive"  // 下架
	ProductStatusDraft    ProductStatus = "draft"     // 草稿
)

// Product 商品领域实体
//
// 遵循 COLA 的 Domain Entity 设计：
// - 包含核心业务属性
// - 封装业务方法（如判断是否可购买）
// - 不关心持久化细节
type Product struct {
	// 内部 ID（数据库自增主键）
	ID int64 `json:"-"`
	// 对外展示的商品 SKU ID
	SKUID string `json:"sku_id"`
	// 商品标题
	Title string `json:"title"`
	// 商品描述
	Description string `json:"description"`
	// 价格（字符串，例如 "2.00"）
	Price string `json:"price"`
	// 货币类型 (USDC/USDT)
	Currency string `json:"currency"`
	// 作者（可选）
	Author string `json:"author,omitempty"`
	// 标签（JSON 数组）
	Tags []string `json:"tags,omitempty"`
	// 商品状态
	Status ProductStatus `json:"status"`
	// DID 标识（对应 skill_did: did:solana:<pubkey>）
	// 从收款钱包地址生成
	SkillDid string `json:"skill_did,omitempty"`
	// 创建时间
	CreatedAt time.Time `json:"created_at"`
	// 更新时间
	UpdatedAt time.Time `json:"updated_at"`
}

// IsPurchasable 判断商品是否可购买
func (p *Product) IsPurchasable() bool {
	return p.Status == ProductStatusActive && p.Price != "" && p.SkillDid != ""
}

// GetPriceMinorUnits 获取价格的 minor units（6 位精度 USDC）
// 例如 "2.00" -> 2000000
func (p *Product) GetPriceMinorUnits() string {
	// TODO: 后续实现 USDC 金额转换工具函数
	return ""
}

// ProductBuilder 商品构建器（Builder 模式）
// 用于创建领域实体，避免构造函数参数过多
type ProductBuilder struct {
	product *Product
}

// NewProductBuilder 创建商品构建器
func NewProductBuilder() *ProductBuilder {
	return &ProductBuilder{
		product: &Product{
			Status:    ProductStatusDraft,
			Currency:  "USDC",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
}

func (b *ProductBuilder) WithSKUID(skuID string) *ProductBuilder {
	b.product.SKUID = skuID
	return b
}

func (b *ProductBuilder) WithTitle(title string) *ProductBuilder {
	b.product.Title = title
	return b
}

func (b *ProductBuilder) WithDescription(desc string) *ProductBuilder {
	b.product.Description = desc
	return b
}

func (b *ProductBuilder) WithPrice(price, currency string) *ProductBuilder {
	b.product.Price = price
	b.product.Currency = currency
	return b
}

func (b *ProductBuilder) WithAuthor(author string) *ProductBuilder {
	b.product.Author = author
	return b
}

func (b *ProductBuilder) WithTags(tags []string) *ProductBuilder {
	b.product.Tags = tags
	return b
}

func (b *ProductBuilder) WithStatus(status ProductStatus) *ProductBuilder {
	b.product.Status = status
	return b
}

func (b *ProductBuilder) WithSellerAddress(sellerAddress string) *ProductBuilder {
	b.product.SkillDid = "did:solana:" + sellerAddress
	return b
}

// Build 构建商品实体
func (b *ProductBuilder) Build() *Product {
	if b.product.SKUID == "" || b.product.Title == "" {
		panic("product: sku_id and title are required")
	}
	return b.product
}
