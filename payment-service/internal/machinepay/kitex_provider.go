package machinepay

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

func (p *KitexProvider) Precreate(ctx context.Context, order Order) (string, error) {
	resp, err := p.client.PrecreateMachinePay(ctx, &contract.MachinePayPrecreateRequest{Order: &contract.MachinePayOrder{
		OrderId: order.ID, ResourceId: order.ResourceID, GoodsName: order.GoodsName, AmountMinor: order.AmountMinor,
		PayBefore: order.ExpiresAt.UTC().Format(time.RFC3339),
	}})
	if err != nil {
		return "", err
	}
	if err := adapterError(resp.GetBase()); err != nil {
		return "", err
	}
	if resp.GetOrderId() != order.ID || resp.GetPaymentLink() == "" {
		return "", fmt.Errorf("Alipay Adapter returned an invalid Machine Pay precreate response")
	}
	return resp.GetPaymentLink(), nil
}

func (p *KitexProvider) VerifyProof(ctx context.Context, proof string) (Receipt, error) {
	resp, err := p.client.VerifyMachinePayProof(ctx, &contract.MachinePayProofRequest{PaymentProofHeader: proof})
	if err != nil {
		return Receipt{}, err
	}
	if err := adapterError(resp.GetBase()); err != nil {
		return Receipt{}, err
	}
	if resp.GetReceipt() == nil {
		return Receipt{}, fmt.Errorf("Alipay Adapter returned no Machine Pay receipt")
	}
	receipt := resp.GetReceipt()
	return Receipt{OrderID: receipt.GetOutTradeNo(), TradeNo: receipt.GetTradeNo(), ResourceID: receipt.GetResourceId(),
		AmountMinor: receipt.GetAmountMinor(), Active: receipt.GetActive(), VerifiedBy: receipt.GetVerifiedBy()}, nil
}

func adapterError(base interface {
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
