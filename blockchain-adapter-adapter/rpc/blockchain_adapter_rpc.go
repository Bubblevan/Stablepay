// Package rpc 聚合所有 RPC 适配器，实现 Kitex 接口
package rpc

import (
	"context"

	"stablepay.blockchain_adapter/kitex_gen/stablepay/blockchain_adapter"
)

// BlockchainAdapterRPC 实现所有 RPC 接口
// 职责：聚合所有适配器，统一对外提供 RPC 服务
type BlockchainAdapterRPC struct {
	transferAdapter *TransferRPCAdapter
	balanceAdapter  *BalanceRPCAdapter
	txStatusAdapter *TxStatusRPCAdapter
}

// NewBlockchainAdapterRPC 创建聚合 RPC 适配器
func NewBlockchainAdapterRPC(
	transferAdapter *TransferRPCAdapter,
	balanceAdapter *BalanceRPCAdapter,
	txStatusAdapter *TxStatusRPCAdapter,
) *BlockchainAdapterRPC {
	return &BlockchainAdapterRPC{
		transferAdapter: transferAdapter,
		balanceAdapter:  balanceAdapter,
		txStatusAdapter: txStatusAdapter,
	}
}

// TransferStableCoin 实现转账接口
// 转发到 TransferRPCAdapter 处理
func (s *BlockchainAdapterRPC) TransferStableCoin(
	ctx context.Context,
	req *blockchain_adapter.TransferStableCoinRequest,
) (*blockchain_adapter.TransferStableCoinResponse, error) {
	return s.transferAdapter.TransferStableCoin(ctx, req)
}

// GetBalance 实现余额查询接口
// 转发到 BalanceRPCAdapter 处理
func (s *BlockchainAdapterRPC) GetBalance(
	ctx context.Context,
	req *blockchain_adapter.GetBalanceRequest,
) (*blockchain_adapter.GetBalanceResponse, error) {
	return s.balanceAdapter.GetBalance(ctx, req)
}

// GetTxStatus 实现交易状态查询接口
// 转发到 TxStatusRPCAdapter 处理
func (s *BlockchainAdapterRPC) GetTxStatus(
	ctx context.Context,
	req *blockchain_adapter.GetTxStatusRequest,
) (*blockchain_adapter.GetTxStatusResponse, error) {
	return s.txStatusAdapter.GetTxStatus(ctx, req)
}


