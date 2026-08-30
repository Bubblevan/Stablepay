// Package encryption 提供加密解密工具
// COLA V5: Infrastructure Layer
// 用于敏感数据（如私钥）的加密存储
package encryption

import (
	"crypto/aes" // AES加密标准库
	"crypto/cipher" // 密码学接口
	"crypto/rand" // 随机数生成
	"encoding/base64" // Base64编解码
	"fmt" // 格式化包
	"io" // IO工具
)

// AESEncryptor AES加密器结构体
type AESEncryptor struct {
	key []byte // AES密钥，必须是16/24/32字节（对应AES-128/192/256）
}

// NewAESEncryptor 创建AES加密器
// 参数 key: 加密密钥，长度必须是16、24或32字节
// 返回: 加密器实例，错误信息
func NewAESEncryptor(key string) (*AESEncryptor, error) {
	keyBytes := []byte(key) // 将字符串转为字节数组
	// 验证密钥长度
	if len(keyBytes) != 16 && len(keyBytes) != 24 && len(keyBytes) != 32 {
		return nil, fmt.Errorf("key length must be 16, 24 or 32 bytes, got %d", len(keyBytes))
	}
	return &AESEncryptor{key: keyBytes}, nil
}

// Encrypt 加密数据（AES-GCM模式）
// 参数 plaintext: 明文数据
// 返回: Base64编码的密文，错误信息
func (e *AESEncryptor) Encrypt(plaintext []byte) (string, error) {
	// 创建AES密码块
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", fmt.Errorf("create cipher failed: %w", err)
	}

	// 创建GCM模式（Galois/Counter Mode，提供认证加密）
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm failed: %w", err)
	}

	// 生成随机nonce（Number used once，每次加密必须唯一）
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce failed: %w", err)
	}

	// 加密并附加认证标签
	// Seal方法将nonce作为additional authenticated data (AAD)
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)

	// Base64编码便于存储和传输
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt 解密数据（AES-GCM模式）
// 参数 ciphertext: Base64编码的密文
// 返回: 明文数据，错误信息
func (e *AESEncryptor) Decrypt(ciphertext string) ([]byte, error) {
	// Base64解码
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decode base64 failed: %w", err)
	}

	// 创建AES密码块
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, fmt.Errorf("create cipher failed: %w", err)
	}

	// 创建GCM模式
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm failed: %w", err)
	}

	// 提取nonce（密文前gcm.NonceSize()字节）
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]

	// 解密并验证认证标签
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt failed: %w", err)
	}

	return plaintext, nil
}

// EncryptString 加密字符串（便捷方法）
// 参数 plaintext: 明文字符串
// 返回: Base64编码的密文，错误信息
func (e *AESEncryptor) EncryptString(plaintext string) (string, error) {
	return e.Encrypt([]byte(plaintext))
}

// DecryptString 解密为字符串（便捷方法）
// 参数 ciphertext: Base64编码的密文
// 返回: 明文字符串，错误信息
func (e *AESEncryptor) DecryptString(ciphertext string) (string, error) {
	plaintext, err := e.Decrypt(ciphertext)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
