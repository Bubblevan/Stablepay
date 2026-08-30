// Copyright 2025 StablePay. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package repository 定义 COLA 架构的 Domain 层仓储接口。
//
// Repository 接口定义在 Domain 层（契约），
// 实现在 Infrastructure 层（细节）。
//
// 这种"依赖倒置" (DIP) 确保领域层不依赖基础设施细节。
package repository

import (
	"context"

	"github.com/stablepay/merchant-server/internal/domain/entity"
)

// ProductRepository 商品仓储接口
//
// 采用 COLA 的 Repository 模式：
// - 接口定义在 Domain 层（高层模块）
// - 实现在 Infrastructure 层（低层模块）
// - Domain 层通过接口依赖，不依赖具体实现
type ProductRepository interface {
	// FindAll 查询所有上架商品（分页）
	FindAll(ctx context.Context, page, size int) ([]*entity.Product, int64, error)

	// FindBySKUID 根据 SKU ID 查询商品
	FindBySKUID(ctx context.Context, skuID string) (*entity.Product, error)

	// FindByID 根据内部 ID 查询商品
	FindByID(ctx context.Context, id int64) (*entity.Product, error)

	// Save 保存商品（新增或更新）
	Save(ctx context.Context, product *entity.Product) error

	// UpdateStatus 更新商品状态
	UpdateStatus(ctx context.Context, id int64, status entity.ProductStatus) error
}
