// Package handler 提供 RPC 接口实现
// TransferHandler 实现转账相关接口
package handler

import (
	"context"
	"fmt"

	"stablepay.blockchain_adapter/internal/service"
	"stablepay.blockchain_adapter/kitex_gen/stablepay/blockchain_adapter"
	"stablepay.blockchain_adapter/kitex_gen/stablepay/common"
)

// TransferHandler 转账处理器
type TransferHandler struct {
	transferSvc *service.TransferService
}

// NewTransferHandler 创建转账处理器
func NewTransferHandler(transferSvc *service.TransferService) *TransferHandler {
	return &TransferHandler{
		transferSvc: transferSvc,
	}
}

// TransferStableCoin 执行稳定币转账并代付 Gas
// 实现 blockchain-adapter.thrift 定义的 RPC 接口
func (h *TransferHandler) TransferStableCoin(
	ctx context.Context,
	req *blockchain_adapter.TransferStableCoinRequest,
) (*blockchain_adapter.TransferStableCoinResponse, error) {
	// 参数校验
	if err := h.validateRequest(req); err != nil {
		return h.buildErrorResponse(
			common.ErrorCode_INVALID_PARAMETERS,
			fmt.Sprintf("invalid request: %v", err),
			req.Base,
		), nil
	}

	// 转换请求
	transferReq := &service.TransferRequest{
		SignedTxBase64: "", // 从 req.SignedTxBase64 获取，目前 thrift 定义是 optional
		ToAddress:      req.ToWalletAddress,
		AmountMinor:    req.AmountMinor,
		Currency:       req.Currency,
		TxID:           req.Base.GetIdempotencyKey(), // 使用幂等键作为交易ID
	}

	// 可选字段 FromWalletAddress
	if req.FromWalletAddress != nil {
		transferReq.FromAddress = *req.FromWalletAddress
	}

	// 如果提供了 base64 交易，使用它
	if req.SignedTxBase64 != nil {
		transferReq.SignedTxBase64 = *req.SignedTxBase64
	}

	// 调用服务层
	result, err := h.transferSvc.TransferStableCoin(ctx, transferReq)
	if err != nil {
		// 判断错误类型
		errorCode := common.ErrorCode_INTERNAL_SERVER_ERROR
		errorMsg := err.Error()

		// 根据错误内容映射错误码
		if isInsufficientBalance(err) {
			errorCode = common.ErrorCode_INSUFFICIENT_BALANCE
			errorMsg = "insufficient balance for transfer"
		} else if isNetworkError(err) {
			errorCode = common.ErrorCode_BLOCKCHAIN_NETWORK_ERROR
			errorMsg = "blockchain network error"
		} else if isGasSubsidyFailed(err) {
			errorCode = common.ErrorCode_GAS_SUBSIDY_FAILED
			errorMsg = "gas subsidy failed"
		}

		return h.buildErrorResponse(errorCode, errorMsg, req.Base), nil
	}

	// 构造成功响应
	return &blockchain_adapter.TransferStableCoinResponse{
		Base: &common.BaseResp{
			Code:    0,
			Message: "success",
			RequestId: strPtr(req.Base.GetRequestId()),
			TraceId:   strPtr(req.Base.GetTraceId()),
		},
		TxHash: result.TxHash,
		Status: result.Status,
	}, nil
}

// validateRequest 校验转账请求
func (h *TransferHandler) validateRequest(req *blockchain_adapter.TransferStableCoinRequest) error {
	if req == nil {
		return fmt.Errorf("request is nil")
	}

	// 校验收款方地址
	if req.ToWalletAddress == "" {
		return fmt.Errorf("to_wallet_address is required")
	}

	// 校验金额
	if req.AmountMinor <= 0 {
		return fmt.Errorf("amount_minor must be greater than 0")
	}

	// 校验币种
	if req.Currency != common.Currency_USDC && req.Currency != common.Currency_USDT {
		return fmt.Errorf("unsupported currency: %v", req.Currency)
	}

	// 校验幂等键（强烈建议）
	if req.Base == nil || req.Base.GetIdempotencyKey() == "" {
		// 不强制要求，但建议
		// return fmt.Errorf("idempotency_key is recommended")
	}

	return nil
}

// buildErrorResponse 构建错误响应
func (h *TransferHandler) buildErrorResponse(
	code common.ErrorCode,
	message string,
	baseReq *common.BaseReq,
) *blockchain_adapter.TransferStableCoinResponse {
	return &blockchain_adapter.TransferStableCoinResponse{
		Base: &common.BaseResp{
			Code:      int32(code),
			Message:   message,
			RequestId: strPtr(baseReq.GetRequestId()),
			TraceId:   strPtr(baseReq.GetTraceId()),
		},
		TxHash: "",
		Status: blockchain_adapter.TxStatus_FAILED,
	}
}

// 错误类型判断辅助函数

func isInsufficientBalance(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return contains(errStr, "insufficient") || contains(errStr, "balance")
}

func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return contains(errStr, "network") ||
		contains(errStr, "timeout") ||
		contains(errStr, "connection") ||
		contains(errStr, "rpc")
}

func isGasSubsidyFailed(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return contains(errStr, "gas") || contains(errStr, "subsidy") || contains(errStr, "fee")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && containsSubstring(s, substr)))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// strPtr 将 string 转换为 *string
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
