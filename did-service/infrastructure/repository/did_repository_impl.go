// Package repository 提供DID仓储的内存实现
// COLA v5: Infrastructure Layer
// 注意: 这是MVP内存实现，需要替换为数据库存储
package repository

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/stablepay/did-service/domain/entity"
	"github.com/stablepay/did-service/domain/gateway"
)

// MemoryDIDRepository 内存DID仓储实现
// 线程安全的map存储
type MemoryDIDRepository struct {
	mu   sync.RWMutex
	data map[string]*entity.DID // key: DIDString
}

// NewMemoryDIDRepository 创建内存仓储
func NewMemoryDIDRepository() gateway.DIDRepository {
	return &MemoryDIDRepository{
		data: make(map[string]*entity.DID),
	}
}

// Save 保存DID
func (r *MemoryDIDRepository) Save(ctx context.Context, did *entity.DID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if did.ID == "" {
		did.ID = fmt.Sprintf("did_%d", time.Now().UnixNano())
	}

	r.data[did.DIDString] = did
	return nil
}

// FindByDID 通过DID字符串查询
func (r *MemoryDIDRepository) FindByDID(ctx context.Context, didString string) (*entity.DID, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	did, ok := r.data[didString]
	if !ok {
		return nil, nil
	}
	// 返回副本防止外部修改
	return copyDID(did), nil
}

// FindByWalletAddress 通过钱包地址查询
func (r *MemoryDIDRepository) FindByWalletAddress(ctx context.Context, walletAddress string) (*entity.DID, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, did := range r.data {
		if did.WalletAddress == walletAddress {
			return copyDID(did), nil
		}
	}
	return nil, nil
}

// Update 更新DID
func (r *MemoryDIDRepository) Update(ctx context.Context, did *entity.DID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	did.UpdatedAt = time.Now()
	r.data[did.DIDString] = did
	return nil
}

// UpdateStatus 更新状态
func (r *MemoryDIDRepository) UpdateStatus(ctx context.Context, didString string, status entity.DIDStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	did, ok := r.data[didString]
	if !ok {
		return fmt.Errorf("did not found: %s", didString)
	}

	did.Status = status
	did.UpdatedAt = time.Now()
	return nil
}

// UpdateConfig 更新配置
func (r *MemoryDIDRepository) UpdateConfig(ctx context.Context, didString string, config map[string]string, newVersion int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	did, ok := r.data[didString]
	if !ok {
		return fmt.Errorf("did not found: %s", didString)
	}

	// 更新配置
	if did.Config == nil {
		did.Config = make(map[string]string)
	}
	for k, v := range config {
		did.Config[k] = v
	}
	did.ConfigVersion = newVersion
	did.UpdatedAt = time.Now()
	return nil
}

// List 查询列表（分页）
func (r *MemoryDIDRepository) List(ctx context.Context, limit, offset int) ([]*entity.DID, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*entity.DID, 0, len(r.data))
	for _, did := range r.data {
		result = append(result, copyDID(did))
	}

	// 简单分页
	if offset >= len(result) {
		return []*entity.DID{}, nil
	}
	end := offset + limit
	if end > len(result) {
		end = len(result)
	}

	return result[offset:end], nil
}

// Exists 检查DID是否存在
func (r *MemoryDIDRepository) Exists(ctx context.Context, didString string) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, ok := r.data[didString]
	return ok, nil
}

// copyDID 复制DID实体
func copyDID(did *entity.DID) *entity.DID {
	newDID := &entity.DID{
		ID:            did.ID,
		DIDString:     did.DIDString,
		PublicKey:     did.PublicKey,
		PrivateKey:    did.PrivateKey, // 加密后的私钥
		WalletAddress: did.WalletAddress,
		UserType:      did.UserType,
		Status:        did.Status,
		ConfigVersion: did.ConfigVersion,
		CreatedAt:     did.CreatedAt,
		UpdatedAt:     did.UpdatedAt,
	}

	// 复制map
	if did.Config != nil {
		newDID.Config = make(map[string]string)
		for k, v := range did.Config {
			newDID.Config[k] = v
		}
	}
	if did.Metadata != nil {
		newDID.Metadata = make(map[string]string)
		for k, v := range did.Metadata {
			newDID.Metadata[k] = v
		}
	}

	return newDID
}

// GetCount 获取总数（用于测试）
func (r *MemoryDIDRepository) GetCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.data)
}

// Clear 清空所有数据（用于测试）
func (r *MemoryDIDRepository) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data = make(map[string]*entity.DID)
}
