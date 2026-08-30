// Package rpc Adapter层RPC适配器 - 构造未签名交易
package rpc

import (
	"context"
	"fmt"

	"github.com/stablepay/blockchain-adapter/internal/application/service"
	"github.com/stablepay/blockchain-adapter/kitex_gen/stablepay/blockchain_adapter"
	"github.com/stablepay/blockchain-adapter/kitex_gen/stablepay/common"
)

// BuildTxRPCAdapter 构造交易RPC适配器
type BuildTxRPCAdapter struct {
	buildTxService *service.BuildTxService
}

// NewBuildTxRPCAdapter 创建适配器
func NewBuildTxRPCAdapter(buildTxService *service.BuildTxService) *BuildTxRPCAdapter {
	return &BuildTxRPCAdapter{
		buildTxService: buildTxService,
	}
}

// BuildUnsignedTransaction 实现RPC接口
func (a *BuildTxRPCAdapter) BuildUnsignedTransaction(
	ctx context.Context,
	req *blockchain_adapter.BuildUnsignedTransactionRequest,
) (*blockchain_adapter.BuildUnsignedTransactionResponse, error) {
	// 1. 参数校验
	if err := a.validateRequest(req); err != nil {
		return a.buildErrorResponse(common.ErrorCode_INVALID_PARAMETERS, err.Error(), req.Base), nil
	}

	// 2. 构建命令
	txID := req.GetTxId()
	cmd := &service.BuildTxCmd{
		FromAddress: req.FromWalletAddress,
		ToAddress:   req.ToWalletAddress,
		AmountMinor: req.AmountMinor,
		Currency:    req.Currency.String(),
		TxID:        txID,
	}

	// 3. 调用应用服务
	result, err := a.buildTxService.Execute(ctx, cmd)
	if err != nil {
		return a.buildErrorResponse(a.mapErrorCode(err), err.Error(), req.Base), nil
	}

	// 4. 构建响应
	reqId := req.Base.GetRequestId()
	traceId := req.Base.GetTraceId()
	return &blockchain_adapter.BuildUnsignedTransactionResponse{
		Base: &common.BaseResp{
			Code:    0,
			Message: "success",
			RequestId: &reqId,
			TraceId:   &traceId,
		},
		UnsignedTxBase64:   result.UnsignedTxBase64,
		RecentBlockhash:    result.RecentBlockhash,
		FeeEstimateMinor:   result.FeeEstimateMinor,
	}, nil
}

// validateRequest 校验请求
func (a *BuildTxRPCAdapter) validateRequest(req *blockchain_adapter.BuildUnsignedTransactionRequest) error {
	if req == nil {
		return fmt.Errorf("request is nil")
	}
	if req.FromWalletAddress == "" {
		return fmt.Errorf("from_wallet_address is required")
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
func (a *BuildTxRPCAdapter) mapErrorCode(err error) common.ErrorCode {
	if err == nil {
		return common.ErrorCode_SUCCESS
	}
	errMsg := err.Error()
	switch {
	case errMsg == "insufficient balance":
		return common.ErrorCode_INSUFFICIENT_BALANCE
	case errMsg == "failed to get recent blockhash":
		return common.ErrorCode_BLOCKCHAIN_NETWORK_ERROR
	default:
		return common.ErrorCode_INTERNAL_SERVER_ERROR
	}
}

// buildErrorResponse 构建错误响应
func (a *BuildTxRPCAdapter) buildErrorResponse(
	code common.ErrorCode,
	message string,
	baseReq *common.BaseReq,
) *blockchain_adapter.BuildUnsignedTransactionResponse {
	resp := &blockchain_adapter.BuildUnsignedTransactionResponse{
		Base: &common.BaseResp{
			Code:    int32(code),
			Message: message,
		},
	}
	if baseReq != nil {
		reqId := baseReq.GetRequestId()
		traceId := baseReq.GetTraceId()
		resp.Base.RequestId = &reqId
		resp.Base.TraceId = &traceId
	}
	return resp
}
