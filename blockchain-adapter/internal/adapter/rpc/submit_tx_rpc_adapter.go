// Package rpc Adapter层RPC适配器 - 提交已签名交易
package rpc

import (
	"context"
	"fmt"

	"github.com/stablepay/blockchain-adapter/internal/application/service"
	"github.com/stablepay/blockchain-adapter/kitex_gen/stablepay/blockchain_adapter"
	"github.com/stablepay/blockchain-adapter/kitex_gen/stablepay/common"
)

// SubmitTxRPCAdapter 提交交易RPC适配器
type SubmitTxRPCAdapter struct {
	submitTxService *service.SubmitTxService
}

// NewSubmitTxRPCAdapter 创建适配器
func NewSubmitTxRPCAdapter(submitTxService *service.SubmitTxService) *SubmitTxRPCAdapter {
	return &SubmitTxRPCAdapter{
		submitTxService: submitTxService,
	}
}

// SubmitSignedTransaction 实现RPC接口
func (a *SubmitTxRPCAdapter) SubmitSignedTransaction(
	ctx context.Context,
	req *blockchain_adapter.SubmitSignedTransactionRequest,
) (*blockchain_adapter.SubmitSignedTransactionResponse, error) {
	// 1. 参数校验
	if err := a.validateRequest(req); err != nil {
		return a.buildErrorResponse(common.ErrorCode_INVALID_PARAMETERS, err.Error(), req.Base), nil
	}

	// 2. 构建命令
	txID := req.GetTxId()
	cmd := &service.SubmitTxCmd{
		SignedTxBase64: req.SignedTxBase64,
		TxID:           txID,
	}

	// 3. 调用应用服务
	result, err := a.submitTxService.Execute(ctx, cmd)
	if err != nil {
		return a.buildErrorResponse(a.mapErrorCode(err), err.Error(), req.Base), nil
	}

	// 4. 构建响应
	reqId := req.Base.GetRequestId()
	traceId := req.Base.GetTraceId()
	return &blockchain_adapter.SubmitSignedTransactionResponse{
		Base: &common.BaseResp{
			Code:    0,
			Message: "success",
			RequestId: &reqId,
			TraceId:   &traceId,
		},
		TxHash: result.TxHash,
		Status: mapStatus(result.Status),
	}, nil
}

// validateRequest 校验请求
func (a *SubmitTxRPCAdapter) validateRequest(req *blockchain_adapter.SubmitSignedTransactionRequest) error {
	if req == nil {
		return fmt.Errorf("request is nil")
	}
	if req.SignedTxBase64 == "" {
		return fmt.Errorf("signed_tx_base64 is required")
	}
	return nil
}

// mapErrorCode 错误码映射
func (a *SubmitTxRPCAdapter) mapErrorCode(err error) common.ErrorCode {
	if err == nil {
		return common.ErrorCode_SUCCESS
	}
	errMsg := err.Error()
	switch {
	case errMsg == "invalid transaction format":
		return common.ErrorCode_INVALID_PARAMETERS
	case errMsg == "fee payer validation failed":
		return common.ErrorCode_INVALID_PARAMETERS
	case errMsg == "failed to send transaction":
		return common.ErrorCode_BLOCKCHAIN_NETWORK_ERROR
	default:
		return common.ErrorCode_INTERNAL_SERVER_ERROR
	}
}

// buildErrorResponse 构建错误响应
func (a *SubmitTxRPCAdapter) buildErrorResponse(
	code common.ErrorCode,
	message string,
	baseReq *common.BaseReq,
) *blockchain_adapter.SubmitSignedTransactionResponse {
	resp := &blockchain_adapter.SubmitSignedTransactionResponse{
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

// mapStatus 状态映射
func mapStatus(status string) blockchain_adapter.TxStatus {
	switch status {
	case "confirmed":
		return blockchain_adapter.TxStatus_CONFIRMED
	case "failed":
		return blockchain_adapter.TxStatus_FAILED
	default:
		return blockchain_adapter.TxStatus_PENDING
	}
}
