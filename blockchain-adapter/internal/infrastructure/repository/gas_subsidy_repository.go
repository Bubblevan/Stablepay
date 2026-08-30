// Package repository 实现领域仓储接口
// 职责：实现 domain/gateway 中定义的 GasSubsidyRepositoryGateway 接口
// 技术实现：使用 GORM 操作 MySQL
package repository

import (
	"context"

	"github.com/stablepay/blockchain-adapter/internal/domain/entity"
	"github.com/stablepay/blockchain-adapter/internal/domain/gateway"
	"github.com/stablepay/blockchain-adapter/internal/infrastructure/persist"
	"github.com/stablepay/blockchain-adapter/internal/infrastructure/repository/convertor"

	"gorm.io/gorm"
)

// GasSubsidyRepository Gas 补贴仓储实现
type GasSubsidyRepository struct {
	db *gorm.DB
}

// NewGasSubsidyRepository 创建仓储实例
func NewGasSubsidyRepository(db *gorm.DB) gateway.GasSubsidyRepositoryGateway {
	return &GasSubsidyRepository{
		db: db,
	}
}

// Save 保存实体
func (r *GasSubsidyRepository) Save(ctx context.Context, entity *entity.GasSubsidyEntity) error {
	po := convertor.EntityToPO(entity)
	return r.db.WithContext(ctx).Create(po).Error
}

// FindByID 根据 ID 查询
func (r *GasSubsidyRepository) FindByID(ctx context.Context, id uint64) (*entity.GasSubsidyEntity, error) {
	var po persist.GasSubsidyPO
	if err := r.db.WithContext(ctx).First(&po, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return convertor.POToEntity(&po), nil
}

// FindByTxHash 根据交易哈希查询
func (r *GasSubsidyRepository) FindByTxHash(ctx context.Context, txHash string) (*entity.GasSubsidyEntity, error) {
	var po persist.GasSubsidyPO
	if err := r.db.WithContext(ctx).Where("original_tx_hash = ?", txHash).First(&po).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return convertor.POToEntity(&po), nil
}

// FindByTxID 根据业务交易 ID 查询
func (r *GasSubsidyRepository) FindByTxID(ctx context.Context, txID string) (*entity.GasSubsidyEntity, error) {
	var po persist.GasSubsidyPO
	if err := r.db.WithContext(ctx).Where("tx_id = ?", txID).First(&po).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return convertor.POToEntity(&po), nil
}

// Update 更新实体
func (r *GasSubsidyRepository) Update(ctx context.Context, entity *entity.GasSubsidyEntity) error {
	po := convertor.EntityToPO(entity)
	return r.db.WithContext(ctx).Save(po).Error
}

// ListByStatus 根据状态查询列表
func (r *GasSubsidyRepository) ListByStatus(ctx context.Context, status entity.SubsidyStatus, limit, offset int) ([]*entity.GasSubsidyEntity, error) {
	var pos []*persist.GasSubsidyPO
	if err := r.db.WithContext(ctx).Where("status = ?", status).Order("created_at DESC").Limit(limit).Offset(offset).Find(&pos).Error; err != nil {
		return nil, err
	}

	return convertor.POsToEntities(pos), nil
}

// ListPending 查询待处理的补贴记录
func (r *GasSubsidyRepository) ListPending(ctx context.Context, limit int) ([]*entity.GasSubsidyEntity, error) {
	return r.ListByStatus(ctx, entity.SubsidyPending, limit, 0)
}
