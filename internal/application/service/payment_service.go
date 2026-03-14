// Package service 应用服务层
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/stablepay/payment-service/internal/application/dto"
	"github.com/stablepay/payment-service/internal/domain/entity"
	"github.com/stablepay/payment-service/internal/domain/repository"
	"github.com/stablepay/payment-service/internal/domain/service"
	"github.com/stablepay/payment-service/internal/domain/vo"
	"github.com/stablepay/payment-service/pkg/constants"
	"github.com/stablepay/payment-service/pkg/errors"
	"github.com/stablepay/payment-service/pkg/utils"
	"go.uber.org/zap"
)

// BlockchainExecutor 区块链执行器接口
type BlockchainExecutor interface {
	// ExecuteTransfer 执行转账交易
	ExecuteTransfer(ctx context.Context, fromWallet, toWallet string, amountMinor int64,
		currency constants.Currency) (txHash string, err error)
	// QueryTxStatus 查询交易状态
	QueryTxStatus(ctx context.Context, txHash string) (status int8, confirmedAt *time.Time, err error)
}

// EventPublisher 事件发布器接口
type EventPublisher interface {
	// PublishPaymentEvent 发布支付事件
	PublishPaymentEvent(ctx context.Context, event *dto.MQPaymentEvent) error
}

// PaymentApplicationService 支付应用服务
type PaymentApplicationService struct {
	// 仓库
	paymentRepo      repository.PaymentRepository
	idempotencyRepo  repository.PaymentIdempotencyRepository

	// 领域服务
	paymentValidator  *service.PaymentValidator
	nonceChecker      *service.NonceChecker
	idempotencyGen    *service.IdempotencyKeyGenerator

	// 基础设施
	blockchainExec    BlockchainExecutor
	eventPublisher    EventPublisher

	// 配置
	config            *PaymentConfig

	// 日志
	logger            *zap.Logger
}

// PaymentConfig 支付服务配置
type PaymentConfig struct {
	TimeoutMinutes       int
	MaxRetryCount        int
	MaxAmountMinor       int64
	PollIntervalSeconds  int
	MaxPollCount         int
}

// NewPaymentApplicationService 创建支付应用服务
func NewPaymentApplicationService(
	paymentRepo repository.PaymentRepository,
	idempotencyRepo repository.PaymentIdempotencyRepository,
	paymentValidator *service.PaymentValidator,
	nonceChecker *service.NonceChecker,
	blockchainExec BlockchainExecutor,
	eventPublisher EventPublisher,
	config *PaymentConfig,
	logger *zap.Logger,
) *PaymentApplicationService {
	return &PaymentApplicationService{
		paymentRepo:      paymentRepo,
		idempotencyRepo:  idempotencyRepo,
		paymentValidator: paymentValidator,
		nonceChecker:     nonceChecker,
		idempotencyGen:   service.NewIdempotencyKeyGenerator(),
		blockchainExec:   blockchainExec,
		eventPublisher:   eventPublisher,
		config:           config,
		logger:           logger,
	}
}

// InitiatePayment 发起支付
func (s *PaymentApplicationService) InitiatePayment(ctx context.Context, req *dto.InitiatePaymentRequest) (*dto.InitiatePaymentResponse, error) {
	// 1. 金额格式转换（字符串 -> 最小单位整数）
	amountMinor, err := utils.StringToMinorUnit(req.AmountStr)
	if err != nil {
		return nil, errors.Wrap(errors.INVALID_PARAMETERS, err, "invalid amount format")
	}

	currency := vo.StringToCommonCurrency(req.Currency)

	// 2. 构建幂等性键
	idempotencyKey := s.idempotencyGen.Generate(req.AgentDID, req.SkillDID, req.IdempotencyKey)

	// 3. 检查幂等性
	existingResp, err := s.checkIdempotency(ctx, idempotencyKey, req)
	if err != nil {
		return nil, err
	}
	if existingResp != nil {
		return existingResp, nil
	}

	// 4. 防重放检查（nonce）
	if err := s.nonceChecker.CheckAndRecord(ctx, req.Nonce); err != nil {
		return nil, err
	}

	// 5. 构建值对象
	amount, err := vo.NewAmount(amountMinor, currency)
	if err != nil {
		return nil, err
	}

	signData := vo.GetSignData(req.AgentDID, req.SkillDID, amountMinor, currency, req.Timestamp, req.Nonce)
	signature := vo.NewSignature(req.Signature, req.Timestamp, req.Nonce, signData)

	// 6. 验证支付请求（签名、金额等）
	if err := s.paymentValidator.ValidatePaymentRequest(ctx, req.AgentDID, req.SkillDID, amount, signature); err != nil {
		s.recordIdempotency(ctx, idempotencyKey, "", 2, req, nil) // 记录失败
		return nil, err
	}

	// 7. 检查余额
	// 注意：这里需要获取 Agent 的钱包地址，实际通过 DID Service 查询
	// 简化处理：假设 DID 中的公钥就是钱包地址
	walletAddress := extractWalletFromDID(req.AgentDID)
	if err := s.paymentValidator.CheckBalance(ctx, walletAddress, currency, amountMinor); err != nil {
		s.recordIdempotency(ctx, idempotencyKey, "", 2, req, nil)
		return nil, err
	}

	// 8. 创建支付记录
	txID := generateTxID()
	payment, err := entity.NewPayment(txID, req.AgentDID, req.SkillDID, amountMinor, currency,
		req.Signature, req.Timestamp, req.Nonce, s.config.TimeoutMinutes)
	if err != nil {
		return nil, err
	}

	if err := s.paymentRepo.Create(ctx, payment); err != nil {
		return nil, errors.Wrap(errors.DATABASE_CONNECTION_ERROR, err, "failed to create payment record")
	}

	// 9. 记录幂等性（进行中）
	if err := s.recordIdempotency(ctx, idempotencyKey, txID, 0, req, nil); err != nil {
		s.logger.Warn("failed to record idempotency", zap.Error(err))
	}

	// 10. 执行链上交易（同步）
	skillWallet := extractWalletFromDID(req.SkillDID)
	txHash, err := s.blockchainExec.ExecuteTransfer(ctx, walletAddress, skillWallet, amountMinor, currency)
	if err != nil {
		// 链上执行失败
		_ = payment.MarkAsFailed("BLOCKCHAIN_ERROR", err.Error())
		_ = s.paymentRepo.Update(ctx, payment)
		s.publishEvent(ctx, payment)
		s.recordIdempotency(ctx, idempotencyKey, txID, 2, req, s.toResponse(payment))
		return nil, errors.Wrap(errors.BLOCKCHAIN_NETWORK_ERROR, err, "blockchain transaction failed")
	}

	// 11. 更新为交易中状态
	if err := payment.MarkAsPending(txHash); err != nil {
		s.logger.Error("failed to transition status", zap.Error(err))
	}
	if err := s.paymentRepo.Update(ctx, payment); err != nil {
		s.logger.Error("failed to update payment status", zap.Error(err))
	}

	// 12. 异步轮询链上状态（后台 goroutine）
	go s.pollTxStatus(payment.TxID, txHash)

	// 13. 返回响应
	resp := s.toResponse(payment)
	s.recordIdempotency(ctx, idempotencyKey, txID, 0, req, resp)

	return resp, nil
}

// GetPaymentStatus 查询支付状态
func (s *PaymentApplicationService) GetPaymentStatus(ctx context.Context, txID string) (*dto.GetPaymentStatusResponse, error) {
	payment, err := s.paymentRepo.GetByTxID(ctx, txID)
	if err != nil {
		return nil, errors.Wrap(errors.RESOURCE_NOT_FOUND, err, "payment not found")
	}

	return s.toStatusResponse(payment), nil
}

// ListPaymentHistory 查询支付历史
func (s *PaymentApplicationService) ListPaymentHistory(ctx context.Context, req *dto.ListPaymentHistoryRequest) (*dto.ListPaymentHistoryResponse, error) {
	pageParam := vo.NewPageParam(req.Page, req.PageSize)

	var status *int8
	if req.Status != "" {
		s := parseStatus(req.Status)
		status = &s
	}

	payments, total, err := s.paymentRepo.ListByAgentDID(ctx, req.AgentDID, status, pageParam.Offset(), pageParam.Limit())
	if err != nil {
		return nil, errors.Wrap(errors.DATABASE_CONNECTION_ERROR, err, "failed to query payment history")
	}

	items := make([]*dto.PaymentHistoryItem, len(payments))
	for i, p := range payments {
		items[i] = &dto.PaymentHistoryItem{
			TxID:      p.TxID,
			SkillDID:  p.SkillDID,
			Amount:    utils.MinorUnitToString(p.AmountMinor),
			Currency:  vo.CommonCurrencyToString(p.Currency),
			Status:    constants.PaymentStatusToString(p.Status),
			CreatedAt: p.CreatedAt.Format(constants.TimeFormatISO8601),
		}
	}

	return &dto.ListPaymentHistoryResponse{
		Items:    items,
		Total:    total,
		Page:     pageParam.Page,
		PageSize: pageParam.PageSize,
	}, nil
}

// GetPaymentRequirement 获取支付要求（HTTP 402）
func (s *PaymentApplicationService) GetPaymentRequirement(ctx context.Context, req *dto.GetPaymentRequirementRequest) (*dto.PaymentRequirementResponse, bool, error) {
	// 如果传入了 agent_did，检查是否已购买
	if req.AgentDID != "" {
		count, err := s.paymentRepo.CountByAgentAndSkill(ctx, req.AgentDID, req.SkillDID)
		if err != nil {
			s.logger.Warn("failed to check purchase status", zap.Error(err))
		}
		if count > 0 {
			// 已购买
			return nil, true, nil
		}
	}

	// 返回支付要求（HTTP 402）
	return &dto.PaymentRequirementResponse{
		SkillDID:  req.SkillDID,
		SkillName: "", // TODO: 从 Skill Registry 获取
		Price:     "0",
		Currency:  "USDC",
		Endpoint:  "/api/v1/pay",
	}, false, nil
}

// pollTxStatus 轮询交易状态
func (s *PaymentApplicationService) pollTxStatus(txID, txHash string) {
	ctx := context.Background()

	for i := 0; i < s.config.MaxPollCount; i++ {
		time.Sleep(time.Duration(s.config.PollIntervalSeconds) * time.Second)

		status, confirmedAt, err := s.blockchainExec.QueryTxStatus(ctx, txHash)
		if err != nil {
			s.logger.Error("failed to query tx status", zap.String("tx_hash", txHash), zap.Error(err))
			continue
		}

		payment, err := s.paymentRepo.GetByTxID(ctx, txID)
		if err != nil {
			s.logger.Error("failed to get payment", zap.String("tx_id", txID), zap.Error(err))
			return
		}

		switch status {
		case constants.PaymentStatusConfirmed:
			// 交易确认成功
			_ = payment.MarkAsConfirmed()
			if confirmedAt != nil {
				payment.ConfirmedAt = confirmedAt
			}
			_ = s.paymentRepo.Update(ctx, payment)

			// 标记为完成
			_ = payment.MarkAsCompleted()
			_ = s.paymentRepo.Update(ctx, payment)

			// 发布事件
			s.publishEvent(ctx, payment)
			return

		case constants.PaymentStatusFailed:
			// 交易失败
			_ = payment.MarkAsFailed("BLOCKCHAIN_FAILED", "transaction failed on chain")
			_ = s.paymentRepo.Update(ctx, payment)
			s.publishEvent(ctx, payment)
			return

		case constants.PaymentStatusPending:
			// 继续轮询
			continue
		}
	}

	// 超过最大轮询次数，标记为超时
	payment, _ := s.paymentRepo.GetByTxID(ctx, txID)
	if payment != nil {
		_ = payment.MarkAsFailed("POLL_TIMEOUT", "exceeded maximum poll count")
		_ = s.paymentRepo.Update(ctx, payment)
		s.publishEvent(ctx, payment)
	}
}

// publishEvent 发布支付事件
func (s *PaymentApplicationService) publishEvent(ctx context.Context, payment *entity.Payment) {
	event := &dto.MQPaymentEvent{
		TxID:        payment.TxID,
		AgentDID:    payment.AgentDID,
		SkillDID:    payment.SkillDID,
		AmountMinor: payment.AmountMinor,
		Currency:    vo.CommonCurrencyToString(payment.Currency),
		TxHash:      payment.TxHash,
		Status:      constants.PaymentStatusToString(payment.Status),
		Timestamp:   time.Now().Unix(),
	}

	if payment.Status == constants.PaymentStatusFailed || payment.Status == constants.PaymentStatusCancelled {
		event.ErrorCode = payment.ErrorCode
		event.ErrorMsg = payment.ErrorMsg
	}

	if err := s.eventPublisher.PublishPaymentEvent(ctx, event); err != nil {
		s.logger.Error("failed to publish payment event", zap.Error(err))
	}
}

// checkIdempotency 检查幂等性
func (s *PaymentApplicationService) checkIdempotency(ctx context.Context, key string, req *dto.InitiatePaymentRequest) (*dto.InitiatePaymentResponse, error) {
	record, err := s.idempotencyRepo.Get(ctx, key)
	if err != nil {
		return nil, nil // 未找到，继续处理
	}

	if record == nil {
		return nil, nil
	}

	// 检查请求是否一致
	requestHash := hashRequest(req)
	if record.RequestHash != requestHash {
		return nil, errors.New(errors.IDEMPOTENCY_KEY_MISMATCH, "idempotency key used with different request")
	}

	// 返回缓存的响应
	if record.Status == 1 && record.TxID != nil { // 已完成
		payment, err := s.paymentRepo.GetByTxID(ctx, *record.TxID)
		if err == nil && payment != nil {
			return s.toResponse(payment), nil
		}
	}

	return nil, nil
}

// recordIdempotency 记录幂等性
func (s *PaymentApplicationService) recordIdempotency(ctx context.Context, key, txID string, status int8,
	req *dto.InitiatePaymentRequest, resp *dto.InitiatePaymentResponse) error {

	record := &entity.PaymentIdempotency{
		IdempotencyKey: key,
		RequestHash:    hashRequest(req),
		Status:         status,
		ExpiresAt:      time.Now().Add(30 * time.Minute),
		CreatedAt:      time.Now(),
	}

	if txID != "" {
		record.TxID = &txID
	}

	if resp != nil {
		// TODO: 序列化响应数据
		_ = resp
	}

	return s.idempotencyRepo.Create(ctx, record)
}

// hashRequest 生成请求哈希（简化实现）
func hashRequest(req *dto.InitiatePaymentRequest) string {
	// 实际应该使用更可靠的哈希算法
	data := fmt.Sprintf("%s|%s|%s|%s|%s|%d|%s",
		req.AgentDID, req.SkillDID, req.AmountStr, req.Currency,
		req.Signature, req.Timestamp, req.Nonce)
	return data
}

// toResponse 转换为响应
func (s *PaymentApplicationService) toResponse(payment *entity.Payment) *dto.InitiatePaymentResponse {
	resp := &dto.InitiatePaymentResponse{
		TxID:      payment.TxID,
		Status:    constants.PaymentStatusToString(payment.Status),
		CreatedAt: payment.CreatedAt.Format(constants.TimeFormatISO8601),
	}

	if payment.TxHash != "" {
		resp.TxHash = payment.TxHash
	}

	if payment.ConfirmedAt != nil {
		resp.ConfirmedAt = payment.ConfirmedAt.Format(constants.TimeFormatISO8601)
	}

	return resp
}

// toStatusResponse 转换为状态响应
func (s *PaymentApplicationService) toStatusResponse(payment *entity.Payment) *dto.GetPaymentStatusResponse {
	resp := &dto.GetPaymentStatusResponse{
		TxID:      payment.TxID,
		AgentDID:  payment.AgentDID,
		SkillDID:  payment.SkillDID,
		Amount:    utils.MinorUnitToString(payment.AmountMinor),
		Currency:  string(payment.Currency),
		Status:    constants.PaymentStatusToString(payment.Status),
		CreatedAt: payment.CreatedAt.Format(constants.TimeFormatISO8601),
	}

	if payment.TxHash != "" {
		resp.TxHash = payment.TxHash
	}

	if payment.ConfirmedAt != nil {
		resp.ConfirmedAt = payment.ConfirmedAt.Format(constants.TimeFormatISO8601)
	}

	return resp
}

// generateTxID 生成交易ID
func generateTxID() string {
	return uuid.New().String()
}

// extractWalletFromDID 从 DID 提取钱包地址
func extractWalletFromDID(did string) string {
	// 简化实现：假设 did:solana:{wallet_address}
	parts := make([]rune, 0, len(did))
	count := 0
	for _, c := range did {
		if c == ':' {
			count++
			if count == 2 {
				continue
			}
		}
		if count >= 2 {
			parts = append(parts, c)
		}
	}
	return string(parts)
}

// parseStatus 解析状态字符串
func parseStatus(s string) int8 {
	switch s {
	case "CREATED":
		return 0
	case "PENDING":
		return 1
	case "CONFIRMED":
		return 2
	case "COMPLETED":
		return 3
	case "FAILED":
		return 4
	case "CANCELLED":
		return 5
	}
	return -1
}
