// Package service 应用服务层 - 提交已签名交易
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/stablepay/blockchain-adapter/internal/domain/entity"
	"github.com/stablepay/blockchain-adapter/internal/domain/gateway"
)

// SubmitTxService 提交交易应用服务
type SubmitTxService struct {
	solanaGateway gateway.SolanaGateway
	subsidyRepo   gateway.GasSubsidyRepositoryGateway
	hotWallet     gateway.HotWalletGateway
}

// SubmitTxCmd 提交交易命令
type SubmitTxCmd struct {
	SignedTxBase64 string
	TxID           string
}

// SubmitTxResult 提交交易结果
type SubmitTxResult struct {
	TxHash string
	Status string
}

// NewSubmitTxService 创建服务
func NewSubmitTxService(
	solanaGateway gateway.SolanaGateway,
	subsidyRepo gateway.GasSubsidyRepositoryGateway,
	hotWallet gateway.HotWalletGateway,
) *SubmitTxService {
	return &SubmitTxService{
		solanaGateway: solanaGateway,
		subsidyRepo:   subsidyRepo,
		hotWallet:     hotWallet,
	}
}

// Execute 执行提交交易
func (s *SubmitTxService) Execute(ctx context.Context, cmd *SubmitTxCmd) (*SubmitTxResult, error) {
	// 1. 验证交易格式
	if err := s.validateTransactionFormat(cmd.SignedTxBase64); err != nil {
		return nil, fmt.Errorf("invalid transaction format: %w", err)
	}

	// 2. 验证交易参数（检查签名数量等）
	if err := s.validateTransaction(cmd.SignedTxBase64); err != nil {
		return nil, fmt.Errorf("transaction validation failed: %w", err)
	}
	if err := s.solanaGateway.ValidateFeePayer(cmd.SignedTxBase64, s.hotWallet.GetAddress()); err != nil {
		return nil, fmt.Errorf("fee payer validation failed: %w", err)
	}

	// 3. 创建补贴记录
	subsidyEntity := entity.NewGasSubsidyEntity(
		cmd.TxID,
		s.hotWallet.GetAddress(),
		s.solanaGateway.GetNetwork(),
	)

	if err := s.subsidyRepo.Save(ctx, subsidyEntity); err != nil {
		return nil, fmt.Errorf("failed to create subsidy record: %w", err)
	}

	// 4. Hot wallet 添加 fee payer 签名
	doubleSignedTxBase64, err := s.hotWallet.SignBase64Transaction(cmd.SignedTxBase64)
	if err != nil {
		subsidyEntity.MarkFailed()
		_ = s.subsidyRepo.Update(ctx, subsidyEntity)
		return nil, fmt.Errorf("failed to sign transaction with hot wallet: %w", err)
	}

	// 5. 发送交易到 Solana 网络
	txHash, err := s.solanaGateway.SendTransaction(ctx, doubleSignedTxBase64)
	if err != nil {
		subsidyEntity.MarkFailed()
		_ = s.subsidyRepo.Update(ctx, subsidyEntity)
		return nil, fmt.Errorf("failed to send transaction: %w", err)
	}

	// 6. 更新交易哈希
	subsidyEntity.SetTxHash(txHash)
	if err := s.subsidyRepo.Update(ctx, subsidyEntity); err != nil {
		return nil, fmt.Errorf("failed to update tx hash: %w", err)
	}

	// 7. 异步等待确认（在后台 goroutine 中）
	go s.waitForConfirmation(cmd.TxID, txHash)

	return &SubmitTxResult{
		TxHash: txHash,
		Status: "pending",
	}, nil
}

// validateTransactionFormat 验证交易格式
func (s *SubmitTxService) validateTransactionFormat(base64Tx string) error {
	return s.solanaGateway.ValidatePartiallySignedTransaction(base64Tx)
}

// validateTransaction 验证交易
func (s *SubmitTxService) validateTransaction(base64Tx string) error {
	// 检查是否包含至少一个签名（客户端签名）
	// 具体实现在 SolanaGateway 中
	return s.solanaGateway.ValidatePartiallySignedTransaction(base64Tx)
}

// waitForConfirmation 等待交易确认
func (s *SubmitTxService) waitForConfirmation(txID, txHash string) {
	ctx := context.Background()
	txStatus, err := s.solanaGateway.WaitForConfirmation(ctx, txHash, 30*time.Second)
	if err != nil {
		// 超时但交易可能已发送
		return
	}

	// 获取补贴记录并更新状态
	subsidyEntity, _ := s.subsidyRepo.FindByTxHash(ctx, txHash)
	if subsidyEntity != nil {
		if txStatus.IsConfirmed() {
			subsidyEntity.MarkCompleted()
		} else if txStatus.IsFailed() {
			subsidyEntity.MarkFailed()
		}
		_ = s.subsidyRepo.Update(ctx, subsidyEntity)
	}
}
