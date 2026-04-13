// Package handler HTTP 处理器
package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/stablepay/payment-service/internal/application/dto"
	"github.com/stablepay/payment-service/internal/application/service"
	"github.com/stablepay/payment-service/pkg/constants"
	"github.com/stablepay/payment-service/pkg/errors"
	"go.uber.org/zap"
)

// PaymentHandler 支付 HTTP 处理器
type PaymentHandler struct {
	paymentService *service.PaymentApplicationService
	logger         *zap.Logger
}

// NewPaymentHandler 创建处理器
func NewPaymentHandler(paymentService *service.PaymentApplicationService, logger *zap.Logger) *PaymentHandler {
	return &PaymentHandler{
		paymentService: paymentService,
		logger:         logger,
	}
}

// InitiatePayment 发起支付
// POST /api/v1/pay
func (h *PaymentHandler) InitiatePayment(ctx context.Context, c *app.RequestContext) {
	var req dto.InitiatePaymentRequest
	if err := c.BindAndValidate(&req); err != nil {
		h.respondError(c, errors.New(errors.INVALID_PARAMETERS, err.Error()))
		return
	}

	// 从 Header 获取幂等性键
	req.IdempotencyKey = string(c.GetHeader(constants.HeaderIdempotencyKey))
	if req.IdempotencyKey == "" {
		h.respondError(c, errors.New(errors.INVALID_PARAMETERS, "missing X-Idempotency-Key header"))
		return
	}

	if req.SignedTxBase64 != "" {
		sum := sha256.Sum256([]byte(req.SignedTxBase64))
		prefix := req.SignedTxBase64
		if len(prefix) > 80 {
			prefix = prefix[:80]
		}
		h.logger.Info("POST /api/v1/pay signed_tx audit",
			zap.String("agent_did", req.AgentDID),
			zap.String("skill_did", req.SkillDID),
			zap.Int("signed_tx_base64_len", len(req.SignedTxBase64)),
			zap.String("signed_tx_base64_sha256_utf8", hex.EncodeToString(sum[:])),
			zap.String("signed_tx_base64_prefix", prefix),
		)
	} else {
		h.logger.Warn("POST /api/v1/pay without signed_tx_base64 (blockchain-adapter will reject if from wallet is not hot wallet)",
			zap.String("agent_did", req.AgentDID),
			zap.String("skill_did", req.SkillDID),
		)
	}

	resp, err := h.paymentService.InitiatePayment(ctx, &req)
	if err != nil {
		h.respondError(c, err)
		return
	}

	h.respondSuccess(c, resp)
}

// GetPaymentStatus 查询支付状态
// GET /api/v1/pay/:tx_id
func (h *PaymentHandler) GetPaymentStatus(ctx context.Context, c *app.RequestContext) {
	var req dto.GetPaymentStatusRequest
	if err := c.BindAndValidate(&req); err != nil {
		h.respondError(c, errors.New(errors.INVALID_PARAMETERS, err.Error()))
		return
	}

	resp, err := h.paymentService.GetPaymentStatus(ctx, req.TxID)
	if err != nil {
		h.respondError(c, err)
		return
	}

	h.respondSuccess(c, resp)
}

// ListPaymentHistory 查询支付历史
// GET /api/v1/pay/history
func (h *PaymentHandler) ListPaymentHistory(ctx context.Context, c *app.RequestContext) {
	var req dto.ListPaymentHistoryRequest
	if err := c.BindAndValidate(&req); err != nil {
		h.respondError(c, errors.New(errors.INVALID_PARAMETERS, err.Error()))
		return
	}

	// 默认值
	if req.Page == 0 {
		req.Page = 1
	}
	if req.PageSize == 0 {
		req.PageSize = constants.DefaultPageSize
	}

	resp, err := h.paymentService.ListPaymentHistory(ctx, &req)
	if err != nil {
		h.respondError(c, err)
		return
	}

	h.respondSuccess(c, resp)
}

// GetPaymentRequirement 获取支付要求（HTTP 402）
// GET /api/v1/pay/require
func (h *PaymentHandler) GetPaymentRequirement(ctx context.Context, c *app.RequestContext) {
	var req dto.GetPaymentRequirementRequest
	if err := c.BindAndValidate(&req); err != nil {
		h.respondError(c, errors.New(errors.INVALID_PARAMETERS, err.Error()))
		return
	}

	resp, alreadyPurchased, err := h.paymentService.GetPaymentRequirement(ctx, &req)
	if err != nil {
		h.respondError(c, err)
		return
	}

	if alreadyPurchased {
		h.respondWithCode(c, http.StatusOK, 0, "Already purchased", &dto.AlreadyPurchasedResponse{
			Purchased: true,
		})
		return
	}

	// 返回 HTTP 402
	h.respondWithCode(c, http.StatusPaymentRequired, 402, "Payment Required", resp)
}

// ShortLinkPay 短链支付跳转
// GET /pay?skill=...&price=...
func (h *PaymentHandler) ShortLinkPay(ctx context.Context, c *app.RequestContext) {
	// 短链由 API Gateway 转发到 /api/v1/pay/require
	// 这里只处理跳转逻辑
	skillDID := c.Query("skill")
	if skillDID == "" {
		h.respondError(c, errors.New(errors.INVALID_PARAMETERS, "missing skill parameter"))
		return
	}

	// 构建规范 API URL
	canonicalURL := "/api/v1/pay/require?skill_did=" + skillDID
	if agentDID := c.Query("agent"); agentDID != "" {
		canonicalURL += "&agent_did=" + agentDID
	}

	c.Redirect(http.StatusMovedPermanently, []byte(canonicalURL))
}

// ShortLinkVerify 短链验证
// GET /verify?skill=...&agent=...
func (h *PaymentHandler) ShortLinkVerify(ctx context.Context, c *app.RequestContext) {
	var req dto.ShortLinkVerifyRequest
	req.SkillDID = c.Query("skill")
	req.AgentDID = c.Query("agent")

	if req.SkillDID == "" || req.AgentDID == "" {
		h.respondError(c, errors.New(errors.INVALID_PARAMETERS, "missing skill or agent parameter"))
		return
	}

	// 调用规范 API 检查
	requireReq := &dto.GetPaymentRequirementRequest{
		SkillDID: req.SkillDID,
		AgentDID: req.AgentDID,
	}

	_, alreadyPurchased, err := h.paymentService.GetPaymentRequirement(ctx, requireReq)
	if err != nil {
		h.respondError(c, err)
		return
	}

	h.respondSuccess(c, &dto.ShortLinkVerifyResponse{
		Purchased: alreadyPurchased,
	})
}

// 响应辅助方法

// BaseResponse 基础响应结构
type BaseResponse struct {
	Code      int         `json:"code"`
	Message   string      `json:"message"`
	Data      interface{} `json:"data,omitempty"`
	RequestID string      `json:"request_id,omitempty"`
	Timestamp int64       `json:"timestamp"`
}

func (h *PaymentHandler) respondSuccess(c *app.RequestContext, data interface{}) {
	h.respondWithCode(c, http.StatusOK, 0, "success", data)
}

func (h *PaymentHandler) respondError(c *app.RequestContext, err error) {
	code := errors.GetErrorCode(err)
	message := err.Error()

	// 映射 HTTP 状态码（链上/适配器错误用 503，避免 api-gateway 把业务 code=20003 与 http=400 混在一起只显示 downstream status: 400）
	httpStatus := http.StatusBadRequest
	switch code {
	case errors.INTERNAL_SERVER_ERROR:
		httpStatus = http.StatusInternalServerError
	case errors.RESOURCE_NOT_FOUND:
		httpStatus = http.StatusNotFound
	case errors.PERMISSION_DENIED:
		httpStatus = http.StatusForbidden
	case errors.RATE_LIMIT_EXCEEDED:
		httpStatus = http.StatusTooManyRequests
	case errors.SERVICE_UNAVAILABLE:
		httpStatus = http.StatusServiceUnavailable
	case errors.BLOCKCHAIN_NETWORK_ERROR, errors.GAS_SUBSIDY_FAILED:
		httpStatus = http.StatusServiceUnavailable
	}

	h.respondWithCode(c, httpStatus, int(code), message, nil)
}

func (h *PaymentHandler) respondWithCode(c *app.RequestContext, httpStatus, code int, message string, data interface{}) {
	resp := BaseResponse{
		Code:      code,
		Message:   message,
		Data:      data,
		RequestID: string(c.GetHeader(constants.HeaderRequestID)),
		Timestamp: time.Now().Unix(),
	}

	c.JSON(httpStatus, resp)
}

// HealthCheck 健康检查
// GET /health
func (h *PaymentHandler) HealthCheck(ctx context.Context, c *app.RequestContext) {
	c.JSON(http.StatusOK, map[string]string{
		"status": "healthy",
		"service": constants.ServiceName,
	})
}
