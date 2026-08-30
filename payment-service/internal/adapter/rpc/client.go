// Package rpc RPC 客户端实现
package rpc

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/cloudwego/kitex/client"
	"github.com/cloudwego/kitex/pkg/retry"
	bchain "github.com/stablepay/blockchain-adapter/kitex_gen/stablepay/blockchain_adapter"
	bchainsvc "github.com/stablepay/blockchain-adapter/kitex_gen/stablepay/blockchain_adapter/blockchainadapterservice"
	bcommon "github.com/stablepay/blockchain-adapter/kitex_gen/stablepay/common"
	"github.com/stablepay/payment-service/internal/domain/service"
	"github.com/stablepay/payment-service/pkg/constants"
	"github.com/stablepay/payment-service/pkg/errors"
)

// DIDServiceClient DID Service RPC 客户端
type DIDServiceClient struct {
	address    string
	timeout    time.Duration
	retryCount int
}

// NewDIDServiceClient 创建 DID Service 客户端
func NewDIDServiceClient(address string, timeoutMs int, retryCount int) *DIDServiceClient {
	return &DIDServiceClient{
		address:    address,
		timeout:    time.Duration(timeoutMs) * time.Millisecond,
		retryCount: retryCount,
	}
}

// ValidateSignature 验证签名
func (c *DIDServiceClient) Validate(ctx context.Context, did string, message string, signature string, timestamp string, nonce string) (bool, error) {
	_ = ctx
	_ = did
	_ = message
	_ = signature
	_ = timestamp
	_ = nonce
	// TODO: Kitex 调用 did-service
	return true, nil
}

var _ service.SignatureValidator = (*DIDServiceClient)(nil)

// BlockchainAdapterClient Blockchain Adapter RPC 客户端（TCP + Kitex）
type BlockchainAdapterClient struct {
	cli bchainsvc.Client
}

// NewBlockchainAdapterClient 创建客户端。address 须为 TCP host:port（如 stablepay-blockchain-adapter:8083）。
// 必须使用 client.WithHostPorts，否则 Kitex 会把整串当作 Unix 套接字路径，出现 dial unix ... connect: no such file or directory。
func NewBlockchainAdapterClient(address string, timeoutMs int, retryCount int) (*BlockchainAdapterClient, error) {
	opts := []client.Option{
		client.WithHostPorts(address),
	}
	if timeoutMs > 0 {
		opts = append(opts, client.WithRPCTimeout(time.Duration(timeoutMs)*time.Millisecond))
	}
	if retryCount > 0 {
		opts = append(opts, client.WithFailureRetry(retry.NewFailurePolicy()))
	}
	cli, err := bchainsvc.NewClient("blockchain-adapter", opts...)
	if err != nil {
		return nil, fmt.Errorf("kitex blockchain-adapter client: %w", err)
	}
	return &BlockchainAdapterClient{cli: cli}, nil
}

func toBCurrency(c constants.Currency) bcommon.Currency {
	return bcommon.Currency(c)
}

// ExecuteTransfer 执行转账交易
func (c *BlockchainAdapterClient) ExecuteTransfer(ctx context.Context, fromWallet, toWallet string, amountMinor int64, currency constants.Currency, signedTxBase64 string) (string, error) {
	req := bchain.NewTransferStableCoinRequest()
	req.Base = bcommon.NewBaseReq()
	req.FromWalletAddress = &fromWallet
	req.ToWalletAddress = toWallet
	req.AmountMinor = amountMinor
	req.Currency = toBCurrency(currency)
	if signedTxBase64 != "" {
		req.SignedTxBase64 = &signedTxBase64
	}

	resp, err := c.cli.TransferStableCoin(ctx, req)
	if err != nil {
		log.Printf("[blockchain-adapter] TransferStableCoin transport error: %v | from=%s to=%s amount_minor=%d signed_tx_len=%d",
			err, fromWallet, toWallet, amountMinor, len(signedTxBase64))
		return "", errors.Wrap(errors.BLOCKCHAIN_NETWORK_ERROR, err, "blockchain adapter TransferStableCoin")
	}
	if resp.GetBase() != nil && resp.GetBase().GetCode() != 0 {
		bc := resp.GetBase().GetCode()
		msg := resp.GetBase().GetMessage()
		log.Printf("[blockchain-adapter] TransferStableCoin logical error: adapter_code=%d adapter_message=%q | from=%s to=%s signed_tx_len=%d",
			bc, msg, fromWallet, toWallet, len(signedTxBase64))
		switch bc {
		case int32(bcommon.ErrorCode_INVALID_PARAMETERS):
			return "", errors.New(errors.INVALID_PARAMETERS, msg)
		case int32(bcommon.ErrorCode_INSUFFICIENT_BALANCE):
			return "", errors.New(errors.INSUFFICIENT_BALANCE, msg)
		default:
			return "", errors.New(errors.BLOCKCHAIN_NETWORK_ERROR, msg)
		}
	}
	return string(resp.GetTxHash()), nil
}

// QueryTxStatus 查询交易状态
func (c *BlockchainAdapterClient) QueryTxStatus(ctx context.Context, txHash string) (int8, *time.Time, error) {
	req := bchain.NewGetTxStatusRequest()
	req.Base = bcommon.NewBaseReq()
	req.TxHash = txHash

	resp, err := c.cli.GetTxStatus(ctx, req)
	if err != nil {
		return 0, nil, errors.Wrap(errors.BLOCKCHAIN_NETWORK_ERROR, err, "blockchain adapter GetTxStatus")
	}
	if resp.GetBase() != nil && resp.GetBase().GetCode() != 0 {
		return 0, nil, errors.New(errors.BLOCKCHAIN_NETWORK_ERROR, resp.GetBase().GetMessage())
	}
	st := int8(resp.GetStatus())
	var confirmedAt *time.Time
	if resp.IsSetConfirmedAt() {
		s := resp.GetConfirmedAt()
		if s != "" {
			t, err := time.Parse(time.RFC3339, s)
			if err == nil {
				confirmedAt = &t
			}
		}
	}
	return st, confirmedAt, nil
}

// CheckBalance 检查余额
func (c *BlockchainAdapterClient) CheckBalance(ctx context.Context, walletAddress string, currency constants.Currency, requiredAmount int64) (bool, int64, error) {
	req := bchain.NewGetBalanceRequest()
	req.Base = bcommon.NewBaseReq()
	req.WalletAddress = walletAddress
	req.Currency = toBCurrency(currency)

	resp, err := c.cli.GetBalance(ctx, req)
	if err != nil {
		return false, 0, errors.Wrap(errors.BLOCKCHAIN_NETWORK_ERROR, err, "blockchain adapter GetBalance")
	}
	if resp.GetBase() != nil && resp.GetBase().GetCode() != 0 {
		return false, 0, errors.New(errors.BLOCKCHAIN_NETWORK_ERROR, resp.GetBase().GetMessage())
	}
	bal := resp.GetBalanceMinor()
	return bal >= requiredAmount, bal, nil
}

var _ service.BalanceChecker = (*BlockchainAdapterClient)(nil)

// KitexClientConfig 返回 Kitex 客户端配置（供其它 RPC 复用）
func KitexClientConfig(timeout time.Duration, retryCount int) []client.Option {
	opts := []client.Option{
		client.WithRPCTimeout(timeout),
	}
	if retryCount > 0 {
		opts = append(opts, client.WithFailureRetry(retry.NewFailurePolicy()))
	}
	return opts
}
