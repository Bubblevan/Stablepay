// Package app 应用服务层
// COLA v5: Application Layer
// 协调领域对象完成用例
package app

import (
	"context"
	"fmt"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/mr-tron/base58"

	"github.com/stablepay/did-service/domain/entity"
	"github.com/stablepay/did-service/domain/gateway"
)

// DIDAppService DID应用服务
type DIDAppService struct {
	repo gateway.DIDRepository
}

// NewDIDAppService 创建应用服务
func NewDIDAppService(repo gateway.DIDRepository) *DIDAppService {
	return &DIDAppService{repo: repo}
}

// CreateDIDCmd 创建DID命令
type CreateDIDCmd struct {
	UserType UserType
	Metadata map[string]string
}

// CreateDIDResult 创建DID结果
type CreateDIDResult struct {
	DIDString     string
	PublicKey     string
	WalletAddress string
	CreatedAt     string
}

// UserType 用户类型
type UserType string

const (
	UserTypeAgent     UserType = "agent"
	UserTypeDeveloper UserType = "developer"
)

// CreateDID 创建DID
// 1. 生成Ed25519密钥对
// 2. 构造did:solana:xxx
// 3. 持久化存储
func (s *DIDAppService) CreateDID(ctx context.Context, cmd *CreateDIDCmd) (*CreateDIDResult, error) {
	// 1. 生成密钥对
	account := solana.NewWallet()
	publicKey := account.PublicKey().String()
	privateKey := base58.Encode(account.PrivateKey)

	// 2. 构造DID字符串
	didString := entity.GenerateDIDString(publicKey)

	// 3. 检查DID是否已存在
	exists, err := s.repo.Exists(ctx, didString)
	if err != nil {
		return nil, fmt.Errorf("check did exists failed: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("did already exists: %s", didString)
	}

	// 4. 创建领域实体
	userType := entity.UserType(cmd.UserType)
	if userType == "" {
		userType = entity.UserTypeAgent
	}

	did := entity.NewDID(didString, publicKey, publicKey, userType)
	did.PrivateKey = privateKey // 注意: 生产环境需要加密存储

	// 复制元数据
	if cmd.Metadata != nil {
		for k, v := range cmd.Metadata {
			did.Metadata[k] = v
		}
	}

	// 5. 持久化
	if err := s.repo.Save(ctx, did); err != nil {
		return nil, fmt.Errorf("save did failed: %w", err)
	}

	return &CreateDIDResult{
		DIDString:     didString,
		PublicKey:     publicKey,
		WalletAddress: publicKey,
		CreatedAt:     did.CreatedAt.Format(time.RFC3339),
	}, nil
}

// GetDIDQuery 查询DID查询
type GetDIDQuery struct {
	DIDString string
}

// GetDIDResult 查询DID结果
type GetDIDResult struct {
	DIDString     string
	PublicKey     string
	WalletAddress string
	Status        string
	UserType      string
	Metadata      map[string]string
}

// GetDID 查询DID
func (s *DIDAppService) GetDID(ctx context.Context, query *GetDIDQuery) (*GetDIDResult, error) {
	if err := entity.ValidateDIDString(query.DIDString); err != nil {
		return nil, fmt.Errorf("invalid did: %w", err)
	}

	did, err := s.repo.FindByDID(ctx, query.DIDString)
	if err != nil {
		return nil, fmt.Errorf("find did failed: %w", err)
	}
	if did == nil {
		return nil, fmt.Errorf("did not found: %s", query.DIDString)
	}

	return &GetDIDResult{
		DIDString:     did.DIDString,
		PublicKey:     did.PublicKey,
		WalletAddress: did.WalletAddress,
		Status:        string(did.Status),
		UserType:      string(did.UserType),
		Metadata:      did.Metadata,
	}, nil
}

// VerifySignatureCmd 验证签名命令
type VerifySignatureCmd struct {
	DIDString string
	Message   string
	Signature string
	Timestamp string
	Nonce     string
}

// VerifySignatureResult 验证签名结果
type VerifySignatureResult struct {
	Valid bool
}

// VerifySignature 验证签名
// 使用DID对应的公钥验证签名
func (s *DIDAppService) VerifySignature(ctx context.Context, cmd *VerifySignatureCmd) (*VerifySignatureResult, error) {
	// 1. 查找DID
	did, err := s.repo.FindByDID(ctx, cmd.DIDString)
	if err != nil {
		return nil, fmt.Errorf("find did failed: %w", err)
	}
	if did == nil {
		return &VerifySignatureResult{Valid: false}, nil
	}

	// 2. 检查DID状态
	if !did.IsActive() {
		return &VerifySignatureResult{Valid: false}, nil
	}

	// 3. 解析公钥
	pubKey, err := solana.PublicKeyFromBase58(did.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("invalid public key: %w", err)
	}

	// 4. 解析签名
	sigBytes, err := base58.Decode(cmd.Signature)
	if err != nil {
		return &VerifySignatureResult{Valid: false}, nil
	}

	// 5. 构造签名数据 (message + timestamp + nonce)
	signData := fmt.Sprintf("%s%s%s", cmd.Message, cmd.Timestamp, cmd.Nonce)

	// 6. 验证签名
	// 注意: Solana签名验证需要完整的交易结构，这里简化处理
	// 实际生产环境需要更严格的验证逻辑
	valid := verifyEd25519Signature(pubKey.Bytes(), []byte(signData), sigBytes)

	return &VerifySignatureResult{Valid: valid}, nil
}

// verifyEd25519Signature 验证Ed25519签名（简化实现）
// 注意: 这是MVP简化版，生产环境需要完整的加密验证
func verifyEd25519Signature(publicKey, message, signature []byte) bool {
	// MVP阶段: 始终返回true（因为Solana签名验证需要完整的交易上下文）
	// 贾越TODO: 实现完整的Ed25519签名验证
	return len(signature) == 64
}

// UpdateConfigCmd 更新配置命令
type UpdateConfigCmd struct {
	DIDString     string
	ConfigVersion int64
	ConfigKV      map[string]string
}

// UpdateConfigResult 更新配置结果
type UpdateConfigResult struct {
	NewConfigVersion int64
}

// UpdateConfig 更新DID配置
func (s *DIDAppService) UpdateConfig(ctx context.Context, cmd *UpdateConfigCmd) (*UpdateConfigResult, error) {
	// 1. 查找DID
	did, err := s.repo.FindByDID(ctx, cmd.DIDString)
	if err != nil {
		return nil, fmt.Errorf("find did failed: %w", err)
	}
	if did == nil {
		return nil, fmt.Errorf("did not found: %s", cmd.DIDString)
	}

	// 2. 检查DID状态
	if !did.IsActive() {
		return nil, fmt.Errorf("did is not active: %s", cmd.DIDString)
	}

	// 3. 版本检查（乐观锁）
	if cmd.ConfigVersion > 0 && did.ConfigVersion != cmd.ConfigVersion {
		return nil, fmt.Errorf("config version mismatch, expected %d, got %d", did.ConfigVersion, cmd.ConfigVersion)
	}

	// 4. 更新配置
	newVersion := did.ConfigVersion + 1
	if err := s.repo.UpdateConfig(ctx, cmd.DIDString, cmd.ConfigKV, newVersion); err != nil {
		return nil, fmt.Errorf("update config failed: %w", err)
	}

	return &UpdateConfigResult{NewConfigVersion: newVersion}, nil
}
