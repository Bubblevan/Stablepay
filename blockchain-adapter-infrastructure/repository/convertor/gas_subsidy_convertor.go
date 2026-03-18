// Package convertor 定义对象转换器
// 职责：Entity（领域对象）和 PO（持久化对象）之间的转换
package convertor

import (
	"time"

	"stablepay.blockchain_adapter/blockchain-adapter-domain/entity"
	"stablepay.blockchain_adapter/blockchain-adapter-infrastructure/persist"
)

// EntityToPO 将领域实体转换为持久化对象
func EntityToPO(e *entity.GasSubsidyEntity) *persist.GasSubsidyPO {
	if e == nil {
		return nil
	}
	return &persist.GasSubsidyPO{
		ID:              e.ID,
		TxID:            e.TxID,
		OriginalTxHash:  e.OriginalTxHash,
		GasAmount:       e.GasAmount,
		SubsidyAmount:   e.SubsidyAmount,
		AgentPaidAmount: e.AgentPaidAmount,
		SubsidyTxHash:   e.SubsidyTxHash,
		Status:          string(e.Status),
		FeePayer:        e.FeePayer,
		Network:         e.Network,
		CreatedAt:       e.CreatedAt.Unix(),
		UpdatedAt:       e.UpdatedAt.Unix(),
	}
}

// POToEntity 将持久化对象转换为领域实体
func POToEntity(po *persist.GasSubsidyPO) *entity.GasSubsidyEntity {
	if po == nil {
		return nil
	}
	return &entity.GasSubsidyEntity{
		ID:              po.ID,
		TxID:            po.TxID,
		OriginalTxHash:  po.OriginalTxHash,
		GasAmount:       po.GasAmount,
		SubsidyAmount:   po.SubsidyAmount,
		AgentPaidAmount: po.AgentPaidAmount,
		SubsidyTxHash:   po.SubsidyTxHash,
		Status:          entity.SubsidyStatus(po.Status),
		FeePayer:        po.FeePayer,
		Network:         po.Network,
		CreatedAt:       time.Unix(po.CreatedAt, 0),
		UpdatedAt:       time.Unix(po.UpdatedAt, 0),
	}
}

// POsToEntities 批量转换
func POsToEntities(pos []*persist.GasSubsidyPO) []*entity.GasSubsidyEntity {
	entities := make([]*entity.GasSubsidyEntity, 0, len(pos))
	for _, po := range pos {
		entities = append(entities, POToEntity(po))
	}
	return entities
}
