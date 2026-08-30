// Package rpc 余额查询RPC适配器
package rpc

import (
	"context"

	"github.com/stablepay/blockchain-adapter/internal/application/service"
	"github.com/stablepay/blockchain-adapter/kitex_gen/stablepay/blockchain_adapter"
	"github.com/stablepay/blockchain-adapter/kitex_gen/stablepay/common"
)

// BalanceRPCAdapter 余额RPC适配器
type BalanceRPCAdapter struct {
	balanceService *service.BalanceQueryService
}

// NewBalanceRPCAdapter 创建适配器
func NewBalanceRPCAdapter(balanceService *service.BalanceQueryService) *BalanceRPCAdapter {
	return &BalanceRPCAdapter{
		balanceService: balanceService,
	}
}

// GetBalance 实现RPC接口
func (a *BalanceRPCAdapter) GetBalance(
	ctx context.Context,
	req *blockchain_adapter.GetBalanceRequest,
) (*blockchain_adapter.GetBalanceResponse, error) {
	// 参数校验
	if req.WalletAddress == "" {
		return a.buildErrorResponse(common.ErrorCode_INVALID_PARAMETERS, "wallet_address is required", req.Base), nil
	}

	// 构造查询
	query := &service.BalanceQuery{
		WalletAddress: req.WalletAddress,
		Currency:      req.Currency.String(), // 将 common.Currency 转换为字符串
	}

	// 调用服务
	balanceVO, err := a.balanceService.GetBalance(ctx, query)
	if err != nil {
		return a.buildErrorResponse(common.ErrorCode_BLOCKCHAIN_NETWORK_ERROR, err.Error(), req.Base), nil
	}

	// 构造响应
	return &blockchain_adapter.GetBalanceResponse{
		Base: &common.BaseResp{
			Code:    0,
			Message: "success",
		},
		BalanceMinor: int64(balanceVO.Balance),
		Currency:     req.Currency,
	}, nil
}

func (a *BalanceRPCAdapter) buildErrorResponse(code common.ErrorCode, message string, baseReq *common.BaseReq) *blockchain_adapter.GetBalanceResponse {
	return &blockchain_adapter.GetBalanceResponse{
		Base: &common.BaseResp{
			Code:    int32(code),
			Message: message,
		},
	}
}
