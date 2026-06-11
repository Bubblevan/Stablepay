// Copyright 2025 StablePay. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package adapter 实现 COLA 架构的 Adapter（适配）层。
//
// 适配层职责：
// - 处理 HTTP 协议细节（请求/响应编解码）
// - 路由注册
// - 参数校验
// - 中间件（日志、鉴权、限流、恢复等）
//
// 适配层不直接依赖领域层，而是通过 Application Service 接口调用。
package adapter

import (
	"github.com/cloudwego/hertz/pkg/app/server"

	"github.com/stablepay/merchant-server/internal/adapter/handler"
)

// Router 负责注册所有 HTTP 路由。
// 采用 COLA 风格：将路由集中管理，每个 handler 独立文件。
type Router struct {
	srv           *server.Hertz
	healthHandler *handler.HealthHandler
}

// NewRouter 创建路由注册器。
func NewRouter(srv *server.Hertz, hh *handler.HealthHandler) *Router {
	return &Router{
		srv:           srv,
		healthHandler: hh,
	}
}

// Register 注册所有路由。
//
// 路由设计原则：
// - 公开 API 使用 /api/v1/ 前缀
// - 内部/运维接口使用 /healthz, /metrics 等标准路径
// - 所有路由按模块分组，便于后续添加中间件
func (r *Router) Register() {
	// ==================== 运维 & 健康检查 ====================
	r.srv.GET("/healthz", r.healthHandler.Healthz)

	// ==================== API v1 分组 ====================
	// TODO: 后续步骤中注册商品查询/购买路由
	// v1 := r.srv.Group("/api/v1")
	// v1.GET("/products", r.productHandler.ListProducts)
	// v1.GET("/products/:id", r.productHandler.GetProduct)
	// v1.GET("/products/:id/execute", r.productHandler.ExecutePurchase)
}
