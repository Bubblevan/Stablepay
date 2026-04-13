// Package rpc Adapter层RPC适配器
// 职责：接收外部请求，转换为应用服务命令，调用领域逻辑
package rpc

import (
	"context"
	"fmt"
	"log"
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

func signedTxBase64Len(req *blockchain_adapter.TransferStableCoinRequest) int {
	if req == nil || !req.IsSetSignedTxBase64() {
		return 0
	}
	return len(req.GetSignedTxBase64())
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
		mapped := a.mapErrorCode(err)
		log.Printf("[TransferStableCoin] Execute failed: %v | mapped_code=%d from=%v to=%s amount_minor=%d signed_tx_len=%d",
			err, mapped, req.FromWalletAddress, req.ToWalletAddress, req.AmountMinor, signedTxBase64Len(req))
		return a.buildErrorResponse(mapped, err.Error(), req.Base), nil
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

	// 1) 明确的本地校验 / 客户端交易格式问题 → INVALID_PARAMETERS（勿用宽泛的 "invalid" 子串）
	switch {
	case strings.Contains(errMsg, "request is nil"),
		strings.Contains(errMsg, "to_wallet_address is required"),
		strings.Contains(errMsg, "amount must be greater than 0"),
		strings.Contains(errMsg, "transaction is empty"),
		strings.Contains(errMsg, "invalid transaction format"),
		strings.Contains(errMsg, "failed to unmarshal transaction"),
		strings.Contains(errMsg, "fee payer validation failed"),
		strings.Contains(errMsg, "hot wallet is not the fee payer"),
		strings.Contains(errMsg, "signed_tx_base64 is required when"):
		return common.ErrorCode_INVALID_PARAMETERS
	}

	// 2) Solana JSON-RPC / 模拟 / 广播（含 "invalid account" 等，避免误判为 10001 → payment HTTP 400）
	if strings.Contains(errMsg, "jsonrpc") ||
		strings.Contains(errMsg, "-32002") ||
		strings.Contains(errMsg, "simulation") ||
		strings.Contains(errMsg, "sendtransaction") ||
		strings.Contains(errMsg, "failed to send transaction") ||
		strings.Contains(errMsg, "blockhash") ||
		strings.Contains(errMsg, "program error") ||
		strings.Contains(errMsg, "instruction") {
		return common.ErrorCode_BLOCKCHAIN_NETWORK_ERROR
	}

	// 3) 余额
	if strings.Contains(errMsg, "insufficient") {
		return common.ErrorCode_INSUFFICIENT_BALANCE
	}

	// 4) 网络 / 超时
	if strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "network") {
		return common.ErrorCode_BLOCKCHAIN_NETWORK_ERROR
	}

	// 5) 其它 not found（如仓储）；BlockhashNotFound 通常含 blockhash 已在上面覆盖
	if strings.Contains(errMsg, "not found") {
		return common.ErrorCode_RESOURCE_NOT_FOUND
	}

	// 6) 其余含 invalid/required 的视为参数问题
	if strings.Contains(errMsg, "invalid") || strings.Contains(errMsg, "required") {
		return common.ErrorCode_INVALID_PARAMETERS
	}

	return common.ErrorCode_INTERNAL_SERVER_ERROR
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
