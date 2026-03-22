// Package repository 提供DID仓储的MySQL实现
// COLA V5: Infrastructure Layer
// 使用GORM作为ORM框架
package repository

import (
	"context"       // 上下文包，用于控制请求生命周期和超时
	"encoding/json" // JSON序列化/反序列化包
	"errors"        // 错误处理包
	"fmt"           // 格式化包
	"time"          // 时间处理包

	"github.com/stablepay/did-service/domain/entity"  // 领域实体包
	"github.com/stablepay/did-service/domain/gateway" // 仓储接口定义包

	"gorm.io/gorm" // GORM主包，Go流行的ORM框架
)

// DidIdentityModel GORM数据库模型结构体
// 对应数据库表 did_identities，用于数据库操作
// 首字母大写表示导出，供外部包（如main）使用AutoMigrate
type DidIdentityModel struct {
	ID            uint64    `gorm:"column:id;primaryKey;autoIncrement"`                     // 自增主键ID，数据库自动生成
	DID           string    `gorm:"column:did;type:varchar(255);uniqueIndex;not null"`      // DID标识符，唯一索引，不能为空
	PublicKey     string    `gorm:"column:public_key;type:varchar(255);not null"`           // Base58编码的公钥，不能为空
	PrivateKey    string    `gorm:"column:private_key;type:varchar(255)"`                   // AES-GCM加密后的私钥（Base64编码）
	WalletAddress string    `gorm:"column:wallet_address;type:varchar(255);index;not null"` // Solana钱包地址，普通索引便于查询
	UserType      int8      `gorm:"column:user_type;type:tinyint;not null;default:1"`       // 用户类型：1=agent, 2=developer
	Status        int8      `gorm:"column:status;type:tinyint;not null;default:1"`          // 状态：1=active, 2=disabled, 3=revoked
	ConfigVersion int64     `gorm:"column:config_version;type:bigint;not null;default:1"`   // 配置版本号，用于乐观锁
	Metadata      string    `gorm:"column:metadata;type:text"`                              // 元数据JSON字符串存储
	CreatedAt     time.Time `gorm:"column:created_at;type:timestamp;not null"`              // 创建时间
	UpdatedAt     time.Time `gorm:"column:updated_at;type:timestamp;not null"`              // 更新时间
}

// TableName 指定GORM使用的数据库表名
// GORM默认使用结构体名的复数形式，这里显式指定确保表名正确
func (DidIdentityModel) TableName() string {
	return "did_identities" // 返回表名字符串
}

// DBDIDRepository MySQL实现的DID仓储结构体
// 实现了 gateway.DIDRepository 接口
type DBDIDRepository struct {
	db *gorm.DB // GORM数据库连接实例，所有数据库操作都通过它进行
}

// NewDBDIDRepository 创建MySQL仓储实例的构造函数
// 参数 db: GORM数据库连接，由调用方初始化并传入
// 返回: 实现了 gateway.DIDRepository 接口的实例
func NewDBDIDRepository(db *gorm.DB) gateway.DIDRepository {
	return &DBDIDRepository{ // 返回结构体指针
		db: db, // 保存数据库连接引用
	}
}

// Save 将DID实体保存到MySQL数据库
// 参数 ctx: 上下文，用于控制超时和取消
// 参数 did: 要保存的领域实体
// 返回: 错误信息，成功返回nil
func (r *DBDIDRepository) Save(ctx context.Context, did *entity.DID) error {
	metadataJSON, err := json.Marshal(did.Metadata) // 将元数据map序列化为JSON字节数组
	if err != nil {                                 // 检查序列化是否出错
		return fmt.Errorf("marshal metadata failed: %w", err) // 包装错误信息返回
	}

	model := DidIdentityModel{ // 创建数据库模型实例
		DID:           did.DIDString,                        // 复制DID字符串
		PublicKey:     did.PublicKey,                        // 复制公钥
		PrivateKey:    did.PrivateKey,                       // 复制加密后的私钥
		WalletAddress: did.WalletAddress,                    // 复制钱包地址
		UserType:      int8(mapUserTypeToInt(did.UserType)), // 将领域层UserType转换为数据库整数
		Status:        int8(mapStatusToInt(did.Status)),     // 将领域层Status转换为数据库整数
		ConfigVersion: did.ConfigVersion,                    // 复制配置版本号
		Metadata:      string(metadataJSON),                 // 将JSON字节数组转为字符串存储
		CreatedAt:     did.CreatedAt,                        // 复制创建时间
		UpdatedAt:     did.UpdatedAt,                        // 复制更新时间
	}

	result := r.db.WithContext(ctx).Create(&model) // 使用上下文创建记录，GORM的Create方法执行INSERT
	if result.Error != nil {                       // 检查数据库操作是否出错
		if errors.Is(result.Error, gorm.ErrDuplicatedKey) { // 判断是否为唯一键冲突错误
			return fmt.Errorf("did already exists: %s", did.DIDString) // 返回已存在错误
		}
		return fmt.Errorf("save did failed: %w", result.Error) // 包装其他数据库错误
	}

	did.ID = fmt.Sprintf("%d", model.ID) // 将数据库生成的自增ID回填到领域实体
	return nil                           // 保存成功，返回nil
}

// FindByDID 根据DID字符串查询身份
// 参数 ctx: 上下文
// 参数 didString: DID标识符，格式为 did:solana:xxx
// 返回: 找到的DID实体，未找到返回nil；错误信息
func (r *DBDIDRepository) FindByDID(ctx context.Context, didString string) (*entity.DID, error) {
	var model DidIdentityModel // 声明模型变量用于接收查询结果

	result := r.db.WithContext(ctx). // 使用上下文开始查询
						Where("did = ?", didString). // 添加WHERE条件：did字段等于参数
						First(&model)                // 执行查询，将结果存入model变量，LIMIT 1

	if result.Error != nil { // 检查查询是否出错
		if errors.Is(result.Error, gorm.ErrRecordNotFound) { // 判断是否为记录不存在错误
			return nil, nil // 保持与内存实现一致，未找到返回nil, nil
		}
		return nil, fmt.Errorf("find did failed: %w", result.Error) // 包装其他错误
	}

	return modelToEntity(&model), nil // 将数据库模型转换为领域实体返回
}

// FindByWalletAddress 根据钱包地址查询DID
// 参数 ctx: 上下文
// 参数 walletAddress: Solana钱包地址
// 返回: 找到的DID实体；错误信息
func (r *DBDIDRepository) FindByWalletAddress(ctx context.Context, walletAddress string) (*entity.DID, error) {
	var model DidIdentityModel // 声明模型变量

	result := r.db.WithContext(ctx). // 使用上下文
						Where("wallet_address = ?", walletAddress). // WHERE条件：钱包地址匹配
						First(&model)                               // 查询第一条匹配记录

	if result.Error != nil { // 检查错误
		if errors.Is(result.Error, gorm.ErrRecordNotFound) { // 记录不存在
			return nil, nil // 返回nil, nil表示未找到
		}
		return nil, fmt.Errorf("find did by wallet failed: %w", result.Error) // 其他错误
	}

	return modelToEntity(&model), nil // 转换并返回
}

// Update 全量更新DID实体
// 参数 ctx: 上下文
// 参数 did: 包含新值的DID实体
// 返回: 错误信息
func (r *DBDIDRepository) Update(ctx context.Context, did *entity.DID) error {
	metadataJSON, err := json.Marshal(did.Metadata) // 序列化元数据为JSON
	if err != nil {                                 // 序列化失败
		return fmt.Errorf("marshal metadata failed: %w", err) // 返回错误
	}

	updates := map[string]interface{}{ // 构建需要更新的字段map
		"public_key":     did.PublicKey,                  // 更新公钥
		"private_key":    did.PrivateKey,                 // 更新加密后的私钥
		"wallet_address": did.WalletAddress,              // 更新钱包地址
		"user_type":      mapUserTypeToInt(did.UserType), // 转换用户类型
		"status":         mapStatusToInt(did.Status),     // 转换状态
		"config_version": did.ConfigVersion,              // 更新配置版本
		"metadata":       string(metadataJSON),           // 更新元数据JSON
		"updated_at":     time.Now(),                     // 设置当前时间为更新时间
	}

	result := r.db.WithContext(ctx). // 使用上下文
						Model(&DidIdentityModel{}).      // 指定操作的数据库模型
						Where("did = ?", did.DIDString). // WHERE条件：匹配DID
						Updates(updates)                 // 执行UPDATE，只更新非零值字段

	if result.Error != nil { // 检查更新错误
		return fmt.Errorf("update did failed: %w", result.Error) // 返回错误
	}

	if result.RowsAffected == 0 { // 检查是否有记录被更新
		return fmt.Errorf("did not found: %s", did.DIDString) // 没有匹配记录
	}

	return nil // 更新成功
}

// UpdateStatus 更新DID状态
// 参数 ctx: 上下文
// 参数 didString: DID标识符
// 参数 status: 新状态
// 返回: 错误信息
func (r *DBDIDRepository) UpdateStatus(ctx context.Context, didString string, status entity.DIDStatus) error {
	result := r.db.WithContext(ctx). // 使用上下文
						Model(&DidIdentityModel{}).              // 指定模型
						Where("did = ?", didString).             // WHERE条件
						Update("status", mapStatusToInt(status)) // 只更新status字段

	if result.Error != nil { // 检查错误
		return fmt.Errorf("update status failed: %w", result.Error) // 返回错误
	}

	if result.RowsAffected == 0 { // 没有记录被更新
		return fmt.Errorf("did not found: %s", didString) // DID不存在
	}

	return nil // 成功
}

// UpdateConfig 更新DID配置，使用乐观锁防止并发冲突
// 参数 ctx: 上下文
// 参数 didString: DID标识符
// 参数 config: 新的配置键值对
// 参数 newVersion: 新版本号（应该是当前版本+1）
// 返回: 错误信息
func (r *DBDIDRepository) UpdateConfig(ctx context.Context, didString string, config map[string]string, newVersion int64) error {
	configJSON, err := json.Marshal(config) // 将配置map序列化为JSON字符串
	if err != nil {                         // 序列化失败
		return fmt.Errorf("marshal config failed: %w", err) // 返回错误
	}

	expectedOldVersion := newVersion - 1 // 计算期望的旧版本号（乐观锁检查）

	result := r.db.WithContext(ctx). // 使用上下文
						Model(&DidIdentityModel{}).                                             // 指定模型
						Where("did = ? AND config_version = ?", didString, expectedOldVersion). // WHERE带版本号条件（乐观锁）
						Updates(map[string]interface{}{                                         // 更新的字段
			"config_version": newVersion,         // 设置新版本号
			"metadata":       string(configJSON), // 存储到metadata字段（或单独config字段）
			"updated_at":     time.Now(),         // 更新时间
		})

	if result.Error != nil { // 数据库错误
		return fmt.Errorf("update config failed: %w", result.Error) // 返回错误
	}

	if result.RowsAffected == 0 { // 没有记录被更新，可能是版本冲突或DID不存在
		// 查询确认是哪种情况
		exists, _ := r.Exists(ctx, didString) // 检查DID是否存在
		if !exists {                          // DID不存在
			return fmt.Errorf("did not found: %s", didString) // 返回不存在错误
		}
		return fmt.Errorf("config version mismatch, expected %d", expectedOldVersion) // 乐观锁冲突
	}

	return nil // 更新成功
}

// List 分页查询DID列表
// 参数 ctx: 上下文
// 参数 limit: 返回最大数量
// 参数 offset: 偏移量（用于分页）
// 返回: DID实体列表；错误信息
func (r *DBDIDRepository) List(ctx context.Context, limit, offset int) ([]*entity.DID, error) {
	var models []DidIdentityModel // 声明切片接收查询结果

	result := r.db.WithContext(ctx). // 使用上下文
						Order("created_at DESC"). // 按创建时间降序排列（最新的在前）
						Limit(limit).             // 限制返回数量
						Offset(offset).           // 设置偏移量
						Find(&models)             // 执行查询，Find用于查询多条记录

	if result.Error != nil { // 检查错误
		return nil, fmt.Errorf("list did failed: %w", result.Error) // 返回错误
	}

	dids := make([]*entity.DID, 0, len(models)) // 创建结果切片，预分配容量
	for i := range models {                     // 遍历查询结果
		dids = append(dids, modelToEntity(&models[i])) // 转换每个模型为实体并追加
	}

	return dids, nil // 返回转换后的列表
}

// Exists 检查DID是否存在
// 参数 ctx: 上下文
// 参数 didString: DID标识符
// 返回: 是否存在；错误信息
func (r *DBDIDRepository) Exists(ctx context.Context, didString string) (bool, error) {
	var count int64 // 声明计数变量

	result := r.db.WithContext(ctx). // 使用上下文
						Model(&DidIdentityModel{}).  // 指定模型
						Where("did = ?", didString). // WHERE条件
						Count(&count)                // 执行COUNT查询，结果存入count变量

	if result.Error != nil { // 检查错误
		return false, fmt.Errorf("check exists failed: %w", result.Error) // 返回错误
	}

	return count > 0, nil // count>0表示存在，返回true
}

// modelToEntity 将数据库模型转换为领域实体
// 参数 m: 数据库模型指针
// 返回: 领域实体指针
func modelToEntity(m *DidIdentityModel) *entity.DID {
	var metadata map[string]string // 声明元数据map
	if m.Metadata != "" {          // 如果metadata字段不为空
		_ = json.Unmarshal([]byte(m.Metadata), &metadata) // 反序列化JSON到map（忽略错误，失败时metadata为nil）
	}
	if metadata == nil { // 如果解析后仍为nil
		metadata = make(map[string]string) // 初始化为空map避免nil指针
	}

	return &entity.DID{ // 创建并返回领域实体
		ID:            fmt.Sprintf("%d", m.ID),           // 将数字ID转为字符串
		DIDString:     m.DID,                             // 复制DID
		PublicKey:     m.PublicKey,                       // 复制公钥
		PrivateKey:    m.PrivateKey,                      // 复制加密后的私钥
		WalletAddress: m.WalletAddress,                   // 复制钱包地址
		UserType:      mapIntToUserType(int(m.UserType)), // 转换用户类型
		Status:        mapIntToStatus(int(m.Status)),     // 转换状态
		ConfigVersion: m.ConfigVersion,                   // 复制配置版本
		Metadata:      metadata,                          // 设置元数据map
		Config:        make(map[string]string),           // 初始化配置map（可从单独表加载）
		CreatedAt:     m.CreatedAt,                       // 复制创建时间
		UpdatedAt:     m.UpdatedAt,                       // 复制更新时间
	}
}

// mapUserTypeToInt 将领域层的UserType枚举转换为数据库存储的整数
// 参数 ut: 领域层的UserType
// 返回: 对应的整数值
func mapUserTypeToInt(ut entity.UserType) int {
	switch ut { // 判断用户类型
	case entity.UserTypeDeveloper: // 如果是开发者
		return 2 // 返回2
	case entity.UserTypeAgent: // 如果是Agent
		return 1 // 返回1
	default: // 其他情况
		return 1 // 默认返回1（Agent）
	}
}

// mapIntToUserType 将数据库整数转换为领域层的UserType枚举
// 参数 i: 数据库中存储的整数值
// 返回: 领域层的UserType
func mapIntToUserType(i int) entity.UserType {
	switch i { // 判断整数值
	case 2: // 如果是2
		return entity.UserTypeDeveloper // 返回开发者类型
	default: // 其他情况（包括1）
		return entity.UserTypeAgent // 返回Agent类型
	}
}

// mapStatusToInt 将领域层的DIDStatus枚举转换为数据库存储的整数
// 参数 s: 领域层的DIDStatus
// 返回: 对应的整数值
func mapStatusToInt(s entity.DIDStatus) int {
	switch s { // 判断状态
	case entity.DIDStatusDisabled: // 如果是禁用状态
		return 2 // 返回2
	case entity.DIDStatusRevoked: // 如果是撤销状态
		return 3 // 返回3
	default: // 其他情况（包括active）
		return 1 // 默认返回1（active）
	}
}

// mapIntToStatus 将数据库整数转换为领域层的DIDStatus枚举
// 参数 i: 数据库中存储的整数值
// 返回: 领域层的DIDStatus
func mapIntToStatus(i int) entity.DIDStatus {
	switch i { // 判断整数值
	case 2: // 如果是2
		return entity.DIDStatusDisabled // 返回禁用状态
	case 3: // 如果是3
		return entity.DIDStatusRevoked // 返回撤销状态
	default: // 其他情况（包括1）
		return entity.DIDStatusActive // 返回活跃状态
	}
}
