// Package entity 定义DID领域实体
// COLA v5: Domain Layer
package entity

import (
	"fmt"
	"time"
)

// DIDStatus DID状态
type DIDStatus string

const (
	// DIDStatusActive 活跃状态
	DIDStatusActive DIDStatus = "active"
	// DIDStatusDisabled 禁用状态
	DIDStatusDisabled DIDStatus = "disabled"
	// DIDStatusRevoked 撤销状态
	DIDStatusRevoked DIDStatus = "revoked"
)

// UserType 用户类型
type UserType string

const (
	// UserTypeAgent Agent类型
	UserTypeAgent UserType = "agent"
	// UserTypeDeveloper 开发者类型
	UserTypeDeveloper UserType = "developer"
)

// DID 领域实体
// 封装DID的核心业务规则
type DID struct {
	ID            string            // 数据库ID
	DIDString     string            // did:solana:xxx
	PublicKey     string            // Base58公钥
	PrivateKey    string            // 服务端托管时：AES-GCM 加密后的 Base58 私钥；客户端自持钱包时为空
	WalletAddress string            // Solana钱包地址
	UserType      UserType          // 用户类型
	Status        DIDStatus         // 状态
	Config        map[string]string // 配置KV
	ConfigVersion int64             // 配置版本
	Metadata      map[string]string // 元数据
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// NewDID 创建新的DID实体
func NewDID(didString, publicKey, walletAddress string, userType UserType) *DID {
	now := time.Now()
	return &DID{
		DIDString:     didString,
		PublicKey:     publicKey,
		WalletAddress: walletAddress,
		UserType:      userType,
		Status:        DIDStatusActive,
		Config:        make(map[string]string),
		Metadata:      make(map[string]string),
		ConfigVersion: 1,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

// IsActive 检查DID是否活跃
func (d *DID) IsActive() bool {
	return d.Status == DIDStatusActive
}

// Disable 禁用DID
func (d *DID) Disable() {
	d.Status = DIDStatusDisabled
	d.UpdatedAt = time.Now()
}

// Revoke 撤销DID
func (d *DID) Revoke() {
	d.Status = DIDStatusRevoked
	d.UpdatedAt = time.Now()
}

// UpdateConfig 更新配置
func (d *DID) UpdateConfig(kv map[string]string) {
	for k, v := range kv {
		d.Config[k] = v
	}
	d.ConfigVersion++
	d.UpdatedAt = time.Now()
}

// GetConfig 获取配置值
func (d *DID) GetConfig(key string) (string, bool) {
	val, ok := d.Config[key]
	return val, ok
}

// ValidateDIDString 验证DID格式
// did:solana:base58publickey
func ValidateDIDString(did string) error {
	if len(did) < 15 {
		return fmt.Errorf("DID too short")
	}
	prefix := "did:solana:"
	if len(did) < len(prefix) || did[:len(prefix)] != prefix {
		return fmt.Errorf("invalid DID prefix, expected did:solana:")
	}
	return nil
}

// ExtractPublicKeyFromDID 从DID中提取公钥
func ExtractPublicKeyFromDID(did string) (string, error) {
	if err := ValidateDIDString(did); err != nil {
		return "", err
	}
	prefix := "did:solana:"
	return did[len(prefix):], nil
}

// GenerateDIDString 生成DID字符串
func GenerateDIDString(publicKey string) string {
	return fmt.Sprintf("did:solana:%s", publicKey)
}
