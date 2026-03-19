// Package service 查询服务
// 职责：处理查询类请求，不涉及事务
package service

import (
	"context"
	"fmt"

	"github.com/stablepay/blockchain-adapter/domain/gateway"
	"github.com/stablepay/blockchain-adapter/domain/vo"
)

// BalanceQueryService 余额查询服务
type BalanceQueryService struct {
	solanaGateway gateway.SolanaGateway
}

// NewBalanceQueryService 创建余额查询服务
func NewBalanceQueryService(solanaGateway gateway.SolanaGateway) *BalanceQueryService {
	return &BalanceQueryService{
		solanaGateway: solanaGateway,
	}
}

// BalanceQuery 余额查询参数
type BalanceQuery struct {
	WalletAddress string
	Currency      string
	MintAddress   string // 可选，直接指定 mint 地址
}

// GetBalance 查询余额
// 优先使用 MintAddress，如果没有则根据 Currency 推断
func (s *BalanceQueryService) GetBalance(ctx context.Context, query *BalanceQuery) (*vo.BalanceVO, error) {
	// 验证地址
	if query.WalletAddress == "" {
		return nil, fmt.Errorf("wallet address is required")
	}

	// 确定币种和 mint 地址
	currency, mintAddress, decimals := s.resolveCurrencyAndMint(query)

	// 查询余额
	balance, err := s.solanaGateway.GetTokenBalance(ctx, query.WalletAddress, mintAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to get balance: %w", err)
	}

	return &vo.BalanceVO{
		Address:  query.WalletAddress,
		Balance:  balance,
		Decimals: decimals,
		Symbol:   currency,
	}, nil
}

// resolveCurrencyAndMint 解析币种和 mint 地址
// 优先级：1. MintAddress 参数 2. Currency 参数
func (s *BalanceQueryService) resolveCurrencyAndMint(query *BalanceQuery) (currency, mintAddress string, decimals uint8) {
	// 默认精度
	decimals = 6 // Token 默认 6 位

	// 如果提供了 MintAddress，尝试推断币种
	if query.MintAddress != "" {
		mintAddress = query.MintAddress
		currency = s.inferCurrencyByMint(mintAddress)
		return
	}

	// 根据 Currency 获取 Mint 地址
	currency = query.Currency
	if currency == "" {
		currency = "USDC" // 默认 USDC
	}

	if currency == "SOL" {
		mintAddress = "" // SOL 不需要 mint 地址
		decimals = 9
		return
	}

	isMainnet := s.solanaGateway.GetNetwork() == "mainnet" || s.solanaGateway.GetNetwork() == "mainnet-beta"
	mintAddress = gateway.GetTokenMintByCurrency(currency, isMainnet)

	return
}

// inferCurrencyByMint 根据 mint 地址推断币种
func (s *BalanceQueryService) inferCurrencyByMint(mint string) string {
	switch mint {
	case gateway.USDCMainnet, gateway.USDCDevnet:
		return "USDC"
	case gateway.USDTMainnet, gateway.USDTDevnet:
		return "USDT"
	default:
		return "UNKNOWN"
	}
}

// GetSOLBalance 查询 SOL 余额
func (s *BalanceQueryService) GetSOLBalance(ctx context.Context, address string) (*vo.BalanceVO, error) {
	if address == "" {
		return nil, fmt.Errorf("address is required")
	}

	balance, err := s.solanaGateway.GetBalance(ctx, address)
	if err != nil {
		return nil, fmt.Errorf("failed to get SOL balance: %w", err)
	}

	return &vo.BalanceVO{
		Address:  address,
		Balance:  balance,
		Decimals: 9,
		Symbol:   "SOL",
	}, nil
}
