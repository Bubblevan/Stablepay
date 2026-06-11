// Copyright 2025 StablePay. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package domain_service 实现 COLA 架构的 Domain Service（领域服务）。
//
// 领域服务与实体的区别：
// - Entity: 有状态，包含自身的业务方法（如 product.IsPurchasable()）
// - DomainService: 无状态，协调多个实体或外部资源的领域逻辑
//
// 当某个业务逻辑不适合放在任何一个 Entity 中时，使用 Domain Service。
package domain_service

import (
	"fmt"
	"time"

	"github.com/stablepay/merchant-server/internal/domain/entity"
	"github.com/stablepay/merchant-server/internal/domain/repository"
)

// ProductDomainService 商品领域服务
//
// 职责：
// 1. 商品购买前的业务校验（是否有货、价格是否有效）
// 2. 计算 x402 支付所需参数
// 3. 生成购买证明（proof）
type ProductDomainService struct {
	repo repository.ProductRepository
}

// NewProductDomainService 创建商品领域服务
func NewProductDomainService(repo repository.ProductRepository) *ProductDomainService {
	return &ProductDomainService{repo: repo}
}

// CanPurchase 检查商品是否可购买，返回错误原因
func (s *ProductDomainService) CanPurchase(product *entity.Product) error {
	if product == nil {
		return fmt.Errorf("product not found")
	}
	if !product.IsPurchasable() {
		return fmt.Errorf("product '%s' is not available for purchase (status=%s)",
			product.SKUID, product.Status)
	}
	return nil
}

// UsdcToMinorUnits 将 USDC 金额转换为 minor units
// 例如 "2.00" -> 2000000 (6位精度)
func (s *ProductDomainService) UsdcToMinorUnits(amount string) string {
	// 简单实现：解析浮点数后乘以 1_000_000
	// TODO: 后续可以抽取为工具函数
	var whole, frac int
	n, _ := fmt.Sscanf(amount, "%d.%d", &whole, &frac)
	if n < 1 {
		return "0"
	}
	if n == 1 {
		return fmt.Sprintf("%d000000", whole)
	}
	// 补齐 6 位小数
	fracStr := fmt.Sprintf("%06d", frac)
	return fmt.Sprintf("%d%s", whole, fracStr[:6])
}

// BuildProof 构建购买证明（HMAC 签名）
func (s *ProductDomainService) BuildProof(agentDid, productID, proofSecret string) map[string]interface{} {
	issuedAt := time.Now().UTC().Format(time.RFC3339)
	// TODO: 后续使用 crypto/hmac 生成实际签名
	// 当前为骨架，返回占位值
	return map[string]interface{}{
		"proof_id":       fmt.Sprintf("proof_%s_%d", productID, time.Now().UnixMilli()),
		"agent_did":      agentDid,
		"product_id":     productID,
		"issued_at":      issuedAt,
		"signature":      "pending_implementation",
	}
}
