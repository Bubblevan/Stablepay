// Package service 提供余额查询服务
package service

import (
	"context"
	"fmt"

	"github.com/gagliardetto/solana-go"

	internalsolana "stablepay.blockchain_adapter/internal/solana"
	"stablepay.blockchain_adapter/kitex_gen/stablepay/common"
)

// BalanceService 余额查询服务
type BalanceService struct {
	client *internalsolana.Client
}

// NewBalanceService 创建余额服务
func NewBalanceService(client *internalsolana.Client) *BalanceService {
	return &BalanceService{
		client: client,
	}
}

// BalanceInfo 余额信息
type BalanceInfo struct {
	Address    string         // 钱包地址
	Balance    uint64         // 余额 (最小单位)
	Decimals   uint8          // 精度
	Currency   common.Currency // 币种
}

// GetBalance 查询钱包余额
//
// 支持:
// - USDC/USDT (SPL Token): 查询 Associated Token Account 余额
// - SOL: 查询原生 SOL 余额
func (s *BalanceService) GetBalance(ctx context.Context, walletAddress string, currency common.Currency) (*BalanceInfo, error) {
	// 获取 Token Mint 地址
	network := s.client.GetNetwork()
	mintAddress, err := s.getTokenMint(currency, network)
	if err != nil {
		return nil, err
	}

	// 查询 Token 余额
	balance, err := s.client.GetTokenBalance(walletAddress, mintAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to get token balance: %w", err)
	}

	return &BalanceInfo{
		Address:    walletAddress,
		Balance:    balance,
		Decimals:   s.getTokenDecimals(currency),
		Currency:   currency,
	}, nil
}

// GetSOLBalance 查询 SOL 余额
// 注意: SOL 是原生代币，不是 SPL Token
func (s *BalanceService) GetSOLBalance(ctx context.Context, walletAddress string) (*BalanceInfo, error) {
	balance, err := s.client.GetBalance(walletAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to get SOL balance: %w", err)
	}

	return &BalanceInfo{
		Address:    walletAddress,
		Balance:    balance,
		Decimals:   9, // SOL 有 9 位小数
		Currency:   common.Currency_USDC, // 标识为 SOL，但使用 Currency 枚举需转换
	}, nil
}

// GetAllBalances 查询钱包的所有余额 (SOL + USDC + USDT)
func (s *BalanceService) GetAllBalances(ctx context.Context, walletAddress string) (map[common.Currency]*BalanceInfo, error) {
	balances := make(map[common.Currency]*BalanceInfo)

	// 查询 USDC
	if usdcBalance, err := s.GetBalance(ctx, walletAddress, common.Currency_USDC); err == nil {
		balances[common.Currency_USDC] = usdcBalance
	}

	// 查询 USDT
	if usdtBalance, err := s.GetBalance(ctx, walletAddress, common.Currency_USDT); err == nil {
		balances[common.Currency_USDT] = usdtBalance
	}

	// 查询 SOL
	if solBalance, err := s.GetSOLBalance(ctx, walletAddress); err == nil {
		// SOL 没有对应的 Currency 枚举，使用 USDC 占位（实际需要扩展枚举）
		balances[common.Currency_USDC] = solBalance
	}

	return balances, nil
}

// HasSufficientBalance 检查余额是否充足
func (s *BalanceService) HasSufficientBalance(ctx context.Context, walletAddress string, currency common.Currency, requiredAmount uint64) (bool, uint64, error) {
	balanceInfo, err := s.GetBalance(ctx, walletAddress, currency)
	if err != nil {
		return false, 0, err
	}

	hasSufficient := balanceInfo.Balance >= requiredAmount
	return hasSufficient, balanceInfo.Balance, nil
}

// HasSufficientSOL 检查 SOL 余额是否充足（用于支付租金等）
func (s *BalanceService) HasSufficientSOL(ctx context.Context, walletAddress string, requiredSOL uint64) (bool, uint64, error) {
	balanceInfo, err := s.GetSOLBalance(ctx, walletAddress)
	if err != nil {
		return false, 0, err
	}

	hasSufficient := balanceInfo.Balance >= requiredSOL
	return hasSufficient, balanceInfo.Balance, nil
}

// getTokenMint 获取 Token Mint 地址
func (s *BalanceService) getTokenMint(currency common.Currency, network string) (string, error) {
	isMainnet := network == "mainnet" || network == "mainnet-beta"

	switch currency {
	case common.Currency_USDC:
		if isMainnet {
			return internalsolana.USDCMainnet.String(), nil
		}
		return internalsolana.USDCDevnet.String(), nil
	case common.Currency_USDT:
		if isMainnet {
			return internalsolana.USDTMainnet.String(), nil
		}
		return internalsolana.USDTDevnet.String(), nil
	default:
		return "", fmt.Errorf("unsupported currency: %v", currency)
	}
}

// getTokenDecimals 获取 Token 精度
func (s *BalanceService) getTokenDecimals(currency common.Currency) uint8 {
	switch currency {
	case common.Currency_USDC, common.Currency_USDT:
		return 6
	default:
		return 6
	}
}

// FormatBalance 格式化余额为可读字符串
// 例如: 1500000 (6 decimals) → "1.5"
func FormatBalance(balance uint64, decimals uint8) string {
	return fmt.Sprintf("%.6f", internalsolana.MinorUnitsToAmount(balance, decimals))
}

// ParseBalance 解析可读字符串为最小单位
// 例如: "1.5" (6 decimals) → 1500000
func ParseBalance(amountStr string, decimals uint8) (uint64, error) {
	var amount float64
	_, err := fmt.Sscanf(amountStr, "%f", &amount)
	if err != nil {
		return 0, fmt.Errorf("invalid amount format: %w", err)
	}
	return internalsolana.AmountToMinorUnits(amount, decimals), nil
}

// GetTokenAccountInfo 获取 Token 账户信息
// 包括 ATA 地址是否存在
func (s *BalanceService) GetTokenAccountInfo(ctx context.Context, walletAddress string, currency common.Currency) (*TokenAccountInfo, error) {
	// 获取 Mint 地址
	network := s.client.GetNetwork()
	mintAddress, err := s.getTokenMint(currency, network)
	if err != nil {
		return nil, err
	}

	// 计算 ATA 地址
	walletPubkey, err := solana.PublicKeyFromBase58(walletAddress)
	if err != nil {
		return nil, fmt.Errorf("invalid wallet address: %w", err)
	}

	mintPubkey, err := solana.PublicKeyFromBase58(mintAddress)
	if err != nil {
		return nil, fmt.Errorf("invalid mint address: %w", err)
	}

	ata, _, err := solana.FindAssociatedTokenAddress(walletPubkey, mintPubkey)
	if err != nil {
		return nil, fmt.Errorf("failed to find ATA: %w", err)
	}

	// 查询余额来判断 ATA 是否存在
	balance, err := s.client.GetTokenBalance(walletAddress, mintAddress)
	if err != nil {
		return nil, err
	}

	return &TokenAccountInfo{
		WalletAddress: walletAddress,
		MintAddress:   mintAddress,
		ATAAddress:    ata.String(),
		Exists:        balance > 0, // 简化判断，实际需要检查账户是否存在
		Balance:       balance,
		Decimals:      s.getTokenDecimals(currency),
	}, nil
}

// TokenAccountInfo Token 账户信息
type TokenAccountInfo struct {
	WalletAddress string // 钱包地址
	MintAddress   string // Mint 地址
	ATAAddress    string // Associated Token Account 地址
	Exists        bool   // 是否存在
	Balance       uint64 // 余额
	Decimals      uint8  // 精度
}
