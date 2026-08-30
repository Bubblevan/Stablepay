// Package app 应用服务层
// COLA V5: Application Layer
// 协调领域对象完成用例
package app

import (
	"context" // 上下文包，用于控制请求生命周期
	"crypto/ed25519" // 标准库Ed25519加密包，用于签名验证
	"fmt" // 格式化包
	"strings"
	"sync" // 同步包，用于nonce缓存的并发安全
	"time" // 时间包，用于时间窗口验证

	"github.com/gagliardetto/solana-go" // Solana区块链SDK
	"github.com/mr-tron/base58" // Base58编解码库

	"github.com/stablepay/did-service/domain/entity" // 领域实体包
	"github.com/stablepay/did-service/domain/gateway" // 仓储接口包
	"github.com/stablepay/did-service/infrastructure/encryption" // 加密工具包
)

// nonceEntry Nonce缓存条目结构体
type nonceEntry struct {
	nonce     string // Nonce值
	timestamp time.Time // 缓存时间，用于过期清理
}

// DIDAppService DID应用服务结构体
type DIDAppService struct {
	repo        gateway.DIDRepository // DID仓储接口
	nonceCache  map[string]nonceEntry // Nonce缓存map，key为did+nonce组合
	nonceMu     sync.RWMutex // Nonce缓存的读写锁，保证并发安全
	encryptor   *encryption.AESEncryptor // AES加密器，用于私钥加密存储
}

// NewDIDAppService 创建应用服务实例的构造函数
// 参数 repo: DID仓储接口实现
// 参数 encryptionKey: AES加密密钥（16/24/32字节）
// 返回: 应用服务实例指针，错误信息
func NewDIDAppService(repo gateway.DIDRepository, encryptionKey string) (*DIDAppService, error) {
	// 创建加密器
	encryptor, err := encryption.NewAESEncryptor(encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("create encryptor failed: %w", err)
	}

	service := &DIDAppService{
		repo:       repo, // 注入仓储依赖
		nonceCache: make(map[string]nonceEntry), // 初始化nonce缓存map
		encryptor:  encryptor, // 注入加密器
	}
	// 启动后台goroutine清理过期nonce
	go service.cleanupExpiredNonces()
	return service, nil
}

// cleanupExpiredNonces 定期清理过期的nonce缓存
// 作为后台goroutine运行，每5分钟清理一次超过10分钟的nonce
func (s *DIDAppService) cleanupExpiredNonces() {
	ticker := time.NewTicker(5 * time.Minute) // 创建5分钟周期的ticker
	defer ticker.Stop() // 函数退出时停止ticker

	for range ticker.C { // 循环等待ticker信号
		s.nonceMu.Lock() // 加写锁
		now := time.Now()
		// 遍历所有缓存条目，删除超过10分钟的
		for key, entry := range s.nonceCache {
			if now.Sub(entry.timestamp) > 10*time.Minute { // 超过10分钟
				delete(s.nonceCache, key) // 从map中删除
			}
		}
		s.nonceMu.Unlock() // 释放写锁
	}
}

// CreateDIDCmd 创建DID命令结构体
type CreateDIDCmd struct {
	UserType UserType // 用户类型
	Metadata map[string]string // 元数据
}

// CreateDIDResult 创建DID结果结构体
type CreateDIDResult struct {
	DIDString     string // DID标识符
	PublicKey     string // 公钥（Base58）
	WalletAddress string // 钱包地址（与公钥相同）
	CreatedAt     string // 创建时间（RFC3339格式）
	// 注意：私钥不会返回给调用方，需要安全保存在客户端
}

// UserType 用户类型定义
type UserType string

const (
	UserTypeAgent     UserType = "agent"     // Agent类型常量
	UserTypeDeveloper UserType = "developer" // 开发者类型常量
)

// CreateDID 创建DID
// 1. 生成Ed25519密钥对
// 2. 构造did:solana:xxx
// 3. 使用AES-GCM加密私钥
// 4. 持久化存储
// 参数 ctx: 上下文
// 参数 cmd: 创建命令
// 返回: 创建结果，错误信息
func (s *DIDAppService) CreateDID(ctx context.Context, cmd *CreateDIDCmd) (*CreateDIDResult, error) {
	// 1. 生成密钥对
	account := solana.NewWallet() // 创建新的Solana钱包（自动生成密钥对）
	publicKey := account.PublicKey().String() // 获取公钥字符串
	privateKeyPlain := base58.Encode(account.PrivateKey) // Base58编码私钥（明文）

	// 2. 使用AES-GCM加密私钥
	privateKeyEncrypted, err := s.encryptor.EncryptString(privateKeyPlain)
	if err != nil {
		return nil, fmt.Errorf("encrypt private key failed: %w", err)
	}

	// 3. 构造DID字符串
	didString := entity.GenerateDIDString(publicKey)

	// 4. 检查DID是否已存在
	exists, err := s.repo.Exists(ctx, didString)
	if err != nil {
		return nil, fmt.Errorf("check did exists failed: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("did already exists: %s", didString)
	}

	// 5. 创建领域实体
	userType := entity.UserType(cmd.UserType)
	if userType == "" {
		userType = entity.UserTypeAgent // 默认为Agent类型
	}

	did := entity.NewDID(didString, publicKey, publicKey, userType)
	did.PrivateKey = privateKeyEncrypted // 存储加密后的私钥

	// 复制元数据
	if cmd.Metadata != nil {
		for k, v := range cmd.Metadata {
			did.Metadata[k] = v
		}
	}

	// 6. 持久化
	if err := s.repo.Save(ctx, did); err != nil {
		return nil, fmt.Errorf("save did failed: %w", err)
	}

	return &CreateDIDResult{
		DIDString:     didString,
		PublicKey:     publicKey,
		WalletAddress: publicKey,
		CreatedAt:     did.CreatedAt.Format(time.RFC3339),
		// 注意：私钥不返回，应由客户端本地安全保存
	}, nil
}

// RegisterDIDCmd 绑定客户端已有 Solana 公钥（OWS 等），服务端不生成、不存储私钥。
type RegisterDIDCmd struct {
	UserType      UserType
	PublicKey     string
	WalletAddress string
	WalletID      string
	WalletName    string
	Metadata      map[string]string
}

// RegisterDIDResult 与 CreateDIDResult 对外字段一致，便于网关统一封装。
type RegisterDIDResult = CreateDIDResult

// RegisterDID 登记 did:solana:{canonical_pubkey}，验签仅依赖入库的公钥，与客户端本地私钥一致。
func (s *DIDAppService) RegisterDID(ctx context.Context, cmd *RegisterDIDCmd) (*RegisterDIDResult, error) {
	if cmd == nil {
		return nil, fmt.Errorf("command is nil")
	}
	pubIn := strings.TrimSpace(cmd.PublicKey)
	if pubIn == "" {
		return nil, fmt.Errorf("public_key is required")
	}
	pk, err := solana.PublicKeyFromBase58(pubIn)
	if err != nil {
		return nil, fmt.Errorf("invalid public_key: %w", err)
	}
	canonical := pk.String()
	walIn := strings.TrimSpace(cmd.WalletAddress)
	if walIn == "" {
		walIn = canonical
	}
	if walIn != canonical {
		return nil, fmt.Errorf("wallet_address must match canonical Solana public key")
	}

	didString := entity.GenerateDIDString(canonical)
	exists, err := s.repo.Exists(ctx, didString)
	if err != nil {
		return nil, fmt.Errorf("check did exists failed: %w", err)
	}
	if exists {
		// 幂等：同一公钥重复登记视为成功，便于客户端重放初始化而不清空本地 state。
		existing, err := s.repo.FindByDID(ctx, didString)
		if err != nil {
			return nil, fmt.Errorf("did already exists: %s (reload failed: %w)", didString, err)
		}
		if existing.PublicKey != canonical || existing.WalletAddress != canonical {
			return nil, fmt.Errorf("did already exists with mismatched wallet keys: %s", didString)
		}
		if !existing.IsActive() {
			return nil, fmt.Errorf("did already exists but is not active: %s", didString)
		}
		return &RegisterDIDResult{
			DIDString:     existing.DIDString,
			PublicKey:     existing.PublicKey,
			WalletAddress: existing.WalletAddress,
			CreatedAt:     existing.CreatedAt.Format(time.RFC3339),
		}, nil
	}

	userType := entity.UserType(cmd.UserType)
	if userType == "" {
		userType = entity.UserTypeAgent
	}

	did := entity.NewDID(didString, canonical, canonical, userType)
	// 客户端持有私钥；服务端不落库加密私钥（空串表示 client-held）
	did.PrivateKey = ""

	meta := make(map[string]string)
	if cmd.Metadata != nil {
		for k, v := range cmd.Metadata {
			meta[k] = v
		}
	}
	if wid := strings.TrimSpace(cmd.WalletID); wid != "" {
		meta["wallet_id"] = wid
	}
	if wn := strings.TrimSpace(cmd.WalletName); wn != "" {
		meta["wallet_name"] = wn
	}
	for k, v := range meta {
		did.Metadata[k] = v
	}

	if err := s.repo.Save(ctx, did); err != nil {
		return nil, fmt.Errorf("save did failed: %w", err)
	}

	return &RegisterDIDResult{
		DIDString:     didString,
		PublicKey:     canonical,
		WalletAddress: canonical,
		CreatedAt:     did.CreatedAt.Format(time.RFC3339),
	}, nil
}

// GetPrivateKey 获取解密后的私钥（用于签名操作）
// 参数 ctx: 上下文
// 参数 didString: DID标识符
// 返回: 解密后的Base58私钥，错误信息
func (s *DIDAppService) GetPrivateKey(ctx context.Context, didString string) (string, error) {
	// 1. 查找DID
	did, err := s.repo.FindByDID(ctx, didString)
	if err != nil {
		return "", fmt.Errorf("find did failed: %w", err)
	}
	if did == nil {
		return "", fmt.Errorf("did not found: %s", didString)
	}

	// 2. 检查DID状态
	if !did.IsActive() {
		return "", fmt.Errorf("did is not active: %s", didString)
	}

	if strings.TrimSpace(did.PrivateKey) == "" {
		return "", fmt.Errorf("private key is not stored on server (client-held wallet)")
	}

	// 3. 解密私钥
	privateKeyPlain, err := s.encryptor.DecryptString(did.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("decrypt private key failed: %w", err)
	}

	return privateKeyPlain, nil
}

// GetDIDQuery 查询DID查询结构体
type GetDIDQuery struct {
	DIDString string // DID标识符
}

// GetDIDResult 查询DID结果结构体
type GetDIDResult struct {
	DIDString     string            // DID标识符
	PublicKey     string            // 公钥
	WalletAddress string            // 钱包地址
	Status        string            // 状态
	UserType      string            // 用户类型
	Metadata      map[string]string // 元数据
	// 注意：私钥字段不返回，确保安全
}

// GetDID 查询DID
// 参数 ctx: 上下文
// 参数 query: 查询条件
// 返回: 查询结果，错误信息
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
		// 注意：私钥不返回
	}, nil
}

// VerifySignatureCmd 验证签名命令结构体
type VerifySignatureCmd struct {
	DIDString string // DID标识符
	Message   string // 原始消息内容
	Signature string // 签名值（Base58编码）
	Timestamp string // 时间戳（RFC3339格式）
	Nonce     string // 随机数，防止重放攻击
}

// VerifySignatureResult 验证签名结果结构体
type VerifySignatureResult struct {
	Valid bool // 签名是否有效
}

// VerifySignature 验证Ed25519签名
// 完整验证流程：
// 1. 查找DID并验证状态
// 2. 解析公钥和签名
// 3. 时间窗口验证（±5分钟）
// 4. Nonce防重放检查
// 5. Ed25519签名验证
// 参数 ctx: 上下文
// 参数 cmd: 验证命令
// 返回: 验证结果，错误信息
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

	// 3. 时间窗口验证（±5分钟），防止重放攻击
	if !s.verifyTimestampWindow(cmd.Timestamp) {
		return &VerifySignatureResult{Valid: false}, nil
	}

	// 4. Nonce防重放检查
	if !s.verifyNonce(cmd.DIDString, cmd.Nonce) {
		return &VerifySignatureResult{Valid: false}, nil
	}

	// 5. 解析公钥（Base58解码）
	pubKeyBytes, err := base58.Decode(did.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("decode public key failed: %w", err)
	}
	// 验证公钥长度（Ed25519公钥必须是32字节）
	if len(pubKeyBytes) != ed25519.PublicKeySize {
		return &VerifySignatureResult{Valid: false}, nil
	}

	// 6. 解析签名（Base58解码）
	sigBytes, err := base58.Decode(cmd.Signature)
	if err != nil {
		return &VerifySignatureResult{Valid: false}, nil
	}
	// 验证签名长度（Ed25519签名必须是64字节）
	if len(sigBytes) != ed25519.SignatureSize {
		return &VerifySignatureResult{Valid: false}, nil
	}

	// 7. 构造签名数据（message + timestamp + nonce）
	signData := fmt.Sprintf("%s%s%s", cmd.Message, cmd.Timestamp, cmd.Nonce)

	// 8. 执行Ed25519签名验证
	valid := ed25519.Verify(pubKeyBytes, []byte(signData), sigBytes)

	// 9. 如果验证成功，记录nonce（防止重复使用）
	if valid {
		s.recordNonce(cmd.DIDString, cmd.Nonce)
	}

	return &VerifySignatureResult{Valid: valid}, nil
}

// verifyTimestampWindow 验证时间戳是否在允许窗口内（±5分钟）
// 参数 timestampStr: RFC3339格式的时间戳字符串
// 返回: 是否在有效窗口内
func (s *DIDAppService) verifyTimestampWindow(timestampStr string) bool {
	timestamp, err := time.Parse(time.RFC3339, timestampStr) // 解析时间戳
	if err != nil {
		return false // 解析失败视为无效
	}

	now := time.Now() // 获取当前时间
	diff := now.Sub(timestamp) // 计算时间差
	if diff < 0 {
		diff = -diff // 取绝对值
	}

	// 允许5分钟的时间窗口
	return diff <= 5*time.Minute
}

// verifyNonce 验证nonce是否已被使用过
// 参数 did: DID标识符
// 参数 nonce: 随机数
// 返回: 是否有效（未使用过返回true）
func (s *DIDAppService) verifyNonce(did, nonce string) bool {
	key := did + ":" + nonce // 构建复合key

	s.nonceMu.RLock() // 加读锁
	defer s.nonceMu.RUnlock() // 函数退出时释放读锁

	_, exists := s.nonceCache[key] // 检查key是否存在于缓存中
	return !exists // 不存在表示未使用过，返回true
}

// recordNonce 记录已使用的nonce
// 参数 did: DID标识符
// 参数 nonce: 随机数
func (s *DIDAppService) recordNonce(did, nonce string) {
	key := did + ":" + nonce // 构建复合key

	s.nonceMu.Lock() // 加写锁
	defer s.nonceMu.Unlock() // 函数退出时释放写锁

	s.nonceCache[key] = nonceEntry{ // 写入缓存
		nonce:     nonce,
		timestamp: time.Now(), // 记录当前时间，用于后续过期清理
	}
}

// UpdateConfigCmd 更新配置命令结构体
type UpdateConfigCmd struct {
	DIDString     string            // DID标识符
	ConfigVersion int64             // 当前配置版本（乐观锁）
	ConfigKV      map[string]string // 配置键值对
}

// UpdateConfigResult 更新配置结果结构体
type UpdateConfigResult struct {
	NewConfigVersion int64 // 新的配置版本号
}

// UpdateConfig 更新DID配置
// 参数 ctx: 上下文
// 参数 cmd: 更新命令
// 返回: 更新结果，错误信息
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
