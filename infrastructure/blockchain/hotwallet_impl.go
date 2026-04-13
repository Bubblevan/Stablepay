// Package blockchain 热钱包基础设施实现
package blockchain

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"github.com/stablepay/blockchain-adapter/domain/entity"
	"github.com/stablepay/blockchain-adapter/domain/gateway"

	"github.com/gagliardetto/solana-go"
	"github.com/mr-tron/base58"
)

// HotWalletImpl 热钱包实现
type HotWalletImpl struct {
	Address     string `json:"address"`
	DID         string `json:"did"`
	PublicKey   string `json:"public_key"`
	PrivateKey  string `json:"private_key"`
	Role        string `json:"role"`
	Description string `json:"description"`
}

// NewHotWallet 加载热钱包
func NewHotWallet(path string) (gateway.HotWalletGateway, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read hot wallet file: %w", err)
	}

	var hw HotWalletImpl
	if err := json.Unmarshal(data, &hw); err != nil {
		return nil, fmt.Errorf("failed to unmarshal hot wallet: %w", err)
	}

	// 验证热钱包配置
	if err := hw.Validate(); err != nil {
		return nil, fmt.Errorf("hot wallet validation failed: %w", err)
	}

	return &hw, nil
}

// GetAddress 获取地址
func (h *HotWalletImpl) GetAddress() string {
	return h.Address
}

// GetPublicKey 获取公钥
func (h *HotWalletImpl) GetPublicKey() string {
	return h.PublicKey
}

// SignTransaction 签名交易（领域实体方式）
// 注意：TransactionEntity 当前设计用于表示交易状态，不包含可签名的原始交易数据
// 实际业务中使用 SignBase64Transaction 方法处理 Base64 编码的完整交易
func (h *HotWalletImpl) SignTransaction(tx *entity.TransactionEntity) error {
	// TransactionEntity 目前只包含交易状态和结果，不包含可签名的原始数据
	// 如需支持此方式，需要扩展 TransactionEntity 包含序列化后的交易数据
	return fmt.Errorf("SignTransaction not implemented for TransactionEntity, use SignBase64Transaction instead")
}

// SignBase64Transaction 签名 Base64 编码的交易
// 实现步骤：
// 1. 反序列化 Base64 交易
// 2. 获取热钱包私钥
// 3. 使用私钥对交易进行部分签名（作为 fee payer）
// 4. 序列化签名后的交易并返回
func (h *HotWalletImpl) SignBase64Transaction(base64Tx string) (string, error) {
	if base64Tx == "" {
		return "", fmt.Errorf("transaction is empty")
	}

	// 1. 反序列化交易
	tx := &solana.Transaction{}
	if err := tx.UnmarshalBase64(base64Tx); err != nil {
		return "", fmt.Errorf("failed to unmarshal transaction: %w", err)
	}

	// 2. 获取热钱包私钥
	privKey, err := h.GetPrivateKey()
	if err != nil {
		return "", fmt.Errorf("failed to get private key: %w", err)
	}

	// 3. 验证热钱包地址是 fee payer（第一个 account）
	if len(tx.Message.AccountKeys) == 0 {
		return "", fmt.Errorf("transaction has no account keys")
	}

	feePayer := tx.Message.AccountKeys[0]
	hotWalletPubKey := privKey.PublicKey()

	if !feePayer.Equals(hotWalletPubKey) {
		return "", fmt.Errorf("hot wallet is not the fee payer: expected %s, got %s", 
			hotWalletPubKey.String(), feePayer.String())
	}

	// 4. 仅补签 fee payer（热钱包）；买家等已签槽位保留。
	// Transaction.Sign 要求为全部 signer 提供私钥；PartialSign 只对 getter 非 nil 的密钥签名。
	signatures, err := tx.PartialSign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(hotWalletPubKey) {
			return &privKey
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("failed to sign transaction: %w", err)
	}

	// 验证签名是否成功
	if len(signatures) == 0 {
		return "", fmt.Errorf("no signatures produced")
	}

	// 5. 序列化签名后的交易
	signedTxBytes, err := tx.MarshalBinary()
	if err != nil {
		return "", fmt.Errorf("failed to marshal signed transaction: %w", err)
	}
	signedTxBase64 := base64.StdEncoding.EncodeToString(signedTxBytes)

	return signedTxBase64, nil
}

// Validate 验证热钱包
func (h *HotWalletImpl) Validate() error {
	if h.Address == "" {
		return fmt.Errorf("address is empty")
	}
	if h.PrivateKey == "" {
		return fmt.Errorf("private key is empty")
	}

	// 解码私钥
	privKeyBytes, err := base58.Decode(h.PrivateKey)
	if err != nil {
		return fmt.Errorf("failed to decode private key: %w", err)
	}
	if len(privKeyBytes) != 64 {
		return fmt.Errorf("invalid private key length: expected 64, got %d", len(privKeyBytes))
	}

	// 验证公钥匹配
	privKey := solana.PrivateKey(privKeyBytes)
	derivedPubkey := privKey.PublicKey()

	addrPubkey, err := solana.PublicKeyFromBase58(h.Address)
	if err != nil {
		return fmt.Errorf("invalid address: %w", err)
	}

	if !derivedPubkey.Equals(addrPubkey) {
		return fmt.Errorf("public key does not match address")
	}

	return nil
}

// GetPrivateKey 获取私钥（内部使用）
func (h *HotWalletImpl) GetPrivateKey() (solana.PrivateKey, error) {
	keyBytes, err := base58.Decode(h.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode private key: %w", err)
	}
	return solana.PrivateKey(keyBytes), nil
}
