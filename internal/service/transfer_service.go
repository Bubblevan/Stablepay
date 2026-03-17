// Package service 提供业务逻辑服务
// TransferService 处理稳定币转账和 Gas 费补贴
package service

import (
	"context"
	"fmt"
	"time"

	"stablepay.blockchain_adapter/data-access-layer/db"
	internalsolana "stablepay.blockchain_adapter/internal/solana"
	"stablepay.blockchain_adapter/kitex_gen/stablepay/blockchain_adapter"
	"stablepay.blockchain_adapter/kitex_gen/stablepay/common"

	"github.com/gagliardetto/solana-go"
)

// TransferService 转账服务
type TransferService struct {
	client     *internalsolana.Client
	hotWallet  *internalsolana.HotWallet
	subsidyDAL *db.GasSubsidyDAL
}

// NewTransferService 创建转账服务
func NewTransferService(client *internalsolana.Client, hotWallet *internalsolana.HotWallet, subsidyDAL *db.GasSubsidyDAL) *TransferService {
	return &TransferService{
		client:     client,
		hotWallet:  hotWallet,
		subsidyDAL: subsidyDAL,
	}
}

// TransferRequest 内部转账请求
type TransferRequest struct {
	SignedTxBase64 string           // 客户端部分签名的交易 (Base64)
	FromAddress    string           // 付款方地址
	ToAddress      string           // 收款方地址
	AmountMinor    int64            // 转账金额 (最小单位)
	Currency       common.Currency  // 币种 (USDC/USDT)
	TxID           string           // 业务交易ID
}

// TransferResult 转账结果
type TransferResult struct {
	TxHash      string
	Status      blockchain_adapter.TxStatus
	ExplorerURL string
}

// TransferStableCoin 执行稳定币转账并代付 Gas
//
// 流程:
//  1. 反序列化 Base64 交易
//  2. 验证交易参数
//  3. 热钱包二次签名 (FeePayer)
//  4. 创建补贴记录 (Pending)
//  5. 发送交易到 Solana 网络
//  6. 等待确认
//  7. 更新补贴记录 (Completed/Failed)
//  8. 返回结果
func (s *TransferService) TransferStableCoin(ctx context.Context, req *TransferRequest) (*TransferResult, error) {
	// 1. 反序列化交易
	tx, err := internalsolana.DeserializeBase64Tx(req.SignedTxBase64)
	if err != nil {
		return nil, fmt.Errorf("invalid transaction: %w", err)
	}

	// 2. 验证交易
	if err := s.validateTransaction(tx, req); err != nil {
		return nil, fmt.Errorf("transaction validation failed: %w", err)
	}

	// 3. 热钱包二次签名
	if err := s.hotWallet.SignTransaction(tx); err != nil {
		return nil, fmt.Errorf("failed to sign with hot wallet: %w", err)
	}

	// 4. 创建补贴记录
	network := s.client.GetNetwork()
	subsidyRecord, err := s.subsidyDAL.CreateSubsidyRecord(
		req.TxID,
		"", // 交易哈希在发送后获得
		s.hotWallet.Address,
		network,
		internalsolana.GetTransactionFeeEstimate(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create subsidy record: %w", err)
	}

	// 5. 发送交易
	txHash, err := s.client.SendTransaction(tx)
	if err != nil {
		// 更新记录为失败
		s.subsidyDAL.UpdateStatus(subsidyRecord.ID, db.SubsidyFailed, 0)
		return nil, fmt.Errorf("failed to send transaction: %w", err)
	}

	// 更新记录的交易哈希
	s.subsidyDAL.UpdateSubsidyStatusByTxHash("", db.SubsidyPending, 0) // 占位，实际需要更新

	// 6. 等待确认 (异步或同步)
	result, err := s.client.WaitForConfirmation(txHash, 30*time.Second)
	if err != nil {
		// 超时但交易可能已发送，返回 pending 状态
		return &TransferResult{
			TxHash:      txHash,
			Status:      blockchain_adapter.TxStatus_PENDING,
			ExplorerURL: s.client.GetExplorerURL(txHash),
		}, fmt.Errorf("transaction sent but confirmation timeout: %w", err)
	}

	// 7. 解析结果并更新记录
	actualFee := uint64(0)
	status := blockchain_adapter.TxStatus_CONFIRMED

	if result.Meta != nil {
		actualFee = uint64(result.Meta.Fee)
		if result.Meta.Err != nil {
			status = blockchain_adapter.TxStatus_FAILED
		}
	}

	// 更新补贴记录
	if err := s.subsidyDAL.FinalizeSubsidy(subsidyRecord.ID, actualFee, 1.0); err != nil {
		// 记录日志但不返回错误，因为交易已成功
		// TODO: 添加到补偿队列
	}

	// 如果交易失败，更新状态
	if status == blockchain_adapter.TxStatus_FAILED {
		_ = s.subsidyDAL.UpdateStatus(subsidyRecord.ID, db.SubsidyFailed, actualFee)
	}

	return &TransferResult{
		TxHash:      txHash,
		Status:      status,
		ExplorerURL: s.client.GetExplorerURL(txHash),
	}, nil
}

// validateTransaction 验证交易参数
func (s *TransferService) validateTransaction(tx *solana.Transaction, req *TransferRequest) error {
	// 验证 FeePayer 是热钱包
	if len(tx.Message.AccountKeys) == 0 {
		return fmt.Errorf("transaction has no accounts")
	}

	feePayer := tx.Message.AccountKeys[0]
	hotWalletPubkey, err := s.hotWallet.GetAddressPubkey()
	if err != nil {
		return fmt.Errorf("failed to get hot wallet pubkey: %w", err)
	}

	if !feePayer.Equals(hotWalletPubkey) {
		return fmt.Errorf("invalid fee payer: expected %s, got %s",
			hotWalletPubkey.String(), feePayer.String())
	}

	// 验证交易包含指令
	if len(tx.Message.Instructions) == 0 {
		return fmt.Errorf("transaction has no instructions")
	}

	// 验证是否已经部分签名
	if len(tx.Signatures) == 0 {
		return fmt.Errorf("transaction is not signed by sender")
	}

	return nil
}

// QuickTransferStableCoin 快速转账（后端构建交易模式）
// 用于测试或特定场景，非主流程
func (s *TransferService) QuickTransferStableCoin(
	ctx context.Context,
	fromKeypair *internalsolana.HotWallet, // 使用 HotWallet 结构兼容
	toAddress string,
	amountMinor uint64,
	mintAddress string,
) (*TransferResult, error) {
	// 构建转账请求
	req := &internalsolana.SPLTokenTransferRequest{
		From:     fromKeypair.Address,
		To:       toAddress,
		Mint:     mintAddress,
		Amount:   amountMinor,
		Decimals: 6, // USDC/USDT
	}

	// 获取 blockhash
	blockhash, err := s.client.GetRecentBlockhash()
	if err != nil {
		return nil, fmt.Errorf("failed to get blockhash: %w", err)
	}

	// 构建交易
	builder := internalsolana.NewTransactionBuilder()
	hotWalletPubkey, _ := s.hotWallet.GetAddressPubkey()

	tx, err := builder.BuildSPLTokenTransferTxWithCreateATA(req, hotWalletPubkey, blockhash)
	if err != nil {
		return nil, fmt.Errorf("failed to build transaction: %w", err)
	}

	// 签名 (fromKeypair 和 hotWallet)
	fromPrivKey, _ := fromKeypair.GetPrivateKey()
	if err := internalsolana.SignTransactionWithKey(tx, fromPrivKey); err != nil {
		return nil, fmt.Errorf("failed to sign with from key: %w", err)
	}

	if err := s.hotWallet.SignTransaction(tx); err != nil {
		return nil, fmt.Errorf("failed to sign with hot wallet: %w", err)
	}

	// 发送交易
	txHash, err := s.client.SendTransaction(tx)
	if err != nil {
		return nil, fmt.Errorf("failed to send transaction: %w", err)
	}

	// 等待确认
	result, err := s.client.WaitForConfirmation(txHash, 30*time.Second)
	if err != nil {
		return &TransferResult{
			TxHash:      txHash,
			Status:      blockchain_adapter.TxStatus_PENDING,
			ExplorerURL: s.client.GetExplorerURL(txHash),
		}, nil
	}

	status := blockchain_adapter.TxStatus_CONFIRMED
	if result.Meta != nil && result.Meta.Err != nil {
		status = blockchain_adapter.TxStatus_FAILED
	}

	return &TransferResult{
		TxHash:      txHash,
		Status:      status,
		ExplorerURL: s.client.GetExplorerURL(txHash),
	}, nil
}

// EstimateTransferFee 估算转账所需费用
// 包括交易手续费和可能的 ATA 创建费用
func (s *TransferService) EstimateTransferFee(needCreateATA bool) uint64 {
	baseFee := internalsolana.GetTransactionFeeEstimate() // 5000 lamports

	if needCreateATA {
		// ATA 创建费用 = 租金 (约 0.00203928 SOL = 2,039,280 lamports)
		// 实际费用取决于账户大小，这里使用估算值
		ataCreationFee := uint64(2_039_280)
		return baseFee + ataCreationFee
	}

	return baseFee
}

// ValidateTransferRequest 验证转账请求参数
func (s *TransferService) ValidateTransferRequest(req *TransferRequest) error {
	if req.SignedTxBase64 == "" {
		return fmt.Errorf("signed transaction is required")
	}

	if req.FromAddress == "" {
		return fmt.Errorf("from address is required")
	}

	if req.ToAddress == "" {
		return fmt.Errorf("to address is required")
	}

	if req.AmountMinor <= 0 {
		return fmt.Errorf("amount must be greater than 0")
	}

	if req.TxID == "" {
		return fmt.Errorf("transaction ID is required")
	}

	return nil
}
