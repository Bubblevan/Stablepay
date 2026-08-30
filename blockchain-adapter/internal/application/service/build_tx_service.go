// Package service 应用服务层 - 构造未签名交易
package service

import (
	"context"
	"fmt"

	"github.com/stablepay/blockchain-adapter/internal/domain/gateway"
)

// BuildTxService 构造交易应用服务
type BuildTxService struct {
	solanaGateway gateway.SolanaGateway
	txBuilder     gateway.TransactionBuilderGateway
	hotWallet     gateway.HotWalletGateway
}

// BuildTxCmd 构造交易命令
type BuildTxCmd struct {
	FromAddress string
	ToAddress   string
	AmountMinor int64
	Currency    string
	TxID        string
}

// BuildTxResult 构造交易结果
type BuildTxResult struct {
	UnsignedTxBase64 string
	RecentBlockhash  string
	FeeEstimateMinor int64
}

// NewBuildTxService 创建服务
func NewBuildTxService(
	solanaGateway gateway.SolanaGateway,
	txBuilder gateway.TransactionBuilderGateway,
	hotWallet gateway.HotWalletGateway,
) *BuildTxService {
	return &BuildTxService{
		solanaGateway: solanaGateway,
		txBuilder:     txBuilder,
		hotWallet:     hotWallet,
	}
}

// Execute 执行构造交易
func (s *BuildTxService) Execute(ctx context.Context, cmd *BuildTxCmd) (*BuildTxResult, error) {
	// 1. 验证参数
	if err := s.validateCmd(cmd); err != nil {
		return nil, err
	}

	// 2. 获取最近的 blockhash
	blockhash, err := s.solanaGateway.GetRecentBlockhash(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get recent blockhash: %w", err)
	}

	// 3. 估算手续费
	feeEstimate, err := s.solanaGateway.EstimateFee(ctx)
	if err != nil {
		// 使用默认估算值
		feeEstimate = 5000 // 0.000005 SOL in lamports
	}

	// 4. 构建未签名交易
	buildReq := &gateway.BuildTransactionRequest{
		FromAddress:     cmd.FromAddress,
		ToAddress:       cmd.ToAddress,
		AmountMinor:     cmd.AmountMinor,
		Currency:        cmd.Currency,
		RecentBlockhash: blockhash,
		FeePayerAddress: s.hotWallet.GetAddress(),
	}

	unsignedTxBase64, err := s.txBuilder.BuildUnsignedTransaction(buildReq)
	if err != nil {
		return nil, fmt.Errorf("failed to build transaction: %w", err)
	}

	return &BuildTxResult{
		UnsignedTxBase64: unsignedTxBase64,
		RecentBlockhash:  blockhash,
		FeeEstimateMinor: feeEstimate,
	}, nil
}

// validateCmd 验证命令
func (s *BuildTxService) validateCmd(cmd *BuildTxCmd) error {
	if cmd.FromAddress == "" {
		return fmt.Errorf("from address is required")
	}
	if cmd.ToAddress == "" {
		return fmt.Errorf("to address is required")
	}
	if cmd.AmountMinor <= 0 {
		return fmt.Errorf("amount must be greater than 0")
	}
	return nil
}
