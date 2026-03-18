// Package rpc Adapter层RPC适配器
package rpc

import (
	"context"
	"fmt"

	"stablepay.blockchain_adapter/blockchain-adapter-app/service"
	"stablepay.blockchain_adapter/kitex_gen/stablepay/blockchain_adapter"
	"stablepay.blockchain_adapter/kitex_gen/stablepay/common"
)

// TxStatusRPCAdapter 交易状态查询 RPC 适配器
type TxStatusRPCAdapter struct {
	txStatusService *service.TxStatusQueryService
}

// NewTxStatusRPCAdapter 创建交易状态查询适配器
func NewTxStatusRPCAdapter(txStatusService *service.TxStatusQueryService) *TxStatusRPCAdapter {
	return &TxStatusRPCAdapter{
		txStatusService: txStatusService,
	}
}

// GetTxStatus 查询交易状态
func (a *TxStatusRPCAdapter) GetTxStatus(
	ctx context.Context,
	req *blockchain_adapter.GetTxStatusRequest,
) (*blockchain_adapter.GetTxStatusResponse, error) {
	// 1. 参数校验
	if req == nil {
		return a.buildErrorResponse(common.ErrorCode_INVALID_PARAMETERS, "request is nil", nil), nil
	}
	if req.TxHash == "" {
		return a.buildErrorResponse(common.ErrorCode_INVALID_PARAMETERS, "tx_hash is required", req.Base), nil
	}

	// 2. 构建查询参数
	query := &service.TxStatusQuery{
		TxHash: req.TxHash,
	}

	// 3. 调用应用服务查询状态
	status, err := a.txStatusService.GetTxStatus(ctx, query)
	if err != nil {
		return a.buildErrorResponse(a.mapErrorCode(err), err.Error(), req.Base), nil
	}

	// 4. 转换状态为响应格式
	txStatus := a.mapStatusToResponse(status.Status)

	// 5. 构建成功响应
	return &blockchain_adapter.GetTxStatusResponse{
		Base: &common.BaseResp{
			Code:    int32(common.ErrorCode_SUCCESS),
			Message: "success",
		},
		Status: txStatus,
	}, nil
}

// mapStatusToResponse 将领域状态映射为响应状态
func (a *TxStatusRPCAdapter) mapStatusToResponse(status string) blockchain_adapter.TxStatus {
	switch status {
	case "confirmed", "finalized":
		return blockchain_adapter.TxStatus_CONFIRMED
	case "failed":
		return blockchain_adapter.TxStatus_FAILED
	default:
		return blockchain_adapter.TxStatus_PENDING
	}
}

// mapErrorCode 错误码映射
func (a *TxStatusRPCAdapter) mapErrorCode(err error) common.ErrorCode {
	if err == nil {
		return common.ErrorCode_SUCCESS
	}

	errMsg := err.Error()

	// 参数错误
	if containsIgnoreCase(errMsg, "required") || containsIgnoreCase(errMsg, "invalid") {
		return common.ErrorCode_INVALID_PARAMETERS
	}

	// 网络错误
	if containsIgnoreCase(errMsg, "network") || containsIgnoreCase(errMsg, "timeout") || containsIgnoreCase(errMsg, "connection") {
		return common.ErrorCode_BLOCKCHAIN_NETWORK_ERROR
	}

	// 未找到
	if containsIgnoreCase(errMsg, "not found") {
		return common.ErrorCode_RESOURCE_NOT_FOUND
	}

	return common.ErrorCode_INTERNAL_SERVER_ERROR
}

// buildErrorResponse 构建错误响应
func (a *TxStatusRPCAdapter) buildErrorResponse(
	code common.ErrorCode,
	message string,
	baseReq *common.BaseReq,
) *blockchain_adapter.GetTxStatusResponse {
	return &blockchain_adapter.GetTxStatusResponse{
		Base: &common.BaseResp{
			Code:    int32(code),
			Message: message,
		},
		Status: blockchain_adapter.TxStatus_FAILED,
	}
}

// buildErrorResponse without baseReq (for nil request case)
func (a *TxStatusRPCAdapter) buildErrorResponseWithoutBase(
	code common.ErrorCode,
	message string,
) *blockchain_adapter.GetTxStatusResponse {
	return &blockchain_adapter.GetTxStatusResponse{
		Base: &common.BaseResp{
			Code:    int32(code),
			Message: message,
		},
		Status: blockchain_adapter.TxStatus_FAILED,
	}
}
