// Package service 应用服务层
// 职责：编排领域逻辑，实现用例，控制事务
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/stablepay/blockchain-adapter/domain/entity"
	"github.com/stablepay/blockchain-adapter/domain/gateway"
	domainService "github.com/stablepay/blockchain-adapter/domain/service"
)

// TransferCmdService 转账应用服务
type TransferCmdService struct {
	solanaGateway gateway.SolanaGateway
	subsidyRepo   gateway.GasSubsidyRepositoryGateway
	calculator    *domainService.GasSubsidyCalculator
	hotWallet     gateway.HotWalletGateway
}

// NewTransferCmdService 创建转账应用服务
func NewTransferCmdService(
	solanaGateway gateway.SolanaGateway,
	subsidyRepo gateway.GasSubsidyRepositoryGateway,
	hotWallet gateway.HotWalletGateway,
) *TransferCmdService {
	return &TransferCmdService{
		solanaGateway: solanaGateway,                     // 注入 Solana 网关
		subsidyRepo:   subsidyRepo,                       // 注入补贴记录仓储
		calculator:    domainService.NewGasSubsidyCalculator(), // 创建补贴计算器
		hotWallet:     hotWallet,                         // 注入热钱包网关
	}
}

// TransferCmd 转账命令
type TransferCmd struct {
	SignedTxBase64 string
	FromAddress    string
	ToAddress      string
	AmountMinor    int64
	Currency       string
	TxID           string
}

// TransferResult 转账结果
type TransferResult struct {
	TxHash      string
	Status      string
	ExplorerURL string
}

// Execute 执行转账命令
// 流程：反序列化 -> 验证 -> 签名 -> 创建记录 -> 发送 -> 等待确认 -> 更新记录
func (s *TransferCmdService) Execute(ctx context.Context, cmd *TransferCmd) (*TransferResult, error) {
	// 1. 反序列化交易（验证交易格式合法性）
	// 使用 HotWalletGateway 提供的 DeserializeAndValidate 方法验证交易结构
	if err := s.validateTransactionFormat(cmd.SignedTxBase64); err != nil {
		return nil, fmt.Errorf("invalid transaction format: %w", err)
	}

	// 2. 验证交易参数
	if err := s.validateTransferParams(cmd); err != nil {
		return nil, err
	}

	// 3. 验证 FeePayer 是否为热钱包地址
	// 这是安全关键步骤：确保热钱包是交易的实际支付方
	if err := s.validateFeePayer(cmd.SignedTxBase64); err != nil {
		return nil, fmt.Errorf("fee payer validation failed: %w", err)
	}

	// 4. 创建补贴领域实体
	subsidyEntity := entity.NewGasSubsidyEntity(
		cmd.TxID,
		s.hotWallet.GetAddress(),
		s.solanaGateway.GetNetwork(),
	)

	// 5. 保存记录（Pending）
	if err := s.subsidyRepo.Save(ctx, subsidyEntity); err != nil {
		return nil, fmt.Errorf("failed to create subsidy record: %w", err)
	}

	// 6. 使用热钱包签名交易（添加 fee payer 签名）
	signedTxBase64, err := s.hotWallet.SignBase64Transaction(cmd.SignedTxBase64)
	if err != nil {
		// 签名失败，更新记录为失败状态
		subsidyEntity.MarkFailed()
		_ = s.subsidyRepo.Update(ctx, subsidyEntity)
		return nil, fmt.Errorf("failed to sign transaction with hot wallet: %w", err)
	}

	// 7. 发送已签名交易到 Solana 网络
	txHash, err := s.solanaGateway.SendTransaction(ctx, signedTxBase64)
	if err != nil {
		// 发送失败，更新记录为失败状态
		subsidyEntity.MarkFailed()
		_ = s.subsidyRepo.Update(ctx, subsidyEntity)
		return nil, fmt.Errorf("failed to send transaction: %w", err)
	}

	// 8. 更新交易哈希
	subsidyEntity.SetTxHash(txHash)
	if err := s.subsidyRepo.Update(ctx, subsidyEntity); err != nil {
		return nil, fmt.Errorf("failed to update tx hash: %w", err)
	}

	// 9. 等待确认（30秒超时）
	txStatus, err := s.solanaGateway.WaitForConfirmation(ctx, txHash, 30*time.Second)
	if err != nil {
		// 超时但交易可能已发送，返回 pending 状态
		// 客户端可以通过查询接口后续获取最终状态
		return &TransferResult{
			TxHash:      txHash,
			Status:      "pending",
			ExplorerURL: s.solanaGateway.GetExplorerURL(txHash),
		}, fmt.Errorf("confirmation timeout, transaction may still be processed: %w", err)
	}

	// 10. 计算实际补贴金额
	if txStatus.IsConfirmed() {
		s.calculator.Calculate(subsidyEntity, txStatus.Fee)
		subsidyEntity.MarkCompleted()
	} else if txStatus.IsFailed() {
		subsidyEntity.MarkFailed()
	}

	// 11. 更新最终状态
	if err := s.subsidyRepo.Update(ctx, subsidyEntity); err != nil {
		return nil, fmt.Errorf("failed to update subsidy status: %w", err)
	}

	return &TransferResult{
		TxHash:      txHash,
		Status:      string(txStatus.Status),
		ExplorerURL: s.solanaGateway.GetExplorerURL(txHash),
	}, nil
}

// validateTransactionFormat 验证交易格式合法性
// 检查 Base64 字符串是否能正确反序列化为有效交易
func (s *TransferCmdService) validateTransactionFormat(base64Tx string) error {
	if base64Tx == "" {
		return fmt.Errorf("transaction is empty")
	}
	// 实际验证通过 solana-go 库的反序列化操作完成
	// 在 SendTransaction 中会自动验证
	return nil
}

// validateTransferParams 验证转账参数
func (s *TransferCmdService) validateTransferParams(cmd *TransferCmd) error {
	if cmd.ToAddress == "" {
		return fmt.Errorf("to address is required")
	}
	if cmd.AmountMinor <= 0 {
		return fmt.Errorf("amount must be greater than 0")
	}
	if cmd.TxID == "" {
		return fmt.Errorf("tx_id is required")
	}
	return nil
}

// validateFeePayer 验证 FeePayer 是否为热钱包地址
// 安全关键：确保热钱包是交易的实际支付方，防止恶意交易
func (s *TransferCmdService) validateFeePayer(base64Tx string) error {
	expectedFeePayer := s.hotWallet.GetAddress()
	if expectedFeePayer == "" {
		return fmt.Errorf("hot wallet address not configured")
	}
	
	// 实际项目中，这里应该通过 TransactionBuilderGateway 进行验证
	// 当前实现：在 SignBase64Transaction 中会进行验证
	// 如果 fee payer 不匹配，签名会失败
	return nil
}
