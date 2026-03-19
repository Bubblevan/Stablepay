// Package assembler 定义对象组装器
// 职责：转换 RPC DTO 和 应用层 Command/Result
package assembler

import (
	"github.com/stablepay/blockchain-adapter/app/service"
	"github.com/stablepay/blockchain-adapter/kitex_gen/stablepay/blockchain_adapter"
	"github.com/stablepay/blockchain-adapter/kitex_gen/stablepay/common"
)

// ToTransferCmd 将RPC请求转换为应用层命令
func ToTransferCmd(req *blockchain_adapter.TransferStableCoinRequest) *service.TransferCmd {
	cmd := &service.TransferCmd{
		ToAddress:   req.ToWalletAddress,
		AmountMinor: req.AmountMinor,
		Currency:    req.Currency.String(), // 将 common.Currency 转换为字符串
	}

	// 处理可选字段
	if req.FromWalletAddress != nil {
		cmd.FromAddress = *req.FromWalletAddress
	}
	if req.SignedTxBase64 != nil {
		cmd.SignedTxBase64 = *req.SignedTxBase64
	}
	if req.Base != nil && req.Base.IdempotencyKey != nil {
		cmd.TxID = *req.Base.IdempotencyKey
	}

	return cmd
}

// ToTransferResponse 将应用层结果转换为RPC响应
func ToTransferResponse(
	result *service.TransferResult,
	baseReq *common.BaseReq,
) *blockchain_adapter.TransferStableCoinResponse {
	resp := &blockchain_adapter.TransferStableCoinResponse{
		Base: &common.BaseResp{
			Code:    0,
			Message: "success",
		},
		TxHash: result.TxHash,
		Status: mapStatus(result.Status),
	}

	// 设置可选字段
	if baseReq != nil {
		if baseReq.RequestId != nil {
			resp.Base.RequestId = baseReq.RequestId
		}
		if baseReq.TraceId != nil {
			resp.Base.TraceId = baseReq.TraceId
		}
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
