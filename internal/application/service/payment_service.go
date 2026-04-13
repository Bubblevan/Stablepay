// Package service 搴旂敤鏈嶅姟灞?
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// BlockchainExecutor 鍖哄潡閾炬墽琛屽櫒鎺ュ彛
type BlockchainExecutor interface {
	// ExecuteTransfer 鎵ц杞处浜ゆ槗
	ExecuteTransfer(ctx context.Context, fromWallet, toWallet string, amountMinor int64,
		currency constants.Currency, signedTxBase64 string) (txHash string, err error)
	// QueryTxStatus 鏌ヨ浜ゆ槗鐘舵€?
	QueryTxStatus(ctx context.Context, txHash string) (status int8, confirmedAt *time.Time, err error)
}

// EventPublisher 浜嬩欢鍙戝竷鍣ㄦ帴鍙?
type EventPublisher interface {
	// PublishPaymentEvent 鍙戝竷鏀粯浜嬩欢
	PublishPaymentEvent(ctx context.Context, event *dto.MQPaymentEvent) error
}

// PaymentApplicationService 鏀粯搴旂敤鏈嶅姟
type PaymentApplicationService struct {
	// 浠撳簱
	paymentRepo     repository.PaymentRepository
	idempotencyRepo repository.PaymentIdempotencyRepository

	// 棰嗗煙鏈嶅姟
	paymentValidator *service.PaymentValidator
	nonceChecker     *service.NonceChecker
	idempotencyGen   *service.IdempotencyKeyGenerator

	// 鍩虹璁炬柦
	blockchainExec BlockchainExecutor
	eventPublisher EventPublisher

	// 閰嶇疆
	config *PaymentConfig

	// 鏃ュ織
	logger *zap.Logger
}

// PaymentConfig 鏀粯鏈嶅姟閰嶇疆
type PaymentConfig struct {
	TimeoutMinutes      int
	MaxRetryCount       int
	MaxAmountMinor      int64
	PollIntervalSeconds int
	MaxPollCount        int
}

// NewPaymentApplicationService 鍒涘缓鏀粯搴旂敤鏈嶅姟
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

// InitiatePayment 鍙戣捣鏀粯
func (s *PaymentApplicationService) InitiatePayment(ctx context.Context, req *dto.InitiatePaymentRequest) (*dto.InitiatePaymentResponse, error) {
	// 1. 閲戦鏍煎紡杞崲锛堝瓧绗︿覆 -> 鏈€灏忓崟浣嶆暣鏁帮級
	amountMinor, err := utils.StringToMinorUnit(req.AmountStr)
	if err != nil {
		return nil, errors.Wrap(errors.INVALID_PARAMETERS, err, "invalid amount format")
	}

	currency := vo.StringToCommonCurrency(req.Currency)

	// 2. 鏋勫缓骞傜瓑鎬ч敭
	idempotencyKey := s.idempotencyGen.Generate(req.AgentDID, req.SkillDID, req.IdempotencyKey)

	// 3. 妫€鏌ュ箓绛夋€?
	existingResp, err := s.checkIdempotency(ctx, idempotencyKey, req)
	if err != nil {
		return nil, err
	}
	if existingResp != nil {
		return existingResp, nil
	}

	// 4. 闃查噸鏀炬鏌ワ紙nonce锛?
	if err := s.nonceChecker.CheckAndRecord(ctx, req.Nonce); err != nil {
		return nil, err
	}

	// 5. 鏋勫缓鍊煎璞?
	amount, err := vo.NewAmount(amountMinor, currency)
	if err != nil {
		return nil, err
	}

	signData := vo.PaymentBusinessSignPayload(req.AgentDID, req.SkillDID, amountMinor, currency, req.SignedTxBase64, req.Timestamp, req.Nonce)
	signature := vo.NewSignature(req.Signature, req.Timestamp, req.Nonce, signData)

	// 6. 楠岃瘉鏀粯璇锋眰锛堢鍚嶃€侀噾棰濈瓑锛?
	if err := s.paymentValidator.ValidatePaymentRequest(ctx, req.AgentDID, req.SkillDID, amount, signature); err != nil {
		s.recordIdempotency(ctx, idempotencyKey, "", 2, req, nil) // 璁板綍澶辫触
		return nil, err
	}

	// 7. 妫€鏌ヤ綑棰?
	// 娉ㄦ剰锛氳繖閲岄渶瑕佽幏鍙?Agent 鐨勯挶鍖呭湴鍧€锛屽疄闄呴€氳繃 DID Service 鏌ヨ
	// 绠€鍖栧鐞嗭細鍋囪 DID 涓殑鍏挜灏辨槸閽卞寘鍦板潃
	walletAddress := extractWalletFromDID(req.AgentDID)
	if err := s.paymentValidator.CheckBalance(ctx, walletAddress, currency, amountMinor); err != nil {
		s.recordIdempotency(ctx, idempotencyKey, "", 2, req, nil)
		return nil, err
	}

	// 8. 鍒涘缓鏀粯璁板綍
	txID := generateTxID()
	payment, err := entity.NewPayment(txID, req.AgentDID, req.SkillDID, amountMinor, currency,
		req.Signature, req.Timestamp, req.Nonce, s.config.TimeoutMinutes)
	if err != nil {
		return nil, err
	}

	if err := s.paymentRepo.Create(ctx, payment); err != nil {
		return nil, errors.Wrap(errors.DATABASE_CONNECTION_ERROR, err, "failed to create payment record")
	}

	// 9. 璁板綍骞傜瓑鎬э紙杩涜涓級
	if err := s.recordIdempotency(ctx, idempotencyKey, txID, 0, req, nil); err != nil {
		s.logger.Warn("failed to record idempotency", zap.Error(err))
	}

	// 10. 鎵ц閾句笂浜ゆ槗锛堝悓姝ワ級
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
		// 閾句笂鎵ц澶辫触
		_ = payment.MarkAsFailed("BLOCKCHAIN_ERROR", err.Error())
		_ = s.paymentRepo.Update(ctx, payment)
		s.publishEvent(ctx, payment)
		s.recordIdempotency(ctx, idempotencyKey, txID, 2, req, s.toResponse(payment))
		return nil, errors.Wrap(errors.BLOCKCHAIN_NETWORK_ERROR, err, "blockchain transaction failed")
	}

	// 11. 鏇存柊涓轰氦鏄撲腑鐘舵€?
	if err := payment.MarkAsPending(txHash); err != nil {
		s.logger.Error("failed to transition status", zap.Error(err))
	}
	if err := s.paymentRepo.Update(ctx, payment); err != nil {
		s.logger.Error("failed to update payment status", zap.Error(err))
	}

	// 12. 寮傛杞閾句笂鐘舵€侊紙鍚庡彴 goroutine锛?
	go s.pollTxStatus(payment.TxID, txHash)

	// 13. 杩斿洖鍝嶅簲
	resp := s.toResponse(payment)
	s.recordIdempotency(ctx, idempotencyKey, txID, 0, req, resp)

	return resp, nil
}

// GetPaymentStatus 鏌ヨ鏀粯鐘舵€?
func (s *PaymentApplicationService) GetPaymentStatus(ctx context.Context, txID string) (*dto.GetPaymentStatusResponse, error) {
	payment, err := s.paymentRepo.GetByTxID(ctx, txID)
	if err != nil {
		return nil, errors.Wrap(errors.RESOURCE_NOT_FOUND, err, "payment not found")
	}

	return s.toStatusResponse(payment), nil
}

// ListPaymentHistory 鏌ヨ鏀粯鍘嗗彶
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

// GetPaymentRequirement 鑾峰彇鏀粯瑕佹眰锛圚TTP 402锛?
func (s *PaymentApplicationService) GetPaymentRequirement(ctx context.Context, req *dto.GetPaymentRequirementRequest) (*dto.PaymentRequirementResponse, bool, error) {
	// 濡傛灉浼犲叆浜?agent_did锛屾鏌ユ槸鍚﹀凡璐拱
	if req.AgentDID != "" {
		count, err := s.paymentRepo.CountByAgentAndSkill(ctx, req.AgentDID, req.SkillDID)
		if err != nil {
			s.logger.Warn("failed to check purchase status", zap.Error(err))
		}
		if count > 0 {
			// 宸茶喘涔?
			return nil, true, nil
		}
	}

	// 杩斿洖鏀粯瑕佹眰锛圚TTP 402锛?
	price := "1.00"
	if req.Price != "" {
		if _, err := utils.StringToMinorUnit(req.Price); err != nil {
			return nil, false, errors.Wrap(errors.INVALID_PARAMETERS, err, "invalid price format")
		}
		price = req.Price
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

// pollTxStatus 杞浜ゆ槗鐘舵€?
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
			// 浜ゆ槗纭鎴愬姛
			_ = payment.MarkAsConfirmed()
			if confirmedAt != nil {
				payment.ConfirmedAt = confirmedAt
			}
			_ = s.paymentRepo.Update(ctx, payment)

			// 鏍囪涓哄畬鎴?
			_ = payment.MarkAsCompleted()
			_ = s.paymentRepo.Update(ctx, payment)

			// 鍙戝竷浜嬩欢
			s.publishEvent(ctx, payment)
			return

		case constants.PaymentStatusFailed:
			// 浜ゆ槗澶辫触
			_ = payment.MarkAsFailed("BLOCKCHAIN_FAILED", "transaction failed on chain")
			_ = s.paymentRepo.Update(ctx, payment)
			s.publishEvent(ctx, payment)
			return

		case constants.PaymentStatusPending:
			// 缁х画杞
			continue
		}
	}

	// 瓒呰繃鏈€澶ц疆璇㈡鏁帮紝鏍囪涓鸿秴鏃?
	payment, _ := s.paymentRepo.GetByTxID(ctx, txID)
	if payment != nil {
		_ = payment.MarkAsFailed("POLL_TIMEOUT", "exceeded maximum poll count")
		_ = s.paymentRepo.Update(ctx, payment)
		s.publishEvent(ctx, payment)
	}
}

// publishEvent 鍙戝竷鏀粯浜嬩欢
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

// checkIdempotency 妫€鏌ュ箓绛夋€?
func (s *PaymentApplicationService) checkIdempotency(ctx context.Context, key string, req *dto.InitiatePaymentRequest) (*dto.InitiatePaymentResponse, error) {
	record, err := s.idempotencyRepo.Get(ctx, key)
	if err != nil {
		return nil, nil // 鏈壘鍒帮紝缁х画澶勭悊
	}

	if record == nil {
		return nil, nil
	}

	// 妫€鏌ヨ姹傛槸鍚︿竴鑷?
	requestHash := hashRequest(req)
	if record.RequestHash != requestHash {
		return nil, errors.New(errors.IDEMPOTENCY_KEY_MISMATCH, "idempotency key used with different request")
	}

	// 杩斿洖缂撳瓨鐨勫搷搴?
	if record.Status == 1 && record.TxID != nil { // 宸插畬鎴?
		payment, err := s.paymentRepo.GetByTxID(ctx, *record.TxID)
		if err == nil && payment != nil {
			return s.toResponse(payment), nil
		}
	}

	return nil, nil
}

// recordIdempotency 璁板綍骞傜瓑鎬?
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
		// TODO: 搴忓垪鍖栧搷搴旀暟鎹?
		_ = resp
	}

	return s.idempotencyRepo.Create(ctx, record)
}

// hashRequest 鐢熸垚璇锋眰鍝堝笇锛堢畝鍖栧疄鐜帮級
func hashRequest(req *dto.InitiatePaymentRequest) string {
	// 瀹為檯搴旇浣跨敤鏇村彲闈犵殑鍝堝笇绠楁硶
	data := fmt.Sprintf("%s|%s|%s|%s|%s|%d|%s|%s",
		req.AgentDID, req.SkillDID, req.AmountStr, req.Currency,
		req.Signature, req.Timestamp, req.Nonce, req.SignedTxBase64)
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

// toResponse 杞崲涓哄搷搴?
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

// toStatusResponse 杞崲涓虹姸鎬佸搷搴?
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

// generateTxID 鐢熸垚浜ゆ槗ID
func generateTxID() string {
	return uuid.New().String()
}

// extractWalletFromDID 浠?DID 鎻愬彇閽卞寘鍦板潃
func extractWalletFromDID(did string) string {
	// 绠€鍖栧疄鐜帮細鍋囪 did:solana:{wallet_address}
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

// parseStatus 瑙ｆ瀽鐘舵€佸瓧绗︿覆
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
