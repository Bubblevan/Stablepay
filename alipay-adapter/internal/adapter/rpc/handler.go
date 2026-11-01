package rpc

import (
	"context"

	"github.com/stablepay/alipay-adapter/internal/provider"
	contract "github.com/stablepay/alipay-adapter/kitex_gen/stablepay/alipay_adapter"
	common "github.com/stablepay/alipay-adapter/kitex_gen/stablepay/common"
)

type Handler struct{ alipay *provider.Alipay }

func NewHandler(alipay *provider.Alipay) *Handler { return &Handler{alipay: alipay} }
func (h *Handler) CreateCheckout(_ context.Context, req *contract.CheckoutRequest) (*contract.CheckoutResponse, error) {
	if req == nil || req.GetOrder() == nil {
		return &contract.CheckoutResponse{Base: failure("order is required")}, nil
	}
	o := req.GetOrder()
	link, err := h.alipay.Checkout(provider.Order{ID: o.GetOrderId(), Subject: o.GetSubject(), AmountMinor: o.GetAmountMinor()})
	if err != nil {
		return &contract.CheckoutResponse{Base: failure(err.Error())}, nil
	}
	return &contract.CheckoutResponse{Base: success(), CashierUrl: link}, nil
}
func (h *Handler) QueryTrade(ctx context.Context, req *contract.QueryRequest) (*contract.QueryResponse, error) {
	if req == nil || req.GetOrder() == nil {
		return &contract.QueryResponse{Base: failure("order is required")}, nil
	}
	o := req.GetOrder()
	receipt, err := h.alipay.Query(ctx, provider.Order{ID: o.GetOrderId(), Subject: o.GetSubject(), AmountMinor: o.GetAmountMinor()})
	if err != nil {
		return &contract.QueryResponse{Base: failure(err.Error())}, nil
	}
	return &contract.QueryResponse{Base: success(), Receipt: mapReceipt(receipt)}, nil
}
func (h *Handler) VerifyNotification(_ context.Context, req *contract.VerifyNotificationRequest) (*contract.VerifyNotificationResponse, error) {
	if req == nil {
		return &contract.VerifyNotificationResponse{Base: failure("notification parameters are required")}, nil
	}
	receipt, err := h.alipay.VerifyNotification(req.GetParameters())
	if err != nil {
		return &contract.VerifyNotificationResponse{Base: failure(err.Error())}, nil
	}
	return &contract.VerifyNotificationResponse{Base: success(), Receipt: mapReceipt(receipt)}, nil
}
func mapReceipt(r provider.Receipt) *contract.TradeReceipt {
	return &contract.TradeReceipt{OrderId: r.OrderID, TradeNo: r.TradeNo, AmountMinor: r.AmountMinor, Status: r.Status, VerifiedBy: r.VerifiedBy}
}
func success() *common.BaseResp {
	return &common.BaseResp{Code: int32(common.ErrorCode_SUCCESS), Message: "success"}
}
func failure(message string) *common.BaseResp {
	return &common.BaseResp{Code: int32(common.ErrorCode_INVALID_PARAMETERS), Message: message}
}
