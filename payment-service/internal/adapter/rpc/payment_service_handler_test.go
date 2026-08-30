package rpc

import (
	"testing"

	commonpb "github.com/stablepay/payment-service/kitex_gen/stablepay/common"
	appdto "github.com/stablepay/payment-service/internal/application/dto"
	payerrors "github.com/stablepay/payment-service/pkg/errors"
)

func TestPaymentStatusMapping(t *testing.T) {
	tests := map[string]commonpb.PaymentStatus{
		"PENDING":   commonpb.PaymentStatus_PENDING,
		"CONFIRMED": commonpb.PaymentStatus_CONFIRMED,
		"COMPLETED": commonpb.PaymentStatus_CONFIRMED,
		"FAILED":    commonpb.PaymentStatus_FAILED,
		"CANCELLED": commonpb.PaymentStatus_FAILED,
		"unknown":   commonpb.PaymentStatus_PENDING,
	}
	for input, expected := range tests {
		if actual := paymentStatus(input); actual != expected {
			t.Fatalf("paymentStatus(%q) = %v, want %v", input, actual, expected)
		}
	}
}

func TestPaymentHandlerPreservesBaseAndResponse(t *testing.T) {
	requestID := "req-1"
	traceID := "trace-1"
	base := &commonpb.BaseReq{RequestId: &requestID, TraceId: &traceID}
	resp := toInitiateResponse(base, &appdto.InitiatePaymentResponse{
		TxID: "tx-1", TxHash: "hash-1", Status: "COMPLETED",
		CreatedAt: "2026-08-30T00:00:00Z", ConfirmedAt: "2026-08-30T00:00:01Z",
	})
	if resp.GetBase().GetCode() != 0 || resp.GetBase().GetRequestId() != requestID || resp.GetBase().GetTraceId() != traceID {
		t.Fatalf("base metadata was not preserved: %+v", resp.GetBase())
	}
	if resp.GetTxId() != "tx-1" || resp.GetStatus() != commonpb.PaymentStatus_CONFIRMED || resp.GetConfirmedAt() == "" {
		t.Fatalf("response mapping is incomplete: %+v", resp)
	}
}

func TestErrorBaseUsesTypedPaymentError(t *testing.T) {
	requestID := "req-2"
	base := errorBase(&commonpb.BaseReq{RequestId: &requestID}, payerrors.New(payerrors.INVALID_PARAMETERS, "bad payment"))
	if base.GetCode() != int32(payerrors.INVALID_PARAMETERS) || base.GetMessage() != "bad payment" || base.GetRequestId() != requestID {
		t.Fatalf("unexpected error base: %+v", base)
	}
}

func TestParseInt64RejectsInvalidAsZero(t *testing.T) {
	if parseInt64("1700000000") != 1700000000 {
		t.Fatal("valid timestamp was not parsed")
	}
	if parseInt64("not-a-timestamp") != 0 {
		t.Fatal("invalid timestamp should not be converted to a value")
	}
}
