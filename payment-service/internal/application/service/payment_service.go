// Package service 应用服务层
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
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
		currency constants.Currency, signedTxBase64 string) (txHash string, err error)
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

	// 日志
	logger *zap.Logger
}

// PaymentConfig 支付服务配置
type PaymentConfig struct {
	TimeoutMinutes      int
	MaxRetryCount       int
	MaxAmountMinor      int64
	PollIntervalSeconds int
	MaxPollCount        int

	// TreasuryWalletAddress 系统金库钱包。X 注册奖励从此地址向用户钱包发 USDC。
	// 为空时 RegisterXRegistrationReward 直接返回 INTERNAL_SERVER_ERROR。
	TreasuryWalletAddress string

	// XRegistrationRewardMinor 单笔 X 注册奖励的 USDC 最小单位数(已 * 10^6)。
	// 启动时由 config.XRegistrationRewardUsdc 转换得到。
	XRegistrationRewardMinor int64

	// InternalApiKey internal 端点鉴权密钥。空时 handler 直接 403。
	InternalApiKey string
}

// ExpectedInternalAPIKey 返回 internal 端点鉴权密钥(供 handler 校验 X-Internal-Api-Key header)。
func (s *PaymentApplicationService) ExpectedInternalAPIKey() string {
	return s.config.InternalApiKey
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

// RegisterXRegistrationReward X 账号注册奖励:treasury -> 用户钱包发 USDC。
// 流程与 InitiatePayment 类似,但没有 payer 签名,Sender 是系统金库。
// idempotency_key 由 verification-service 提供,格式: "x-reward-<agent_did>-<tweet_id>"。
func (s *PaymentApplicationService) RegisterXRegistrationReward(
	ctx context.Context, req *dto.XRegistrationRewardRequest,
) (*dto.XRegistrationRewardResponse, error) {

	// 1. treasury 必填(配置错误)
	if s.config.TreasuryWalletAddress == "" {
		return nil, errors.New(errors.INTERNAL_SERVER_ERROR,
			"treasury wallet not configured; set PaymentConfig.TreasuryWalletAddress or TREASURY_WALLET_ADDRESS env")
	}

	// 2. 解析金额 + 币种
	amountMinor, err := utils.StringToMinorUnit(req.AmountStr)
	if err != nil {
		return nil, errors.Wrap(errors.INVALID_PARAMETERS, err, "invalid amount format")
	}
	if amountMinor <= 0 {
		return nil, errors.New(errors.INVALID_PARAMETERS, "reward amount must be positive")
	}
	if req.Currency != "USDC" {
		return nil, errors.New(errors.INVALID_PARAMETERS, "only USDC supported for x registration reward")
	}

	// 3. 决定 recipient wallet: 优先用请求里的 wallet_address,否则从 DID 提取
	recipientWallet := strings.TrimSpace(req.WalletAddress)
	if recipientWallet == "" {
		recipientWallet = extractWalletFromDID(req.AgentDID)
	}
	if recipientWallet == "" {
		return nil, errors.New(errors.INVALID_PARAMETERS, "wallet_address or valid agent_did required")
	}

	// 4. 幂等性 storage key —— 与 /pay 名字空间隔离(用 "x-registration" scope)
	storageKey := s.idempotencyGen.Generate(req.AgentDID, "x-registration", req.IdempotencyKey)
	requestHash := hashRewardRequest(req)

	cached, found, idemErr := s.checkIdempotencyByHash(ctx, storageKey, requestHash)
	if idemErr != nil {
		return nil, idemErr
	}
	if found && cached != nil {
		s.logger.Info("XRegistrationReward idempotent replay",
			zap.String("agent_did", req.AgentDID),
			zap.String("tweet_id", req.TweetID),
			zap.String("tx_id", cached.TxID),
		)
		return cached, nil
	}

	// 5. reason 兜底
	reason := req.Reason
	if reason == "" {
		reason = constants.RewardPurposeXRegistration
	}

	// 6. 创建 Payment 实体,用哨兵值占位 Signature / SkillDID
	txID := generateTxID()
	payment, err := entity.NewPayment(
		txID,
		req.AgentDID,
		constants.SkillDidXRegistrationReward,
		amountMinor,
		constants.CurrencyUSDC,
		constants.SignatureInternalReward,
		time.Now().Unix(),
		req.IdempotencyKey, // 复用 verification 提供的全局唯一 key 作为 SignNonce
		s.config.TimeoutMinutes,
	)
	if err != nil {
		return nil, err
	}
	payment.RewardPurpose = reason

	if err := s.paymentRepo.Create(ctx, payment); err != nil {
		return nil, errors.Wrap(errors.DATABASE_CONNECTION_ERROR, err,
			"failed to create reward payment record")
	}

	// 7. 同步调 chain: treasury -> recipient
	txHash, err := s.blockchainExec.ExecuteTransfer(
		ctx,
		s.config.TreasuryWalletAddress,
		recipientWallet,
		amountMinor,
		constants.CurrencyUSDC,
		"", // 空 signedTxBase64,让 blockchain-adapter 的 hot wallet 作 fee-payer
	)
	if err != nil {
		s.logger.Error("XRegistrationReward ExecuteTransfer failed",
			zap.String("tx_id", txID),
			zap.String("from_wallet", s.config.TreasuryWalletAddress),
			zap.String("to_wallet", recipientWallet),
			zap.String("tweet_id", req.TweetID),
			zap.Error(err),
		)
		_ = payment.MarkAsFailed("BLOCKCHAIN_ERROR", err.Error())
		_ = s.paymentRepo.Update(ctx, payment)
		s.publishEvent(ctx, payment)
		_ = s.recordIdempotencyByHash(ctx, storageKey, requestHash, txID, 2, "")
		return nil, errors.Wrap(errors.BLOCKCHAIN_NETWORK_ERROR, err, "treasury transfer failed")
	}

	// 8. 转 PENDING,起后台轮询
	if err := payment.MarkAsPending(txHash); err != nil {
		s.logger.Error("XRegistrationReward MarkAsPending failed", zap.Error(err))
	}
	if err := s.paymentRepo.Update(ctx, payment); err != nil {
		s.logger.Error("XRegistrationReward Update failed", zap.Error(err))
	}

	go s.pollTxStatus(payment.TxID, txHash)

	// 9. 写 PENDING 幂等记录
	_ = s.recordIdempotencyByHash(ctx, storageKey, requestHash, txID, 0, "")

	resp := toRewardResponse(payment, s.config.TreasuryWalletAddress, false)
	s.logger.Info("XRegistrationReward submitted",
		zap.String("tx_id", txID),
		zap.String("tx_hash", txHash),
		zap.String("from_wallet", s.config.TreasuryWalletAddress),
		zap.String("to_wallet", recipientWallet),
		zap.String("agent_did", req.AgentDID),
		zap.String("tweet_id", req.TweetID),
		zap.String("x_handle", req.XHandle),
		zap.Int64("amount_minor", amountMinor),
	)
	return resp, nil
}

// toRewardResponse 把 Payment 装配成 XRegistrationRewardResponse。
// alreadyPaid=true 用于幂等命中,直接告诉 verification 已经发过。
// 必须传 treasuryWallet(由 service 持有,放这里避免 handler 重复覆盖)。
func toRewardResponse(payment *entity.Payment, treasuryWallet string, alreadyPaid bool) *dto.XRegistrationRewardResponse {
	resp := &dto.XRegistrationRewardResponse{
		TxID:            payment.TxID,
		Status:          constants.PaymentStatusToString(payment.Status),
		Amount:          utils.MinorUnitToString(payment.AmountMinor),
		Currency:        vo.CommonCurrencyToString(payment.Currency),
		AgentDID:        payment.AgentDID,
		RecipientWallet: extractWalletFromDID(payment.AgentDID),
		FromWallet:      treasuryWallet,
		CreatedAt:       payment.CreatedAt.Format(constants.TimeFormatISO8601),
		IdempotencyKey:  payment.SignNonce,
		AlreadyPaid:     alreadyPaid,
	}
	if payment.TxHash != "" {
		resp.TxHash = payment.TxHash
	}
	if payment.ConfirmedAt != nil {
		resp.ConfirmedAt = payment.ConfirmedAt.Format(constants.TimeFormatISO8601)
	}
	return resp
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

	signData := vo.PaymentBusinessSignPayload(req.AgentDID, req.SkillDID, amountMinor, currency, req.SignedTxBase64, req.Timestamp, req.Nonce)
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
	txHash, err := s.blockchainExec.ExecuteTransfer(ctx, walletAddress, skillWallet, amountMinor, currency, req.SignedTxBase64)
	if err != nil {
		signedLen := 0
		if req.SignedTxBase64 != "" {
			signedLen = len(req.SignedTxBase64)
		}
		s.logger.Error("blockchain ExecuteTransfer failed",
			zap.String("tx_id", txID),
			zap.String("from_wallet", walletAddress),
			zap.String("to_wallet", skillWallet),
			zap.Int64("amount_minor", amountMinor),
			zap.String("currency", string(currency)),
			zap.Int("signed_tx_base64_len", signedLen),
			zap.Error(err),
		)
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
	// 优先使用 amount，如果不存在则使用 price（向后兼容）
	var price string
	if req.Amount != "" {
		if _, err := utils.StringToMinorUnit(req.Amount); err != nil {
			return nil, false, errors.Wrap(errors.INVALID_PARAMETERS, err, "invalid amount format")
		}
		price = req.Amount
	} else if req.Price != "" {
		if _, err := utils.StringToMinorUnit(req.Price); err != nil {
			return nil, false, errors.Wrap(errors.INVALID_PARAMETERS, err, "invalid price format")
		}
		price = req.Price
	} else {
		return nil, false, errors.New(errors.INVALID_PARAMETERS, "amount or price is required")
	}

	currency := "USDC"
	if req.Currency != "" {
		currency = req.Currency
	}

	message := "Payment required to access this skill"
	if req.Message != "" {
		message = req.Message
	}

	return &dto.PaymentRequirementResponse{
		SkillDID:  req.SkillDID,
		SkillName: req.SkillName,
		Price:     price,
		Currency:  currency,
		Message:   message,
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

	// 奖励事件打上 event_type / reward_purpose,方便下游按 tag 过滤
	if payment.RewardPurpose != "" {
		event.EventType = constants.MQTagRewardGranted
		event.RewardPurpose = payment.RewardPurpose
		event.FromWallet = s.config.TreasuryWalletAddress
		event.ToWallet = extractWalletFromDID(payment.AgentDID)
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

	reqHash := hashRequest(req)
	expires := time.Now().Add(30 * time.Minute)

	existing, err := s.idempotencyRepo.Get(ctx, key)
	if err != nil {
		return err
	}
	if existing != nil {
		existing.RequestHash = reqHash
		existing.Status = status
		existing.ExpiresAt = expires
		if txID != "" {
			existing.TxID = &txID
		}
		if resp != nil {
			_ = resp
		}
		return s.idempotencyRepo.Update(ctx, existing)
	}

	record := &entity.PaymentIdempotency{
		IdempotencyKey: key,
		RequestHash:    reqHash,
		Status:         status,
		ExpiresAt:      expires,
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
	data := fmt.Sprintf("%s|%s|%s|%s|%s|%d|%s|%s",
		req.AgentDID, req.SkillDID, req.AmountStr, req.Currency,
		req.Signature, req.Timestamp, req.Nonce, req.SignedTxBase64)
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

// hashRewardRequest 生成 X 注册奖励请求的 SHA256 哈希。
// 与 hashRequest 解耦,避免对 *dto.InitiatePaymentRequest 的依赖。
func hashRewardRequest(req *dto.XRegistrationRewardRequest) string {
	data := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s",
		req.AgentDID, req.WalletAddress, req.TweetID, req.XHandle,
		req.AmountStr, req.Currency, req.IdempotencyKey)
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

// checkIdempotencyByHash 通用版幂等性查询。
// 返回 (已缓存响应, true=已存在) 或 (nil, false=需新建)。
// 命中且状态为 COMPLETED 时返回原 Payment 的响应;PENDING 状态视为"进行中",调用方继续处理。
func (s *PaymentApplicationService) checkIdempotencyByHash(
	ctx context.Context, key, requestHash string,
) (*dto.XRegistrationRewardResponse, bool, error) {

	record, err := s.idempotencyRepo.Get(ctx, key)
	if err != nil {
		// 任何错误都视为"未找到",继续后续处理(与 checkIdempotency 保持一致)
		return nil, false, nil
	}
	if record == nil {
		return nil, false, nil
	}
	if record.RequestHash != requestHash {
		return nil, false, errors.New(errors.IDEMPOTENCY_KEY_MISMATCH,
			"idempotency key used with different request")
	}
	if record.Status == 1 && record.TxID != nil {
		payment, getErr := s.paymentRepo.GetByTxID(ctx, *record.TxID)
		if getErr == nil && payment != nil {
			return toRewardResponse(payment, s.config.TreasuryWalletAddress, true), true, nil
		}
	}
	return nil, false, nil
}

// recordIdempotencyByHash 通用版幂等性记录。responseJSON 可为空。
func (s *PaymentApplicationService) recordIdempotencyByHash(
	ctx context.Context, key, requestHash, txID string, status int8, responseJSON string,
) error {
	expires := time.Now().Add(30 * time.Minute)

	existing, err := s.idempotencyRepo.Get(ctx, key)
	if err != nil {
		// not-found 视为新建,其他错误往上抛
		if e, ok := err.(*errors.Error); ok && e.Code == errors.RESOURCE_NOT_FOUND {
			existing = nil
		} else {
			return err
		}
	}
	if existing != nil {
		existing.RequestHash = requestHash
		existing.Status = status
		existing.ExpiresAt = expires
		if txID != "" {
			existing.TxID = &txID
		}
		if responseJSON != "" {
			existing.ResponseData = responseJSON
		}
		return s.idempotencyRepo.Update(ctx, existing)
	}

	record := &entity.PaymentIdempotency{
		IdempotencyKey: key,
		RequestHash:    requestHash,
		Status:         status,
		ExpiresAt:      expires,
		CreatedAt:      time.Now(),
	}
	if txID != "" {
		record.TxID = &txID
	}
	if responseJSON != "" {
		record.ResponseData = responseJSON
	}
	return s.idempotencyRepo.Create(ctx, record)
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
