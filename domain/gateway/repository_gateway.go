// Package gateway 定义领域网关接口
// 领域网关：由 Domain 层定义，Infrastructure 层实现
// 实现依赖倒置，Domain 层不依赖具体技术实现
package gateway

import (
	"context"

	"github.com/stablepay/blockchain-adapter/domain/entity"
)

// GasSubsidyRepositoryGateway Gas 补贴仓储网关接口
// 职责：定义 Gas 补贴实体的持久化操作
// 实现：由 Infrastructure 层的 repository 包实现
type GasSubsidyRepositoryGateway interface {
	// Save 保存实体
	Save(ctx context.Context, entity *entity.GasSubsidyEntity) error
	
	// FindByID 根据 ID 查询
	FindByID(ctx context.Context, id uint64) (*entity.GasSubsidyEntity, error)
	
	// FindByTxHash 根据交易哈希查询
	FindByTxHash(ctx context.Context, txHash string) (*entity.GasSubsidyEntity, error)
	
	// FindByTxID 根据业务交易 ID 查询
	FindByTxID(ctx context.Context, txID string) (*entity.GasSubsidyEntity, error)
	
	// Update 更新实体
	Update(ctx context.Context, entity *entity.GasSubsidyEntity) error
	
	// ListByStatus 根据状态查询列表
	ListByStatus(ctx context.Context, status entity.SubsidyStatus, limit, offset int) ([]*entity.GasSubsidyEntity, error)
	
	// ListPending 查询待处理的补贴记录
	ListPending(ctx context.Context, limit int) ([]*entity.GasSubsidyEntity, error)
}

// SubsidyStatsVO 补贴统计值对象
type SubsidyStatsVO struct {
	TotalCount     int64
	CompletedCount int64
	FailedCount    int64
	PendingCount   int64
	TotalSubsidy   uint64
	TotalAgentPaid uint64
}

// StatsQuery 统计查询接口（可选扩展）
type StatsQuery interface {
	// GetStats 获取整体统计
	GetStats(ctx context.Context) (*SubsidyStatsVO, error)
	
	// GetStatsByNetwork 按网络获取统计
	GetStatsByNetwork(ctx context.Context, network string) (*SubsidyStatsVO, error)
}
