package adapters

import (
	"context"
	"net"
	"testing"
	"time"

	kitexclient "github.com/cloudwego/kitex/client"
	kitexserver "github.com/cloudwego/kitex/server"
	"github.com/stablepay/commerce-runtime/internal/payment"
	kitexcommon "github.com/stablepay/payment-service/kitex_gen/stablepay/common"
	kitexpayment "github.com/stablepay/payment-service/kitex_gen/stablepay/payment_service"
	kitexpaymentservice "github.com/stablepay/payment-service/kitex_gen/stablepay/payment_service/paymentservice"
)

type contractPaymentHandler struct {
	initiate *kitexpayment.InitiatePaymentRequest
}

func (h *contractPaymentHandler) InitiatePayment(_ context.Context, request *kitexpayment.InitiatePaymentRequest) (*kitexpayment.InitiatePaymentResponse, error) {
	h.initiate = request
	return &kitexpayment.InitiatePaymentResponse{Base: &kitexcommon.BaseResp{Code: int32(kitexcommon.ErrorCode_SUCCESS), Message: "success"}, TxId: "tx-kitex-1", TxHash: "hash-kitex-1", Status: kitexcommon.PaymentStatus_CONFIRMED}, nil
}

func (h *contractPaymentHandler) GetPaymentStatus(_ context.Context, request *kitexpayment.GetPaymentStatusRequest) (*kitexpayment.GetPaymentStatusResponse, error) {
	return &kitexpayment.GetPaymentStatusResponse{Base: &kitexcommon.BaseResp{Code: int32(kitexcommon.ErrorCode_SUCCESS), Message: "success"}, TxId: request.GetTxId(), Status: kitexcommon.PaymentStatus_CONFIRMED}, nil
}

func (h *contractPaymentHandler) ListPaymentHistory(context.Context, *kitexpayment.ListPaymentHistoryRequest) (*kitexpayment.ListPaymentHistoryResponse, error) {
	return nil, nil
}

func (h *contractPaymentHandler) GetPaymentRequirement(context.Context, *kitexpayment.GetPaymentRequirementRequest) (*kitexpayment.GetPaymentRequirementResponse, error) {
	return nil, nil
}

func TestKitexPaymentAdapterUsesCanonicalTransportAndPayeeMapping(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	handler := &contractPaymentHandler{}
	server := kitexpaymentservice.NewServer(handler, kitexserver.WithListener(listener))
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Run() }()
	defer func() {
		_ = server.Stop()
		select {
		case err := <-serverErr:
			if err != nil {
				t.Logf("Kitex server stopped with: %v", err)
			}
		case <-time.After(time.Second):
			t.Log("Kitex server stop timed out")
		}
	}()

	client, err := kitexpaymentservice.NewClient("payment-service", kitexclient.WithHostPorts(listener.Addr().String()))
	if err != nil {
		t.Fatal(err)
	}
	intent := payment.PaymentIntent{
		IntentID: "pi-kitex", EpisodeID: "episode-kitex", MerchantDID: "did:merchant:1", CapabilityID: "capability-1", PayeeDID: "did:payee:1",
		QuoteHash: "sha256:quote", AmountMinor: 300, Currency: "USDC", RequesterDID: "did:agent:1", EpisodeVersion: 5, BudgetReservation: 300,
		IdempotencyKey: "kitex-idempotency", EconomicKey: payment.EconomicIdentityKey("episode-kitex", "did:merchant:1", "capability-1", "did:payee:1", "sha256:quote", 300, "USDC"),
		ExpiresAt: time.Date(2099, 9, 15, 13, 0, 0, 0, time.UTC), Status: payment.IntentAuthorized,
		CreatedAt: time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC),
		AuthorizationRef: "auth-1", CredentialRef: "credential-ref",
	}
	intent.RequestFingerprint = payment.RequestFingerprint(intent)
	authorization := AuthorizationResult{Allowed: true, RequesterDID: intent.RequesterDID, MerchantDID: intent.MerchantDID, CapabilityID: intent.CapabilityID,
		PayeeDID: intent.PayeeDID, QuoteHash: intent.QuoteHash, AmountMinor: intent.AmountMinor, Currency: intent.Currency, AuthorizationRef: intent.AuthorizationRef}
	adapter := &KitexPaymentAdapter{Client: client, Credentials: CredentialFunc(func(context.Context, payment.PaymentIntent) (PaymentCredentials, error) {
		return PaymentCredentials{Reference: "credential-ref", Signature: "sig", Timestamp: "123", Nonce: "nonce"}, nil
	})}
	outcome, err := adapter.Submit(context.Background(), PaymentSubmitRequest{Intent: intent, Authorization: authorization, PayeeDID: intent.PayeeDID, RequestFingerprint: intent.RequestFingerprint, TraceID: "trace-kitex"})
	if err != nil || outcome.Status != payment.OutcomeConfirmed {
		t.Fatalf("Kitex submit failed: %#v err=%v", outcome, err)
	}
	if handler.initiate == nil || handler.initiate.GetAgentDid() != intent.RequesterDID || handler.initiate.GetSkillDid() != intent.PayeeDID || handler.initiate.GetAmountMinor() != intent.AmountMinor || handler.initiate.GetBase().GetIdempotencyKey() != intent.IdempotencyKey {
		t.Fatalf("canonical Kitex field mapping is wrong: %#v", handler.initiate)
	}
	queried, err := adapter.Query(context.Background(), PaymentQuery{Intent: func() payment.PaymentIntent { intent.TxID = "tx-kitex-1"; return intent }(), PayeeDID: intent.PayeeDID})
	if err != nil || queried.Status != payment.OutcomeConfirmed || queried.TxID != "tx-kitex-1" {
		t.Fatalf("Kitex status query failed: %#v err=%v", queried, err)
	}
}
