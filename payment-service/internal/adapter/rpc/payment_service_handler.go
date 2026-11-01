package rpc

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	appdto "github.com/stablepay/payment-service/internal/application/dto"
	appservice "github.com/stablepay/payment-service/internal/application/service"
	fiatservice "github.com/stablepay/payment-service/internal/fiat"
	commonpb "github.com/stablepay/payment-service/kitex_gen/stablepay/common"
	paymentpb "github.com/stablepay/payment-service/kitex_gen/stablepay/payment_service"
	payerrors "github.com/stablepay/payment-service/pkg/errors"
	"github.com/stablepay/payment-service/pkg/utils"
)

// PaymentServiceHandler is the only transport adapter exposed by payment-service.
// HTTP concerns stay in api-gateway; this adapter only translates the canonical
// Kitex contract to the existing application layer.
type PaymentServiceHandler struct {
	app  *appservice.PaymentApplicationService
	fiat *fiatservice.Service
}

func NewPaymentServiceHandler(app *appservice.PaymentApplicationService, fiat ...*fiatservice.Service) *PaymentServiceHandler {
	h := &PaymentServiceHandler{app: app}
	if len(fiat) > 0 {
		h.fiat = fiat[0]
	}
	return h
}

func (h *PaymentServiceHandler) CreateFiatPayment(ctx context.Context, req *paymentpb.CreateFiatPaymentRequest) (*paymentpb.CreateFiatPaymentResponse, error) {
	if req == nil || h.fiat == nil {
		return &paymentpb.CreateFiatPaymentResponse{Base: fiatErrorBase(nil, errors.New("fiat payment service is unavailable"))}, nil
	}
	base := req.GetBase()
	order, err := h.fiat.CreateContext(ctx, fiatservice.CreateRequest{RequestID: req.GetRequestId(), IdempotencyKey: baseIdempotencyKey(base), Nonce: req.GetNonce(),
		ProductID: req.GetProductId(), Subject: req.GetSubject(), QuoteHash: req.GetQuoteHash(), ApprovalRef: req.GetApprovalRef(),
		AgentDID: string(req.GetAgentDid()), SkillDID: string(req.GetSkillDid()), AmountMinor: req.GetAmountMinor(), Currency: req.GetCurrency()})
	if err != nil {
		return &paymentpb.CreateFiatPaymentResponse{Base: fiatErrorBase(base, err)}, nil
	}
	return &paymentpb.CreateFiatPaymentResponse{Base: successBase(base), Order: toFiatPaymentOrder(order)}, nil
}

func (h *PaymentServiceHandler) GetFiatPayment(_ context.Context, req *paymentpb.GetFiatPaymentRequest) (*paymentpb.GetFiatPaymentResponse, error) {
	if req == nil || h.fiat == nil {
		return &paymentpb.GetFiatPaymentResponse{Base: fiatErrorBase(nil, errors.New("fiat payment service is unavailable"))}, nil
	}
	order, err := h.fiat.Get(req.GetOrderId())
	if err != nil {
		return &paymentpb.GetFiatPaymentResponse{Base: fiatErrorBase(req.GetBase(), err)}, nil
	}
	return &paymentpb.GetFiatPaymentResponse{Base: successBase(req.GetBase()), Order: toFiatPaymentOrder(order)}, nil
}

func (h *PaymentServiceHandler) FindFiatPayment(_ context.Context, req *paymentpb.FindFiatPaymentRequest) (*paymentpb.FindFiatPaymentResponse, error) {
	if req == nil || h.fiat == nil {
		return &paymentpb.FindFiatPaymentResponse{Base: fiatErrorBase(nil, errors.New("fiat payment service is unavailable"))}, nil
	}
	order, err := h.fiat.FindByRequest(req.GetRequestId())
	if err != nil {
		return &paymentpb.FindFiatPaymentResponse{Base: fiatErrorBase(req.GetBase(), err)}, nil
	}
	return &paymentpb.FindFiatPaymentResponse{Base: successBase(req.GetBase()), Order: toFiatPaymentOrder(order)}, nil
}

func (h *PaymentServiceHandler) CheckoutFiatPayment(ctx context.Context, req *paymentpb.FiatPaymentActionRequest) (*paymentpb.FiatPaymentActionResponse, error) {
	if req == nil || h.fiat == nil {
		return &paymentpb.FiatPaymentActionResponse{Base: fiatErrorBase(nil, errors.New("fiat payment service is unavailable"))}, nil
	}
	url, err := h.fiat.CheckoutContext(ctx, req.GetOrderId())
	if err != nil {
		return &paymentpb.FiatPaymentActionResponse{Base: fiatErrorBase(req.GetBase(), err)}, nil
	}
	mode := h.fiat.Mode()
	return &paymentpb.FiatPaymentActionResponse{Base: successBase(req.GetBase()), CashierUrl: &url, Mode: &mode}, nil
}

func (h *PaymentServiceHandler) ReconcileFiatPayment(ctx context.Context, req *paymentpb.FiatPaymentActionRequest) (*paymentpb.FiatPaymentActionResponse, error) {
	if req == nil || h.fiat == nil {
		return &paymentpb.FiatPaymentActionResponse{Base: fiatErrorBase(nil, errors.New("fiat payment service is unavailable"))}, nil
	}
	order, err := h.fiat.Reconcile(ctx, req.GetOrderId())
	if err != nil {
		return &paymentpb.FiatPaymentActionResponse{Base: fiatErrorBase(req.GetBase(), err)}, nil
	}
	return &paymentpb.FiatPaymentActionResponse{Base: successBase(req.GetBase()), Order: toFiatPaymentOrder(order)}, nil
}

func (h *PaymentServiceHandler) SimulateFiatPayment(_ context.Context, req *paymentpb.FiatPaymentActionRequest) (*paymentpb.FiatPaymentActionResponse, error) {
	if req == nil || h.fiat == nil {
		return &paymentpb.FiatPaymentActionResponse{Base: fiatErrorBase(nil, errors.New("fiat payment service is unavailable"))}, nil
	}
	if strings.TrimSpace(req.GetOrderId()) == "" {
		return &paymentpb.FiatPaymentActionResponse{Base: errorBase(nil, payerrors.New(payerrors.INVALID_PARAMETERS, "order_id is required"))}, nil
	}
	order, err := h.fiat.Simulate(req.GetOrderId())
	if err != nil {
		return &paymentpb.FiatPaymentActionResponse{Base: errorBase(req.GetBase(), err)}, nil
	}
	return &paymentpb.FiatPaymentActionResponse{Base: successBase(req.GetBase()), Order: toFiatPaymentOrder(order)}, nil
}

func (h *PaymentServiceHandler) NotifyAlipay(ctx context.Context, req *paymentpb.NotifyAlipayRequest) (*paymentpb.NotifyAlipayResponse, error) {
	if req == nil || h.fiat == nil {
		return &paymentpb.NotifyAlipayResponse{Base: fiatErrorBase(nil, errors.New("fiat payment service is unavailable"))}, nil
	}
	order, err := h.fiat.NotifyContext(ctx, req.GetParameters())
	if err != nil {
		return &paymentpb.NotifyAlipayResponse{Base: fiatErrorBase(req.GetBase(), err)}, nil
	}
	return &paymentpb.NotifyAlipayResponse{Base: successBase(req.GetBase()), Order: toFiatPaymentOrder(order)}, nil
}

func toFiatPaymentOrder(order fiatservice.Order) *paymentpb.FiatPaymentOrder {
	out := &paymentpb.FiatPaymentOrder{Id: order.ID, RequestId: order.RequestID, ProductId: order.ProductID, Subject: order.Subject,
		QuoteHash: order.QuoteHash, ApprovalRef: order.ApprovalRef, AgentDid: order.AgentDID, SkillDid: order.SkillDID,
		AmountMinor: order.AmountMinor, Currency: order.Currency, Mode: order.Mode, Status: order.Status, CreatedAt: order.CreatedAt.UTC().Format(time.RFC3339Nano)}
	if order.TradeNo != "" {
		out.TradeNo = &order.TradeNo
	}
	if order.VerifiedBy != "" {
		out.VerifiedBy = &order.VerifiedBy
	}
	if order.PaidAt != nil {
		value := order.PaidAt.UTC().Format(time.RFC3339Nano)
		out.PaidAt = &value
	}
	return out
}

func fiatErrorBase(req *commonpb.BaseReq, err error) *commonpb.BaseResp {
	base := successBase(req)
	base.Message = err.Error()
	switch {
	case errors.Is(err, fiatservice.ErrNotFound):
		base.Code = int32(commonpb.ErrorCode_RESOURCE_NOT_FOUND)
	case errors.Is(err, fiatservice.ErrConflict):
		base.Code = int32(commonpb.ErrorCode_PAYMENT_ALREADY_EXISTS)
	default:
		base.Code = int32(commonpb.ErrorCode_INVALID_PARAMETERS)
	}
	return base
}

func (h *PaymentServiceHandler) InitiatePayment(ctx context.Context, req *paymentpb.InitiatePaymentRequest) (*paymentpb.InitiatePaymentResponse, error) {
	if req == nil {
		return &paymentpb.InitiatePaymentResponse{Base: errorBase(nil, payerrors.New(payerrors.INVALID_PARAMETERS, "request is required"))}, nil
	}
	appReq := &appdto.InitiatePaymentRequest{
		IdempotencyKey: baseIdempotencyKey(req.GetBase()),
		RequestID:      req.GetBase().GetRequestId(),
		TraceID:        req.GetBase().GetTraceId(),
		AgentDID:       string(req.GetAgentDid()),
		SkillDID:       string(req.GetSkillDid()),
		AmountStr:      utils.MinorUnitToString(req.GetAmountMinor()),
		Currency:       req.GetCurrency().String(),
		Signature:      req.GetSignature(),
		Timestamp:      parseInt64(req.GetTimestamp()),
		Nonce:          req.GetNonce(),
		SignedTxBase64: req.GetSignedTxBase64(),
	}
	resp, err := h.app.InitiatePayment(ctx, appReq)
	if err != nil {
		return &paymentpb.InitiatePaymentResponse{Base: errorBase(req.GetBase(), err)}, nil
	}
	return toInitiateResponse(req.GetBase(), resp), nil
}

func (h *PaymentServiceHandler) GetPaymentStatus(ctx context.Context, req *paymentpb.GetPaymentStatusRequest) (*paymentpb.GetPaymentStatusResponse, error) {
	if req == nil {
		return &paymentpb.GetPaymentStatusResponse{Base: errorBase(nil, payerrors.New(payerrors.INVALID_PARAMETERS, "request is required"))}, nil
	}
	resp, err := h.app.GetPaymentStatus(ctx, string(req.GetTxId()))
	if err != nil {
		return &paymentpb.GetPaymentStatusResponse{Base: errorBase(req.GetBase(), err)}, nil
	}
	out := &paymentpb.GetPaymentStatusResponse{
		Base:   successBase(req.GetBase()),
		TxId:   resp.TxID,
		Status: paymentStatus(resp.Status),
	}
	if resp.TxHash != "" {
		hash := commonpb.TxHash(resp.TxHash)
		out.TxHash = &hash
	}
	if resp.ConfirmedAt != "" {
		out.ConfirmedAt = &resp.ConfirmedAt
	}
	if resp.FailedAt != "" {
		out.FailedAt = &resp.FailedAt
	}
	return out, nil
}

func (h *PaymentServiceHandler) ListPaymentHistory(ctx context.Context, req *paymentpb.ListPaymentHistoryRequest) (*paymentpb.ListPaymentHistoryResponse, error) {
	if req == nil {
		return &paymentpb.ListPaymentHistoryResponse{Base: errorBase(nil, payerrors.New(payerrors.INVALID_PARAMETERS, "request is required"))}, nil
	}
	limit, offset := 20, 0
	if page := req.GetPage(); page != nil {
		limit, offset = int(page.GetLimit()), int(page.GetOffset())
	}
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	appReq := &appdto.ListPaymentHistoryRequest{
		AgentDID: string(req.GetAgentDid()),
		Page:     offset/limit + 1,
		PageSize: limit,
	}
	resp, err := h.app.ListPaymentHistory(ctx, appReq)
	if err != nil {
		return &paymentpb.ListPaymentHistoryResponse{Base: errorBase(req.GetBase(), err)}, nil
	}
	out := &paymentpb.ListPaymentHistoryResponse{
		Base: successBase(req.GetBase()),
		Page: &commonpb.PageResult_{Total: int32(resp.Total)},
	}
	for _, item := range resp.Items {
		if item == nil {
			continue
		}
		amountMinor, parseErr := strconv.ParseInt(item.Amount, 10, 64)
		if parseErr != nil {
			// Application amounts are decimal strings. The current canonical
			// contract is minor-unit based, so invalid conversion is surfaced as
			// a deterministic service error rather than silently truncating.
			return &paymentpb.ListPaymentHistoryResponse{Base: errorBase(req.GetBase(), payerrors.Wrap(payerrors.INTERNAL_SERVER_ERROR, parseErr, "invalid payment amount"))}, nil
		}
		currency, parseErr := commonpb.CurrencyFromString(strings.ToUpper(item.Currency))
		if parseErr != nil {
			return &paymentpb.ListPaymentHistoryResponse{Base: errorBase(req.GetBase(), payerrors.Wrap(payerrors.INTERNAL_SERVER_ERROR, parseErr, "invalid payment currency"))}, nil
		}
		out.Items = append(out.Items, &paymentpb.PaymentHistoryItem{
			TxId: item.TxID, SkillDid: item.SkillDID, AmountMinor: amountMinor,
			Currency: currency, Status: paymentStatus(item.Status), CreatedAt: item.CreatedAt,
		})
	}
	return out, nil
}

func (h *PaymentServiceHandler) GetPaymentRequirement(ctx context.Context, req *paymentpb.GetPaymentRequirementRequest) (*paymentpb.GetPaymentRequirementResponse, error) {
	if req == nil {
		return &paymentpb.GetPaymentRequirementResponse{Base: errorBase(nil, payerrors.New(payerrors.INVALID_PARAMETERS, "request is required"))}, nil
	}
	appReq := &appdto.GetPaymentRequirementRequest{
		SkillDID: string(req.GetSkillDid()), AgentDID: string(req.GetAgentDid()),
		SkillName: req.GetSkillName(), Amount: req.GetAmount(), Price: req.GetPrice(),
		Message: req.GetMessage(),
	}
	if req.IsSetCurrency() {
		appReq.Currency = req.GetCurrency().String()
	}
	resp, alreadyPurchased, err := h.app.GetPaymentRequirement(ctx, appReq)
	if err != nil {
		return &paymentpb.GetPaymentRequirementResponse{Base: errorBase(req.GetBase(), err)}, nil
	}
	out := &paymentpb.GetPaymentRequirementResponse{
		Base: successBase(req.GetBase()), AlreadyPurchased: alreadyPurchased,
	}
	if resp != nil {
		out.SkillDid = stringPtr(resp.SkillDID)
		out.SkillName = stringPtr(resp.SkillName)
		out.Price = stringPtr(resp.Price)
		out.Message = stringPtr(resp.Message)
		out.PaymentEndpoint = stringPtr(resp.Endpoint)
		if currency, parseErr := commonpb.CurrencyFromString(strings.ToUpper(resp.Currency)); parseErr == nil {
			out.Currency = &currency
		}
	}
	return out, nil
}

func successBase(req *commonpb.BaseReq) *commonpb.BaseResp {
	base := &commonpb.BaseResp{Code: int32(commonpb.ErrorCode_SUCCESS), Message: "success"}
	if req != nil {
		base.RequestId = req.RequestId
		base.TraceId = req.TraceId
	}
	return base
}

func errorBase(req *commonpb.BaseReq, err error) *commonpb.BaseResp {
	base := successBase(req)
	base.Code = int32(payerrors.GetErrorCode(err))
	base.Message = payerrors.GetErrorMessage(payerrors.GetErrorCode(err))
	if typed, ok := err.(*payerrors.Error); ok && typed.Message != "" {
		base.Message = typed.Message
	}
	return base
}

func baseIdempotencyKey(req *commonpb.BaseReq) string {
	if req == nil {
		return ""
	}
	return req.GetIdempotencyKey()
}

func parseInt64(value string) int64 {
	n, _ := strconv.ParseInt(value, 10, 64)
	return n
}

func stringPtr(value string) *string { return &value }

func paymentStatus(value string) commonpb.PaymentStatus {
	switch strings.ToUpper(value) {
	case "CONFIRMED", "COMPLETED":
		return commonpb.PaymentStatus_CONFIRMED
	case "FAILED", "CANCELLED":
		return commonpb.PaymentStatus_FAILED
	default:
		return commonpb.PaymentStatus_PENDING
	}
}

func toInitiateResponse(req *commonpb.BaseReq, resp *appdto.InitiatePaymentResponse) *paymentpb.InitiatePaymentResponse {
	out := &paymentpb.InitiatePaymentResponse{
		Base: successBase(req), TxId: resp.TxID, TxHash: resp.TxHash,
		Status: paymentStatus(resp.Status), CreatedAt: resp.CreatedAt,
	}
	if resp.ConfirmedAt != "" {
		out.ConfirmedAt = &resp.ConfirmedAt
	}
	return out
}

var _ paymentpb.PaymentService = (*PaymentServiceHandler)(nil)
