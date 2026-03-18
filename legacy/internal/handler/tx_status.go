// Package handler 提供 RPC 接口实现
// TxStatusHandler 实现交易状态查询相关接口
package handler

import (
	"context"
	"fmt"

	"stablepay.blockchain_adapter/internal/service"
	"stablepay.blockchain_adapter/kitex_gen/stablepay/blockchain_adapter"
	"stablepay.blockchain_adapter/kitex_gen/stablepay/common"
)

// TxStatusHandler 交易状态处理器
type TxStatusHandler struct {
	txStatusSvc *service.TxStatusService
}

// NewTxStatusHandler 创建交易状态处理器
func NewTxStatusHandler(txStatusSvc *service.TxStatusService) *TxStatusHandler {
	return &TxStatusHandler{
		txStatusSvc: txStatusSvc,
	}
}

// GetTxStatus 查询交易状态
// 实现 blockchain-adapter.thrift 定义的 RPC 接口
func (h *TxStatusHandler) GetTxStatus(
	ctx context.Context,
	req *blockchain_adapter.GetTxStatusRequest,
) (*blockchain_adapter.GetTxStatusResponse, error) {
	// 参数校验
	if err := h.validateRequest(req); err != nil {
		return h.buildErrorResponse(
			common.ErrorCode_INVALID_PARAMETERS,
			fmt.Sprintf("invalid request: %v", err),
			req.Base,
		), nil
	}

	// 调用服务层查询状态
	statusInfo, err := h.txStatusSvc.GetTxStatus(ctx, req.TxHash)
	if err != nil {
		return h.buildErrorResponse(
			common.ErrorCode_BLOCKCHAIN_NETWORK_ERROR,
			fmt.Sprintf("failed to get transaction status: %v", err),
			req.Base,
		), nil
	}

	// 构造响应
	resp := &blockchain_adapter.GetTxStatusResponse{
		Base: &common.BaseResp{
			Code:      0,
			Message:   "success",
			RequestId: strPtr(req.Base.GetRequestId()),
			TraceId:   strPtr(req.Base.GetTraceId()),
		},
		Status: statusInfo.Status,
	}

	// 设置确认时间（如果有）
	if statusInfo.BlockTime != nil {
		confirmedAt := statusInfo.BlockTime.Format("2006-01-02T15:04:05Z07:00")
		resp.ConfirmedAt = &confirmedAt
	}

	// 设置失败信息（如果失败）
	if statusInfo.Status == blockchain_adapter.TxStatus_FAILED {
		if statusInfo.Err != nil {
			errMsg := fmt.Sprintf("%v", statusInfo.Err)
			resp.ReasonMessage = &errMsg
		}
		failedAt := ""
		if statusInfo.BlockTime != nil {
			failedAt = statusInfo.BlockTime.Format("2006-01-02T15:04:05Z07:00")
		}
		resp.FailedAt = &failedAt
	}

	return resp, nil
}

// validateRequest 校验交易状态查询请求
func (h *TxStatusHandler) validateRequest(req *blockchain_adapter.GetTxStatusRequest) error {
	if req == nil {
		return fmt.Errorf("request is nil")
	}

	// 校验交易哈希
	if req.TxHash == "" {
		return fmt.Errorf("tx_hash is required")
	}

	// 校验交易哈希格式（Solana 交易哈希 Base58 编码，约 64-88 字符）
	if len(req.TxHash) < 64 || len(req.TxHash) > 96 {
		return fmt.Errorf("invalid tx_hash format")
	}

	return nil
}

// buildErrorResponse 构建错误响应
func (h *TxStatusHandler) buildErrorResponse(
	code common.ErrorCode,
	message string,
	baseReq *common.BaseReq,
) *blockchain_adapter.GetTxStatusResponse {
	return &blockchain_adapter.GetTxStatusResponse{
		Base: &common.BaseResp{
			Code:      int32(code),
			Message:   message,
			RequestId: strPtr(baseReq.GetRequestId()),
			TraceId:   strPtr(baseReq.GetTraceId()),
		},
		Status: blockchain_adapter.TxStatus_FAILED,
	}
}

// SyncTxStatus 同步交易状态（管理接口）
// 用于手动触发状态同步，非 Thrift 定义的标准接口
func (h *TxStatusHandler) SyncTxStatus(
	ctx context.Context,
	txHash string,
	baseReq *common.BaseReq,
) (*blockchain_adapter.GetTxStatusResponse, error) {
	// 校验
	if txHash == "" {
		return h.buildErrorResponse(
			common.ErrorCode_INVALID_PARAMETERS,
			"tx_hash is required",
			baseReq,
		), nil
	}

	// 调用服务层同步
	if err := h.txStatusSvc.SyncTxStatus(ctx, txHash); err != nil {
		return h.buildErrorResponse(
			common.ErrorCode_INTERNAL_SERVER_ERROR,
			fmt.Sprintf("failed to sync transaction status: %v", err),
			baseReq,
		), nil
	}

	// 同步后再查询最新状态
	return h.GetTxStatus(ctx, &blockchain_adapter.GetTxStatusRequest{
		Base:   baseReq,
		TxHash: txHash,
	})
}
