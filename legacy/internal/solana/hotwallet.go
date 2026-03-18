// Package solana 提供热钱包管理功能
// 从 demo1/internal/solana/gas_subsidy.go 提取并扩展
package solana

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/gagliardetto/solana-go"
	"github.com/mr-tron/base58"
)

// HotWallet StablePay 官方热钱包
// 用于支付用户交易的 Gas 费用 (FeePayer)
type HotWallet struct {
	Address    string `json:"address"`     // Solana 地址 (Base58)
	DID        string `json:"did"`         // W3C DID 标识符
	PublicKey  string `json:"public_key"`  // Base58 编码公钥
	PrivateKey string `json:"private_key"` // Base58 编码私钥 (64字节)
	Role       string `json:"role"`        // 钱包角色
	Description string `json:"description"` // 描述信息
}

// LoadHotWallet 从文件加载热钱包配置
// 配置文件应为 JSON 格式，包含 address, did, public_key, private_key 等字段
func LoadHotWallet(path string) (*HotWallet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read hot wallet file: %w", err)
	}

	var hw HotWallet
	if err := json.Unmarshal(data, &hw); err != nil {
		return nil, fmt.Errorf("failed to unmarshal hot wallet: %w", err)
	}

	// 验证必要字段
	if hw.Address == "" {
		return nil, fmt.Errorf("hot wallet address is empty")
	}
	if hw.PrivateKey == "" {
		return nil, fmt.Errorf("hot wallet private key is empty")
	}

	return &hw, nil
}

// GetPrivateKey 获取 solana.PrivateKey 对象
// 将 Base58 编码的私钥解码为 64 字节扩展私钥
func (hw *HotWallet) GetPrivateKey() (solana.PrivateKey, error) {
	keyBytes, err := base58.Decode(hw.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode private key: %w", err)
	}
	if len(keyBytes) != 64 {
		return nil, fmt.Errorf("invalid private key length: expected 64 bytes, got %d", len(keyBytes))
	}
	return solana.PrivateKey(keyBytes), nil
}

// GetPublicKey 获取 solana.PublicKey 对象
func (hw *HotWallet) GetPublicKey() (solana.PublicKey, error) {
	return solana.PublicKeyFromBase58(hw.PublicKey)
}

// GetAddressPubkey 获取地址的公钥对象
// 地址和公钥在 Solana 中是同一概念
func (hw *HotWallet) GetAddressPubkey() (solana.PublicKey, error) {
	return solana.PublicKeyFromBase58(hw.Address)
}

// Validate 验证热钱包配置是否完整
func (hw *HotWallet) Validate() error {
	if hw.Address == "" {
		return fmt.Errorf("address is required")
	}
	if hw.PrivateKey == "" {
		return fmt.Errorf("private key is required")
	}
	if hw.PublicKey == "" {
		return fmt.Errorf("public key is required")
	}

	// 验证地址格式
	addrPubkey, err := hw.GetAddressPubkey()
	if err != nil {
		return fmt.Errorf("invalid address format: %w", err)
	}

	// 验证私钥并检查公钥是否匹配
	privKey, err := hw.GetPrivateKey()
	if err != nil {
		return fmt.Errorf("invalid private key: %w", err)
	}

	// 验证公钥是否从私钥派生
	derivedPubkey := privKey.PublicKey()
	if !derivedPubkey.Equals(addrPubkey) {
		return fmt.Errorf("public key does not match address")
	}

	return nil
}

// SignTransaction 使用热钱包签名交易
// 这是 Gas 补贴的核心操作：热钱包作为 FeePayer 签名
func (hw *HotWallet) SignTransaction(tx *solana.Transaction) error {
	privKey, err := hw.GetPrivateKey()
	if err != nil {
		return fmt.Errorf("failed to get private key: %w", err)
	}

	return SignTransactionWithKey(tx, privKey)
}

// String 返回热钱包的格式化字符串（不包含私钥）
func (hw *HotWallet) String() string {
	return fmt.Sprintf(
		"HotWallet{\n"+
		"  Address: %s\n"+
		"  DID: %s\n"+
		"  Role: %s\n"+
		"  Description: %s\n"+
		"}",
		hw.Address,
		hw.DID,
		hw.Role,
		hw.Description,
	)
}

// SaveToFile 将热钱包配置保存到文件
// 注意：生产环境中应加密存储私钥
func (hw *HotWallet) SaveToFile(path string) error {
	data, err := json.MarshalIndent(hw, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal hot wallet: %w", err)
	}

	// 设置文件权限为 0600 (仅所有者可读写)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write hot wallet file: %w", err)
	}

	return nil
}
