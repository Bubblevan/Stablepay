// Package solana 提供 Solana 交易构建和签名功能
// 从 demo1/internal/solana/transaction.go 迁移适配
package solana

import (
	"encoding/base64"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/mr-tron/base58"
)

// TransactionBuilder 提供交易构建功能
type TransactionBuilder struct{}

// NewTransactionBuilder 创建新的交易构建器
func NewTransactionBuilder() *TransactionBuilder {
	return &TransactionBuilder{}
}

// BuildSOLTransferTx 构建原生 SOL 转账交易
// 使用 System Program 的 Transfer 指令
//
// 参数:
//   - from: 付款方公钥
//   - to: 收款方公钥
//   - amountLamports: 转账金额 (lamports)
//   - recentBlockHash: 最新的 blockhash
//
// 返回:
//   - 未签名的交易对象
func (tb *TransactionBuilder) BuildSOLTransferTx(
	from solana.PublicKey,
	to solana.PublicKey,
	amountLamports uint64,
	recentBlockHash solana.Hash,
) (*solana.Transaction, error) {
	// 创建 SystemProgram.Transfer 指令
	// 这是 Solana 原生 SOL 转账的标准方式
	instruction := system.NewTransferInstruction(
		amountLamports, // 转账金额 (lamports)
		from,           // 付款方
		to,             // 收款方
	).Build()

	// 构建交易
	// solana-go 的 NewTransaction 会自动从指令中提取账户并设置 fee payer
	tx, err := solana.NewTransaction([]solana.Instruction{instruction}, recentBlockHash)
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction: %w", err)
	}

	return tx, nil
}

// BuildSOLTransferTxWithFeePayer 构建带自定义 FeePayer 的 SOL 转账交易
// FeePayer 是支付交易手续费的账户（Gas 补贴场景下是热钱包）
//
// 参数:
//   - from: 付款方公钥 (Agent)
//   - to: 收款方公钥 (Developer)
//   - feePayer: 手续费支付方公钥 (StablePay 热钱包)
//   - amountLamports: 转账金额 (lamports)
//   - recentBlockHash: 最新的 blockhash
func (tb *TransactionBuilder) BuildSOLTransferTxWithFeePayer(
	from solana.PublicKey,
	to solana.PublicKey,
	feePayer solana.PublicKey,
	amountLamports uint64,
	recentBlockHash solana.Hash,
) (*solana.Transaction, error) {
	// 创建 SystemProgram.Transfer 指令
	instruction := system.NewTransferInstruction(
		amountLamports,
		from,
		to,
	).Build()

	// 构建交易
	tx, err := solana.NewTransaction([]solana.Instruction{instruction}, recentBlockHash)
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction: %w", err)
	}

	// 设置自定义 FeePayer（重要：这决定了谁支付手续费）
	// 注意：solana-go 没有直接的 SetFeePayer 方法，需要通过修改账户列表实现
	// 第一个账户默认是 FeePayer
	tx.Message.AccountKeys[0] = feePayer

	return tx, nil
}

// DeserializeBase64Tx 从 Base64 字符串反序列化交易
// 用于接收前端传来的部分签名交易
func DeserializeBase64Tx(base64Tx string) (*solana.Transaction, error) {
	tx := new(solana.Transaction)
	if err := tx.UnmarshalBase64(base64Tx); err != nil {
		return nil, fmt.Errorf("failed to deserialize base64 transaction: %w", err)
	}
	return tx, nil
}

// SerializeToBase64 将交易序列化为 Base64 字符串
func SerializeToBase64(tx *solana.Transaction) (string, error) {
	// 序列化交易为字节
	data, err := tx.MarshalBinary()
	if err != nil {
		return "", fmt.Errorf("failed to marshal transaction: %w", err)
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// SignTransaction 使用私钥签名交易
//
// 参数:
//   - tx: 待签名的交易
//   - privateKey: Ed25519 私钥 (64 字节扩展私钥)
//
// 注意: 交易会原地修改，添加签名
func SignTransaction(tx *solana.Transaction, privateKey []byte) error {
	// 从私钥创建 Solana 私钥对象
	// 注意: solana-go 需要 64 字节的扩展私钥
	if len(privateKey) != 64 {
		return fmt.Errorf("invalid private key length: expected 64 bytes, got %d", len(privateKey))
	}

	privKey := solana.PrivateKey(privateKey)

	// 签名交易
	_, err := tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		// 如果公钥匹配，返回对应的私钥
		if key.Equals(privKey.PublicKey()) {
			return &privKey
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to sign transaction: %w", err)
	}

	return nil
}

// SignTransactionWithMultipleKeys 使用多个私钥签名交易（多签名）
// 用于 Gas 补贴场景：Agent 签名 + 热钱包签名
//
// 参数:
//   - tx: 待签名的交易
//   - keys: 多个 Ed25519 私钥 (64 字节扩展私钥)
func SignTransactionWithMultipleKeys(tx *solana.Transaction, keys [][]byte) error {
	if len(keys) == 0 {
		return fmt.Errorf("no private keys provided")
	}

	// 创建私钥查找函数
	signers := make(map[solana.PublicKey]solana.PrivateKey)
	for _, keyBytes := range keys {
		if len(keyBytes) != 64 {
			return fmt.Errorf("invalid private key length: expected 64 bytes, got %d", len(keyBytes))
		}
		privKey := solana.PrivateKey(keyBytes)
		signers[privKey.PublicKey()] = privKey
	}

	// 签名交易
	_, err := tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if privKey, ok := signers[key]; ok {
			return &privKey
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to sign transaction: %w", err)
	}

	return nil
}

// SignTransactionWithKey 使用单个私钥签名交易（便捷方法）
// 支持热钱包二次签名场景
func SignTransactionWithKey(tx *solana.Transaction, privateKey solana.PrivateKey) error {
	_, err := tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(privateKey.PublicKey()) {
			return &privateKey
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to sign transaction: %w", err)
	}
	return nil
}

// GetTransactionFeeEstimate 估算交易费用
// 简单转账通常为 5000 lamports (0.000005 SOL)
func GetTransactionFeeEstimate() uint64 {
	return 5000 // lamports
}

// ValidateTransfer 验证转账参数
func ValidateTransfer(from, to string, amountLamports, balance uint64) error {
	// 验证地址格式
	fromPubKey, err := solana.PublicKeyFromBase58(from)
	if err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	if fromPubKey.IsZero() {
		return fmt.Errorf("from address cannot be zero")
	}

	toPubKey, err := solana.PublicKeyFromBase58(to)
	if err != nil {
		return fmt.Errorf("invalid to address: %w", err)
	}
	if toPubKey.IsZero() {
		return fmt.Errorf("to address cannot be zero")
	}

	// 验证地址不相同
	if fromPubKey.Equals(toPubKey) {
		return fmt.Errorf("from and to addresses cannot be the same")
	}

	// 验证金额
	if amountLamports == 0 {
		return fmt.Errorf("transfer amount must be greater than 0")
	}

	// 验证余额充足 (金额 + 手续费)
	requiredBalance := amountLamports + GetTransactionFeeEstimate()
	if balance < requiredBalance {
		return fmt.Errorf("insufficient balance: have %d lamports, need %d lamports (amount + fee)",
			balance, requiredBalance)
	}

	return nil
}

// ValidateSPLTokenTransfer 验证 SPL Token 转账参数
func ValidateSPLTokenTransfer(from, to, mint string, amount uint64, balance uint64) error {
	// 验证地址格式
	_, err := solana.PublicKeyFromBase58(from)
	if err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}

	_, err = solana.PublicKeyFromBase58(to)
	if err != nil {
		return fmt.Errorf("invalid to address: %w", err)
	}

	_, err = solana.PublicKeyFromBase58(mint)
	if err != nil {
		return fmt.Errorf("invalid mint address: %w", err)
	}

	// 验证金额
	if amount == 0 {
		return fmt.Errorf("transfer amount must be greater than 0")
	}

	// 验证余额充足
	if balance < amount {
		return fmt.Errorf("insufficient token balance: have %d, need %d", balance, amount)
	}

	return nil
}

// mustDecodeBase58 解码 base58，失败则 panic (仅用于已知正确的配置)
func mustDecodeBase58(s string) []byte {
	b, err := base58.Decode(s)
	if err != nil {
		panic(fmt.Sprintf("failed to decode base58: %v", err))
	}
	return b
}

// DecodeBase58PrivateKey 解码 Base58 编码的私钥
// 返回 64 字节扩展私钥
func DecodeBase58PrivateKey(base58Key string) ([]byte, error) {
	key, err := base58.Decode(base58Key)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base58 private key: %w", err)
	}
	if len(key) != 64 {
		return nil, fmt.Errorf("invalid private key length: expected 64 bytes, got %d", len(key))
	}
	return key, nil
}

// GetSignerPubkeys 获取交易的所有签名者公钥
func GetSignerPubkeys(tx *solana.Transaction) []solana.PublicKey {
	return tx.Message.AccountKeys
}

// IsFullySigned 检查交易是否已完整签名
func IsFullySigned(tx *solana.Transaction) bool {
	// 获取需要签名的账户数量
	numRequiredSignatures := int(tx.Message.Header.NumRequiredSignatures)
	
	// 检查签名数量是否足够
	return len(tx.Signatures) >= numRequiredSignatures
}
