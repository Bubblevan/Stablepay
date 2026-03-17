// Package solana 提供 Solana 区块链交互功能
// 从 demo1/internal/solana/client.go 迁移适配
package solana

import (
	"context"
	"fmt"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

// Network 定义 Solana 网络类型
type Network string

const (
	// DevNet Solana 开发网 (公共测试网)
	DevNet Network = "devnet"
	// LocalNet 本地测试网 (solana-test-validator)
	LocalNet Network = "localnet"
	// MainNetBeta Solana 主网
	MainNetBeta Network = "mainnet-beta"
)

// RPC 端点映射
var rpcEndpoints = map[Network]string{
	DevNet:      "https://api.devnet.solana.com",
	LocalNet:    "http://localhost:8899",
	MainNetBeta: "https://api.mainnet-beta.solana.com",
}

// Explorer 基础 URL
var explorerURLs = map[Network]string{
	DevNet:      "https://explorer.solana.com",
	LocalNet:    "https://explorer.solana.com",
	MainNetBeta: "https://explorer.solana.com",
}

// Client 封装 Solana RPC 客户端
type Client struct {
	rpcClient *rpc.Client
	network   Network
	endpoint  string
}

// NewClient 创建新的 Solana RPC 客户端
// network: devnet, localnet, mainnet-beta
func NewClient(network string) (*Client, error) {
	net := Network(network)
	endpoint, ok := rpcEndpoints[net]
	if !ok {
		return nil, fmt.Errorf("unsupported network: %s (valid: devnet, localnet, mainnet-beta)", network)
	}

	rpcClient := rpc.New(endpoint)

	return &Client{
		rpcClient: rpcClient,
		network:   net,
		endpoint:  endpoint,
	}, nil
}

// GetBalance 查询账户 SOL 余额 (返回 lamports)
func (c *Client) GetBalance(address string) (uint64, error) {
	pubKey, err := solana.PublicKeyFromBase58(address)
	if err != nil {
		return 0, fmt.Errorf("invalid address: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := c.rpcClient.GetBalance(ctx, pubKey, rpc.CommitmentFinalized)
	if err != nil {
		return 0, fmt.Errorf("failed to get balance: %w", err)
	}

	return result.Value, nil
}

// GetTokenBalance 查询 SPL Token 余额
// 适用于 USDC/USDT 等代币
func (c *Client) GetTokenBalance(walletAddress string, mintAddress string) (uint64, error) {
	walletPubKey, err := solana.PublicKeyFromBase58(walletAddress)
	if err != nil {
		return 0, fmt.Errorf("invalid wallet address: %w", err)
	}

	mintPubKey, err := solana.PublicKeyFromBase58(mintAddress)
	if err != nil {
		return 0, fmt.Errorf("invalid mint address: %w", err)
	}

	// 计算 Associated Token Account (ATA) 地址
	tokenAccount, _, err := solana.FindAssociatedTokenAddress(walletPubKey, mintPubKey)
	if err != nil {
		return 0, fmt.Errorf("failed to find associated token address: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := c.rpcClient.GetTokenAccountBalance(ctx, tokenAccount, rpc.CommitmentFinalized)
	if err != nil {
		// 可能是ATA不存在
		return 0, nil
	}

	if result.Value == nil {
		return 0, nil
	}

	// Amount 是字符串类型，解析为 uint64
	var amount uint64
	_, err = fmt.Sscanf(result.Value.Amount, "%d", &amount)
	if err != nil {
		return 0, fmt.Errorf("failed to parse token balance: %w", err)
	}

	return amount, nil
}

// GetRecentBlockhash 获取最新的 blockhash 用于交易
func (c *Client) GetRecentBlockhash() (solana.Hash, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	recent, err := c.rpcClient.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return solana.Hash{}, fmt.Errorf("failed to get recent blockhash: %w", err)
	}

	return recent.Value.Blockhash, nil
}

// SendTransaction 发送已签名的交易
// 返回交易签名 (base58 编码的 tx hash)
func (c *Client) SendTransaction(tx *solana.Transaction) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sig, err := c.rpcClient.SendTransaction(ctx, tx)
	if err != nil {
		return "", fmt.Errorf("failed to send transaction: %w", err)
	}

	return sig.String(), nil
}

// GetTransactionStatus 查询交易状态
// 返回交易详情，如果交易未确认则返回 nil, nil
func (c *Client) GetTransactionStatus(sig string) (*rpc.GetTransactionResult, error) {
	signature, err := solana.SignatureFromBase58(sig)
	if err != nil {
		return nil, fmt.Errorf("invalid signature: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := c.rpcClient.GetTransaction(ctx, signature, &rpc.GetTransactionOpts{
		Commitment: rpc.CommitmentConfirmed,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction: %w", err)
	}

	return result, nil
}

// WaitForConfirmation 等待交易确认
// 轮询直到交易被确认或超时
func (c *Client) WaitForConfirmation(sig string, timeout time.Duration) (*rpc.GetTransactionResult, error) {
	start := time.Now()
	checkInterval := 3 * time.Second

	for time.Since(start) < timeout {
		result, err := c.GetTransactionStatus(sig)
		if err != nil {
			// 交易可能还在处理中，继续等待
			time.Sleep(checkInterval)
			continue
		}
		if result != nil {
			return result, nil
		}
		time.Sleep(checkInterval)
	}
	return nil, fmt.Errorf("transaction confirmation timeout after %v", timeout)
}

// GetExplorerURL 获取交易的浏览器链接
func (c *Client) GetExplorerURL(sig string) string {
	baseURL := explorerURLs[c.network]
	if c.network == DevNet {
		return fmt.Sprintf("%s/tx/%s?cluster=devnet", baseURL, sig)
	} else if c.network == LocalNet {
		return fmt.Sprintf("%s/tx/%s?cluster=custom&customUrl=http://localhost:8899", baseURL, sig)
	}
	return fmt.Sprintf("%s/tx/%s", baseURL, sig)
}

// GetNetwork 返回当前网络类型
func (c *Client) GetNetwork() string {
	return string(c.network)
}

// GetEndpoint 返回当前 RPC 端点
func (c *Client) GetEndpoint() string {
	return c.endpoint
}

// SOLToLamports 将 SOL 转换为 lamports
// 1 SOL = 10^9 lamports
func SOLToLamports(sol float64) uint64 {
	return uint64(sol * 1_000_000_000)
}

// LamportsToSOL 将 lamports 转换为 SOL
func LamportsToSOL(lamports uint64) float64 {
	return float64(lamports) / 1_000_000_000
}
