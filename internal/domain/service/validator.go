// Package service 定义领域服务
package service

import (
	"context"
	"fmt"

	"github.com/stablepay/payment-service/internal/domain/vo"
	"github.com/stablepay/payment-service/pkg/constants"
	"github.com/stablepay/payment-service/pkg/errors"
)

// SignatureValidator 签名验证器接口
// 由基础设施层实现，调用 DID Service 进行验证
type SignatureValidator interface {
	// Validate 验证签名是否合法
	Validate(ctx context.Context, did string, message string, signature string, timestamp string, nonce string) (bool, error)
}

// BalanceChecker 余额检查器接口
// 由基础设施层实现，调用 Blockchain Adapter 查询余额
type BalanceChecker interface {
	// CheckBalance 检查钱包余额是否充足
	CheckBalance(ctx context.Context, walletAddress string, currency constants.Currency, requiredAmount int64) (bool, int64, error)
}

// PaymentValidator 支付验证领域服务
type PaymentValidator struct {
	sigValidator  SignatureValidator
	balanceChecker BalanceChecker
	maxAmountMinor int64
}

// NewPaymentValidator 创建支付验证服务
func NewPaymentValidator(sigValidator SignatureValidator, balanceChecker BalanceChecker, maxAmountMinor int64) *PaymentValidator {
	return &PaymentValidator{
		sigValidator:   sigValidator,
		balanceChecker: balanceChecker,
		maxAmountMinor: maxAmountMinor,
	}
}

// ValidatePaymentRequest 验证支付请求的完整合法性
func (v *PaymentValidator) ValidatePaymentRequest(ctx context.Context, agentDID, skillDID string,
	amount vo.Amount, signature vo.Signature) error {

	// 1. 验证金额有效性
	if err := v.validateAmount(amount); err != nil {
		return err
	}

	// 2. 验证签名未过期
	if signature.IsExpired(constants.SignatureTTLMinutes) {
		return errors.New(errors.SIGNATURE_VERIFICATION_FAILED, "signature has expired")
	}

	// 3. 验证签名合法性
	valid, err := v.sigValidator.Validate(ctx, agentDID, signature.SignData, signature.Value,
		fmt.Sprintf("%d", signature.Timestamp), signature.Nonce)
	if err != nil {
		return errors.Wrap(errors.SIGNATURE_VERIFICATION_FAILED, err, "failed to validate signature")
	}
	if !valid {
		return errors.New(errors.SIGNATURE_VERIFICATION_FAILED, "invalid signature")
	}

	return nil
}

// validateAmount 验证金额
func (v *PaymentValidator) validateAmount(amount vo.Amount) error {
	// 检查金额为正
	if !amount.IsPositive() {
		return errors.New(errors.INVALID_PARAMETERS, "payment amount must be positive")
	}

	// 检查金额不超过上限
	if amount.MinorUnit > v.maxAmountMinor {
		return errors.Newf(errors.PAYMENT_AMOUNT_EXCEEDED,
			"payment amount exceeds maximum limit of %s", constants.MaxAmount)
	}

	return nil
}

// CheckBalance 检查余额
func (v *PaymentValidator) CheckBalance(ctx context.Context, walletAddress string, currency constants.Currency, requiredAmount int64) error {
	sufficient, balance, err := v.balanceChecker.CheckBalance(ctx, walletAddress, currency, requiredAmount)
	if err != nil {
		return errors.Wrap(errors.BLOCKCHAIN_NETWORK_ERROR, err, "failed to check balance")
	}

	if !sufficient {
		return errors.Newf(errors.INSUFFICIENT_BALANCE,
			"insufficient balance: required %d, actual %d", requiredAmount, balance)
	}

	return nil
}

// NonceChecker nonce检查器
type NonceChecker struct {
	// 实际实现会依赖 Redis 或其他缓存
	checker NonceDuplicateChecker
}

// NonceDuplicateChecker 重复nonce检查器接口
type NonceDuplicateChecker interface {
	IsDuplicate(ctx context.Context, nonce string) (bool, error)
	RecordNonce(ctx context.Context, nonce string, ttlMinutes int) error
}

// NewNonceChecker 创建nonce检查器
func NewNonceChecker(checker NonceDuplicateChecker) *NonceChecker {
	return &NonceChecker{checker: checker}
}

// CheckAndRecord 检查并记录nonce
func (c *NonceChecker) CheckAndRecord(ctx context.Context, nonce string) error {
	// 检查是否重复
	duplicate, err := c.checker.IsDuplicate(ctx, nonce)
	if err != nil {
		return errors.Wrap(errors.INTERNAL_SERVER_ERROR, err, "failed to check nonce")
	}
	if duplicate {
		return errors.New(errors.DUPLICATE_NONCE, "duplicate nonce detected")
	}

	// 记录nonce
	if err := c.checker.RecordNonce(ctx, nonce, constants.NonceCacheMinutes); err != nil {
		return errors.Wrap(errors.INTERNAL_SERVER_ERROR, err, "failed to record nonce")
	}

	return nil
}

// IdempotencyKeyGenerator 幂等性键生成器
type IdempotencyKeyGenerator struct{}

// NewIdempotencyKeyGenerator 创建生成器
func NewIdempotencyKeyGenerator() *IdempotencyKeyGenerator {
	return &IdempotencyKeyGenerator{}
}

// Generate 生成幂等性键
// 格式: agent_did:skill_did:client_idempotency_key
func (g *IdempotencyKeyGenerator) Generate(agentDID, skillDID, clientKey string) string {
	return fmt.Sprintf("%s:%s:%s", agentDID, skillDID, clientKey)
}
