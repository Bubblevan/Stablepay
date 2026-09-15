package service

import (
	"context"
	"testing"
	"time"

	"github.com/stablepay/payment-service/internal/application/dto"
	"github.com/stablepay/payment-service/internal/domain/entity"
	"github.com/stablepay/payment-service/pkg/constants"
)

func TestPaymentIdempotencyReplayReturnsOriginalPaymentWhileInFlightOrCompleted(t *testing.T) {
	ctx := context.Background()
	paymentRepo := newMemPaymentRepo()
	idempotencyRepo := newMemIdempotencyRepo()
	service := &PaymentApplicationService{
		paymentRepo:     paymentRepo,
		idempotencyRepo: idempotencyRepo,
	}

	req := &dto.InitiatePaymentRequest{
		AgentDID:       "did:solana:agent",
		SkillDID:       "did:solana:skill",
		AmountStr:      "0.01",
		Currency:       "USDC",
		Signature:      "real-signature",
		Timestamp:      time.Now().Unix(),
		Nonce:          "payment-nonce",
		SignedTxBase64: "real-partial-transaction",
	}
	payment := &entity.Payment{
		TxID:        "tx-original",
		AgentDID:    req.AgentDID,
		SkillDID:    req.SkillDID,
		AmountMinor: 10000,
		Currency:    constants.CurrencyUSDC,
		Status:      constants.PaymentStatusPending,
	}
	if err := paymentRepo.Create(ctx, payment); err != nil {
		t.Fatalf("create payment: %v", err)
	}

	key := "stablepay-e2e-idempotency"
	if err := idempotencyRepo.Create(ctx, &entity.PaymentIdempotency{
		IdempotencyKey: key,
		RequestHash:    hashRequest(req),
		Status:         0,
		ExpiresAt:      time.Now().Add(time.Minute),
		CreatedAt:      time.Now(),
	}); err != nil {
		t.Fatalf("create pending idempotency state: %v", err)
	}
	if err := service.recordIdempotency(ctx, key, payment.TxID, 0, req, nil); err != nil {
		t.Fatalf("record in-flight idempotency state: %v", err)
	}

	got, err := service.checkIdempotency(ctx, key, req)
	if err != nil {
		t.Fatalf("check idempotency: %v", err)
	}
	if got == nil || got.TxID != payment.TxID {
		t.Fatalf("idempotency replay = %#v, want original tx_id %q", got, payment.TxID)
	}

	if err := service.recordIdempotency(ctx, key, payment.TxID, 1, req, &dto.InitiatePaymentResponse{TxID: payment.TxID}); err != nil {
		t.Fatalf("record completed idempotency state: %v", err)
	}
	got, err = service.checkIdempotency(ctx, key, req)
	if err != nil {
		t.Fatalf("check completed idempotency: %v", err)
	}
	if got == nil || got.TxID != payment.TxID {
		t.Fatalf("completed idempotency replay = %#v, want original tx_id %q", got, payment.TxID)
	}
}
