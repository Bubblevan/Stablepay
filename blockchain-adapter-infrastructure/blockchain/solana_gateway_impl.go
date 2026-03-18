// Package blockchain 实现领域网关接口
// 职责：将原有的 Solana 客户端包装为实现 domain/gateway 接口的形式
package blockchain

import (
	"context"
	"fmt"
	"time"

	"stablepay.blockchain_adapter/blockchain-adapter-domain/gateway"
	"stablepay.blockchain_adapter/blockchain-adapter-domain/vo"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

// SolanaGatewayImpl Solana 网关实现
type SolanaGatewayImpl struct {
	client   *rpc.Client
	network  string
	endpoint string
}

// NewSolanaGateway 创建 Solana 网关实现
func NewSolanaGateway(network, endpoint string) (gateway.SolanaGateway, error) {
	client := rpc.New(endpoint)
	return &SolanaGatewayImpl{
		client:   client,
		network:  network,
		endpoint: endpoint,
	}, nil
}

// GetNetwork 获取网络类型
func (s *SolanaGatewayImpl) GetNetwork() string {
	return s.network
}

// GetBalance 查询 SOL 余额
func (s *SolanaGatewayImpl) GetBalance(ctx context.Context, address string) (uint64, error) {
	pubKey, err := solana.PublicKeyFromBase58(address)
	if err != nil {
		return 0, fmt.Errorf("invalid address: %w", err)
	}

	result, err := s.client.GetBalance(ctx, pubKey, rpc.CommitmentFinalized)
	if err != nil {
		return 0, fmt.Errorf("failed to get balance: %w", err)
	}

	return result.Value, nil
}

// GetTokenBalance 查询 Token 余额
func (s *SolanaGatewayImpl) GetTokenBalance(ctx context.Context, walletAddress, mintAddress string) (uint64, error) {
	walletPubKey, err := solana.PublicKeyFromBase58(walletAddress)
	if err != nil {
		return 0, fmt.Errorf("invalid wallet address: %w", err)
	}

	mintPubKey, err := solana.PublicKeyFromBase58(mintAddress)
	if err != nil {
		return 0, fmt.Errorf("invalid mint address: %w", err)
	}

	// 计算 ATA
	tokenAccount, _, err := solana.FindAssociatedTokenAddress(walletPubKey, mintPubKey)
	if err != nil {
		return 0, fmt.Errorf("failed to find ATA: %w", err)
	}

	result, err := s.client.GetTokenAccountBalance(ctx, tokenAccount, rpc.CommitmentFinalized)
	if err != nil {
		// ATA 不存在，返回 0
		return 0, nil
	}

	if result.Value == nil {
		return 0, nil
	}

	// 解析金额（字符串转 uint64）
	var amount uint64
	_, err = fmt.Sscanf(result.Value.Amount, "%d", &amount)
	if err != nil {
		return 0, fmt.Errorf("failed to parse amount: %w", err)
	}

	return amount, nil
}

// GetRecentBlockhash 获取最新 blockhash
func (s *SolanaGatewayImpl) GetRecentBlockhash(ctx context.Context) (string, error) {
	result, err := s.client.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return "", fmt.Errorf("failed to get blockhash: %w", err)
	}
	return result.Value.Blockhash.String(), nil
}

// SendTransaction 发送交易
func (s *SolanaGatewayImpl) SendTransaction(ctx context.Context, signedTx string) (string, error) {
	// 反序列化交易
	tx := &solana.Transaction{}
	if err := tx.UnmarshalBase64(signedTx); err != nil {
		return "", fmt.Errorf("failed to unmarshal transaction: %w", err)
	}

	sig, err := s.client.SendTransaction(ctx, tx)
	if err != nil {
		return "", fmt.Errorf("failed to send transaction: %w", err)
	}

	return sig.String(), nil
}

// GetTransactionStatus 查询交易状态
func (s *SolanaGatewayImpl) GetTransactionStatus(ctx context.Context, txHash string) (*vo.TxStatusVO, error) {
	sig, err := solana.SignatureFromBase58(txHash)
	if err != nil {
		return nil, fmt.Errorf("invalid signature: %w", err)
	}

	result, err := s.client.GetTransaction(ctx, sig, &rpc.GetTransactionOpts{
		Commitment: rpc.CommitmentConfirmed,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction: %w", err)
	}

	// 交易未找到
	if result == nil {
		return &vo.TxStatusVO{
			TxHash: txHash,
			Status: "pending",
		}, nil
	}

	// 构建值对象
	status := "confirmed"
	var fee uint64
	if result.Meta != nil {
		fee = uint64(result.Meta.Fee)
		if result.Meta.Err != nil {
			status = "failed"
		}
	}

	var blockTime *time.Time
	if result.BlockTime != nil {
		t := time.Unix(int64(*result.BlockTime), 0)
		blockTime = &t
	}

	return &vo.TxStatusVO{
		TxHash:    txHash,
		Status:    status,
		Slot:      uint64(result.Slot),
		BlockTime: blockTime,
		Fee:       fee,
		Err:       result.Meta.Err,
	}, nil
}

// WaitForConfirmation 等待交易确认
func (s *SolanaGatewayImpl) WaitForConfirmation(ctx context.Context, txHash string, timeout time.Duration) (*vo.TxStatusVO, error) {
	start := time.Now()
	checkInterval := 3 * time.Second

	for time.Since(start) < timeout {
		status, err := s.GetTransactionStatus(ctx, txHash)
		if err != nil {
			time.Sleep(checkInterval)
			continue
		}

		if status.Status == "confirmed" || status.Status == "failed" {
			return status, nil
		}

		time.Sleep(checkInterval)
	}

	return s.GetTransactionStatus(ctx, txHash)
}

// GetExplorerURL 获取浏览器链接
func (s *SolanaGatewayImpl) GetExplorerURL(txHash string) string {
	baseURL := "https://explorer.solana.com"
	if s.network == "devnet" {
		return fmt.Sprintf("%s/tx/%s?cluster=devnet", baseURL, txHash)
	} else if s.network == "localnet" {
		return fmt.Sprintf("%s/tx/%s?cluster=custom&customUrl=http://localhost:8899", baseURL, txHash)
	}
	return fmt.Sprintf("%s/tx/%s", baseURL, txHash)
}
