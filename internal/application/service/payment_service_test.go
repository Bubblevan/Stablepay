// Package service 应用服务单元测试
package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stablepay/payment-service/internal/application/dto"
	"github.com/stablepay/payment-service/internal/domain/entity"
	domainsvc "github.com/stablepay/payment-service/internal/domain/service"
	"github.com/stablepay/payment-service/pkg/constants"
	pkgerrors "github.com/stablepay/payment-service/pkg/errors"
	"go.uber.org/zap"
)

// MockPaymentRepository 旧版未对齐真实接口的占位类型,已删除。
// RegisterXRegistrationReward 测试用的是下面 memPaymentRepo,与 repository.PaymentRepository 接口对齐。

// TestParseStatus 测试状态解析
func TestParseStatus(t *testing.T) {
	tests := []struct {
		input    string
		expected int8
	}{
		{"CREATED", 0},
		{"PENDING", 1},
		{"CONFIRMED", 2},
		{"COMPLETED", 3},
		{"FAILED", 4},
		{"CANCELLED", 5},
		{"UNKNOWN", -1},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseStatus(tt.input)
			if got != tt.expected {
				t.Errorf("parseStatus(%s) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

// TestExtractWalletFromDID 测试钱包地址提取
func TestExtractWalletFromDID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"did:solana:abc123", "abc123"},
		{"did:solana:4fK9x2HyJkLmNpQrStUvWxYz", "4fK9x2HyJkLmNpQrStUvWxYz"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := extractWalletFromDID(tt.input)
			if got != tt.expected {
				t.Errorf("extractWalletFromDID(%s) = %s, want %s", tt.input, got, tt.expected)
			}
		})
	}
}

// TestGenerateTxID 测试交易ID生成
func TestGenerateTxID(t *testing.T) {
	id1 := generateTxID()
	id2 := generateTxID()

	if id1 == "" {
		t.Error("generateTxID() should not return empty string")
	}

	if id1 == id2 {
		t.Error("generateTxID() should generate unique IDs")
	}
}

// TestDTOToMQEventTag 测试事件 Tag 转换
func TestDTOToMQEventTag(t *testing.T) {
	tests := []struct {
		status   int8
		expected string
	}{
		{constants.PaymentStatusConfirmed, constants.MQTagPaymentSucceeded},
		{constants.PaymentStatusCompleted, constants.MQTagPaymentSucceeded},
		{constants.PaymentStatusFailed, constants.MQTagPaymentFailed},
		{constants.PaymentStatusCancelled, constants.MQTagPaymentFailed},
		{constants.PaymentStatusCreated, ""},
		{constants.PaymentStatusPending, ""},
	}

	for _, tt := range tests {
		t.Run(constants.PaymentStatusToString(tt.status), func(t *testing.T) {
			got := dto.ToMQEventTag(tt.status)
			if got != tt.expected {
				t.Errorf("ToMQEventTag(%d) = %s, want %s", tt.status, got, tt.expected)
			}
		})
	}
}

// MockBlockchainExecutor Mock 区块链执行器
type MockBlockchainExecutor struct {
	shouldFail bool
	txHash     string
	delay      time.Duration
}

func (m *MockBlockchainExecutor) ExecuteTransfer(ctx context.Context, fromWallet, toWallet string, amountMinor int64, currency constants.Currency, _ string) (string, error) {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	if m.shouldFail {
		return "", pkgerrors.New(pkgerrors.BLOCKCHAIN_NETWORK_ERROR, "mock blockchain error")
	}
	return m.txHash, nil
}

func (m *MockBlockchainExecutor) QueryTxStatus(ctx context.Context, txHash string) (int8, *time.Time, error) {
	return constants.PaymentStatusConfirmed, nil, nil
}

// TestPaymentApplicationService_InitiatePayment_Validation 测试参数校验
func TestPaymentApplicationService_InitiatePayment_Validation(t *testing.T) {
	// 这里可以添加更完整的测试
	// 由于依赖较多，实际项目中应使用 mock 框架

	tests := []struct {
		name    string
		req     *dto.InitiatePaymentRequest
		wantErr bool
	}{
		{
			name: "缺少幂等性键",
			req: &dto.InitiatePaymentRequest{
				AgentDID:  "did:solana:agent123",
				SkillDID:  "did:solana:skill456",
				AmountStr: "5.00",
				Currency:  "USDC",
				Signature: "sig123",
				Timestamp: time.Now().Unix(),
				Nonce:     "nonce123",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 简化测试，实际应构造完整的服务实例
			_ = tt.req
		})
	}
}

// =====================================================================
// RegisterXRegistrationReward 测试
// =====================================================================

// memPaymentRepo in-memory PaymentRepository(只实现测试用得到的方法)
type memPaymentRepo struct {
	mu   sync.Mutex
	rows map[string]*entity.Payment
}

func newMemPaymentRepo() *memPaymentRepo {
	return &memPaymentRepo{rows: map[string]*entity.Payment{}}
}

func (m *memPaymentRepo) Create(ctx context.Context, p *entity.Payment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[p.TxID]; ok {
		return pkgerrors.New(pkgerrors.PAYMENT_ALREADY_EXISTS, "tx exists")
	}
	cp := *p
	m.rows[p.TxID] = &cp
	return nil
}

func (m *memPaymentRepo) GetByTxID(ctx context.Context, txID string) (*entity.Payment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.rows[txID]
	if !ok {
		return nil, pkgerrors.New(pkgerrors.RESOURCE_NOT_FOUND, "not found")
	}
	cp := *p
	return &cp, nil
}

func (m *memPaymentRepo) GetByTxHash(ctx context.Context, txHash string) (*entity.Payment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.rows {
		if p.TxHash == txHash {
			cp := *p
			return &cp, nil
		}
	}
	return nil, pkgerrors.New(pkgerrors.RESOURCE_NOT_FOUND, "not found")
}

func (m *memPaymentRepo) Update(ctx context.Context, p *entity.Payment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *p
	m.rows[p.TxID] = &cp
	return nil
}

func (m *memPaymentRepo) UpdateStatus(ctx context.Context, txID string, status int8) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.rows[txID]; ok {
		p.Status = status
	}
	return nil
}

func (m *memPaymentRepo) ListByAgentDID(ctx context.Context, agentDID string, status *int8, offset, limit int) ([]*entity.Payment, int64, error) {
	return nil, 0, nil
}

func (m *memPaymentRepo) ListBySkillDID(ctx context.Context, skillDID string, status *int8, offset, limit int) ([]*entity.Payment, int64, error) {
	return nil, 0, nil
}

func (m *memPaymentRepo) ListPending(ctx context.Context, beforeTime time.Time, limit int) ([]*entity.Payment, error) {
	return nil, nil
}

func (m *memPaymentRepo) ListFailedForRetry(ctx context.Context, maxRetryCount int, limit int) ([]*entity.Payment, error) {
	return nil, nil
}

func (m *memPaymentRepo) CountByAgentAndSkill(ctx context.Context, agentDID, skillDID string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for _, p := range m.rows {
		if p.AgentDID == agentDID && p.SkillDID == skillDID {
			n++
		}
	}
	return n, nil
}

// memIdempotencyRepo in-memory PaymentIdempotencyRepository
type memIdempotencyRepo struct {
	mu   sync.Mutex
	rows map[string]*entity.PaymentIdempotency
}

func newMemIdempotencyRepo() *memIdempotencyRepo {
	return &memIdempotencyRepo{rows: map[string]*entity.PaymentIdempotency{}}
}

func (m *memIdempotencyRepo) Get(ctx context.Context, key string) (*entity.PaymentIdempotency, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[key]
	if !ok {
		return nil, pkgerrors.New(pkgerrors.RESOURCE_NOT_FOUND, "not found")
	}
	cp := *r
	return &cp, nil
}

func (m *memIdempotencyRepo) Create(ctx context.Context, r *entity.PaymentIdempotency) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[r.IdempotencyKey]; ok {
		return pkgerrors.New(pkgerrors.PAYMENT_ALREADY_EXISTS, "key exists")
	}
	cp := *r
	m.rows[r.IdempotencyKey] = &cp
	return nil
}

func (m *memIdempotencyRepo) Update(ctx context.Context, r *entity.PaymentIdempotency) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *r
	m.rows[r.IdempotencyKey] = &cp
	return nil
}

func (m *memIdempotencyRepo) DeleteExpired(ctx context.Context, beforeTime time.Time) error {
	return nil
}

// memEventPublisher in-memory EventPublisher
type memEventPublisher struct {
	mu     sync.Mutex
	events []*dto.MQPaymentEvent
}

func (m *memEventPublisher) PublishPaymentEvent(ctx context.Context, e *dto.MQPaymentEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *e
	m.events = append(m.events, &cp)
	return nil
}

// stubSignatureValidator 永远返回 true,跳过真实签名校验
type stubSignatureValidator struct{}

func (stubSignatureValidator) Validate(ctx context.Context, did, message, signature, timestamp, nonce string) (bool, error) {
	return true, nil
}

// stubBalanceChecker 永远返回"余额充足"
type stubBalanceChecker struct{}

func (stubBalanceChecker) CheckBalance(ctx context.Context, wallet string, currency constants.Currency, amount int64) (bool, int64, error) {
	return true, 1_000_000_000, nil
}

// memNonceDuplicateChecker 进程内 nonce 去重
type memNonceDuplicateChecker struct {
	mu sync.Mutex
	ss map[string]struct{}
}

func newMemNonceDuplicateChecker() *memNonceDuplicateChecker {
	return &memNonceDuplicateChecker{ss: map[string]struct{}{}}
}

func (m *memNonceDuplicateChecker) IsDuplicate(ctx context.Context, nonce string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.ss[nonce]
	return ok, nil
}

func (m *memNonceDuplicateChecker) RecordNonce(ctx context.Context, nonce string, ttlMinutes int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ss[nonce] = struct{}{}
	return nil
}

func newTestRewardService(t *testing.T, bc BlockchainExecutor, cfg *PaymentConfig) (*PaymentApplicationService, *memPaymentRepo, *memIdempotencyRepo, *memEventPublisher) {
	t.Helper()
	if cfg == nil {
		cfg = &PaymentConfig{
			TimeoutMinutes:          5,
			TreasuryWalletAddress:   "TreasuryWallet111111111111111111111111111111111",
			XRegistrationRewardMinor: 1_000_000,
			InternalApiKey:          "test-internal-key",
		}
	}
	if bc == nil {
		bc = &MockBlockchainExecutor{txHash: "mock-tx-hash-001"}
	}

	paymentRepo := newMemPaymentRepo()
	idemRepo := newMemIdempotencyRepo()
	evPub := &memEventPublisher{}

	validator := domainsvc.NewPaymentValidator(stubSignatureValidator{}, stubBalanceChecker{}, 1_000_000_000)
	nonceChecker := domainsvc.NewNonceChecker(newMemNonceDuplicateChecker())

	svc := NewPaymentApplicationService(
		paymentRepo,
		idemRepo,
		validator,
		nonceChecker,
		bc,
		evPub,
		cfg,
		zap.NewNop(),
	)
	return svc, paymentRepo, idemRepo, evPub
}

func sampleRewardRequest() *dto.XRegistrationRewardRequest {
	return &dto.XRegistrationRewardRequest{
		IdempotencyKey: "x-reward-did:solana:AbcWallet-1234567890",
		AgentDID:       "did:solana:AbcWallet1234567890",
		WalletAddress:  "AbcWallet1234567890",
		TweetID:        "1234567890",
		XHandle:        "testuser",
		AmountStr:      "1.0",
		Currency:       "USDC",
		Reason:         "x_registration_reward",
	}
}

// TestRegisterXRegistrationReward_HappyPath 验证正常流程:入账 PENDING、写幂等、发 PENDING MQ 事件。
func TestRegisterXRegistrationReward_HappyPath(t *testing.T) {
	svc, paymentRepo, idemRepo, evPub := newTestRewardService(t, nil, nil)
	req := sampleRewardRequest()

	resp, err := svc.RegisterXRegistrationReward(context.Background(), req)
	if err != nil {
		t.Fatalf("RegisterXRegistrationReward err=%v", err)
	}
	if resp.TxID == "" {
		t.Fatal("expected non-empty tx_id")
	}
	if resp.FromWallet != svc.config.TreasuryWalletAddress {
		t.Errorf("FromWallet=%q, want %q", resp.FromWallet, svc.config.TreasuryWalletAddress)
	}
	if resp.RecipientWallet != "AbcWallet1234567890" {
		t.Errorf("RecipientWallet=%q, want %q", resp.RecipientWallet, "AbcWallet1234567890")
	}
	if resp.AlreadyPaid {
		t.Error("AlreadyPaid should be false on first call")
	}
	if resp.Amount != "1" {
		t.Errorf("resp.Amount=%q, want \"1\" (MinorUnitToString trims zeros)", resp.Amount)
	}

	// payment_repo 应该有 1 条 Payment,status=PENDING,reward_purpose=x_registration_reward
	pmt, err := paymentRepo.GetByTxID(context.Background(), resp.TxID)
	if err != nil {
		t.Fatalf("payment not found: %v", err)
	}
	if pmt.Status != constants.PaymentStatusPending {
		t.Errorf("payment.Status=%d, want PENDING(%d)", pmt.Status, constants.PaymentStatusPending)
	}
	if pmt.RewardPurpose != "x_registration_reward" {
		t.Errorf("payment.RewardPurpose=%q", pmt.RewardPurpose)
	}
	if pmt.SkillDID != constants.SkillDidXRegistrationReward {
		t.Errorf("payment.SkillDID=%q, want %q", pmt.SkillDID, constants.SkillDidXRegistrationReward)
	}
	if pmt.Signature != constants.SignatureInternalReward {
		t.Errorf("payment.Signature=%q, want %q", pmt.Signature, constants.SignatureInternalReward)
	}

	// idempotency 应该记录了
	if len(idemRepo.rows) == 0 {
		t.Error("expected idempotency record")
	}

	// 至少 1 个 MQ 事件(Initial PENDING 不在 publishEvent 触发,这里应该没有;若 done 也不会有)
	_ = evPub
}

// TestRegisterXRegistrationReward_Replay 同样 key 重放应该命中 idempotency,AlreadyPaid=true,不调 chain。
func TestRegisterXRegistrationReward_Replay(t *testing.T) {
	bc := &countingBlockchain{txHash: "mock-tx-hash-002"}
	svc, paymentRepo, _, _ := newTestRewardService(t, bc, nil)
	req := sampleRewardRequest()

	first, err := svc.RegisterXRegistrationReward(context.Background(), req)
	if err != nil {
		t.Fatalf("first call err=%v", err)
	}
	if first.AlreadyPaid {
		t.Fatal("first call should not be AlreadyPaid")
	}

	second, err := svc.RegisterXRegistrationReward(context.Background(), req)
	if err != nil {
		t.Fatalf("replay call err=%v", err)
	}
	if !second.AlreadyPaid {
		t.Error("replay should set AlreadyPaid=true")
	}
	if second.TxID != first.TxID {
		t.Errorf("replay tx_id=%q, want %q", second.TxID, first.TxID)
	}
	if bc.callCount != 1 {
		t.Errorf("ExecuteTransfer called %d times, want 1 (replay must not call chain)", bc.callCount)
	}

	// payment 仍只有 1 条
	if len(paymentRepo.rows) != 1 {
		t.Errorf("payment_repo rows=%d, want 1", len(paymentRepo.rows))
	}
}

// TestRegisterXRegistrationReward_KeyMismatch 同样 key + 不同 body 应该返回 IDEMPOTENCY_KEY_MISMATCH。
func TestRegisterXRegistrationReward_KeyMismatch(t *testing.T) {
	svc, _, _, _ := newTestRewardService(t, nil, nil)
	first := sampleRewardRequest()
	if _, err := svc.RegisterXRegistrationReward(context.Background(), first); err != nil {
		t.Fatalf("first call err=%v", err)
	}

	second := sampleRewardRequest()
	second.TweetID = "9999" // 改 tweet_id,request hash 改变
	_, err := svc.RegisterXRegistrationReward(context.Background(), second)
	if err == nil {
		t.Fatal("expected error on key mismatch")
	}
	if !pkgerrors.IsErrorCode(err, pkgerrors.IDEMPOTENCY_KEY_MISMATCH) {
		t.Errorf("err code=%d, want IDEMPOTENCY_KEY_MISMATCH(%d)",
			pkgerrors.GetErrorCode(err), pkgerrors.IDEMPOTENCY_KEY_MISMATCH)
	}
}

// TestRegisterXRegistrationReward_TreasuryEmpty treasury 没配置时直接 500。
func TestRegisterXRegistrationReward_TreasuryEmpty(t *testing.T) {
	svc, _, _, _ := newTestRewardService(t, nil, &PaymentConfig{
		TimeoutMinutes:        5,
		TreasuryWalletAddress: "", // 故意空
		InternalApiKey:        "test-internal-key",
	})
	req := sampleRewardRequest()

	_, err := svc.RegisterXRegistrationReward(context.Background(), req)
	if err == nil {
		t.Fatal("expected error when treasury empty")
	}
	if !pkgerrors.IsErrorCode(err, pkgerrors.INTERNAL_SERVER_ERROR) {
		t.Errorf("err code=%d, want INTERNAL_SERVER_ERROR(%d)",
			pkgerrors.GetErrorCode(err), pkgerrors.INTERNAL_SERVER_ERROR)
	}
}

// TestRegisterXRegistrationReward_ChainFail chain 失败时标 FAILED、记 idempotency、返回 BLOCKCHAIN_NETWORK_ERROR。
func TestRegisterXRegistrationReward_ChainFail(t *testing.T) {
	bc := &countingBlockchain{shouldFail: true, txHash: "wont-be-used"}
	svc, paymentRepo, idemRepo, _ := newTestRewardService(t, bc, nil)
	req := sampleRewardRequest()

	_, err := svc.RegisterXRegistrationReward(context.Background(), req)
	if err == nil {
		t.Fatal("expected error when chain fails")
	}
	if !pkgerrors.IsErrorCode(err, pkgerrors.BLOCKCHAIN_NETWORK_ERROR) {
		t.Errorf("err code=%d, want BLOCKCHAIN_NETWORK_ERROR(%d)",
			pkgerrors.GetErrorCode(err), pkgerrors.BLOCKCHAIN_NETWORK_ERROR)
	}

	if len(paymentRepo.rows) != 1 {
		t.Fatalf("payment_repo rows=%d, want 1", len(paymentRepo.rows))
	}
	for _, p := range paymentRepo.rows {
		if p.Status != constants.PaymentStatusFailed {
			t.Errorf("payment.Status=%d, want FAILED(%d)", p.Status, constants.PaymentStatusFailed)
		}
	}

	if len(idemRepo.rows) != 1 {
		t.Errorf("idem rows=%d, want 1 (failed attempts should still be idempotent)", len(idemRepo.rows))
	}
}

// TestRegisterXRegistrationReward_InvalidAmount amount 非正数应该被拒。
func TestRegisterXRegistrationReward_InvalidAmount(t *testing.T) {
	svc, _, _, _ := newTestRewardService(t, nil, nil)
	req := sampleRewardRequest()
	req.AmountStr = "0"

	_, err := svc.RegisterXRegistrationReward(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for zero amount")
	}
	if !pkgerrors.IsErrorCode(err, pkgerrors.INVALID_PARAMETERS) {
		t.Errorf("err code=%d, want INVALID_PARAMETERS", pkgerrors.GetErrorCode(err))
	}
}

// countingBlockchain 带调用计数的 BlockchainExecutor
type countingBlockchain struct {
	shouldFail bool
	txHash     string
	callCount  int
	mu         sync.Mutex
}

func (c *countingBlockchain) ExecuteTransfer(ctx context.Context, fromWallet, toWallet string, amountMinor int64, currency constants.Currency, _ string) (string, error) {
	c.mu.Lock()
	c.callCount++
	c.mu.Unlock()
	if c.shouldFail {
		return "", pkgerrors.New(pkgerrors.BLOCKCHAIN_NETWORK_ERROR, "mock chain fail")
	}
	return c.txHash, nil
}

func (c *countingBlockchain) QueryTxStatus(ctx context.Context, txHash string) (int8, *time.Time, error) {
	return constants.PaymentStatusConfirmed, nil, nil
}
