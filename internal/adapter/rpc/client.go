// Package rpc RPC 客户端实现
package rpc

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/kitex/client"
	"github.com/cloudwego/kitex/pkg/retry"
	"github.com/stablepay/payment-service/internal/domain/service"
	"github.com/stablepay/payment-service/pkg/constants"
)

// DIDServiceClient DID Service RPC 客户端
type DIDServiceClient struct {
	// 实际使用 Kitex 生成的客户端
	// client DIDService.Client
	address string
	timeout time.Duration
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
// 调用 DID Service 的 VerifySignature 接口
func (c *DIDServiceClient) Validate(ctx context.Context, did string, message string, signature string, timestamp string, nonce string) (bool, error) {
	// TODO: 使用 Kitex 生成的客户端调用
	// 这里提供骨架实现，待 IDL 最终确定后替换

	// 模拟实现
	fmt.Printf("[DIDServiceClient] Validate signature for DID: %s\n", did)

	// 实际调用示例：
	// req := &did_service.VerifySignatureRequest{
	//     Base: &common.BaseReq{...},
	//     Did: did,
	//     Message: message,
	//     Signature: signature,
	//     Timestamp: timestamp,
	//     Nonce: nonce,
	// }
	// resp, err := client.VerifySignature(ctx, req)
	// if err != nil {
	//     return false, errors.Wrap(errors.SERVICE_UNAVAILABLE, err, "did service call failed")
	// }
	// return resp.Valid, nil

	// 临时返回成功
	return true, nil
}

// Ensure DIDServiceClient 实现 SignatureValidator 接口
var _ service.SignatureValidator = (*DIDServiceClient)(nil)

// BlockchainAdapterClient Blockchain Adapter RPC 客户端
type BlockchainAdapterClient struct {
	address string
	timeout time.Duration
	retryCount int
}

// NewBlockchainAdapterClient 创建 Blockchain Adapter 客户端
func NewBlockchainAdapterClient(address string, timeoutMs int, retryCount int) *BlockchainAdapterClient {
	return &BlockchainAdapterClient{
		address:    address,
		timeout:    time.Duration(timeoutMs) * time.Millisecond,
		retryCount: retryCount,
	}
}

// ExecuteTransfer 执行转账交易
// 调用 Blockchain Adapter 的 TransferStableCoin 接口
func (c *BlockchainAdapterClient) ExecuteTransfer(ctx context.Context, fromWallet, toWallet string, amountMinor int64, currency constants.Currency) (string, error) {
	// TODO: 使用 Kitex 生成的客户端调用
	// 这里提供骨架实现，待 IDL 最终确定后替换

	fmt.Printf("[BlockchainAdapterClient] Execute transfer: %s -> %s, amount: %d %s\n",
		fromWallet, toWallet, amountMinor, currency)

	// 实际调用示例：
	// req := &blockchain_adapter.TransferStableCoinRequest{
	//     Base: &common.BaseReq{...},
	//     FromWalletAddress: fromWallet,
	//     ToWalletAddress: toWallet,
	//     AmountMinor: amountMinor,
	//     Currency: convertCurrency(currency),
	// }
	// resp, err := client.TransferStableCoin(ctx, req)
	// if err != nil {
	//     return "", errors.Wrap(errors.BLOCKCHAIN_NETWORK_ERROR, err, "blockchain adapter call failed")
	// }
	// if resp.Base.Code != 0 {
	//     return "", errors.New(errors.BLOCKCHAIN_NETWORK_ERROR, resp.Base.Message)
	// }
	// return resp.TxHash, nil

	// 临时返回模拟 txHash
	return fmt.Sprintf("simulated_tx_hash_%d", time.Now().UnixNano()), nil
}

// QueryTxStatus 查询交易状态
// 调用 Blockchain Adapter 的 GetTxStatus 接口
func (c *BlockchainAdapterClient) QueryTxStatus(ctx context.Context, txHash string) (int8, *time.Time, error) {
	// TODO: 使用 Kitex 生成的客户端调用

	fmt.Printf("[BlockchainAdapterClient] Query tx status: %s\n", txHash)

	// 实际调用示例：
	// req := &blockchain_adapter.GetTxStatusRequest{
	//     Base: &common.BaseReq{...},
	//     TxHash: txHash,
	// }
	// resp, err := client.GetTxStatus(ctx, req)
	// ...

	// 临时返回模拟结果
	return constants.PaymentStatusConfirmed, nil, nil
}

// CheckBalance 检查余额
// 调用 Blockchain Adapter 的 GetBalance 接口
func (c *BlockchainAdapterClient) CheckBalance(ctx context.Context, walletAddress string, currency constants.Currency, requiredAmount int64) (bool, int64, error) {
	// TODO: 使用 Kitex 生成的客户端调用

	fmt.Printf("[BlockchainAdapterClient] Check balance: %s, required: %d %s\n",
		walletAddress, requiredAmount, currency)

	// 实际调用示例：
	// req := &blockchain_adapter.GetBalanceRequest{
	//     Base: &common.BaseReq{...},
	//     WalletAddress: walletAddress,
	//     Currency: convertCurrency(currency),
	// }
	// resp, err := client.GetBalance(ctx, req)
	// ...

	// 临时返回模拟结果（余额充足）
	return true, requiredAmount * 2, nil
}

// Ensure BlockchainAdapterClient 实现所需接口
var _ service.BalanceChecker = (*BlockchainAdapterClient)(nil)

// KitexClientConfig 返回 Kitex 客户端配置
func KitexClientConfig(timeout time.Duration, retryCount int) []client.Option {
	opts := []client.Option{
		client.WithRPCTimeout(timeout),
	}

	if retryCount > 0 {
		opts = append(opts, client.WithFailureRetry(
			retry.NewFailurePolicy(),
		))
	}

	return opts
}
