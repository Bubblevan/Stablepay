// Package router HTTP 路由配置
package router

import (
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/stablepay/payment-service/internal/adapter/http/handler"
)

// RegisterRoutes 注册所有路由
func RegisterRoutes(h *server.Hertz, paymentHandler *handler.PaymentHandler) {
	// 规范 API 路由
	apiV1 := h.Group("/api/v1")
	{
		// 支付相关
		apiV1.POST("/pay", paymentHandler.InitiatePayment)
		apiV1.GET("/pay/:tx_id", paymentHandler.GetPaymentStatus)
		apiV1.GET("/pay/history", paymentHandler.ListPaymentHistory)
		apiV1.GET("/pay/require", paymentHandler.GetPaymentRequirement)

		// internal 奖励(由 verification-service 等内部服务调用,X-Internal-Api-Key 鉴权)
		apiV1.POST("/internal/rewards/x-registration", paymentHandler.RegisterXRegistrationReward)
	}

	// 短链兼容路由（由 API Gateway 映射到后端规范路由）
	// 实际这里只是示例，真正的映射在 Gateway 层处理
	h.GET("/pay", paymentHandler.ShortLinkPay)
	h.GET("/verify", paymentHandler.ShortLinkVerify)

	// 健康检查
	h.GET("/health", paymentHandler.HealthCheck)
}
