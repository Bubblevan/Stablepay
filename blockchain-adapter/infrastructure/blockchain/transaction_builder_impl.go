// Package blockchain 交易构建基础设施实现
package blockchain

import (
	"encoding/binary"
	"fmt"

	"github.com/stablepay/blockchain-adapter/domain/entity"
	"github.com/stablepay/blockchain-adapter/domain/gateway"

	"github.com/gagliardetto/solana-go"
)

// TransactionBuilderImpl builds unsigned Solana transactions.
type TransactionBuilderImpl struct {
	client  *solanaRPCClient
	network string
}

// solanaRPCClient is a thin RPC client wrapper.
type solanaRPCClient struct {
	// 实际项目中应该包�? *rpc.Client
}

// NewTransactionBuilder 创建交易构建�?
func NewTransactionBuilder(network string) gateway.TransactionBuilderGateway {
	return &TransactionBuilderImpl{
		network: network,
	}
}

// BuildSOLTransferTx 构建 SOL 转账交易
func (t *TransactionBuilderImpl) BuildSOLTransferTx(from, to string, amountLamports uint64, recentBlockHash string) (string, error) {
	fromPubKey, err := solana.PublicKeyFromBase58(from)
	if err != nil {
		return "", fmt.Errorf("invalid from address: %w", err)
	}

	toPubKey, err := solana.PublicKeyFromBase58(to)
	if err != nil {
		return "", fmt.Errorf("invalid to address: %w", err)
	}

	blockhash, err := solana.HashFromBase58(recentBlockHash)
	if err != nil {
		return "", fmt.Errorf("invalid blockhash: %w", err)
	}

	// 创建 System Program Transfer 指令
	// 指令数据: [2, amount(8 bytes)] - 2 �? Transfer 指令类型
	data := make([]byte, 12)
	data[0] = 2 // Transfer instruction type
	binary.LittleEndian.PutUint64(data[4:], amountLamports)
	
	accounts := solana.AccountMetaSlice{
		solana.NewAccountMeta(fromPubKey, true, true),  // from (signer, writable)
		solana.NewAccountMeta(toPubKey, false, true),   // to (writable)
	}
	
	transferIx := solana.NewInstruction(
		solana.SystemProgramID,
		accounts,
		data,
	)
	
	tx, err := solana.NewTransaction(
		[]solana.Instruction{transferIx},
		blockhash,
		solana.TransactionPayer(fromPubKey),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create transaction: %w", err)
	}

	return tx.ToBase64()
}

// BuildSOLTransferTxWithFeePayer 构建�? FeePayer �? SOL 转账
func (t *TransactionBuilderImpl) BuildSOLTransferTxWithFeePayer(from, to, feePayer string, amountLamports uint64, recentBlockHash string) (string, error) {
	fromPubKey, err := solana.PublicKeyFromBase58(from)
	if err != nil {
		return "", fmt.Errorf("invalid from address: %w", err)
	}

	toPubKey, err := solana.PublicKeyFromBase58(to)
	if err != nil {
		return "", fmt.Errorf("invalid to address: %w", err)
	}

	feePayerPubKey, err := solana.PublicKeyFromBase58(feePayer)
	if err != nil {
		return "", fmt.Errorf("invalid fee payer address: %w", err)
	}

	blockhash, err := solana.HashFromBase58(recentBlockHash)
	if err != nil {
		return "", fmt.Errorf("invalid blockhash: %w", err)
	}

	// 创建 System Program Transfer 指令
	// 指令数据: [2, amount(8 bytes)] - 2 �? Transfer 指令类型
	data := make([]byte, 12)
	data[0] = 2 // Transfer instruction type
	binary.LittleEndian.PutUint64(data[4:], amountLamports)
	
	accounts := solana.AccountMetaSlice{
		solana.NewAccountMeta(fromPubKey, true, true),  // from (signer, writable)
		solana.NewAccountMeta(toPubKey, false, true),   // to (writable)
	}
	
	transferIx := solana.NewInstruction(
		solana.SystemProgramID,
		accounts,
		data,
	)
	
	tx, err := solana.NewTransaction(
		[]solana.Instruction{transferIx},
		blockhash,
		solana.TransactionPayer(feePayerPubKey),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create transaction: %w", err)
	}

	return tx.ToBase64()
}

// BuildUnsignedTransaction 构建未签名的 SPL Token 转账交易
func (t *TransactionBuilderImpl) BuildUnsignedTransaction(req *gateway.BuildTransactionRequest) (string, error) {
	fromPubKey, err := solana.PublicKeyFromBase58(req.FromAddress)
	if err != nil {
		return "", fmt.Errorf("invalid from address: %w", err)
	}

	toPubKey, err := solana.PublicKeyFromBase58(req.ToAddress)
	if err != nil {
		return "", fmt.Errorf("invalid to address: %w", err)
	}

	feePayerPubKey, err := solana.PublicKeyFromBase58(req.FeePayerAddress)
	if err != nil {
		return "", fmt.Errorf("invalid fee payer address: %w", err)
	}

	blockhash, err := solana.HashFromBase58(req.RecentBlockhash)
	if err != nil {
		return "", fmt.Errorf("invalid blockhash: %w", err)
	}

	// 获取代币 Mint 地址
	mintAddress := gateway.GetTokenMintByCurrency(req.Currency, gateway.IsMainnet(t.network))
	if mintAddress == "" {
		return "", fmt.Errorf("unsupported currency: %s", req.Currency)
	}

	mintPubKey, err := solana.PublicKeyFromBase58(mintAddress)
	if err != nil {
		return "", fmt.Errorf("invalid mint address: %w", err)
	}

	// 计算 ATA
	fromATA, _, err := solana.FindAssociatedTokenAddress(fromPubKey, mintPubKey)
	if err != nil {
		return "", fmt.Errorf("failed to find from ATA: %w", err)
	}

	toATA, _, err := solana.FindAssociatedTokenAddress(toPubKey, mintPubKey)
	if err != nil {
		return "", fmt.Errorf("failed to find to ATA: %w", err)
	}

	// SPL Token Program ID
	tokenProgramID := solana.TokenProgramID

	// 构建 SPL Token Transfer 指令数据
	// 指令格式: [3, amount(8 bytes)]  - 3 �? Transfer 指令类型
	data := make([]byte, 9)
	data[0] = 3 // Transfer instruction type
	binary.LittleEndian.PutUint64(data[1:], uint64(req.AmountMinor))

	// 创建指令账户列表
	accounts := solana.AccountMetaSlice{
		solana.NewAccountMeta(fromATA, false, true),  // source (writable, signer)
		solana.NewAccountMeta(toATA, false, true),    // destination (writable)
		solana.NewAccountMeta(fromPubKey, true, false), // owner (signer)
	}

	// 创建 SPL Token 转账指令
	transferIx := solana.NewInstruction(
		tokenProgramID,
		accounts,
		data,
	)

	// 创建交易
	tx, err := solana.NewTransaction(
		[]solana.Instruction{transferIx},
		blockhash,
		solana.TransactionPayer(feePayerPubKey),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create transaction: %w", err)
	}

	return tx.ToBase64()
}

// DeserializeBase64Tx 反序列化 Base64 交易
func (t *TransactionBuilderImpl) DeserializeBase64Tx(base64Tx string) (*entity.TransactionEntity, error) {
	tx := &solana.Transaction{}
	if err := tx.UnmarshalBase64(base64Tx); err != nil {
		return nil, fmt.Errorf("failed to unmarshal transaction: %w", err)
	}

	// 转换为领域实�?
	return &entity.TransactionEntity{
		// 填充相关字段
	}, nil
}

// SerializeToBase64 序列化交易为 Base64
func (t *TransactionBuilderImpl) SerializeToBase64(tx *entity.TransactionEntity) (string, error) {
	// 实际项目中需要将 TransactionEntity 转换�? solana.Transaction
	// 这里简化处�?
	return "", fmt.Errorf("SerializeToBase64 not implemented")
}

// SignTransaction 签名交易
func (t *TransactionBuilderImpl) SignTransaction(tx *entity.TransactionEntity, privateKey string) error {
	return fmt.Errorf("SignTransaction not implemented")
}

// ValidateTransfer 验证转账参数
func (t *TransactionBuilderImpl) ValidateTransfer(from, to string, amountLamports, balance uint64) error {
	if from == "" || to == "" {
		return fmt.Errorf("from and to addresses are required")
	}
	if amountLamports == 0 {
		return fmt.Errorf("amount must be greater than 0")
	}
	if balance < amountLamports {
		return fmt.Errorf("insufficient balance")
	}
	return nil
}
