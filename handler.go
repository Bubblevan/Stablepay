package main

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/kitex/pkg/klog"
	"github.com/google/uuid"
	"github.com/stablepay/payment-service/internal/domain/entity"
	"github.com/stablepay/payment-service/internal/domain/repository"
	"github.com/stablepay/payment-service/internal/domain/service"
	"github.com/stablepay/payment-service/internal/domain/vo"
	"github.com/stablepay/payment-service/internal/infrastructure/config"
	"github.com/stablepay/payment-service/internal/infrastructure/mysql"
	"github.com/stablepay/payment-service/kitex_gen/stablepay/common"
	"github.com/stablepay/payment-service/kitex_gen/stablepay/payment_service"
	"github.com/stablepay/payment-service/pkg/constants"
	"github.com/stablepay/payment-service/pkg/errors"
)

// PaymentServiceImpl implements the PaymentService interface defined in the IDL.
type PaymentServiceImpl struct {
	// 仓库
	paymentRepo     repository.PaymentRepository
	idempotencyRepo repository.PaymentIdempotencyRepository

	// 领域服务
	paymentValidator *service.PaymentValidator
	nonceChecker     *service.NonceChecker
	idempotencyGen   *service.IdempotencyKeyGenerator

	// 基础设施
	blockchainExec BlockchainExecutor
	eventPublisher EventPublisher

	// 配置
	config *PaymentConfig
}

// PaymentConfig 支付服务配置
type PaymentConfig struct {
	TimeoutMinutes      int
	MaxRetryCount       int
	MaxAmountMinor      int64
	PollIntervalSeconds int
	MaxPollCount        int
}

// BlockchainExecutor 区块链执行器接口
type BlockchainExecutor interface {
	ExecuteTransfer(ctx context.Context, fromWallet, toWallet string, amountMinor int64,
		currency constants.Currency) (txHash string, err error)
	QueryTxStatus(ctx context.Context, txHash string) (status int8, confirmedAt *time.Time, err error)
}

// EventPublisher 事件发布器接口
type EventPublisher interface {
	PublishPaymentEvent(ctx context.Context, event *PaymentEvent) error
}

// PaymentEvent MQ 支付事件
type PaymentEvent struct {
	TxID        string
	AgentDID    string
	SkillDID    string
	AmountMinor int64
	Currency    string
	TxHash      string
	Status      string
	Timestamp   int64
	ErrorCode   string
	ErrorMsg    string
}

// NewPaymentServiceImpl 创建 PaymentServiceImpl 实例
func NewPaymentServiceImpl() *PaymentServiceImpl {
	// 初始化数据库连接
	db, err := mysql.NewDB(config.Get().MySQL)
	if err != nil {
		klog.Fatalf("failed to init database: %v", err)
	}

	// 初始化仓库
	paymentRepo := repository.NewPaymentRepository(db)
	idempotencyRepo := repository.NewPaymentIdempotencyRepository(db)

	// 初始化领域服务
	paymentValidator := service.NewPaymentValidator()
	nonceChecker := service.NewNonceChecker()

	return &PaymentServiceImpl{
		paymentRepo:      paymentRepo,
		idempotencyRepo:  idempotencyRepo,
		paymentValidator: paymentValidator,
		nonceChecker:     nonceChecker,
		idempotencyGen:   service.NewIdempotencyKeyGenerator(),
		config: &PaymentConfig{
			TimeoutMinutes:      30,
			MaxRetryCount:       3,
			MaxAmountMinor:      1000000000000, // 100万 USDC
			PollIntervalSeconds: 5,
			MaxPollCount:        60, // 最多轮询5分钟
		},
	}
}

// InitiatePayment 发起支付
func (s *PaymentServiceImpl) InitiatePayment(ctx context.Context, req *payment_service.InitiatePaymentRequest) (resp *payment_service.InitiatePaymentResponse, err error) {
	// 1. 参数校验
	if err := s.validateInitiatePaymentRequest(req); err != nil {
		return nil, err
	}

	// 2. 构建幂等性键
	idempotencyKey := s.idempotencyGen.Generate(string(req.AgentDid), string(req.SkillDid), "")

	// 3. 检查幂等性
	existingResp, err := s.checkIdempotency(ctx, idempotencyKey, req)
	if err != nil {
		return nil, err
	}
	if existingResp != nil {
		return existingResp, nil
	}

	// 4. 构建值对象
	amount, err := vo.NewAmount(req.AmountMinor, vo.ThriftCurrencyToCommon(req.Currency))
	if err != nil {
		s.recordIdempotency(ctx, idempotencyKey, "", 2, req, nil)
		return nil, errors.Wrap(errors.INVALID_PARAMETERS, err, "invalid amount")
	}

	var signature *vo.Signature
	if req.Signature != nil && req.Timestamp != nil {
		signData := vo.GetSignData(string(req.AgentDid), string(req.SkillDid), req.AmountMinor,
			vo.ThriftCurrencyToCommon(req.Currency), 0, "") // Thrift 定义中没有 timestamp_ms 和 nonce，简化处理
		signature = vo.NewSignature(*req.Signature, 0, "", signData)
	}

	// 5. 验证支付请求
	if err := s.paymentValidator.ValidatePaymentRequest(ctx, string(req.AgentDid), string(req.SkillDid), amount, signature); err != nil {
		s.recordIdempotency(ctx, idempotencyKey, "", 2, req, nil)
		return nil, err
	}

	// 6. 检查余额（通过 Blockchain Service）
	walletAddress := extractWalletFromDID(string(req.AgentDid))
	if err := s.paymentValidator.CheckBalance(ctx, walletAddress, vo.ThriftCurrencyToCommon(req.Currency), req.AmountMinor); err != nil {
		s.recordIdempotency(ctx, idempotencyKey, "", 2, req, nil)
		return nil, err
	}

	// 7. 创建支付记录
	txID := generateTxID()
	payment, err := entity.NewPayment(txID, string(req.AgentDid), string(req.SkillDid), req.AmountMinor,
		vo.ThriftCurrencyToCommon(req.Currency), "", 0, "", s.config.TimeoutMinutes)
	if err != nil {
		return nil, err
	}

	if err := s.paymentRepo.Create(ctx, payment); err != nil {
		return nil, errors.Wrap(errors.DATABASE_CONNECTION_ERROR, err, "failed to create payment record")
	}

	// 8. 记录幂等性（进行中）
	if err := s.recordIdempotency(ctx, idempotencyKey, txID, 0, req, nil); err != nil {
		klog.Warn("failed to record idempotency", err)
	}

	// 9. 执行链上交易
	skillWallet := extractWalletFromDID(string(req.SkillDid))
	txHash, err := s.blockchainExec.ExecuteTransfer(ctx, walletAddress, skillWallet, req.AmountMinor,
		vo.ThriftCurrencyToCommon(req.Currency))
	if err != nil {
		_ = payment.MarkAsFailed("BLOCKCHAIN_ERROR", err.Error())
		_ = s.paymentRepo.Update(ctx, payment)
		s.publishEvent(ctx, payment)
		s.recordIdempotency(ctx, idempotencyKey, txID, 2, req, s.toResponse(payment))
		return nil, errors.Wrap(errors.BLOCKCHAIN_NETWORK_ERROR, err, "blockchain transaction failed")
	}

	// 10. 更新为交易中状态
	if err := payment.MarkAsPending(txHash); err != nil {
		klog.Error("failed to transition status", err)
	}
	if err := s.paymentRepo.Update(ctx, payment); err != nil {
		klog.Error("failed to update payment status", err)
	}

	// 11. 异步轮询链上状态
	go s.pollTxStatus(payment.TxID, txHash)

	// 12. 返回响应
	resp = s.toResponse(payment)
	s.recordIdempotency(ctx, idempotencyKey, txID, 0, req, resp)

	return resp, nil
}

// GetPaymentStatus 查询支付状态
func (s *PaymentServiceImpl) GetPaymentStatus(ctx context.Context, req *payment_service.GetPaymentStatusRequest) (resp *payment_service.GetPaymentStatusResponse, err error) {
	if req.TxId == "" {
		return nil, errors.New(errors.INVALID_PARAMETERS, "tx_id is required")
	}

	payment, err := s.paymentRepo.GetByTxID(ctx, string(req.TxId))
	if err != nil {
		return nil, errors.Wrap(errors.RESOURCE_NOT_FOUND, err, "payment not found")
	}

	return s.toStatusResponse(payment), nil
}

// ListPaymentHistory 查询支付历史
func (s *PaymentServiceImpl) ListPaymentHistory(ctx context.Context, req *payment_service.ListPaymentHistoryRequest) (resp *payment_service.ListPaymentHistoryResponse, err error) {
	if req.AgentDid == "" {
		return nil, errors.New(errors.INVALID_PARAMETERS, "agent_did is required")
	}

	// 分页参数
	page := int32(1)
	pageSize := int32(constants.DefaultPageSize)
	if req.Page != nil {
		if req.Page.Limit > 0 {
			pageSize = req.Page.Limit
		}
		if req.Page.Offset > 0 {
			page = req.Page.Offset
		}
	}
	offset := int((page - 1) * pageSize)
	limit := int(pageSize)

	payments, total, err := s.paymentRepo.ListByAgentDID(ctx, string(req.AgentDid), nil, offset, limit)
	if err != nil {
		return nil, errors.Wrap(errors.DATABASE_CONNECTION_ERROR, err, "failed to query payment history")
	}

	items := make([]*payment_service.PaymentHistoryItem, len(payments))
	for i, p := range payments {
		items[i] = &payment_service.PaymentHistoryItem{
			TxId:        p.TxID,
			SkillDid:    p.SkillDID,
			AmountMinor: p.AmountMinor,
			Currency:    vo.CommonCurrencyToThrift(p.Currency),
			Status:      vo.CommonStatusToThrift(p.Status),
			CreatedAt:   p.CreatedAt.Format(constants.TimeFormatISO8601),
		}
	}

	return &payment_service.ListPaymentHistoryResponse{
		Base: &common.BaseResp{
			Code:    int32(constants.ErrorCodeSuccess),
			Message: "success",
		},
		Items: items,
		Page: &common.PageResult_{
			Total: int32(total),
		},
	}, nil
}

// validateInitiatePaymentRequest 验证发起支付请求
func (s *PaymentServiceImpl) validateInitiatePaymentRequest(req *payment_service.InitiatePaymentRequest) error {
	if req.AgentDid == "" {
		return errors.New(errors.INVALID_PARAMETERS, "agent_did is required")
	}
	if req.SkillDid == "" {
		return errors.New(errors.INVALID_PARAMETERS, "skill_did is required")
	}
	if req.AmountMinor <= 0 {
		return errors.New(errors.INVALID_PARAMETERS, "amount_minor must be positive")
	}
	if req.Currency != common.Currency_USDC && req.Currency != common.Currency_USDT {
		return errors.New(errors.INVALID_PARAMETERS, "invalid currency")
	}
	return nil
}

// checkIdempotency 检查幂等性
func (s *PaymentServiceImpl) checkIdempotency(ctx context.Context, key string, req *payment_service.InitiatePaymentRequest) (*payment_service.InitiatePaymentResponse, error) {
	record, err := s.idempotencyRepo.Get(ctx, key)
	if err != nil {
		return nil, nil // 未找到，继续处理
	}

	if record == nil {
		return nil, nil
	}

	// 检查请求是否一致（简化处理）
	requestHash := hashRequest(req)
	if record.RequestHash != requestHash {
		return nil, errors.New(errors.INVALID_PARAMETERS, "idempotency key used with different request")
	}

	// 返回缓存的响应
	if record.Status == 1 && record.TxID != nil {
		payment, err := s.paymentRepo.GetByTxID(ctx, *record.TxID)
		if err == nil && payment != nil {
			return s.toResponse(payment), nil
		}
	}

	return nil, nil
}

// recordIdempotency 记录幂等性
func (s *PaymentServiceImpl) recordIdempotency(ctx context.Context, key, txID string, status int8,
	req *payment_service.InitiatePaymentRequest, resp *payment_service.InitiatePaymentResponse) error {

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

	return s.idempotencyRepo.Create(ctx, record)
}

// hashRequest 生成请求哈希（简化实现）
func hashRequest(req *payment_service.InitiatePaymentRequest) string {
	data := fmt.Sprintf("%s|%s|%d|%d",
		req.AgentDid, req.SkillDid, req.AmountMinor, req.Currency)
	return data
}

// pollTxStatus 轮询交易状态
func (s *PaymentServiceImpl) pollTxStatus(txID, txHash string) {
	ctx := context.Background()

	for i := 0; i < s.config.MaxPollCount; i++ {
		time.Sleep(time.Duration(s.config.PollIntervalSeconds) * time.Second)

		status, confirmedAt, err := s.blockchainExec.QueryTxStatus(ctx, txHash)
		if err != nil {
			klog.Errorf("failed to query tx status: tx_hash=%s, err=%v", txHash, err)
			continue
		}

		payment, err := s.paymentRepo.GetByTxID(ctx, txID)
		if err != nil {
			klog.Errorf("failed to get payment: tx_id=%s, err=%v", txID, err)
			return
		}

		switch status {
		case constants.PaymentStatusConfirmed:
			_ = payment.MarkAsConfirmed()
			if confirmedAt != nil {
				payment.ConfirmedAt = confirmedAt
			}
			_ = s.paymentRepo.Update(ctx, payment)
			_ = payment.MarkAsCompleted()
			_ = s.paymentRepo.Update(ctx, payment)
			s.publishEvent(ctx, payment)
			return

		case constants.PaymentStatusFailed:
			_ = payment.MarkAsFailed("BLOCKCHAIN_FAILED", "transaction failed on chain")
			_ = s.paymentRepo.Update(ctx, payment)
			s.publishEvent(ctx, payment)
			return

		case constants.PaymentStatusPending:
			continue
		}
	}

	// 超过最大轮询次数
	payment, _ := s.paymentRepo.GetByTxID(ctx, txID)
	if payment != nil {
		_ = payment.MarkAsFailed("POLL_TIMEOUT", "exceeded maximum poll count")
		_ = s.paymentRepo.Update(ctx, payment)
		s.publishEvent(ctx, payment)
	}
}

// publishEvent 发布支付事件
func (s *PaymentServiceImpl) publishEvent(ctx context.Context, payment *entity.Payment) {
	event := &PaymentEvent{
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
		klog.Errorf("failed to publish payment event: %v", err)
	}
}

// toResponse 转换为响应
func (s *PaymentServiceImpl) toResponse(payment *entity.Payment) *payment_service.InitiatePaymentResponse {
	resp := &payment_service.InitiatePaymentResponse{
		Base: &common.BaseResp{
			Code:    int32(constants.ErrorCodeSuccess),
			Message: "success",
		},
		TxId:      payment.TxID,
		Status:    vo.CommonStatusToThrift(payment.Status),
		CreatedAt: payment.CreatedAt.Format(constants.TimeFormatISO8601),
	}

	if payment.TxHash != "" {
		resp.TxHash = payment.TxHash
	}

	if payment.ConfirmedAt != nil {
		confirmedAt := payment.ConfirmedAt.Format(constants.TimeFormatISO8601)
		resp.ConfirmedAt = &confirmedAt
	}

	return resp
}

// toStatusResponse 转换为状态响应
func (s *PaymentServiceImpl) toStatusResponse(payment *entity.Payment) *payment_service.GetPaymentStatusResponse {
	resp := &payment_service.GetPaymentStatusResponse{
		Base: &common.BaseResp{
			Code:    int32(constants.ErrorCodeSuccess),
			Message: "success",
		},
		TxId:   payment.TxID,
		Status: vo.CommonStatusToThrift(payment.Status),
	}

	if payment.TxHash != "" {
		resp.TxHash = &payment.TxHash
	}

	if payment.ConfirmedAt != nil {
		confirmedAt := payment.ConfirmedAt.Format(constants.TimeFormatISO8601)
		resp.ConfirmedAt = &confirmedAt
	}

	return resp
}

// generateTxID 生成交易ID
func generateTxID() string {
	return uuid.New().String()
}

// extractWalletFromDID 从 DID 提取钱包地址
func extractWalletFromDID(did string) string {
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
