// Package rpc Adapter层RPC适配器
// 职责：接收外部请求，转换为应用服务命令，调用领域逻辑
package rpc

import (
	"context"
	"fmt"
	"strings"

	"github.com/stablepay/blockchain-adapter/app/service"
	"github.com/stablepay/blockchain-adapter/adapter/rpc/assembler"
	"github.com/stablepay/blockchain-adapter/kitex_gen/stablepay/blockchain_adapter"
	"github.com/stablepay/blockchain-adapter/kitex_gen/stablepay/common"
)

// TransferRPCAdapter 转账RPC适配器
type TransferRPCAdapter struct {
	transferService *service.TransferCmdService
}

// NewTransferRPCAdapter 创建适配器
func NewTransferRPCAdapter(transferService *service.TransferCmdService) *TransferRPCAdapter {
	return &TransferRPCAdapter{
		transferService: transferService,
	}
}

// TransferStableCoin 实现RPC接口
func (a *TransferRPCAdapter) TransferStableCoin(
	ctx context.Context,
	req *blockchain_adapter.TransferStableCoinRequest,
) (*blockchain_adapter.TransferStableCoinResponse, error) {
	// 1. 参数校验
	if err := a.validateRequest(req); err != nil {
		return a.buildErrorResponse(common.ErrorCode_INVALID_PARAMETERS, err.Error(), req.Base), nil
	}

	// 2. Assembler转换请求
	cmd := assembler.ToTransferCmd(req)

	// 3. 调用应用服务
	result, err := a.transferService.Execute(ctx, cmd)
	if err != nil {
		return a.buildErrorResponse(a.mapErrorCode(err), err.Error(), req.Base), nil
	}

	// 4. Assembler转换响应
	return assembler.ToTransferResponse(result, req.Base), nil
}

// validateRequest 校验请求
func (a *TransferRPCAdapter) validateRequest(req *blockchain_adapter.TransferStableCoinRequest) error {
	if req == nil {
		return fmt.Errorf("request is nil")
	}
	if req.ToWalletAddress == "" {
		return fmt.Errorf("to_wallet_address is required")
	}
	if req.AmountMinor <= 0 {
		return fmt.Errorf("amount must be greater than 0")
	}
	return nil
}

// mapErrorCode 错误码映射
// 根据错误类型将内部错误映射为标准的 RPC 错误码
func (a *TransferRPCAdapter) mapErrorCode(err error) common.ErrorCode {
	if err == nil {
		return common.ErrorCode_SUCCESS
	}

	errMsg := strings.ToLower(err.Error())

	// 参数错误
	switch {
	case strings.Contains(errMsg, "invalid") || strings.Contains(errMsg, "required"):
		return common.ErrorCode_INVALID_PARAMETERS

	// 余额不足
	case strings.Contains(errMsg, "insufficient"):
		return common.ErrorCode_INSUFFICIENT_BALANCE

	// 网络超时或区块链网络错误
	case strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "network"):
		return common.ErrorCode_BLOCKCHAIN_NETWORK_ERROR

	// 未找到
	case strings.Contains(errMsg, "not found"):
		return common.ErrorCode_RESOURCE_NOT_FOUND

	// 默认内部错误
	default:
		return common.ErrorCode_INTERNAL_SERVER_ERROR
	}
}

// buildErrorResponse 构建错误响应
func (a *TransferRPCAdapter) buildErrorResponse(
	code common.ErrorCode,
	message string,
	baseReq *common.BaseReq,
) *blockchain_adapter.TransferStableCoinResponse {
	return &blockchain_adapter.TransferStableCoinResponse{
		Base: &common.BaseResp{
			Code:    int32(code),
			Message: message,
		},
		Status: blockchain_adapter.TxStatus_FAILED,
	}
}
