// Package gateway 定义DID仓储接口
// COLA v5: Domain Gateway Layer
// 依赖倒置原则: 领域层定义接口，基础设施层实现
package gateway

import (
	"context"
	"github.com/stablepay/did-service/internal/domain/entity"
)

// DIDRepository DID仓储接口
type DIDRepository interface {
	// Save 保存DID
	Save(ctx context.Context, did *entity.DID) error

	// FindByDID 通过DID字符串查询
	FindByDID(ctx context.Context, didString string) (*entity.DID, error)

	// FindByWalletAddress 通过钱包地址查询
	FindByWalletAddress(ctx context.Context, walletAddress string) (*entity.DID, error)

	// Update 更新DID
	Update(ctx context.Context, did *entity.DID) error

	// UpdateStatus 更新状态
	UpdateStatus(ctx context.Context, didString string, status entity.DIDStatus) error

	// UpdateConfig 更新配置
	UpdateConfig(ctx context.Context, didString string, config map[string]string, newVersion int64) error

	// List 查询列表（分页）
	List(ctx context.Context, limit, offset int) ([]*entity.DID, error)

	// Exists 检查DID是否存在
	Exists(ctx context.Context, didString string) (bool, error)
}
