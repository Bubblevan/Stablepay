// Copyright 2025 StablePay. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package service 实现 COLA 架构的 Application Service（应用服务）。
//
// Application Service 职责：
// 1. 业务流程编排（调用多个 Domain Service / Repository）
// 2. 事务管理
// 3. DTO 组装
// 4. 权限校验
//
// 应用服务不包含业务规则，只负责"调度"。
package service

import (
	"context"
	"fmt"

	domainSvc "github.com/stablepay/merchant-server/internal/domain/service"
	"github.com/stablepay/merchant-server/internal/domain/entity"
	"github.com/stablepay/merchant-server/internal/domain/repository"
)

// ProductAppService 商品应用服务
//
// 这是 COLA 架构中的"应用服务层"入口。
// COLA 的 AppService 特点：
// - 一个 Use Case 对应一个方法
// - 负责事务边界
// - 负责 DTO 转换
// - 不包含业务逻辑
type ProductAppService struct {
	productRepo    repository.ProductRepository
	domainService  *domainSvc.ProductDomainService
	sellerAddress string
	facilitatorURL string
	usdcMint      string
	solanaNetwork string
}

// NewProductAppService 创建商品应用服务
func NewProductAppService(
	productRepo repository.ProductRepository,
	domainService *domainSvc.ProductDomainService,
	sellerAddress, facilitatorURL, usdcMint, solanaNetwork string,
) *ProductAppService {
	return &ProductAppService{
		productRepo:    productRepo,
		domainService:  domainService,
		sellerAddress: sellerAddress,
		facilitatorURL: facilitatorURL,
		usdcMint:      usdcMint,
		solanaNetwork: solanaNetwork,
	}
}

// ProductListItem 商品列表项（应用层 DTO）
type ProductListItem struct {
	ID          string   `json:"id"`
	SKUID       string   `json:"sku_id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Price       string   `json:"price"`
	Currency    string   `json:"currency"`
	Author      string   `json:"author,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Status      string   `json:"status"`
}

// ListProducts 查询商品列表
func (s *ProductAppService) ListProducts(ctx context.Context, page, size int) ([]*ProductListItem, int64, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}

	products, total, err := s.productRepo.FindAll(ctx, page, size)
	if err != nil {
		return nil, 0, fmt.Errorf("list products: %w", err)
	}

	items := make([]*ProductListItem, 0, len(products))
	for _, p := range products {
		items = append(items, &ProductListItem{
			ID:          p.SKUID,
			SKUID:       p.SKUID,
			Title:       p.Title,
			Description: p.Description,
			Price:       p.Price,
			Currency:    p.Currency,
			Author:      p.Author,
			Tags:        p.Tags,
			Status:      string(p.Status),
		})
	}

	return items, total, nil
}

// GetProductDetail 获取商品详情
func (s *ProductAppService) GetProductDetail(ctx context.Context, skuID string) (*ProductListItem, error) {
	product, err := s.productRepo.FindBySKUID(ctx, skuID)
	if err != nil {
		return nil, fmt.Errorf("get product detail: %w", err)
	}

	return &ProductListItem{
		ID:          product.SKUID,
		SKUID:       product.SKUID,
		Title:       product.Title,
		Description: product.Description,
		Price:       product.Price,
		Currency:    product.Currency,
		Author:      product.Author,
		Tags:        product.Tags,
		Status:      string(product.Status),
	}, nil
}

// X402PaymentRequirement x402 支付要求（应用层 DTO）
type X402PaymentRequirement struct {
	Accepts []X402AcceptItem `json:"accepts"`
}

// X402AcceptItem x402 接受的支付项
type X402AcceptItem struct {
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

// BuildX402PaymentRequirement 构建 x402 Payment Required 信息
func (s *ProductAppService) BuildX402PaymentRequirement(product *entity.Product) *X402PaymentRequirement {
	accept := X402AcceptItem{
		Scheme:            "exact",
		Network:           s.solanaNetwork,
		MaxAmountRequired: s.domainService.UsdcToMinorUnits(product.Price),
		PayTo:             s.sellerAddress,
		Asset:             s.usdcMint,
		Description:       fmt.Sprintf("购买 %s", product.Title),
		Resource:          fmt.Sprintf("/api/v1/products/%s/execute", product.SKUID),
		MaxTimeoutSeconds: 300,
	}
	accept.Extra.FacilitatorURL = s.facilitatorURL
	accept.Extra.Currency = product.Currency
	accept.Extra.ProductID = product.SKUID
	accept.Extra.SkillDid = product.SkillDid

	return &X402PaymentRequirement{
		Accepts: []X402AcceptItem{accept},
	}
}

// ExecutePurchaseResult 购买执行结果
type ExecutePurchaseResult struct {
	IsPurchased         bool
	Proof               map[string]interface{}
	X402Requirement     *X402PaymentRequirement
}

// ExecutePurchase 执行购买流程
//
// 这是核心 Use Case：
// 1. 查询商品
// 2. 检查商品是否可购买
// 3. 如果需要支付 -> 返回 x402 Payment Required
// 4. 如果已支付 -> 返回购买成功
//
// TODO: 后续步骤实现实际的 Gateway 验证
func (s *ProductAppService) ExecutePurchase(ctx context.Context, skuID, agentDid string) (*ExecutePurchaseResult, error) {
	product, err := s.productRepo.FindBySKUID(ctx, skuID)
	if err != nil {
		return nil, fmt.Errorf("product not found: %s", skuID)
	}

	if err := s.domainService.CanPurchase(product); err != nil {
		return nil, err
	}

	// TODO: 后续步骤 - 调用 Gateway /api/v1/verify 检查是否已购买
	// 当前骨架：始终返回未购买（402）
	requirement := s.BuildX402PaymentRequirement(product)

	return &ExecutePurchaseResult{
		IsPurchased:     false,
		X402Requirement: requirement,
	}, nil
}
