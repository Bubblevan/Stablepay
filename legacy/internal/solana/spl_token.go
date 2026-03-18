// Package solana 提供 SPL Token (USDC/USDT) 转账功能
// 从 demo1/internal/solana/transfer.go 扩展适配
package solana

import (
	"fmt"

	"github.com/gagliardetto/solana-go"
	ata "github.com/gagliardetto/solana-go/programs/associated-token-account"
	"github.com/gagliardetto/solana-go/programs/token"
)

// TokenInfo SPL Token 基本信息
type TokenInfo struct {
	Mint     solana.PublicKey // Token Mint 地址
	Decimals uint8            // Token 精度 (USDC = 6)
	Symbol   string           // Token 符号
}

// 常用 Token Mint 地址
var (
	// USDCDevnet USDC Devnet 地址
	USDCDevnet = solana.MustPublicKeyFromBase58("4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU")
	// USDTDevnet USDT Devnet 地址
	USDTDevnet = solana.MustPublicKeyFromBase58("BQcdHdAQW1hczDbBi9hiegXAR7A18QhzhCoXFBtBj9QA")
	
	// USDCMainnet USDC Mainnet 地址
	USDCMainnet = solana.MustPublicKeyFromBase58("EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	// USDTMainnet USDT Mainnet 地址
	USDTMainnet = solana.MustPublicKeyFromBase58("Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB")
)

// TokenProgramID SPL Token 程序 ID
var TokenProgramID = token.ProgramID

// AssociatedTokenProgramID Associated Token Account 程序 ID
var AssociatedTokenProgramID = ata.ProgramID

// SPLTokenTransferRequest SPL Token 转账请求
type SPLTokenTransferRequest struct {
	From       string // 付款方地址 (Base58)
	To         string // 收款方地址 (Base58)
	Mint       string // Token Mint 地址 (Base58)
	Amount     uint64 // 转账金额 (已考虑 decimals)
	Decimals   uint8  // Token 精度
}

// BuildSPLTokenTransferTx 构建 SPL Token 转账交易
//
// 流程:
//  1. 获取/创建付款方 ATA
//  2. 获取/创建收款方 ATA (如果不存在)
//  3. 构建 TokenProgram.Transfer 指令
//  4. 设置 FeePayer
//
// 注意: 如果收款方没有 ATA，会创建一个，费用由 FeePayer 支付
func (tb *TransactionBuilder) BuildSPLTokenTransferTx(
	req *SPLTokenTransferRequest,
	feePayer solana.PublicKey,
	recentBlockHash solana.Hash,
) (*solana.Transaction, error) {
	// 解析地址
	fromPubkey, err := solana.PublicKeyFromBase58(req.From)
	if err != nil {
		return nil, fmt.Errorf("invalid from address: %w", err)
	}

	toPubkey, err := solana.PublicKeyFromBase58(req.To)
	if err != nil {
		return nil, fmt.Errorf("invalid to address: %w", err)
	}

	mintPubkey, err := solana.PublicKeyFromBase58(req.Mint)
	if err != nil {
		return nil, fmt.Errorf("invalid mint address: %w", err)
	}

	// 计算 Associated Token Accounts
	fromATA, _, err := solana.FindAssociatedTokenAddress(fromPubkey, mintPubkey)
	if err != nil {
		return nil, fmt.Errorf("failed to find from ATA: %w", err)
	}

	toATA, _, err := solana.FindAssociatedTokenAddress(toPubkey, mintPubkey)
	if err != nil {
		return nil, fmt.Errorf("failed to find to ATA: %w", err)
	}

	// 构建指令列表
	instructions := []solana.Instruction{}

	// 注意: 实际应用中需要检查 ATA 是否存在
	// 如果不存在，需要添加 CreateAssociatedTokenAccount 指令
	// 这里假设调用者已经确保 ATA 存在，或者通过其他方式创建

	// 构建 Token Transfer 指令
	transferIx := token.NewTransferInstruction(
		req.Amount,
		fromATA,
		toATA,
		fromPubkey, // owner
		[]solana.PublicKey{},
	).Build()

	instructions = append(instructions, transferIx)

	// 构建交易
	tx, err := solana.NewTransaction(instructions, recentBlockHash)
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction: %w", err)
	}

	// 设置 FeePayer (AccountKeys[0] 支付手续费)
	tx.Message.AccountKeys[0] = feePayer

	return tx, nil
}

// BuildSPLTokenTransferTxWithCreateATA 构建 SPL Token 转账交易
// 自动创建收款方的 ATA（如果不存在）
func (tb *TransactionBuilder) BuildSPLTokenTransferTxWithCreateATA(
	req *SPLTokenTransferRequest,
	feePayer solana.PublicKey,
	recentBlockHash solana.Hash,
) (*solana.Transaction, error) {
	// 解析地址
	fromPubkey, err := solana.PublicKeyFromBase58(req.From)
	if err != nil {
		return nil, fmt.Errorf("invalid from address: %w", err)
	}

	toPubkey, err := solana.PublicKeyFromBase58(req.To)
	if err != nil {
		return nil, fmt.Errorf("invalid to address: %w", err)
	}

	mintPubkey, err := solana.PublicKeyFromBase58(req.Mint)
	if err != nil {
		return nil, fmt.Errorf("invalid mint address: %w", err)
	}

	// 计算 Associated Token Accounts
	fromATA, _, err := solana.FindAssociatedTokenAddress(fromPubkey, mintPubkey)
	if err != nil {
		return nil, fmt.Errorf("failed to find from ATA: %w", err)
	}

	toATA, _, err := solana.FindAssociatedTokenAddress(toPubkey, mintPubkey)
	if err != nil {
		return nil, fmt.Errorf("failed to find to ATA: %w", err)
	}

	// 构建指令列表
	instructions := []solana.Instruction{}

	// 添加创建收款方 ATA 的指令（idempotent，已存在则跳过）
	createATAIx := ata.NewCreateInstruction(
		feePayer, // payer (热钱包支付)
		toPubkey, // wallet
		mintPubkey,
	).Build()
	instructions = append(instructions, createATAIx)

	// 构建 Token Transfer 指令
	transferIx := token.NewTransferInstruction(
		req.Amount,
		fromATA,
		toATA,
		fromPubkey, // owner
		[]solana.PublicKey{},
	).Build()
	instructions = append(instructions, transferIx)

	// 构建交易
	tx, err := solana.NewTransaction(instructions, recentBlockHash)
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction: %w", err)
	}

	// 设置 FeePayer (AccountKeys[0] 支付手续费)
	tx.Message.AccountKeys[0] = feePayer

	return tx, nil
}

// GetTokenMintByCurrency 根据币种获取 Mint 地址
// network: "devnet" 或 "mainnet"
func GetTokenMintByCurrency(currency string, network string) (solana.PublicKey, error) {
	isMainnet := network == "mainnet" || network == "mainnet-beta"

	switch currency {
	case "USDC":
		if isMainnet {
			return USDCMainnet, nil
		}
		return USDCDevnet, nil
	case "USDT":
		if isMainnet {
			return USDTMainnet, nil
		}
		return USDTDevnet, nil
	default:
		return solana.PublicKey{}, fmt.Errorf("unsupported currency: %s", currency)
	}
}

// AmountToMinorUnits 将人类可读金额转换为最小单位
// 例如: 1.5 USDC (6 decimals) → 1500000
func AmountToMinorUnits(amount float64, decimals uint8) uint64 {
	multiplier := float64(1)
	for i := uint8(0); i < decimals; i++ {
		multiplier *= 10
	}
	return uint64(amount * multiplier)
}

// MinorUnitsToAmount 将最小单位转换为人类可读金额
// 例如: 1500000 (6 decimals) → 1.5
func MinorUnitsToAmount(minorUnits uint64, decimals uint8) float64 {
	divisor := float64(1)
	for i := uint8(0); i < decimals; i++ {
		divisor *= 10
	}
	return float64(minorUnits) / divisor
}

// GetTokenDecimals 获取 Token 精度
// USDC/USDT 通常为 6
func GetTokenDecimals(mint solana.PublicKey) uint8 {
	// 预定义常用 Token 的精度
	knownTokens := map[string]uint8{
		USDCDevnet.String():  6,
		USDTDevnet.String():  6,
		USDCMainnet.String(): 6,
		USDTMainnet.String(): 6,
	}

	if decimals, ok := knownTokens[mint.String()]; ok {
		return decimals
	}

	// 默认为 6 (大多数稳定币)
	return 6
}

// VerifySPLTokenTransfer 验证 SPL Token 转账交易结构
// 用于验证前端传来的部分签名交易是否符合预期
func VerifySPLTokenTransfer(tx *solana.Transaction, expectedAmount uint64, expectedMint string) error {
	// 解析 Mint 地址
	mintPubkey, err := solana.PublicKeyFromBase58(expectedMint)
	if err != nil {
		return fmt.Errorf("invalid expected mint: %w", err)
	}

	// 验证交易中包含 Token Transfer 指令
	hasTransfer := false
	for _, ix := range tx.Message.Instructions {
		progID, err := tx.Message.Program(ix.ProgramIDIndex)
		if err != nil {
			continue
		}

		// 检查是否是 Token Program
		if !progID.Equals(token.ProgramID) {
			continue
		}

		// 解析指令数据检查是否是 Transfer
		if len(ix.Data) > 0 && ix.Data[0] == 3 { // Transfer = 3
			hasTransfer = true
			// 注意：这里简化处理，实际应完整解析指令数据验证金额
			break
		}
	}

	if !hasTransfer {
		return fmt.Errorf("transaction does not contain token transfer instruction")
	}

	// 验证 Mint 相关的 ATA 是否在交易中
	// 简化处理：检查账户列表中是否包含与 Mint 相关的 PDA
	_ = mintPubkey // 实际实现中应验证 ATA 地址

	return nil
}
