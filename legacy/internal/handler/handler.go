// Package handler 提供 RPC 接口实现
// 聚合所有 Handler，统一暴露给 Kitex 服务
package handler

import (
	"context"

	"stablepay.blockchain_adapter/kitex_gen/stablepay/blockchain_adapter"
)

// BlockchainAdapterHandler 聚合所有 RPC 接口实现
// 实现 kitex_gen 生成的 BlockchainAdapterService 接口
type BlockchainAdapterHandler struct {
	transferHandler  *TransferHandler
	balanceHandler   *BalanceHandler
	txStatusHandler  *TxStatusHandler
}

// NewBlockchainAdapterHandler 创建聚合处理器
func NewBlockchainAdapterHandler(
	transferHandler *TransferHandler,
	balanceHandler *BalanceHandler,
	txStatusHandler *TxStatusHandler,
) *BlockchainAdapterHandler {
	return &BlockchainAdapterHandler{
		transferHandler:  transferHandler,
		balanceHandler:   balanceHandler,
		txStatusHandler:  txStatusHandler,
	}
}

// TransferStableCoin 实现 RPC 接口
// 调用 TransferHandler 处理转账请求
func (h *BlockchainAdapterHandler) TransferStableCoin(
	ctx context.Context,
	req *blockchain_adapter.TransferStableCoinRequest,
) (*blockchain_adapter.TransferStableCoinResponse, error) {
	return h.transferHandler.TransferStableCoin(ctx, req)
}

// GetBalance 实现 RPC 接口
// 调用 BalanceHandler 处理余额查询请求
func (h *BlockchainAdapterHandler) GetBalance(
	ctx context.Context,
	req *blockchain_adapter.GetBalanceRequest,
) (*blockchain_adapter.GetBalanceResponse, error) {
	return h.balanceHandler.GetBalance(ctx, req)
}

// GetTxStatus 实现 RPC 接口
// 调用 TxStatusHandler 处理交易状态查询请求
func (h *BlockchainAdapterHandler) GetTxStatus(
	ctx context.Context,
	req *blockchain_adapter.GetTxStatusRequest,
) (*blockchain_adapter.GetTxStatusResponse, error) {
	return h.txStatusHandler.GetTxStatus(ctx, req)
}
