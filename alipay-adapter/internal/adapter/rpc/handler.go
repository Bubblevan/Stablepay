package rpc

import (
	"context"
	"time"

	"github.com/stablepay/alipay-adapter/internal/provider"
	contract "github.com/stablepay/alipay-adapter/kitex_gen/stablepay/alipay_adapter"
	common "github.com/stablepay/alipay-adapter/kitex_gen/stablepay/common"
)

type Handler struct {
	alipay     *provider.Alipay
	machinePay *provider.MachinePay
}

func NewHandler(alipay *provider.Alipay, machinePay ...*provider.MachinePay) *Handler {
	h := &Handler{alipay: alipay}
	if len(machinePay) > 0 {
		h.machinePay = machinePay[0]
	}
	return h
}
func (h *Handler) CreateCheckout(_ context.Context, req *contract.CheckoutRequest) (*contract.CheckoutResponse, error) {
	if req == nil || req.GetOrder() == nil {
		return &contract.CheckoutResponse{Base: failure("order is required")}, nil
	}
	if h.alipay == nil {
		return &contract.CheckoutResponse{Base: failure("website Alipay provider is not configured")}, nil
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
	if h.alipay == nil {
		return &contract.QueryResponse{Base: failure("website Alipay provider is not configured")}, nil
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
	if h.alipay == nil {
		return &contract.VerifyNotificationResponse{Base: failure("website Alipay provider is not configured")}, nil
	}
	receipt, err := h.alipay.VerifyNotification(req.GetParameters())
	if err != nil {
		return &contract.VerifyNotificationResponse{Base: failure(err.Error())}, nil
	}
	return &contract.VerifyNotificationResponse{Base: success(), Receipt: mapReceipt(receipt)}, nil
}

func (h *Handler) PrecreateMachinePay(ctx context.Context, req *contract.MachinePayPrecreateRequest) (*contract.MachinePayPrecreateResponse, error) {
	if req == nil || req.GetOrder() == nil || h.machinePay == nil {
		return &contract.MachinePayPrecreateResponse{Base: failure("Machine Pay provider is not configured")}, nil
	}
	o := req.GetOrder()
	payBefore, err := time.Parse(time.RFC3339, o.GetPayBefore())
	if err != nil {
		return &contract.MachinePayPrecreateResponse{Base: failure("pay_before must be RFC3339")}, nil
	}
	link, err := h.machinePay.Precreate(ctx, provider.MachinePayOrder{OrderID: o.GetOrderId(), ResourceID: o.GetResourceId(), GoodsName: o.GetGoodsName(), AmountMinor: o.GetAmountMinor(), PayBefore: payBefore})
	if err != nil {
		return &contract.MachinePayPrecreateResponse{Base: failure(err.Error())}, nil
	}
	return &contract.MachinePayPrecreateResponse{Base: success(), OrderId: o.GetOrderId(), PaymentLink: link}, nil
}

func (h *Handler) VerifyMachinePayProof(ctx context.Context, req *contract.MachinePayProofRequest) (*contract.MachinePayVerifyResponse, error) {
	if req == nil || h.machinePay == nil {
		return &contract.MachinePayVerifyResponse{Base: failure("Machine Pay provider is not configured")}, nil
	}
	receipt, err := h.machinePay.VerifyProof(ctx, req.GetPaymentProofHeader())
	if err != nil {
		return &contract.MachinePayVerifyResponse{Base: failure(err.Error())}, nil
	}
	return &contract.MachinePayVerifyResponse{Base: success(), Receipt: &contract.MachinePayReceipt{OutTradeNo: receipt.OrderID, TradeNo: receipt.TradeNo, ResourceId: receipt.ResourceID, AmountMinor: receipt.AmountMinor, Active: receipt.Active, VerifiedBy: receipt.VerifiedBy}}, nil
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
