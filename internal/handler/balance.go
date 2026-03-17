// Package handler 提供 RPC 接口实现
// BalanceHandler 实现余额查询相关接口
package handler

import (
	"context"
	"fmt"

	"stablepay.blockchain_adapter/internal/service"
	"stablepay.blockchain_adapter/kitex_gen/stablepay/blockchain_adapter"
	"stablepay.blockchain_adapter/kitex_gen/stablepay/common"
)

// BalanceHandler 余额处理器
type BalanceHandler struct {
	balanceSvc *service.BalanceService
}

// NewBalanceHandler 创建余额处理器
func NewBalanceHandler(balanceSvc *service.BalanceService) *BalanceHandler {
	return &BalanceHandler{
		balanceSvc: balanceSvc,
	}
}

// GetBalance 查询钱包余额
// 实现 blockchain-adapter.thrift 定义的 RPC 接口
func (h *BalanceHandler) GetBalance(
	ctx context.Context,
	req *blockchain_adapter.GetBalanceRequest,
) (*blockchain_adapter.GetBalanceResponse, error) {
	// 参数校验
	if err := h.validateRequest(req); err != nil {
		return h.buildErrorResponse(
			common.ErrorCode_INVALID_PARAMETERS,
			fmt.Sprintf("invalid request: %v", err),
			req.Base,
		), nil
	}

	// 调用服务层查询余额
	balanceInfo, err := h.balanceSvc.GetBalance(ctx, req.WalletAddress, req.Currency)
	if err != nil {
		return h.buildErrorResponse(
			common.ErrorCode_BLOCKCHAIN_NETWORK_ERROR,
			fmt.Sprintf("failed to get balance: %v", err),
			req.Base,
		), nil
	}

	// 构造成功响应
	return &blockchain_adapter.GetBalanceResponse{
		Base: &common.BaseResp{
			Code:      0,
			Message:   "success",
			RequestId: strPtr(req.Base.GetRequestId()),
			TraceId:   strPtr(req.Base.GetTraceId()),
		},
		BalanceMinor: int64(balanceInfo.Balance), // uint64 → int64
		Currency:     req.Currency,
	}, nil
}

// validateRequest 校验余额查询请求
func (h *BalanceHandler) validateRequest(req *blockchain_adapter.GetBalanceRequest) error {
	if req == nil {
		return fmt.Errorf("request is nil")
	}

	// 校验钱包地址
	if req.WalletAddress == "" {
		return fmt.Errorf("wallet_address is required")
	}

	// 校验地址格式（Solana 地址 Base58 编码，长度约 32-44 字符）
	if len(req.WalletAddress) < 32 || len(req.WalletAddress) > 48 {
		return fmt.Errorf("invalid wallet_address format")
	}

	// 校验币种
	if req.Currency != common.Currency_USDC && req.Currency != common.Currency_USDT {
		return fmt.Errorf("unsupported currency: %v", req.Currency)
	}

	return nil
}

// buildErrorResponse 构建错误响应
func (h *BalanceHandler) buildErrorResponse(
	code common.ErrorCode,
	message string,
	baseReq *common.BaseReq,
) *blockchain_adapter.GetBalanceResponse {
	return &blockchain_adapter.GetBalanceResponse{
		Base: &common.BaseResp{
			Code:      int32(code),
			Message:   message,
			RequestId: strPtr(baseReq.GetRequestId()),
			TraceId:   strPtr(baseReq.GetTraceId()),
		},
		BalanceMinor: 0,
		Currency:     0,
	}
}

// GetSOLBalance 查询 SOL 余额（扩展接口）
// 注意：这不是 Thrift 定义的接口，如需暴露需要在 IDL 中添加
func (h *BalanceHandler) GetSOLBalance(
	ctx context.Context,
	walletAddress string,
	baseReq *common.BaseReq,
) (*blockchain_adapter.GetBalanceResponse, error) {
	// 校验地址
	if walletAddress == "" {
		return h.buildErrorResponse(
			common.ErrorCode_INVALID_PARAMETERS,
			"wallet_address is required",
			baseReq,
		), nil
	}

	// 调用服务层
	balanceInfo, err := h.balanceSvc.GetSOLBalance(ctx, walletAddress)
	if err != nil {
		return h.buildErrorResponse(
			common.ErrorCode_BLOCKCHAIN_NETWORK_ERROR,
			fmt.Sprintf("failed to get SOL balance: %v", err),
			baseReq,
		), nil
	}

	// 构造响应
	return &blockchain_adapter.GetBalanceResponse{
		Base: &common.BaseResp{
			Code:      0,
			Message:   "success",
			RequestId: strPtr(baseReq.GetRequestId()),
			TraceId:   strPtr(baseReq.GetTraceId()),
		},
		BalanceMinor: int64(balanceInfo.Balance),
		Currency:     common.Currency_USDC, // SOL 没有对应枚举，暂时使用 USDC
	}, nil
}
