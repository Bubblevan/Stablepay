package fiat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/kitex/client"
	"github.com/cloudwego/kitex/pkg/retry"
	contract "github.com/stablepay/payment-service/kitex_gen/stablepay/alipay_adapter"
	service "github.com/stablepay/payment-service/kitex_gen/stablepay/alipay_adapter/alipayadapterservice"
)

type KitexProvider struct{ client service.Client }

func NewKitexProvider(address string, timeoutMs, retryCount int) (*KitexProvider, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, fmt.Errorf("rpc_clients.alipay_adapter.address is required")
	}
	opts := []client.Option{client.WithHostPorts(address)}
	if timeoutMs > 0 {
		opts = append(opts, client.WithRPCTimeout(time.Duration(timeoutMs)*time.Millisecond))
	}
	if retryCount > 0 {
		opts = append(opts, client.WithFailureRetry(retry.NewFailurePolicy()))
	}
	cli, err := service.NewClient("alipay-adapter", opts...)
	if err != nil {
		return nil, fmt.Errorf("create Alipay Adapter Kitex client: %w", err)
	}
	return &KitexProvider{client: cli}, nil
}
func (*KitexProvider) Mode() string { return "alipay_sandbox" }
func (p *KitexProvider) Checkout(ctx context.Context, order Order) (string, error) {
	resp, err := p.client.CreateCheckout(ctx, &contract.CheckoutRequest{Order: &contract.PaymentOrder{OrderId: order.ID, Subject: order.Subject, AmountMinor: order.AmountMinor}})
	if err != nil {
		return "", err
	}
	if err := providerError(resp.GetBase()); err != nil {
		return "", err
	}
	if resp.GetCashierUrl() == "" {
		return "", fmt.Errorf("Alipay Adapter returned an empty cashier URL")
	}
	return resp.GetCashierUrl(), nil
}
func (p *KitexProvider) Query(ctx context.Context, order Order) (Receipt, error) {
	resp, err := p.client.QueryTrade(ctx, &contract.QueryRequest{Order: &contract.PaymentOrder{OrderId: order.ID, Subject: order.Subject, AmountMinor: order.AmountMinor}})
	if err != nil {
		return Receipt{}, err
	}
	if err := providerError(resp.GetBase()); err != nil {
		return Receipt{}, err
	}
	return mapProviderReceipt(resp.GetReceipt())
}
func (p *KitexProvider) VerifyNotification(ctx context.Context, params map[string]string) (Receipt, error) {
	resp, err := p.client.VerifyNotification(ctx, &contract.VerifyNotificationRequest{Parameters: params})
	if err != nil {
		return Receipt{}, err
	}
	if err := providerError(resp.GetBase()); err != nil {
		return Receipt{}, err
	}
	return mapProviderReceipt(resp.GetReceipt())
}
func providerError(base interface {
	GetCode() int32
	GetMessage() string
}) error {
	if base == nil {
		return fmt.Errorf("Alipay Adapter returned no response status")
	}
	if base.GetCode() != 0 {
		return fmt.Errorf("Alipay Adapter: %s", base.GetMessage())
	}
	return nil
}
func mapProviderReceipt(receipt *contract.TradeReceipt) (Receipt, error) {
	if receipt == nil {
		return Receipt{}, fmt.Errorf("Alipay Adapter returned no trade receipt")
	}
	return Receipt{OrderID: receipt.GetOrderId(), TradeNo: receipt.GetTradeNo(), AmountMinor: receipt.GetAmountMinor(), Status: receipt.GetStatus(), VerifiedBy: receipt.GetVerifiedBy()}, nil
}
