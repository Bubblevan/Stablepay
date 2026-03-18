package main

import (
	"context"
	blockchain_adapter "stablepay.blockchain_adapter/kitex_gen/stablepay/blockchain_adapter"
)

// BlockchainAdapterServiceImpl implements the last service interface defined in the IDL.
type BlockchainAdapterServiceImpl struct{}

// TransferStableCoin implements the BlockchainAdapterServiceImpl interface.
func (s *BlockchainAdapterServiceImpl) TransferStableCoin(ctx context.Context, req *blockchain_adapter.TransferStableCoinRequest) (resp *blockchain_adapter.TransferStableCoinResponse, err error) {
	// TODO: Your code here...
	return
}

// GetBalance implements the BlockchainAdapterServiceImpl interface.
func (s *BlockchainAdapterServiceImpl) GetBalance(ctx context.Context, req *blockchain_adapter.GetBalanceRequest) (resp *blockchain_adapter.GetBalanceResponse, err error) {
	// TODO: Your code here...
	return
}

// GetTxStatus implements the BlockchainAdapterServiceImpl interface.
func (s *BlockchainAdapterServiceImpl) GetTxStatus(ctx context.Context, req *blockchain_adapter.GetTxStatusRequest) (resp *blockchain_adapter.GetTxStatusResponse, err error) {
	// TODO: Your code here...
	return
}
