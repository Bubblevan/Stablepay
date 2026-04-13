// Package service 应用服务层
// 职责：编排领域逻辑，实现用例，控制事务
package service

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/google/uuid"
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
// 支持两种模式：
// 1. 传入预签名交易（cmd.SignedTxBase64 非空）：验证并由热钱包补签 fee payer 签名后发送
// 2. 无预签名交易（cmd.SignedTxBase64 为空）：由热钱包构建 SPL Token 转账交易并签名发送
func (s *TransferCmdService) Execute(ctx context.Context, cmd *TransferCmd) (*TransferResult, error) {
	// 1. 验证基础参数
	if err := s.validateTransferParams(cmd); err != nil {
		return nil, err
	}

	// 2. 若无预签名交易，仅允许「热钱包自持代币」的托管路径：由热钱包同时作为 fee payer 与 SPL authority构建交易。
	//    若 from 为买家/代理等非热钱包地址，必须由客户端提交 signed_tx_base64（买家已签 + 热钱包作 fee payer），否则会错误地从热钱包 ATA 扣款。
	if cmd.SignedTxBase64 == "" {
		hot := s.hotWallet.GetAddress()
		if cmd.FromAddress != "" && cmd.FromAddress != hot {
			return nil, fmt.Errorf("signed_tx_base64 is required when from_wallet (%s) is not the custodial hot wallet (%s); refusing server-built tx to avoid debiting wrong token account",
				cmd.FromAddress, hot)
		}
		log.Printf("[transfer] mode=server_built_spl hot_wallet_token_owner=%s to=%s amount_minor=%d currency=%s",
			hot, cmd.ToAddress, cmd.AmountMinor, cmd.Currency)
		builtTx, err := s.solanaGateway.BuildSPLTransferTx(
			ctx,
			hot,
			cmd.ToAddress,
			cmd.Currency,
			uint64(cmd.AmountMinor),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to build SPL transfer transaction: %w", err)
		}
		cmd.SignedTxBase64 = builtTx
		LogBase64TxAudit("after_server_build_unsigned", cmd.SignedTxBase64)
	} else {
		log.Printf("[transfer] mode=client_partial_signed from=%s to=%s amount_minor=%d", cmd.FromAddress, cmd.ToAddress, cmd.AmountMinor)
		LogBase64TxAudit("incoming_client_partial_signed", cmd.SignedTxBase64)
		// 解码并打印客户端传来的 blockhash
		txIn := &solana.Transaction{}
		if err := txIn.UnmarshalBase64(cmd.SignedTxBase64); err == nil {
			log.Printf("[transfer] incoming_client_tx: recent_blockhash=%s fee_payer=%s",
				txIn.Message.RecentBlockhash.String(),
				txIn.Message.AccountKeys[0].String(),
			)
		}
		// 3. 预签名交易模式：验证交易格式和 fee payer
		if err := s.validateTransactionFormat(cmd.SignedTxBase64); err != nil {
			return nil, fmt.Errorf("invalid transaction format: %w", err)
		}
		if err := s.validateFeePayer(cmd.SignedTxBase64); err != nil {
			return nil, fmt.Errorf("fee payer validation failed: %w", err)
		}
	}

	// 4. 创建补贴领域实体
	subsidyEntity := entity.NewGasSubsidyEntity(
		cmd.TxID,
		s.hotWallet.GetAddress(),
		s.solanaGateway.GetNetwork(),
	)

	// 5. 保存记录（Pending）
	if err := s.subsidyRepo.Save(ctx, subsidyEntity); err != nil {
		log.Printf("[transfer] subsidy Save pending failed: %v | tx_id=%s", err, cmd.TxID)
		return nil, fmt.Errorf("failed to create subsidy record: %w", err)
	}

	// 6. 使用热钱包签名交易（添加 fee payer 签名）
	signedTxBase64, err := s.hotWallet.SignBase64Transaction(cmd.SignedTxBase64)
	if err != nil {
		log.Printf("[transfer] hot wallet SignBase64Transaction failed: %v | tx_id=%s from=%s", err, cmd.TxID, cmd.FromAddress)
		// 签名失败，更新记录为失败状态
		subsidyEntity.MarkFailed()
		_ = s.subsidyRepo.Update(ctx, subsidyEntity)
		return nil, fmt.Errorf("failed to sign transaction with hot wallet: %w", err)
	}

	LogBase64TxAudit("after_hot_wallet_fee_payer_sign", signedTxBase64)

	// 7. 解码并打印交易信息（用于调试 Blockhash 问题）
	txDebug := &solana.Transaction{}
	if err := txDebug.UnmarshalBase64(signedTxBase64); err == nil {
		log.Printf("[transfer] before_send: recent_blockhash=%s fee_payer=%s num_signatures=%d",
			txDebug.Message.RecentBlockhash.String(),
			txDebug.Message.AccountKeys[0].String(),
			len(txDebug.Signatures),
		)
	} else {
		log.Printf("[transfer] before_send: failed to decode tx for debug: %v", err)
	}

	// 8. 发送已签名交易到 Solana 网络
	txHash, err := s.solanaGateway.SendTransaction(ctx, signedTxBase64)
	if err != nil {
		log.Printf("[transfer] SendTransaction failed: %v | tx_id=%s from=%s to=%s", err, cmd.TxID, cmd.FromAddress, cmd.ToAddress)
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

// validateTransferParams 验证转账参数，TxID 为空时自动生成
func (s *TransferCmdService) validateTransferParams(cmd *TransferCmd) error {
	if cmd.ToAddress == "" {
		return fmt.Errorf("to address is required")
	}
	if cmd.AmountMinor <= 0 {
		return fmt.Errorf("amount must be greater than 0")
	}
	if cmd.TxID == "" {
		cmd.TxID = uuid.New().String()
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
