// Package gateway 定义领域网关接口
// SolanaGateway: 定义区块链交互的抽象接口
package gateway

import (
	"context"
	"time"

	"github.com/stablepay/blockchain-adapter/domain/entity"
	"github.com/stablepay/blockchain-adapter/domain/vo"
)

// SolanaGateway Solana 区块链网关接口
// 职责：定义与 Solana 区块链交互的抽象
// 实现：由 Infrastructure 层的 blockchain 包实现
type SolanaGateway interface {
	// GetNetwork 获取当前网络类型
	GetNetwork() string
	
	// GetBalance 查询 SOL 余额
	GetBalance(ctx context.Context, address string) (uint64, error)
	
	// GetTokenBalance 查询 Token 余额
	GetTokenBalance(ctx context.Context, walletAddress, mintAddress string) (uint64, error)
	
	// GetRecentBlockhash 获取最新 blockhash
	GetRecentBlockhash(ctx context.Context) (string, error)
	
	// SendTransaction 发送已签名交易
	SendTransaction(ctx context.Context, signedTx string) (string, error)
	
	// GetTransactionStatus 查询交易状态
	GetTransactionStatus(ctx context.Context, txHash string) (*vo.TxStatusVO, error)
	
	// WaitForConfirmation 等待交易确认
	WaitForConfirmation(ctx context.Context, txHash string, timeout time.Duration) (*vo.TxStatusVO, error)
	
	// GetExplorerURL 获取交易浏览器链接
	GetExplorerURL(txHash string) string
}

// TransactionBuilderGateway 交易构建网关接口
// 职责：定义交易构建的抽象
type TransactionBuilderGateway interface {
	// BuildSOLTransferTx 构建 SOL 转账交易
	BuildSOLTransferTx(from, to string, amountLamports uint64, recentBlockHash string) (string, error)
	
	// BuildSOLTransferTxWithFeePayer 构建带 FeePayer 的 SOL 转账
	BuildSOLTransferTxWithFeePayer(from, to, feePayer string, amountLamports uint64, recentBlockHash string) (string, error)
	
	// DeserializeBase64Tx 反序列化 Base64 交易
	DeserializeBase64Tx(base64Tx string) (*entity.TransactionEntity, error)
	
	// SerializeToBase64 序列化交易为 Base64
	SerializeToBase64(tx *entity.TransactionEntity) (string, error)
	
	// SignTransaction 签名交易
	SignTransaction(tx *entity.TransactionEntity, privateKey string) error
	
	// ValidateTransfer 验证转账参数
	ValidateTransfer(from, to string, amountLamports, balance uint64) error
}

// HotWalletGateway 热钱包网关接口
// 职责：定义热钱包操作的抽象
type HotWalletGateway interface {
	// GetAddress 获取热钱包地址
	GetAddress() string
	
	// GetPublicKey 获取公钥
	GetPublicKey() string
	
	// SignTransaction 使用热钱包签名交易（领域实体方式）
	SignTransaction(tx *entity.TransactionEntity) error
	
	// SignBase64Transaction 签名 Base64 编码的交易
	// 参数：base64 编码的部分签名交易（用户已签名）
	// 返回：base64 编码的完整签名交易（用户 + 热钱包签名）
	SignBase64Transaction(base64Tx string) (string, error)
	
	// Validate 验证热钱包配置
	Validate() error
}

// TokenMint 代币 Mint 地址常量
const (
	USDCDevnet  = "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"
	USDTDevnet  = "BQcdHdAQW1hczDbBi9hiegXAR7A18QhzhCoXFBtBj9QA"
	USDCMainnet = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	USDTMainnet = "Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB"
)

// GetTokenMintByCurrency 根据币种获取 Mint 地址
func GetTokenMintByCurrency(currency string, isMainnet bool) string {
	switch currency {
	case "USDC":
		if isMainnet {
			return USDCMainnet
		}
		return USDCDevnet
	case "USDT":
		if isMainnet {
			return USDTMainnet
		}
		return USDTDevnet
	default:
		return ""
	}
}
