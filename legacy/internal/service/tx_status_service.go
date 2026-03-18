// Package service 提供交易状态查询服务
package service

import (
	"context"
	"fmt"
	"time"

	"stablepay.blockchain_adapter/data-access-layer/db"
	internalsolana "stablepay.blockchain_adapter/internal/solana"
	"stablepay.blockchain_adapter/kitex_gen/stablepay/blockchain_adapter"
	"stablepay.blockchain_adapter/kitex_gen/stablepay/common"
)

// TxStatusService 交易状态服务
type TxStatusService struct {
	client     *internalsolana.Client
	subsidyDAL *db.GasSubsidyDAL
}

// NewTxStatusService 创建交易状态服务
func NewTxStatusService(client *internalsolana.Client, subsidyDAL *db.GasSubsidyDAL) *TxStatusService {
	return &TxStatusService{
		client:     client,
		subsidyDAL: subsidyDAL,
	}
}

// TxStatusInfo 交易状态信息
type TxStatusInfo struct {
	TxHash      string                      // 交易哈希
	Status      blockchain_adapter.TxStatus // 状态
	Slot        uint64                      // 确认的区块高度
	BlockTime   *time.Time                  // 区块时间
	Fee         uint64                      // 手续费
	Err         interface{}                 // 错误信息（如有）
	ExplorerURL string                      // 浏览器链接
}

// GetTxStatus 查询交易状态
//
// 状态映射:
// - 交易未找到 → PENDING (可能还在处理中)
// - 交易找到且无错误 → CONFIRMED
// - 交易找到但有错误 → FAILED
func (s *TxStatusService) GetTxStatus(ctx context.Context, txHash string) (*TxStatusInfo, error) {
	result, err := s.client.GetTransactionStatus(txHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction status: %w", err)
	}

	// 交易未找到
	if result == nil {
		return &TxStatusInfo{
			TxHash:      txHash,
			Status:      blockchain_adapter.TxStatus_PENDING,
			ExplorerURL: s.client.GetExplorerURL(txHash),
		}, nil
	}

	// 解析状态
	status := blockchain_adapter.TxStatus_CONFIRMED
	var fee uint64
	var errInfo interface{}

	if result.Meta != nil {
		fee = uint64(result.Meta.Fee)
		errInfo = result.Meta.Err
		if result.Meta.Err != nil {
			status = blockchain_adapter.TxStatus_FAILED
		}
	}

	// 解析区块时间 (UnixTimeSeconds 是 int64 类型的 Unix 时间戳)
	var blockTime *time.Time
	if result.BlockTime != nil {
		t := time.Unix(int64(*result.BlockTime), 0)
		blockTime = &t
	}

	return &TxStatusInfo{
		TxHash:      txHash,
		Status:      status,
		Slot:        uint64(result.Slot),
		BlockTime:   blockTime,
		Fee:         fee,
		Err:         errInfo,
		ExplorerURL: s.client.GetExplorerURL(txHash),
	}, nil
}

// GetTxStatusWithSubsidy 查询交易状态并关联补贴记录
// 返回交易状态和对应的补贴信息
func (s *TxStatusService) GetTxStatusWithSubsidy(ctx context.Context, txHash string) (*TxStatusInfo, *db.GasSubsidyRecord, error) {
	// 查询交易状态
	txStatus, err := s.GetTxStatus(ctx, txHash)
	if err != nil {
		return nil, nil, err
	}

	// 查询补贴记录
	subsidyRecord, err := s.subsidyDAL.GetByTxHash(txHash)
	if err != nil {
		// 不返回错误，补贴记录可能还未创建
		return txStatus, nil, nil
	}

	return txStatus, subsidyRecord, nil
}

// PollTxStatus 轮询交易状态直到确认或超时
// 用于需要确保交易最终性的场景
func (s *TxStatusService) PollTxStatus(ctx context.Context, txHash string, timeout time.Duration, interval time.Duration) (*TxStatusInfo, error) {
	start := time.Now()

	for time.Since(start) < timeout {
		statusInfo, err := s.GetTxStatus(ctx, txHash)
		if err != nil {
			return nil, err
		}

		// 如果已确认或失败，立即返回
		if statusInfo.Status == blockchain_adapter.TxStatus_CONFIRMED ||
			statusInfo.Status == blockchain_adapter.TxStatus_FAILED {
			return statusInfo, nil
		}

		// 等待后重试
		select {
		case <-time.After(interval):
			continue
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// 超时，返回当前状态（可能是 PENDING）
	return s.GetTxStatus(ctx, txHash)
}

// SyncTxStatus 同步交易状态到数据库
// 更新补贴记录中的交易状态和实际 Gas 费用
func (s *TxStatusService) SyncTxStatus(ctx context.Context, txHash string) error {
	// 查询链上状态
	txStatus, err := s.GetTxStatus(ctx, txHash)
	if err != nil {
		return err
	}

	// 查询补贴记录
	subsidyRecord, err := s.subsidyDAL.GetByTxHash(txHash)
	if err != nil {
		return fmt.Errorf("failed to get subsidy record: %w", err)
	}

	if subsidyRecord == nil {
		return fmt.Errorf("subsidy record not found for tx: %s", txHash)
	}

	// 映射状态
	var dbStatus db.GasSubsidyStatus
	switch txStatus.Status {
	case blockchain_adapter.TxStatus_CONFIRMED:
		dbStatus = db.SubsidyCompleted
	case blockchain_adapter.TxStatus_FAILED:
		dbStatus = db.SubsidyFailed
	case blockchain_adapter.TxStatus_PENDING:
		dbStatus = db.SubsidyPending
	default:
		dbStatus = db.SubsidyPending
	}

	// 更新记录
	if err := s.subsidyDAL.UpdateSubsidyStatusByTxHash(txHash, dbStatus, txStatus.Fee); err != nil {
		return fmt.Errorf("failed to update subsidy status: %w", err)
	}

	return nil
}

// ListPendingTxs 列出所有待确认的交易
// 用于定时任务扫描并更新状态
func (s *TxStatusService) ListPendingTxs(ctx context.Context, limit int) ([]*db.GasSubsidyRecord, error) {
	return s.subsidyDAL.ListByStatus(db.SubsidyPending, limit, 0)
}

// BatchSyncTxStatus 批量同步交易状态
// 用于定时任务批量更新 pending 交易的状态
func (s *TxStatusService) BatchSyncTxStatus(ctx context.Context, limit int) error {
	// 获取待确认的交易
	pendingRecords, err := s.ListPendingTxs(ctx, limit)
	if err != nil {
		return fmt.Errorf("failed to list pending txs: %w", err)
	}

	// 逐个同步
	for _, record := range pendingRecords {
		if record.OriginalTxHash == "" {
			continue
		}

		if err := s.SyncTxStatus(ctx, record.OriginalTxHash); err != nil {
			// 记录日志但不中断，继续处理其他交易
			// TODO: 记录到错误队列
			continue
		}
	}

	return nil
}

// IsTxConfirmed 检查交易是否已确认
func (s *TxStatusService) IsTxConfirmed(ctx context.Context, txHash string) (bool, error) {
	statusInfo, err := s.GetTxStatus(ctx, txHash)
	if err != nil {
		return false, err
	}

	return statusInfo.Status == blockchain_adapter.TxStatus_CONFIRMED, nil
}

// GetTxConfirmationTime 获取交易确认时间
func (s *TxStatusService) GetTxConfirmationTime(ctx context.Context, txHash string) (*time.Time, error) {
	statusInfo, err := s.GetTxStatus(ctx, txHash)
	if err != nil {
		return nil, err
	}

	if statusInfo.Status != blockchain_adapter.TxStatus_CONFIRMED {
		return nil, fmt.Errorf("transaction not confirmed")
	}

	return statusInfo.BlockTime, nil
}

// GetTxFee 获取交易实际手续费
func (s *TxStatusService) GetTxFee(ctx context.Context, txHash string) (uint64, error) {
	statusInfo, err := s.GetTxStatus(ctx, txHash)
	if err != nil {
		return 0, err
	}

	return statusInfo.Fee, nil
}

// ConvertToResponse 转换为 Thrift 响应格式
func (s *TxStatusService) ConvertToResponse(statusInfo *TxStatusInfo) *blockchain_adapter.GetTxStatusResponse {
	resp := &blockchain_adapter.GetTxStatusResponse{
		Base: &common.BaseResp{
			Code:    0,
			Message: "success",
		},
		Status: statusInfo.Status,
	}

	if statusInfo.BlockTime != nil {
		confirmedAt := statusInfo.BlockTime.Format(time.RFC3339)
		resp.ConfirmedAt = &confirmedAt
	}

	if statusInfo.Status == blockchain_adapter.TxStatus_FAILED && statusInfo.Err != nil {
		errMsg := fmt.Sprintf("%v", statusInfo.Err)
		resp.ReasonMessage = &errMsg
	}

	return resp
}
