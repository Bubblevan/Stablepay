// Copyright 2025 StablePay. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package sqlite 实现 COLA 架构的 Infrastructure 层数据库持久化。
//
// 遵循"依赖倒置原则"(DIP)：
// - Domain 层定义 Repository 接口
// - Infrastructure 层实现 Repository 接口
// - Domain 层不依赖 Infrastructure 层
package sqlite

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/stablepay/merchant-server/internal/domain/entity"
)

// TODO: 后续步骤引入 modernc.org/sqlite 实现真正的 SQLite 连接
// 当前为骨架代码，使用内存存储模拟

// ProductRepoImpl ProductRepository 的 SQLite 实现
type ProductRepoImpl struct {
	mu       sync.RWMutex
	products map[string]*entity.Product // key: SKUID
	nextID   int64
}

// NewProductRepo 创建 SQLite 商品仓储实现
func NewProductRepo() *ProductRepoImpl {
	return &ProductRepoImpl{
		products: make(map[string]*entity.Product),
		nextID:   1,
	}
}

// FindAll 查询所有上架商品
func (r *ProductRepoImpl) FindAll(ctx context.Context, page, size int) ([]*entity.Product, int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var active []*entity.Product
	for _, p := range r.products {
		if p.Status == entity.ProductStatusActive {
			active = append(active, p)
		}
	}

	total := int64(len(active))
	start := (page - 1) * size
	if start >= len(active) {
		return []*entity.Product{}, total, nil
	}

	end := start + size
	if end > len(active) {
		end = len(active)
	}

	return active[start:end], total, nil
}

// FindBySKUID 根据 SKUID 查询商品
func (r *ProductRepoImpl) FindBySKUID(ctx context.Context, skuID string) (*entity.Product, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, ok := r.products[skuID]
	if !ok {
		return nil, fmt.Errorf("product not found: %s", skuID)
	}
	return p, nil
}

// FindByID 根据内部 ID 查询商品
func (r *ProductRepoImpl) FindByID(ctx context.Context, id int64) (*entity.Product, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, p := range r.products {
		if p.ID == id {
			return p, nil
		}
	}
	return nil, fmt.Errorf("product not found: id=%d", id)
}

// Save 保存商品
func (r *ProductRepoImpl) Save(ctx context.Context, product *entity.Product) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if product.ID == 0 {
		product.ID = r.nextID
		r.nextID++
	}
	product.UpdatedAt = time.Now()
	r.products[product.SKUID] = product
	return nil
}

// UpdateStatus 更新商品状态
func (r *ProductRepoImpl) UpdateStatus(ctx context.Context, id int64, status entity.ProductStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, p := range r.products {
		if p.ID == id {
			p.Status = status
			p.UpdatedAt = time.Now()
			return nil
		}
	}
	return fmt.Errorf("product not found: id=%d", id)
}

// Seed 写入种子数据（首次启动时调用）
func (r *ProductRepoImpl) Seed(ctx context.Context, sellerAddress string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.products) > 0 {
		return nil // 已有数据，不重复写入
	}

	now := time.Now()
	seedProducts := []*entity.Product{
		{
			ID:          r.nextID,
			SKUID:       "ai-agent-job-2025",
			Title:       "AI Agent 岗位分析报告 2025",
			Description: "深入分析 2025 年 AI Agent 领域的岗位需求、技能要求、薪资水平和发展趋势",
			Price:       "2.00",
			Currency:    "USDC",
			Author:      "StablePay Research",
			Tags:        []string{"AI", "Agent", "求职", "行业分析"},
			Status:      entity.ProductStatusActive,
			SkillDid:    "did:solana:" + sellerAddress,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          r.nextID + 1,
			SKUID:       "industry-briefing-q1",
			Title:       "2025 Q1 行业研究简报",
			Description: "涵盖 AI、区块链、Web3 领域的最新趋势和投资机会",
			Price:       "1.50",
			Currency:    "USDC",
			Author:      "StablePay Research",
			Tags:        []string{"行业研究", "AI", "区块链", "Web3"},
			Status:      entity.ProductStatusActive,
			SkillDid:    "did:solana:" + sellerAddress,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          r.nextID + 2,
			SKUID:       "resume-optimization-guide",
			Title:       "简历优化建议报告",
			Description: "针对技术岗位的简历优化建议，包含模板和案例分析",
			Price:       "1.00",
			Currency:    "USDC",
			Author:      "StablePay Research",
			Tags:        []string{"求职", "简历", "技术岗位"},
			Status:      entity.ProductStatusActive,
			SkillDid:    "did:solana:" + sellerAddress,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			ID:          r.nextID + 3,
			SKUID:       "vitality-research",
			Title:       "生命力研究：为什么有些人看起来生命力很强",
			Description: "基于萨特《恶心》的存在主义解读，探讨生命力的本质与来源",
			Price:       "1.50",
			Currency:    "USDC",
			Author:      "StablePay Research",
			Tags:        []string{"哲学", "心理学", "存在主义", "个人成长"},
			Status:      entity.ProductStatusActive,
			SkillDid:    "did:solana:" + sellerAddress,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}

	for _, p := range seedProducts {
		r.products[p.SKUID] = p
		r.nextID = p.ID + 1
	}

	fmt.Printf("[sqlite] seeded %d products\n", len(seedProducts))
	return nil
}

// DBReady 检查数据库是否就绪
func (r *ProductRepoImpl) DBReady() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return true
}
