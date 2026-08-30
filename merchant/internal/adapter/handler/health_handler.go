// Copyright 2025 StablePay. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package handler 实现 COLA 架构的 Adapter 层。
//
// Handler 职责：
// 1. 解析 HTTP 请求参数（query/path/header/body）
// 2. 调用 Application Service 处理业务
// 3. 将 Service 返回的结果封装为 DTO 响应
//
// Handler 不包含任何业务逻辑，只做"翻译"工作。
package handler

import (
	"context"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/stablepay/merchant-server/internal/adapter/dto"
)

// HealthHandler 健康检查 handler
type HealthHandler struct {
	serviceName string
	dbReady     func() bool // 数据库就绪检查函数，由 infrastructure 注入
}

// NewHealthHandler 创建健康检查 handler
func NewHealthHandler(serviceName string, dbReadyFn func() bool) *HealthHandler {
	return &HealthHandler{
		serviceName: serviceName,
		dbReady:     dbReadyFn,
	}
}

// Healthz 健康检查端点
// GET /healthz
//
// 返回服务运行状态，用于 Kubernetes liveness/readiness probe。
func (h *HealthHandler) Healthz(ctx context.Context, c *app.RequestContext) {
	dbReady := false
	if h.dbReady != nil {
		dbReady = h.dbReady()
	}

	resp := dto.HealthResp{
		BaseResponse: dto.BaseResponse{
			OK:        true,
			Message:   "service is healthy",
			Timestamp: time.Now().Format(time.RFC3339),
		},
		Service: h.serviceName,
		Version: "0.1.0",
		DBReady: dbReady,
	}

	c.JSON(consts.StatusOK, resp)
}
