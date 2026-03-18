// Package rpc Adapter层RPC适配器
// 职责：接收外部请求，转换为应用服务命令，调用领域逻辑
package rpc

import (
	"context"
	"fmt"

	"stablepay.blockchain_adapter/blockchain-adapter-app/service"
	"stablepay.blockchain_adapter/blockchain-adapter-adapter/rpc/assembler"
	"stablepay.blockchain_adapter/kitex_gen/stablepay/blockchain_adapter"
	"stablepay.blockchain_adapter/kitex_gen/stablepay/common"
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

	errMsg := err.Error()

	// 参数错误
	switch {
	case containsAny(errMsg, []string{"invalid parameters", "is required", "must be greater", "invalid address"}):
		return common.ErrorCode_INVALID_PARAMETERS

	// 余额不足
	case containsAny(errMsg, []string{"insufficient balance", "insufficient funds", "InsufficientFunds"}):
		return common.ErrorCode_INSUFFICIENT_BALANCE

	// 交易相关错误
	case containsAny(errMsg, []string{"invalid transaction", "transaction"}):
		return common.ErrorCode_INVALID_PARAMETERS

	// 签名错误
	case containsAny(errMsg, []string{"sign", "signature"}):
		return common.ErrorCode_SIGNATURE_VERIFICATION_FAILED

	// 网络超时或区块链网络错误
	case containsAny(errMsg, []string{"timeout", "deadline exceeded", "context canceled", "network", "connection", "rpc"}):
		return common.ErrorCode_BLOCKCHAIN_NETWORK_ERROR

	// 已存在/重复交易
	case containsAny(errMsg, []string{"already exists", "duplicate"}):
		return common.ErrorCode_PAYMENT_ALREADY_EXISTS

	// 未找到
	case containsAny(errMsg, []string{"not found", "not exist"}):
		return common.ErrorCode_RESOURCE_NOT_FOUND

	// 数据库错误
	case containsAny(errMsg, []string{"database", "db", "sql"}):
		return common.ErrorCode_DATABASE_CONNECTION_ERROR

	// 默认内部错误
	default:
		return common.ErrorCode_INTERNAL_SERVER_ERROR
	}
}

// containsAny 检查字符串是否包含任意一个子串
func containsAny(s string, substrs []string) bool {
	for _, substr := range substrs {
		if containsIgnoreCase(s, substr) {
			return true
		}
	}
	return false
}

// containsIgnoreCase 忽略大小写检查是否包含子串
func containsIgnoreCase(s, substr string) bool {
	if len(substr) > len(s) {
		return false
	}
	// 简单的忽略大小写比较
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if toLower(s[i+j]) != toLower(substr[j]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// toLower 将字节转换为小写
func toLower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
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
