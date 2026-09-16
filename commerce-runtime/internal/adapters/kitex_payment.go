package adapters

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/stablepay/commerce-runtime/internal/payment"
	kitexcommon "github.com/stablepay/payment-service/kitex_gen/stablepay/common"
	kitexpayment "github.com/stablepay/payment-service/kitex_gen/stablepay/payment_service"
	kitexpaymentservice "github.com/stablepay/payment-service/kitex_gen/stablepay/payment_service/paymentservice"
)

// KitexPaymentAdapter is the production adapter from commerce-runtime to the
// canonical payment-service Kitex contract. It imports only generated public
// IDL types; payment-service internal packages are intentionally unreachable.
type KitexPaymentAdapter struct {
	Client      kitexpaymentservice.Client
	Credentials CredentialProvider
}

func (a *KitexPaymentAdapter) Submit(ctx context.Context, request PaymentSubmitRequest) (payment.PaymentOutcome, error) {
	if err := request.Intent.Validate(); err != nil {
		return unknownOutcome(request.Intent, err), err
	}
	if err := request.Authorization.Validate(); err != nil {
		return unknownOutcome(request.Intent, err), err
	}
	if err := validatePaymentBinding(request.Intent, request.Authorization); err != nil {
		return unknownOutcome(request.Intent, err), err
	}
	if request.PayeeDID != request.Intent.PayeeDID {
		err := errors.New("payment request payee does not match intent snapshot")
		return unknownOutcome(request.Intent, err), err
	}
	if a == nil || a.Client == nil || a.Credentials == nil {
		err := errors.New("kitex payment client and credential provider are required")
		return unknownOutcome(request.Intent, err), err
	}
	if request.RequestFingerprint == "" || request.RequestFingerprint != request.Intent.RequestFingerprint {
		err := errors.New("payment request fingerprint does not match intent")
		return unknownOutcome(request.Intent, err), err
	}
	credentials, err := a.Credentials.Credentials(ctx, request.Intent)
	if err != nil {
		return unknownOutcome(request.Intent, err), err
	}
	if request.Intent.CredentialRef != "" && credentials.Reference != request.Intent.CredentialRef {
		err := errors.New("credential reference changed for payment retry")
		return unknownOutcome(request.Intent, err), err
	}
	base := baseRequest(request.Intent, request.TraceID)
	requestPayload := &kitexpayment.InitiatePaymentRequest{
		Base:        base,
		AgentDid:    kitexcommon.DID(request.Intent.RequesterDID),
		SkillDid:    kitexcommon.DID(request.Intent.PayeeDID),
		AmountMinor: request.Intent.AmountMinor,
		Currency:    paymentCurrency(request.Intent.Currency),
	}
	if credentials.Signature != "" {
		requestPayload.Signature = &credentials.Signature
	}
	if credentials.Timestamp != "" {
		requestPayload.Timestamp = &credentials.Timestamp
	}
	if credentials.Nonce != "" {
		requestPayload.Nonce = &credentials.Nonce
	}
	if credentials.SignedTxBase64 != "" {
		requestPayload.SignedTxBase64 = &credentials.SignedTxBase64
	}
	response, err := a.Client.InitiatePayment(ctx, requestPayload)
	if err != nil {
		return unknownOutcome(request.Intent, err), err
	}
	if response == nil {
		err := errors.New("payment-service returned an empty initiate response")
		return unknownOutcome(request.Intent, err), err
	}
	if response.Base != nil && response.Base.GetCode() != int32(kitexcommon.ErrorCode_SUCCESS) {
		err := fmt.Errorf("payment-service rejected initiate: %s", response.Base.GetMessage())
		return payment.PaymentOutcome{Status: payment.OutcomeFailed, IntentID: request.Intent.IntentID, AmountMinor: request.Intent.AmountMinor, Currency: request.Intent.Currency, Reason: err.Error()}, err
	}
	return payment.PaymentOutcome{Status: mapKitexStatus(response.Status), IntentID: request.Intent.IntentID, TxID: string(response.TxId), TxHash: string(response.TxHash), AmountMinor: request.Intent.AmountMinor, Currency: strings.ToUpper(request.Intent.Currency), OccurredAt: response.GetConfirmedAt()}, nil
}

func (a *KitexPaymentAdapter) Query(ctx context.Context, request PaymentQuery) (payment.PaymentOutcome, error) {
	if err := request.Intent.Validate(); err != nil {
		return unknownOutcome(request.Intent, err), err
	}
	if request.PayeeDID != request.Intent.PayeeDID {
		err := errors.New("payment query payee does not match intent snapshot")
		return unknownOutcome(request.Intent, err), err
	}
	if request.Intent.TxID == "" {
		err := errors.New("payment status query requires a tx_id")
		return unknownOutcome(request.Intent, err), err
	}
	if a == nil || a.Client == nil {
		err := errors.New("kitex payment client is required")
		return unknownOutcome(request.Intent, err), err
	}
	response, err := a.Client.GetPaymentStatus(ctx, &kitexpayment.GetPaymentStatusRequest{Base: baseRequest(request.Intent, request.TraceID), TxId: kitexcommon.TxId(request.Intent.TxID)})
	if err != nil {
		return unknownOutcome(request.Intent, err), err
	}
	if response == nil {
		err := errors.New("payment-service returned an empty status response")
		return unknownOutcome(request.Intent, err), err
	}
	if response.Base != nil && response.Base.GetCode() != int32(kitexcommon.ErrorCode_SUCCESS) {
		err := fmt.Errorf("payment-service rejected status query: %s", response.Base.GetMessage())
		return unknownOutcome(request.Intent, err), err
	}
	txHash := ""
	if response.IsSetTxHash() {
		txHash = string(response.GetTxHash())
	}
	return payment.PaymentOutcome{Status: mapKitexStatus(response.Status), IntentID: request.Intent.IntentID, TxID: string(response.TxId), TxHash: txHash, AmountMinor: request.Intent.AmountMinor, Currency: strings.ToUpper(request.Intent.Currency), OccurredAt: response.GetConfirmedAt()}, nil
}

func baseRequest(intent payment.PaymentIntent, traceID string) *kitexcommon.BaseReq {
	requestID := intent.IntentID
	idempotencyKey := intent.IdempotencyKey
	base := &kitexcommon.BaseReq{RequestId: &requestID, IdempotencyKey: &idempotencyKey}
	if traceID != "" {
		base.TraceId = &traceID
	}
	timestamp := intent.UpdatedAt.UTC().UnixMilli()
	base.TimestampMs = &timestamp
	return base
}

func paymentCurrency(value string) kitexcommon.Currency {
	if strings.EqualFold(value, "USDT") {
		return kitexcommon.Currency_USDT
	}
	return kitexcommon.Currency_USDC
}

func mapKitexStatus(value kitexcommon.PaymentStatus) payment.OutcomeStatus {
	switch value {
	case kitexcommon.PaymentStatus_CONFIRMED:
		return payment.OutcomeConfirmed
	case kitexcommon.PaymentStatus_FAILED:
		return payment.OutcomeFailed
	default:
		return payment.OutcomePending
	}
}

func validatePaymentBinding(intent payment.PaymentIntent, authorization AuthorizationResult) error {
	if authorization.RequesterDID != intent.RequesterDID || authorization.MerchantDID != intent.MerchantDID || authorization.CapabilityID != intent.CapabilityID || authorization.PayeeDID != intent.PayeeDID || authorization.QuoteHash != intent.QuoteHash || authorization.AmountMinor != intent.AmountMinor || !strings.EqualFold(authorization.Currency, intent.Currency) {
		return errors.New("payment authorization does not match intent snapshot")
	}
	return nil
}
