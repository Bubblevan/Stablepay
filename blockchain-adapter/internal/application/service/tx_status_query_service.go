// Package service 查询服务
package service

import (
	"context"
	"fmt"

	"github.com/stablepay/blockchain-adapter/internal/domain/entity"
	"github.com/stablepay/blockchain-adapter/internal/domain/gateway"
	domainService "github.com/stablepay/blockchain-adapter/internal/domain/service"
	"github.com/stablepay/blockchain-adapter/internal/domain/vo"
)

// TxStatusQueryService 交易状态查询服务
type TxStatusQueryService struct {
	solanaGateway gateway.SolanaGateway
	subsidyRepo   gateway.GasSubsidyRepositoryGateway
	calculator    *domainService.GasSubsidyCalculator
}

// NewTxStatusQueryService 创建交易状态查询服务
func NewTxStatusQueryService(
	solanaGateway gateway.SolanaGateway,
	subsidyRepo gateway.GasSubsidyRepositoryGateway,
) *TxStatusQueryService {
	return &TxStatusQueryService{
		solanaGateway: solanaGateway,
		subsidyRepo:   subsidyRepo,
		calculator:    domainService.NewGasSubsidyCalculator(),
	}
}

// TxStatusQuery 交易状态查询参数
type TxStatusQuery struct {
	TxHash string
}

// GetTxStatus 查询交易状态
// 直接从链上查询交易最新状态
func (s *TxStatusQueryService) GetTxStatus(ctx context.Context, query *TxStatusQuery) (*vo.TxStatusVO, error) {
	if query.TxHash == "" {
		return nil, fmt.Errorf("tx_hash is required")
	}
	return s.solanaGateway.GetTransactionStatus(ctx, query.TxHash)
}

// SyncTxStatus 同步交易状态到数据库
// 流程：查询链上状态 -> 对比数据库状态 -> 状态变化时更新记录 -> 计算补贴金额
func (s *TxStatusQueryService) SyncTxStatus(ctx context.Context, txHash string) error {
	if txHash == "" {
		return fmt.Errorf("tx_hash is required")
	}

	// 1. 查询链上状态
	status, err := s.solanaGateway.GetTransactionStatus(ctx, txHash)
	if err != nil {
		return fmt.Errorf("failed to get transaction status from chain: %w", err)
	}

	// 2. 查询补贴记录
	subsidy, err := s.subsidyRepo.FindByTxHash(ctx, txHash)
	if err != nil {
		return fmt.Errorf("failed to find subsidy record: %w", err)
	}
	if subsidy == nil {
		// 记录不存在，无需同步
		return nil
	}

	// 3. 检查状态是否需要更新
	// 只有状态发生变化时才更新数据库，避免不必要的写入
	chainStatus := mapChainStatusToEntity(status.Status)
	if chainStatus == subsidy.Status {
		// 状态未变化，无需更新
		return nil
	}

	// 4. 根据链上状态更新补贴记录
	switch chainStatus {
	case entity.SubsidyCompleted:
		// 交易已确认，计算实际补贴金额
		if status.Fee > 0 {
			s.calculator.Calculate(subsidy, status.Fee)
		}
		subsidy.MarkCompleted()

	case entity.SubsidyFailed:
		// 交易失败
		subsidy.MarkFailed()

	case entity.SubsidyPending:
		// 交易仍在等待确认，更新状态但不改变最终状态标记
		subsidy.UpdateStatus(entity.SubsidyPending)
	}

	// 5. 持久化更新
	if err := s.subsidyRepo.Update(ctx, subsidy); err != nil {
		return fmt.Errorf("failed to update subsidy record: %w", err)
	}

	return nil
}

// mapChainStatusToEntity 将链上状态映射为领域实体状态
func mapChainStatusToEntity(chainStatus string) entity.SubsidyStatus {
	switch chainStatus {
	case "confirmed", "finalized":
		return entity.SubsidyCompleted
	case "failed":
		return entity.SubsidyFailed
	default:
		return entity.SubsidyPending
	}
}
